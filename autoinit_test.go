package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestBackupAutoInitializesRepository verifies that a backup against an
// uninitialized repository (restic exit 10) initializes it and retries, so a job
// "just runs" without a separate Initialize step.
func TestBackupAutoInitializesRepository(t *testing.T) {
	var attempts atomic.Int32
	fake := &fakeRunner{installed: true, initOut: "created restic repository", streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		if attempts.Add(1) == 1 {
			sink.Log("error", "stdout", "Fatal: repository does not exist")
			return resticExitNotInitialized, nil
		}
		sink.Summary(Summary{SnapshotID: "snap-after-init", FilesNew: 1, DataAdded: 10})
		return 0, nil
	}}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	run, err := app.coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Summary == nil || final.Summary.SnapshotID != "snap-after-init" {
		t.Fatalf("expected the retried backup's snapshot, got %+v", final.Summary)
	}
	if fake.initCalls() != 1 {
		t.Fatalf("Init called %d times, want exactly 1", fake.initCalls())
	}
	if attempts.Load() != 2 {
		t.Fatalf("backup attempted %d times, want 2 (initial + retry)", attempts.Load())
	}
}

// TestBackupAutoInitFailureKeepsFailure verifies that when auto-initialization
// itself fails, the run stays failed rather than looping or masking the error.
func TestBackupAutoInitFailureKeepsFailure(t *testing.T) {
	var attempts atomic.Int32
	fake := &fakeRunner{installed: true, initErr: errors.New("permission denied"), streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		attempts.Add(1)
		return resticExitNotInitialized, nil
	}}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	run, err := app.coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.runs, run.ID, StatusFailed, 2*time.Second)
	if final.Error == "" {
		t.Fatal("failed run should carry an error message")
	}
	if fake.initCalls() != 1 {
		t.Fatalf("Init called %d times, want 1", fake.initCalls())
	}
	// Backup is attempted once; no retry after a failed init.
	if attempts.Load() != 1 {
		t.Fatalf("backup attempted %d times, want 1", attempts.Load())
	}
}
