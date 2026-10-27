package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// This file holds shared test doubles used across the package's tests: a fake
// Runner (so the run pipeline can be exercised with no restic binary) and a
// capture sink that records the events a Runner emits.

// captureSink records everything a Runner streams to it.
type captureSink struct {
	mu       sync.Mutex
	logs     []LogLine
	progress []Progress
	summary  *Summary
	pid      int
}

func (c *captureSink) Log(level, stream, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, LogLine{Level: level, Stream: stream, Message: message})
}

func (c *captureSink) Progress(p Progress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progress = append(c.progress, p)
}

func (c *captureSink) Summary(s Summary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := s
	c.summary = &cp
}

func (c *captureSink) PID(pid int, startToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pid = pid
}

func (c *captureSink) lastProgress() (Progress, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.progress) == 0 {
		return Progress{}, false
	}
	return c.progress[len(c.progress)-1], true
}

// fakeRunner is a scriptable Runner for tests. Streaming operations delegate to
// streamFn, which the test supplies to emit events, honor ctx cancellation, and
// return an exit code.
type fakeRunner struct {
	installed  bool
	version    string
	testResult TestResult
	initOut    string
	initErr    error
	snaps      []Snapshot
	snapErr    error
	unlockErr  error

	// snapshotsFn, if set, replaces snaps/snapErr (e.g. to block like a slow
	// backend until ctx ends).
	snapshotsFn func(ctx context.Context, tag string) ([]Snapshot, error)

	// onRestore, if set, is called with the restore target so a test can simulate
	// restic writing files there (used to exercise the download zip).
	onRestore func(target string)

	restoreCalls []restoreCall

	// streamFn drives Backup/Restore/Forget. If nil, the op succeeds immediately.
	streamFn func(ctx context.Context, kind RunKind, sink RunSink) (int, error)

	// dumpData maps "snapshotID file" to the bytes Dump streams. BackupStdin
	// stores what it was given here too, under the snapshot id it reports, so
	// a restore can read a safety backup back.
	dumpData map[string][]byte
	// dumpCode is the exit code Dump returns after streaming.
	dumpCode int
	lsNodes  []LsNode
	lsErr    error

	mu              sync.Mutex
	snapTags        []string // tags passed to Snapshots, for assertions
	initCount       int      // number of Init calls, for assertions
	forgetCalls     []forgetCall
	previewCalls    []forgetCall
	forgetSnapCalls []forgetSnapshotsCall
	dropCalls       []forgetSnapshotsCall
	checkCalls      []string
	stdinCalls      []stdinCall
	dumpCalls       []string
	lsCalls         int
}

type stdinCall struct {
	Filename string
	Tags     []string
	Data     string
}

type forgetCall struct {
	Tag    string
	Policy JobRetention
}

type forgetSnapshotsCall struct {
	IDs []string
}

type restoreCall struct {
	SnapshotID string
	Target     string
	Include    []string
}

func (f *fakeRunner) recordedTags() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.snapTags...)
}

func (f *fakeRunner) initCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initCount
}

func (f *fakeRunner) Available() bool { return f.installed }

func (f *fakeRunner) Version(ctx context.Context) (string, error) { return f.version, nil }

func (f *fakeRunner) Test(ctx context.Context, repo *Repository) TestResult { return f.testResult }

func (f *fakeRunner) Init(ctx context.Context, repo *Repository) (string, error) {
	f.mu.Lock()
	f.initCount++
	f.mu.Unlock()
	return f.initOut, f.initErr
}

func (f *fakeRunner) Snapshots(ctx context.Context, repo *Repository, tag string) ([]Snapshot, error) {
	f.mu.Lock()
	f.snapTags = append(f.snapTags, tag)
	f.mu.Unlock()
	if f.snapshotsFn != nil {
		return f.snapshotsFn(ctx, tag)
	}
	return f.snaps, f.snapErr
}

func (f *fakeRunner) Unlock(ctx context.Context, repo *Repository) error { return f.unlockErr }

func (f *fakeRunner) Backup(ctx context.Context, repo *Repository, source string, tags []string, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	if f.streamFn != nil {
		return f.streamFn(ctx, KindBackup, sink)
	}
	return 0, nil
}

func (f *fakeRunner) Restore(ctx context.Context, repo *Repository, snapshotID, target string, include []string, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	f.mu.Lock()
	f.restoreCalls = append(f.restoreCalls, restoreCall{
		SnapshotID: snapshotID,
		Target:     target,
		Include:    append([]string(nil), include...),
	})
	f.mu.Unlock()
	if f.onRestore != nil {
		f.onRestore(target)
	}
	if f.streamFn != nil {
		return f.streamFn(ctx, KindRestore, sink)
	}
	return 0, nil
}

func (f *fakeRunner) recordedRestores() []restoreCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]restoreCall, len(f.restoreCalls))
	copy(out, f.restoreCalls)
	return out
}

