package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"restic-web/internal/core"
)

func (c *CLI) cmdRepo(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpRepo())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "list", "ls":
		return c.repoList()
	case "get", "show":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoGet(args[1])
	case "create", "add":
		return c.repoCreate(args[1:])
	case "update", "edit":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoDelete(args[1])
	case "test":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoTest(args[1])
	case "init":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		_, wait, follow := parseWaitFollow(args[2:])
		return c.repoInit(args[1], wait, follow)
	case "unlock":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoUnlock(args[1])
	case "check":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoCheck(args[1], args[2:])
	case "snapshots", "snaps":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoSnapshots(args[1])
	case "restore":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoRestore(args[1], args[2:])
	case "download":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoDownload(args[1], args[2:])
	case "restore-db":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoRestoreDB(args[1], args[2:])
	case "files", "browse":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoFiles(args[1], args[2:])
	case "forget":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoForget(args[1], args[2:])
	case "reset":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		_, wait, follow := parseWaitFollow(args[2:])
		return c.repoReset(args[1], wait, follow)
	default:
		return c.fail(usagef("unknown repo command %q\n\n%s", args[0], helpRepo()))
	}
}

func (c *CLI) repoList() int {
	items := c.app.Repos.List()
	views := make([]repoView, 0, len(items))
	for _, it := range items {
		views = append(views, repoViewOf(it))
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "repositories": views})
	}
	rows := make([][]string, 0, len(views))
	for _, it := range views {
		loc := it.LocalPath
		if it.BackendType == "S3" {
			loc = it.Endpoint + "/" + it.Bucket
		}
		rows = append(rows, []string{
			it.ID,
			it.Name,
			it.BackendType,
			loc,
			yn(it.HasPassword),
		})
	}
	c.printTable([]string{"ID", "NAME", "BACKEND", "LOCATION", "PASSWORD"}, rows)
	return exitOK
}

func (c *CLI) repoGet(query string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	v := repoViewOf(repo)
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "repository": v})
	}
	fmt.Printf("id:\t%s\n", v.ID)
	fmt.Printf("name:\t%s\n", v.Name)
	fmt.Printf("backendType:\t%s\n", v.BackendType)
	if v.BackendType == "Local" {
		fmt.Printf("localPath:\t%s\n", v.LocalPath)
	} else {
		fmt.Printf("endpoint:\t%s\n", v.Endpoint)
		fmt.Printf("bucket:\t%s\n", v.Bucket)
		fmt.Printf("region:\t%s\n", dash(v.Region))
		fmt.Printf("accessKey:\t%s\n", dash(v.AccessKey))
		fmt.Printf("hasSecretKey:\t%v\n", v.HasSecretKey)
	}
	fmt.Printf("hasPassword:\t%v\n", v.HasPassword)
	return exitOK
}

func (c *CLI) repoCreate(args []string) int {
	fs := newFlagSet("repo create")
	name := fs.String("name", "", "repository name")
	backend := fs.String("backend", "", "Local or S3")
	pass := fs.String("password", "", "restic repository password")
	path := fs.String("path", "", "local repository directory")
	endpoint := fs.String("endpoint", "", "S3 endpoint URL")
	bucket := fs.String("bucket", "", "S3 bucket")
	region := fs.String("region", "", "S3 region")
	accessKey := fs.String("access-key", "", "S3 access key")
	secretKey := fs.String("secret-key", "", "S3 secret key")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	b := strings.TrimSpace(*backend)
	if b == "local" {
		b = "Local"
	}
	if b == "s3" {
		b = "S3"
	}
	if *name == "" || b == "" || *pass == "" {
		return c.fail(usagef("--name, --backend, and --password are required\n\n%s", helpRepo()))
	}
	repo := core.Repository{
		Meta:        core.Meta{Name: *name},
		BackendType: b,
		Password:    *pass,
	}
	switch b {
	case "Local":
		if *path == "" {
			return c.fail(usagef("--path is required for Local backend"))
		}
		repo.LocalPath = *path
	case "S3":
		if *endpoint == "" || *bucket == "" || *accessKey == "" || *secretKey == "" {
			return c.fail(usagef("S3 requires --endpoint --bucket --access-key --secret-key"))
		}
		repo.Endpoint = *endpoint
		repo.Bucket = *bucket
		repo.Region = *region
		repo.AccessKey = *accessKey
		repo.SecretKey = *secretKey
	default:
		return c.fail(usagef(`--backend must be "Local" or "S3"`))
	}
	if err := repo.Validate(); err != nil {
		return c.fail(&core.ValidationError{Msg: err.Error()})
	}
	created, err := c.app.Repos.Create(repo)
	if err != nil {
		return c.fail(err)
	}
	v := repoViewOf(created)
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "repository": v})
	}
	c.note("Created repository %s (%s)", created.Name, created.ID)
	fmt.Println(created.ID)
	return exitOK
}

