package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExitCodeOf(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, exitOK},
		{&apiError{Code: "unauthorized"}, exitAuth},
		{&apiError{Code: "setup_required"}, exitAuth},
		{&apiError{Code: "not_found"}, exitNotFound},
		{&apiError{Code: "busy"}, exitConflict},
		{&apiError{Code: "conflict"}, exitConflict},
		{&apiError{Status: 500, Message: "boom"}, exitError},
		{usagef("bad"), exitUsage},
	}
	for _, tc := range cases {
		if got := exitCodeOf(tc.err); got != tc.want {
			t.Fatalf("exitCodeOf(%v)=%d want %d", tc.err, got, tc.want)
		}
	}
}

func TestResolveRef(t *testing.T) {
	items := []entityRef{
		{ID: "abc123", Name: "Home"},
		{ID: "abc999", Name: "Other"},
		{ID: "def456", Name: "Work"},
	}
	got, err := resolveRef("folder", "Home", items)
	if err != nil || got.ID != "abc123" {
		t.Fatalf("name resolve: got=%v err=%v", got, err)
	}
	got, err = resolveRef("folder", "def", items)
	if err != nil || got.ID != "def456" {
		t.Fatalf("prefix resolve: got=%v err=%v", got, err)
	}
	_, err = resolveRef("folder", "abc", items)
	if err == nil {
		t.Fatal("expected ambiguous prefix")
	}
	_, err = resolveRef("folder", "nope", items)
	if exitCodeOf(err) != exitNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestClientLoginAndGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "tok123", Path: "/"})
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "authenticated": true})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value != "tok123" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "code": "unauthorized", "error": "Please log in."})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "resticInstalled": true,
			"counts": map[string]int{"repositories": 1, "folders": 2, "jobs": 3},
			"activeRuns": []any{},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	session := filepath.Join(dir, "session")
	cfg := &config{
		url:         srv.URL,
		password:    "secret",
		sessionFile: session,
		timeout:     0,
	}
	cli, err := newCLI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Force unauthorized path then auto-login.
	status, m, err := cli.doJSON(http.MethodGet, "/api/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 || !truthy(m["ok"]) {
		t.Fatalf("status=%d body=%v", status, m)
	}
	tok, err := os.ReadFile(session)
	if err != nil || string(tok) != "tok123" {
		t.Fatalf("session file=%q err=%v", tok, err)
	}
}

func TestParseGlobal(t *testing.T) {
	cfg, rest, err := parseGlobal([]string{"--json", "--url", "http://example:9", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.json || cfg.url != "http://example:9" || len(rest) != 1 || rest[0] != "status" {
		t.Fatalf("cfg=%+v rest=%v", cfg, rest)
	}

	cfg, rest, err = parseGlobal([]string{"auth", "status", "--json", "--url=http://example:9"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.json || cfg.url != "http://example:9" || len(rest) != 2 || rest[0] != "auth" || rest[1] != "status" {
		t.Fatalf("relocated globals: cfg=%+v rest=%v", cfg, rest)
	}
}