func (f *fakeRunner) Forget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	f.mu.Lock()
	f.forgetCalls = append(f.forgetCalls, forgetCall{Tag: tag, Policy: policy})
	f.mu.Unlock()
	if f.streamFn != nil {
		return f.streamFn(ctx, KindRetention, sink)
	}
	sink.Log("info", "stdout", "fake forget applied")
	return 0, nil
}

func (f *fakeRunner) PreviewForget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	f.mu.Lock()
	f.previewCalls = append(f.previewCalls, forgetCall{Tag: tag, Policy: policy})
	f.mu.Unlock()
	sink.Log("info", "stdout", "Keeping 3 snapshot(s), removing 2: a, b.")
	return 0, nil
}

func (f *fakeRunner) recordedForgets() []forgetCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forgetCall(nil), f.forgetCalls...)
}

func (f *fakeRunner) ForgetSnapshots(ctx context.Context, repo *Repository, snapshotIDs []string, sink RunSink) (int, error) {
	ids := uniqueNonEmpty(snapshotIDs)
	f.mu.Lock()
	f.forgetSnapCalls = append(f.forgetSnapCalls, forgetSnapshotsCall{IDs: append([]string(nil), ids...)})
	f.mu.Unlock()
	if len(ids) == 0 {
		sink.Log("info", "system", "No snapshots to forget.")
		return 0, nil
	}
	sink.PID(4242, "faketoken")
	if f.streamFn != nil {
		return f.streamFn(ctx, KindForget, sink)
	}
	sink.Log("info", "stdout", "fake forget snapshots applied")
	return 0, nil
}

func (f *fakeRunner) Check(ctx context.Context, repo *Repository, readData string, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	f.mu.Lock()
	f.checkCalls = append(f.checkCalls, readData)
	f.mu.Unlock()
	if f.streamFn != nil {
		return f.streamFn(ctx, KindCheck, sink)
	}
	sink.Log("info", "stdout", "no errors were found")
	return 0, nil
}

func (f *fakeRunner) recordedChecks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.checkCalls...)
}

func (f *fakeRunner) recordedForgetSnapshots() []forgetSnapshotsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forgetSnapshotsCall(nil), f.forgetSnapCalls...)
}

// DropSnapshots records the ids it was given; it never prunes, so it does not
// go through streamFn.
func (f *fakeRunner) DropSnapshots(ctx context.Context, repo *Repository, snapshotIDs []string, sink RunSink) (int, error) {
	ids := uniqueNonEmpty(snapshotIDs)
	f.mu.Lock()
	f.dropCalls = append(f.dropCalls, forgetSnapshotsCall{IDs: append([]string(nil), ids...)})
	f.mu.Unlock()
	if len(ids) > 0 {
		sink.PID(4242, "faketoken")
	}
	return 0, nil
}

func (f *fakeRunner) recordedDrops() []forgetSnapshotsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forgetSnapshotsCall(nil), f.dropCalls...)
}

func (f *fakeRunner) BackupStdin(ctx context.Context, repo *Repository, filename string, tags []string, produce func(w io.Writer) error, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	var buf bytes.Buffer
	if err := produce(&buf); err != nil {
		return resticExitInterrupted, err
	}
	f.mu.Lock()
	f.stdinCalls = append(f.stdinCalls, stdinCall{Filename: filename, Tags: append([]string(nil), tags...), Data: buf.String()})
	id := fmt.Sprintf("stdinsnap%02d", len(f.stdinCalls))
	if f.dumpData == nil {
		f.dumpData = map[string][]byte{}
	}
	f.dumpData[id+" /"+filename] = buf.Bytes()
	f.mu.Unlock()
	if f.streamFn != nil {
		code, err := f.streamFn(ctx, KindBackup, sink)
		if code != 0 || err != nil {
			return code, err
		}
	}
	sink.Summary(Summary{SnapshotID: id, DataAdded: int64(buf.Len()), TotalBytesProcessed: int64(buf.Len())})
	return 0, nil
}

func (f *fakeRunner) recordedStdinBackups() []stdinCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]stdinCall(nil), f.stdinCalls...)
}

func (f *fakeRunner) Dump(ctx context.Context, repo *Repository, snapshotID, file string, consume func(r io.Reader) error, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	f.mu.Lock()
	f.dumpCalls = append(f.dumpCalls, snapshotID+" "+file)
	data := f.dumpData[snapshotID+" "+file]
	code := f.dumpCode
	f.mu.Unlock()
	if err := consume(bytes.NewReader(data)); err != nil {
		return 0, err
	}
	return code, nil
}

func (f *fakeRunner) recordedDumps() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dumpCalls...)
}

func (f *fakeRunner) List(ctx context.Context, repo *Repository, snapshotID string) ([]LsNode, error) {
	f.mu.Lock()
	f.lsCalls++
	f.mu.Unlock()
	return f.lsNodes, f.lsErr
}