func (c *CLI) repoUpdate(query string, args []string) int {
	existing, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("repo update")
	name := fs.String("name", "", "new name")
	backend := fs.String("backend", "", "Local or S3")
	pass := fs.String("password", "", "new restic password (omit to keep)")
	path := fs.String("path", "", "local path")
	endpoint := fs.String("endpoint", "", "S3 endpoint")
	bucket := fs.String("bucket", "", "S3 bucket")
	region := fs.String("region", "", "S3 region")
	accessKey := fs.String("access-key", "", "S3 access key")
	secretKey := fs.String("secret-key", "", "S3 secret key")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}

	upd := existing
	if *name != "" {
		upd.Name = *name
	}
	if *backend != "" {
		b := *backend
		if b == "local" {
			b = "Local"
		}
		if b == "s3" {
			b = "S3"
		}
		upd.BackendType = b
	}
	if *path != "" {
		upd.LocalPath = *path
	}
	if *endpoint != "" {
		upd.Endpoint = *endpoint
	}
	if *bucket != "" {
		upd.Bucket = *bucket
	}
	if *region != "" {
		upd.Region = *region
	}
	if *accessKey != "" {
		upd.AccessKey = *accessKey
	}
	if *pass != "" {
		upd.Password = *pass
	}
	if *secretKey != "" {
		upd.SecretKey = *secretKey
	}
	if err := upd.Validate(); err != nil {
		return c.fail(&core.ValidationError{Msg: err.Error()})
	}
	updated, err := c.app.Repos.Update(existing.ID, upd)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "repository": repoViewOf(updated)})
	}
	return c.okMessage("Repository updated.", map[string]any{"id": existing.ID})
}

func (c *CLI) repoDelete(query string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	if using := c.app.JobsUsingRepository(repo.ID); len(using) > 0 {
		return c.fail(&core.ConflictError{
			Msg: "This repository is used by " + jobNames(using) + ". Delete or edit those jobs first.",
		})
	}
	if err := c.app.Repos.Delete(repo.ID); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted repository %s.", repo.Name), map[string]any{"id": repo.ID})
}

// resticContext bounds a direct restic call by --timeout and ends it on Ctrl-C
// or SIGTERM. restic runs in its own process group, so without this an
// interrupted CLI would exit and leave restic running with its lock held;
// canceling the context instead sends restic SIGINT so it removes the lock.
func (c *CLI) resticContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if c.cfg.timeout <= 0 {
		return ctx, stop
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.timeout)
	return ctx, func() { cancel(); stop() }
}

// resticCallError turns a failed direct restic call into a CLI error. A timeout
// gets its own code and says how to raise the limit.
func (c *CLI) resticCallError(code string, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &apiError{Code: "timeout", Message: err.Error() + ". " + c.timeoutHint()}
	case errors.Is(err, context.Canceled):
		return &apiError{Code: "interrupted", Message: err.Error()}
	}
	return &apiError{Code: code, Message: err.Error()}
}

func (c *CLI) timeoutHint() string {
	return fmt.Sprintf("The limit is --timeout %s and the repository may just be slow: retry with a longer --timeout, or --timeout 0 for no limit.", c.cfg.timeout)
}

func (c *CLI) repoTest(query string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := c.resticContext()
	defer cancel()
	res := c.app.Runner.Test(ctx, &repo)
	m := map[string]any{
		"ok":          res.OK,
		"initialized": res.Initialized,
		"message":     res.Message,
	}
	if !res.OK && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		m["code"] = "timeout"
		res.Message += " " + c.timeoutHint()
		m["message"] = res.Message
	}
	if res.Detail != "" {
		m["detail"] = res.Detail
	}
	if c.cfg.json {
		if code := c.writeJSON(m); code != exitOK || res.OK {
			return code
		}
		return exitError
	}
	fmt.Printf("ok:\t%v\n", res.OK)
	fmt.Printf("initialized:\t%v\n", res.Initialized)
	fmt.Printf("message:\t%s\n", dash(res.Message))
	if res.Detail != "" {
		fmt.Printf("detail:\t%s\n", res.Detail)
	}
	if !res.OK {
		return exitError
	}
	return exitOK
}

func (c *CLI) repoInit(query string, wait, follow bool) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	run, err := c.app.Coord.StartInit(repo.ID)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) repoUnlock(query string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := c.resticContext()
	defer cancel()
	if err := c.app.Runner.Unlock(ctx, &repo); err != nil {
		return c.fail(c.resticCallError("unlock_failed", err))
	}
	return c.okMessage("Removed any stale locks.", nil)
}

func (c *CLI) repoCheck(query string, args []string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	args, readAll := hasBoolFlag(args, "--read-data")
	fs := newFlagSet("repo check")
	subset := fs.String("read-data-subset", "", "read a subset of data packs (n/t, x%, or a size)")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	readData := *subset
	if readAll {
		if readData != "" {
			return c.fail(usagef("use either --read-data or --read-data-subset, not both"))
		}
		readData = "all"
	}
	run, err := c.app.Coord.StartCheck(repo.ID, readData)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) repoSnapshots(query string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	return c.printSnapshots(&repo, "")
}

