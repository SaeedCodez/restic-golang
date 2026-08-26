package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newRunTestApp(t *testing.T, fake *fakeRunner) *App {
	t.Helper()
	return testApp(t, fake)
}

// makeJob creates a repository, folder and job with unique names and returns the
// repository and job ids.
func makeJob(t *testing.T, app *App, key, folderPath string) (repoID, jobID string) {
	t.Helper()
	repo, err := app.Repos.Create(Repository{Meta: Meta{Name: "repo-" + key}, BackendType: "Local", LocalPath: "/tmp/" + key, Password: "pw"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	folder, err := app.Folders.Create(Folder{Meta: Meta{Name: "folder-" + key}, Path: folderPath})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	job, err := app.Jobs.Create(Job{Meta: Meta{Name: "job-" + key}, FolderID: folder.ID, RepositoryID: repo.ID})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	return repo.ID, job.ID
}

func addJob(t *testing.T, app *App, key, folderPath, repoID string) string {
	t.Helper()
	folder, err := app.Folders.Create(Folder{Meta: Meta{Name: "folder-" + key}, Path: folderPath})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	job, err := app.Jobs.Create(Job{Meta: Meta{Name: "job-" + key}, FolderID: folder.ID, RepositoryID: repoID})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	return job.ID
}

func waitForStatus(t *testing.T, store *RunStore, runID string, want RunStatus, timeout time.Duration) *Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if run, ok := store.Get(runID); ok && run.Status == want {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	run, _ := store.Get(runID)
	t.Fatalf("run %s did not reach %s in %s (last: %+v)", runID, want, timeout, run)
	return nil
}

func TestBackupRunSucceeds(t *testing.T) {
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Log("info", "system", "starting up")
		sink.Progress(Progress{Percent: 1, FilesDone: 2, TotalFiles: 2, BytesDone: 100, TotalBytes: 100})
		sink.Summary(Summary{SnapshotID: "snap1", FilesNew: 2, DataAdded: 100, TotalDuration: 0.2})
		return 0, nil
	}}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data/docs")

	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}

	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Summary == nil || final.Summary.SnapshotID != "snap1" {
		t.Fatalf("summary not persisted: %+v", final.Summary)
	}
	if final.FinishedAt == nil {
		t.Fatal("finishedAt not set")
	}
	if final.PID != 4242 {
		t.Fatalf("pid = %d, want 4242 (durably persisted)", final.PID)
	}
	if final.JobName == "" || final.FolderPath != "/data/docs" || final.RepoName == "" {
		t.Fatalf("denormalized fields missing: %+v", final)
	}

	lines, err := app.Runs.ReadLog(run.ID, 0)
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("no log lines persisted")
	}
	for i, ln := range lines {
		if ln.Seq != int64(i+1) {
			t.Fatalf("log seq not monotonic at %d: %+v", i, lines)
		}
	}

	// ReadLog after a cursor returns only later lines.
	tail, _ := app.Runs.ReadLog(run.ID, lines[0].Seq)
	if len(tail) != len(lines)-1 {
		t.Fatalf("after-cursor read: got %d, want %d", len(tail), len(lines)-1)
	}
}

func TestRunVisibleWhileRunning(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	<-started

	// Durable record shows it running, with its log-so-far — independent of any
	// live connection.
	running := waitForStatus(t, app.Runs, run.ID, StatusRunning, 2*time.Second)
	if running.Progress.TotalBytes != 100 {
		t.Fatalf("progress not persisted while running: %+v", running.Progress)
	}
	if len(app.Runs.ActiveRuns()) != 1 {
		t.Fatalf("activeRuns = %d, want 1", len(app.Runs.ActiveRuns()))
	}
	lines, _ := app.Runs.ReadLog(run.ID, 0)
	if len(lines) == 0 {
		t.Fatal("expected log-so-far while running")
	}

	close(release)
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if len(app.Runs.ActiveRuns()) != 0 {
		t.Fatal("run still active after completion")
	}
}

func TestPerRepositoryContention(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)

	repoA, jobA := makeJob(t, app, "A", "/a")
	jobB := addJob(t, app, "B", "/b", repoA) // same repository as A
	_, jobC := makeJob(t, app, "C", "/c")    // different repository

	runA, err := app.Coord.StartBackup(jobA)
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	<-started

	// A second run on the SAME repository is rejected with a named blocker.
	_, err = app.Coord.StartBackup(jobB)
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("same-repo start: want BusyError, got %v", err)
	}
	if busy.Blocking.RunID != runA.ID {
		t.Fatalf("blocker runId = %q, want %q", busy.Blocking.RunID, runA.ID)
	}

	// A run on a DIFFERENT repository proceeds in parallel.
	runC, err := app.Coord.StartBackup(jobC)
	if err != nil {
		t.Fatalf("different-repo start should succeed: %v", err)
	}

	close(release)
	waitForStatus(t, app.Runs, runA.ID, StatusSuccess, 2*time.Second)
	waitForStatus(t, app.Runs, runC.ID, StatusSuccess, 2*time.Second)
}

func TestFailedBackupClassified(t *testing.T) {
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Log("error", "stdout", "boom")
		return 1, nil
	}}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")
	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if final.ExitCode == nil || *final.ExitCode != 1 {
		t.Fatalf("exit code = %v, want 1", final.ExitCode)
	}
}

func TestRunStoreReloadFromDisk(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")
	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)

	// A fresh store over the same directory sees the persisted run and its job
	// history.
	reloaded := newRunStore(testPool(t), nil)
	got, ok := reloaded.Get(run.ID)
	if !ok {
		t.Fatal("run not present after reload")
	}
	if got.Status != StatusSuccess {
		t.Fatalf("reloaded status = %s, want success", got.Status)
	}
	if len(reloaded.runsForJob(jobID)) != 1 {
		t.Fatalf("job history after reload = %d, want 1", len(reloaded.runsForJob(jobID)))
	}
}
