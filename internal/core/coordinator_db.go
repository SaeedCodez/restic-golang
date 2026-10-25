package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file holds the coordinator's database runs. A database is backed up by
// streaming a dump made inside its container (docker exec) into
// `restic backup --stdin`. A restore first takes a safety snapshot of the
// database as it is. PostgreSQL then imports into a temporary database and
// swaps it in only once the whole dump went in, so the live database is never
// left half-restored. MySQL and MariaDB have no database rename, so the
// database is dropped, created empty and imported, and if that fails the
// safety snapshot is put back.

// backupDatabase dumps database `name` from cont into the repository. The dump
// is a single file, <name>.sql, in its snapshot.
//
// The dump is written to a spool file in the data directory first and only
// then streamed into restic. Fed straight into restic, the dump tool would
// stall whenever restic waits on slow storage, and MariaDB drops a client that
// stops reading for net_write_timeout (60s by default; mariadb-dump cannot
// raise it). Spooling also keeps the dump's transaction short, and a failed
// dump never reaches restic at all.
func (c *Coordinator) backupDatabase(ctx context.Context, h *runHandle, repo *Repository, cont Container, engine, name string, tags []string, sink RunSink) (int, error) {
	cmd, err := dumpCmd(engine, name)
	if err != nil {
		return 0, err
	}
	dir := filepath.Join(c.app.DataDir, spoolDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("could not create the dump spool directory: %w", err)
	}
	spool, err := os.CreateTemp(dir, name+"-*.sql")
	if err != nil {
		return 0, fmt.Errorf("could not create the dump spool file: %w", err)
	}
	defer os.Remove(spool.Name())
	defer spool.Close()

	h.Log("info", "system", fmt.Sprintf("Dumping %s database %q from container %s", engineLabel(engine), name, cont.Name))
	started := time.Now()
	tail := &tailWriter{w: spool}
	if err := c.dbCommand(ctx, h, cont.ID, "the database dump", cmd, nil, tail); err != nil {
		return 0, err
	}
	// The tool exited 0; still, a dump without its closing marker was cut
	// short and must not be saved.
	if !dumpComplete(engine, tail.bytes()) {
		return 0, errors.New("the database dump ended without its completion marker, so it is incomplete")
	}
	size, err := spool.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	h.Log("info", "system", fmt.Sprintf("Dump complete (%.1f MB in %s); storing it in the repository…",
		float64(size)/1e6, time.Since(started).Round(time.Second)))
	return c.runner.BackupStdin(ctx, repo, dumpFileName(name), tags, func(w io.Writer) error {
		_, err := io.Copy(w, spool)
		return err
	}, sink)
}

// spoolDir (under the data directory) holds database dumps on their way into
// restic. Anything left there is from a run that died and is removed on start.
const spoolDir = "spool"

// resolveDatabaseContainer finds a source's container, noting (and recording)
// a new name after Coolify recreated it.
func (c *Coordinator) resolveDatabaseContainer(ctx context.Context, h *runHandle, db Database) (Container, error) {
	cont, err := c.app.Discovery.ResolveDatabaseContainer(ctx, db)
	if err != nil {
		return Container{}, err
	}
	if cont.Name != db.Container {
		h.Log("info", "system", fmt.Sprintf("The database container is now %s (was %s).", cont.Name, db.Container))
		c.app.Databases.SetContainer(db.ID, cont.Name)
	}
	return cont, nil
}

// dbExitError is a database command that ran and exited non-zero.
type dbExitError struct {
	what string
	code int
	last string
}

func (e *dbExitError) Error() string {
	msg := fmt.Sprintf("%s failed (exit code %d)", e.what, e.code)
	if e.last != "" {
		msg += ": " + e.last
	}
	return msg
}

// execTokenVar marks every process a database command starts, so it can be
// found and ended if the command is abandoned: Docker does not stop an exec'd
// process when its connection closes.
const execTokenVar = "RESTIC_WEB_EXEC"

