package core

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// reapOrphan kills the process group of a restic child that outlived a previous
// app instance (e.g. after the app was SIGKILLed). It acts only when the pid is
// alive AND its current start token matches the one recorded when we launched it
// AND it looks like restic — so a recycled pid (whose start token necessarily
// differs) is never killed. It returns whether it reaped anything.
func reapOrphan(pid int, startToken string) bool {
	if pid <= 0 || !processAlive(pid) {
		return false
	}
	if !isOwnResticProcess(pid, startToken) {
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
	_ = os.RemoveAll(filepath.Join(a.DataDir, "downloads"))

	repoIDs, stopped := a.Runs.reconcile(reapOrphan)
	if a.RetainRunsPerJob > 0 {
		a.Runs.prune(a.RetainRunsPerJob)
	}
	// A restore that stopped a site and died before starting it again leaves
	// the site down; start those containers again.
	if len(stopped) > 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			for _, id := range stopped {
				if err := a.Docker.Start(ctx, id); err != nil {
					log.Printf("reconcile: could not start container %s again: %v", shortID(id), err)
				}
			}
		}()
	}

	if !a.Runner.Available() {
		return
	}
	for _, id := range repoIDs {
		repo, ok := a.Repos.Get(id)
		if !ok {
			continue
		}
		go func(r Repository) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = a.Runner.Unlock(ctx, &r)
		}(repo)
	}
}
