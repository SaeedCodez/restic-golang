package main

import (
	"os"
	"strings"
	"time"

	"restic-web/internal/core"
)

// defaultTimeout bounds commands that wait on restic directly (repo test,
// repo unlock, repo/job snapshots). On a slow S3 backend these can legitimately
// take minutes; runs (backup, restore, check, …) are not bounded by it.
const defaultTimeout = 10 * time.Minute

// config holds global CLI settings shared by every command.
type config struct {
	database string
	dataDir  string
	json     bool
	quiet    bool
	help     bool
	timeout  time.Duration // 0 = no limit
}

func parseGlobal(args []string) (*config, []string, error) {
	cfg := &config{
		database: strings.TrimSpace(os.Getenv("DATABASE_URL")),
		dataDir:  defaultDataDir(),
		timeout:  defaultTimeout,
	}
	if v := strings.TrimSpace(os.Getenv("RESTIC_WEB_DATA")); v != "" {
		cfg.dataDir = v
	}

	var err error
	args, err = extractRelocatableGlobals(cfg, args)
	if err != nil {
		return nil, nil, err
	}

	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch a {
		case "-h", "--help":
			cfg.help = true
			i++
		default:
			return nil, nil, usagef("unknown global flag %q (place --json/--database/--data/--timeout/--quiet anywhere)", a)
		}
	}
	return cfg, args[i:], nil
}

// extractRelocatableGlobals removes --json/--database/--data/--timeout/--quiet/
// --help from anywhere in args (except after `--`) and applies them to cfg.
func extractRelocatableGlobals(cfg *config, args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		switch a {
		case "--json":
			cfg.json = true
			continue
		case "-q", "--quiet":
			cfg.quiet = true
			continue
		case "-h", "--help":
			cfg.help = true
			continue
		case "--database":
			if i+1 >= len(args) {
				return nil, usagef("--database requires a value")
			}
			cfg.database = args[i+1]
			i++
			continue
		case "--data":
			if i+1 >= len(args) {
				return nil, usagef("--data requires a value")
			}
			cfg.dataDir = args[i+1]
			i++
			continue
		case "--timeout":
			if i+1 >= len(args) {
				return nil, usagef("--timeout requires a value")
			}
			d, err := parseTimeout(args[i+1])
			if err != nil {
				return nil, err
			}
			cfg.timeout = d
			i++
			continue
		}
		if strings.HasPrefix(a, "--database=") {
			cfg.database = strings.TrimPrefix(a, "--database=")
			continue
		}
		if strings.HasPrefix(a, "--data=") {
			cfg.dataDir = strings.TrimPrefix(a, "--data=")
			continue
		}
		if strings.HasPrefix(a, "--timeout=") {
			d, err := parseTimeout(strings.TrimPrefix(a, "--timeout="))
			if err != nil {
				return nil, err
			}
			cfg.timeout = d
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// parseTimeout reads a --timeout value (see core.ParseTimeout).
func parseTimeout(v string) (time.Duration, error) {
	d, err := core.ParseTimeout(v)
	if err != nil {
		return 0, usagef("--timeout: %v", err)
	}
	return d, nil
}

func defaultDataDir() string {
	if st, err := os.Stat("/app/data"); err == nil && st.IsDir() {
		return "/app/data"
	}
	return "data"
}

func requireArgs(args []string, n int, usage string) error {
	if len(args) < n {
		return usagef("missing arguments\n\n%s", usage)
	}
	return nil
}

func hasBoolFlag(args []string, names ...string) ([]string, bool) {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	out := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		if set[a] {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}
