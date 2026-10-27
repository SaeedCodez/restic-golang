package core

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestScoreSpeed(t *testing.T) {
	const mb = 1 << 20
	cases := []struct {
		name       string
		up, down   float64 // bytes per second
		overhead   float64 // seconds
		wantScore  int
		wantRating string
	}{
		{"everything at the top", 100 * mb, 100 * mb, 1, 100, "Excellent"},
		{"beyond the top is clamped", 1000 * mb, 1000 * mb, 0.1, 100, "Excellent"},
		{"10 MB/s both ways, 1 s", 10 * mb, 10 * mb, 1, 60, "Good"},
		{"10 MB/s both ways, 30 s", 10 * mb, 10 * mb, 30, 40, "Fair"},
		{"1 MB/s both ways, 30 s", 1 * mb, 1 * mb, 30, 0, "Very slow"},
		{"below the bottom is clamped", 0.1 * mb, 0.1 * mb, 300, 0, "Very slow"},
		{"fast upload, slow download", 100 * mb, 1 * mb, 1, 60, "Good"},
		{"no data measured", 0, 0, 0, 20, "Slow"},
	}
	for _, c := range cases {
		got := scoreSpeed(SpeedTest{UploadBytesPerSec: c.up, DownloadBytesPerSec: c.down, OverheadSeconds: c.overhead})
		if got.Score != c.wantScore || got.Rating != c.wantRating {
			t.Errorf("%s: score %d %q, want %d %q", c.name, got.Score, got.Rating, c.wantScore, c.wantRating)
		}
	}
}

func TestSpeedRatingBands(t *testing.T) {
	for score, want := range map[int]string{100: "Excellent", 80: "Excellent", 79: "Good", 60: "Good", 59: "Fair", 40: "Fair", 39: "Slow", 20: "Slow", 19: "Very slow", 0: "Very slow"} {
		if got := speedRating(score); got != want {
			t.Errorf("speedRating(%d) = %q, want %q", score, got, want)
		}
	}
}

func TestMeasureSpeed(t *testing.T) {
	const size, base = 64 << 20, 1 << 10
	// 1 s of overhead each way; the data took 3 s up and 1 s down.
	got := measureSpeed(size, base, time.Second, 4*time.Second, time.Second, 2*time.Second)
	if got.UploadSeconds != 3 || got.DownloadSeconds != 1 || got.OverheadSeconds != 1 {
		t.Fatalf("seconds = up %v down %v overhead %v", got.UploadSeconds, got.DownloadSeconds, got.OverheadSeconds)
	}
	if got.SizeBytes != size || got.UploadBytesPerSec != (size-base)/3.0 || got.DownloadBytesPerSec != size-base {
		t.Fatalf("speeds = up %v down %v", got.UploadBytesPerSec, got.DownloadBytesPerSec)
	}

	// A baseline slower than the full operation (timing noise) cannot make
	// the transfer take no time: it counts as at least a tenth of the whole.
	noisy := measureSpeed(size, base, 3*time.Second, 2*time.Second, 0, 0)
	if noisy.UploadSeconds != 0.2 {
		t.Fatalf("noisy upload seconds = %v, want 0.2", noisy.UploadSeconds)
	}
	if noisy.DownloadSeconds != 0.001 {
		t.Fatalf("instant download seconds = %v, want 0.001", noisy.DownloadSeconds)
	}
}

func TestSpeedTestRun(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	repoID, _ := makeJob(t, app, "speed", "/data/s")

	var resp struct {
		RunID string `json:"runId"`
	}
	if code := doJSON(t, h, "POST", "/api/repositories/"+repoID+"/speedtest", map[string]any{"sizeMB": 1}, &resp); code != 202 {
		t.Fatalf("start status = %d", code)
	}
	run := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 3*time.Second)
	if run.Kind != KindSpeedTest || run.Params["sizeBytes"] != "1048576" {
		t.Fatalf("run = kind %s params %+v", run.Kind, run.Params)
	}

	// A tiny baseline file, then 1 MiB of data that does not compress.
	backups := fake.recordedStdinBackups()
	if len(backups) != 2 {
		t.Fatalf("stdin backups = %d, want 2", len(backups))
	}
	for _, b := range backups {
		if b.Filename != speedTestFile || !slices.Equal(b.Tags, []string{SpeedTestTag}) {
			t.Fatalf("backup = %q tags %v", b.Filename, b.Tags)
		}
	}
	if len(backups[0].Data) != speedTestBaseBytes || len(backups[1].Data) != 1<<20 {
		t.Fatalf("backup sizes = %d, %d", len(backups[0].Data), len(backups[1].Data))
	}
	if fresh := measureSpeedData(t); fresh == backups[1].Data[:64] {
		t.Fatal("two tests sent the same data; it must be random each time")
	}

	// Each one read back, then both forgotten without a prune.
	if dumps := fake.recordedDumps(); !slices.Equal(dumps, []string{"stdinsnap01 /" + speedTestFile, "stdinsnap02 /" + speedTestFile}) {
		t.Fatalf("dumps = %v", dumps)
	}
	if drops := fake.recordedDrops(); len(drops) != 1 || !slices.Equal(drops[0].IDs, []string{"stdinsnap01", "stdinsnap02"}) {
		t.Fatalf("drops = %+v", drops)
	}
	if len(fake.recordedForgetSnapshots()) != 0 {
		t.Fatal("a speed test must not prune")
	}

	sp := run.Summary.Speed
	if sp == nil || sp.SizeBytes != 1<<20 || sp.UploadBytesPerSec <= 0 || sp.DownloadBytesPerSec <= 0 || sp.Rating == "" {
		t.Fatalf("speed = %+v", sp)
	}
	if sp.Score < 0 || sp.Score > 100 {
		t.Fatalf("score = %d", sp.Score)
	}

	// The result is listed for the repository and on the repository itself.
	var list struct {
		Results []SpeedTestView `json:"results"`
	}
	if code := doJSON(t, h, "GET", "/api/repositories/"+repoID+"/speedtests", nil, &list); code != 200 {
		t.Fatalf("list status = %d", code)
	}
	if len(list.Results) != 1 || list.Results[0].RunID != run.ID || list.Results[0].Score != sp.Score {
		t.Fatalf("results = %+v", list.Results)
	}
	var repos struct {
		Repositories []repoView `json:"repositories"`
	}
	doJSON(t, h, "GET", "/api/repositories", nil, &repos)
	if len(repos.Repositories) != 1 || repos.Repositories[0].LastSpeedTest == nil || repos.Repositories[0].LastSpeedTest.RunID != run.ID {
		t.Fatalf("repositories = %+v", repos.Repositories)
	}
	var one struct {
		Repository repoView `json:"repository"`
	}
	doJSON(t, h, "GET", "/api/repositories/"+repoID, nil, &one)
	if one.Repository.LastSpeedTest == nil || one.Repository.LastSpeedTest.Score != sp.Score {
		t.Fatalf("repository = %+v", one.Repository)
	}
}