// killExecCmd ends the processes carrying the given token.
func killExecCmd(token string) []string {
	return []string{"sh", "-c", `for p in /proc/[0-9]*; do
  if tr '\000' '\n' 2>/dev/null < "$p/environ" | grep -qx "` + execTokenVar + `=$1"; then kill -TERM "${p#/proc/}" 2>/dev/null; fi
done`, "sh", token}
}

// dbCommand runs one database command in its container. Its stderr goes to the
// run log; a non-zero exit is a *dbExitError quoting its last message. If the
// command is abandoned (the run stopped, its output or input broke), its
// processes in the container are ended so they do not run on unseen.
func (c *Coordinator) dbCommand(ctx context.Context, h *runHandle, containerID, what string, cmd []string, stdin io.Reader, stdout io.Writer) error {
	token := randomToken()
	full := append([]string{"env", execTokenVar + "=" + token}, cmd...)
	errs := newLineLogger(h, "warn", "stderr", 200)
	code, err := c.app.Docker.Exec(ctx, containerID, full, stdin, stdout, errs)
	errs.flush()
	if err != nil {
		kctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		_, _ = c.app.Docker.Exec(kctx, containerID, killExecCmd(token), nil, nil, nil)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s could not run: %w", what, err)
	}
	if code != 0 {
		return &dbExitError{what: what, code: code, last: errs.last()}
	}
	return nil
}

func randomToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// dbOutput runs a database command and returns its stdout.
func (c *Coordinator) dbOutput(ctx context.Context, h *runHandle, containerID, what string, cmd []string) (string, error) {
	var out bytes.Buffer
	if err := c.dbCommand(ctx, h, containerID, what, cmd, nil, &out); err != nil {
		return "", err
	}
	return out.String(), nil
}

// databaseKey identifies one database on one server across runs, whatever
// container name it has now.
func databaseKey(db Database, name string) string {
	server := db.Container
	if db.OwnerUUID != "" {
		server = db.OwnerKind + ":" + db.OwnerUUID + ":" + db.OwnerService
	}
	return server + "/" + name
}

// databaseBusy refuses to start a database run while another backup or
// restore of the same database is in progress, in any repository and in any
// process: a backup taken mid-restore would save an empty or partial dump.
func (c *Coordinator) databaseBusy(key, name string) error {
	for _, r := range c.store.ActiveRuns() {
		if r.Params["databaseKey"] == key {
			return conflictf("database %q is busy: a %s (run %s) is already running on it. Wait for it to finish or stop it.",
				name, r.Kind, r.ID)
		}
	}
	return nil
}

// RestoreDatabaseOptions shapes a database restore.
type RestoreDatabaseOptions struct {
	// Into restores into a database of this name on the same server instead of
	// the source's own database (e.g. "shop_restored", to inspect a backup
	// without touching the live data).
	Into string
	// SafetyBackup snapshots the target database before it is replaced, and is
	// what a failed MySQL/MariaDB import is rolled back from.
	SafetyBackup bool
}

