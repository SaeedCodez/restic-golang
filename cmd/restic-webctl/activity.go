package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
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

	stActive, activeBody, err := c.doJSON(http.MethodGet, "/api/runs?status=active", nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(stActive, activeBody); err != nil {
		return c.fail(err)
	}

	stFin, finBody, err := c.doJSON(http.MethodGet, "/api/runs?status=finished&limit="+strconv.Itoa(*limit), nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(stFin, finBody); err != nil {
		return c.fail(err)
	}

	if c.cfg.json {
		return c.writeJSON(map[string]any{
			"ok":       true,
			"active":   asSlice(activeBody["runs"]),
			"recent":   asSlice(finBody["runs"]),
			"activeTotal": intField(activeBody, "total"),
			"recentTotal": intField(finBody, "total"),
		})
	}

	fmt.Println("Active runs")
	active := asSlice(activeBody["runs"])
	if len(active) == 0 {
		fmt.Println("  (none)")
	} else {
		rows := make([][]string, 0, len(active))
		for _, item := range active {
			r := asMap(item)
			rows = append(rows, []string{
				strField(r, "id"),
				strField(r, "kind"),
				strField(r, "status"),
				dash(strField(r, "jobName")),
				summarizeProgress(r),
			})
		}
		c.printTable([]string{"ID", "KIND", "STATUS", "JOB", "PROGRESS"}, rows)
	}

	fmt.Println()
	fmt.Println("Recent history")
	recent := asSlice(finBody["runs"])
	if len(recent) == 0 {
		fmt.Println("  (none)")
	} else {
		rows := make([][]string, 0, len(recent))
		for _, item := range recent {
			r := asMap(item)
			rows = append(rows, []string{
				strField(r, "id"),
				strField(r, "kind"),
				strField(r, "status"),
				dash(strField(r, "jobName")),
				timeField(r, "startedAt"),
			})
		}
		c.printTable([]string{"ID", "KIND", "STATUS", "JOB", "STARTED"}, rows)
	}
	return exitOK
}
