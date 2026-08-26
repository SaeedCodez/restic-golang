package main

import (
	"fmt"
	"net/http"
	"os"
)

func (c *CLI) cmdStatus(args []string) int {
	if wantHelp(args) {
		fmt.Fprint(os.Stdout, helpStatus())
		return exitOK
	}
	status, m, err := c.doJSON(http.MethodGet, "/api/status", nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}

	fmt.Printf("resticInstalled:\t%v\n", boolField(m, "resticInstalled"))
	if v := strField(m, "resticVersion"); v != "" {
		fmt.Printf("resticVersion:\t%s\n", v)
	}
	counts := asMap(m["counts"])
	fmt.Printf("repositories:\t%d\n", intField(counts, "repositories"))
	fmt.Printf("folders:\t%d\n", intField(counts, "folders"))
	fmt.Printf("jobs:\t%d\n", intField(counts, "jobs"))

	active := asSlice(m["activeRuns"])
	fmt.Printf("activeRuns:\t%d\n", len(active))
	if len(active) > 0 {
		fmt.Println()
		rows := make([][]string, 0, len(active))
		for _, item := range active {
			r := asMap(item)
			rows = append(rows, []string{
				shortID(strField(r, "id")),
				strField(r, "kind"),
				strField(r, "status"),
				dash(strField(r, "jobName")),
				dash(strField(r, "repoName")),
			})
		}
		c.printTable([]string{"RUN", "KIND", "STATUS", "JOB", "REPO"}, rows)
	}
	return exitOK
}
