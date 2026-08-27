package core

import (
	"context"
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

	// onRestore, if set, is called with the restore target so a test can simulate
	// restic writing files there (used to exercise the download zip).
	onRestore func(target string)

	// streamFn drives Backup/Restore/Forget. If nil, the op succeeds immediately.
	streamFn func(ctx context.Context, kind RunKind, sink RunSink) (int, error)

	mu              sync.Mutex
	snapTags        []string // tags passed to Snapshots, for assertions
	initCount       int      // number of Init calls, for assertions
	forgetCalls     []forgetCall
	forgetSnapCalls []forgetSnapshotsCall
}

type forgetCall struct {
	Tag    string
	Policy JobRetention
}

type forgetSnapshotsCall struct {
	IDs []string
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

func (f *fakeRunner) Restore(ctx context.Context, repo *Repository, snapshotID, target string, sink RunSink) (int, error) {
	sink.PID(4242, "faketoken")
	if f.onRestore != nil {
		f.onRestore(target)
	}
	if f.streamFn != nil {
		return f.streamFn(ctx, KindRestore, sink)
	}
	return 0, nil
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

func (f *fakeRunner) recordedForgetSnapshots() []forgetSnapshotsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forgetSnapshotsCall(nil), f.forgetSnapCalls...)
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