// StartRestoreDatabase replaces a database with the dump in a snapshot, as a
// tracked run. The result is exactly the database as it was backed up (owner,
// character set and, on PostgreSQL, settings and privileges kept).
func (c *Coordinator) StartRestoreDatabase(repoID, snapshotID, databaseID string, opts RestoreDatabaseOptions) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	db, ok := c.app.Databases.Get(databaseID)
	if !ok {
		return nil, notFoundf("database source not found")
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, validf("please choose a snapshot to restore")
	}
	target := strings.TrimSpace(opts.Into)
	if target == "" {
		target = db.DatabaseName
	}
	if err := validateDBName(target); err != nil {
		return nil, validf("%s", err.Error())
	}
	if isSystemDatabase(db.Engine, target) {
		return nil, validf("%q is a system database and cannot be replaced", target)
	}
	key := databaseKey(db, target)
	if err := c.databaseBusy(key, target); err != nil {
		return nil, err
	}

	dest := db
	dest.DatabaseName = target
	run := &Run{
		Kind: KindRestoreDB, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name,
		FolderPath: databaseLabel(dest),
		Params: map[string]string{
			"snapshotId":   snapshotID,
			"databaseId":   db.ID,
			"databaseKey":  key,
			"database":     target,
			"engine":       db.Engine,
			"container":    db.Container,
			"safetyBackup": strconv.FormatBool(opts.SafetyBackup),
		},
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		h.Log("info", "system", "Reading snapshot "+shortID(snapshotID)+" to plan the restore…")
		snaps, err := c.listSnapshots(ctx, h, &repo, "")
		if err != nil {
			return 0, fmt.Errorf("could not read snapshot %s to plan the restore: %w", shortID(snapshotID), err)
		}
		snap, err := matchSnapshot(snaps, snapshotID)
		if err != nil {
			return 0, err
		}
		file, err := dumpFileOf(snap, db.Engine)
		if err != nil {
			return 0, err
		}
		cont, err := c.resolveDatabaseContainer(ctx, h, db)
		if err != nil {
			return 0, err
		}

		metaCmd, err := dbMetaCmd(db.Engine, target)
		if err != nil {
			return 0, err
		}
		metaOut, err := c.dbOutput(ctx, h, cont.ID, "reading the database settings", metaCmd)
		if err != nil {
			return 0, err
		}
		meta := parseDBMeta(db.Engine, metaOut)

		safetyID := ""
		if opts.SafetyBackup && meta.Exists {
			h.Log("info", "system", fmt.Sprintf("Taking a safety backup of %q before replacing it…", target))
			tags := append([]string{dbSnapshotTag(db.Engine), preRestoreTag}, c.jobTagsForDatabase(db, target)...)
			sub := &stepSink{h: h}
			code, err := c.backupDatabase(ctx, h, &repo, cont, db.Engine, target, tags, sub)
			if err != nil || (code != 0 && code != resticExitWarnings) || sub.snapshotID() == "" {
				if ctx.Err() != nil {
					return resticExitInterrupted, nil
				}
				msg := "the safety backup did not finish"
				if err != nil {
					msg += ": " + err.Error()
				} else if code != 0 {
					msg += fmt.Sprintf(" (restic exited with code %d)", code)
				}
				return 0, errors.New(msg + "; the database was not changed")
			}
			safetyID = sub.snapshotID()
			c.app.Runs.mutate(h.id, true, func(r *Run) { r.Params["safetySnapshot"] = safetyID })
			h.Log("ok", "system", "Safety backup saved as snapshot "+shortID(safetyID)+".")
		} else if !meta.Exists {
			h.Log("info", "system", fmt.Sprintf("Database %q does not exist yet; it will be created.", target))
		}

		var total int64
		if snap.SizeBytes != nil {
			total = *snap.SizeBytes
		}
		r := dbRestore{c: c, h: h, repo: &repo, cont: cont, engine: db.Engine, name: target, meta: meta, total: total}
		started := time.Now()
		h.Log("info", "system", fmt.Sprintf("Restoring database %q from %s in snapshot %s…", target, strings.TrimPrefix(file, "/"), shortID(snap.ID)))

		if db.Engine == EnginePostgres {
			tmp := "rwrestore_" + randomToken()[:8]
			c.app.Runs.mutate(h.id, true, func(r *Run) { r.Params["tempDatabase"] = tmp })
			n, err := r.postgres(ctx, snap.ID, file, tmp)
			if err != nil {
				return r.failed(ctx, err, safetyID)
			}
			return r.succeeded(n, started, snap.ID)
		}

		n, changed, err := r.replace(ctx, snap.ID, file)
		if err == nil {
			return r.succeeded(n, started, snap.ID)
		}
		if !changed {
			return r.failed(ctx, fmt.Errorf("%v; the database was not changed", err), "")
		}
		if safetyID == "" {
			if meta.Exists {
				return r.failed(ctx, fmt.Errorf("%v; the database %q may be incomplete", err, target), "")
			}
			return r.failed(ctx, err, "")
		}
		// The database is dropped or half-imported now. Put it back even if the
		// run was stopped: leaving it broken is worse than finishing late.
		h.Log("warn", "system", fmt.Sprintf("Restore failed (%v). Putting %q back from safety snapshot %s…", err, target, shortID(safetyID)))
		if _, _, rerr := r.replace(context.WithoutCancel(ctx), safetyID, "/"+dumpFileName(target)); rerr != nil {
			return r.failed(ctx, fmt.Errorf("%v; putting the database back also failed: %v. Its previous contents are in snapshot %s", err, rerr, shortID(safetyID)), "")
		}
		h.Log("ok", "system", fmt.Sprintf("Database %q is back as it was before the restore.", target))
		return r.failed(ctx, fmt.Errorf("%v; the database was put back as it was before (safety snapshot %s)", err, shortID(safetyID)), "")
	})
}

