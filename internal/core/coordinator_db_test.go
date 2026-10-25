package core

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// dbJobApp sets up an app on the Coolify-like host with a repository, a
// database source for "shop" on the shared MariaDB, and a job for it.
func dbJobApp(t *testing.T, fake *fakeRunner) (*App, *fakeDocker, Repository, Database, Job) {
	t.Helper()
	app := testApp(t, fake)
	fd := coolifyHost()
	app.Docker = fd
	app.Discovery.selfID = func() string { return "self00000000" }
	repo, err := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := app.Databases.Create(Database{Meta: Meta{Name: "shop db"}, Engine: EngineMariaDB, Container: "mdb1",
		OwnerKind: "database", OwnerUUID: "mdb1", DatabaseName: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := app.Jobs.Create(Job{Meta: Meta{Name: "shop database"}, DatabaseID: db.ID, RepositoryID: repo.ID})
	if err != nil {
		t.Fatal(err)
	}
	return app, fd, repo, db, job
}

// Dumps as the fake server emits them: complete, with their closing marker.
const (
	shopDump = "-- MariaDB dump of shop\n-- Dump completed on 2026-10-25\n"
	liveDump = "-- live data\n-- Dump completed on 2026-10-25\n"
)

func execKinds(calls []execCall) string {
	kinds := make([]string, len(calls))
	for i, c := range calls {
		kinds[i] = c.Kind
	}
	return strings.Join(kinds, ",")
}

func logText(t *testing.T, app *App, runID string) string {
	t.Helper()
	lines, _ := app.Runs.ReadLog(runID, 0)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Message + "\n")
	}
	return b.String()
}

func TestDatabaseBackupStreamsDumpIntoRestic(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app, fd, _, _, job := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		if dbCmdKind(cmd) == "dump" {
			return shopDump, "", 0
		}
		return "", "", 0
	}

	run, err := app.Coord.StartBackup(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.FolderPath != "mariadb://mdb1/shop" || final.Params["database"] != "shop" {
		t.Fatalf("run = %+v", final)
	}
	calls := fake.recordedStdinBackups()
	if len(calls) != 1 || calls[0].Filename != "shop.sql" || calls[0].Data != shopDump {
		t.Fatalf("stdin backups = %+v", calls)
	}
	if strings.Join(calls[0].Tags, ",") != job.ResticTag()+",resticweb-db:mariadb" {
		t.Fatalf("tags = %v", calls[0].Tags)
	}
	execs := fd.recordedExecs()
	if execKinds(execs) != "dump" || execs[0].Container != "mdb1" || execs[0].Args[0] != "shop" {
		t.Fatalf("execs = %+v", execs)
	}
}

func TestDatabaseBackupFailsWhenDumpFails(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app, fd, _, _, job := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		return "partial", "mariadb-dump: Got error: 1045: Access denied\n", 2
	}
	run, err := app.Coord.StartBackup(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(final.Error, "the database dump failed (exit code 2): mariadb-dump: Got error: 1045: Access denied") {
		t.Fatalf("error = %q", final.Error)
	}
	if n := len(fake.recordedStdinBackups()); n != 0 {
		t.Fatalf("a failed dump must not be saved, got %d snapshot(s)", n)
	}
}

func TestDatabaseBackupFindsRecreatedContainer(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app, _, repo, _, _ := dbJobApp(t, fake)
	// Saved under an old name; Coolify recreated the service container since.
	db, _ := app.Databases.Create(Database{Meta: Meta{Name: "pelk db"}, Engine: EngineMariaDB, Container: "mariadb-svc2-123",
		OwnerKind: "service", OwnerUUID: "svc2", OwnerService: "mariadb", DatabaseName: "pelk"})
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "pelk database"}, DatabaseID: db.ID, RepositoryID: repo.ID})
	app.Docker.(*fakeDocker).execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		return shopDump, "", 0
	}

	run, err := app.Coord.StartBackup(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if got, _ := app.Databases.Get(db.ID); got.Container != "mariadb-svc2" {
		t.Fatalf("container name not updated: %q", got.Container)
	}
	if !strings.Contains(logText(t, app, run.ID), "The database container is now mariadb-svc2") {
		t.Fatal("the rename should be logged")
	}
}

