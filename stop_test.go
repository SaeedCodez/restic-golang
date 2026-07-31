package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStopCancelsRun(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	run, err := app.coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	<-started
	waitForStatus(t, app.runs, run.ID, StatusRunning, 2*time.Second)

	if err := app.coord.Stop(run.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	final := waitForStatus(t, app.runs, run.ID, StatusCanceled, 2*time.Second)
	if final.FinishedAt == nil {
		t.Fatal("canceled run has no finishedAt")
	}

	lines, _ := app.runs.ReadLog(run.ID, 0)
	var sawStop bool
	for _, ln := range lines {
		if strings.Contains(ln.Message, "Stop requested") {
			sawStop = true
		}
	}
	if !sawStop {
		t.Fatal("expected a 'Stop requested' log line")
	}

	// Stopping a finished run reports it is no longer active.
	if err := app.coord.Stop(run.ID); !errors.Is(err, errRunNotActive) {
		t.Fatalf("stop finished run: want errRunNotActive, got %v", err)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	run, _ := app.coord.StartBackup(jobID)
	<-started
	waitForStatus(t, app.runs, run.ID, StatusRunning, 2*time.Second)

	// Two rapid stops must not double-log or error.
	_ = app.coord.Stop(run.ID)
	_ = app.coord.Stop(run.ID)

	waitForStatus(t, app.runs, run.ID, StatusCanceled, 2*time.Second)
	lines, _ := app.runs.ReadLog(run.ID, 0)
	stops := 0
	for _, ln := range lines {
		if strings.Contains(ln.Message, "Stop requested") {
			stops++
		}
	}
	if stops != 1 {
		t.Fatalf("'Stop requested' logged %d times, want 1", stops)
	}
}

func TestStopHTTPEndpoint(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app, err := newAppWithRunner(t.TempDir(), fake)
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	h := newServer(app).routes(http.NotFoundHandler())

	_, jobID := makeJob(t, app, "a", "/data")
	run, _ := app.coord.StartBackup(jobID)
	<-started
	waitForStatus(t, app.runs, run.ID, StatusRunning, 2*time.Second)

	if code := doJSON(t, h, "POST", "/api/runs/"+run.ID+"/stop", nil, nil); code != http.StatusOK {
		t.Fatalf("stop endpoint: code=%d, want 200", code)
	}
	waitForStatus(t, app.runs, run.ID, StatusCanceled, 2*time.Second)

	// Stopping the finished run now yields 409.
	if code := doJSON(t, h, "POST", "/api/runs/"+run.ID+"/stop", nil, nil); code != http.StatusConflict {
		t.Fatalf("stop finished run: code=%d, want 409", code)
	}
	// Stopping an unknown run yields 404.
	if code := doJSON(t, h, "POST", "/api/runs/nope/stop", nil, nil); code != http.StatusNotFound {
		t.Fatalf("stop unknown run: code=%d, want 404", code)
	}
}
