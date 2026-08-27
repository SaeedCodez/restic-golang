package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This file covers the API additions the redesigned UI depends on: a job's last
// run travelling with the job, filterable/limited run history, and repository
// secrets never reaching the browser.

// rawGet performs a GET and returns the status plus the raw response body, so a
// test can assert on which keys are present — not just on decoded values.
func rawGet(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Code, rec.Body.String()
}

// ---- repository secrets ----------------------------------------------------

func TestRepositorySecretsNeverReachTheClient(t *testing.T) {
	h := testServer(t)

	var created struct {
		Repository struct {
			ID           string `json:"id"`
			HasPassword  bool   `json:"hasPassword"`
			HasSecretKey bool   `json:"hasSecretKey"`
		} `json:"repository"`
	}
	if code := doJSON(t, h, "POST", "/api/repositories", map[string]any{
		"name": "S3", "backendType": "S3", "endpoint": "https://s3.example.com",
		"bucket": "b", "accessKey": "AK", "secretKey": "SUPERSECRET", "password": "hunter2",
	}, &created); code != http.StatusOK {
		t.Fatalf("create repo: code=%d", code)
	}
	id := created.Repository.ID
	if !created.Repository.HasPassword || !created.Repository.HasSecretKey {
		t.Fatalf("expected hasPassword/hasSecretKey to be reported: %+v", created.Repository)
	}

	for _, path := range []string{"/api/repositories", "/api/repositories/" + id} {
		code, body := rawGet(t, h, path)
		if code != http.StatusOK {
			t.Fatalf("GET %s: code=%d", path, code)
		}
		if strings.Contains(body, "hunter2") || strings.Contains(body, "SUPERSECRET") {
			t.Fatalf("GET %s leaked a secret: %s", path, body)
		}
		// The access key is not a secret and stays visible, so the edit form can
		// show which key is in use.
		if !strings.Contains(body, "AK") {
			t.Fatalf("GET %s dropped the access key: %s", path, body)
		}
	}
}

