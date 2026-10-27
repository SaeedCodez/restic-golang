package core

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"time"
)

// A speed test backs up random data into a repository and reads it back,
// timing each against the same operation on a tiny file. The difference is
// how fast data moves; the tiny operations are what every backup or restore
// pays before any data moves (opening the repository, unlocking its key,
// loading the index, taking a lock). Both snapshots are forgotten afterwards,
// without a prune: their data is reclaimed by the next prune.

// SpeedTestTag marks the snapshots a speed test makes, so one left behind by
// a test that died is found and forgotten by the next.
const SpeedTestTag = "resticweb-speedtest"

const (
	speedTestFile = "restic-web-speedtest.bin"
	// speedTestBaseBytes is the size of the baseline file. It is not empty
	// because restic saves no file for empty input; 1 KiB takes no measurable
	// time to move.
	speedTestBaseBytes = 1 << 10
	// SpeedTestDefaultMB is the test size when none is given.
	SpeedTestDefaultMB = 64
	speedTestMaxMB     = 1024
	// speedTestCleanup bounds forgetting the test snapshots, which runs even
	// after the test was stopped.
	speedTestCleanup = 2 * time.Minute
)

// StartSpeedTest measures how fast a repository takes and returns data,
// moving sizeMB MiB of random data each way (0 means SpeedTestDefaultMB).
func (c *Coordinator) StartSpeedTest(repoID string, sizeMB int) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	if sizeMB == 0 {
		sizeMB = SpeedTestDefaultMB
	}
	if sizeMB < 1 || sizeMB > speedTestMaxMB {
		return nil, validf("the test size must be between 1 and %d MB", speedTestMaxMB)
	}
	size := int64(sizeMB) << 20
	run := &Run{
		Kind: KindSpeedTest, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: map[string]string{"sizeBytes": strconv.FormatInt(size, 10)},
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		t := &speedTest{runner: c.runner, repo: &repo, h: h, size: size}
		return t.run(ctx)
	})
}

type speedTest struct {
	runner Runner
	repo   *Repository
	h      *runHandle
	size   int64

	made     []string // snapshots this test created, forgotten at the end
	lastTick time.Time
}

func (t *speedTest) run(ctx context.Context) (int, error) {
	t.h.Log("info", "system", fmt.Sprintf("Testing %s with %s of random data each way", t.repo.Name, mib(t.size)))
	defer t.cleanup()

	if code, err := t.dropLeftovers(ctx); code != 0 || err != nil {
		return code, err
	}

	t.h.Log("info", "system", "Timing the backup and restore of a tiny file…")
	baseUp, baseID, code, err := t.backup(ctx, speedTestBaseBytes, 0, 0.05)
	if code != 0 || err != nil {
		return code, err
	}
	baseDown, code, err := t.restore(ctx, baseID, speedTestBaseBytes, 0.05, 0.10, 0)
	if code != 0 || err != nil {
		return code, err
	}
	t.h.Log("info", "system", fmt.Sprintf("Backing it up took %s and restoring it %s.", secs(baseUp), secs(baseDown)))

	t.mark(0.10, 0)
	t.h.Log("info", "system", fmt.Sprintf("Uploading %s…", mib(t.size)))
	up, id, code, err := t.backup(ctx, t.size, 0.10, 0.55)
	if code != 0 || err != nil {
		return code, err
	}
	t.mark(0.55, t.size)
	t.h.Log("info", "system", fmt.Sprintf("Uploaded in %s. Downloading it back…", secs(up)))
	down, code, err := t.restore(ctx, id, t.size, 0.55, 0.95, t.size)
	if code != 0 || err != nil {
		return code, err
	}
	t.mark(0.95, 2*t.size)

	res := scoreSpeed(measureSpeed(t.size, speedTestBaseBytes, baseUp, up, baseDown, down))
	t.h.Summary(Summary{Speed: &res})
	t.h.Log("ok", "system", fmt.Sprintf("Upload %s/s, download %s/s, %s overhead per operation. Score %d of 100: %s.",
		mib(int64(res.UploadBytesPerSec)), mib(int64(res.DownloadBytesPerSec)),
		secs(time.Duration(res.OverheadSeconds*float64(time.Second))), res.Score, res.Rating))
	return 0, nil
}