// measureSpeedData returns the start of a fresh random test stream.
func measureSpeedData(t *testing.T) string {
	t.Helper()
	r, err := newRandomReader(64)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func TestSpeedTestForgetsLeftovers(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{
		{ID: "old-test", Tags: []string{SpeedTestTag}},
		{ID: "a-backup", Tags: []string{"resticweb-job:x"}},
	}}
	app := newRunTestApp(t, fake)
	repoID, _ := makeJob(t, app, "leftover", "/data/l")

	run, err := app.Coord.StartSpeedTest(repoID, 1)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 3*time.Second)
	drops := fake.recordedDrops()
	if len(drops) != 2 || !slices.Equal(drops[0].IDs, []string{"old-test"}) {
		t.Fatalf("drops = %+v; want the leftover first, and only it", drops)
	}
}

func TestSpeedTestFailedReadStillCleansUp(t *testing.T) {
	fake := &fakeRunner{installed: true, dumpCode: 1}
	app := newRunTestApp(t, fake)
	repoID, _ := makeJob(t, app, "badread", "/data/b")

	run, err := app.Coord.StartSpeedTest(repoID, 1)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	got := waitForStatus(t, app.Runs, run.ID, StatusFailed, 3*time.Second)
	if got.Summary != nil && got.Summary.Speed != nil {
		t.Fatal("a failed test must not report a speed")
	}
	if drops := fake.recordedDrops(); len(drops) != 1 || !slices.Equal(drops[0].IDs, []string{"stdinsnap01"}) {
		t.Fatalf("drops = %+v; the baseline test snapshot must still be forgotten", drops)
	}
	if results := app.Runs.SpeedTests(repoID, 10); len(results) != 0 {
		t.Fatalf("results = %d, want none from a failed test", len(results))
	}
}

func TestSpeedTestStoppedStillCleansUp(t *testing.T) {
	var backups atomic.Int32
	uploading := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		if kind != KindBackup || backups.Add(1) < 2 {
			return 0, nil
		}
		close(uploading)
		<-ctx.Done()
		return resticExitInterrupted, nil
	}}
	app := newRunTestApp(t, fake)
	repoID, _ := makeJob(t, app, "stopped", "/data/st")

	run, err := app.Coord.StartSpeedTest(repoID, 1)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	<-uploading
	if err := app.Coord.Stop(run.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusCanceled, 3*time.Second)
	if drops := fake.recordedDrops(); len(drops) != 1 || !slices.Equal(drops[0].IDs, []string{"stdinsnap01"}) {
		t.Fatalf("drops = %+v; the test snapshot made before the stop must be forgotten", drops)
	}
}

func TestSpeedTestValidationAndBusy(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	repoID, jobID := makeJob(t, app, "busy", "/data/bz")

	for _, size := range []int{-1, 1025} {
		var verr *ValidationError
		if _, err := app.Coord.StartSpeedTest(repoID, size); !errors.As(err, &verr) {
			t.Fatalf("size %d: err = %v, want a validation error", size, err)
		}
	}
	if _, err := app.Coord.StartSpeedTest("nope", 1); err == nil {
		t.Fatal("unknown repository should fail")
	}

	backup, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	<-started
	var busy *BusyError
	if _, err := app.Coord.StartSpeedTest(repoID, 1); !errors.As(err, &busy) {
		t.Fatalf("err = %v, want busy while a backup runs", err)
	}
	close(release)
	waitForStatus(t, app.Runs, backup.ID, StatusSuccess, 3*time.Second)
}
