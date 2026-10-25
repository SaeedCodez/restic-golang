package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
)

// This file holds the per-repository restic operations and the download stream.
// Read-only checks (test connection, list snapshots) are synchronous; everything
// that touches the repository as a long operation (init, restore, download) is a
// tracked run through the coordinator.

func (s *Server) handleRepoTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	repo, ok := s.app.Repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	ctx, cancel := s.resticContext(r)
	defer cancel()
	res := s.app.Runner.Test(ctx, &repo)
	body := map[string]any{
		"ok":          res.OK,
		"initialized": res.Initialized,
		"message":     res.Message,
		"detail":      res.Detail,
	}
	if !res.OK && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		body["code"] = "timeout"
		body["message"] = res.Message + " " + s.timeoutHint()
	}
	writeJSON(w, http.StatusOK, body)
}

// resticContext bounds a synchronous restic call by the app's ResticTimeout
// (0 = no limit). It derives from the request, so a client that goes away
// stops restic too.
func (s *Server) resticContext(r *http.Request) (context.Context, context.CancelFunc) {
	if s.app.ResticTimeout <= 0 {
		return context.WithCancel(r.Context())
	}
	return context.WithTimeout(r.Context(), s.app.ResticTimeout)
}

func (s *Server) timeoutHint() string {
	return fmt.Sprintf("The repository did not answer within %s; it may just be slow. Try again, or raise the server's -restic-timeout (RESTIC_WEB_RESTIC_TIMEOUT).", s.app.ResticTimeout)
}

// writeResticError reports a failed synchronous restic call, with code
// "timeout" when the time limit (rather than restic) ended it.
func (s *Server) writeResticError(w http.ResponseWriter, code string, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		errorJSON(w, http.StatusOK, "timeout", err.Error()+". "+s.timeoutHint())
		return
	}
	errorJSON(w, http.StatusOK, code, err.Error())
}

func (s *Server) handleRepoInit(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	run, err := s.app.Coord.StartInit(r.PathValue("id"))
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleRepoUnlock(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	repo, ok := s.app.Repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	ctx, cancel := s.resticContext(r)
	defer cancel()
	if err := s.app.Runner.Unlock(ctx, &repo); err != nil {
		s.writeResticError(w, "unlock_failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Removed any stale locks."})
}

func (s *Server) handleRepoSnapshots(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.app.Repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	s.writeSnapshots(w, r, &repo, "")
}

func (s *Server) handleJobSnapshots(w http.ResponseWriter, r *http.Request) {
	job, ok := s.app.Jobs.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	repo, ok := s.app.Repos.Get(job.RepositoryID)
	if !ok {
		errorJSON(w, http.StatusBadRequest, "bad_request", "this job's storage repository no longer exists")
		return
	}
	// Filter to just this job's snapshots via its immutable tag.
	s.writeSnapshots(w, r, &repo, job.ResticTag())
}

// writeSnapshots lists snapshots for a repository (optionally filtered to a tag)
// and classifies the common "not initialized" case for the UI.
func (s *Server) writeSnapshots(w http.ResponseWriter, r *http.Request, repo *Repository, tag string) {
	if !s.requireRestic(w) {
		return
	}
	ctx, cancel := s.resticContext(r)
	defer cancel()
	snaps, err := s.app.Runner.Snapshots(ctx, repo, tag)
	if err != nil {
		if errors.Is(err, errNotInitialized) {
			errorJSON(w, http.StatusOK, "not_initialized", "No repository found here yet. Initialize it first.")
			return
		}
		s.writeResticError(w, "restic_error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "snapshots": snaps})
}

func (s *Server) handleRepoRestore(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		SnapshotID     string   `json:"snapshotId"`
		Target         string   `json:"target"`
		Include        []string `json:"include"`
		StopContainers bool     `json:"stopContainers"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.app.Coord.StartRestoreWith(r.PathValue("id"), body.SnapshotID, body.Target,
		RestoreOptions{Include: body.Include, StopContainers: body.StopContainers})
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleRepoForget(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		SnapshotID string `json:"snapshotId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.app.Coord.StartForgetSnapshot(r.PathValue("id"), body.SnapshotID)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleRepoReset(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	run, err := s.app.Coord.StartResetRepo(r.PathValue("id"))
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleRepoCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		ReadData string `json:"readData"`
	}
	if !decodeJSONAllowEmpty(w, r, &body) {
		return
	}
	run, err := s.app.Coord.StartCheck(r.PathValue("id"), body.ReadData)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleRepoDownload(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		SnapshotID string   `json:"snapshotId"`
		Include    []string `json:"include"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.app.Coord.StartDownloadWith(r.PathValue("id"), body.SnapshotID, body.Include)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleRepoRestoreDatabase(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	body := struct {
		SnapshotID   string `json:"snapshotId"`
		DatabaseID   string `json:"databaseId"`
		Into         string `json:"into"`
		SafetyBackup *bool  `json:"safetyBackup"`
	}{}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !s.requireDocker(w, r) {
		return
	}
	opts := RestoreDatabaseOptions{Into: body.Into, SafetyBackup: body.SafetyBackup == nil || *body.SafetyBackup}
	run, err := s.app.Coord.StartRestoreDatabase(r.PathValue("id"), body.SnapshotID, body.DatabaseID, opts)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

// handleSnapshotLs lists one directory of a snapshot for the file browser. The
// first call lists the whole snapshot with restic (slow on remote storage);
// later calls for the same snapshot are served from memory.
func (s *Server) handleSnapshotLs(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	repo, ok := s.app.Repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	snapshotID := strings.TrimSpace(r.PathValue("sid"))
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	ctx, cancel := s.resticContext(r)
	defer cancel()
	tree, err := s.app.SnapshotTree(ctx, &repo, snapshotID)
	if err != nil {
		s.writeResticError(w, "restic_error", err)
		return
	}
	entries, ok := tree.List(dir)
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "no such folder in this snapshot: "+dir)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path.Clean("/" + dir), "entries": entries})
}

// handleRunDownload streams a zip of a completed download run's workspace.
func (s *Server) handleRunDownload(w http.ResponseWriter, r *http.Request) {
	run, ok := s.app.Runs.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	if run.Kind != KindDownload {
		http.Error(w, "this run is not a download", http.StatusBadRequest)
		return
	}
	if run.Status != StatusSuccess {
		http.Error(w, "the download is not ready", http.StatusConflict)
		return
	}
	target := run.Params["target"]
	if target == "" {
		http.Error(w, "download workspace is missing", http.StatusNotFound)
		return
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		http.Error(w, "the download is no longer available; run it again", http.StatusNotFound)
		return
	}

	filename := "snapshot-" + shortID(run.Params["snapshotId"]) + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	// Headers are committed on the first byte, so a mid-stream failure can't change
	// the status — but it must not be swallowed silently.
	if err := zipDir(w, target); err != nil {
		log.Printf("run %s: download zip stream failed: %v", run.ID, err)
	}
}