// dbSnapshot is a database snapshot as restic lists it.
func dbSnapshot(id, engine, file string) Snapshot {
	return Snapshot{ID: id, ShortID: id[:8], Paths: []string{"/" + file}, Tags: []string{"resticweb-job:x", dbSnapshotTag(engine)}}
}

func TestDatabaseRestoreTakesSafetyBackupThenReplaces(t *testing.T) {
	fake := &fakeRunner{installed: true,
		snaps:    []Snapshot{dbSnapshot("aaaaaaaa1111", EngineMariaDB, "shop.sql")},
		dumpData: map[string][]byte{"aaaaaaaa1111 /shop.sql": []byte("-- the backup")},
	}
	app, fd, repo, db, job := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		switch dbCmdKind(cmd) {
		case "meta":
			return "utf8mb4|utf8mb4_unicode_ci\n", "", 0
		case "dump":
			return liveDump, "", 0
		}
		return "", "", 0
	}

	run, err := app.Coord.StartRestoreDatabase(repo.ID, "aaaaaaaa", db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Kind != KindRestoreDB || final.Params["safetySnapshot"] != "stdinsnap01" {
		t.Fatalf("run = %+v", final)
	}
	if final.Summary == nil || final.Summary.BytesRestored != int64(len("-- the backup")) {
		t.Fatalf("summary = %+v", final.Summary)
	}

	execs := fd.recordedExecs()
	if execKinds(execs) != "meta,dump,drop,create,import" {
		t.Fatalf("exec order = %s", execKinds(execs))
	}
	if !strings.Contains(execs[3].Args[0], "CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci") {
		t.Fatalf("create kept no charset: %v", execs[3].Args)
	}
	if execs[4].Stdin != "-- the backup" {
		t.Fatalf("import stdin = %q", execs[4].Stdin)
	}
	// The safety backup is a normal database snapshot, also tagged for the job.
	safety := fake.recordedStdinBackups()
	if len(safety) != 1 || safety[0].Data != liveDump {
		t.Fatalf("safety = %+v", safety)
	}
	tags := strings.Join(safety[0].Tags, ",")
	if !strings.Contains(tags, preRestoreTag) || !strings.Contains(tags, job.ResticTag()) || !strings.Contains(tags, "resticweb-db:mariadb") {
		t.Fatalf("safety tags = %s", tags)
	}
	if strings.Join(fake.recordedDumps(), ",") != "aaaaaaaa1111 /shop.sql" {
		t.Fatalf("dumps = %v", fake.recordedDumps())
	}
}

func TestDatabaseRestoreRollsBackWhenImportFails(t *testing.T) {
	fake := &fakeRunner{installed: true,
		snaps:    []Snapshot{dbSnapshot("bbbbbbbb2222", EngineMariaDB, "shop.sql")},
		dumpData: map[string][]byte{"bbbbbbbb2222 /shop.sql": []byte("-- broken backup")},
	}
	app, fd, repo, db, _ := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		switch dbCmdKind(cmd) {
		case "meta":
			return "utf8mb4|utf8mb4_general_ci\n", "", 0
		case "dump":
			return liveDump, "", 0
		case "import":
			if stdin == "-- broken backup" {
				return "", "ERROR 1064 (42000) at line 12: You have an error in your SQL syntax\n", 1
			}
		}
		return "", "", 0
	}

	run, err := app.Coord.StartRestoreDatabase(repo.ID, "bbbbbbbb2222", db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(final.Error, "ERROR 1064") || !strings.Contains(final.Error, "put back as it was before") {
		t.Fatalf("error = %q", final.Error)
	}
	execs := fd.recordedExecs()
	if execKinds(execs) != "meta,dump,drop,create,import,drop,create,import" {
		t.Fatalf("exec order = %s", execKinds(execs))
	}
	if execs[7].Stdin != liveDump {
		t.Fatalf("rollback imported %q, want the safety dump", execs[7].Stdin)
	}
}

