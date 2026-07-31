package main

import (
	"os"
	"path/filepath"
	"testing"
)

// seedRunningRun writes a run left in the "running" state (as a crash would),
// with the given pid, and returns its id.
func seedRunningRun(t *testing.T, runsDir, repoID string, pid int) string {
	t.Helper()
	store, err := newRunStore(runsDir, nil)
	if err != nil {
		t.Fatalf("newRunStore: %v", err)
	}
	run := &Run{Kind: KindBackup, Status: StatusRunning, RepositoryID: repoID, RepoName: "R", PID: pid}
	h, err := store.Begin(run)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	h.Log("info", "system", "was running when the app died")
	// Deliberately do not finalize: simulate the process dying mid-run.
	return run.ID
}

func TestReconcileMarksRunningAsInterrupted(t *testing.T) {
	runsDir := filepath.Join(t.TempDir(), "runs")
	runID := seedRunningRun(t, runsDir, "repo1", 0)

	// Restart: a fresh store loads the run still marked running.
	store, err := newRunStore(runsDir, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(store.activeRuns()) != 1 {
		t.Fatalf("precondition: want 1 active run, got %d", len(store.activeRuns()))
	}

	repos := store.reconcile(func(pid int) bool { return false })

	if len(store.activeRuns()) != 0 {
		t.Fatal("a run is still reported active after reconcile — the app would be lying")
	}
	got, _ := store.Get(runID)
	if got.Status != StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("interrupted run has no finishedAt")
	}
	if got.Error == "" {
		t.Fatal("interrupted run has no explanatory error")
	}
	if len(repos) != 1 || repos[0] != "repo1" {
		t.Fatalf("affected repos = %v, want [repo1]", repos)
	}

	// The log continues monotonically and records the interruption.
	lines, _ := store.ReadLog(runID, 0)
	if len(lines) < 2 {
		t.Fatalf("want at least 2 log lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if ln.Seq != int64(i+1) {
			t.Fatalf("log seq not monotonic across reconcile: %+v", lines)
		}
	}
	if lines[len(lines)-1].Level != "error" {
		t.Fatalf("last log line level = %q, want error", lines[len(lines)-1].Level)
	}
}

func TestReconcileReapsOrphan(t *testing.T) {
	runsDir := filepath.Join(t.TempDir(), "runs")
	runID := seedRunningRun(t, runsDir, "repo1", 1234)

	store, _ := newRunStore(runsDir, nil)
	reaped := 0
	store.reconcile(func(pid int) bool {
		if pid == 1234 {
			reaped++
			return true
		}
		return false
	})
	if reaped != 1 {
		t.Fatalf("reap called %d times, want 1", reaped)
	}
	lines, _ := store.ReadLog(runID, 0)
	var sawWarn bool
	for _, ln := range lines {
		if ln.Level == "warn" {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatal("expected a warn log line noting the reaped orphan")
	}
}

func TestAppReconcileEndToEnd(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	runID := seedRunningRun(t, runsDir, "repo1", 0)

	// Build an app over the same data dir (restic absent, so no unlock spawns).
	app, err := newAppWithRunner(dir, &fakeRunner{installed: false})
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	if len(app.runs.activeRuns()) != 1 {
		t.Fatalf("precondition: want 1 active, got %d", len(app.runs.activeRuns()))
	}
	app.Reconcile()
	if len(app.runs.activeRuns()) != 0 {
		t.Fatal("App.Reconcile left a run marked active")
	}
	got, _ := app.runs.Get(runID)
	if got.Status != StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", got.Status)
	}
}

func TestReconcileNoActiveRunsIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	app, err := newAppWithRunner(dir, &fakeRunner{installed: true})
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	app.Reconcile() // must not panic or error with an empty store
}
