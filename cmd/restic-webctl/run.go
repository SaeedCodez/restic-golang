package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
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
	q := url.Values{}
	if *status != "" {
		q.Set("status", *status)
	}
	if *kind != "" {
		q.Set("kind", *kind)
	}
	if *limit > 0 {
		q.Set("limit", strconv.Itoa(*limit))
	}
	if *job != "" {
		ref, err := c.resolveJob(*job)
		if err != nil {
			return c.fail(err)
		}
		q.Set("jobId", ref.ID)
	}
	path := "/api/runs"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	st, m, err := c.doJSON(http.MethodGet, path, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(st, m); err != nil {
		return c.fail(err)
	}
	return c.printRunList(m)
}

func (c *CLI) runGet(id string) int {
	st, m, err := c.doJSON(http.MethodGet, "/api/runs/"+id, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(st, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	r := asMap(m["run"])
	fmt.Printf("id:\t%s\n", strField(r, "id"))
	fmt.Printf("kind:\t%s\n", strField(r, "kind"))
	fmt.Printf("status:\t%s\n", strField(r, "status"))
	fmt.Printf("job:\t%s\n", dash(strField(r, "jobName")))
	fmt.Printf("repo:\t%s\n", dash(strField(r, "repoName")))
	fmt.Printf("folderPath:\t%s\n", dash(strField(r, "folderPath")))
	fmt.Printf("startedAt:\t%s\n", timeField(r, "startedAt"))
	if fa := timeField(r, "finishedAt"); fa != "" {
		fmt.Printf("finishedAt:\t%s\n", fa)
	}
	if p := summarizeProgress(r); p != "" {
		fmt.Printf("progress:\t%s\n", p)
	}
	if errMsg := strField(r, "error"); errMsg != "" {
		fmt.Printf("error:\t%s\n", errMsg)
	}
	if sum := asMap(r["summary"]); sum != nil {
		if sid := strField(sum, "snapshotId"); sid != "" {
			fmt.Printf("snapshotId:\t%s\n", sid)
		}
		fmt.Printf("dataAdded:\t%.0f\n", floatField(sum, "dataAdded"))
	}
	return runStatusExit(strField(r, "status"))
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
	st, m, err := c.doJSON(http.MethodGet, fmt.Sprintf("/api/runs/%s/log?after=%d", id, *after), nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(st, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	for _, item := range asSlice(m["lines"]) {
		line := asMap(item)
		level := strField(line, "level")
		msg := strField(line, "message")
		if level != "" && level != "info" {
			fmt.Printf("[%s] %s\n", level, msg)
		} else {
			fmt.Println(msg)
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
		st, m, err := c.doJSON(http.MethodGet, "/api/runs/"+id, nil)
		if err != nil {
			return c.fail(err)
		}
		if err := c.requireOK(st, m); err != nil {
			return c.fail(err)
		}
		run := asMap(m["run"])
		status := strField(run, "status")
		if isTerminalStatus(status) {
			_ = c.printNewLogs(id, &cur)
			if c.cfg.json {
				_ = jsonStdout(map[string]any{"type": "done", "run": run})
			}
			return runStatusExit(status)
		}
		<-ticker.C
	}
}

func (c *CLI) runWatch(id string) int {
	if c.cfg.json {
		final, err := c.waitForRun(id, true)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(map[string]any{"ok": true, "run": final})
	}
	final, err := c.waitForRun(id, true)
	if err != nil {
		return c.fail(err)
	}
	fmt.Printf("\nfinished: %s\n", strField(final, "status"))
	return runStatusExit(strField(final, "status"))
}

func (c *CLI) runStop(id string) int {
	st, m, err := c.doJSON(http.MethodPost, "/api/runs/"+id+"/stop", map[string]any{})
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(st, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	return c.okMessage(dash(strField(m, "message")), map[string]any{"id": id})
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
	if err := c.doDownload("/api/runs/"+id+"/download", path); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Wrote %s", path), map[string]any{"path": path, "runId": id})
}
