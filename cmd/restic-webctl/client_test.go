package main

import (
	"errors"
	"testing"

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
