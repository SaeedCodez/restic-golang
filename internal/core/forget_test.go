package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPlanForgetBatches(t *testing.T) {
	if got := planForgetBatches(nil, 100); len(got) != 0 {
		t.Fatalf("empty = %v", got)
	}
	if got := planForgetBatches([]string{"", "  "}, 100); len(got) != 0 {
		t.Fatalf("blank = %v", got)
	}

	got := planForgetBatches([]string{"a", "", "a", "b"}, 100)
	if len(got) != 1 || !got[0].Prune {
		t.Fatalf("single batch = %+v", got)
	}
	if strings.Join(got[0].IDs, ",") != "a,b" {
		t.Fatalf("dedupe order = %v", got[0].IDs)
	}

	ids := make([]string, 250)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%03d", i)
	}
	got = planForgetBatches(ids, 100)
	if len(got) != 3 {
		t.Fatalf("batches = %d, want 3", len(got))
	}
	if len(got[0].IDs) != 100 || got[0].Prune {
		t.Fatalf("batch 0 = %+v", got[0])
	}
	if len(got[1].IDs) != 100 || got[1].Prune {
		t.Fatalf("batch 1 = %+v", got[1])
	}
	if len(got[2].IDs) != 50 || !got[2].Prune {
		t.Fatalf("batch 2 = %+v", got[2])
	}
}

func TestForgetSnapshotArgs(t *testing.T) {
	got := strings.Join(forgetSnapshotArgs([]string{"abc", "def"}, true), " ")
	want := "forget --json --prune abc def"
	if got != want {
		t.Fatalf("prune args = %q, want %q", got, want)
	}
	got = strings.Join(forgetSnapshotArgs([]string{"abc"}, false), " ")
	if got != "forget --json abc" {
		t.Fatalf("no-prune args = %q", got)
	}
}

func TestMatchSnapshot(t *testing.T) {
	snaps := []Snapshot{
		{ID: "aaaaaaaa1111", ShortID: "aaaaaaaa"},
		{ID: "bbbbbbbb2222", ShortID: "bbbbbbbb"},
		{ID: "aaaaaccccccc", ShortID: "aaaaaccc"},
	}
	got, err := matchSnapshot(snaps, "bbbbbbbb2222")
	if err != nil || got.ID != "bbbbbbbb2222" {
		t.Fatalf("full id: %+v %v", got, err)
	}
	got, err = matchSnapshot(snaps, "bbbbbbbb")
	if err != nil || got.ID != "bbbbbbbb2222" {
		t.Fatalf("short id: %+v %v", got, err)
	}
	if _, err := matchSnapshot(snaps, "aaaa"); err == nil {
		t.Fatal("ambiguous prefix should fail")
	}
	if _, err := matchSnapshot(snaps, "missing"); err == nil {
		t.Fatal("missing should fail")
	}
	if _, err := matchSnapshot(snaps, ""); err == nil {
		t.Fatal("empty should fail")
	}
	got, err = matchSnapshot(snaps, "bbbbbbbb222")
	if err != nil || got.ID != "bbbbbbbb2222" {
		t.Fatalf("unique prefix: %+v %v", got, err)
	}
}

func TestForgetSnapshotHTTP(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{
		{ID: "snap-full-id", ShortID: "snap-ful"},
	}}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	repoID, _ := makeJob(t, app, "forget-one", "/data/f")

	var resp struct {
		RunID string `json:"runId"`
		Run   *Run   `json:"run"`
	}
	code := doJSON(t, h, "POST", "/api/repositories/"+repoID+"/forget",
		map[string]any{"snapshotId": "snap-ful"}, &resp)
	if code != 202 {
		t.Fatalf("status = %d", code)
	}
	got := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if got.Kind != KindForget {
		t.Fatalf("kind = %s", got.Kind)
	}
	calls := fake.recordedForgetSnapshots()
	if len(calls) != 1 || len(calls[0].IDs) != 1 || calls[0].IDs[0] != "snap-full-id" {
		t.Fatalf("forget snapshots = %+v", calls)
	}
}

func TestForgetJobByTag(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{
		{ID: "j1"}, {ID: "j2"},
	}}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "forget-job", "/data/j")

	var resp struct {
		RunID string `json:"runId"`
	}
	code := doJSON(t, h, "POST", "/api/jobs/"+jobID+"/forget", map[string]any{}, &resp)
	if code != 202 {
		t.Fatalf("status = %d", code)
	}
	got := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if got.Params["tag"] != "resticweb-job:"+jobID {
		t.Fatalf("tag param = %q", got.Params["tag"])
	}
	if got.Params["deleteJob"] != "" {
		t.Fatalf("deleteJob param = %q", got.Params["deleteJob"])
	}
	tags := fake.recordedTags()
	if len(tags) == 0 || tags[len(tags)-1] != "resticweb-job:"+jobID {
		t.Fatalf("snapshot list tags = %v", tags)
	}
	calls := fake.recordedForgetSnapshots()
	if len(calls) != 1 || strings.Join(calls[0].IDs, ",") != "j1,j2" {
		t.Fatalf("ids = %+v", calls)
	}
	if _, ok := app.Jobs.Get(jobID); !ok {
		t.Fatal("job should still exist")
	}
}