// fakeDocker is a scriptable Docker Engine. Exec delegates to execFn with the
// command's whole stdin; without one, every command succeeds silently.
type fakeDocker struct {
	pingErr    error
	containers []Container
	execFn     func(c Container, cmd []string, stdin string) (stdout, stderr string, code int)

	mu        sync.Mutex
	execCalls []execCall
	stopped   []string
	started   []string
}

type execCall struct {
	Container string
	Kind      string // dbCmdKind of the command
	Args      []string
	Stdin     string
}

func (d *fakeDocker) Ping(ctx context.Context) error { return d.pingErr }

func (d *fakeDocker) Containers(ctx context.Context) ([]Container, error) {
	if d.pingErr != nil {
		return nil, d.pingErr
	}
	out := make([]Container, len(d.containers))
	for i, c := range d.containers {
		c.Env, c.Aliases = nil, nil // like the list endpoint
		out[i] = c
	}
	return out, nil
}

func (d *fakeDocker) find(id string) (Container, bool) {
	for _, c := range d.containers {
		if c.ID == id || c.Name == id || (len(id) >= 12 && strings.HasPrefix(c.ID, id)) {
			return c, true
		}
	}
	return Container{}, false
}

func (d *fakeDocker) Inspect(ctx context.Context, id string) (Container, error) {
	if c, ok := d.find(id); ok {
		return c, nil
	}
	return Container{}, &dockerAPIError{Status: 404, Message: "No such container: " + id}
}

func (d *fakeDocker) Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	c, ok := d.find(id)
	if !ok {
		return -1, &dockerAPIError{Status: 404, Message: "No such container: " + id}
	}
	if len(cmd) > 2 && cmd[0] == "env" {
		cmd = cmd[2:] // the token dbCommand prefixes
	}
	var in []byte
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return -1, err
		}
		in = b
	}
	var args []string
	if len(cmd) > 4 {
		args = cmd[4:]
	}
	d.mu.Lock()
	d.execCalls = append(d.execCalls, execCall{Container: c.Name, Kind: dbCmdKind(cmd), Args: args, Stdin: string(in)})
	d.mu.Unlock()
	if d.execFn == nil {
		return 0, nil
	}
	out, errOut, code := d.execFn(c, cmd, string(in))
	if stdout != nil {
		if _, err := io.WriteString(stdout, out); err != nil {
			return -1, err
		}
	}
	if stderr != nil {
		_, _ = io.WriteString(stderr, errOut)
	}
	return code, nil
}

func (d *fakeDocker) Stop(ctx context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = append(d.stopped, id)
	return nil
}

func (d *fakeDocker) Start(ctx context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started = append(d.started, id)
	return nil
}

func (d *fakeDocker) recordedExecs() []execCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]execCall(nil), d.execCalls...)
}

// dbCmdKind names which database command a dbengine.go builder produced.
func dbCmdKind(cmd []string) string {
	if len(cmd) < 3 {
		return "?"
	}
	script := cmd[2]
	switch {
	case strings.Contains(script, "/proc/[0-9]*"):
		return "kill"
	case strings.Contains(script, "pg_dump") || strings.Contains(script, `exec "$DUMP"`):
		return "dump"
	case strings.Contains(script, "SHOW DATABASES") || strings.Contains(script, "FROM pg_database WHERE NOT datistemplate"):
		return "list"
	case strings.Contains(script, "pg_db_role_setting"):
		return "props"
	case strings.Contains(script, "RENAME TO"):
		return "swap"
	case strings.Contains(script, "ALLOW_CONNECTIONS true"):
		return "allow"
	case strings.Contains(script, "pg_get_userbyid") || (len(cmd) > 4 && strings.Contains(cmd[4], "information_schema.SCHEMATA")):
		return "meta"
	case strings.Contains(script, "DROP DATABASE") || (len(cmd) > 5 && strings.Contains(cmd[5], "DROP DATABASE")):
		return "drop"
	case strings.Contains(script, "CREATE DATABASE") || (len(cmd) > 4 && strings.HasPrefix(cmd[4], "CREATE DATABASE")):
		return "create"
	case strings.Contains(script, "--single-transaction >/dev/null") || strings.Contains(script, "--default-character-set=utf8mb4 \"$1\""):
		return "import"
	}
	return "?"
}

// gatedStream is a streamFn that emits an initial log + progress line, signals
// on started (non-blocking, so it is safe when shared across concurrent runs),
// then waits: a canceled context returns 130 (restic's SIGINT code), while a
// closed release channel emits a summary and returns exit code 0. It lets a test
// hold an operation "running" and then let it finish or be stopped.
func gatedStream(started chan<- struct{}, release <-chan struct{}) func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
	return func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Log("info", "system", "working")
		sink.Progress(Progress{Percent: 0.5, FilesDone: 1, TotalFiles: 2, BytesDone: 50, TotalBytes: 100})
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		select {
		case <-ctx.Done():
			return 130, nil
		case <-release:
			sink.Summary(Summary{SnapshotID: "snap-gated", FilesNew: 2, DataAdded: 50, TotalDuration: 0.1})
			return 0, nil
		}
	}
}