// dbRestore is one database restore in progress.
type dbRestore struct {
	c      *Coordinator
	h      *runHandle
	repo   *Repository
	cont   Container
	engine string
	name   string
	meta   dbMeta
	total  int64
}

func (r *dbRestore) succeeded(n int64, started time.Time, snapshotID string) (int, error) {
	r.h.Summary(Summary{BytesRestored: n, TotalBytesProcessed: n, TotalDuration: time.Since(started).Seconds()})
	r.h.Log("ok", "system", fmt.Sprintf("Database %q restored from snapshot %s.", r.name, shortID(snapshotID)))
	return 0, nil
}

// failed ends the run with err. A stopped run still says what state the
// database is in, so the message is kept rather than reduced to "stopped".
func (r *dbRestore) failed(ctx context.Context, err error, safetyID string) (int, error) {
	if safetyID != "" {
		err = fmt.Errorf("%w (a safety snapshot of the database before the restore is %s)", err, shortID(safetyID))
	}
	if ctx.Err() != nil {
		return resticExitInterrupted, fmt.Errorf("stopped: %w", err)
	}
	return 0, fmt.Errorf("restore failed: %w", err)
}

// importInto streams file from the snapshot into the database `into`, which
// must exist and be empty. It returns the bytes imported. A non-zero restic
// exit means the stream was cut short, so the import does not count even if
// the client accepted it.
func (r *dbRestore) importInto(ctx context.Context, snapshotID, file, into string) (int64, error) {
	imp, err := importCmd(r.engine, into)
	if err != nil {
		return 0, err
	}
	counter := &progressReader{h: r.h, total: r.total}
	code, err := r.c.runner.Dump(ctx, r.repo, snapshotID, file, func(rd io.Reader) error {
		counter.r = rd
		return r.c.dbCommand(ctx, r.h, r.cont.ID, "the import", imp, counter, nil)
	}, r.h)
	counter.report()
	if err != nil {
		return counter.n, err
	}
	if code != 0 {
		if code == resticExitInterrupted || ctx.Err() != nil {
			return counter.n, errors.New("the restore was stopped")
		}
		return counter.n, fmt.Errorf("restic could not read the dump from the snapshot (exit code %d)", code)
	}
	return counter.n, nil
}

// postgres imports into the temporary database tmp and, once all of the dump
// is in, swaps it in for the live database. Until the swap the live database
// is untouched; a failure before it only removes tmp.
func (r *dbRestore) postgres(ctx context.Context, snapshotID, file, tmp string) (int64, error) {
	drop, err := dropCmd(r.engine, tmp)
	if err != nil {
		return 0, err
	}
	cleanup := func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_ = r.c.dbCommand(cctx, r.h, r.cont.ID, "removing the temporary database", drop, nil, nil)
	}
	create, err := createCmd(r.engine, tmp, r.meta)
	if err != nil {
		return 0, err
	}
	if err := r.c.dbCommand(ctx, r.h, r.cont.ID, "creating the temporary database", create, nil, nil); err != nil {
		return 0, fmt.Errorf("%v; the database was not changed", err)
	}
	r.h.Log("info", "system", fmt.Sprintf("Importing into the temporary database %s; %q stays online until it is complete.", tmp, r.name))
	n, err := r.importInto(ctx, snapshotID, file, tmp)
	if err == nil && r.meta.Exists {
		var props []string
		if props, err = pgCopyPropertiesCmd(r.name, tmp); err == nil {
			err = r.c.dbCommand(ctx, r.h, r.cont.ID, "copying the database settings", props, nil, nil)
		}
	}
	if err != nil {
		cleanup()
		return n, fmt.Errorf("%v; the database was not changed", err)
	}

	swap, err := pgSwapCmd(tmp, r.name, r.meta.Exists)
	if err != nil {
		cleanup()
		return n, err
	}
	// The swap is quick and must not be left half done, so a stop does not
	// interrupt it.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	r.h.Log("info", "system", fmt.Sprintf("Replacing %q with the restored database…", r.name))
	if err := r.c.dbCommand(sctx, r.h, r.cont.ID, "swapping in the restored database", swap, nil, nil); err == nil {
		return n, nil
	} else {
		return n, r.recoverSwap(sctx, tmp, err)
	}
}

