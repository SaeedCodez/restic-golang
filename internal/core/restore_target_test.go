package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveResticRestoreTarget(t *testing.T) {
	folder := "/docker-volumes/site_wordpress-files/_data"
	cases := []struct {
		name      string
		requested string
		paths     []string
		want      string
	}{
		{"in-place exact", folder, []string{folder}, "/"},
		{"in-place trailing slash", folder + "/", []string{folder}, "/"},
		{"in-place snap trailing", folder, []string{folder + "/"}, "/"},
		{"custom other dir", "/tmp/restore-copy", []string{folder}, "/tmp/restore-copy"},
		{"no paths", folder, nil, folder},
		{"empty paths", folder, []string{}, folder},
		{"root explicit", "/", []string{folder}, "/"},
		{"multi-path match first", "/a", []string{"/a", "/b"}, "/"},
		{"multi-path match second", "/b", []string{"/a", "/b"}, "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveResticRestoreTarget(tc.requested, tc.paths)
			if got != tc.want {
				t.Fatalf("resolveResticRestoreTarget(%q, %v) = %q, want %q", tc.requested, tc.paths, got, tc.want)
			}
		})
	}
}

func TestLookupSnapshotPaths(t *testing.T) {
	folder := "/data/site"
	fake := &fakeRunner{
		installed: true,
		snaps: []Snapshot{
			{ID: "aaaaaaaaaaaaaaaa", ShortID: "aaaa", Paths: []string{folder}},
			{ID: "bbbbbbbbbbbbbbbb", ShortID: "bbbb", Paths: []string{"/other"}},
		},
	}
	repo := &Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r"}

	got := lookupSnapshotPaths(context.Background(), fake, repo, "aaaaaaaaaaaaaaaa")
	if len(got) != 1 || got[0] != folder {
		t.Fatalf("full id: got %v", got)
	}
	got = lookupSnapshotPaths(context.Background(), fake, repo, "aaaa")
	if len(got) != 1 || got[0] != folder {
		t.Fatalf("short id: got %v", got)
	}
	got = lookupSnapshotPaths(context.Background(), fake, repo, "aaaaaaa")
	if len(got) != 1 || got[0] != folder {
		t.Fatalf("unique prefix: got %v", got)
	}
	if got := lookupSnapshotPaths(context.Background(), fake, repo, "missing"); got != nil {
		t.Fatalf("missing id: got %v", got)
	}
}

func TestLookupSnapshotPathsAmbiguousPrefix(t *testing.T) {
	fake := &fakeRunner{
		installed: true,
		snaps: []Snapshot{
			{ID: "aaaa1111", ShortID: "aaaa1111", Paths: []string{"/a"}},
			{ID: "aaaa2222", ShortID: "aaaa2222", Paths: []string{"/b"}},
		},
	}
	repo := &Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r"}
	if got := lookupSnapshotPaths(context.Background(), fake, repo, "aaaa"); got != nil {
		t.Fatalf("ambiguous prefix should return nil, got %v", got)
	}
}

func TestStartRestoreInPlaceUsesRootTarget(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "site-data")
	var gotTarget string
	fake := &fakeRunner{
		installed: true,
		snaps: []Snapshot{
			{ID: "snapinplace0001", ShortID: "snapin", Paths: []string{folder}},
		},
		streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
			sink.Summary(Summary{FilesRestored: 1, BytesRestored: 10})
			return 0, nil
		},
		onRestore: func(target string) { gotTarget = target },
	}
	app := newRunTestApp(t, fake)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	run, err := app.Coord.StartRestore(repo.ID, "snapinplace0001", folder)
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Params["target"] != folder {
		t.Fatalf("params.target = %q, want user-facing folder", final.Params["target"])
	}
	if final.Params["resticTarget"] != "/" {
		t.Fatalf("params.resticTarget = %q, want /", final.Params["resticTarget"])
	}
	if gotTarget != "/" {
		t.Fatalf("Runner.Restore target = %q, want /", gotTarget)
	}
}

func TestStartRestoreCustomKeepsTarget(t *testing.T) {
	folder := "/docker-volumes/vol/_data"
	custom := filepath.Join(t.TempDir(), "copy-here")
	var gotTarget string
	fake := &fakeRunner{
		installed: true,
		snaps: []Snapshot{
			{ID: "snapcustom0001", ShortID: "snapcu", Paths: []string{folder}},
		},
		streamFn: func(ctx context.Context, kind RunKind, sink RunSink) (int, error) {
			return 0, nil
		},
		onRestore: func(target string) { gotTarget = target },
	}
	app := newRunTestApp(t, fake)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	run, err := app.Coord.StartRestore(repo.ID, "snapcustom0001", custom)
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Params["target"] != custom {
		t.Fatalf("params.target = %q", final.Params["target"])
	}
	if _, ok := final.Params["resticTarget"]; ok {
		t.Fatalf("resticTarget should be omitted when equal to target")
	}
	if gotTarget != custom {
		t.Fatalf("Runner.Restore target = %q, want %q", gotTarget, custom)
	}
}
