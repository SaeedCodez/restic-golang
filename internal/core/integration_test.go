//go:build unix

package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests run the real restic binary and, for databases, real database
// containers on the local Docker Engine. They are slow and need both installed,
// so they only run with RESTIC_WEB_IT=1:
//
//	RESTIC_WEB_IT=1 go test ./internal/core -run Integration -v

func requireIntegration(t *testing.T, needDocker bool) {
	t.Helper()
	if os.Getenv("RESTIC_WEB_IT") == "" {
		t.Skip("set RESTIC_WEB_IT=1 to run integration tests with real restic and Docker")
	}
	if !ResticInstalled() {
		t.Skip("restic is not installed")
	}
	if needDocker {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := newDockerClient().Ping(ctx); err != nil {
			t.Skipf("Docker is not available: %v", err)
		}
	}
}

func localRepo(t *testing.T, app *App) Repository {
	t.Helper()
	repo, err := app.Repos.Create(Repository{Meta: Meta{Name: "it"}, BackendType: "Local", LocalPath: filepath.Join(t.TempDir(), "repo"), Password: "it-password"})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func waitTerminal(t *testing.T, app *App, runID string, timeout time.Duration) *Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if run, ok := app.Runs.Get(runID); ok && run.Status.Terminal() {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish in %s", runID, timeout)
	return nil
}

// succeeds returns a func that waits for a started run and fails the test
// unless it succeeded: succeeds(t, app)(app.Coord.StartBackup(id)).
func succeeds(t *testing.T, app *App) func(*Run, error) *Run {
	return func(run *Run, err error) *Run {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		final := waitTerminal(t, app, run.ID, 3*time.Minute)
		if final.Status != StatusSuccess {
			t.Fatalf("%s run %s: %s (%s)\n%s", final.Kind, final.ID, final.Status, final.Error, logText(t, app, final.ID))
		}
		return final
	}
}

// TestIntegrationSelectiveRestoreReplacesOnlySelection checks the claim the
// file browser relies on: with --include, restic's --delete only touches the
// selected paths.
func TestIntegrationSelectiveRestoreReplacesOnlySelection(t *testing.T) {
	requireIntegration(t, false)
	app := testApp(t, newResticRunner())
	repo := localRepo(t, app)
	// Resolve symlinks (macOS keeps temp dirs under the /var symlink): an
	// in-place restore walks the real path from "/".
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(tmp, "site")
	write := func(rel, content string) {
		p := filepath.Join(site, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(site, rel))
		if err != nil {
			return "<missing>"
		}
		return string(b)
	}
	write("uploads/1.jpg", "one")
	write("uploads/2.jpg", "two")
	write("themes/style.css", "v1")
	folder, _ := app.Folders.Create(Folder{Meta: Meta{Name: "site"}, Path: site})
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "site"}, FolderID: folder.ID, RepositoryID: repo.ID})
	backup := succeeds(t, app)(app.Coord.StartBackup(job.ID))

	os.Remove(filepath.Join(site, "uploads/1.jpg"))
	write("uploads/new.jpg", "new")
	write("themes/style.css", "v2")

	tree, err := app.SnapshotTree(context.Background(), &repo, backup.Summary.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if entries, ok := tree.List(filepath.Join(site, "uploads")); !ok || len(entries) != 2 {
		t.Fatalf("browser listing = %+v", entries)
	}

	succeeds(t, app)(app.Coord.StartRestoreWith(repo.ID, backup.Summary.SnapshotID, site,
		RestoreOptions{Include: []string{filepath.Join(site, "uploads")}}))
	if read("uploads/1.jpg") != "one" || read("uploads/new.jpg") != "<missing>" {
		t.Fatalf("uploads not replaced: 1=%q new=%q", read("uploads/1.jpg"), read("uploads/new.jpg"))
	}
	if read("themes/style.css") != "v2" {
		t.Fatalf("an unselected file was touched: %q", read("themes/style.css"))
	}
}

// dbEngineCase is one database server image to exercise.
type dbEngineCase struct {
	engine, image string
	env           []string
	// sql runs a statement in database db and returns its output.
	sql func(db, stmt string) []string
}

