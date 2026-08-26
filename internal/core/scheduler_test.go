package core

import (
	"context"
	"testing"
	"time"
)

func atLocal(t *testing.T, layout, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func TestJobScheduleValidate(t *testing.T) {
	cases := []struct {
		name    string
		sched   *JobSchedule
		wantErr bool
	}{
		{"nil ok", nil, false},
		{"hourly", &JobSchedule{Kind: ScheduleHourly}, false},
		{"every 6h", &JobSchedule{Kind: ScheduleEvery, Every: "6h"}, false},
		{"every too short", &JobSchedule{Kind: ScheduleEvery, Every: "30m"}, true},
		{"every bad", &JobSchedule{Kind: ScheduleEvery, Every: "nope"}, true},
		{"daily", &JobSchedule{Kind: ScheduleDaily, At: "02:00"}, false},
		{"daily bad clock", &JobSchedule{Kind: ScheduleDaily, At: "25:00"}, true},
		{"weekly", &JobSchedule{Kind: ScheduleWeekly, At: "02:00", Weekdays: []int{1, 3}}, false},
		{"weekly no days", &JobSchedule{Kind: ScheduleWeekly, At: "02:00"}, true},
		{"weekly bad day", &JobSchedule{Kind: ScheduleWeekly, At: "02:00", Weekdays: []int{7}}, true},
		{"empty kind", &JobSchedule{Kind: ""}, true},
		{"unknown kind", &JobSchedule{Kind: "cron"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sched.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}

func TestJobDueInterval(t *testing.T) {
	sched := &JobSchedule{Enabled: true, Kind: ScheduleEvery, Every: "6h"}
	now := atLocal(t, "2006-01-02 15:04", "2026-08-26 15:00")

	// Never run → due immediately.
	if !jobDue(sched, nil, false, now) {
		t.Fatal("never-run interval should be due")
	}

	// Success 2h ago → not due.
	last := now.Add(-2 * time.Hour)
	if jobDue(sched, &last, true, now) {
		t.Fatal("should not be due within period after success")
	}

	// Success 7h ago → due.
	last = now.Add(-7 * time.Hour)
	if !jobDue(sched, &last, true, now) {
		t.Fatal("should be due after period")
	}

	// Failed 30m ago → not yet (retry is 1h).
	last = now.Add(-30 * time.Minute)
	if jobDue(sched, &last, false, now) {
		t.Fatal("should not retry sooner than 1h after failure")
	}

	// Failed 90m ago → due (retry).
	last = now.Add(-90 * time.Minute)
	if !jobDue(sched, &last, false, now) {
		t.Fatal("should retry after 1h on failure")
	}
}

func TestJobDueDaily(t *testing.T) {
	sched := &JobSchedule{Enabled: true, Kind: ScheduleDaily, At: "02:00"}

	// Enable at 15:00, never run → wait for tomorrow 02:00.
	now := atLocal(t, "2006-01-02 15:04", "2026-08-26 15:00")
	if jobDue(sched, nil, false, now) {
		t.Fatal("never-run daily should wait for next At")
	}
	next := nextDueAt(sched, nil, false, now)
	want := atLocal(t, "2006-01-02 15:04", "2026-08-27 02:00")
	if next == nil || !next.Equal(want) {
		t.Fatalf("nextDueAt = %v, want %v", next, want)
	}

	// Catch-up: last attempt yesterday 01:00, now 10:00 → missed 02:00 → due.
	last := atLocal(t, "2006-01-02 15:04", "2026-08-25 01:00")
	now = atLocal(t, "2006-01-02 15:04", "2026-08-26 10:00")
	if !jobDue(sched, &last, true, now) {
		t.Fatal("missed daily slot should catch up")
	}

	// Success today at 02:05 → next is tomorrow.
	last = atLocal(t, "2006-01-02 15:04", "2026-08-26 02:05")
	now = atLocal(t, "2006-01-02 15:04", "2026-08-26 10:00")
	if jobDue(sched, &last, true, now) {
		t.Fatal("already backed up today should not be due")
	}

	// Manual success at 15:00 satisfies today's slot.
	last = atLocal(t, "2006-01-02 15:04", "2026-08-26 15:00")
	now = atLocal(t, "2006-01-02 15:04", "2026-08-26 16:00")
	if jobDue(sched, &last, true, now) {
		t.Fatal("manual success should postpone next calendar fire")
	}

	// Failed at 02:05; 90m later → retry due.
	last = atLocal(t, "2006-01-02 15:04", "2026-08-26 02:05")
	now = atLocal(t, "2006-01-02 15:04", "2026-08-26 03:40")
	if !jobDue(sched, &last, false, now) {
		t.Fatal("failed daily should retry after 1h")
	}
}

func TestJobDueWeekly(t *testing.T) {
	// Monday and Thursday at 02:00. 2026-08-26 is a Wednesday.
	sched := &JobSchedule{Enabled: true, Kind: ScheduleWeekly, At: "02:00", Weekdays: []int{1, 4}}
	now := atLocal(t, "2006-01-02 15:04", "2026-08-26 15:00") // Wed
	next := nextDueAt(sched, nil, false, now)
	want := atLocal(t, "2006-01-02 15:04", "2026-08-27 02:00") // Thu
	if next == nil || !next.Equal(want) {
		t.Fatalf("next weekly = %v, want %v (weekday %v)", next, want, want.Weekday())
	}
}

func TestJobDueDisabled(t *testing.T) {
	sched := &JobSchedule{Enabled: false, Kind: ScheduleHourly}
	now := time.Now()
	if jobDue(sched, nil, false, now) {
		t.Fatal("disabled schedule must never be due")
	}
	if nextDueAt(sched, nil, false, now) != nil {
		t.Fatal("disabled schedule has no nextDueAt")
	}
}

func TestScheduleOverdue(t *testing.T) {
	sched := &JobSchedule{Enabled: true, Kind: ScheduleEvery, Every: "6h"}
	now := atLocal(t, "2006-01-02 15:04", "2026-08-26 15:00")

	// Never succeeded but due → overdue.
	if !scheduleOverdue(sched, nil, false, nil, false, now) {
		t.Fatal("due never-success interval should be overdue")
	}

	// Daily never-run at 15:00 (not yet due) → not overdue.
	daily := &JobSchedule{Enabled: true, Kind: ScheduleDaily, At: "02:00"}
	if scheduleOverdue(daily, nil, false, nil, false, now) {
		t.Fatal("waiting for first daily slot should not be overdue")
	}

	// Success 7h ago on 6h schedule → overdue.
	success := now.Add(-7 * time.Hour)
	attempt := success
	if !scheduleOverdue(sched, &attempt, true, &success, false, now) {
		t.Fatal("stale success should be overdue")
	}

	// Running → not overdue.
	if scheduleOverdue(sched, &attempt, true, &success, true, now) {
		t.Fatal("running job should not be overdue")
	}
}

func TestSchedulerFiresDueJob(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "sched-fire", "/data/a")

	job, _ := app.Jobs.Get(jobID)
	job.Schedule = &JobSchedule{Enabled: true, Kind: ScheduleHourly}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	now := time.Now()
	sched := newScheduler(app)
	sched.now = func() time.Time { return now }
	sched.tickOnce()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not start a due backup")
	}
	close(release)

	// Find the run and check trigger.
	runs := app.Runs.runsForJob(jobID)
	if len(runs) == 0 {
		t.Fatal("no run created")
	}
	if runs[0].Params["trigger"] != TriggerSchedule {
		t.Fatalf("trigger = %q, want schedule", runs[0].Params["trigger"])
	}
	waitForStatus(t, app.Runs, runs[0].ID, StatusSuccess, 2*time.Second)
}

func TestSchedulerSkipsWhenNotDue(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "sched-skip", "/data/b")

	// Seed a fresh successful backup.
	run, err := app.Coord.StartBackup(jobID)
	if err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)

	job, _ := app.Jobs.Get(jobID)
	job.Schedule = &JobSchedule{Enabled: true, Kind: ScheduleEvery, Every: "6h"}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	sched := newScheduler(app)
	sched.now = time.Now
	sched.tickOnce()

	runs := app.Runs.runsForJob(jobID)
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want only the manual one", len(runs))
	}
}

