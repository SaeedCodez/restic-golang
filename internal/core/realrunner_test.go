//go:build unix

package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeResticScript is a minimal restic stand-in that emits real --json output,
// so the actual resticRunner (subprocess spawn, stdout/stderr scanning, message
// parsing, exit codes) is exercised end to end without a real restic binary.
const fakeResticScript = `#!/usr/bin/env bash
if [ "$1" = "version" ]; then echo "restic 0.17.0 (fake)"; exit 0; fi
repo="$2"; cmd="$3"; shift 3
# "hang": a backend that is reachable but never answers. Wait until interrupted,
# then react the way restic does mid-open.
if [ -f "$repo/hang" ]; then
  trap 'kill $sp; echo "signal interrupt received, cleaning up" 1>&2; echo "{\"message_type\":\"exit_error\",\"code\":130,\"message\":\"Fatal: unable to open config file: context canceled\"}" 1>&2; exit 130' INT
  : > "$repo/hang.ready"
  echo "Stat(<config/>) returned error, retrying after 1.4s: connection reset by peer" 1>&2
  sleep 30 >/dev/null 2>&1 & sp=$!
  wait $sp; exit 1
fi
# "quiet": print nothing for a while first, like restic opening a slow repository.
if [ -f "$repo/quiet" ]; then sleep "$(cat "$repo/quiet")"; fi
case "$cmd" in
  "cat")
    if [ -f "$repo/config" ]; then echo '{"version":2}'; exit 0; else echo "unable to open config file" 1>&2; exit 10; fi ;;
  "init")
    mkdir -p "$repo"; echo '{"version":2}' > "$repo/config"; echo "created restic repository"; exit 0 ;;
  "unlock") echo "successfully removed 0 locks"; exit 0 ;;
  "snapshots")
    if [ -f "$repo/retried" ]; then
      echo "Load(<snapshot/deadbeef>, 0, 0) returned error, retrying after 720ms: unexpected EOF" 1>&2
      echo "Load(<snapshot/deadbeef>, 0, 0) operation successful after 1 retries" 1>&2
    fi
    if [ -f "$repo/snaps.json" ]; then cat "$repo/snaps.json"; else echo "[]"; fi; exit 0 ;;
  "stats")
    echo '{"total_size":2048,"total_file_count":4,"snapshots_count":1}'; exit 0 ;;
  "backup")
    src="$1"
    if [ ! -f "$repo/config" ]; then
      echo '{"message_type":"exit_error","code":10,"message":"Fatal: repository does not exist: unable to open config file"}' 1>&2
      exit 10
    fi
    echo "Warn: could not read some file" 1>&2
    echo '{"message_type":"status","percent_done":0.5,"total_files":4,"files_done":2,"total_bytes":2048,"bytes_done":1024,"current_files":["'"$src"'/a"]}'
    echo '{"message_type":"summary","files_new":4,"files_changed":0,"files_unmodified":0,"data_added":2048,"total_files_processed":4,"total_bytes_processed":2048,"total_duration":0.1,"snapshot_id":"deadbeefcafe0001"}'
    printf '[{"id":"deadbeefcafe0001","short_id":"deadbeef","time":"2026-01-01T00:00:00Z","paths":["%s"],"hostname":"fake","tags":["t"],"summary":{"total_bytes_processed":2048,"total_files_processed":4}}]\n' "$src" > "$repo/snaps.json"
    exit 0 ;;
  "restore")
    tgt=""; prev=""; has_delete=0
    for a in "$@"; do
      if [ "$prev" = "--target" ]; then tgt="$a"; fi
      if [ "$a" = "--delete" ]; then has_delete=1; fi
      prev="$a"
    done
    if [ "$has_delete" != 1 ]; then echo "missing --delete" 1>&2; exit 1; fi
    mkdir -p "$tgt"; echo "hello" > "$tgt/a.txt"
    echo '{"message_type":"status","percent_done":1,"total_files":4,"files_restored":4,"total_bytes":2048,"bytes_restored":2048}'
    echo '{"message_type":"summary","total_files":4,"files_restored":4,"total_bytes":2048,"bytes_restored":2048,"total_duration":0.1}'
    exit 0 ;;
  "forget")
    # Policy forget prints every group's kept and removed snapshots as one JSON
    # array line; pad it the way full snapshot summaries bloat the real thing.
    pad=$(printf 'x%.0s' $(seq 1 6000))
    echo '[{"tags":null,"host":"fake","paths":["/data"],"keep":[{"id":"keep00000001","pad":"'"$pad"'"}],"remove":[{"id":"gone00000001"},{"id":"gone00000002"}],"reasons":[]}]'
    exit 0 ;;
  *) echo "unknown $cmd" 1>&2; exit 1 ;;
esac
`