var dbEngineCases = []dbEngineCase{
	{
		engine: EnginePostgres, image: "postgres:16-alpine",
		env: []string{"POSTGRES_USER=app", "POSTGRES_PASSWORD=pw", "POSTGRES_DB=shop"},
		sql: func(db, stmt string) []string {
			return shCmd(pgPrelude+`exec psql -U "$U" -d "$1" -v ON_ERROR_STOP=1 -qAtX -c "$2"`, db, stmt)
		},
	},
	{
		engine: EngineMariaDB, image: "mariadb:11",
		env: []string{"MARIADB_ROOT_PASSWORD=rootpw", "MARIADB_DATABASE=shop"},
		sql: func(db, stmt string) []string {
			return shCmd(mysqlPrelude+`exec "$CLI" -u"$U" -N -B "$1" -e "$2"`, db, stmt)
		},
	},
	{
		engine: EngineMySQL, image: "mysql:8.4",
		env: []string{"MYSQL_ROOT_PASSWORD=rootpw", "MYSQL_DATABASE=shop"},
		sql: func(db, stmt string) []string {
			return shCmd(mysqlPrelude+`exec "$CLI" -u"$U" -N -B "$1" -e "$2"`, db, stmt)
		},
	},
}

// TestIntegrationDatabaseBackupRestore backs up a real database, changes it,
// restores it (drop and recreate: rows and tables added later are gone),
// restores a copy under another name, and checks a broken dump is rolled back.
func TestIntegrationDatabaseBackupRestore(t *testing.T) {
	requireIntegration(t, true)
	for _, tc := range dbEngineCases {
		t.Run(tc.engine, func(t *testing.T) { runDatabaseRoundTrip(t, tc) })
	}
}