// recoverSwap handles a swap that failed: if the live database is still there
// (its DROP failed), it accepts connections again and the restored copy is
// removed; if it is gone, the rename is retried.
func (r *dbRestore) recoverSwap(ctx context.Context, tmp string, swapErr error) error {
	metaCmd, _ := dbMetaCmd(r.engine, r.name)
	out, err := r.c.dbOutput(ctx, r.h, r.cont.ID, "checking the database after a failed swap", metaCmd)
	if err == nil && parseDBMeta(r.engine, out).Exists {
		if allow, aerr := pgAllowConnectionsCmd(r.name); aerr == nil {
			_ = r.c.dbCommand(ctx, r.h, r.cont.ID, "reopening the database", allow, nil, nil)
		}
		if drop, derr := dropCmd(r.engine, tmp); derr == nil {
			_ = r.c.dbCommand(ctx, r.h, r.cont.ID, "removing the temporary database", drop, nil, nil)
		}
		return fmt.Errorf("%v; the database was not changed", swapErr)
	}
	if rename, rerr := pgSwapCmd(tmp, r.name, false); rerr == nil {
		if r.c.dbCommand(ctx, r.h, r.cont.ID, "renaming the restored database", rename, nil, nil) == nil {
			return nil
		}
	}
	return fmt.Errorf("%v; %q was dropped but the restored copy is still named %s: rename it with ALTER DATABASE %s RENAME TO %s", swapErr, r.name, tmp, tmp, r.name)
}

// replace drops the database, creates it empty and imports file into it
// (MySQL/MariaDB). changed reports whether the database was touched at all:
// if the drop did not go through, it is as it was.
func (r *dbRestore) replace(ctx context.Context, snapshotID, file string) (n int64, changed bool, err error) {
	drop, err := dropCmd(r.engine, r.name)
	if err != nil {
		return 0, false, err
	}
	create, err := createCmd(r.engine, r.name, r.meta)
	if err != nil {
		return 0, false, err
	}
	if ctx.Err() != nil {
		return 0, false, errors.New("the restore was stopped")
	}
	if err := r.c.dbCommand(ctx, r.h, r.cont.ID, "dropping the database", drop, nil, nil); err != nil {
		var exit *dbExitError
		// It ran and failed: the server refused the DROP, nothing changed.
		return 0, !errors.As(err, &exit), err
	}
	if err := r.c.dbCommand(ctx, r.h, r.cont.ID, "creating the database", create, nil, nil); err != nil {
		return 0, true, err
	}
	n, err = r.importInto(ctx, snapshotID, file, r.name)
	return n, true, err
}

// dumpFileOf returns the dump file inside a database snapshot, checking that
// the snapshot holds a dump of the same engine. MySQL and MariaDB dumps are
// close but not interchangeable (sandbox markers, collations), so a cross
// restore is refused before anything is dropped.
func dumpFileOf(snap Snapshot, engine string) (string, error) {
	src := snapshotDBEngine(snap.Tags)
	if src == "" {
		return "", fmt.Errorf("snapshot %s is not a database backup", shortID(snap.ID))
	}
	if src != engine {
		return "", fmt.Errorf("snapshot %s is a %s dump and cannot be restored into %s", shortID(snap.ID), engineLabel(src), engineLabel(engine))
	}
	if len(snap.Paths) != 1 || !strings.HasSuffix(snap.Paths[0], ".sql") {
		return "", fmt.Errorf("snapshot %s does not hold a single database dump", shortID(snap.ID))
	}
	return snap.Paths[0], nil
}

