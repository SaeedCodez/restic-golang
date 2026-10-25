package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"restic-web/internal/core"
)

func isTerminalStatus(status core.RunStatus) bool {
	return status.Terminal()
}

// waitForRun polls run status (and optionally logs) until terminal.
//
// The run executes in this process, so being killed would abandon it midway:
// a database restore would not roll back and a stopped site would stay down.
// The first Ctrl-C (or SIGTERM/SIGHUP) therefore stops the run cleanly and
// keeps waiting for it to wind down; a second one exits at once.
func (c *CLI) waitForRun(runID string, followLogs bool) (*core.Run, error) {
	after := int64(0)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	stopping := false

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
			fmt.Fprintf(os.Stderr, "\r%s %s %.0f%%   ", shortID(runID), st, run.Progress.Percent*100)
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

		select {
		case <-ticker.C:
		case sig := <-sigs:
			if stopping {
				fmt.Fprintf(os.Stderr, "\n%v again: exiting without waiting for run %s\n", sig, shortID(runID))
				os.Exit(exitConflict)
			}
			stopping = true
			fmt.Fprintf(os.Stderr, "\n%v: stopping run %s and waiting for it to finish (again to exit now)\n", sig, shortID(runID))
			if err := c.app.Coord.Stop(runID); err != nil && !errors.Is(err, core.ErrRunNotActive) {
				fmt.Fprintf(os.Stderr, "could not stop the run: %v\n", err)
			}
		}
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

	// Runs execute inside this CLI process. Exiting before the run finishes
	// kills the restic child and leaves a durable "running"/"starting" ghost
	// that blocks the repository until the web server restarts and reconciles.
	// There is no detach mode yet, so always wait for completion.
	if !wait && !follow {
		wait = true
		c.note("waiting for %s (CLI always waits; exiting early would orphan the run)…", runID)
	}

	if c.cfg.json && !follow {
		c.note("waiting for %s…", runID)
	}

	final, err := c.waitForRun(runID, follow)
	if err != nil {
		return c.fail(err)
	}
	st := final.Status
	code := runStatusExit(st)
	if c.cfg.json {
		ok := st == core.StatusSuccess || st == core.StatusSuccessWarnings
		_ = c.writeJSON(map[string]any{"ok": ok, "runId": runID, "run": final})
		return code
	}
	fmt.Printf("run %s finished: %s\n", runID, st)
	if final.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", final.Error)
	}
	return code
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
	pct := run.Progress.Percent * 100 // restic reports a 0–1 fraction
	cur := run.Progress.CurrentFile
	if cur != "" {
		return fmt.Sprintf("%.0f%% %s", pct, cur)
	}
	return fmt.Sprintf("%.0f%%", pct)
}
