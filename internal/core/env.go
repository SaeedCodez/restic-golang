package core

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultListenAddr prefers ADDR, then PORT (as 0.0.0.0:PORT for containers),
// then the local-dev loopback default.
func DefaultListenAddr() string {
	if a := strings.TrimSpace(os.Getenv("ADDR")); a != "" {
		return a
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		return "0.0.0.0:" + p
	}
	return "127.0.0.1:8080"
}

// LoadDotEnv reads KEY=VALUE pairs from path into the process environment.
// Existing variables are left alone so a real shell export wins over .env.
// Missing files are ignored (return nil).
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		_ = os.Setenv(key, val)
	}
	return sc.Err()
}

// DefaultResticTimeout bounds the web API's synchronous restic calls (test
// connection, list snapshots, unlock). A slow S3 backend can take minutes to
// list snapshots, but it stays under the 300s write timeout that Coolify's
// Traefik proxy applies, so the app answers with a clear "timeout" instead of
// the proxy dropping the connection.
const DefaultResticTimeout = 4 * time.Minute

// ParseTimeout reads a timeout setting: a duration ("90s", "15m") or whole
// seconds ("600"). 0 means no limit.
func ParseTimeout(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	d, err := time.ParseDuration(v)
	if n, aerr := strconv.Atoi(v); aerr == nil {
		d, err = time.Duration(n)*time.Second, nil
	}
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid timeout %q: use a duration such as 90s or 15m, or 0 for no limit", v)
	}
	return d, nil
}
