package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"restic-web/internal/core"
)

func (c *CLI) cmdRun(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpRun())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "list", "ls":
		return c.runList(args[1:])
	case "get", "show":
		if err := requireArgs(args[1:], 1, helpRun()); err != nil {
			return c.fail(err)
		}
		return c.runGet(args[1])
	case "log", "logs":
		if err := requireArgs(args[1:], 1, helpRun()); err != nil {
			return c.fail(err)
		}
		return c.runLog(args[1], args[2:])
	case "watch", "follow":
		if err := requireArgs(args[1:], 1, helpRun()); err != nil {
			return c.fail(err)
		}
		return c.runWatch(args[1])
	case "stop", "cancel":
		if err := requireArgs(args[1:], 1, helpRun()); err != nil {
			return c.fail(err)
		}
		return c.runStop(args[1])
	case "download":
		if err := requireArgs(args[1:], 1, helpRun()); err != nil {
			return c.fail(err)
		}
		return c.runDownload(args[1], args[2:])
	default:
		return c.fail(usagef("unknown run command %q\n\n%s", args[0], helpRun()))
	}
}

func (c *CLI) runList(args []string) int {
	fs := newFlagSet("run list")
	status := fs.String("status", "", "active|finished|exact status")
	kind := fs.String("kind", "", "backup|restore|init|download|retention")
	job := fs.String("job", "", "job id or name")
	limit := fs.Int("limit", 0, "max rows")
	if err := parseFlagSet(fs, args, helpRun()); err != nil {
		return c.fail(err)
	}
	jobID := ""
	if *job != "" {
		ref, err := c.resolveJob(*job)
		if err != nil {
			return c.fail(err)
		}
		jobID = ref.ID
	}
	runs, total := c.app.Runs.Query(*status, *kind, jobID, *limit, 0)
	return c.printRunList(runs, total)
}

func (c *CLI) runGet(id string) int {
	run, ok := c.app.Runs.Get(id)
	if !ok {
		return c.fail(&apiError{Code: "not_found", Message: "run not found"})
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "run": run})
	}
	fmt.Printf("id:\t%s\n", run.ID)
	fmt.Printf("kind:\t%s\n", run.Kind)
	fmt.Printf("status:\t%s\n", run.Status)
	fmt.Printf("job:\t%s\n", dash(run.JobName))
	fmt.Printf("repo:\t%s\n", dash(run.RepoName))
	fmt.Printf("folderPath:\t%s\n", dash(run.FolderPath))
	fmt.Printf("startedAt:\t%s\n", formatTime(run.StartedAt))
	if fa := formatTimePtr(run.FinishedAt); fa != "" {
		fmt.Printf("finishedAt:\t%s\n", fa)
	}
	if p := summarizeProgress(run); p != "" {
		fmt.Printf("progress:\t%s\n", p)
	}
	if run.Error != "" {
		fmt.Printf("error:\t%s\n", run.Error)
	}
	if run.Summary != nil {
		if run.Summary.SnapshotID != "" {
			fmt.Printf("snapshotId:\t%s\n", run.Summary.SnapshotID)
		}
		fmt.Printf("dataAdded:\t%d\n", run.Summary.DataAdded)
	}
	return runStatusExit(run.Status)
}

func (c *CLI) runLog(id string, args []string) int {
	fs := newFlagSet("run log")
	after := fs.Int64("after", 0, "only lines after this seq")
	follow := fs.Bool("follow", false, "follow until run ends")
	if err := parseFlagSet(fs, args, helpRun()); err != nil {
		return c.fail(err)
	}
	if *follow {
		return c.runFollowLog(id, *after)
	}
	lines, err := c.app.Runs.ReadLog(id, *after)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "lines": lines})
	}
	for _, line := range lines {
		if line.Level != "" && line.Level != "info" {
			fmt.Printf("[%s] %s\n", line.Level, line.Message)
		} else {
			fmt.Println(line.Message)
		}
	}
	return exitOK
}

func (c *CLI) runFollowLog(id string, after int64) int {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	cur := after
	for {
		if err := c.printNewLogs(id, &cur); err != nil {
			return c.fail(err)
		}
		run, ok := c.app.Runs.Get(id)
		if !ok {
			return c.fail(&apiError{Code: "not_found", Message: "run not found"})
		}
		if isTerminalStatus(run.Status) {
			_ = c.printNewLogs(id, &cur)
			if c.cfg.json {
				_ = jsonStdout(map[string]any{"type": "done", "run": run})
			}
			return runStatusExit(run.Status)
		}
		<-ticker.C
	}
}

func (c *CLI) runWatch(id string) int {
	final, err := c.waitForRun(id, true)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "run": final})
	}
	fmt.Printf("\nfinished: %s\n", final.Status)
	return runStatusExit(final.Status)
}

func (c *CLI) runStop(id string) int {
	err := c.app.Coord.Stop(id)
	if err == nil {
		return c.okMessage("Stopping…", map[string]any{"id": id})
	}
	if errors.Is(err, core.ErrRunNotActive) {
		if _, ok := c.app.Runs.Get(id); ok {
			return c.fail(&apiError{Code: "not_active", Message: "This run has already finished."})
		}
		return c.fail(&apiError{Code: "not_found", Message: "run not found"})
	}
	return c.fail(err)
}

func (c *CLI) runDownload(id string, args []string) int {
	fs := newFlagSet("run download")
	out := fs.String("o", "", "output zip path")
	outLong := fs.String("output", "", "output zip path")
	if err := parseFlagSet(fs, args, helpRun()); err != nil {
		return c.fail(err)
	}
	path := *out
	if path == "" {
		path = *outLong
	}
	if path == "" {
		path = id + ".zip"
	}

	run, ok := c.app.Runs.Get(id)
	if !ok {
		return c.fail(&apiError{Code: "not_found", Message: "run not found"})
	}
	if run.Kind != core.KindDownload {
		return c.fail(&core.ValidationError{Msg: "this run is not a download"})
	}
	if run.Status != core.StatusSuccess {
		return c.fail(&core.ConflictError{Msg: "the download is not ready"})
	}
	target := run.Params["target"]
	if target == "" {
		return c.fail(&apiError{Code: "not_found", Message: "download workspace is missing"})
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		return c.fail(&apiError{Code: "not_found", Message: "the download is no longer available; run it again"})
	}

	var w io.Writer
	var closer io.Closer
	if path == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(path)
		if err != nil {
			return c.fail(err)
		}
		closer = f
		w = f
	}
	if err := core.ZipDir(w, target); err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return c.fail(err)
	}
	if closer != nil {
		_ = closer.Close()
	}
	if path == "-" {
		return exitOK
	}
	return c.okMessage(fmt.Sprintf("Wrote %s", path), map[string]any{"path": path, "runId": id})
}
