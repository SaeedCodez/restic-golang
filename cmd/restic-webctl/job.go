package main

import (
	"fmt"
	"os"
	"strconv"

	"restic-web/internal/core"
)

func (c *CLI) cmdJob(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpJob())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "list", "ls":
		return c.jobList()
	case "get", "show":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		return c.jobGet(args[1])
	case "create", "add":
		return c.jobCreate(args[1:])
	case "update", "edit":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		return c.jobUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		return c.jobDelete(args[1], args[2:])
	case "run", "backup":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		_, wait, follow := parseWaitFollow(args[2:])
		return c.jobRun(args[1], wait, follow)
	case "retention":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		_, wait, follow := parseWaitFollow(args[2:])
		return c.jobRetention(args[1], wait, follow)
	case "forget":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		return c.jobForget(args[1], args[2:])
	case "runs", "history":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		return c.jobRuns(args[1], args[2:])
	case "snapshots", "snaps":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		return c.jobSnapshots(args[1])
	default:
		return c.fail(usagef("unknown job command %q\n\n%s", args[0], helpJob()))
	}
}

func (c *CLI) jobList() int {
	jobs := c.app.Jobs.List()
	views := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		views = append(views, c.viewOf(j))
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "jobs": views})
	}
	rows := make([][]string, 0, len(views))
	for _, it := range views {
		last := "-"
		if it.LastRun != nil {
			last = string(it.LastRun.Status)
		}
		rows = append(rows, []string{
			it.ID,
			it.Name,
			dash(it.FolderName),
			dash(it.RepoName),
			dash(it.ScheduleState),
			last,
			strconv.Itoa(it.RunCount),
		})
	}
	c.printTable([]string{"ID", "NAME", "FOLDER", "REPO", "SCHEDULE", "LAST", "RUNS"}, rows)
	return exitOK
}

func (c *CLI) jobGet(query string) int {
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	v := c.viewOf(job)
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "job": v})
	}
	fmt.Printf("id:\t%s\n", v.ID)
	fmt.Printf("name:\t%s\n", v.Name)
	fmt.Printf("folder:\t%s (%s)\n", dash(v.FolderName), v.FolderID)
	fmt.Printf("path:\t%s\n", dash(v.FolderPath))
	fmt.Printf("repo:\t%s (%s)\n", dash(v.RepoName), v.RepositoryID)
	fmt.Printf("tag:\t%s\n", dash(v.Tag))
	fmt.Printf("scheduleState:\t%s\n", dash(v.ScheduleState))
	fmt.Printf("runCount:\t%d\n", v.RunCount)
	if v.Schedule != nil {
		fmt.Printf("schedule:\tenabled=%v kind=%s\n", v.Schedule.Enabled, v.Schedule.Kind)
	}
	if v.Retention != nil {
		fmt.Printf("retention:\tenabled=%v preset=%s\n", v.Retention.Enabled, dash(v.Retention.Preset))
	}
	if v.LastRun != nil {
		fmt.Printf("lastRun:\t%s %s\n", shortID(v.LastRun.ID), v.LastRun.Status)
	}
	return exitOK
}

type jobFlags struct {
	name              string
	folder            string
	repo              string
	scheduleEnabled   bool
	scheduleDisabled  bool
	scheduleKind      string
	every             string
	at                string
	weekdays          string
	retentionEnabled  bool
	retentionDisabled bool
	retentionPreset   string
	keepLast          int
	keepHourly        int
	keepDaily         int
	keepWeekly        int
	keepMonthly       int
	keepWithinDays    int
	setSchedule       bool
	setRetention      bool
}

