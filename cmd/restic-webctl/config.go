package main

import (
	"os"
	"strings"
)

// config holds global CLI settings shared by every command.
type config struct {
	database string
	dataDir  string
	json     bool
	quiet    bool
	help     bool
}

func parseGlobal(args []string) (*config, []string, error) {
	cfg := &config{
		database: strings.TrimSpace(os.Getenv("DATABASE_URL")),
		dataDir:  defaultDataDir(),
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
			return nil, nil, usagef("unknown global flag %q (place --json/--database/--data/--quiet anywhere)", a)
		}
	}
	return cfg, args[i:], nil
}

// extractRelocatableGlobals removes --json/--database/--data/--quiet/--help
// from anywhere in args (except after `--`) and applies them to cfg.
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
		}
		if strings.HasPrefix(a, "--database=") {
			cfg.database = strings.TrimPrefix(a, "--database=")
			continue
		}
		if strings.HasPrefix(a, "--data=") {
			cfg.dataDir = strings.TrimPrefix(a, "--data=")
			continue
		}
		out = append(out, a)
	}
	return out, nil
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
