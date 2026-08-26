package core

import (
	"errors"
	"log"
	"strings"
	"time"
)

// Scheduler fires enabled job schedules by calling Coordinator.StartBackup with
// trigger=schedule. It polls the wall clock so laptop sleep / clock skew still
// produce correct catch-up; a long monotonic timer would not.
type Scheduler struct {
	app  *App
	now  func() time.Time
	tick time.Duration

	stop chan struct{}
	done chan struct{}
}

func newScheduler(app *App) *Scheduler {
	return &Scheduler{
		app:  app,
		now:  time.Now,
		tick: 15 * time.Second,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// Start begins the poll loop in a background goroutine.
func (s *Scheduler) Start() {
	go s.loop()
}

// Stop ends the poll loop and waits for it to exit.
func (s *Scheduler) Stop() {
	select {
	case <-s.stop:
		// already stopped
	default:
		close(s.stop)
	}
	<-s.done
}

func (s *Scheduler) loop() {
	defer close(s.done)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	s.tickOnce()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tickOnce()
		}
	}
}

// tickOnce evaluates every enabled schedule once.
func (s *Scheduler) tickOnce() {
	if s.app == nil || s.app.Coord == nil || s.app.Runner == nil {
		return
	}
	if !s.app.Runner.Available() {
		return
	}
	now := s.now()
	if now.IsZero() {
		now = time.Now()
	}

	for _, job := range s.app.Jobs.List() {
		if job.Schedule == nil || !job.Schedule.Enabled {
			continue
		}
		if s.jobHasActiveBackup(job.ID) {
			continue
		}
		lastAttempt, lastOK, _ := s.app.Runs.backupTiming(job.ID)
		if !jobDue(job.Schedule, lastAttempt, lastOK, now) {
			continue
		}
		if _, err := s.app.Coord.StartBackupTriggered(job.ID, TriggerSchedule); err != nil {
			var busy *BusyError
			if errors.As(err, &busy) {
				continue // repository busy — try again next tick
			}
			log.Printf("scheduler: could not start backup for job %q: %v", job.Name, err)
		}
	}
}

func (s *Scheduler) jobHasActiveBackup(jobID string) bool {
	for _, run := range s.app.Runs.ActiveRuns() {
		if run.JobID == jobID && run.Kind == KindBackup {
			return true
		}
	}
	return false
}

// ---- due / next / overdue (pure; tested with a fake clock) ------------------

// schedulePeriod is the nominal interval between successful backups for health
// and interval cadence. Weekly uses 7 days.
func schedulePeriod(s *JobSchedule) time.Duration {
	if s == nil {
		return 0
	}
	switch s.Kind {
	case ScheduleHourly:
		return time.Hour
	case ScheduleEvery:
		d, err := time.ParseDuration(strings.TrimSpace(s.Every))
		if err != nil || d < time.Hour {
			return time.Hour
		}
		return d
	case ScheduleDaily:
		return 24 * time.Hour
	case ScheduleWeekly:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// retryWait after a failed attempt: min(period, 1h) so a daily job is not stuck
// waiting a full day after a transient error.
func retryWait(period time.Duration) time.Duration {
	if period <= 0 || period > time.Hour {
		return time.Hour
	}
	return period
}

// jobDue reports whether an enabled schedule should fire a backup at now.
// lastAttempt is the FinishedAt of the job's latest terminal backup (nil if
// never run). lastOK is true when that attempt succeeded (or succeeded with
// warnings).
func jobDue(sched *JobSchedule, lastAttempt *time.Time, lastOK bool, now time.Time) bool {
	next := nextDueAt(sched, lastAttempt, lastOK, now)
	return next != nil && !next.After(now)
}

// nextDueAt is when the schedule may next fire. Nil means the schedule is off.
func nextDueAt(sched *JobSchedule, lastAttempt *time.Time, lastOK bool, now time.Time) *time.Time {
	if sched == nil || !sched.Enabled {
		return nil
	}
	period := schedulePeriod(sched)
	if period <= 0 {
		return nil
	}

	switch sched.Kind {
	case ScheduleHourly, ScheduleEvery:
		if lastAttempt == nil {
			t := now
			return &t
		}
		wait := period
		if !lastOK {
			wait = retryWait(period)
		}
		t := lastAttempt.Add(wait)
		return &t

	case ScheduleDaily, ScheduleWeekly:
		if lastAttempt == nil {
			// Never run: wait for the next calendar slot (do not fire on enable).
			t := nextCalendarFire(sched, now)
			return &t
		}
		cal := nextCalendarFireAfter(sched, *lastAttempt)
		if !lastOK {
			retry := lastAttempt.Add(retryWait(period))
			if retry.Before(cal) {
				return &retry
			}
		}
		return &cal

	default:
		return nil
	}
}

// nextCalendarFire returns the next fire time >= from in the local zone.
func nextCalendarFire(sched *JobSchedule, from time.Time) time.Time {
	from = from.In(time.Local)
	hour, min, err := parseClock(sched.At)
	if err != nil {
		return from
	}
	base := time.Date(from.Year(), from.Month(), from.Day(), hour, min, 0, 0, time.Local)
	for i := 0; i < 8; i++ {
		candidate := base.AddDate(0, 0, i)
		if sched.Kind == ScheduleWeekly && !weekdayAllowed(sched.Weekdays, candidate.Weekday()) {
			continue
		}
		if !candidate.Before(from) {
			return candidate
		}
	}
	return base.AddDate(0, 0, 7)
}

// nextCalendarFireAfter returns the next fire time strictly after after.
func nextCalendarFireAfter(sched *JobSchedule, after time.Time) time.Time {
	return nextCalendarFire(sched, after.Add(time.Nanosecond))
}

func weekdayAllowed(days []int, wd time.Weekday) bool {
	d := int(wd) // Sunday=0
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

// scheduleOverdue reports whether the job's last successful backup is too old
// relative to its schedule. A never-succeeded job is overdue only once it is
// also due (so a daily job enabled at 3pm is not overdue until after 2am).
func scheduleOverdue(sched *JobSchedule, lastAttempt *time.Time, lastOK bool, lastSuccess *time.Time, running bool, now time.Time) bool {
	if sched == nil || !sched.Enabled || running {
		return false
	}
	period := schedulePeriod(sched)
	if period <= 0 {
		return false
	}
	if lastSuccess == nil {
		return jobDue(sched, lastAttempt, lastOK, now)
	}
	return !lastSuccess.Add(period).After(now)
}

// deriveScheduleState is the label the jobs API exposes.
func deriveScheduleState(sched *JobSchedule, lastAttempt *time.Time, lastOK bool, lastSuccess *time.Time, running bool, now time.Time) string {
	if sched == nil || !sched.Enabled {
		return ScheduleStateOff
	}
	if running {
		return ScheduleStateRunning
	}
	if scheduleOverdue(sched, lastAttempt, lastOK, lastSuccess, false, now) {
		return ScheduleStateOverdue
	}
	return ScheduleStateScheduled
}