func TestForgetJobDeleteAfterSuccess(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "gone"}}}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "forget-del", "/data/d")

	var resp struct {
		RunID string `json:"runId"`
	}
	code := doJSON(t, h, "POST", "/api/jobs/"+jobID+"/forget",
		map[string]any{"deleteJob": true}, &resp)
	if code != 202 {
		t.Fatalf("status = %d", code)
	}
	got := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if got.Params["deleteJob"] != "true" {
		t.Fatalf("deleteJob param = %q", got.Params["deleteJob"])
	}
	if _, ok := app.Jobs.Get(jobID); ok {
		t.Fatal("job should be deleted after successful forget")
	}
}

func TestForgetJobDeleteKeepsJobOnFailure(t *testing.T) {
	fake := &fakeRunner{
		installed: true,
		snaps:     []Snapshot{{ID: "keepme"}},
		streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
			return 1, nil
		},
	}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "forget-fail", "/data/ff")

	run, err := app.Coord.StartForgetJob(jobID, true)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if _, ok := app.Jobs.Get(jobID); !ok {
		t.Fatal("job should remain after failed forget")
	}
}

func TestForgetJobEmptySnapshotsStillDeletes(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "forget-empty", "/data/e")

	run, err := app.Coord.StartForgetJob(jobID, true)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	calls := fake.recordedForgetSnapshots()
	if len(calls) != 1 || len(calls[0].IDs) != 0 {
		t.Fatalf("expected empty forget, got %+v", calls)
	}
	if _, ok := app.Jobs.Get(jobID); ok {
		t.Fatal("job should be deleted even with no snapshots")
	}
}

func TestResetRepoForgetsAllSnapshots(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{
		{ID: "one"}, {ID: "two"},
	}}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	repoID, jobID := makeJob(t, app, "reset-repo", "/data/r")

	var resp struct {
		RunID string `json:"runId"`
	}
	code := doJSON(t, h, "POST", "/api/repositories/"+repoID+"/reset", nil, &resp)
	if code != 202 {
		t.Fatalf("status = %d", code)
	}
	got := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if got.Kind != KindForget || got.Params["scope"] != "repo" {
		t.Fatalf("run = kind %s params %+v", got.Kind, got.Params)
	}
	calls := fake.recordedForgetSnapshots()
	if len(calls) != 1 || strings.Join(calls[0].IDs, ",") != "one,two" {
		t.Fatalf("ids = %+v", calls)
	}
	if _, ok := app.Jobs.Get(jobID); !ok {
		t.Fatal("reset must not delete jobs")
	}
	if _, ok := app.Repos.Get(repoID); !ok {
		t.Fatal("reset must not delete the repository entity")
	}
}

func TestForgetBusyConflict(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	repoID, jobID := makeJob(t, app, "forget-busy", "/data/b")

	backup, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	waitForStatus(t, app.Runs, backup.ID, StatusRunning, 2*time.Second)
	<-started

	code := doJSON(t, h, "POST", "/api/repositories/"+repoID+"/forget",
		map[string]any{"snapshotId": "x"}, nil)
	if code != 409 {
		t.Fatalf("forget while busy: %d", code)
	}
	close(release)
	waitForStatus(t, app.Runs, backup.ID, StatusSuccess, 2*time.Second)
}

func TestDeleteJobWhileRunningConflicts(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "del-running", "/data/dr")

	backup, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	waitForStatus(t, app.Runs, backup.ID, StatusRunning, 2*time.Second)
	<-started

	code := doJSON(t, h, "DELETE", "/api/jobs/"+jobID, nil, nil)
	if code != 409 {
		t.Fatalf("delete while running: %d", code)
	}
	close(release)
	waitForStatus(t, app.Runs, backup.ID, StatusSuccess, 2*time.Second)
	if _, ok := app.Jobs.Get(jobID); !ok {
		t.Fatal("job should still exist")
	}
}

func TestCatalogJobDeleteDoesNotForget(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "keep"}}}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "del-catalog", "/data/c")

	code := doJSON(t, h, "DELETE", "/api/jobs/"+jobID, nil, nil)
	if code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if n := len(fake.recordedForgetSnapshots()); n != 0 {
		t.Fatalf("forget calls = %d, want 0", n)
	}
	if _, ok := app.Jobs.Get(jobID); ok {
		t.Fatal("job should be gone")
	}
}

func TestForgetSnapshotMissingID(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	if _, err := app.Coord.StartForgetSnapshot("missing", "abc"); err == nil {
		t.Fatal("expected not found")
	}
	repoID, _ := makeJob(t, app, "forget-missing", "/data/m")
	if _, err := app.Coord.StartForgetSnapshot(repoID, "  "); err == nil {
		t.Fatal("expected validation error")
	}
}
