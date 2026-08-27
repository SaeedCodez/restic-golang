package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// runExec performs the actual restic work for a run, streaming into the handle
// and returning restic's exit code (err only if it could not be started).
type runExec func(ctx context.Context, h *runHandle) (int, error)

// blockingRun describes the run that is holding a repository, for a clear
// "can't start" message.
type blockingRun struct {
	RunID     string    `json:"runId"`
	Kind      RunKind   `json:"kind"`
	JobName   string    `json:"jobName,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}

// BusyError is returned when an operation cannot start because the repository is
// already running one. Handlers turn it into a 409 with the blocker's details.
type BusyError struct {
	RepoName string
	Blocking blockingRun
}

func (e *BusyError) Error() string {
	who := string(e.Blocking.Kind)
	if e.Blocking.JobName != "" {
		who += ` for job "` + e.Blocking.JobName + `"`
	}
	return fmt.Sprintf("Repository %q is busy: a %s is already running. Stop it or wait for it to finish.", e.RepoName, who)
}

// activeRun is the in-memory handle to a currently-running operation. It exists
// only to stream and to stop; it is NEVER consulted to answer "is it running" —
// that is always the durable run record.
type activeRun struct {
	runID     string
	kind      RunKind
	repoID    string
	jobName   string
	startedAt time.Time
	cancel    context.CancelFunc
	handle    *runHandle
	stopped   atomic.Bool
}

func (ar *activeRun) info() blockingRun {
	return blockingRun{RunID: ar.runID, Kind: ar.kind, JobName: ar.jobName, StartedAt: ar.startedAt}
}

// Coordinator serializes work per repository (matching the natural unit of
// contention — a repository is what restic operations share) and drives each run
// from start to durable finish.
type Coordinator struct {
	app    *App
	store  *RunStore
	runner Runner
	bus    eventBus

	mu     sync.Mutex
	active map[string]*activeRun // key: repositoryID
}

func newCoordinator(app *App, store *RunStore, runner Runner, bus eventBus) *Coordinator {
	if bus == nil {
		bus = noopBus{}
	}
	return &Coordinator{app: app, store: store, runner: runner, bus: bus, active: map[string]*activeRun{}}
}

// StartBackup starts a manually triggered backup run for a job.
func (c *Coordinator) StartBackup(jobID string) (*Run, error) {
	return c.StartBackupTriggered(jobID, TriggerManual)
}

// StartBackupTriggered starts a backup run for a job with an explicit trigger
// ("manual" or "schedule"), recorded on the run and in its first log line.
func (c *Coordinator) StartBackupTriggered(jobID, trigger string) (*Run, error) {
	if trigger == "" {
		trigger = TriggerManual
	}
	job, folder, repo, err := c.app.resolveJob(jobID)
	if err != nil {
		return nil, err
	}
	run := &Run{
		Kind:         KindBackup,
		Status:       StatusStarting,
		JobID:        job.ID,
		RepositoryID: repo.ID,
		JobName:      job.Name,
		FolderPath:   folder.Path,
		RepoName:     repo.Name,
		Params: map[string]string{
			"source":  folder.Path,
			"tag":     job.ResticTag(),
			"trigger": trigger,
		},
	}
	tag := job.ResticTag()
	src := folder.Path
	schedDesc := ""
	if trigger == TriggerSchedule && job.Schedule != nil {
		schedDesc = job.Schedule.Describe()
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		if trigger == TriggerSchedule {
			if schedDesc != "" {
				h.Log("info", "system", "Started by schedule ("+schedDesc+").")
			} else {
				h.Log("info", "system", "Started by schedule.")
			}
		}
		h.Log("info", "system", fmt.Sprintf("Backing up %s to repository %q", src, repo.Name))
		code, err := c.runner.Backup(ctx, &repo, src, []string{tag}, h)

		// If the repository has not been initialized yet (restic exit code 10),
		// create it and retry once, so running a job "just works" without a
		// separate Initialize step. The step is logged, never silent.
		if err == nil && code == resticExitNotInitialized && ctx.Err() == nil {
			h.Log("warn", "system", "Repository is not initialized yet — initializing it now…")
			if out, ierr := c.runner.Init(ctx, &repo); ierr != nil {
				h.Log("error", "system", "Automatic initialization failed: "+ierr.Error())
				return code, err // keep the original not-initialized failure
			} else if s := strings.TrimSpace(out); s != "" {
				h.Log("info", "stdout", s)
			}
			h.Log("ok", "system", "Repository initialized. Retrying backup…")
			code, err = c.runner.Backup(ctx, &repo, src, []string{tag}, h)
		}
		return code, err
	})
}

// StartInit initializes a repository as a tracked run (repository setup is
// visible the same way as any other long operation).
func (c *Coordinator) StartInit(repoID string) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	run := &Run{Kind: KindInit, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		h.Log("info", "system", "Initializing repository "+repo.Name)
		out, err := c.app.Runner.Init(ctx, &repo)
		if s := strings.TrimSpace(out); s != "" {
			h.Log("info", "stdout", s)
		}
		if err != nil {
			return 0, err
		}
		h.Log("ok", "system", "Repository initialized.")
		return 0, nil
	})
}

// StartRestore restores a snapshot into a target folder as a tracked run.
// The destination is replaced: files from the snapshot overwrite, and files
// that are not in the snapshot are deleted.
//
// When target equals a path stored in the snapshot (typical "restore into this
// job's folder"), restic is invoked with --target / so absolute snapshot paths
// are written back in place instead of nested under target, and --include of
// that folder so --delete cannot touch the rest of the filesystem.
func (c *Coordinator) StartRestore(repoID, snapshotID, target string) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	snapshotID = strings.TrimSpace(snapshotID)
	target = strings.TrimSpace(target)
	if snapshotID == "" {
		return nil, validf("please choose a snapshot to restore")
	}
	if target == "" {
		return nil, validf("please provide a target folder for the restore")
	}

	lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	paths := lookupSnapshotPaths(lookupCtx, c.app.Runner, &repo, snapshotID)
	lookupCancel()
	plan := planResticRestore(target, paths)
	if _, err := restoreArgs(snapshotID, plan.Target, plan.Include); err != nil {
		return nil, validf("%s", err.Error())
	}

	// Ensure the user-facing destination exists. In-place restores use --target /;
	// MkdirAll on the original folder is best-effort (restic creates missing dirs).
	if target != "/" {
		if err := os.MkdirAll(target, 0o755); err != nil {
			if !isResticRootTarget(plan.Target) {
				return nil, validf("could not create target folder: %v", err)
			}
			log.Printf("restore: could not ensure in-place target %s: %v", target, err)
		}
	}

	params := map[string]string{"snapshotId": snapshotID, "target": target}
	if plan.Target != target {
		params["resticTarget"] = plan.Target
	}
	run := &Run{
		Kind: KindRestore, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: params,
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		if isResticRootTarget(plan.Target) && target != "/" {
			h.Log("info", "system", "Restoring "+shortID(snapshotID)+" in place to "+target+" (replacing folder contents)")
		} else {
			h.Log("info", "system", "Restoring "+shortID(snapshotID)+" to "+target+" (replacing destination contents)")
		}
		return c.app.Runner.Restore(ctx, &repo, snapshotID, plan.Target, plan.Include, h)
	})
}

// StartDownload restores a snapshot into an app-managed temp workspace as a
// tracked run; once it succeeds, GET /api/runs/{id}/download streams a zip of
// that workspace. Modeling download as a normal run keeps it uniform — visible,
// cancelable, and repo-serialized — and avoids coupling run liveness to the
// download connection.
func (c *Coordinator) StartDownload(repoID, snapshotID string) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, validf("please choose a snapshot to download")
	}
	run := &Run{
		Kind: KindDownload, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: map[string]string{"snapshotId": snapshotID},
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		target := filepath.Join(c.app.DataDir, "downloads", h.id)
		if err := os.MkdirAll(target, 0o755); err != nil {
			return 0, fmt.Errorf("could not create download workspace: %w", err)
		}
		c.app.Runs.mutate(h.id, true, func(r *Run) { r.Params["target"] = target })
		h.Log("info", "system", "Preparing download of snapshot "+shortID(snapshotID))
		return c.app.Runner.Restore(ctx, &repo, snapshotID, target, nil, h)
	})
}

// startRun reserves the repository, creates the durable run, and launches the
// operation in the background. It returns the created run (or a BusyError).
func (c *Coordinator) startRun(repo Repository, run *Run, exec runExec) (*Run, error) {
	c.mu.Lock()
	if ar := c.active[repo.ID]; ar != nil {
		// A holder whose run has already reached a terminal state is finishing but
		// has not yet released the slot; it no longer blocks a new operation, so a
		// job can be re-run the instant it is seen to have succeeded.
		if held, ok := c.store.Get(ar.runID); !ok || !held.Status.Terminal() {
			c.mu.Unlock()
			return nil, &BusyError{RepoName: repo.Name, Blocking: ar.info()}
		}
	}
	// Durable busy check: another process (e.g. restic-webctl alongside the web
	// server) may already be driving a run for this repository.
	for _, other := range c.store.ActiveRuns() {
		if other.RepositoryID != repo.ID {
			continue
		}
		if ar := c.active[repo.ID]; ar != nil && ar.runID == other.ID {
			continue
		}
		c.mu.Unlock()
		return nil, &BusyError{
			RepoName: repo.Name,
			Blocking: blockingRun{
				RunID:     other.ID,
				Kind:      other.Kind,
				JobName:   other.JobName,
				StartedAt: other.StartedAt,
			},
		}
	}
	handle, err := c.store.Begin(run)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	ar := &activeRun{
		runID:     run.ID,
		kind:      run.Kind,
		repoID:    repo.ID,
		jobName:   run.JobName,
		startedAt: run.StartedAt,
		cancel:    cancel,
		handle:    handle,
	}
	c.active[repo.ID] = ar
	c.mu.Unlock()

	created, _ := c.store.Get(run.ID)
	go c.drive(ctx, ar, exec)
	return created, nil
}

// drive runs the operation to completion and records its terminal state.
// Successful backups may chain a retention run after the repository slot is
// released.
func (c *Coordinator) drive(ctx context.Context, ar *activeRun, exec runExec) {
	var chainRetentionJob string
	defer func() {
		c.finish(ar)
		if chainRetentionJob != "" {
			if _, err := c.StartRetention(chainRetentionJob, TriggerAfterBackup); err != nil {
				var busy *BusyError
				if errors.As(err, &busy) {
					// Another operation grabbed the repo; the next successful
					// backup will apply retention.
					return
				}
				log.Printf("retention: could not start after backup for job %s: %v", chainRetentionJob, err)
			}
		}
	}()

	ar.handle.setStatus(StatusRunning)
	code, err := exec(ctx, ar.handle)
	status, errMsg := classifyRun(ar, ctx, code, err)

	if errMsg != "" {
		ar.handle.Log("error", "system", errMsg)
	}
	ar.handle.Log(logLevelFor(status), "system", terminalMessage(status))
	ar.handle.finalize(status, code, errMsg)

	if ar.kind == KindBackup && (status == StatusSuccess || status == StatusSuccessWarnings) {
		if run, ok := c.store.Get(ar.runID); ok && run.JobID != "" {
			if job, ok := c.app.Jobs.Get(run.JobID); ok && job.Retention != nil && job.Retention.Enabled {
				chainRetentionJob = run.JobID
			}
		}
	}
}

// StartRetention starts a forget+prune run for a job's tagged snapshots.
func (c *Coordinator) StartRetention(jobID, trigger string) (*Run, error) {
	if trigger == "" {
		trigger = TriggerManual
	}
	job, _, repo, err := c.app.resolveJob(jobID)
	if err != nil {
		return nil, err
	}
	if job.Retention == nil || !job.Retention.Enabled {
		return nil, validf("retention is not enabled for this job")
	}
	policy := *job.Retention
	if err := policy.Validate(); err != nil {
		return nil, validf("%s", err.Error())
	}
	tag := job.ResticTag()
	desc := policy.Describe()
	run := &Run{
		Kind:         KindRetention,
		Status:       StatusStarting,
		JobID:        job.ID,
		RepositoryID: repo.ID,
		JobName:      job.Name,
		RepoName:     repo.Name,
		Params: map[string]string{
			"tag":     tag,
			"trigger": trigger,
			"policy":  desc,
			"preset":  policy.Preset,
		},
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		switch trigger {
		case TriggerAfterBackup:
			h.Log("info", "system", "Applying retention after backup ("+desc+").")
		default:
			h.Log("info", "system", "Applying retention ("+desc+").")
		}
		h.Log("info", "system", fmt.Sprintf("Forgetting old snapshots tagged %s in repository %q", tag, repo.Name))
		return c.runner.Forget(ctx, &repo, tag, policy, h)
	})
}

// StartForgetSnapshot forgets one snapshot in a repository, then prunes.
func (c *Coordinator) StartForgetSnapshot(repoID, snapshotID string) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, validf("please choose a snapshot to forget")
	}
	run := &Run{
		Kind: KindForget, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: map[string]string{"snapshotId": snapshotID, "scope": "snapshot"},
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		h.Log("info", "system", "Forgetting snapshot "+shortID(snapshotID)+" from repository "+repo.Name)
		snaps, err := c.runner.Snapshots(ctx, &repo, "")
		if err != nil {
			return 0, err
		}
		snap, err := matchSnapshot(snaps, snapshotID)
		if err != nil {
			return 0, err
		}
		if snap.ID != snapshotID {
			h.Log("info", "system", "Matched snapshot "+snap.ID)
		}
		return c.runner.ForgetSnapshots(ctx, &repo, []string{snap.ID}, h)
	})
}

// StartForgetJob forgets every snapshot tagged for the job, then prunes.
// When deleteJob is true the catalog row is removed only after restic succeeds.
func (c *Coordinator) StartForgetJob(jobID string, deleteJob bool) (*Run, error) {
	job, _, repo, err := c.app.resolveJob(jobID)
	if err != nil {
		return nil, err
	}
	if deleteJob {
		if active := c.app.JobActiveRun(job.ID); active != nil {
			return nil, &BusyError{
				RepoName: repo.Name,
				Blocking: blockingRun{
					RunID:     active.ID,
					Kind:      active.Kind,
					JobName:   active.JobName,
					StartedAt: active.StartedAt,
				},
			}
		}
	}
	tag := job.ResticTag()
	params := map[string]string{"tag": tag, "scope": "job"}
	if deleteJob {
		params["deleteJob"] = "true"
	}
	run := &Run{
		Kind:         KindForget,
		Status:       StatusStarting,
		JobID:        job.ID,
		RepositoryID: repo.ID,
		JobName:      job.Name,
		RepoName:     repo.Name,
		Params:       params,
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		if deleteJob {
			h.Log("info", "system", fmt.Sprintf("Forgetting all snapshots tagged %s, then deleting job %q", tag, job.Name))
		} else {
			h.Log("info", "system", fmt.Sprintf("Forgetting all snapshots tagged %s in repository %q", tag, repo.Name))
		}
		snaps, err := c.runner.Snapshots(ctx, &repo, tag)
		if err != nil {
			return 0, err
		}
		ids := snapshotIDsOf(snaps)
		h.Log("info", "system", fmt.Sprintf("Found %d snapshot(s) for this job.", len(ids)))
		code, err := c.runner.ForgetSnapshots(ctx, &repo, ids, h)
		if err != nil {
			return code, err
		}
		if code != 0 && code != resticExitWarnings {
			return code, nil
		}
		if deleteJob {
			if derr := c.app.Jobs.Delete(job.ID); derr != nil {
				var nf *NotFoundError
				if !errors.As(derr, &nf) {
					h.Log("error", "system", "Snapshots were forgotten, but the job could not be deleted: "+derr.Error())
					return 0, derr
				}
			} else {
				h.Log("ok", "system", "Deleted job "+job.Name+" from the app.")
			}
		}
		return code, nil
	})
}

// StartResetRepo forgets every snapshot in a repository, then prunes.
// The repository entity and restic keys stay; the next backup recreates snapshots.
func (c *Coordinator) StartResetRepo(repoID string) (*Run, error) {
	repo, ok := c.app.Repos.Get(repoID)
	if !ok {
		return nil, notFoundf("repository not found")
	}
	run := &Run{
		Kind: KindForget, Status: StatusStarting, RepositoryID: repo.ID, RepoName: repo.Name,
		Params: map[string]string{"scope": "repo"},
	}
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		h.Log("info", "system", "Forgetting every snapshot in repository "+repo.Name)
		snaps, err := c.runner.Snapshots(ctx, &repo, "")
		if err != nil {
			return 0, err
		}
		ids := snapshotIDsOf(snaps)
		h.Log("info", "system", fmt.Sprintf("Found %d snapshot(s).", len(ids)))
		return c.runner.ForgetSnapshots(ctx, &repo, ids, h)
	})
}

// ErrRunNotActive means a stop was requested for a run that is not currently
// running (already finished, unknown, or not stoppable from this process).
var ErrRunNotActive = errors.New("run is not currently running")

// Stop requests a graceful stop of a running operation. It marks the run's
// in-memory handle stopped (so the terminal status is classified as canceled),
// records the intent in the durable log, and cancels the context — which sends
// restic SIGINT (clean: no partial snapshot, lock released) with a hard-kill
// fallback. It is idempotent: a repeated stop does not re-log or re-signal.
//
// If this process is not driving the run (e.g. stop from restic-webctl while the
// web server owns it), Stop signals the recorded restic child process group so
// the owning process can observe the exit and finalize the durable record.
func (c *Coordinator) Stop(runID string) error {
	// The durable record is authoritative: if the run is unknown or already in a
	// terminal state, it cannot be stopped — even during the brief window where
	// its in-memory handle has not yet been released.
	run, ok := c.store.Get(runID)
	if !ok || run.Status.Terminal() {
		return ErrRunNotActive
	}

	c.mu.Lock()
	var target *activeRun
	for _, ar := range c.active {
		if ar.runID == runID {
			target = ar
			break
		}
	}
	if target != nil {
		already := target.stopped.Swap(true)
		cancel := target.cancel
		handle := target.handle
		c.mu.Unlock()

		if !already {
			handle.Log("warn", "system", "Stop requested by user.")
			cancel()
		}
		return nil
	}
	c.mu.Unlock()

	if run.PID <= 0 || !processAlive(run.PID) {
		return ErrRunNotActive
	}
	if run.PIDStart != "" && !isOwnResticProcess(run.PID, run.PIDStart) {
		return ErrRunNotActive
	}
	c.store.AppendSystemLine(runID, "warn", "Stop requested by another process.")
	_ = signalProcessGroup(run.PID, syscall.SIGINT)
	return nil
}

// finish releases the repository slot when a run ends.
func (c *Coordinator) finish(ar *activeRun) {
	c.mu.Lock()
	if cur := c.active[ar.repoID]; cur == ar {
		delete(c.active, ar.repoID)
	}
	c.mu.Unlock()
}

// classifyRun maps restic's exit code (and cancellation) to a terminal status.
// The cancellation check comes first so a user-stopped run is labeled canceled
// even on the one exec path (init) that surfaces the interruption as an error.
func classifyRun(ar *activeRun, ctx context.Context, code int, err error) (RunStatus, string) {
	if ar.stopped.Load() || errors.Is(ctx.Err(), context.Canceled) {
		return StatusCanceled, ""
	}
	if err != nil {
		return StatusFailed, err.Error()
	}
	switch code {
	case 0:
		return StatusSuccess, ""
	case resticExitWarnings:
		// Backup created a snapshot but some source files were unreadable.
		return StatusSuccessWarnings, ""
	case resticExitInterrupted:
		// Interrupted by SIGINT without an app-level stop request.
		return StatusCanceled, ""
	case resticExitNotInitialized:
		return StatusFailed, "repository is not initialized"
	case resticExitLocked:
		return StatusFailed, "repository is locked by another operation"
	case resticExitBadPassword:
		return StatusFailed, "wrong repository password"
	default:
		return StatusFailed, fmt.Sprintf("restic exited with code %d", code)
	}
}

func logLevelFor(status RunStatus) string {
	switch status {
	case StatusSuccess:
		return "ok"
	case StatusSuccessWarnings:
		return "warn"
	default:
		return "error"
	}
}

func terminalMessage(status RunStatus) string {
	switch status {
	case StatusSuccess:
		return "Completed successfully."
	case StatusSuccessWarnings:
		return "Completed with warnings (some files could not be read)."
	case StatusCanceled:
		return "Stopped."
	case StatusInterrupted:
		return "Interrupted."
	default:
		return "Failed."
	}
}

// ActiveCount reports how many operations are currently running.
func (c *Coordinator) ActiveCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.active)
}