// dropLeftovers forgets test snapshots a previous test left behind.
func (t *speedTest) dropLeftovers(ctx context.Context) (int, error) {
	snaps, err := t.runner.Snapshots(ctx, t.repo, SpeedTestTag)
	if err != nil {
		return 0, fmt.Errorf("could not list the repository's snapshots: %w", err)
	}
	var ids []string
	for _, s := range snaps {
		if slices.Contains(s.Tags, SpeedTestTag) {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	t.h.Log("info", "system", fmt.Sprintf("Forgetting %d snapshot(s) left by an earlier speed test…", len(ids)))
	code, err := t.runner.DropSnapshots(ctx, t.repo, ids, &speedSink{h: t.h})
	if code == resticExitWarnings {
		code = 0
	}
	return code, err
}

// backup stores size bytes of random data as a tagged snapshot, reporting
// progress between from and to, and returns how long restic took.
func (t *speedTest) backup(ctx context.Context, size int64, from, to float64) (time.Duration, string, int, error) {
	data, err := newRandomReader(size)
	if err != nil {
		return 0, "", 0, err
	}
	sink := &speedSink{h: t.h}
	start := time.Now()
	code, err := t.runner.BackupStdin(ctx, t.repo, speedTestFile, []string{SpeedTestTag}, func(w io.Writer) error {
		_, err := io.Copy(w, &countingReader{r: data, tick: func(n int64) { t.tick(from, to, n, 0) }})
		return err
	}, sink)
	took := time.Since(start)
	if id := sink.snapshotID(); id != "" {
		t.made = append(t.made, id)
	}
	if code == resticExitWarnings {
		// The only source is this test's own data, so a warning means restic
		// did not store it.
		return took, "", 0, errors.New("restic could not store the test data; see the log")
	}
	if code != 0 || err != nil {
		return took, "", code, err
	}
	id := sink.snapshotID()
	if id == "" {
		return took, "", 0, errors.New("restic did not report the test snapshot")
	}
	return took, id, 0, nil
}

// restore reads the test file of snapshotID back, discarding it, and returns
// how long restic took. Anything but exactly size bytes fails the test. before
// is what earlier phases moved, for progress.
func (t *speedTest) restore(ctx context.Context, snapshotID string, size int64, from, to float64, before int64) (time.Duration, int, error) {
	var got int64
	start := time.Now()
	code, err := t.runner.Dump(ctx, t.repo, snapshotID, "/"+speedTestFile, func(r io.Reader) error {
		n, err := io.Copy(io.Discard, &countingReader{r: r, tick: func(n int64) { t.tick(from, to, n, before) }})
		got = n
		return err
	}, &speedSink{h: t.h})
	took := time.Since(start)
	if code != 0 || err != nil {
		return took, code, err
	}
	if got != size {
		return took, 0, fmt.Errorf("the test file came back as %d bytes instead of %d", got, size)
	}
	return took, 0, nil
}

// tick reports progress through a phase that moves t.size bytes (the baseline
// phases move too little to show), at most ten times a second. before is what
// earlier phases moved.
func (t *speedTest) tick(from, to float64, n, before int64) {
	if time.Since(t.lastTick) < 100*time.Millisecond {
		return
	}
	t.lastTick = time.Now()
	frac := from
	if t.size > 0 {
		frac += (to - from) * math.Min(1, float64(n)/float64(t.size))
	}
	t.h.Progress(Progress{Percent: frac, BytesDone: before + n, TotalBytes: 2 * t.size})
}

// mark reports progress at the boundary between two phases.
func (t *speedTest) mark(frac float64, moved int64) {
	t.lastTick = time.Now()
	t.h.Progress(Progress{Percent: frac, BytesDone: moved, TotalBytes: 2 * t.size})
}

// cleanup forgets the snapshots this test made. It runs however the test
// ended, on its own context, so a stopped test does not leave them behind.
func (t *speedTest) cleanup() {
	if len(t.made) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), speedTestCleanup)
	defer cancel()
	t.h.Log("info", "system", "Forgetting the test snapshots; their data is reclaimed at the next prune…")
	code, err := t.runner.DropSnapshots(ctx, t.repo, t.made, &speedSink{h: t.h})
	switch {
	case err != nil:
		t.h.Log("warn", "system", "Could not forget the test snapshots: "+err.Error()+". The next speed test forgets them.")
	case code != 0 && code != resticExitWarnings:
		t.h.Log("warn", "system", fmt.Sprintf("Could not forget the test snapshots (restic exit code %d). The next speed test forgets them.", code))
	default:
		t.h.Progress(Progress{Percent: 1, BytesDone: 2 * t.size, TotalBytes: 2 * t.size})
	}
}

