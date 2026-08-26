package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"restic-web/internal/core"
)

func isTerminalStatus(status core.RunStatus) bool {
	return status.Terminal()
}

// waitForRun polls run status (and optionally logs) until terminal.
func (c *CLI) waitForRun(runID string, followLogs bool) (*core.Run, error) {
	after := int64(0)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		run, ok := c.app.Runs.Get(runID)
		if !ok {
			return nil, &apiError{Code: "not_found", Message: "run not found"}
		}
		st := run.Status

		if followLogs {
			if err := c.printNewLogs(runID, &after); err != nil {
				return nil, err
			}
		} else if !c.cfg.json && !c.cfg.quiet {
			fmt.Fprintf(os.Stderr, "\r%s %s %.0f%%   ", shortID(runID), st, run.Progress.Percent)
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
	lines, err := c.app.Runs.ReadLog(runID, *after)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line.Seq > *after {
			*after = line.Seq
		}
		if c.cfg.json {
			_ = jsonStdout(map[string]any{"type": "log", "line": line})
			continue
		}
		if line.Level != "" && line.Level != "info" {
			fmt.Printf("[%s] %s\n", line.Level, line.Message)
		} else {
			fmt.Println(line.Message)
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

func (c *CLI) startAndMaybeWait(run *core.Run, err error, wait, follow bool) int {
	if err != nil {
		return c.fail(err)
	}
	runID := run.ID

	if !wait && !follow {
		if c.cfg.json {
			return c.writeJSON(map[string]any{"ok": true, "runId": runID, "run": run})
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
	st := final.Status
	if c.cfg.json {
		ok := st == core.StatusSuccess || st == core.StatusSuccessWarnings
		return c.writeJSON(map[string]any{"ok": ok, "runId": runID, "run": final})
	}
	fmt.Printf("run %s finished: %s\n", runID, st)
	if final.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", final.Error)
	}
	return runStatusExit(st)
}

func runStatusExit(st core.RunStatus) int {
	switch st {
	case core.StatusSuccess, core.StatusSuccessWarnings:
		return exitOK
	case core.StatusCanceled, core.StatusInterrupted:
		return exitConflict
	case core.StatusFailed:
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

func summarizeProgress(run *core.Run) string {
	if run == nil {
		return ""
	}
	pct := run.Progress.Percent
	cur := run.Progress.CurrentFile
	if cur != "" {
		return fmt.Sprintf("%.0f%% %s", pct, cur)
	}
	return fmt.Sprintf("%.0f%%", pct)
}
