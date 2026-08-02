package main

import (
	"context"
	"net/http"
	"time"
)

// handleStatus reports app-wide state the UI shows at a glance: whether restic is
// installed (and its version), how many entities exist, and what is running now.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	installed := s.app.runner.Available()
	resp := map[string]any{
		"ok":              true,
		"resticInstalled": installed,
		"counts": map[string]int{
			"repositories": s.app.repos.Count(),
			"folders":      s.app.folders.Count(),
			"jobs":         s.app.jobs.Count(),
		},
		"activeRuns": s.app.runs.activeRuns(),
	}
	if installed {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if v, err := s.app.runner.Version(ctx); err == nil {
			resp["resticVersion"] = v
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