func TestRepositoryUpdateKeepsOmittedSecrets(t *testing.T) {
	app := testApp(t, newResticRunner())
	h := routesFor(t, app)

	repo, err := app.Repos.Create(Repository{
		Meta: Meta{Name: "S3"}, BackendType: "S3", Endpoint: "https://s3.example.com",
		Bucket: "b", AccessKey: "AK", SecretKey: "SUPERSECRET", Password: "hunter2",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// An edit that renames the repository, sending no secrets at all (exactly what
	// the UI sends when the user leaves the secret fields blank).
	if code := doJSON(t, h, "PUT", "/api/repositories/"+repo.ID, map[string]any{
		"name": "S3 renamed", "backendType": "S3", "endpoint": "https://s3.example.com",
		"bucket": "b", "accessKey": "AK",
	}, nil); code != http.StatusOK {
		t.Fatalf("update without secrets: code=%d, want 200", code)
	}

	stored, _ := app.Repos.Get(repo.ID)
	if stored.Name != "S3 renamed" {
		t.Fatalf("rename did not apply: %q", stored.Name)
	}
	if stored.Password != "hunter2" || stored.SecretKey != "SUPERSECRET" {
		t.Fatalf("omitted secrets were not preserved: password=%q secretKey=%q",
			stored.Password, stored.SecretKey)
	}

	// A supplied secret still replaces the stored one.
	if code := doJSON(t, h, "PUT", "/api/repositories/"+repo.ID, map[string]any{
		"name": "S3 renamed", "backendType": "S3", "endpoint": "https://s3.example.com",
		"bucket": "b", "accessKey": "AK", "password": "newpass",
	}, nil); code != http.StatusOK {
		t.Fatalf("update with a new password: code=%d", code)
	}
	if stored, _ = app.Repos.Get(repo.ID); stored.Password != "newpass" {
		t.Fatalf("new password not stored: %q", stored.Password)
	}
}

// ---- job last-run ----------------------------------------------------------

func TestJobViewCarriesLastRun(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "nightly", "/data")

	// Before any run, the job reports no history at all.
	var before struct {
		Jobs []jobView `json:"jobs"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs", nil, &before); code != http.StatusOK {
		t.Fatalf("list jobs: %d", code)
	}
	if len(before.Jobs) != 1 || before.Jobs[0].LastRun != nil || before.Jobs[0].RunCount != 0 {
		t.Fatalf("expected no run history yet: %+v", before.Jobs)
	}

	// Run twice; the newest run must be the one reported.
	first, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	waitForStatus(t, app.Runs, first.ID, StatusSuccess, 2*time.Second)
	time.Sleep(2 * time.Millisecond) // distinct start timestamps
	second, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup 2: %v", err)
	}
	waitForStatus(t, app.Runs, second.ID, StatusSuccess, 2*time.Second)

	var after struct {
		Jobs []jobView `json:"jobs"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs", nil, &after); code != http.StatusOK {
		t.Fatalf("list jobs: %d", code)
	}
	got := after.Jobs[0]
	if got.RunCount != 2 {
		t.Fatalf("RunCount = %d, want 2", got.RunCount)
	}
	if got.LastRun == nil || got.LastRun.ID != second.ID {
		t.Fatalf("LastRun = %+v, want the newest run %s", got.LastRun, second.ID)
	}

	// The single-job endpoint agrees with the list.
	var one struct {
		Job jobView `json:"job"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs/"+jobID, nil, &one); code != http.StatusOK {
		t.Fatalf("get job: %d", code)
	}
	if one.Job.LastRun == nil || one.Job.LastRun.ID != second.ID || one.Job.RunCount != 2 {
		t.Fatalf("single-job view disagrees: %+v", one.Job)
	}
}

func TestJobScheduleInView(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "sched-view", "/data")

	job, _ := app.Jobs.Get(jobID)
	job.Schedule = &JobSchedule{Enabled: true, Kind: ScheduleDaily, At: "02:00"}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	var one struct {
		Job jobView `json:"job"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs/"+jobID, nil, &one); code != http.StatusOK {
		t.Fatalf("get job: %d", code)
	}
	if one.Job.Schedule == nil || !one.Job.Schedule.Enabled || one.Job.Schedule.Kind != ScheduleDaily {
		t.Fatalf("schedule missing on view: %+v", one.Job.Schedule)
	}
	if one.Job.ScheduleState != ScheduleStateScheduled {
		t.Fatalf("scheduleState = %q, want scheduled", one.Job.ScheduleState)
	}
	if one.Job.NextDueAt == nil {
		t.Fatal("nextDueAt should be set for an enabled daily schedule")
	}

	// Manual run stamps trigger=manual.
	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	got, _ := app.Runs.Get(run.ID)
	if got.Params["trigger"] != TriggerManual {
		t.Fatalf("trigger = %q, want manual", got.Params["trigger"])
	}
}

func TestJobScheduleOverdueInView(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "sched-overdue", "/data")

	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	past := time.Now().UTC().Add(-8 * time.Hour)
	app.Runs.mutate(final.ID, true, func(r *Run) {
		r.StartedAt = past
		r.FinishedAt = &past
	})

	job, _ := app.Jobs.Get(jobID)
	job.Schedule = &JobSchedule{Enabled: true, Kind: ScheduleEvery, Every: "6h"}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	var one struct {
		Job jobView `json:"job"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs/"+jobID, nil, &one); code != http.StatusOK {
		t.Fatalf("get job: %d", code)
	}
	if one.Job.ScheduleState != ScheduleStateOverdue {
		t.Fatalf("scheduleState = %q, want overdue", one.Job.ScheduleState)
	}
	if one.Job.NextDueAt == nil || one.Job.NextDueAt.After(time.Now()) {
		t.Fatalf("nextDueAt should be in the past when overdue, got %v", one.Job.NextDueAt)
	}
}

// ---- run list filters ------------------------------------------------------

func TestRunListFiltersAndLimit(t *testing.T) {
	fake := &fakeRunner{installed: true, testResult: TestResult{OK: true}}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "nightly", "/data")

	// Two backups and one init, so kind/status filtering has something to bite on.
	for i := 0; i < 2; i++ {
		run, err := app.Coord.StartBackup(jobID)
		if err != nil {
			t.Fatalf("StartBackup: %v", err)
		}
		waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
		time.Sleep(2 * time.Millisecond)
	}
	job, _ := app.Jobs.Get(jobID)
	initRun, err := app.Coord.StartInit(job.RepositoryID)
	if err != nil {
		t.Fatalf("StartInit: %v", err)
	}
	waitForStatus(t, app.Runs, initRun.ID, StatusSuccess, 2*time.Second)

	list := func(query string) (runs []*Run, total int) {
		var resp struct {
			Runs  []*Run `json:"runs"`
			Total int    `json:"total"`
		}
		code, body := rawGet(t, h, "/api/runs"+query)
		if code != http.StatusOK {
			t.Fatalf("GET /api/runs%s: code=%d", query, code)
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Runs, resp.Total
	}

	if runs, total := list(""); len(runs) != 3 || total != 3 {
		t.Fatalf("unfiltered: got %d runs (total %d), want 3", len(runs), total)
	}
	if runs, _ := list("?kind=backup"); len(runs) != 2 {
		t.Fatalf("kind=backup: got %d, want 2", len(runs))
	}
	if runs, _ := list("?kind=init"); len(runs) != 1 {
		t.Fatalf("kind=init: got %d, want 1", len(runs))
	}
	if runs, _ := list("?status=finished"); len(runs) != 3 {
		t.Fatalf("status=finished: got %d, want 3", len(runs))
	}
	if runs, _ := list("?status=active"); len(runs) != 0 {
		t.Fatalf("status=active: got %d, want 0", len(runs))
	}
	if runs, _ := list("?jobId=" + jobID); len(runs) != 2 {
		t.Fatalf("jobId: got %d, want 2", len(runs))
	}

	// A limit truncates the page but `total` still reports the full match count,
	// so the UI can say how much it is not showing.
	runs, total := list("?limit=2")
	if len(runs) != 2 || total != 3 {
		t.Fatalf("limit=2: got %d runs, total %d; want 2 and 3", len(runs), total)
	}
	// Newest first: the init run started last.
	if runs[0].Kind != KindInit {
		t.Fatalf("expected newest-first ordering, got %s first", runs[0].Kind)
	}

	page2, total2 := list("?limit=2&offset=2")
	if len(page2) != 1 || total2 != 3 {
		t.Fatalf("limit=2&offset=2: got %d runs, total %d; want 1 and 3", len(page2), total2)
	}
	if page2[0].ID == runs[0].ID || page2[0].ID == runs[1].ID {
		t.Fatalf("offset page overlapped with first page")
	}
}

func TestJobRunsRespectsLimit(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "nightly", "/data")

	for i := 0; i < 3; i++ {
		run, err := app.Coord.StartBackup(jobID)
		if err != nil {
			t.Fatalf("StartBackup: %v", err)
		}
		waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
		time.Sleep(2 * time.Millisecond)
	}

	var resp struct {
		Runs  []*Run `json:"runs"`
		Total int    `json:"total"`
	}
	if code := doJSON(t, h, "GET", "/api/jobs/"+jobID+"/runs?limit=2", nil, &resp); code != http.StatusOK {
		t.Fatalf("job runs: %d", code)
	}
	if len(resp.Runs) != 2 || resp.Total != 3 {
		t.Fatalf("got %d runs (total %d), want 2 and 3", len(resp.Runs), resp.Total)
	}
}
