package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"restic-web/internal/core"
)

func activeRunsOrEmpty(runs []*core.Run) []*core.Run {
	if runs == nil {
		return []*core.Run{}
	}
	return runs
}

func (c *CLI) cmdStatus(args []string) int {
	if wantHelp(args) {
		fmt.Fprint(os.Stdout, helpStatus())
		return exitOK
	}

	installed := c.app.Runner.Available()
	resp := map[string]any{
		"ok":              true,
		"resticInstalled": installed,
		"counts": map[string]int{
			"repositories": c.app.Repos.Count(),
			"folders":      c.app.Folders.Count(),
			"jobs":         c.app.Jobs.Count(),
		},
		"activeRuns": activeRunsOrEmpty(c.app.Runs.ActiveRuns()),
	}
	var version string
	if installed {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if v, err := c.app.Runner.Version(ctx); err == nil {
			version = v
			resp["resticVersion"] = v
		}
	}

	if c.cfg.json {
		return c.writeJSON(resp)
	}

	fmt.Printf("resticInstalled:\t%v\n", installed)
	if version != "" {
		fmt.Printf("resticVersion:\t%s\n", version)
	}
	fmt.Printf("repositories:\t%d\n", c.app.Repos.Count())
	fmt.Printf("folders:\t%d\n", c.app.Folders.Count())
	fmt.Printf("jobs:\t%d\n", c.app.Jobs.Count())

	active := c.app.Runs.ActiveRuns()
	fmt.Printf("activeRuns:\t%d\n", len(active))
	if len(active) > 0 {
		fmt.Println()
		rows := make([][]string, 0, len(active))
		for _, r := range active {
			rows = append(rows, []string{
				shortID(r.ID),
				string(r.Kind),
				string(r.Status),
				dash(r.JobName),
				dash(r.RepoName),
			})
		}
		c.printTable([]string{"RUN", "KIND", "STATUS", "JOB", "REPO"}, rows)
	}
	return exitOK
}
