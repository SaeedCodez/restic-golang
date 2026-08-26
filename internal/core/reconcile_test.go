package core

import (
	"testing"
)

func seedRunningRun(t *testing.T, repoID string, pid int) string {
	t.Helper()
	store := newRunStore(testPool(t), nil)
	run := &Run{Kind: KindBackup, Status: StatusRunning, RepositoryID: repoID, RepoName: "R", PID: pid}
	h, err := store.Begin(run)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	h.Log("info", "system", "was running when the app died")
	return run.ID
}

func TestReconcileMarksRunningAsInterrupted(t *testing.T) {
	runID := seedRunningRun(t, "repo1", 0)

	store := newRunStore(testPool(t), nil)
	if len(store.ActiveRuns()) != 1 {
		t.Fatalf("precondition: want 1 active run, got %d", len(store.ActiveRuns()))
	}

	repos := store.reconcile(func(pid int, startToken string) bool { return false })

	if len(store.ActiveRuns()) != 0 {
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
	runID := seedRunningRun(t, "repo1", 1234)

	store := newRunStore(testPool(t), nil)
	reaped := 0
	store.reconcile(func(pid int, startToken string) bool {
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
	runID := seedRunningRun(t, "repo1", 0)

	app := testApp(t, &fakeRunner{installed: false})
	if len(app.Runs.ActiveRuns()) != 1 {
		t.Fatalf("precondition: want 1 active, got %d", len(app.Runs.ActiveRuns()))
	}
	app.Reconcile()
	if len(app.Runs.ActiveRuns()) != 0 {
		t.Fatal("App.Reconcile left a run marked active")
	}
	got, _ := app.Runs.Get(runID)
	if got.Status != StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", got.Status)
	}
}

func TestReconcileNoActiveRunsIsNoop(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	app.Reconcile()
}