func installFakeRestic(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "restic")
	if err := os.WriteFile(path, []byte(fakeResticScript), 0o755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if !ResticInstalled() {
		t.Skip("fake restic not runnable in this environment")
	}
	// The first exec of a new script can be slow (macOS checks new executables);
	// pay that here so tests with tight timings are not thrown off.
	if _, err := resticVersion(context.Background()); err != nil {
		t.Skipf("fake restic not runnable in this environment: %v", err)
	}
}

// TestRealRunnerAgainstFakeRestic drives the real resticRunner through a full
// init → backup → list → restore cycle against the fake binary, asserting the
// subprocess streaming and parsing produce correct sink events and results.
func TestRealRunnerAgainstFakeRestic(t *testing.T) {
	installFakeRestic(t)
	r := newResticRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoDir := filepath.Join(t.TempDir(), "repo")
	repo := &Repository{BackendType: "Local", LocalPath: repoDir, Password: "pw"}

	// Before init, Test reports not-initialized.
	if res := r.Test(ctx, repo); res.OK || res.Initialized {
		t.Fatalf("test before init: %+v", res)
	}
	if _, err := r.Init(ctx, repo); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if res := r.Test(ctx, repo); !res.OK {
		t.Fatalf("test after init: %+v", res)
	}

	// Backup: assert the subprocess stream produced progress, a warning (stderr)
	// log line, a summary with the snapshot id, and the child pid.
	src := t.TempDir()
	sink := &captureSink{}
	code, err := r.Backup(ctx, repo, src, []string{"resticweb-job:abc"}, sink)
	if err != nil || code != 0 {
		t.Fatalf("Backup: code=%d err=%v", code, err)
	}
	if _, ok := sink.lastProgress(); !ok {
		t.Fatal("no progress parsed from backup stream")
	}
	if sink.summary == nil || sink.summary.SnapshotID != "deadbeefcafe0001" {
		t.Fatalf("backup summary wrong: %+v", sink.summary)
	}
	if sink.summary.DataAdded != 2048 {
		t.Fatalf("data_added parsed wrong: %d", sink.summary.DataAdded)
	}
	if sink.pid <= 0 {
		t.Fatalf("child pid not reported: %d", sink.pid)
	}
	var sawWarn bool
	for _, l := range sink.logs {
		if l.Level == "warn" {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatal("stderr warning not captured as a warn log line")
	}

	// Snapshots list (summary present → size/files filled without stats).
	snaps, err := r.Snapshots(ctx, repo, "")
	if err != nil || len(snaps) != 1 {
		t.Fatalf("Snapshots: %v (n=%d)", err, len(snaps))
	}
	if snaps[0].SizeBytes == nil || *snaps[0].SizeBytes != 2048 {
		t.Fatalf("snapshot size: %+v", snaps[0].SizeBytes)
	}
	if snaps[0].FileCount == nil || *snaps[0].FileCount != 4 {
		t.Fatalf("snapshot files: %+v", snaps[0].FileCount)
	}

	// Snapshots without a stored summary are backfilled via `restic stats`.
	noSummary := `[{"id":"deadbeefcafe0001","short_id":"deadbeef","time":"2026-01-01T00:00:00Z","paths":["/x"],"hostname":"fake","tags":["t"]}]`
	if err := os.WriteFile(filepath.Join(repoDir, "snaps.json"), []byte(noSummary), 0o644); err != nil {
		t.Fatal(err)
	}
	snaps, err = r.Snapshots(ctx, repo, "")
	if err != nil || len(snaps) != 1 {
		t.Fatalf("Snapshots backfill: %v (n=%d)", err, len(snaps))
	}
	if snaps[0].SizeBytes == nil || *snaps[0].SizeBytes != 2048 {
		t.Fatalf("backfilled size: %+v", snaps[0].SizeBytes)
	}
	if snaps[0].FileCount == nil || *snaps[0].FileCount != 4 {
		t.Fatalf("backfilled files: %+v", snaps[0].FileCount)
	}

	// Restore writes files into the target.
	tgt := filepath.Join(t.TempDir(), "restore")
	sink2 := &captureSink{}
	code, err = r.Restore(ctx, repo, snaps[0].ID, tgt, nil, sink2)
	if err != nil || code != 0 {
		t.Fatalf("Restore: code=%d err=%v", code, err)
	}
	if sink2.summary == nil || sink2.summary.FilesRestored != 4 {
		t.Fatalf("restore summary wrong: %+v", sink2.summary)
	}
	if _, err := os.Stat(filepath.Join(tgt, "a.txt")); err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
}

// TestRealRunnerBackupThroughCoordinator drives a real subprocess backup all the
// way through the coordinator and durable run store — the full production path
// minus the HTTP layer.
func TestRealRunnerBackupThroughCoordinator(t *testing.T) {
	installFakeRestic(t)
	app := testApp(t, newResticRunner())
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: filepath.Join(t.TempDir(), "repo"), Password: "pw"})
	// Initialize via a run.
	initRun, err := app.Coord.StartInit(repo.ID)
	if err != nil {
		t.Fatalf("StartInit: %v", err)
	}
	waitForStatus(t, app.Runs, initRun.ID, StatusSuccess, 5*time.Second)

	folder, _ := app.Folders.Create(Folder{Meta: Meta{Name: "F"}, Path: t.TempDir()})
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "J"}, FolderID: folder.ID, RepositoryID: repo.ID})

	run, err := app.Coord.StartBackup(job.ID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 5*time.Second)
	if final.Summary == nil || final.Summary.SnapshotID == "" {
		t.Fatalf("no snapshot recorded: %+v", final.Summary)
	}
	lines, _ := app.Runs.ReadLog(run.ID, 0)
	if len(lines) == 0 {
		t.Fatal("no durable log lines from a real backup")
	}
}