func (c *CLI) printSnapshots(repo *core.Repository, tag string) int {
	ctx, cancel := c.resticContext()
	defer cancel()
	snaps, err := c.app.Runner.Snapshots(ctx, repo, tag)
	if err != nil {
		if err.Error() == "repository is not initialized" {
			return c.fail(&apiError{Code: "not_initialized", Message: "No repository found here yet. Initialize it first."})
		}
		return c.fail(c.resticCallError("restic_error", err))
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "snapshots": snaps})
	}
	rows := [][]string{}
	for _, s := range snaps {
		tags := strings.Join(s.Tags, ",")
		id := s.ShortID
		if id == "" {
			id = s.ID
		}
		t := s.Time
		if parsed, err := time.Parse(time.RFC3339Nano, s.Time); err == nil {
			t = formatTime(parsed)
		} else if parsed, err := time.Parse(time.RFC3339, s.Time); err == nil {
			t = formatTime(parsed)
		}
		rows = append(rows, []string{dash(id), dash(t), dash(s.Hostname), dash(tags)})
	}
	c.printTable([]string{"SNAPSHOT", "TIME", "HOST", "TAGS"}, rows)
	return exitOK
}

func (c *CLI) repoRestore(query string, args []string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("repo restore")
	snap := fs.String("snapshot", "", "snapshot id")
	target := fs.String("target", "", "restore destination directory")
	var include stringList
	fs.Var(&include, "include", "restore only this snapshot path (repeatable)")
	stop := fs.Bool("stop-containers", false, "stop the containers using --target while restoring")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" || *target == "" {
		return c.fail(usagef("--snapshot and --target are required"))
	}
	run, err := c.app.Coord.StartRestoreWith(repo.ID, *snap, *target, core.RestoreOptions{Include: include, StopContainers: *stop})
	return c.startAndMaybeWait(run, err, wait, follow)
}

// repoRestoreDB replaces a database with the dump in a snapshot.
func (c *CLI) repoRestoreDB(query string, args []string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("repo restore-db")
	snap := fs.String("snapshot", "", "snapshot id")
	dbRef := fs.String("db-source", "", "database source id or name")
	into := fs.String("into", "", "restore into this database name instead (same server)")
	noSafety := fs.Bool("no-safety-backup", false, "do not snapshot the current database first")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" || *dbRef == "" {
		return c.fail(usagef("--snapshot and --db-source are required"))
	}
	db, err := c.resolveDatabase(*dbRef)
	if err != nil {
		return c.fail(err)
	}
	run, err := c.app.Coord.StartRestoreDatabase(repo.ID, *snap, db.ID, core.RestoreDatabaseOptions{Into: *into, SafetyBackup: !*noSafety})
	return c.startAndMaybeWait(run, err, wait, follow)
}

// repoFiles lists one folder of a snapshot.
func (c *CLI) repoFiles(query string, args []string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("repo files")
	snap := fs.String("snapshot", "", "snapshot id")
	dir := fs.String("path", "/", "folder inside the snapshot")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" {
		return c.fail(usagef("--snapshot is required"))
	}
	ctx, cancel := c.resticContext()
	defer cancel()
	tree, err := c.app.SnapshotTree(ctx, &repo, *snap)
	if err != nil {
		return c.fail(c.resticCallError("restic_error", err))
	}
	entries, ok := tree.List(*dir)
	if !ok {
		return c.fail(&apiError{Code: "not_found", Message: "no such folder in this snapshot: " + *dir})
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "path": *dir, "entries": entries})
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{e.Type, fmt.Sprintf("%d", e.Size), e.Path})
	}
	c.printTable([]string{"TYPE", "SIZE", "PATH"}, rows)
	return exitOK
}

func (c *CLI) repoDownload(query string, args []string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("repo download")
	snap := fs.String("snapshot", "", "snapshot id")
	var include stringList
	fs.Var(&include, "include", "download only this snapshot path (repeatable)")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" {
		return c.fail(usagef("--snapshot is required"))
	}
	run, err := c.app.Coord.StartDownloadWith(repo.ID, *snap, include)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) repoForget(query string, args []string) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("repo forget")
	snap := fs.String("snapshot", "", "snapshot id")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" {
		return c.fail(usagef("--snapshot is required"))
	}
	run, err := c.app.Coord.StartForgetSnapshot(repo.ID, *snap)
	return c.startAndMaybeWait(run, err, wait, follow)
}

func (c *CLI) repoReset(query string, wait, follow bool) int {
	repo, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	run, err := c.app.Coord.StartResetRepo(repo.ID)
	return c.startAndMaybeWait(run, err, wait, follow)
}
