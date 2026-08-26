package main

import (
	"fmt"
	"strings"
)

func (c *CLI) dispatch(args []string) int {
	group := args[0]
	rest := args[1:]
	if group == "help" || group == "-h" || group == "--help" {
		fmt.Print(fullUsage())
		return exitOK
	}

	switch group {
	case "auth":
		return c.cmdAuth(rest)
	case "status":
		return c.cmdStatus(rest)
	case "activity":
		return c.cmdActivity(rest)
	case "folder", "folders":
		return c.cmdFolder(rest)
	case "repo", "repos", "repository", "repositories":
		return c.cmdRepo(rest)
	case "job", "jobs":
		return c.cmdJob(rest)
	case "run", "runs":
		return c.cmdRun(rest)
	default:
		return c.fail(usagef("unknown command %q\n\n%s", group, shortUsage()))
	}
}

func wantHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func stripHelp(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "-h" || a == "--help" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func splitFlags(args []string) (positionals []string, flags []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// value-taking flags: if next token doesn't look like a flag, keep it with flags
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Heuristic: known value flags only — handled per-command via flag.FlagSet.
				// Here we just separate for simple bool detection; commands re-parse.
			}
			continue
		}
		positionals = append(positionals, a)
	}
	return positionals, flags
}
