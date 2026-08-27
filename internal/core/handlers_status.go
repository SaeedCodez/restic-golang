package core

import (
	"context"
	"net/http"
	"time"
)

// handleStatus reports app-wide state the UI shows at a glance: whether restic is
// installed (and its version), how many entities exist, and what is running now.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	installed := s.app.Runner.Available()
	resp := map[string]any{
		"ok":              true,
		"resticInstalled": installed,
		"counts": map[string]int{
			"repositories": s.app.Repos.Count(),
			"folders":      s.app.Folders.Count(),
			"jobs":         s.app.Jobs.Count(),
		},
		"activeRuns": s.app.Runs.ActiveRuns(),
	}
	if installed {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if v, err := s.app.Runner.Version(ctx); err == nil {
			resp["resticVersion"] = v
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