func TestSchedulerSkipsBusyRepository(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app := newRunTestApp(t, fake)

	repoID, jobA := makeJob(t, app, "busy-a", "/data/a")
	jobB := addJob(t, app, "busy-b", "/data/b", repoID)

	// Hold the repository with job A.
	runA, err := app.Coord.StartBackup(jobA)
	if err != nil {
		t.Fatalf("StartBackup A: %v", err)
	}
	<-started

	job, _ := app.Jobs.Get(jobB)
	job.Schedule = &JobSchedule{Enabled: true, Kind: ScheduleHourly}
	if _, err := app.Jobs.Update(jobB, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	sched := newScheduler(app)
	sched.tickOnce()

	if got := len(app.Runs.runsForJob(jobB)); got != 0 {
		t.Fatalf("busy repo should skip scheduled start, got %d runs", got)
	}

	close(release)
	waitForStatus(t, app.Runs, runA.ID, StatusSuccess, 2*time.Second)
}

func TestSchedulerDoesNotRetryStorm(t *testing.T) {
	fake := &fakeRunner{
		installed: true,
		streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
			return 1, nil // failure
		},
	}
	app := newRunTestApp(t, fake)
	_, jobID := makeJob(t, app, "storm", "/data/c")

	job, _ := app.Jobs.Get(jobID)
	job.Schedule = &JobSchedule{Enabled: true, Kind: ScheduleHourly}
	if _, err := app.Jobs.Update(jobID, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	sched := newScheduler(app)
	base := time.Now()
	sched.now = func() time.Time { return base }

	sched.tickOnce()
	runs := app.Runs.runsForJob(jobID)
	if len(runs) != 1 {
		t.Fatalf("first tick should fire once, got %d", len(runs))
	}
	waitForStatus(t, app.Runs, runs[0].ID, StatusFailed, 2*time.Second)

	// Immediate re-tick must not fire again (retry wait is 1h for hourly = 1h).
	sched.tickOnce()
	if got := len(app.Runs.runsForJob(jobID)); got != 1 {
		t.Fatalf("retry storm: got %d runs after second tick", got)
	}

	// After 1h, may fire again.
	sched.now = func() time.Time { return base.Add(time.Hour + time.Minute) }
	sched.tickOnce()
	runs = app.Runs.runsForJob(jobID)
	if len(runs) != 2 {
		t.Fatalf("after retry wait want 2 runs, got %d", len(runs))
	}
	waitForStatus(t, app.Runs, runs[0].ID, StatusFailed, 2*time.Second)
}