// measureSpeed turns the four timings into speeds: size bytes against the
// base bytes of the baseline. The time the difference took to move is each
// timing less its baseline, kept to at least a tenth of the whole so timing
// noise cannot claim an impossible speed.
func measureSpeed(size, base int64, baseUp, up, baseDown, down time.Duration) SpeedTest {
	transfer := func(total, baseline time.Duration) float64 {
		d := max(total-baseline, total/10, time.Millisecond)
		return d.Seconds()
	}
	upS, downS := transfer(up, baseUp), transfer(down, baseDown)
	moved := float64(max(size-base, 0))
	return SpeedTest{
		SizeBytes:           size,
		UploadSeconds:       upS,
		DownloadSeconds:     downS,
		OverheadSeconds:     (baseUp + baseDown).Seconds() / 2,
		UploadBytesPerSec:   moved / upS,
		DownloadBytesPerSec: moved / downS,
	}
}

// Speed scores run on a log scale between a speed that scores 0 and one that
// scores 100: for transfers 1 MiB/s and 100 MiB/s (10 MiB/s scores 50), for
// the overhead of an operation 30 s and 1 s.
const (
	speedSlowBps     = 1 << 20
	speedFastBps     = 100 << 20
	speedSlowSeconds = 30.0
	speedFastSeconds = 1.0
)

// scoreSpeed fills in Score and Rating: upload and download weigh 40% each,
// the per-operation overhead 20%.
func scoreSpeed(s SpeedTest) SpeedTest {
	up := logScore(s.UploadBytesPerSec, speedSlowBps, speedFastBps)
	down := logScore(s.DownloadBytesPerSec, speedSlowBps, speedFastBps)
	over := 100.0
	if s.OverheadSeconds > 0 {
		over = logScore(1/s.OverheadSeconds, 1/speedSlowSeconds, 1/speedFastSeconds)
	}
	s.Score = int(math.Round(0.4*up + 0.4*down + 0.2*over))
	s.Rating = speedRating(s.Score)
	return s
}

// logScore places v on a log scale where zero scores 0 and full scores 100,
// clamped to that range.
func logScore(v, zero, full float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Max(0, math.Min(100, 100*math.Log(v/zero)/math.Log(full/zero)))
}

func speedRating(score int) string {
	switch {
	case score >= 80:
		return "Excellent"
	case score >= 60:
		return "Good"
	case score >= 40:
		return "Fair"
	case score >= 20:
		return "Slow"
	default:
		return "Very slow"
	}
}

// speedSink passes a step's log lines and process id to the run but keeps its
// summary aside, and drops restic's own progress: the test reports progress
// across all of its steps itself.
type speedSink struct {
	h       *runHandle
	mu      sync.Mutex
	summary *Summary
}

func (s *speedSink) Log(level, stream, message string) { s.h.Log(level, stream, message) }
func (s *speedSink) Progress(Progress)                 {}
func (s *speedSink) PID(pid int, startToken string)    { s.h.PID(pid, startToken) }
func (s *speedSink) Summary(sm Summary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary = &sm
}

func (s *speedSink) snapshotID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.summary == nil {
		return ""
	}
	return s.summary.SnapshotID
}

// newRandomReader yields size bytes no compressor or deduplicator can shrink:
// a ChaCha8 stream from a fresh random seed, so no two tests share data.
func newRandomReader(size int64) (io.Reader, error) {
	var seed [32]byte
	if _, err := crand.Read(seed[:]); err != nil {
		return nil, err
	}
	return io.LimitReader(rand.NewChaCha8(seed), size), nil
}

// countingReader reports the running total of bytes read through it.
type countingReader struct {
	r    io.Reader
	n    int64
	tick func(n int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if n > 0 {
		c.tick(c.n)
	}
	return n, err
}

// mib formats a byte count the way the UI does (binary units, "MB").
func mib(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func secs(d time.Duration) string { return fmt.Sprintf("%.1f s", d.Seconds()) }