// TestRealRunnerBackupAutoInitializes runs a backup against a repository that was
// never initialized: the fake restic exits 10, the coordinator initializes the
// repo and retries, and the run succeeds — the full production auto-init path.
func TestRealRunnerBackupAutoInitializes(t *testing.T) {
	installFakeRestic(t)
	app := testApp(t, newResticRunner())
	// Deliberately NOT initialized.
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: filepath.Join(t.TempDir(), "repo"), Password: "pw"})
	folder, _ := app.Folders.Create(Folder{Meta: Meta{Name: "F"}, Path: t.TempDir()})
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "J"}, FolderID: folder.ID, RepositoryID: repo.ID})

	run, err := app.Coord.StartBackup(job.ID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 5*time.Second)
	if final.Summary == nil || final.Summary.SnapshotID == "" {
		t.Fatalf("auto-init backup produced no snapshot: %+v", final.Summary)
	}
	// The log should record the automatic initialization.
	lines, _ := app.Runs.ReadLog(run.ID, 0)
	var sawInit bool
	for _, l := range lines {
		if strings.Contains(l.Message, "initializing it now") || strings.Contains(l.Message, "Repository initialized") {
			sawInit = true
		}
	}
	if !sawInit {
		t.Fatal("auto-init was not recorded in the run log")
	}
}

