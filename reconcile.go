package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// reapOrphan kills the process group of a restic child that outlived a previous
// app instance (e.g. after the app was SIGKILLed). It only acts when the pid can
// be positively identified as a live restic process, so a recycled pid is never
// killed. It returns whether it reaped anything.
func reapOrphan(pid int) bool {
	if pid <= 0 || !processAlive(pid) {
		return false
	}
	if !isOwnResticProcess(pid) {
		return false
	}
	_ = signalProcessGroup(pid, syscall.SIGKILL)
	return true
}

// Reconcile brings the durable run records into an honest state on startup and
// must run before the HTTP server accepts traffic. Every run still marked
// running is marked interrupted (a fresh process drives nothing, so any such
// record is provably stale); any orphaned restic child is reaped; and each
// affected repository gets a best-effort, stale-only `restic unlock` in the
// background so its next operation is not blocked by a leftover lock.
func (a *App) Reconcile() {
	// Download workspaces are ephemeral; drop any left by a previous session.
	_ = os.RemoveAll(filepath.Join(a.dataDir, "downloads"))

	repoIDs := a.runs.reconcile(reapOrphan)
	a.runs.prune(maxRunsPerJob)

	if !a.runner.Available() {
		return
	}
	for _, id := range repoIDs {
		repo, ok := a.repos.Get(id)
		if !ok {
			continue
		}
		go func(r Repository) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = a.runner.Unlock(ctx, &r)
		}(repo)
	}
}
