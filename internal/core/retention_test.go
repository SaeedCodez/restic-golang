package core

import (
	"context"
	"errors"
	"strings"
	"sync"
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
	backup := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)

	// The finished backup already names the retention run, so a caller that
	// waits on the backup (restic-webctl) knows to wait for it too.
	retentionID := backup.Params[ParamFollowUpRun]
	if retentionID == "" {
		t.Fatal("expected the finished backup to name the retention run after it")
	}
	got := waitForStatus(t, app.Runs, retentionID, StatusSuccess, 2*time.Second)
	if got.Kind != KindRetention {
		t.Fatalf("follow-up kind = %q, want retention", got.Kind)
	}
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
	backup := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if next := backup.Params[ParamFollowUpRun]; next != "" {
		t.Fatalf("backup names follow-up run %q, want none", next)
	}
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

// The retention run takes the repository straight from the backup, so another
// operation cannot slip in between and push retention to the next backup.
func TestBackupHandsRepositoryToRetention(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	fake := &fakeRunner{installed: true, streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
		if kind == KindRetention {
			select {
			case <-release:
			case <-ctx.Done():
				return resticExitInterrupted, nil
			}
		}
		return 0, nil
	}}
	app := newRunTestApp(t, fake)
	t.Cleanup(unblock)
	repoID, jobID := makeJob(t, app, "ret-handoff", "/data/handoff")

	job, _ := app.Jobs.Get(jobID)
	job.Retention = &JobRetention{Enabled: true, Preset: RetentionPresetBalanced}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	backup := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	retentionID := backup.Params[ParamFollowUpRun]
	if retentionID == "" {
		t.Fatal("expected the finished backup to name the retention run after it")
	}
	if ret, ok := app.Runs.Get(retentionID); !ok || ret.Status.Terminal() {
		t.Fatalf("retention run should still be going, got %+v", ret)
	}

	_, err = app.Coord.StartCheck(repoID, "")
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("StartCheck during retention: err = %v, want busy", err)
	}
	if busy.Blocking.RunID != retentionID {
		t.Fatalf("blocker = %q, want the retention run %q", busy.Blocking.RunID, retentionID)
	}

	unblock()
	waitForStatus(t, app.Runs, retentionID, StatusSuccess, 2*time.Second)
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

func TestPreviewForgetArgsAreADryRunWithoutLockOrPrune(t *testing.T) {
	args, err := previewForgetArgs("resticweb-job:abc", JobRetention{KeepLast: 5})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if strings.Contains(got, "--prune") || !strings.Contains(got, "--dry-run --no-lock") || !strings.Contains(got, "--keep-last 5") {
		t.Fatalf("args = %s", got)
	}
}

func TestRetentionPreviewChangesNothing(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "prev", "/data/prev")
	job, _ := app.Jobs.Get(jobID)
	job.Retention = &JobRetention{Enabled: true, Preset: RetentionPresetBalanced}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatal(err)
	}
	run, err := app.Coord.StartRetentionWith(jobID, TriggerManual, true)
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Params["dryRun"] != "true" || len(fake.recordedForgets()) != 0 || len(fake.previewCalls) != 1 {
		t.Fatalf("dryRun=%q forgets=%d previews=%d", final.Params["dryRun"], len(fake.recordedForgets()), len(fake.previewCalls))
	}
}
