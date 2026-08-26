package core

import (
	"bufio"
	"os"
	"strings"
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
