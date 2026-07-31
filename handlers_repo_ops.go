package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// This file holds the per-repository restic operations and the download stream.
// Read-only checks (test connection, list snapshots) are synchronous; everything
// that touches the repository as a long operation (init, restore, download) is a
// tracked run through the coordinator.

func (s *Server) handleRepoTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	repo, ok := s.app.repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res := s.app.runner.Test(ctx, &repo)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          res.OK,
		"initialized": res.Initialized,
		"message":     res.Message,
		"detail":      res.Detail,
	})
}

func (s *Server) handleRepoInit(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	run, err := s.app.coord.StartInit(r.PathValue("id"))
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
	repo, ok := s.app.repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.app.runner.Unlock(ctx, &repo); err != nil {
		errorJSON(w, http.StatusOK, "unlock_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Removed any stale locks."})
}

func (s *Server) handleRepoSnapshots(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.app.repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	s.writeSnapshots(w, r, &repo, "")
}

func (s *Server) handleJobSnapshots(w http.ResponseWriter, r *http.Request) {
	job, ok := s.app.jobs.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	repo, ok := s.app.repos.Get(job.RepositoryID)
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	snaps, err := s.app.runner.Snapshots(ctx, repo, tag)
	if err != nil {
		if errors.Is(err, errNotInitialized) {
			errorJSON(w, http.StatusOK, "not_initialized", "No repository found here yet. Initialize it first.")
			return
		}
		errorJSON(w, http.StatusOK, "restic_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "snapshots": snaps})
}

func (s *Server) handleRepoRestore(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		SnapshotID string `json:"snapshotId"`
		Target     string `json:"target"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.app.coord.StartRestore(r.PathValue("id"), body.SnapshotID, body.Target)
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
		SnapshotID string `json:"snapshotId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	run, err := s.app.coord.StartDownload(r.PathValue("id"), body.SnapshotID)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

// handleRunDownload streams a zip of a completed download run's workspace.
func (s *Server) handleRunDownload(w http.ResponseWriter, r *http.Request) {
	run, ok := s.app.runs.Get(r.PathValue("id"))
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
	_ = zipDir(w, target)
}
