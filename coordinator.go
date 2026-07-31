package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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

// StartBackup starts a backup run for a job. It resolves the job's folder and
// repository, then dispatches a tagged restic backup.
func (c *Coordinator) StartBackup(jobID string) (*Run, error) {
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
		Params:       map[string]string{"source": folder.Path, "tag": job.ResticTag()},
	}
	tag := job.ResticTag()
	src := folder.Path
	return c.startRun(repo, run, func(ctx context.Context, h *runHandle) (int, error) {
		h.Log("info", "system", fmt.Sprintf("Backing up %s to repository %q", src, repo.Name))
		return c.runner.Backup(ctx, &repo, src, []string{tag}, h)
	})
}

// startRun reserves the repository, creates the durable run, and launches the
// operation in the background. It returns the created run (or a BusyError).
func (c *Coordinator) startRun(repo Repository, run *Run, exec runExec) (*Run, error) {
	c.mu.Lock()
	if ar := c.active[repo.ID]; ar != nil {
		c.mu.Unlock()
		return nil, &BusyError{RepoName: repo.Name, Blocking: ar.info()}
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
func (c *Coordinator) drive(ctx context.Context, ar *activeRun, exec runExec) {
	defer c.finish(ar)

	ar.handle.setStatus(StatusRunning)
	code, err := exec(ctx, ar.handle)
	status, errMsg := classifyRun(ar, ctx, code, err)

	if errMsg != "" {
		ar.handle.Log("error", "system", errMsg)
	}
	ar.handle.Log(logLevelFor(status), "system", terminalMessage(status))
	ar.handle.finalize(status, code, errMsg)
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
func classifyRun(ar *activeRun, ctx context.Context, code int, err error) (RunStatus, string) {
	if err != nil {
		return StatusFailed, err.Error()
	}
	if ar.stopped.Load() || errors.Is(ctx.Err(), context.Canceled) {
		return StatusCanceled, ""
	}
	switch code {
	case 0:
		return StatusSuccess, ""
	case 3:
		// Backup created a snapshot but some source files were unreadable.
		return StatusSuccessWarnings, ""
	case 130:
		// Interrupted by SIGINT without an app-level stop request.
		return StatusCanceled, ""
	case 10:
		return StatusFailed, "repository is not initialized"
	case 11:
		return StatusFailed, "repository is locked by another operation"
	case 12:
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
