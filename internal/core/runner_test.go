package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	if ResticInstalled() {
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

func TestLastResticNote(t *testing.T) {
	cases := map[string]string{
		"Stat(<config/>) returned error, retrying after 1s: connection reset\n" +
			"signal interrupt received, cleaning up \n" +
			"Fatal: unable to open config file: context canceled\ns3:https://example/bucket\n": "Stat(<config/>) returned error, retrying after 1s: connection reset",
		"signal interrupt received, cleaning up \n" +
			`{"message_type":"exit_error","code":130,"message":"Fatal: unable to open config file: context canceled"}`: "",
		"": "",
	}
	for in, want := range cases {
		if got := lastResticNote([]byte(in)); got != want {
			t.Fatalf("lastResticNote(%q) = %q, want %q", in, got, want)
		}
	}
}

// forgetSnap is one snapshot as restic forget --json prints it, full summary
// included (that is what makes the output so large).
func forgetSnap(id string) string {
	return `{"time":"2026-10-12T19:17:31Z","tree":"13e4e768","paths":["/data"],"hostname":"h","username":"root",` +
		`"tags":["resticweb-job:abc"],"program_version":"restic 0.19.1","summary":{"backup_start":"2026-10-12T19:17:31Z",` +
		`"files_new":5917,"total_files_processed":5917,"total_bytes_processed":151369221},"id":"` + id + `","short_id":"` + id[:8] + `"}`
}

func forgetOutput(keep, remove int) string {
	snaps := func(prefix string, n int) string {
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, forgetSnap(fmt.Sprintf("%s%07d%056d", prefix, i, 0)))
		}
		if len(parts) == 0 {
			return "null"
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return `[{"tags":null,"host":"h","paths":["/data"],"keep":` + snaps("k", keep) + `,"remove":` + snaps("r", remove) + `,"reasons":[]}]`
}

func TestForgetSummary(t *testing.T) {
	notes, ok := forgetSummary([]byte(forgetOutput(48, 12)))
	if !ok || len(notes) != 1 {
		t.Fatalf("forgetSummary: ok=%v notes=%v", ok, notes)
	}
	want := "Keeping 48 snapshot(s), removing 12: r0000000, r0000001, r0000002, r0000003, r0000004, r0000005, r0000006, r0000007, r0000008, r0000009 and 2 more."
	if notes[0] != want {
		t.Fatalf("summary = %q\nwant      %q", notes[0], want)
	}

	if notes, _ := forgetSummary([]byte(forgetOutput(3, 0))); len(notes) != 1 || notes[0] != "Keeping 3 snapshot(s), removing none." {
		t.Fatalf("nothing removed: %v", notes)
	}
	if notes, ok := forgetSummary([]byte("[]")); !ok || len(notes) != 1 || !strings.Contains(notes[0], "nothing to forget") {
		t.Fatalf("empty output: ok=%v notes=%v", ok, notes)
	}

	two := `[{"host":"a","paths":["/x"],"keep":[{"id":"aaaaaaaaaaaa"}],"remove":null},` +
		`{"host":"b","paths":["/y","/z"],"keep":[],"remove":[{"id":"bbbbbbbbbbbb"}]}]`
	notes, _ = forgetSummary([]byte(two))
	if len(notes) != 2 || notes[0] != "a (/x): Keeping 1 snapshot(s), removing none." ||
		notes[1] != "b (/y, /z): Keeping 0 snapshot(s), removing 1: bbbbbbbb." {
		t.Fatalf("two groups: %v", notes)
	}

	for _, other := range []string{"", "successfully removed 0 locks", `{"message_type":"summary"}`, "[not json"} {
		if _, ok := forgetSummary([]byte(other)); ok {
			t.Fatalf("forgetSummary(%q) claimed to recognize it", other)
		}
	}
}

func TestClipLogLine(t *testing.T) {
	if got := clipLogLine("short"); got != "short" {
		t.Fatalf("short line changed: %q", got)
	}
	long := strings.Repeat("é", maxLogLine) // 2 bytes each: cut lands mid-rune
	got := clipLogLine(long)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, fmt.Sprintf("(truncated, %d bytes in all)", len(long))) || len(got) > maxLogLine+64 {
		t.Fatalf("clipped line: valid=%v len=%d suffix=%q", utf8.ValidString(got), len(got), got[len(got)-40:])
	}
}
