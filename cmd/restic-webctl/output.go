package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func (c *CLI) writeJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "error: encode json: %v\n", err)
		return exitError
	}
	return exitOK
}

func (c *CLI) writeJSONRaw(m map[string]any) int {
	return c.writeJSON(m)
}

func cliFail(cfg *config, err error) int {
	if err == nil {
		return exitOK
	}
	code := exitCodeOf(err)
	if cfg != nil && cfg.json {
		payload := map[string]any{"ok": false, "error": err.Error()}
		if ae, ok := err.(*apiError); ok {
			payload["code"] = ae.Code
			payload["error"] = ae.Message
			if ae.Message == "" {
				payload["error"] = ae.Error()
			}
			if ae.Body != nil {
				for _, k := range []string{"blockingRun", "repoName"} {
					if v, ok := ae.Body[k]; ok {
						payload[k] = v
					}
				}
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(payload)
		return code
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	return code
}

func (c *CLI) fail(err error) int {
	return cliFail(c.cfg, err)
}

func (c *CLI) okMessage(msg string, extra map[string]any) int {
	if c.cfg.json {
		m := map[string]any{"ok": true, "message": msg}
		for k, v := range extra {
			m[k] = v
		}
		return c.writeJSON(m)
	}
	if !c.cfg.quiet {
		fmt.Fprintln(os.Stdout, msg)
	}
	return exitOK
}

func (c *CLI) printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

func (c *CLI) note(format string, args ...any) {
	if c.cfg.quiet || c.cfg.json {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
