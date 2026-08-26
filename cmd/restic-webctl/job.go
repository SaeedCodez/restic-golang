package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
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
		return c.jobDelete(args[1])
	case "run", "backup":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		rest, wait, follow := parseWaitFollow(args[2:])
		_ = rest
		return c.jobRun(args[1], wait, follow)
	case "retention", "forget":
		if err := requireArgs(args[1:], 1, helpJob()); err != nil {
			return c.fail(err)
		}
		rest, wait, follow := parseWaitFollow(args[2:])
		_ = rest
		return c.jobRetention(args[1], wait, follow)
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
	items, err := c.listJobs()
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		rows := make([]any, 0, len(items))
		for _, it := range items {
			rows = append(rows, it.Raw)
		}
		return c.writeJSON(map[string]any{"ok": true, "jobs": rows})
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		last := "-"
		if lr := asMap(it.Raw["lastRun"]); lr != nil {
			last = strField(lr, "status")
		}
		rows = append(rows, []string{
			it.ID,
			it.Name,
			dash(strField(it.Raw, "folderName")),
			dash(strField(it.Raw, "repoName")),
			dash(strField(it.Raw, "scheduleState")),
			last,
			strconv.Itoa(intField(it.Raw, "runCount")),
		})
	}
	c.printTable([]string{"ID", "NAME", "FOLDER", "REPO", "SCHEDULE", "LAST", "RUNS"}, rows)
	return exitOK
}

func (c *CLI) jobGet(query string) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodGet, "/api/jobs/"+ref.ID, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	j := asMap(m["job"])
	fmt.Printf("id:\t%s\n", strField(j, "id"))
	fmt.Printf("name:\t%s\n", strField(j, "name"))
	fmt.Printf("folder:\t%s (%s)\n", dash(strField(j, "folderName")), strField(j, "folderId"))
	fmt.Printf("path:\t%s\n", dash(strField(j, "folderPath")))
	fmt.Printf("repo:\t%s (%s)\n", dash(strField(j, "repoName")), strField(j, "repositoryId"))
	fmt.Printf("tag:\t%s\n", dash(strField(j, "tag")))
	fmt.Printf("scheduleState:\t%s\n", dash(strField(j, "scheduleState")))
	if nd := timeField(j, "nextDueAt"); nd != "" {
		fmt.Printf("nextDueAt:\t%s\n", nd)
	}
	fmt.Printf("runCount:\t%d\n", intField(j, "runCount"))
	if sched := asMap(j["schedule"]); sched != nil {
		fmt.Printf("schedule:\tenabled=%v kind=%s\n", boolField(sched, "enabled"), strField(sched, "kind"))
	}
	if ret := asMap(j["retention"]); ret != nil {
		fmt.Printf("retention:\tenabled=%v preset=%s\n", boolField(ret, "enabled"), dash(strField(ret, "preset")))
	}
	if lr := asMap(j["lastRun"]); lr != nil {
		fmt.Printf("lastRun:\t%s %s\n", shortID(strField(lr, "id")), strField(lr, "status"))
	}
	return exitOK
}

