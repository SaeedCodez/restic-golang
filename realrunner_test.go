//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeResticScript is a minimal restic stand-in that emits real --json output,
// so the actual resticRunner (subprocess spawn, stdout/stderr scanning, message
// parsing, exit codes) is exercised end to end without a real restic binary.
const fakeResticScript = `#!/usr/bin/env bash
if [ "$1" = "version" ]; then echo "restic 0.17.0 (fake)"; exit 0; fi
repo="$2"; cmd="$3"; shift 3
case "$cmd" in
  "cat")
    if [ -f "$repo/config" ]; then echo '{"version":2}'; exit 0; else echo "unable to open config file" 1>&2; exit 10; fi ;;
  "init")
    mkdir -p "$repo"; echo '{"version":2}' > "$repo/config"; echo "created restic repository"; exit 0 ;;
  "unlock") echo "successfully removed 0 locks"; exit 0 ;;
  "snapshots")
    if [ -f "$repo/snaps.json" ]; then cat "$repo/snaps.json"; else echo "[]"; fi; exit 0 ;;
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
    tgt=""; prev=""
    for a in "$@"; do if [ "$prev" = "--target" ]; then tgt="$a"; fi; prev="$a"; done
    mkdir -p "$tgt"; echo "hello" > "$tgt/a.txt"
    echo '{"message_type":"status","percent_done":1,"total_files":4,"files_restored":4,"total_bytes":2048,"bytes_restored":2048}'
    echo '{"message_type":"summary","total_files":4,"files_restored":4,"total_bytes":2048,"bytes_restored":2048,"total_duration":0.1}'
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
	if !resticInstalled() {
		t.Skip("fake restic not runnable in this environment")
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

	// Snapshots list.
	snaps, err := r.Snapshots(ctx, repo, "")
	if err != nil || len(snaps) != 1 {
		t.Fatalf("Snapshots: %v (n=%d)", err, len(snaps))
	}

	// Restore writes files into the target.
	tgt := filepath.Join(t.TempDir(), "restore")
	sink2 := &captureSink{}
	code, err = r.Restore(ctx, repo, snaps[0].ID, tgt, sink2)
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
	app, err := newAppWithRunner(t.TempDir(), newResticRunner())
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: filepath.Join(t.TempDir(), "repo"), Password: "pw"})
	// Initialize via a run.
	initRun, err := app.coord.StartInit(repo.ID)
	if err != nil {
		t.Fatalf("StartInit: %v", err)
	}
	waitForStatus(t, app.runs, initRun.ID, StatusSuccess, 5*time.Second)

	folder, _ := app.folders.Create(Folder{Meta: Meta{Name: "F"}, Path: t.TempDir()})
	job, _ := app.jobs.Create(Job{Meta: Meta{Name: "J"}, FolderID: folder.ID, RepositoryID: repo.ID})

	run, err := app.coord.StartBackup(job.ID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.runs, run.ID, StatusSuccess, 5*time.Second)
	if final.Summary == nil || final.Summary.SnapshotID == "" {
		t.Fatalf("no snapshot recorded: %+v", final.Summary)
	}
	lines, _ := app.runs.ReadLog(run.ID, 0)
	if len(lines) == 0 {
		t.Fatal("no durable log lines from a real backup")
	}
}

// TestRealRunnerBackupAutoInitializes runs a backup against a repository that was
// never initialized: the fake restic exits 10, the coordinator initializes the
// repo and retries, and the run succeeds — the full production auto-init path.
func TestRealRunnerBackupAutoInitializes(t *testing.T) {
	installFakeRestic(t)
	app, err := newAppWithRunner(t.TempDir(), newResticRunner())
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	// Deliberately NOT initialized.
	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: filepath.Join(t.TempDir(), "repo"), Password: "pw"})
	folder, _ := app.folders.Create(Folder{Meta: Meta{Name: "F"}, Path: t.TempDir()})
	job, _ := app.jobs.Create(Job{Meta: Meta{Name: "J"}, FolderID: folder.ID, RepositoryID: repo.ID})

	run, err := app.coord.StartBackup(job.ID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.runs, run.ID, StatusSuccess, 5*time.Second)
	if final.Summary == nil || final.Summary.SnapshotID == "" {
		t.Fatalf("auto-init backup produced no snapshot: %+v", final.Summary)
	}
	// The log should record the automatic initialization.
	lines, _ := app.runs.ReadLog(run.ID, 0)
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
