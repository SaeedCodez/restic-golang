package main

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

func (c *captureSink) PID(pid int) {
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

	// streamFn drives Backup/Restore. If nil, the op succeeds immediately.
	streamFn func(ctx context.Context, kind RunKind, sink RunSink) (int, error)
}

func (f *fakeRunner) Available() bool { return f.installed }

func (f *fakeRunner) Version(ctx context.Context) (string, error) { return f.version, nil }

func (f *fakeRunner) Test(ctx context.Context, repo *Repository) TestResult { return f.testResult }

func (f *fakeRunner) Init(ctx context.Context, repo *Repository) (string, error) {
	return f.initOut, f.initErr
}

func (f *fakeRunner) Snapshots(ctx context.Context, repo *Repository, tag string) ([]Snapshot, error) {
	return f.snaps, f.snapErr
}

func (f *fakeRunner) Unlock(ctx context.Context, repo *Repository) error { return f.unlockErr }

func (f *fakeRunner) Backup(ctx context.Context, repo *Repository, source string, tags []string, sink RunSink) (int, error) {
	sink.PID(4242)
	if f.streamFn != nil {
		return f.streamFn(ctx, KindBackup, sink)
	}
	return 0, nil
}

func (f *fakeRunner) Restore(ctx context.Context, repo *Repository, snapshotID, target string, sink RunSink) (int, error) {
	sink.PID(4242)
	if f.streamFn != nil {
		return f.streamFn(ctx, KindRestore, sink)
	}
	return 0, nil
}

// blockUntilCancel is a streamFn that emits an initial progress line then blocks
// until the context is canceled, returning exit code 130 (restic's SIGINT code).
func blockUntilCancel(started chan<- struct{}) func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
	return func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Log("info", "system", "started")
		sink.Progress(Progress{Percent: 0.1, TotalBytes: 100})
		if started != nil {
			close(started)
		}
		<-ctx.Done()
		return 130, nil
	}
}