type jobFlags struct {
	name               string
	folder             string
	repo               string
	scheduleEnabled    bool
	scheduleDisabled   bool
	scheduleKind       string
	every              string
	at                 string
	weekdays           string
	retentionEnabled   bool
	retentionDisabled  bool
	retentionPreset    string
	keepLast           int
	keepHourly         int
	keepDaily          int
	keepWeekly         int
	keepMonthly        int
	keepWithinDays     int
	setSchedule        bool
	setRetention       bool
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

func (c *CLI) buildSchedule(f *jobFlags, existing map[string]any) (map[string]any, error) {
	if f.scheduleDisabled {
		return map[string]any{"enabled": false, "kind": "daily"}, nil
	}
	out := map[string]any{}
	if existing != nil {
		for k, v := range existing {
			out[k] = v
		}
	}
	if f.scheduleEnabled || existing == nil {
		out["enabled"] = true
	}
	if f.scheduleEnabled {
		out["enabled"] = true
	}
	kind := f.scheduleKind
	if kind == "" {
		kind = strField(out, "kind")
	}
	if kind == "" {
		kind = "daily"
	}
	out["kind"] = kind
	if f.every != "" {
		out["every"] = f.every
	}
	if f.at != "" {
		out["at"] = f.at
	}
	if f.weekdays != "" {
		days, err := parseCSVInts(f.weekdays)
		if err != nil {
			return nil, err
		}
		out["weekdays"] = days
	}
	return out, nil
}

func (c *CLI) buildRetention(f *jobFlags, existing map[string]any) map[string]any {
	if f.retentionDisabled {
		return map[string]any{"enabled": false}
	}
	out := map[string]any{}
	if existing != nil {
		for k, v := range existing {
			out[k] = v
		}
	}
	out["enabled"] = true
	if f.retentionPreset != "" {
		out["preset"] = f.retentionPreset
	} else if strField(out, "preset") == "" {
		out["preset"] = "balanced"
	}
	setKeep := func(key string, n int) {
		if n >= 0 {
			out[key] = n
		}
	}
	setKeep("keepLast", f.keepLast)
	setKeep("keepHourly", f.keepHourly)
	setKeep("keepDaily", f.keepDaily)
	setKeep("keepWeekly", f.keepWeekly)
	setKeep("keepMonthly", f.keepMonthly)
	setKeep("keepWithinDays", f.keepWithinDays)
	return out
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
	body := map[string]any{
		"name":         f.name,
		"folderId":     folder.ID,
		"repositoryId": repo.ID,
	}
	if f.setSchedule {
		sched, err := c.buildSchedule(f, nil)
		if err != nil {
			return c.fail(err)
		}
		body["schedule"] = sched
	}
	if f.setRetention {
		body["retention"] = c.buildRetention(f, nil)
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/jobs", body)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	j := asMap(m["job"])
	c.note("Created job %s (%s)", strField(j, "name"), strField(j, "id"))
	fmt.Println(strField(j, "id"))
	return exitOK
}

func (c *CLI) jobUpdate(query string, args []string) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	f, _, err := parseJobFlags("job update", args)
	if err != nil {
		return c.fail(err)
	}
	body := map[string]any{
		"name":         ref.Name,
		"folderId":     strField(ref.Raw, "folderId"),
		"repositoryId": strField(ref.Raw, "repositoryId"),
	}
	if sched := asMap(ref.Raw["schedule"]); sched != nil {
		body["schedule"] = sched
	}
	if ret := asMap(ref.Raw["retention"]); ret != nil {
		body["retention"] = ret
	}
	if f.name != "" {
		body["name"] = f.name
	}
	if f.folder != "" {
		folder, err := c.resolveFolder(f.folder)
		if err != nil {
			return c.fail(err)
		}
		body["folderId"] = folder.ID
	}
	if f.repo != "" {
		repo, err := c.resolveRepo(f.repo)
		if err != nil {
			return c.fail(err)
		}
		body["repositoryId"] = repo.ID
	}
	if f.setSchedule {
		sched, err := c.buildSchedule(f, asMap(body["schedule"]))
		if err != nil {
			return c.fail(err)
		}
		if f.scheduleDisabled {
			body["schedule"] = nil
		} else {
			body["schedule"] = sched
		}
	}
	if f.setRetention {
		if f.retentionDisabled {
			body["retention"] = nil
		} else {
			body["retention"] = c.buildRetention(f, asMap(body["retention"]))
		}
	}
	status, m, err := c.doJSON(http.MethodPut, "/api/jobs/"+ref.ID, body)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	return c.okMessage("Job updated.", map[string]any{"id": ref.ID})
}

func (c *CLI) jobDelete(query string) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodDelete, "/api/jobs/"+ref.ID, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted job %s.", ref.Name), map[string]any{"id": ref.ID})
}

func (c *CLI) jobRun(query string, wait, follow bool) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/jobs/"+ref.ID+"/run", map[string]any{})
	if err != nil {
		return c.fail(err)
	}
	return c.startAndMaybeWait(status, m, wait, follow)
}

func (c *CLI) jobRetention(query string, wait, follow bool) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/jobs/"+ref.ID+"/retention", map[string]any{})
	if err != nil {
		return c.fail(err)
	}
	return c.startAndMaybeWait(status, m, wait, follow)
}

func (c *CLI) jobRuns(query string, args []string) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("job runs")
	limit := fs.Int("limit", 0, "max rows")
	if err := parseFlagSet(fs, args, helpJob()); err != nil {
		return c.fail(err)
	}
	path := "/api/jobs/" + ref.ID + "/runs"
	if *limit > 0 {
		path += "?limit=" + strconv.Itoa(*limit)
	}
	status, m, err := c.doJSON(http.MethodGet, path, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	return c.printRunList(m)
}

func (c *CLI) jobSnapshots(query string) int {
	ref, err := c.resolveJob(query)
	if err != nil {
		return c.fail(err)
	}
	return c.printSnapshots("/api/jobs/" + ref.ID + "/snapshots")
}

func (c *CLI) printRunList(m map[string]any) int {
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	rows := [][]string{}
	for _, item := range asSlice(m["runs"]) {
		r := asMap(item)
		rows = append(rows, []string{
			strField(r, "id"),
			strField(r, "kind"),
			strField(r, "status"),
			dash(strField(r, "jobName")),
			dash(strField(r, "repoName")),
			timeField(r, "startedAt"),
			summarizeProgress(r),
		})
	}
	total := intField(m, "total")
	c.printTable([]string{"ID", "KIND", "STATUS", "JOB", "REPO", "STARTED", "PROGRESS"}, rows)
	if total > len(rows) {
		fmt.Printf("\nshowing %d of %d\n", len(rows), total)
	}
	return exitOK
}
