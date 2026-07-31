package main

import (
	"context"
	"testing"
	"time"
)

// TestMapResticMessageBackup feeds captured restic --json backup lines through
// the parser and asserts the sink receives correct Progress and Summary.
func TestMapResticMessageBackup(t *testing.T) {
	sink := &captureSink{}

	status := &resticMessage{
		MessageType: "status", PercentDone: 0.5, TotalFiles: 10, FilesDone: 5,
		TotalBytes: 2000, BytesDone: 1000, CurrentFiles: []string{"/a/b.txt"},
	}
	mapResticMessage(KindBackup, status, sink)

	p, ok := sink.lastProgress()
	if !ok {
		t.Fatal("no progress emitted")
	}
	if p.Percent != 0.5 || p.FilesDone != 5 || p.TotalFiles != 10 || p.BytesDone != 1000 || p.CurrentFile != "/a/b.txt" {
		t.Fatalf("progress wrong: %+v", p)
	}

	summary := &resticMessage{
		MessageType: "summary", SnapshotID: "abc123", FilesNew: 7, FilesChanged: 2,
		FilesUnmodified: 1, DataAdded: 555, TotalFilesProcessed: 10, TotalBytesProcessed: 2000,
		TotalDuration: 3.5,
	}
	mapResticMessage(KindBackup, summary, sink)
	if sink.summary == nil {
		t.Fatal("no summary emitted")
	}
	if sink.summary.SnapshotID != "abc123" || sink.summary.FilesNew != 7 || sink.summary.DataAdded != 555 {
		t.Fatalf("summary wrong: %+v", sink.summary)
	}
	if sink.summary.TotalDuration != 3.5 {
		t.Fatalf("duration wrong: %v", sink.summary.TotalDuration)
	}
}

// TestMapResticMessageRestore checks the restore field hedging (files_restored/
// bytes_restored vs files_done/bytes_done).
func TestMapResticMessageRestore(t *testing.T) {
	sink := &captureSink{}
	status := &resticMessage{
		MessageType: "status", PercentDone: 0.25, TotalFiles: 8, FilesRestored: 2,
		TotalBytes: 800, BytesRestored: 200,
	}
	mapResticMessage(KindRestore, status, sink)
	p, _ := sink.lastProgress()
	if p.FilesDone != 2 || p.BytesDone != 200 {
		t.Fatalf("restore progress should use restored fields: %+v", p)
	}

	summary := &resticMessage{
		MessageType: "summary", FilesRestored: 8, BytesRestored: 800, TotalFiles: 8, TotalBytes: 800,
		TotalDuration: 1.0,
	}
	mapResticMessage(KindRestore, summary, sink)
	if sink.summary == nil || sink.summary.FilesRestored != 8 || sink.summary.BytesRestored != 800 {
		t.Fatalf("restore summary wrong: %+v", sink.summary)
	}
}

// TestMapResticMessageError formats per-item errors into an error log line.
func TestMapResticMessageError(t *testing.T) {
	sink := &captureSink{}
	m := &resticMessage{MessageType: "error", During: "archival", Item: "/x/y"}
	m.Error.Message = "permission denied"
	mapResticMessage(KindBackup, m, sink)
	if len(sink.logs) != 1 {
		t.Fatalf("want 1 log line, got %d", len(sink.logs))
	}
	got := sink.logs[0]
	if got.Level != "error" {
		t.Fatalf("level = %q", got.Level)
	}
	if got.Message != "during archival: permission denied (/x/y)" {
		t.Fatalf("message = %q", got.Message)
	}
}

// TestResticRunnerBackupNoBinary verifies the real runner returns a start error
// when restic is not installed (as in this environment), rather than panicking.
func TestResticRunnerBackupNoBinary(t *testing.T) {
	if resticInstalled() {
		t.Skip("restic is installed; this test asserts the missing-binary path")
	}
	r := newResticRunner()
	sink := &captureSink{}
	repo := &Repository{BackendType: "Local", LocalPath: t.TempDir(), Password: "pw"}
	code, err := r.Backup(context.Background(), repo, t.TempDir(), []string{"resticweb-job:x"}, sink)
	if err == nil {
		t.Fatal("expected an error starting restic when it is not installed")
	}
	if code != -1 {
		t.Fatalf("exit code = %d, want -1", code)
	}
}

// TestFakeRunnerHonorsCancel confirms the fake's blocking stream returns when the
// context is canceled — the primitive later steps rely on for stop/reconcile.
func TestFakeRunnerHonorsCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	f := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	sink := &captureSink{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		code, _ := f.Backup(ctx, &Repository{}, "/src", nil, sink)
		done <- code
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream never started")
	}
	if sink.pid != 4242 {
		t.Fatalf("pid = %d, want 4242", sink.pid)
	}

	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("exit code = %d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop after cancel")
	}
}

// TestExitCode covers the exit-code extraction used for run classification.
func TestExitCode(t *testing.T) {
	if got := exitCode(nil); got != 0 {
		t.Fatalf("nil error -> %d, want 0", got)
	}
	if got := exitCode(context.Canceled); got != -1 {
		t.Fatalf("non-exit error -> %d, want -1", got)
	}
}