func parseJobFlags(name string, args []string) (*jobFlags, []string, error) {
	fs := newFlagSet(name)
	f := &jobFlags{}
	fs.StringVar(&f.name, "name", "", "job name")
	fs.StringVar(&f.folder, "folder", "", "folder id or name")
	fs.StringVar(&f.repo, "repo", "", "repository id or name")
	fs.StringVar(&f.repo, "repository", "", "alias for --repo")
	fs.BoolVar(&f.scheduleEnabled, "schedule-enabled", false, "enable schedule")
	fs.BoolVar(&f.scheduleDisabled, "schedule-disabled", false, "disable schedule")
	fs.StringVar(&f.scheduleKind, "schedule-kind", "", "hourly|every|daily|weekly")
	fs.StringVar(&f.every, "every", "", "duration for kind=every")
	fs.StringVar(&f.at, "schedule-at", "", "HH:MM for daily/weekly")
	fs.StringVar(&f.weekdays, "weekdays", "", "comma weekdays 0=Sun")
	fs.BoolVar(&f.retentionEnabled, "retention-enabled", false, "enable retention")
	fs.BoolVar(&f.retentionDisabled, "retention-disabled", false, "disable retention")
	fs.StringVar(&f.retentionPreset, "retention-preset", "", "light|balanced|long|custom")
	fs.IntVar(&f.keepLast, "keep-last", -1, "keep last N")
	fs.IntVar(&f.keepHourly, "keep-hourly", -1, "keep hourly N")
	fs.IntVar(&f.keepDaily, "keep-daily", -1, "keep daily N")
	fs.IntVar(&f.keepWeekly, "keep-weekly", -1, "keep weekly N")
	fs.IntVar(&f.keepMonthly, "keep-monthly", -1, "keep monthly N")
	fs.IntVar(&f.keepWithinDays, "keep-within-days", -1, "keep within N days")
	if err := parseFlagSet(fs, args, helpJob()); err != nil {
		return nil, nil, err
	}
	f.setSchedule = f.scheduleEnabled || f.scheduleDisabled || f.scheduleKind != "" || f.every != "" || f.at != "" || f.weekdays != ""
	f.setRetention = f.retentionEnabled || f.retentionDisabled || f.retentionPreset != "" ||
		f.keepLast >= 0 || f.keepHourly >= 0 || f.keepDaily >= 0 || f.keepWeekly >= 0 || f.keepMonthly >= 0 || f.keepWithinDays >= 0
	return f, fs.Args(), nil
}

func (c *CLI) buildSchedule(f *jobFlags, existing *core.JobSchedule) (*core.JobSchedule, error) {
	if f.scheduleDisabled {
		return nil, nil
	}
	out := &core.JobSchedule{Enabled: true, Kind: "daily"}
	if existing != nil {
		cp := *existing
		out = &cp
	}
	if f.scheduleEnabled || existing == nil {
		out.Enabled = true
	}
	if f.scheduleKind != "" {
		out.Kind = f.scheduleKind
	}
	if out.Kind == "" {
		out.Kind = "daily"
	}
	if f.every != "" {
		out.Every = f.every
	}
	if f.at != "" {
		out.At = f.at
	}
	if f.weekdays != "" {
		days, err := parseCSVInts(f.weekdays)
		if err != nil {
			return nil, err
		}
		out.Weekdays = days
	}
	return out, nil
}

func (c *CLI) buildRetention(f *jobFlags, existing *core.JobRetention) *core.JobRetention {
	if f.retentionDisabled {
		return nil
	}
	out := &core.JobRetention{Enabled: true, Preset: "balanced"}
	if existing != nil {
		cp := *existing
		out = &cp
		out.Enabled = true
	}
	if f.retentionPreset != "" {
		out.Preset = f.retentionPreset
	} else if out.Preset == "" {
		out.Preset = "balanced"
	}
	setKeep := func(dst *int, n int) {
		if n >= 0 {
			*dst = n
		}
	}
	setKeep(&out.KeepLast, f.keepLast)
	setKeep(&out.KeepHourly, f.keepHourly)
	setKeep(&out.KeepDaily, f.keepDaily)
	setKeep(&out.KeepWeekly, f.keepWeekly)
	setKeep(&out.KeepMonthly, f.keepMonthly)
	setKeep(&out.KeepWithinDays, f.keepWithinDays)
	return out
}

func (c *CLI) validateJobRefs(job *core.Job) error {
	if err := job.Validate(); err != nil {
		return &core.ValidationError{Msg: err.Error()}
	}
	if !c.app.Folders.Exists(job.FolderID) {
		return &core.ValidationError{Msg: "the chosen backup folder does not exist"}
	}
	if !c.app.Repos.Exists(job.RepositoryID) {
		return &core.ValidationError{Msg: "the chosen storage repository does not exist"}
	}
	return nil
}

func (c *CLI) jobCreate(args []string) int {
	f, _, err := parseJobFlags("job create", args)
	if err != nil {
		return c.fail(err)
	}
	if f.name == "" || f.folder == "" || f.repo == "" {
		return c.fail(usagef("--name, --folder, and --repo are required\n\n%s", helpJob()))
	}
	folder, err := c.resolveFolder(f.folder)
	if err != nil {
		return c.fail(err)
	}
	repo, err := c.resolveRepo(f.repo)
	if err != nil {
		return c.fail(err)
	}
	job := core.Job{
		Meta:         core.Meta{Name: f.name},
		FolderID:     folder.ID,
		RepositoryID: repo.ID,
	}
	if f.setSchedule {
		sched, err := c.buildSchedule(f, nil)
		if err != nil {
			return c.fail(err)
		}
		job.Schedule = sched
	}
	if f.setRetention {
		job.Retention = c.buildRetention(f, nil)
	}
	if err := c.validateJobRefs(&job); err != nil {
		return c.fail(err)
	}
	created, err := c.app.Jobs.Create(job)
	if err != nil {
		return c.fail(err)
	}
	v := c.viewOf(created)
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "job": v})
	}
	c.note("Created job %s (%s)", created.Name, created.ID)
	fmt.Println(created.ID)
	return exitOK
}

