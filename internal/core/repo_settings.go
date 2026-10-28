package core

import (
	"net/http"
	"time"
)

// This file holds the repository settings that are saved apart from the
// repository itself: the automatic check schedule and the speed limits.

// SetRepoCheckSchedule saves a repository's check schedule. A disabled schedule
// is kept too, so turning it back on restores the same choices.
func (a *App) SetRepoCheckSchedule(id string, c RepoCheckSchedule) (Repository, error) {
	c.Normalize()
	if c.Enabled {
		if err := c.Validate(); err != nil {
			return Repository{}, err
		}
	}
	c.Since = time.Now().UTC().Truncate(time.Second)
	return a.Repos.SetCheckSchedule(id, &c)
}

// SetRepoSpeedLimit saves a repository's upload and download limits in KiB/s
// (0 = no limit).
func (a *App) SetRepoSpeedLimit(id string, uploadKiB, downloadKiB int) (Repository, error) {
	if err := ValidateSpeedLimit(uploadKiB); err != nil {
		return Repository{}, err
	}
	if err := ValidateSpeedLimit(downloadKiB); err != nil {
		return Repository{}, err
	}
	return a.Repos.SetSpeedLimit(id, uploadKiB, downloadKiB)
}

// CheckView is the outcome of a repository's newest finished check.
type CheckView struct {
	RunID      string     `json:"runId"`
	Status     RunStatus  `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// ReadData is how much of the data the check read ("" = structure only).
	ReadData string `json:"readData,omitempty"`
	// Scheduled is true when the schedule started it rather than a person.
	Scheduled bool   `json:"scheduled,omitempty"`
	Error     string `json:"error,omitempty"`
}

func checkViewOf(run *Run) *CheckView {
	if run == nil {
		return nil
	}
	return &CheckView{
		RunID: run.ID, Status: run.Status, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		ReadData: run.Params["readData"], Scheduled: run.Params["trigger"] == TriggerSchedule,
		Error: run.Error,
	}
}

// withChecks attaches each repository's newest check and next scheduled one.
func (s *Server) withChecks(views []repoView) []repoView {
	latest := s.app.Runs.latestChecks()
	for i := range views {
		views[i].LastCheck = checkViewOf(latest[views[i].ID])
		if sched := views[i].CheckSchedule; sched != nil && sched.Enabled {
			views[i].NextCheckAt = nextCheckAt(sched, s.app.Runs.lastCheckStart(views[i].ID))
		}
	}
	return views
}

func (s *Server) handleRepoCheckSchedule(w http.ResponseWriter, r *http.Request) {
	var c RepoCheckSchedule
	if !decodeJSON(w, r, &c) {
		return
	}
	repo, err := s.app.SetRepoCheckSchedule(r.PathValue("id"), c)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": s.withChecks([]repoView{repoViewOf(repo)})[0]})
}

type speedLimitBody struct {
	UploadLimitKiB   int `json:"uploadLimitKiB"`
	DownloadLimitKiB int `json:"downloadLimitKiB"`
}

func (s *Server) handleRepoSpeedLimit(w http.ResponseWriter, r *http.Request) {
	var body speedLimitBody
	if !decodeJSON(w, r, &body) {
		return
	}
	repo, err := s.app.SetRepoSpeedLimit(r.PathValue("id"), body.UploadLimitKiB, body.DownloadLimitKiB)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": repoViewOf(repo)})
}
