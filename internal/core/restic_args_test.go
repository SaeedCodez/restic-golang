//go:build unix

package core

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// argLogResticScript records each invocation's arguments, one call per line
// with the arguments separated by \037, and otherwise does nothing.
const argLogResticScript = `#!/bin/sh
if [ "$1" = "version" ]; then echo "restic 0.19.0 compiled with go1.26 on linux/amd64"; exit 0; fi
for a in "$@"; do printf '%s\037' "$a" >> "$RESTIC_ARG_LOG"; done
printf '\n' >> "$RESTIC_ARG_LOG"
exit 0
`

func installArgLogRestic(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "restic"), []byte(argLogResticScript), 0o755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
	logPath := filepath.Join(dir, "args.log")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RESTIC_ARG_LOG", logPath)
	if _, err := resticVersion(context.Background()); err != nil {
		t.Skipf("fake restic not runnable in this environment: %v", err)
	}
	return logPath
}

func readArgLog(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read arg log: %v", err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		calls = append(calls, strings.Split(strings.TrimSuffix(line, "\037"), "\037"))
	}
	return calls
}

// TestUserValuesReachResticAfterDoubleDash runs every restic call that takes a
// user value as a positional argument, with values that look like options, and
// checks restic receives each of them after "--", where it cannot read them as
// options.
func TestUserValuesReachResticAfterDoubleDash(t *testing.T) {
	logPath := installArgLogRestic(t)
	r := newResticRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := &Repository{BackendType: "Local", LocalPath: t.TempDir(), Password: "pw"}
	sink := &captureSink{}

	_, _ = r.Backup(ctx, repo, "-source", []string{"resticweb-job:abc"}, sink)
	_, _ = r.Restore(ctx, repo, "-restore-snap", t.TempDir(), nil, sink)
	_, _ = r.List(ctx, repo, "-ls-snap")
	_, _, _ = r.snapshotStats(ctx, repo, "-stats-snap")
	_, _ = r.Dump(ctx, repo, "-dump-snap", "/shop.sql", func(rd io.Reader) error {
		_, err := io.Copy(io.Discard, rd)
		return err
	}, sink)
	_, _ = r.ForgetSnapshots(ctx, repo, []string{"-forget-snap"}, sink)

	calls := readArgLog(t, logPath)
	for _, value := range []string{"-source", "-restore-snap", "-ls-snap", "-stats-snap", "-dump-snap", "-forget-snap"} {
		found := false
		for _, args := range calls {
			at := indexOf(args, value)
			if at < 0 {
				continue
			}
			found = true
			if dash := indexOf(args, "--"); dash < 0 || dash > at {
				t.Errorf("%s reached restic before \"--\": %q", value, args)
			}
		}
		if !found {
			t.Errorf("%s never reached restic; calls: %q", value, calls)
		}
	}
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func TestSnapshotBrowseRejectsValuesThatAreNotSnapshotIDs(t *testing.T) {
	fake := &fakeRunner{installed: true, lsNodes: []LsNode{{Path: "/data", Type: "dir"}}}
	app := testApp(t, fake)
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	for _, sid := range []string{"-x", "--no-lock", "latest", "ABCDEF12", "abc:etc", strings.Repeat("a", 65)} {
		if code := doJSON(t, h, "GET", "/api/repositories/"+repo.ID+"/snapshots/"+sid+"/ls", nil, nil); code != http.StatusBadRequest {
			t.Errorf("ls %q: code=%d, want 400", sid, code)
		}
	}
	if fake.lsCalls != 0 {
		t.Fatalf("restic ls ran %d time(s) for invalid ids", fake.lsCalls)
	}
	if code := doJSON(t, h, "GET", "/api/repositories/"+repo.ID+"/snapshots/abcdef12/ls", nil, nil); code != http.StatusOK {
		t.Fatalf("ls of a hex id: code=%d, want 200", code)
	}
}

func TestDownloadOnlyPassesASnapshotResticListed(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "abcdef1234567890", ShortID: "abcdef12"}}}
	app := testApp(t, fake)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	run, err := app.Coord.StartDownload(repo.ID, "-x")
	if err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(final.Error, "not found") || len(fake.recordedRestores()) != 0 {
		t.Fatalf("error = %q, restores = %d", final.Error, len(fake.recordedRestores()))
	}

	// A short id is resolved to the full id restic listed.
	run, _ = app.Coord.StartDownload(repo.ID, "abcdef12")
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	got := fake.recordedRestores()
	if len(got) != 1 || got[0].SnapshotID != "abcdef1234567890" {
		t.Fatalf("restores = %+v", got)
	}
}

func TestFolderPathMustBeAbsolute(t *testing.T) {
	h := testServer(t)
	for _, p := range []string{"data/site", "-x", "./site", "~/site"} {
		if code := doJSON(t, h, "POST", "/api/folders", map[string]any{"name": "f " + p, "path": p}, nil); code != http.StatusBadRequest {
			t.Errorf("folder path %q: code=%d, want 400", p, code)
		}
	}
	if code := doJSON(t, h, "POST", "/api/folders", map[string]any{"name": "ok", "path": "/data/site"}, nil); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("absolute folder path: code=%d", code)
	}
}
