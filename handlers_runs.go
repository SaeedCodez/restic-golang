package main

import (
	"errors"
	"net/http"
	"strconv"
)

// This file holds the HTTP surface for runs: starting a job's backup, reading a
// run's record and log, and listing history. Every long-running operation is a
// run, so restore/init/download (later steps) reuse this same surface.

// writeStartError maps the errors a run-start can produce to HTTP responses,
// including a 409 that names the blocking run so the UI can explain the wait.
func writeStartError(w http.ResponseWriter, err error) {
	var busy *BusyError
	if errors.As(err, &busy) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok":          false,
			"code":        "busy",
			"error":       busy.Error(),
			"repoName":    busy.RepoName,
			"blockingRun": busy.Blocking,
		})
		return
	}
	writeStoreError(w, err)
}

func (s *Server) handleJobRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	run, err := s.app.coord.StartBackup(r.PathValue("id"))
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleJobRuns(w http.ResponseWriter, r *http.Request) {
	if !s.app.jobs.Exists(r.PathValue("id")) {
		errorJSON(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runs": s.app.runs.runsForJob(r.PathValue("id"))})
}

func (s *Server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	run, ok := s.app.runs.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run": run})
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	after := int64(0)
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			after = n
		}
	}
	lines, err := s.app.runs.ReadLog(r.PathValue("id"), after)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "lines": lines})
}

func (s *Server) handleRunList(w http.ResponseWriter, r *http.Request) {
	var runs []*Run
	if r.URL.Query().Get("status") == "active" {
		runs = s.app.runs.activeRuns()
	} else {
		runs = s.app.runs.list(nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runs": runs})
}
