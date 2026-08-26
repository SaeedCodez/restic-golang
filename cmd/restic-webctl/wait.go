package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

func isTerminalStatus(status string) bool {
	switch status {
	case "success", "success_warnings", "failed", "canceled", "interrupted":
		return true
	default:
		return false
	}
}

// waitForRun polls run status (and optionally logs) until terminal.
// When followLogs is true, new log lines are printed as they arrive.
func (c *CLI) waitForRun(runID string, followLogs bool) (map[string]any, error) {
	after := int64(0)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		status, m, err := c.doJSON(http.MethodGet, "/api/runs/"+runID, nil)
		if err != nil {
			return nil, err
		}
		if err := c.requireOK(status, m); err != nil {
			return nil, err
		}
		run := asMap(m["run"])
		st := strField(run, "status")

		if followLogs {
			if err := c.printNewLogs(runID, &after); err != nil {
				return nil, err
			}
		} else if !c.cfg.json && !c.cfg.quiet {
			prog := asMap(run["progress"])
			pct := floatField(prog, "percent")
			fmt.Fprintf(os.Stderr, "\r%s %s %.0f%%   ", shortID(runID), st, pct)
		}

		if isTerminalStatus(st) {
			if !c.cfg.json && !c.cfg.quiet && !followLogs {
				fmt.Fprintln(os.Stderr)
			}
			if followLogs {
				_ = c.printNewLogs(runID, &after)
			}
			return run, nil
		}

		<-ticker.C
	}
}

func (c *CLI) printNewLogs(runID string, after *int64) error {
	status, m, err := c.doJSON(http.MethodGet, fmt.Sprintf("/api/runs/%s/log?after=%d", runID, *after), nil)
	if err != nil {
		return err
	}
	if err := c.requireOK(status, m); err != nil {
		return err
	}
	for _, item := range asSlice(m["lines"]) {
		line := asMap(item)
		seq := int64(floatField(line, "seq"))
		if seq > *after {
			*after = seq
		}
		if c.cfg.json {
			encLine := map[string]any{"type": "log", "line": line}
			_ = jsonStdout(encLine)
			continue
		}
		level := strField(line, "level")
		msg := strField(line, "message")
		if level != "" && level != "info" {
			fmt.Printf("[%s] %s\n", level, msg)
		} else {
			fmt.Println(msg)
		}
	}
	return nil
}

func jsonStdout(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

func (c *CLI) startAndMaybeWait(status int, m map[string]any, wait, follow bool) int {
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	runID := strField(m, "runId")
	if runID == "" {
		if run := asMap(m["run"]); run != nil {
			runID = strField(run, "id")
		}
	}

	if !wait && !follow {
		if c.cfg.json {
			return c.writeJSONRaw(m)
		}
		c.note("Started run %s", runID)
		fmt.Println(runID)
		return exitOK
	}

	if c.cfg.json && !follow {
		c.note("waiting for %s…", runID)
	}

	final, err := c.waitForRun(runID, follow)
	if err != nil {
		return c.fail(err)
	}
	st := strField(final, "status")
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": isTerminalStatus(st) && (st == "success" || st == "success_warnings"), "runId": runID, "run": final})
	}
	fmt.Printf("run %s finished: %s\n", runID, st)
	if errMsg := strField(final, "error"); errMsg != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", errMsg)
	}
	switch st {
	case "success", "success_warnings":
		return exitOK
	case "canceled", "interrupted":
		return exitConflict
	default:
		return exitError
	}
}

func runStatusExit(st string) int {
	switch st {
	case "success", "success_warnings":
		return exitOK
	case "canceled", "interrupted":
		return exitConflict
	case "failed":
		return exitError
	default:
		return exitOK
	}
}

func parseWaitFollow(args []string) (rest []string, wait, follow bool) {
	rest = args
	rest, wait = hasBoolFlag(rest, "--wait")
	rest, follow = hasBoolFlag(rest, "--follow")
	if follow {
		wait = true
	}
	return rest, wait, follow
}

func summarizeProgress(run map[string]any) string {
	prog := asMap(run["progress"])
	if prog == nil {
		return ""
	}
	pct := floatField(prog, "percent")
	cur := strField(prog, "currentFile")
	if cur != "" {
		return fmt.Sprintf("%.0f%% %s", pct, cur)
	}
	return fmt.Sprintf("%.0f%%", pct)
}