// jobTagsForDatabase returns the tags of the jobs that back up database `name`
// on the source's server, so a safety snapshot shows up on their pages.
func (c *Coordinator) jobTagsForDatabase(db Database, name string) []string {
	var tags []string
	for _, s := range c.app.Databases.List() {
		sameServer := s.ID == db.ID || s.Container == db.Container ||
			(s.OwnerUUID != "" && s.OwnerKind == db.OwnerKind && s.OwnerUUID == db.OwnerUUID && s.OwnerService == db.OwnerService)
		if !sameServer || s.DatabaseName != name {
			continue
		}
		for _, j := range c.app.JobsUsingDatabase(s.ID) {
			tags = append(tags, j.ResticTag())
		}
	}
	return tags
}

// tailWriter passes writes through and keeps the last bytes written, so the
// end of a dump can be checked once it is complete.
type tailWriter struct {
	w    io.Writer
	tail []byte
}

const tailKeep = 4096

func (t *tailWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.tail = append(t.tail, p[:n]...)
	if len(t.tail) > tailKeep {
		t.tail = t.tail[len(t.tail)-tailKeep:]
	}
	return n, err
}

func (t *tailWriter) bytes() []byte { return t.tail }

// stepSink is the sink for a backup taken inside another run (the safety
// backup of a restore): its log lines and process id go to the run, but its
// summary is kept aside so it does not read as the run's own result.
type stepSink struct {
	h       *runHandle
	mu      sync.Mutex
	summary *Summary
}

func (s *stepSink) Log(level, stream, message string) { s.h.Log(level, stream, message) }
func (s *stepSink) Progress(p Progress)               { s.h.Progress(p) }
func (s *stepSink) PID(pid int, startToken string)    { s.h.PID(pid, startToken) }
func (s *stepSink) Summary(sm Summary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary = &sm
}

func (s *stepSink) snapshotID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.summary == nil {
		return ""
	}
	return s.summary.SnapshotID
}

// progressReader reports bytes streamed into an import as run progress, at
// most twice a second.
type progressReader struct {
	r     io.Reader
	h     *runHandle
	n     int64
	total int64
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if time.Since(p.last) >= 500*time.Millisecond {
		p.report()
	}
	return n, err
}

func (p *progressReader) report() {
	p.last = time.Now()
	pr := Progress{BytesDone: p.n, TotalBytes: p.total}
	if p.total > 0 {
		pr.Percent = float64(p.n) / float64(p.total)
		if pr.Percent > 1 {
			pr.Percent = 1
		}
	}
	p.h.Progress(pr)
}

// lineLogger is an io.Writer that logs each line written to it, up to max
// lines (then counts the rest), and remembers the last one, preferring the
// last error line (psql ends with a context line and a "^" marker).
type lineLogger struct {
	sink          RunSink
	level, stream string
	max, logged   int
	dropped       int
	buf           []byte
	lastLine      string
	lastError     string
}

func newLineLogger(sink RunSink, level, stream string, max int) *lineLogger {
	return &lineLogger{sink: sink, level: level, stream: stream, max: max}
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.line(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > maxLogLine {
		l.line(string(l.buf))
		l.buf = l.buf[:0]
	}
	return len(p), nil
}

func (l *lineLogger) line(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	l.lastLine = s
	if strings.Contains(s, "ERROR") || strings.Contains(s, "FATAL") || strings.HasPrefix(strings.ToLower(s), "error") {
		l.lastError = s
	}
	if l.logged >= l.max {
		l.dropped++
		return
	}
	l.logged++
	l.sink.Log(l.level, l.stream, clipLogLine(s))
}

// flush logs a trailing partial line and how many lines were not logged.
func (l *lineLogger) flush() {
	if len(l.buf) > 0 {
		l.line(string(l.buf))
		l.buf = nil
	}
	if l.dropped > 0 {
		l.sink.Log(l.level, "system", fmt.Sprintf("… %d more line(s) not shown.", l.dropped))
		l.dropped = 0
	}
}

func (l *lineLogger) last() string {
	if l.lastError != "" {
		return l.lastError
	}
	return l.lastLine
}
