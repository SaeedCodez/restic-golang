package core

import (
	"errors"
	"testing"
	"time"
)

func TestNextCheckAt(t *testing.T) {
	at := func(v string) time.Time { return atLocal(t, "2006-01-02 15:04", v) }
	weekly := &RepoCheckSchedule{Enabled: true, Every: CheckEveryWeek, At: "04:00", Since: at("2026-10-06 15:00")}

	cases := []struct {
		name      string
		sched     *RepoCheckSchedule
		lastStart string
		want      string
	}{
		{"first check at the first At after saving", weekly, "", "2026-10-07 04:00"},
		{"a week after the last check", weekly, "2026-10-07 04:00", "2026-10-14 04:00"},
		{"a late start does not slip a day", weekly, "2026-10-07 09:30", "2026-10-14 04:00"},
		{"a manual check postpones the next", weekly, "2026-10-09 15:00", "2026-10-16 04:00"},
		{"a recent check before saving counts", weekly, "2026-10-01 04:00", "2026-10-08 04:00"},
		{"an old check before saving does not", weekly, "2026-09-01 04:00", "2026-10-07 04:00"},
		{"daily", &RepoCheckSchedule{Enabled: true, Every: CheckEveryDay, At: "04:00", Since: at("2026-10-06 15:00")}, "2026-10-07 04:00", "2026-10-08 04:00"},
		{"monthly", &RepoCheckSchedule{Enabled: true, Every: CheckEveryMonth, At: "04:00", Since: at("2026-10-06 15:00")}, "2026-10-07 04:00", "2026-11-06 04:00"},
	}
	for _, c := range cases {
		var last *time.Time
		if c.lastStart != "" {
			v := at(c.lastStart)
			last = &v
		}
		got := nextCheckAt(c.sched, last)
		if got == nil || !got.Equal(at(c.want)) {
			t.Errorf("%s: next = %v, want %s", c.name, got, c.want)
		}
	}

	if nextCheckAt(nil, nil) != nil || nextCheckAt(&RepoCheckSchedule{Every: CheckEveryWeek, At: "04:00"}, nil) != nil {
		t.Fatal("an absent or disabled schedule has a next check")
	}
	if checkDue(weekly, nil, at("2026-10-07 03:59")) || !checkDue(weekly, nil, at("2026-10-07 04:00")) {
		t.Fatal("first check is not due exactly at its slot")
	}
	// A server that was down across the slot catches up when it is back.
	if !checkDue(weekly, nil, at("2026-10-08 12:00")) {
		t.Fatal("missed check is not due")
	}
}

func TestSetRepoCheckScheduleValidates(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	for _, c := range []RepoCheckSchedule{
		{Enabled: true, Every: "hourly"},
		{Enabled: true, At: "25:00"},
		{Enabled: true, ReadData: "0%"},
		{Enabled: true, ReadData: "101%"},
		{Enabled: true, ReadData: "all"},
		{Enabled: true, ReadData: "--read-data"},
	} {
		var invalid *ValidationError
		if _, err := app.SetRepoCheckSchedule(repo.ID, c); !errors.As(err, &invalid) {
			t.Errorf("%+v: err = %v, want a validation error", c, err)
		}
	}

	saved, err := app.SetRepoCheckSchedule(repo.ID, RepoCheckSchedule{Enabled: true, ReadData: "5%"})
	if err != nil {
		t.Fatal(err)
	}
	c := saved.CheckSchedule
	if c == nil || c.Every != CheckEveryWeek || c.At != defaultCheckAt || c.ReadData != "5%" || c.Since.IsZero() {
		t.Fatalf("saved schedule = %+v", c)
	}
	if _, err := app.SetRepoCheckSchedule("missing", RepoCheckSchedule{Enabled: true}); err == nil {
		t.Fatal("saved a schedule for a missing repository")
	}
}

