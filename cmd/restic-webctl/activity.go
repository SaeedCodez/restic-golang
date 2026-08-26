package main

import (
	"fmt"
	"os"
)

func (c *CLI) cmdActivity(args []string) int {
	if wantHelp(args) {
		fmt.Fprint(os.Stdout, helpActivity())
		return exitOK
	}
	fs := newFlagSet("activity")
	limit := fs.Int("limit", 20, "recent finished runs to show")
	if err := parseFlagSet(fs, args, helpActivity()); err != nil {
		return c.fail(err)
	}

	active, activeTotal := c.app.Runs.Query("active", "", "", 0)
	recent, recentTotal := c.app.Runs.Query("finished", "", "", *limit)

	if c.cfg.json {
		return c.writeJSON(map[string]any{
			"ok":          true,
			"active":      active,
			"recent":      recent,
			"activeTotal": activeTotal,
			"recentTotal": recentTotal,
		})
	}

	fmt.Println("Active runs")
	if len(active) == 0 {
		fmt.Println("  (none)")
	} else {
		rows := make([][]string, 0, len(active))
		for _, r := range active {
			rows = append(rows, []string{
				r.ID,
				string(r.Kind),
				string(r.Status),
				dash(r.JobName),
				summarizeProgress(r),
			})
		}
		c.printTable([]string{"ID", "KIND", "STATUS", "JOB", "PROGRESS"}, rows)
	}

	fmt.Println()
	fmt.Println("Recent history")
	if len(recent) == 0 {
		fmt.Println("  (none)")
	} else {
		rows := make([][]string, 0, len(recent))
		for _, r := range recent {
			rows = append(rows, []string{
				r.ID,
				string(r.Kind),
				string(r.Status),
				dash(r.JobName),
				formatTime(r.StartedAt),
			})
		}
		c.printTable([]string{"ID", "KIND", "STATUS", "JOB", "STARTED"}, rows)
	}
	return exitOK
}
