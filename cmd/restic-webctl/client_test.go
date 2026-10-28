package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"restic-web/internal/core"
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
		{&apiError{Code: "not_active"}, exitConflict},
		{&apiError{Message: "boom"}, exitError},
		{usagef("bad"), exitUsage},
		{&core.BusyError{RepoName: "r"}, exitConflict},
		{&core.ConflictError{Msg: "c"}, exitConflict},
		{&core.NotFoundError{Msg: "n"}, exitNotFound},
		{&core.ValidationError{Msg: "v"}, exitError},
		{core.ErrRunNotActive, exitConflict},
		{errors.New("other"), exitError},
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

func TestParseGlobal(t *testing.T) {
	cfg, rest, err := parseGlobal([]string{"--json", "--database", "postgres://x", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.json || cfg.database != "postgres://x" || len(rest) != 1 || rest[0] != "status" {
		t.Fatalf("cfg=%+v rest=%v", cfg, rest)
	}

	cfg, rest, err = parseGlobal([]string{"auth", "status", "--json", "--data=/tmp/data"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.json || cfg.dataDir != "/tmp/data" || len(rest) != 2 || rest[0] != "auth" || rest[1] != "status" {
		t.Fatalf("relocated globals: cfg=%+v rest=%v", cfg, rest)
	}
}

func TestRepoViewOfRedactsSecrets(t *testing.T) {
	v := repoViewOf(core.Repository{
		Meta:      core.Meta{Name: "r"},
		Password:  "secret",
		SecretKey: "sk",
	})
	if v.Password != "" || v.SecretKey != "" {
		t.Fatalf("secrets leaked: %+v", v)
	}
	if !v.HasPassword || !v.HasSecretKey {
		t.Fatalf("flags: %+v", v)
	}
}

func TestParseGlobalTimeout(t *testing.T) {
	cfg, _, err := parseGlobal([]string{"repo", "snapshots", "r"})
	if err != nil || cfg.timeout != defaultTimeout {
		t.Fatalf("default timeout = %v (err %v), want %v", cfg.timeout, err, defaultTimeout)
	}
	for in, want := range map[string]time.Duration{"90s": 90 * time.Second, "30m": 30 * time.Minute, "120": 120 * time.Second, "0": 0} {
		for _, args := range [][]string{{"--timeout", in, "repo", "snapshots", "r"}, {"repo", "snapshots", "r", "--timeout=" + in}} {
			cfg, rest, err := parseGlobal(args)
			if err != nil || cfg.timeout != want || len(rest) != 3 {
				t.Fatalf("parseGlobal(%v): timeout=%v rest=%v err=%v, want %v", args, cfg.timeout, rest, err, want)
			}
		}
	}
	for _, bad := range []string{"-5s", "soon", ""} {
		if _, _, err := parseGlobal([]string{"--timeout", bad, "status"}); exitCodeOf(err) != exitUsage {
			t.Fatalf("--timeout %q: err=%v, want a usage error", bad, err)
		}
	}
	if _, _, err := parseGlobal([]string{"status", "--timeout"}); exitCodeOf(err) != exitUsage {
		t.Fatalf("--timeout without a value: err=%v, want a usage error", err)
	}
}

func TestResticCallErrorTimeout(t *testing.T) {
	c := &CLI{cfg: &config{timeout: 30 * time.Second}}
	stopped := fmt.Errorf("restic snapshots timed out: %w", context.DeadlineExceeded)
	err := c.resticCallError("restic_error", stopped)
	ae, ok := err.(*apiError)
	if !ok || ae.Code != "timeout" || exitCodeOf(err) != exitError {
		t.Fatalf("timeout error = %#v", err)
	}
	if !strings.Contains(ae.Message, "--timeout 30s") || !strings.Contains(ae.Message, "restic snapshots timed out") {
		t.Fatalf("timeout message = %q", ae.Message)
	}
	if ae := c.resticCallError("restic_error", errors.New("boom")).(*apiError); ae.Code != "restic_error" {
		t.Fatalf("plain failure code = %q", ae.Code)
	}
	if ae := c.resticCallError("restic_error", fmt.Errorf("x: %w", context.Canceled)).(*apiError); ae.Code != "interrupted" {
		t.Fatalf("canceled code = %q", ae.Code)
	}
}

func TestFmtLimit(t *testing.T) {
	for kib, want := range map[int]string{0: "none", -1: "none", 1024: "1 MB/s", 5120: "5 MB/s", 512: "0.5 MB/s"} {
		if got := fmtLimit(kib); got != want {
			t.Errorf("fmtLimit(%d) = %q, want %q", kib, got, want)
		}
	}
}
