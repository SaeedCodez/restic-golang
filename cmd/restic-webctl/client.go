package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

const sessionCookieName = "restic_session"

// CLI is the shared runtime for every command.
type CLI struct {
	cfg    *config
	http   *http.Client
	base   *url.URL
	jar    http.CookieJar
	authed bool
}

func newCLI(cfg *config) (*CLI, error) {
	u, err := url.Parse(cfg.url)
	if err != nil {
		return nil, fmt.Errorf("invalid --url: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &CLI{
		cfg:  cfg,
		base: u,
		jar:  jar,
		http: &http.Client{
			Timeout: cfg.timeout,
			Jar:     jar,
		},
	}
	if tok, err := readStringFile(cfg.sessionFile); err == nil && tok != "" {
		c.setSessionCookie(tok)
		c.authed = true
	}
	return c, nil
}

func (c *CLI) close() {}

func (c *CLI) setSessionCookie(token string) {
	c.jar.SetCookies(c.base, []*http.Cookie{{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	}})
}

func (c *CLI) saveSessionFromJar() error {
	for _, ck := range c.jar.Cookies(c.base) {
		if ck.Name == sessionCookieName && ck.Value != "" {
			return writeStringFile(c.cfg.sessionFile, ck.Value, 0o600)
		}
	}
	return nil
}

func (c *CLI) clearSession() {
	c.setSessionCookie("")
	_ = os.Remove(c.cfg.sessionFile)
	c.authed = false
}

func (c *CLI) resolve(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	rel, err := url.Parse(path)
	if err != nil {
		return c.cfg.url + path
	}
	return c.base.ResolveReference(rel).String()
}

// doJSON performs an HTTP JSON call. On 401 it auto-logins once when a password
// is available, then retries.
func (c *CLI) doJSON(method, path string, body any) (int, map[string]any, error) {
	status, m, err := c.doJSONOnce(method, path, body)
	if err != nil {
		return status, m, err
	}
	if status == http.StatusUnauthorized || codeOf(m) == "unauthorized" || codeOf(m) == "setup_required" {
		if codeOf(m) == "setup_required" {
			return status, m, &apiError{Status: status, Code: "setup_required", Message: messageOf(m), Body: m}
		}
		if c.cfg.password == "" {
			return status, m, &apiError{Status: status, Code: "unauthorized", Message: messageOf(m), Body: m}
		}
		if err := c.login(c.cfg.password); err != nil {
			return 0, nil, err
		}
		return c.doJSONOnce(method, path, body)
	}
	return status, m, nil
}

func (c *CLI) doJSONOnce(method, path string, body any) (int, map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	} else if method == http.MethodPost || method == http.MethodPut {
		rdr = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequest(method, c.resolve(path), rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil || method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("could not reach %s: %w", c.cfg.url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	m := map[string]any{}
	ct := resp.Header.Get("Content-Type")
	trim := bytes.TrimSpace(raw)
	looksJSON := len(trim) > 0 && (trim[0] == '{' || trim[0] == '[')
	if len(raw) > 0 && (strings.Contains(ct, "application/json") || looksJSON) {
		if err := json.Unmarshal(raw, &m); err != nil {
			return resp.StatusCode, nil, fmt.Errorf("invalid JSON from server: %w", err)
		}
	} else if len(raw) > 0 {
		m["error"] = strings.TrimSpace(string(raw))
	}
	return resp.StatusCode, m, nil
}

// doDownload streams a binary response to w (or a file).
func (c *CLI) doDownload(path, outPath string) error {
	status, m, err := c.doJSON(http.MethodGet, "/api/status", nil) // ensure auth warm
	if err != nil {
		return err
	}
	_ = status
	_ = m

	client := &http.Client{
		Timeout: 0, // large downloads
		Jar:     c.jar,
	}
	req, err := http.NewRequest(http.MethodGet, c.resolve(path), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		if c.cfg.password == "" {
			return &apiError{Status: 401, Code: "unauthorized", Message: "Please log in."}
		}
		if err := c.login(c.cfg.password); err != nil {
			return err
		}
		resp.Body.Close()
		req, _ = http.NewRequest(http.MethodGet, c.resolve(path), nil)
		resp, err = client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		m := map[string]any{}
		_ = json.Unmarshal(raw, &m)
		return &apiError{Status: resp.StatusCode, Code: codeOf(m), Message: messageOf(m), Body: m}
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func (c *CLI) login(password string) error {
	status, m, err := c.doJSONOnce(http.MethodPost, "/api/auth/login", map[string]string{"password": password})
	if err != nil {
		return err
	}
	if status >= 400 || !truthy(m["ok"]) {
		return &apiError{Status: status, Code: codeOf(m), Message: messageOf(m), Body: m}
	}
	if err := c.saveSessionFromJar(); err != nil {
		return fmt.Errorf("login ok but could not save session file: %w", err)
	}
	c.authed = true
	return nil
}

func (c *CLI) setup(password string) error {
	status, m, err := c.doJSONOnce(http.MethodPost, "/api/auth/setup", map[string]string{"password": password})
	if err != nil {
		return err
	}
	if status >= 400 || !truthy(m["ok"]) {
		return &apiError{Status: status, Code: codeOf(m), Message: messageOf(m), Body: m}
	}
	if err := c.saveSessionFromJar(); err != nil {
		return fmt.Errorf("setup ok but could not save session file: %w", err)
	}
	c.authed = true
	return nil
}

func (c *CLI) requireOK(status int, m map[string]any) error {
	if status >= 400 || (m != nil && m["ok"] == false) {
		return &apiError{Status: status, Code: codeOf(m), Message: messageOf(m), Body: m}
	}
	return nil
}

func codeOf(m map[string]any) string {
	if m == nil {
		return ""
	}
	if v, ok := m["code"].(string); ok {
		return v
	}
	return ""
}

func messageOf(m map[string]any) string {
	if m == nil {
		return ""
	}
	for _, k := range []string{"error", "message"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

func floatField(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func intField(m map[string]any, key string) int {
	return int(floatField(m, key))
}

func boolField(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

func timeField(m map[string]any, key string) string {
	s := strField(m, key)
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
