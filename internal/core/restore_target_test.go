package core

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPlanResticRestore(t *testing.T) {
	folder := "/docker-volumes/site_wordpress-files/_data"
	cases := []struct {
		name      string
		requested string
		paths     []string
		wantTgt   string
		wantInc   []string
	}{
		{"in-place exact", folder, []string{folder}, "/", []string{folder}},
		{"in-place trailing slash", folder + "/", []string{folder}, "/", []string{folder}},
		{"custom other dir", "/tmp/restore-copy", []string{folder}, "/tmp/restore-copy", nil},
		{"no paths keeps requested", folder, nil, folder, nil},
		{"root with snapshot paths", "/", []string{folder, "/other"}, "/", []string{folder, "/other"}},
		{"root without paths", "/", nil, "/", nil},
		{"multi-path match first", "/a", []string{"/a", "/b"}, "/", []string{"/a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planResticRestore(tc.requested, tc.paths)
			if got.Target != tc.wantTgt {
				t.Fatalf("target = %q, want %q", got.Target, tc.wantTgt)
			}
			if len(got.Include) != len(tc.wantInc) {
				t.Fatalf("include = %v, want %v", got.Include, tc.wantInc)
			}
			for i := range tc.wantInc {
				if got.Include[i] != tc.wantInc[i] {
					t.Fatalf("include = %v, want %v", got.Include, tc.wantInc)
				}
			}
		})
	}
}

func TestRestoreArgs(t *testing.T) {
	got, err := restoreArgs("snap1", "/tmp/out", nil)
	if err != nil {
		t.Fatalf("custom: %v", err)
	}
	want := []string{"restore", "--target", "/tmp/out", "--delete", "--json", "--", "snap1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("custom args = %v, want %v", got, want)
	}

	got, err = restoreArgs("snap1", "/", []string{"/data/folder"})
	if err != nil {
		t.Fatalf("in-place: %v", err)
	}
	want = []string{"restore", "--target", "/", "--delete", "--json", "--include", "/data/folder", "--", "snap1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("in-place args = %v, want %v", got, want)
	}

	if _, err := restoreArgs("snap1", "/", nil); err == nil {
		t.Fatal("root without include should fail")
	}
	if _, err := restoreArgs("snap1", "/", []string{"", "  "}); err == nil {
		t.Fatal("root with empty include should fail")
	}
}

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

// restoreRunFails starts a restore that is expected to fail while planning, and
// checks restic restore was never invoked.
func restoreRunFails(t *testing.T, fake *fakeRunner, snapshotID, target, wantErr string) {
	t.Helper()
	app := newRunTestApp(t, fake)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	run, err := app.Coord.StartRestore(repo.ID, snapshotID, target)
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(final.Error, wantErr) {
		t.Fatalf("run error = %q, want it to contain %q", final.Error, wantErr)
	}
	if n := len(fake.recordedRestores()); n != 0 {
		t.Fatalf("restic restore should not have been invoked, got %d calls", n)
	}
}

func TestStartRestoreResolvesSnapshotPrefix(t *testing.T) {
	full := "aaaaaaaaaaaaaaaa"
	fake := &fakeRunner{
		installed: true,
		snaps: []Snapshot{
			{ID: full, ShortID: "aaaa", Paths: []string{"/data/site"}},
			{ID: "bbbbbbbbbbbbbbbb", ShortID: "bbbb", Paths: []string{"/other"}},
		},
	}
	app := newRunTestApp(t, fake)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	run, err := app.Coord.StartRestore(repo.ID, "aaaaaaa", filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	calls := fake.recordedRestores()
	if len(calls) != 1 || calls[0].SnapshotID != full {
		t.Fatalf("restore calls = %+v, want the full id %s", calls, full)
	}
}

func TestStartRestoreUnknownSnapshotFails(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "bbbbbbbbbbbbbbbb", ShortID: "bbbb"}}}
	restoreRunFails(t, fake, "aaaa", filepath.Join(t.TempDir(), "out"), "was not found")
}

func TestStartRestoreAmbiguousSnapshotFails(t *testing.T) {
	fake := &fakeRunner{
		installed: true,
		snaps: []Snapshot{
			{ID: "aaaa1111", ShortID: "aaaa1111", Paths: []string{"/a"}},
			{ID: "aaaa2222", ShortID: "aaaa2222", Paths: []string{"/b"}},
		},
	}
	restoreRunFails(t, fake, "aaaa", filepath.Join(t.TempDir(), "out"), "ambiguous")
}

// A failed lookup must not fall back to the requested target: if that target is
// the snapshot's own folder, a plain --target restore would nest a copy inside
// it and --delete the live data.
func TestStartRestoreLookupErrorFailsRun(t *testing.T) {
	fake := &fakeRunner{installed: true, snapErr: errors.New("restic snapshots timed out")}
	restoreRunFails(t, fake, "aaaa", "/docker-volumes/vol/_data", "could not read snapshot aaaa to plan the restore: restic snapshots timed out")
}

// The lookup runs inside the run: StartRestore returns at once, the wait is in
// the log, and Stop cancels it.
func TestStartRestoreLookupIsPartOfRun(t *testing.T) {
	defer func(after, poll time.Duration) { quietNoticeAfter, quietPoll = after, poll }(quietNoticeAfter, quietPoll)
	quietNoticeAfter, quietPoll = 50*time.Millisecond, 5*time.Millisecond
	fake := &fakeRunner{
		installed: true,
		snapshotsFn: func(ctx context.Context, tag string) ([]Snapshot, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	app := newRunTestApp(t, fake)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	start := time.Now()
	run, err := app.Coord.StartRestore(repo.ID, "aaaa", filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("StartRestore: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("StartRestore blocked for %s; the lookup should run inside the run", d)
	}
	waitForStatus(t, app.Runs, run.ID, StatusRunning, 2*time.Second)
	time.Sleep(150 * time.Millisecond) // long enough for a "still waiting" note
	if err := app.Coord.Stop(run.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusCanceled, 2*time.Second)

	lines, _ := app.Runs.ReadLog(run.ID, 0)
	var sawLookup, sawWaiting bool
	for _, l := range lines {
		if strings.Contains(l.Message, "Reading snapshot aaaa to plan the restore") {
			sawLookup = true
		}
		if strings.Contains(l.Message, "Still waiting for restic to list snapshots") {
			sawWaiting = true
		}
	}
	if !sawLookup || !sawWaiting {
		t.Fatalf("lookup (%v) or wait note (%v) missing from the run log: %+v", sawLookup, sawWaiting, lines)
	}
	if n := len(fake.recordedRestores()); n != 0 {
		t.Fatalf("restic restore should not have been invoked, got %d calls", n)
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
	calls := fake.recordedRestores()
	if len(calls) != 1 {
		t.Fatalf("restore calls = %d, want 1", len(calls))
	}
	if !reflect.DeepEqual(calls[0].Include, []string{folder}) {
		t.Fatalf("restore include = %v, want [%s]", calls[0].Include, folder)
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
	calls := fake.recordedRestores()
	if len(calls) != 1 {
		t.Fatalf("restore calls = %d, want 1", len(calls))
	}
	if len(calls[0].Include) != 0 {
		t.Fatalf("custom restore include = %v, want empty", calls[0].Include)
	}
}

func TestStartRestoreRootWithoutPathsRefused(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "nopaths00000001", ShortID: "nopaths0"}}}
	restoreRunFails(t, fake, "nopaths00000001", "/", "cannot restore to / as a replacement")
}