func (c *CLI) jobUpdate(query string, args []string) int {
	existing, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	f, _, err := parseJobFlags("job update", args)
	if err != nil {
		return c.fail(err)
	}
	upd := existing
	if f.name != "" {
		upd.Name = f.name
	}
	if f.folder != "" {
		folder, err := c.resolveFolder(f.folder)
		if err != nil {
			return c.fail(err)
		}
		upd.FolderID = folder.ID
	}
	if f.repo != "" {
		repo, err := c.resolveRepo(f.repo)
		if err != nil {
			return c.fail(err)
		}
		upd.RepositoryID = repo.ID
	}
	if f.setSchedule {
		sched, err := c.buildSchedule(f, existing.Schedule)
		if err != nil {
			return c.fail(err)
		}
		upd.Schedule = sched
	}
	if f.setRetention {
		upd.Retention = c.buildRetention(f, existing.Retention)
	}
	if err := c.validateJobRefs(&upd); err != nil {
		return c.fail(err)
	}
	updated, err := c.app.Jobs.Update(existing.ID, upd)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "job": c.viewOf(updated)})
	}
	return c.okMessage("Job updated.", map[string]any{"id": existing.ID})
}

func (c *CLI) jobDelete(query string, args []string) int {
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("job delete")
	forget := fs.Bool("forget", false, "also forget this job's snapshots and prune unused data")
	if err := parseFlagSet(fs, args, helpJob()); err != nil {
		return c.fail(err)
	}
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	if *forget {
		run, err := c.app.Coord.StartForgetJob(job.ID, true)
		return c.startAndMaybeWait(run, err, wait, follow)
	}
	if run := c.app.JobActiveRun(job.ID); run != nil {
		return c.fail(&core.ConflictError{
			Msg: fmt.Sprintf("This job has a %s still running. Stop it or wait for it to finish.", run.Kind),
		})
	}
	if err := c.app.Jobs.Delete(job.ID); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted job %s.", job.Name), map[string]any{"id": job.ID})
}

func (c *CLI) jobForget(query string, args []string) int {
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("job forget")
	deleteJob := fs.Bool("delete-job", false, "delete the job after its snapshots are forgotten")
	if err := parseFlagSet(fs, args, helpJob()); err != nil {
		return c.fail(err)
	}
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	run, err := c.app.Coord.StartForgetJob(job.ID, *deleteJob)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) jobRun(query string, wait, follow bool) int {
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	run, err := c.app.Coord.StartBackup(job.ID)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) jobRetention(query string, wait, follow bool) int {
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	run, err := c.app.Coord.StartRetention(job.ID, core.TriggerManual)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) jobRuns(query string, args []string) int {
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("job runs")
	limit := fs.Int("limit", 0, "max rows")
	if err := parseFlagSet(fs, args, helpJob()); err != nil {
		return c.fail(err)
	}
	runs, total := c.app.Runs.Query("", "", job.ID, *limit, 0)
	return c.printRunList(runs, total)
}

func (c *CLI) jobSnapshots(query string) int {
	job, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	repo, ok := c.app.Repos.Get(job.RepositoryID)
	if !ok {
		return c.fail(&core.ValidationError{Msg: "this job's storage repository no longer exists"})
	}
	return c.printSnapshots(&repo, job.ResticTag())
}

func (c *CLI) printRunList(runs []*core.Run, total int) int {
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "runs": runs, "total": total})
	}
	rows := [][]string{}
	for _, r := range runs {
		rows = append(rows, []string{
			r.ID,
			string(r.Kind),
			string(r.Status),
			dash(r.JobName),
			dash(r.RepoName),
			formatTime(r.StartedAt),
			summarizeProgress(r),
		})
	}
	c.printTable([]string{"ID", "KIND", "STATUS", "JOB", "REPO", "STARTED", "PROGRESS"}, rows)
	if total > len(rows) {
		fmt.Printf("\nshowing %d of %d\n", len(rows), total)
	}
	return exitOK
}