func runDatabaseRoundTrip(t *testing.T, tc dbEngineCase) {
	ctx := context.Background()
	d := newDockerClient()
	name := fmt.Sprintf("rwit-%s-%d", tc.engine, time.Now().UnixNano()%1e6)
	args := []string{"run", "-d", "--name", name}
	for _, e := range tc.env {
		args = append(args, "-e", e)
	}
	if out, err := exec.Command("docker", append(args, tc.image)...).CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", "-v", name).Run() })
	waitDatabaseReady(t, d, name, tc.engine)

	query := func(db, stmt string) string {
		t.Helper()
		var out, errOut strings.Builder
		code, err := d.Exec(ctx, name, tc.sql(db, stmt), nil, &out, &errOut)
		if err != nil || code != 0 {
			t.Fatalf("query %q: code=%d err=%v %s", stmt, code, err, errOut.String())
		}
		return strings.TrimSpace(out.String())
	}
	query("shop", "CREATE TABLE items (id int PRIMARY KEY, name varchar(20)); INSERT INTO items VALUES (1, 'a'), (2, 'b')")

	app := testApp(t, newResticRunner())
	app.Docker = d
	repo := localRepo(t, app)
	cont, err := d.Inspect(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	kind, uuid := containerOwner(cont)
	db, err := app.Databases.Create(Database{Meta: Meta{Name: "shop"}, Engine: tc.engine, Container: name,
		OwnerKind: kind, OwnerUUID: uuid, DatabaseName: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "shop"}, DatabaseID: db.ID, RepositoryID: repo.ID})

	backup := succeeds(t, app)(app.Coord.StartBackup(job.ID))
	snapID := backup.Summary.SnapshotID
	if snapID == "" {
		t.Fatal("backup reported no snapshot")
	}

	query("shop", "INSERT INTO items VALUES (3, 'c'); CREATE TABLE extra (id int)")
	if tc.engine == EnginePostgres {
		// Database-level settings live outside a plain dump; a restore keeps them.
		query("postgres", "ALTER DATABASE shop SET statement_timeout = '42s'")
		query("postgres", "COMMENT ON DATABASE shop IS 'the shop'")
	}
	restore := succeeds(t, app)(app.Coord.StartRestoreDatabase(repo.ID, snapID, db.ID, RestoreDatabaseOptions{SafetyBackup: true}))
	if restore.Params["safetySnapshot"] == "" {
		t.Fatal("no safety snapshot recorded")
	}
	if got := query("shop", "SELECT count(*) FROM items"); got != "2" {
		t.Fatalf("rows after restore = %s, want 2", got)
	}
	if got := tableCountIn(t, query, tc.engine, "shop", "extra"); got != "0" {
		t.Fatalf("a table created after the backup survived the restore")
	}
	if tc.engine == EnginePostgres {
		if got := query("postgres", "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'shop'"); got != "app" {
			t.Fatalf("owner after restore = %q, want app", got)
		}
		if got := query("shop", "SHOW statement_timeout"); got != "42s" {
			t.Fatalf("database setting after restore = %q, want 42s", got)
		}
		if got := query("postgres", "SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname = 'shop'"); got != "the shop" {
			t.Fatalf("comment after restore = %q", got)
		}
		if got := query("postgres", "SELECT count(*) FROM pg_database WHERE datname LIKE 'rwrestore_%'"); got != "0" {
			t.Fatalf("temporary databases left behind: %s", got)
		}
	}

	succeeds(t, app)(app.Coord.StartRestoreDatabase(repo.ID, snapID, db.ID, RestoreDatabaseOptions{Into: "shop_copy"}))
	if got := query("shop_copy", "SELECT name FROM items ORDER BY id"); got != "a\nb" {
		t.Fatalf("copy rows = %q", got)
	}

	// A dump that does not import: the database must be put back as it was.
	query("shop", "INSERT INTO items VALUES (4, 'd')")
	tags := []string{dbSnapshotTag(tc.engine)}
	sink := &captureSink{}
	code, err := app.Runner.BackupStdin(ctx, &repo, dumpFileName("shop"), tags, func(w io.Writer) error {
		_, err := io.WriteString(w, "THIS IS NOT SQL;\n")
		return err
	}, sink)
	if err != nil || code != 0 || sink.summary == nil {
		t.Fatalf("bad snapshot: code=%d err=%v", code, err)
	}
	run, err := app.Coord.StartRestoreDatabase(repo.ID, sink.summary.SnapshotID, db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	final := waitTerminal(t, app, run.ID, 3*time.Minute)
	// PostgreSQL imports into a temporary database, so the live one was never
	// touched; MySQL/MariaDB drop it first and put the safety snapshot back.
	want := "put back as it was before"
	if tc.engine == EnginePostgres {
		want = "the database was not changed"
	}
	if final.Status != StatusFailed || !strings.Contains(final.Error, want) || !strings.Contains(final.Error, "ERROR") {
		t.Fatalf("broken restore: %s %q\n%s", final.Status, final.Error, logText(t, app, final.ID))
	}
	if got := query("shop", "SELECT count(*) FROM items"); got != "3" {
		t.Fatalf("rows after rollback = %s, want 3 (1, 2 and 4)", got)
	}

	_, dbs, _, err := app.ListContainerDatabases(ctx, name)
	if err != nil || !containsString(dbs, "shop") || !containsString(dbs, "shop_copy") {
		t.Fatalf("list databases = %v %v", dbs, err)
	}
}

func tableCountIn(t *testing.T, query func(db, stmt string) string, engine, db, table string) string {
	t.Helper()
	if engine == EnginePostgres {
		return query(db, "SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = '"+table+"'")
	}
	return query(db, "SELECT count(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = '"+db+"' AND TABLE_NAME = '"+table+"'")
}

// waitDatabaseReady waits for the image's init to finish (its temporary
// server is gone and the real one is up) and the client to answer.
func waitDatabaseReady(t *testing.T, d *dockerClient, name, engine string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
		s := string(logs)
		if i := strings.Index(s, "init process"); i >= 0 &&
			(strings.Contains(s[i:], "ready for connections") || strings.Contains(s[i:], "ready to accept connections")) {
			cmd, _ := listDatabasesCmd(engine)
			if code, err := d.Exec(context.Background(), name, cmd, nil, io.Discard, io.Discard); err == nil && code == 0 {
				return
			}
		}
		time.Sleep(time.Second)
	}
	logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
	t.Fatalf("%s did not become ready:\n%s", name, logs)
}
