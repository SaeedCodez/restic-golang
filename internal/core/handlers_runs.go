package core

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
	run, err := s.app.Coord.StartBackup(r.PathValue("id"))
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleJobRetention(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	run, err := s.app.Coord.StartRetention(r.PathValue("id"), TriggerManual)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleJobForget(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		DeleteJob bool `json:"deleteJob"`
	}
	if !decodeJSONAllowEmpty(w, r, &body) {
		return
	}
	run, err := s.app.Coord.StartForgetJob(r.PathValue("id"), body.DeleteJob)
	if err != nil {
		writeStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": run.ID, "run": run})
}

func (s *Server) handleJobRuns(w http.ResponseWriter, r *http.Request) {
	if !s.app.Jobs.Exists(r.PathValue("id")) {
		errorJSON(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	runs, total := s.app.Runs.Query("", "", r.PathValue("id"), queryLimit(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runs": runs, "total": total})
}

// queryLimit reads a positive ?limit=, or 0 for "no limit".
func queryLimit(r *http.Request) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func (s *Server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	run, ok := s.app.Runs.Get(r.PathValue("id"))
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
	lines, err := s.app.Runs.ReadLog(r.PathValue("id"), after)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "lines": lines})
}

func (s *Server) handleRunStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.app.Coord.Stop(id)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Stopping…"})
		return
	}
	if errors.Is(err, ErrRunNotActive) {
		if _, ok := s.app.Runs.Get(id); ok {
			errorJSON(w, http.StatusConflict, "not_active", "This run has already finished.")
		} else {
			errorJSON(w, http.StatusNotFound, "not_found", "run not found")
		}
		return
	}
	writeStoreError(w, err)
}

// handleRunList returns run history, newest first, with optional filters:
//
//	?status=active|finished|<exact status>   ?kind=backup|restore|init|download
//	?jobId=<id>                              ?limit=<n>
//
// `total` is the match count before the limit, so the UI can say "showing 20 of
// 137" rather than silently truncating.
func (s *Server) handleRunList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	runs, total := s.app.Runs.Query(q.Get("status"), q.Get("kind"), q.Get("jobId"), queryLimit(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runs": runs, "total": total})
}