// A backend retry warning on stderr must not corrupt the --json list on stdout.
func TestRealRunnerSnapshotsIgnoresStderrWarnings(t *testing.T) {
	installFakeRestic(t)
	repoDir := t.TempDir()
	repo := &Repository{BackendType: "Local", LocalPath: repoDir, Password: "pw"}
	list := `[{"id":"deadbeefcafe0001","short_id":"deadbeef","time":"2026-01-01T00:00:00Z","paths":["/x"],"hostname":"fake","summary":{"total_bytes_processed":1,"total_files_processed":1}}]`
	if err := os.WriteFile(filepath.Join(repoDir, "snaps.json"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "retried"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	snaps, err := newResticRunner().Snapshots(context.Background(), repo, "")
	if err != nil || len(snaps) != 1 || snaps[0].ID != "deadbeefcafe0001" {
		t.Fatalf("Snapshots with a retried load: snaps=%+v err=%v", snaps, err)
	}
}

// When a direct restic call outlives its context, the error must say it timed
// out (and unwrap to DeadlineExceeded) instead of restic's "unable to open config
// file: context canceled", which would be classified as "not initialized".
func TestRealRunnerTimeoutIsNotMisclassified(t *testing.T) {
	installFakeRestic(t)
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "hang"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{BackendType: "Local", LocalPath: repoDir, Password: "pw"}
	r := newResticRunner()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := r.Snapshots(ctx, repo, "")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errNotInitialized) {
		t.Fatalf("Snapshots error = %v, want a timeout", err)
	}
	for _, want := range []string{"restic snapshots timed out", "connection reset by peer"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Snapshots error %q does not mention %q", err, want)
		}
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := r.Unlock(ctx2, repo); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Unlock error = %v, want a timeout", err)
	}

	ctx3, cancel3 := context.WithTimeout(context.Background(), time.Second)
	defer cancel3()
	res := r.Test(ctx3, repo)
	if res.OK || !strings.Contains(res.Message, "timed out") {
		t.Fatalf("Test result = %+v, want a timeout message", res)
	}
	if strings.Contains(res.Detail, "context canceled") || !strings.Contains(res.Detail, "connection reset by peer") {
		t.Fatalf("Test detail = %q, want restic's output from before the interrupt only", res.Detail)
	}
}

// A streamed run that prints nothing for a while gets a note in its log, so a
// slow backend is distinguishable from a dead process.
func TestRealRunnerNotesQuietRestic(t *testing.T) {
	installFakeRestic(t)
	defer func(after, poll time.Duration) { quietNoticeAfter, quietPoll = after, poll }(quietNoticeAfter, quietPoll)
	quietNoticeAfter, quietPoll = 150*time.Millisecond, 10*time.Millisecond

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "quiet"), []byte("0.5"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{BackendType: "Local", LocalPath: repoDir, Password: "pw"}
	sink := &captureSink{}
	code, err := newResticRunner().Restore(context.Background(), repo, "deadbeef", filepath.Join(t.TempDir(), "out"), nil, sink)
	if err != nil || code != 0 {
		t.Fatalf("Restore: code=%d err=%v", code, err)
	}
	var notes int
	for _, l := range sink.logs {
		if strings.HasPrefix(l.Message, "No output from restic for") {
			if l.Level != "info" || l.Stream != "system" || !strings.Contains(l.Message, fmt.Sprintf("pid %d", sink.pid)) {
				t.Fatalf("unexpected quiet note: %+v", l)
			}
			notes++
		}
	}
	if notes == 0 {
		t.Fatalf("no quiet note logged: %+v", sink.logs)
	}
}

