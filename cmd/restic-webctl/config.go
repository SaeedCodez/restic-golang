package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// config holds global CLI settings shared by every command.
type config struct {
	url         string
	password    string
	sessionFile string
	json        bool
	quiet       bool
	timeout     time.Duration
	help        bool
}

func parseGlobal(args []string) (*config, []string, error) {
	cfg := &config{
		url:         defaultURL(),
		password:    strings.TrimSpace(os.Getenv("RESTIC_WEB_PASSWORD")),
		sessionFile: defaultSessionFile(),
		timeout:     30 * time.Second,
	}
	if v := strings.TrimSpace(os.Getenv("RESTIC_WEB_URL")); v != "" {
		cfg.url = v
	}
	if v := strings.TrimSpace(os.Getenv("RESTIC_WEB_SESSION_FILE")); v != "" {
		cfg.sessionFile = v
	}

	// Pull relocatable globals from anywhere (--json/--url/…).
	// --password stays prefix-only so it does not clash with `repo create --password`.
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
		case "--password":
			if i+1 >= len(args) {
				return nil, nil, usagef("--password requires a value")
			}
			cfg.password = args[i+1]
			i += 2
		default:
			if strings.HasPrefix(a, "--password=") {
				cfg.password = strings.TrimPrefix(a, "--password=")
				i++
				continue
			}
			return nil, nil, usagef("unknown global flag %q (place --json/--url anywhere; --password before the command)", a)
		}
	}
	cfg.url = strings.TrimRight(cfg.url, "/")
	if cfg.url == "" {
		return nil, nil, usagef("URL must not be empty")
	}
	return cfg, args[i:], nil
}

// extractRelocatableGlobals removes --json/--url/--timeout/--session-file/--quiet/--help
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
		case "--url":
			if i+1 >= len(args) {
				return nil, usagef("--url requires a value")
			}
			cfg.url = strings.TrimRight(args[i+1], "/")
			i++
			continue
		case "--session-file":
			if i+1 >= len(args) {
				return nil, usagef("--session-file requires a value")
			}
			cfg.sessionFile = args[i+1]
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
		if strings.HasPrefix(a, "--url=") {
			cfg.url = strings.TrimRight(strings.TrimPrefix(a, "--url="), "/")
			continue
		}
		if strings.HasPrefix(a, "--session-file=") {
			cfg.sessionFile = strings.TrimPrefix(a, "--session-file=")
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

func parseTimeout(raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		if n, err2 := strconv.Atoi(raw); err2 == nil {
			return time.Duration(n) * time.Second, nil
		}
		return 0, usagef("invalid --timeout: %v", err)
	}
	return d, nil
}

func defaultURL() string {
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		return "http://127.0.0.1:" + p
	}
	return "http://127.0.0.1:8080"
}

func defaultSessionFile() string {
	// Prefer the Docker data volume when present and writable.
	if st, err := os.Stat("/app/data"); err == nil && st.IsDir() {
		return "/app/data/.restic-webctl-session"
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "restic-web", "session")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".restic-webctl-session"
	}
	return filepath.Join(home, ".config", "restic-web", "session")
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

func writeStringFile(path, content string, mode os.FileMode) error {
	if err := ensureParentDir(path); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readStringFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func requireArgs(args []string, n int, usage string) error {
	if len(args) < n {
		return usagef("missing arguments\n\n%s", usage)
	}
	return nil
}

func peekFlag(args []string, name string) (string, []string, bool) {
	out := make([]string, 0, len(args))
	var val string
	found := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == name {
			if i+1 >= len(args) {
				return "", args, false
			}
			val = args[i+1]
			found = true
			i++
			continue
		}
		if strings.HasPrefix(a, name+"=") {
			val = strings.TrimPrefix(a, name+"=")
			found = true
			continue
		}
		out = append(out, a)
	}
	return val, out, found
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

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return d.String()
	}
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	s = s % 60
	if m < 60 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh%dm%ds", h, m, s)
}