func TestDatabaseRestoreRollsBackWhenResticDumpFails(t *testing.T) {
	fake := &fakeRunner{installed: true, dumpCode: 1,
		snaps:    []Snapshot{dbSnapshot("cccccccc3333", EngineMariaDB, "shop.sql")},
		dumpData: map[string][]byte{"cccccccc3333 /shop.sql": []byte("-- trunc")},
	}
	app, fd, repo, db, _ := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		switch dbCmdKind(cmd) {
		case "meta":
			return "utf8mb4|utf8mb4_general_ci\n", "", 0
		case "dump":
			return liveDump, "", 0
		}
		return "", "", 0
	}
	run, _ := app.Coord.StartRestoreDatabase(repo.ID, "cccccccc3333", db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	// The import "succeeded" on a truncated stream, which must not count; the
	// rollback's own dump fails the same way here, so both are reported.
	if !strings.Contains(final.Error, "restic could not read the dump") {
		t.Fatalf("error = %q", final.Error)
	}
	if !strings.Contains(final.Error, "previous contents are in snapshot") {
		t.Fatalf("error should point at the safety snapshot: %q", final.Error)
	}
}

func TestDatabaseRestoreIntoNewDatabaseSkipsSafetyBackup(t *testing.T) {
	fake := &fakeRunner{installed: true,
		snaps:    []Snapshot{dbSnapshot("dddddddd4444", EngineMariaDB, "shop.sql")},
		dumpData: map[string][]byte{"dddddddd4444 /shop.sql": []byte("-- the backup")},
	}
	app, fd, repo, db, _ := dbJobApp(t, fake)
	run, err := app.Coord.StartRestoreDatabase(repo.ID, "dddddddd4444", db.ID, RestoreDatabaseOptions{Into: "shop_restored", SafetyBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	if final.Params["database"] != "shop_restored" || final.FolderPath != "mariadb://mdb1/shop_restored" {
		t.Fatalf("run = %+v", final)
	}
	execs := fd.recordedExecs()
	// Meta found nothing: no safety backup, a plain CREATE DATABASE.
	if execKinds(execs) != "meta,drop,create,import" {
		t.Fatalf("exec order = %s", execKinds(execs))
	}
	if execs[2].Args[0] != "CREATE DATABASE `shop_restored`" || execs[3].Args[0] != "shop_restored" {
		t.Fatalf("create/import = %v / %v", execs[2].Args, execs[3].Args)
	}
	if len(fake.recordedStdinBackups()) != 0 {
		t.Fatal("no safety backup expected for a database that did not exist")
	}
}

func TestDatabaseRestoreRejectsWrongSnapshots(t *testing.T) {
	fake := &fakeRunner{installed: true, snaps: []Snapshot{
		{ID: "eeeeeeee5555", Paths: []string{"/docker-volumes/x/_data"}, Tags: []string{"resticweb-job:x"}},
		dbSnapshot("ffffffff6666", EnginePostgres, "app.sql"),
	}}
	app, _, repo, db, _ := dbJobApp(t, fake)
	for id, want := range map[string]string{
		"eeeeeeee5555": "is not a database backup",
		"ffffffff6666": "is a PostgreSQL dump and cannot be restored into MariaDB",
	} {
		run, err := app.Coord.StartRestoreDatabase(repo.ID, id, db.ID, RestoreDatabaseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
		if !strings.Contains(final.Error, want) {
			t.Errorf("%s: error = %q, want %q", id, final.Error, want)
		}
	}
	if _, err := app.Coord.StartRestoreDatabase(repo.ID, "x", db.ID, RestoreDatabaseOptions{Into: "mysql"}); err == nil {
		t.Error("restoring over a system database must be refused")
	}
	if _, err := app.Coord.StartRestoreDatabase(repo.ID, "x", db.ID, RestoreDatabaseOptions{Into: "a;b"}); err == nil {
		t.Error("an unsafe database name must be refused")
	}
}

func TestSelectiveRestoreInPlaceStopsAndRestartsSite(t *testing.T) {
	const folder = "/docker-volumes/svc1_wordpress-files/_data"
	fake := &fakeRunner{installed: true, snaps: []Snapshot{{ID: "abcdabcd1234", Paths: []string{folder}}},
		lsNodes: []LsNode{
			{Path: folder, Name: "_data", Type: "dir"},
			{Path: folder + "/wp-config.php", Name: "wp-config.php", Type: "file"},
			{Path: folder + "/wp-content/uploads", Name: "uploads", Type: "dir"},
		}}
	app := testApp(t, fake)
	fd := coolifyHost()
	app.Docker = fd
	app.Discovery.selfID = func() string { return "self00000000" }
	repoID, _ := makeJob(t, app, "sel", folder)

	run, err := app.Coord.StartRestoreWith(repoID, "abcdabcd", folder, RestoreOptions{
		Include:        []string{folder + "/wp-content/uploads", folder + "/wp-config.php"},
		StopContainers: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	calls := fake.recordedRestores()
	if len(calls) != 1 || calls[0].Target != "/" {
		t.Fatalf("restores = %+v", calls)
	}
	if strings.Join(calls[0].Include, ",") != folder+"/wp-content/uploads,"+folder+"/wp-config.php" {
		t.Fatalf("include = %v", calls[0].Include)
	}
	fd.mu.Lock()
	stopped, started := strings.Join(fd.stopped, ","), strings.Join(fd.started, ",")
	fd.mu.Unlock()
	if stopped != "wp00000000000000aaaa" || started != "wp00000000000000aaaa" {
		t.Fatalf("stopped=%s started=%s", stopped, started)
	}

	// A selection outside the folder being restored is refused before restic runs.
	run, _ = app.Coord.StartRestoreWith(repoID, "abcdabcd", folder, RestoreOptions{Include: []string{"/etc/passwd"}})
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(final.Error, "not in this snapshot") {
		t.Fatalf("error = %q", final.Error)
	}
	// So is a path inside it that the snapshot does not hold: --delete would
	// remove the live file instead of restoring it.
	run, _ = app.Coord.StartRestoreWith(repoID, "abcdabcd", folder, RestoreOptions{Include: []string{folder + "/new-since-backup.php"}})
	final = waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if !strings.Contains(final.Error, "is not in snapshot") || len(fake.recordedRestores()) != 1 {
		t.Fatalf("error = %q, restores = %d", final.Error, len(fake.recordedRestores()))
	}
}

func TestIncludesAreEscapedForRestic(t *testing.T) {
	args, err := restoreArgs("s1", "/", []string{"/data/site[1]/photo*.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if got := args[len(args)-1]; got != `/data/site\[1]/photo\*.jpg` {
		t.Fatalf("include = %q", got)
	}
}

func TestPostgresRestoreImportsIntoTemporaryDatabaseThenSwaps(t *testing.T) {
	fake := &fakeRunner{installed: true,
		snaps:    []Snapshot{dbSnapshot("pppppppp1111", EnginePostgres, "app.sql")},
		dumpData: map[string][]byte{"pppppppp1111 /app.sql": []byte("-- pg backup")},
	}
	app, fd, repo, _, _ := dbJobApp(t, fake)
	db, _ := app.Databases.Create(Database{Meta: Meta{Name: "pg app"}, Engine: EnginePostgres, Container: "pg1",
		OwnerKind: "database", OwnerUUID: "pg1", DatabaseName: "iranreps"})
	for i := range fd.containers {
		if fd.containers[i].Name == "pg1" {
			fd.containers[i].State = "running"
		}
	}
	fail := ""
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		kind := dbCmdKind(cmd)
		switch {
		case kind == fail:
			return "", "ERROR: boom\n", 3
		case kind == "meta":
			return "iranreps|UTF8|en_US.utf8|en_US.utf8\n", "", 0
		case kind == "dump":
			return "-- live\n-- PostgreSQL database dump complete\n", "", 0
		}
		return "", "", 0
	}

	run, err := app.Coord.StartRestoreDatabase(repo.ID, "pppppppp1111", db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	final := waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
	tmp := final.Params["tempDatabase"]
	execs := fd.recordedExecs()
	if !strings.HasPrefix(tmp, "rwrestore_") || execKinds(execs) != "meta,dump,create,import,props,swap" {
		t.Fatalf("tmp=%q exec order = %s", tmp, execKinds(execs))
	}
	// Created and imported under the temporary name, owner kept, then renamed.
	if execs[2].Args[0] != tmp || execs[2].Args[1] != "iranreps" || execs[3].Args[0] != tmp || execs[3].Stdin != "-- pg backup" {
		t.Fatalf("create/import = %+v / %+v", execs[2].Args, execs[3])
	}
	if execs[5].Args[0] != "iranreps" || execs[5].Args[1] != tmp {
		t.Fatalf("swap = %v", execs[5].Args)
	}

	// A failed import leaves the live database alone: the copy is removed and
	// nothing is rolled back, because nothing was replaced.
	fail = "import"
	fd.mu.Lock()
	fd.execCalls = nil
	fd.mu.Unlock()
	run, _ = app.Coord.StartRestoreDatabase(repo.ID, "pppppppp1111", db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	final = waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	execs = fd.recordedExecs()
	if execKinds(execs) != "meta,dump,create,import,drop" || execs[4].Args[0] != final.Params["tempDatabase"] {
		t.Fatalf("exec order = %s (%v)", execKinds(execs), execs[len(execs)-1].Args)
	}
	if !strings.Contains(final.Error, "the database was not changed") {
		t.Fatalf("error = %q", final.Error)
	}

	// A swap whose DROP failed: the live database is reopened to clients.
	fail = "swap"
	fd.mu.Lock()
	fd.execCalls = nil
	fd.mu.Unlock()
	run, _ = app.Coord.StartRestoreDatabase(repo.ID, "pppppppp1111", db.ID, RestoreDatabaseOptions{})
	final = waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if got := execKinds(fd.recordedExecs()); got != "meta,create,import,props,swap,meta,allow,drop" {
		t.Fatalf("exec order = %s", got)
	}
}

func TestMySQLRestoreDoesNotRollBackWhenDropIsRefused(t *testing.T) {
	fake := &fakeRunner{installed: true,
		snaps:    []Snapshot{dbSnapshot("mmmmmmmm1111", EngineMariaDB, "shop.sql")},
		dumpData: map[string][]byte{"mmmmmmmm1111 /shop.sql": []byte("-- b")},
	}
	app, fd, repo, db, _ := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		switch dbCmdKind(cmd) {
		case "meta":
			return "utf8mb4|utf8mb4_general_ci\n", "", 0
		case "dump":
			return liveDump, "", 0
		case "drop":
			return "", "ERROR 1205: Lock wait timeout exceeded\n", 1
		}
		return "", "", 0
	}
	run, _ := app.Coord.StartRestoreDatabase(repo.ID, "mmmmmmmm1111", db.ID, RestoreDatabaseOptions{SafetyBackup: true})
	final := waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if got := execKinds(fd.recordedExecs()); got != "meta,dump,drop" {
		t.Fatalf("exec order = %s", got)
	}
	if !strings.Contains(final.Error, "Lock wait timeout") || !strings.Contains(final.Error, "the database was not changed") {
		t.Fatalf("error = %q", final.Error)
	}
}

func TestAbandonedDatabaseCommandIsKilled(t *testing.T) {
	fake := &fakeRunner{installed: true}
	app, fd, _, _, job := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		return liveDump, "", 0
	}
	// restic refuses the stream (it went away): the dump is abandoned midway.
	fake.streamFn = nil
	app.Docker = &brokenOutputDocker{fakeDocker: fd}
	run, _ := app.Coord.StartBackup(job.ID)
	waitForStatus(t, app.Runs, run.ID, StatusFailed, 2*time.Second)
	if got := execKinds(fd.recordedExecs()); got != "kill" {
		t.Fatalf("exec order = %s", got)
	}
}

// brokenOutputDocker fails every command's output, like a reader that went
// away, but still records the follow-up exec that ends it.
type brokenOutputDocker struct{ *fakeDocker }

func (b *brokenOutputDocker) Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(cmd) > 0 && cmd[0] == "env" {
		return -1, errors.New("docker exec output: write |1: broken pipe")
	}
	return b.fakeDocker.Exec(ctx, id, cmd, stdin, stdout, stderr)
}

func TestDatabaseRunsAreSerializedPerDatabase(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeRunner{installed: true, streamFn: gatedStream(started, release)}
	app, fd, _, db, job := dbJobApp(t, fake)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) { return shopDump, "", 0 }
	other, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r2"}, BackendType: "Local", LocalPath: "/tmp/r2", Password: "pw"})
	job2, _ := app.Jobs.Create(Job{Meta: Meta{Name: "shop to r2"}, DatabaseID: db.ID, RepositoryID: other.ID})

	run, err := app.Coord.StartBackup(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	// A different repository, the same database: refused while the first runs.
	if _, err := app.Coord.StartBackup(job2.ID); err == nil || !strings.Contains(err.Error(), `database "shop" is busy`) {
		t.Fatalf("second backup err = %v", err)
	}
	if _, err := app.Coord.StartRestoreDatabase(other.ID, "x", db.ID, RestoreDatabaseOptions{}); err == nil {
		t.Fatal("a restore of the same database must be refused")
	}
	close(release)
	waitForStatus(t, app.Runs, run.ID, StatusSuccess, 2*time.Second)
}

func TestSnapshotTreeListsOneLevel(t *testing.T) {
	tree := buildSnapshotTree([]LsNode{
		{Path: "/docker-volumes", Name: "docker-volumes", Type: "dir"},
		{Path: "/docker-volumes/v/_data", Name: "_data", Type: "dir"},
		{Path: "/docker-volumes/v/_data/index.php", Name: "index.php", Type: "file", Size: 10},
		{Path: "/docker-volumes/v/_data/wp-content", Name: "wp-content", Type: "dir"},
		{Path: "/docker-volumes/v/_data/wp-content/a.jpg", Name: "a.jpg", Type: "file", Size: 90},
	})
	list, ok := tree.List("/docker-volumes/v/_data")
	if !ok || len(list) != 2 || list[0].Name != "wp-content" || list[0].Size != 90 || list[1].Name != "index.php" {
		t.Fatalf("list = %+v", list)
	}
	// The missing "/docker-volumes/v" is filled in so the tree walks from "/".
	if l, ok := tree.List("/docker-volumes"); !ok || len(l) != 1 || l[0].Name != "v" || l[0].Size != 100 {
		t.Fatalf("parent = %+v", l)
	}
	if _, ok := tree.List("/docker-volumes/v/_data/index.php"); ok {
		t.Fatal("a file is not a folder")
	}
	nodes, err := decodeLs([]byte(`{"time":"x","paths":["/a"],"message_type":"snapshot","id":"1"}
{"name":"a","type":"dir","path":"/a","message_type":"node"}
{"name":"b.txt","type":"file","path":"/a/b.txt","size":5,"struct_type":"node"}
`))
	if err != nil || len(nodes) != 2 || nodes[1].Size != 5 {
		t.Fatalf("decode = %+v %v", nodes, err)
	}
}

func TestInterruptedDatabaseRestorePointsAtSafetySnapshot(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	h, err := app.Runs.Begin(&Run{Kind: KindRestoreDB, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: map[string]string{"database": "shop", "safetySnapshot": "abcdef1234567890"}})
	if err != nil {
		t.Fatal(err)
	}
	h.setStatus(StatusRunning)
	app.Runs.reconcile(nil)
	run, _ := app.Runs.Get(h.id)
	if run.Status != StatusInterrupted || !strings.Contains(run.Error, `restoring database "shop"`) ||
		!strings.Contains(run.Error, "safety snapshot abcdef12") {
		t.Fatalf("run = %s %q", run.Status, run.Error)
	}
}

func TestReconcileStartsContainersARestoreLeftStopped(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: false})
	fd := &fakeDocker{}
	app.Docker = fd
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	h, err := app.Runs.Begin(&Run{Kind: KindRestore, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: map[string]string{stoppedContainersParam: "aaa111,bbb222"}})
	if err != nil {
		t.Fatal(err)
	}
	h.setStatus(StatusRunning)
	app.Reconcile()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fd.mu.Lock()
		got := strings.Join(fd.started, ",")
		fd.mu.Unlock()
		if got == "aaa111,bbb222" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("containers not started again: %v", fd.started)
}