func TestQuietNotesDoubleAndReset(t *testing.T) {
	defer func(after, poll time.Duration) { quietNoticeAfter, quietPoll = after, poll }(quietNoticeAfter, quietPoll)
	quietNoticeAfter, quietPoll = 40*time.Millisecond, 2*time.Millisecond

	notes := make(chan time.Duration, 16)
	q := startQuietNotes(func(silent time.Duration) { notes <- silent })
	defer q.stop()

	for i, min := range []time.Duration{40 * time.Millisecond, 80 * time.Millisecond} {
		select {
		case s := <-notes:
			if s < min {
				t.Fatalf("note %d after %s, want at least %s", i, s, min)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("note %d never came", i)
		}
	}
	// New output starts the count over: the next note comes at the first
	// threshold again, not at 160ms.
	q.touch()
	for len(notes) > 0 {
		<-notes
	}
	select {
	case s := <-notes:
		if s >= 80*time.Millisecond {
			t.Fatalf("after output, note came at %s, want the 40ms threshold again", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no note after the silence restarted")
	}
}

// A restore's snapshot lookup records restic's pid on the run, and a SIGINT to
// it (what Stop does from another process, e.g. `restic-webctl run stop` while
// the web server owns the run) ends the run as canceled, not failed.
func TestRealRunnerRestoreLookupStoppableFromOutside(t *testing.T) {
	installFakeRestic(t)
	app := testApp(t, newResticRunner())
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "hang"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: repoDir, Password: "pw"})

	run, err := app.Coord.StartRestore(repo.ID, "deadbeef", filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	var pid int
	for deadline := time.Now().Add(5 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if r, ok := app.Runs.Get(run.ID); ok {
			pid = r.PID
		}
	}
	if pid == 0 {
		t.Fatal("the snapshot lookup did not record restic's pid on the run")
	}
	// Wait for the fake restic to install its SIGINT trap (a first exec of a new
	// script can take a while on macOS).
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(repoDir, "hang.ready")); err == nil {
			break
		}
	}
	if err := signalProcessGroup(pid, syscall.SIGINT); err != nil {
		t.Fatalf("signal restic: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusCanceled, 5*time.Second)
	if final.Error != "" {
		t.Fatalf("canceled run carries an error: %q", final.Error)
	}
	lines, _ := app.Runs.ReadLog(run.ID, 0)
	for _, l := range lines {
		if strings.HasPrefix(l.Message, "Restoring ") {
			t.Fatalf("restore ran after the lookup was stopped: %+v", lines)
		}
	}
}

// The API's synchronous restic calls stop at App.ResticTimeout and say so with
// code "timeout", rather than a raw restic error (or a misread "not initialized").
func TestAPIResticCallsTimeOut(t *testing.T) {
	installFakeRestic(t)
	app := testApp(t, newResticRunner())
	app.ResticTimeout = 500 * time.Millisecond
	h := routesFor(t, app)
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "hang"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: repoDir, Password: "pw"})
	folder, _ := app.Folders.Create(Folder{Meta: Meta{Name: "F"}, Path: t.TempDir()})
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "J"}, FolderID: folder.ID, RepositoryID: repo.ID})

	for _, c := range []struct{ method, path, field string }{
		{"POST", "/api/repositories/" + repo.ID + "/test", "message"},
		{"POST", "/api/repositories/" + repo.ID + "/unlock", "error"},
		{"GET", "/api/repositories/" + repo.ID + "/snapshots", "error"},
		{"GET", "/api/jobs/" + job.ID + "/snapshots", "error"},
	} {
		var body map[string]any
		if code := doJSON(t, h, c.method, c.path, nil, &body); code != http.StatusOK {
			t.Fatalf("%s %s: status %d", c.method, c.path, code)
		}
		msg, _ := body[c.field].(string)
		if body["ok"] != false || body["code"] != "timeout" || !strings.Contains(msg, "did not answer within 500ms") {
			t.Fatalf("%s %s: body %+v, want ok=false code=timeout", c.method, c.path, body)
		}
	}
}

// Retention logs a short summary of what forget kept and removed, not restic's
// whole JSON array as one enormous line.
func TestRealRunnerForgetLogsSummary(t *testing.T) {
	installFakeRestic(t)
	repo := &Repository{BackendType: "Local", LocalPath: t.TempDir(), Password: "pw"}
	sink := &captureSink{}
	code, err := newResticRunner().Forget(context.Background(), repo, "resticweb-job:abc", JobRetention{KeepLast: 1}, sink)
	if err != nil || code != 0 {
		t.Fatalf("Forget: code=%d err=%v", code, err)
	}
	var sawSummary bool
	for _, l := range sink.logs {
		if len(l.Message) > maxLogLine {
			t.Fatalf("log line of %d bytes: %.80s…", len(l.Message), l.Message)
		}
		if l.Message == "Keeping 1 snapshot(s), removing 2: gone0000, gone0000." {
			sawSummary = true
		}
	}
	if !sawSummary {
		t.Fatalf("no forget summary in the log: %+v", sink.logs)
	}
}
