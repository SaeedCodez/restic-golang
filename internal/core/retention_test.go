package core

import (
	"strings"
	"testing"
	"time"
)

func TestJobRetentionValidate(t *testing.T) {
	cases := []struct {
		name    string
		ret     *JobRetention
		wantErr bool
	}{
		{"nil ok", nil, false},
		{"disabled empty ok", &JobRetention{Enabled: false}, false},
		{"balanced", &JobRetention{Enabled: true, Preset: RetentionPresetBalanced}, false},
		{"custom with rules", &JobRetention{Enabled: true, Preset: RetentionPresetCustom, KeepLast: 10}, false},
		{"enabled no rules", &JobRetention{Enabled: true, Preset: RetentionPresetCustom}, true},
		{"bad preset", &JobRetention{Enabled: true, Preset: "yearly", KeepLast: 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ret.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}

	bal := &JobRetention{Enabled: true, Preset: RetentionPresetBalanced}
	if err := bal.Validate(); err != nil {
		t.Fatal(err)
	}
	if bal.KeepLast != 24 || bal.KeepDaily != 7 || bal.KeepWeekly != 4 {
		t.Fatalf("balanced normalize = %+v", bal)
	}
}

func TestJobRetentionDescribe(t *testing.T) {
	r := &JobRetention{KeepLast: 24, KeepDaily: 7, KeepWeekly: 4}
	got := r.Describe()
	if !strings.Contains(got, "last 24") || !strings.Contains(got, "7 daily") || !strings.Contains(got, "4 weekly") {
		t.Fatalf("Describe = %q", got)
	}
}

func TestForgetArgs(t *testing.T) {
	args, err := forgetArgs("resticweb-job:abc", JobRetention{
		Preset: RetentionPresetBalanced, KeepLast: 24, KeepDaily: 7, KeepWeekly: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	want := []string{"forget", "--json", "--prune", "--tag", "resticweb-job:abc",
		"--keep-last", "24", "--keep-daily", "7", "--keep-weekly", "4"}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Fatalf("args %v missing %q", args, w)
		}
	}

	args, err = forgetArgs("resticweb-job:abc", JobRetention{KeepWithinDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "--keep-within 30d") {
		t.Fatalf("keep-within missing: %v", args)
	}

	if _, err := forgetArgs("", JobRetention{KeepLast: 1}); err == nil {
		t.Fatal("empty tag should fail")
	}
	if _, err := forgetArgs("tag", JobRetention{}); err == nil {
		t.Fatal("no keep rules should fail")
	}
}

func TestRetentionChainsAfterBackup(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "ret-chain", "/data/ret")

	job, _ := app.Jobs.Get(jobID)
	job.Retention = &JobRetention{Enabled: true, Preset: RetentionPresetBalanced}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)

	var retentionID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range app.Runs.runsForJob(jobID) {
			if r.Kind == KindRetention {
				retentionID = r.ID
				break
			}
		}
		if retentionID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if retentionID == "" {
		t.Fatal("expected a retention run after successful backup")
	}
	got := waitForStatus(t, app.Runs, retentionID, StatusSuccess, 2*time.Second)
	if got.Params["trigger"] != TriggerAfterBackup {
		t.Fatalf("trigger = %q, want after_backup", got.Params["trigger"])
	}
	forgets := fake.recordedForgets()
	if len(forgets) != 1 {
		t.Fatalf("forget calls = %d, want 1", len(forgets))
	}
	if forgets[0].Tag != "resticweb-job:"+jobID {
		t.Fatalf("forget tag = %q", forgets[0].Tag)
	}
	if forgets[0].Policy.KeepLast != 24 || forgets[0].Policy.KeepWeekly != 4 {
		t.Fatalf("forget policy = %+v", forgets[0].Policy)
	}
}

func TestRetentionSkippedWhenDisabled(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "ret-off", "/data/off")

	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	time.Sleep(100 * time.Millisecond)

	for _, r := range app.Runs.runsForJob(jobID) {
		if r.Kind == KindRetention {
			t.Fatal("retention must not run when disabled")
		}
	}
	if n := len(fake.recordedForgets()); n != 0 {
		t.Fatalf("forget calls = %d, want 0", n)
	}
}

func TestStartRetentionManual(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "ret-manual", "/data/m")

	job, _ := app.Jobs.Get(jobID)
	job.Retention = &JobRetention{Enabled: true, Preset: RetentionPresetLight}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	var resp struct {
		RunID string `json:"runId"`
		Run   *Run   `json:"run"`
	}
	code := doJSON(t, h, "POST", "/api/jobs/"+jobID+"/retention", nil, &resp)
	if code != 202 {
		t.Fatalf("status = %d", code)
	}
	got := waitForStatus(t, app.Runs, resp.RunID, StatusSuccess, 2*time.Second)
	if got.Kind != KindRetention {
		t.Fatalf("kind = %s", got.Kind)
	}
	if got.Params["trigger"] != TriggerManual {
		t.Fatalf("trigger = %q", got.Params["trigger"])
	}
}

func TestStartRetentionRequiresEnabled(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "ret-req", "/data/r")
	if _, err := app.Coord.StartRetention(jobID, TriggerManual); err == nil {
		t.Fatal("expected error when retention disabled")
	}
}

func TestJobRetentionPersisted(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	h := routesFor(t, app)
	_, jobID := makeJob(t, app, "ret-persist", "/data/p")

	body := map[string]any{
		"name":         "ret-persist",
		"folderId":     "",
		"repositoryId": "",
		"retention": map[string]any{
			"enabled": true,
			"preset":  "long",
		},
	}
	job, _ := app.Jobs.Get(jobID)
	body["folderId"] = job.FolderID
	body["repositoryId"] = job.RepositoryID

	var updated struct {
		Job jobView `json:"job"`
	}
	code := doJSON(t, h, "PUT", "/api/jobs/"+jobID, body, &updated)
	if code != 200 {
		t.Fatalf("update status = %d", code)
	}
	if updated.Job.Retention == nil || !updated.Job.Retention.Enabled {
		t.Fatalf("retention missing: %+v", updated.Job.Retention)
	}
	if updated.Job.Retention.Preset != RetentionPresetLong {
		t.Fatalf("preset = %q", updated.Job.Retention.Preset)
	}
	if updated.Job.Retention.KeepMonthly != 12 {
		t.Fatalf("long preset keepMonthly = %d", updated.Job.Retention.KeepMonthly)
	}

	reloaded, ok := app.Jobs.Get(jobID)
	if !ok || reloaded.Retention == nil || reloaded.Retention.KeepDaily != 14 {
		t.Fatalf("persisted retention = %+v", reloaded.Retention)
	}
}
