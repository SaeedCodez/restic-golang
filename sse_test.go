package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sseEvt is one parsed SSE event (its id and joined data payload).
type sseEvt struct {
	id   string
	data string
}

func (e sseEvt) obj() map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(e.data), &m)
	return m
}

func sawTerminalRun(events []sseEvt) bool {
	for _, e := range events {
		m := e.obj()
		if m["type"] != "run" {
			continue
		}
		run, _ := m["run"].(map[string]any)
		st, _ := run["status"].(string)
		if RunStatus(st).Terminal() {
			return true
		}
	}
	return false
}

func countType(events []sseEvt, typ string) int {
	n := 0
	for _, e := range events {
		if e.obj()["type"] == typ {
			n++
		}
	}
	return n
}

// collectSSE connects to an SSE endpoint and gathers events until want returns
// true or the timeout elapses.
func collectSSE(t *testing.T, url, lastEventID string, want func([]sseEvt) bool, timeout time.Duration) []sseEvt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()

	var events []sseEvt
	var data []string
	var id string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(data) > 0 {
				events = append(events, sseEvt{id: id, data: strings.Join(data, "\n")})
				data, id = nil, ""
				if want != nil && want(events) {
					return events
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case strings.HasPrefix(line, "id:"):
			id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
	}
	return events
}

func newSSETestServer(t *testing.T, fake *fakeRunner) (*App, *httptest.Server) {
	t.Helper()
	app, err := newAppWithRunner(t.TempDir(), fake)
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	ts := httptest.NewServer(routesFor(t, app))
	t.Cleanup(ts.Close)
	return app, ts
}

func TestRunEventsBacklogForFinishedRun(t *testing.T) {
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Log("info", "system", "line one")
		sink.Progress(Progress{Percent: 1, TotalBytes: 100, BytesDone: 100})
		sink.Summary(Summary{SnapshotID: "snap1"})
		return 0, nil
	}}
	app, ts := newSSETestServer(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")
	run, _ := app.coord.StartBackup(jobID)
	waitForStatus(t, app.runs, run.ID, StatusSuccess, 2*time.Second)

	// A late joiner (run already finished) gets the terminal record and the full
	// log as backlog — the "come back an hour later" case.
	events := collectSSE(t, ts.URL+"/api/runs/"+run.ID+"/events", "",
		func(evs []sseEvt) bool { return sawTerminalRun(evs) && countType(evs, "log") >= 1 },
		3*time.Second)
	if !sawTerminalRun(events) {
		t.Fatal("no terminal run event replayed for finished run")
	}
	if countType(events, "log") < 1 {
		t.Fatal("no log backlog replayed for finished run")
	}

	// Resuming after the last seq replays no log lines (only the run snapshot).
	lines, _ := app.runs.ReadLog(run.ID, 0)
	last := lines[len(lines)-1].Seq
	resumed := collectSSE(t, ts.URL+"/api/runs/"+run.ID+"/events",
		strconv.FormatInt(last, 10), nil, 400*time.Millisecond)
	if countType(resumed, "log") != 0 {
		t.Fatalf("resume after last seq replayed %d log lines, want 0", countType(resumed, "log"))
	}
}

func TestRunEventsLiveCompletion(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app, ts := newSSETestServer(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	run, _ := app.coord.StartBackup(jobID)
	<-started
	waitForStatus(t, app.runs, run.ID, StatusRunning, 2*time.Second)

	// Connect while running, then let it finish: the terminal run event must be
	// delivered live (it is not in the backlog, since status was running at
	// connect time).
	go func() {
		time.Sleep(150 * time.Millisecond)
		close(release)
	}()
	events := collectSSE(t, ts.URL+"/api/runs/"+run.ID+"/events", "", sawTerminalRun, 3*time.Second)
	if !sawTerminalRun(events) {
		t.Fatal("terminal run event was not delivered live")
	}
}

func TestGlobalEventsStream(t *testing.T) {
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(nil, release)}
	app, ts := newSSETestServer(t, fake)
	_, jobID := makeJob(t, app, "a", "/data")

	// Start the job shortly after the global stream connects, so its lifecycle
	// event lands on the stream.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, _ = app.coord.StartBackup(jobID)
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	events := collectSSE(t, ts.URL+"/api/events", "",
		func(evs []sseEvt) bool { return countType(evs, "run") >= 1 }, 3*time.Second)
	if countType(events, "run") < 1 {
		t.Fatal("no run lifecycle event on the global stream")
	}
}