func TestRepoSettingsSurviveARepositoryEdit(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	if code := doJSON(t, h, "PUT", "/api/repositories/"+repo.ID+"/check-schedule", map[string]any{"enabled": true, "every": "daily", "at": "03:30"}, nil); code != 200 {
		t.Fatalf("check schedule: code=%d", code)
	}
	if code := doJSON(t, h, "PUT", "/api/repositories/"+repo.ID+"/speed-limit", map[string]any{"uploadLimitKiB": 5120, "downloadLimitKiB": 0}, nil); code != 200 {
		t.Fatalf("speed limit: code=%d", code)
	}

	// The edit form sends only the repository's own fields.
	var edited struct {
		Repository repoView `json:"repository"`
	}
	if code := doJSON(t, h, "PUT", "/api/repositories/"+repo.ID, map[string]any{"name": "renamed", "backendType": "Local", "localPath": "/tmp/r"}, &edited); code != 200 {
		t.Fatalf("edit: code=%d", code)
	}
	got, _ := app.Repos.Get(repo.ID)
	for _, r := range []Repository{got, edited.Repository.Repository} {
		if r.Name != "renamed" || r.UploadLimitKiB != 5120 || r.CheckSchedule == nil || r.CheckSchedule.At != "03:30" {
			t.Fatalf("after edit: name=%q upload=%d check=%+v", r.Name, r.UploadLimitKiB, r.CheckSchedule)
		}
	}

	// A new repository starts with no schedule and no limits, whatever the body says.
	var created struct {
		Repository repoView `json:"repository"`
	}
	doJSON(t, h, "POST", "/api/repositories", map[string]any{
		"name": "new", "backendType": "Local", "localPath": "/tmp/n", "password": "pw",
		"uploadLimitKiB": 99, "checkSchedule": map[string]any{"enabled": true},
	}, &created)
	fresh, _ := app.Repos.Get(created.Repository.ID)
	if fresh.UploadLimitKiB != 0 || fresh.CheckSchedule != nil {
		t.Fatalf("create stored settings: upload=%d check=%+v", fresh.UploadLimitKiB, fresh.CheckSchedule)
	}
}

func TestSpeedLimitIsValidated(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	for _, body := range []map[string]any{
		{"uploadLimitKiB": -1},
		{"downloadLimitKiB": maxSpeedLimitKiB + 1},
	} {
		if code := doJSON(t, h, "PUT", "/api/repositories/"+repo.ID+"/speed-limit", body, nil); code != 400 {
			t.Errorf("%v: code=%d, want 400", body, code)
		}
	}
	if code := doJSON(t, h, "PUT", "/api/repositories/missing/speed-limit", map[string]any{"uploadLimitKiB": 1}, nil); code != 404 {
		t.Fatalf("missing repository: code=%d, want 404", code)
	}
}

func TestSchedulerStartsADueCheckOnce(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := testApp(t, fake)
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	if _, err := app.Repos.SetCheckSchedule(repo.ID, &RepoCheckSchedule{
		Enabled: true, Every: CheckEveryWeek, At: "04:00", ReadData: "5%", Since: time.Now().Add(-8 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	sched := newScheduler(app)
	sched.tickOnce()
	var run *Run
	deadline := time.Now().Add(2 * time.Second)
	for run == nil && time.Now().Before(deadline) {
		for _, r := range checkRuns(app) {
			run = r
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run == nil {
		t.Fatal("the scheduler did not start a due check")
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Params["trigger"] != TriggerSchedule || final.Params["readData"] != "5%" {
		t.Fatalf("check params = %v", final.Params)
	}

	// The check that just ran satisfies the schedule for a week.
	sched.tickOnce()
	time.Sleep(50 * time.Millisecond)
	if n := len(checkRuns(app)); n != 1 {
		t.Fatalf("checks after a second tick = %d, want 1", n)
	}

	// The repository lists show its outcome and the next one.
	var list struct {
		Repositories []repoView `json:"repositories"`
	}
	doJSON(t, h, "GET", "/api/repositories", nil, &list)
	v := list.Repositories[0]
	if v.LastCheck == nil || v.LastCheck.RunID != run.ID || !v.LastCheck.Scheduled || v.LastCheck.Status != StatusSuccess || v.NextCheckAt == nil {
		t.Fatalf("repository view: lastCheck=%+v next=%v", v.LastCheck, v.NextCheckAt)
	}
	if !v.NextCheckAt.After(time.Now().Add(6 * 24 * time.Hour)) {
		t.Fatalf("next check %v is less than six days away", v.NextCheckAt)
	}
}

func checkRuns(app *App) []*Run {
	runs, _ := app.Runs.Query("", string(KindCheck), "", 50, 0)
	return runs
}
