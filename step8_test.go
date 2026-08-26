package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitRunSuccessAndFailure(t *testing.T) {
	// Success.
	app := newRunTestApp(t, &fakeRunner{installed: true, initOut: "created restic repository"})
	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	run, err := app.coord.StartInit(repo.ID)
	if err != nil {
		t.Fatalf("StartInit: %v", err)
	}
	final := waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Kind != KindInit {
		t.Fatalf("kind = %s, want init", final.Kind)
	}

	// Failure (already initialized).
	app2 := newRunTestApp(t, &fakeRunner{installed: true, initErr: errWrongPassword})
	repo2, _ := app2.repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	run2, _ := app2.coord.StartInit(repo2.ID)
	f2 := waitForStatus(t, app2.runs, run2.ID, StatusFailed, 2*time.Second)
	if f2.Error == "" {
		t.Fatal("failed init should carry an error message")
	}
}

func TestRestoreRunCreatesTargetAndSucceeds(t *testing.T) {
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Progress(Progress{Percent: 1, FilesDone: 3, TotalFiles: 3})
		sink.Summary(Summary{FilesRestored: 3, BytesRestored: 300})
		return 0, nil
	}}
	app := newRunTestApp(t, fake)
	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	target := filepath.Join(t.TempDir(), "restore-here")

	run, err := app.coord.StartRestore(repo.ID, "snap1", target)
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	final := waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Kind != KindRestore {
		t.Fatalf("kind = %s, want restore", final.Kind)
	}
	if final.Params["target"] != target {
		t.Fatalf("target param = %q", final.Params["target"])
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target folder not created: %v", err)
	}
}

func TestRestoreValidation(t *testing.T) {
	app := newRunTestApp(t, &fakeRunner{installed: true})
	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	if _, err := app.coord.StartRestore(repo.ID, "", "/tmp/x"); err == nil {
		t.Fatal("empty snapshot id should fail")
	}
	if _, err := app.coord.StartRestore(repo.ID, "snap", ""); err == nil {
		t.Fatal("empty target should fail")
	}
}

func TestDownloadRunAndZip(t *testing.T) {
	fake := &fakeRunner{
		installed: true,
		onRestore: func(target string) {
			_ = os.WriteFile(filepath.Join(target, "hello.txt"), []byte("hi there"), 0o644)
		},
		streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
			sink.Summary(Summary{FilesRestored: 1})
			return 0, nil
		},
	}
	app, err := newAppWithRunner(t.TempDir(), fake)
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	h := routesFor(t, app)
	ts := httptest.NewServer(h)
	defer ts.Close()

	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	run, err := app.coord.StartDownload(repo.ID, "snap1")
	if err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)

	resp, err := http.Get(ts.URL + "/api/runs/" + run.ID + "/download")
	if err != nil {
		t.Fatalf("download GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	var found string
	for _, f := range zr.File {
		if f.Name == "hello.txt" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			found = string(b)
		}
	}
	if found != "hi there" {
		t.Fatalf("zip did not contain the restored file; got %q", found)
	}
}

func TestDownloadNotReady(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app, _ := newAppWithRunner(t.TempDir(), fake)
	h := routesFor(t, app)

	repo, _ := app.repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	run, _ := app.coord.StartDownload(repo.ID, "snap1")
	<-started
	waitForStatus(t, app.runs, run.ID, StatusRunning, 2*time.Second)

	// While still running, the zip GET reports it is not ready.
	if code := doJSON(t, h, "GET", "/api/runs/"+run.ID+"/download", nil, nil); code != http.StatusConflict {
		t.Fatalf("download while running: code=%d, want 409", code)
	}
	close(release)
	waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)
}

func TestJobSnapshotsUseTag(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "s1", ShortID: "s1"}}}
	app, _ := newAppWithRunner(t.TempDir(), fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "a", "/data")
	job, _ := app.jobs.Get(jobID)

	var resp struct {
		OK        bool       `json:"ok"`
		Snapshots []Snapshot `json:"snapshots"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs/"+jobID+"/snapshots", nil, &resp); code != http.StatusOK {
		t.Fatalf("job snapshots: code=%d", code)
	}
	if len(resp.Snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(resp.Snapshots))
	}
	tags := fake.recordedTags()
	if len(tags) != 1 || tags[0] != job.ResticTag() {
		t.Fatalf("Snapshots called with tags %v, want [%s]", tags, job.ResticTag())
	}
}

func TestRetentionPrune(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	// Create several completed runs for the job.
	for i := 0; i < 5; i++ {
		run, err := app.coord.StartBackup(jobID)
		if err != nil {
			t.Fatalf("StartBackup %d: %v", i, err)
		}
		waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)
	}
	if got := len(app.runs.runsForJob(jobID)); got != 5 {
		t.Fatalf("precondition: %d runs, want 5", got)
	}

	app.runs.prune(2)
	if got := len(app.runs.runsForJob(jobID)); got != 2 {
		t.Fatalf("after prune(2): %d runs, want 2", got)
	}
	// The pruned run directories are gone from disk too.
	entries, _ := os.ReadDir(filepath.Join(app.dataDir, "runs"))
	if len(entries) != 2 {
		t.Fatalf("on-disk run dirs = %d, want 2", len(entries))
	}
}
