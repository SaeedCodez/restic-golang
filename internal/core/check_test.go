package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCheckArgs(t *testing.T) {
	cases := map[string]string{
		"":      "check",
		"all":   "check --read-data",
		" 10% ": "check --read-data-subset 10%",
		"1/5":   "check --read-data-subset 1/5",
		"500M":  "check --read-data-subset 500M",
	}
	for in, want := range cases {
		args, err := checkArgs(in)
		if err != nil {
			t.Fatalf("checkArgs(%q): %v", in, err)
		}
		if got := strings.Join(args, " "); got != want {
			t.Fatalf("checkArgs(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"--read-data", "-1", "10 %"} {
		if _, err := checkArgs(bad); err == nil {
			t.Fatalf("checkArgs(%q) should fail", bad)
		}
	}
}

func TestRepoCheckRun(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	repoID, _ := makeJob(t, app, "check-ok", "/data/c")

	var resp struct {
		RunID string `json:"runId"`
	}
	code := doJSON(t, h, "POST", "/api/repositories/"+repoID+"/check",
		map[string]any{"readData": "10%"}, &resp)
	if code != 202 {
		t.Fatalf("status = %d", code)
	}
	got := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if got.Kind != KindCheck || got.Params["readData"] != "10%" {
		t.Fatalf("run = kind %s params %+v", got.Kind, got.Params)
	}
	if calls := fake.recordedChecks(); len(calls) != 1 || calls[0] != "10%" {
		t.Fatalf("check calls = %v", calls)
	}

	// An empty body is a structure-only check.
	code = doJSON(t, h, "POST", "/api/repositories/"+repoID+"/check", nil, &resp)
	if code != 202 {
		t.Fatalf("empty body status = %d", code)
	}
	waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if calls := fake.recordedChecks(); len(calls) != 2 || calls[1] != "" {
		t.Fatalf("check calls = %v", calls)
	}
}

func TestRepoCheckFoundErrors(t *testing.T) {
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		sink.Log("warn", "stderr", "pack abc contains 2 errors")
		sink.Log("warn", "stderr", "Fatal: repository contains errors")
		return 1, nil
	}}
	app := newRunTestApp(t, fake)
	repoID, _ := makeJob(t, app, "check-bad", "/data/c")

	run, err := app.Coord.StartCheck(repoID, "all")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	got := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(got.Error, "found errors") {
		t.Fatalf("error = %q", got.Error)
	}
}

func TestRepoCheckRejectsBadSubset(t *testing.T) {
	app := newRunTestApp(t, &fakeRunner{installed: true})
	h := routesFor(t, app)
	repoID, _ := makeJob(t, app, "check-invalid", "/data/c")

	code := doJSON(t, h, "POST", "/api/repositories/"+repoID+"/check",
		map[string]any{"readData": "--read-data"}, nil)
	if code != 400 {
		t.Fatalf("status = %d, want 400", code)
	}
}
