package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// This file holds the CRUD HTTP handlers for the three user-managed entities:
// repositories, folders and jobs. They are thin: decode, delegate to the store,
// map typed store errors to status codes.

// decodeJSON reads the request body into dst, writing a 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", "Could not read request: "+err.Error())
		return false
	}
	return true
}

// writeStoreError maps a typed store/validation error to an HTTP response.
func writeStoreError(w http.ResponseWriter, err error) {
	var ce *ConflictError
	var nf *NotFoundError
	var ve *ValidationError
	switch {
	case errors.As(err, &ce):
		errorJSON(w, http.StatusConflict, "conflict", err.Error())
	case errors.As(err, &nf):
		errorJSON(w, http.StatusNotFound, "not_found", err.Error())
	case errors.As(err, &ve):
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		errorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
	}
}

// ---- repositories ----------------------------------------------------------

func (s *Server) handleRepoList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repositories": s.app.repos.List()})
}

func (s *Server) handleRepoCreate(w http.ResponseWriter, r *http.Request) {
	var repo Repository
	if !decodeJSON(w, r, &repo) {
		return
	}
	if err := repo.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	created, err := s.app.repos.Create(repo)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": created})
}

func (s *Server) handleRepoGet(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.app.repos.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": repo})
}

func (s *Server) handleRepoUpdate(w http.ResponseWriter, r *http.Request) {
	var repo Repository
	if !decodeJSON(w, r, &repo) {
		return
	}
	if err := repo.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	updated, err := s.app.repos.Update(r.PathValue("id"), repo)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": updated})
}

func (s *Server) handleRepoDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if using := s.app.jobsUsingRepository(id); len(using) > 0 {
		errorJSON(w, http.StatusConflict, "conflict",
			"This repository is used by "+jobNames(using)+". Delete or edit those jobs first.")
		return
	}
	if err := s.app.repos.Delete(id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- folders ---------------------------------------------------------------

func (s *Server) handleFolderList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folders": s.app.folders.List()})
}

func (s *Server) handleFolderCreate(w http.ResponseWriter, r *http.Request) {
	var f Folder
	if !decodeJSON(w, r, &f) {
		return
	}
	if err := f.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	created, err := s.app.folders.Create(f)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": created})
}

func (s *Server) handleFolderGet(w http.ResponseWriter, r *http.Request) {
	f, ok := s.app.folders.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "folder not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": f})
}

func (s *Server) handleFolderUpdate(w http.ResponseWriter, r *http.Request) {
	var f Folder
	if !decodeJSON(w, r, &f) {
		return
	}
	if err := f.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	updated, err := s.app.folders.Update(r.PathValue("id"), f)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": updated})
}

func (s *Server) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if using := s.app.jobsUsingFolder(id); len(using) > 0 {
		errorJSON(w, http.StatusConflict, "conflict",
			"This folder is used by "+jobNames(using)+". Delete or edit those jobs first.")
		return
	}
	if err := s.app.folders.Delete(id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- jobs ------------------------------------------------------------------

// jobView is a job enriched with its folder/repository display fields and its
// derived restic tag, so the UI can render a job without extra lookups.
type jobView struct {
	Job
	FolderName string `json:"folderName"`
	FolderPath string `json:"folderPath"`
	RepoName   string `json:"repoName"`
	Tag        string `json:"tag"`
}

func (s *Server) viewOf(job Job) jobView {
	v := jobView{Job: job, Tag: job.ResticTag()}
	if f, ok := s.app.folders.Get(job.FolderID); ok {
		v.FolderName = f.Name
		v.FolderPath = f.Path
	}
	if repo, ok := s.app.repos.Get(job.RepositoryID); ok {
		v.RepoName = repo.Name
	}
	return v
}

func (s *Server) handleJobList(w http.ResponseWriter, r *http.Request) {
	jobs := s.app.jobs.List()
	views := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		views = append(views, s.viewOf(j))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "jobs": views})
}

// validateJobRefs checks that the job's folder and repository exist.
func (s *Server) validateJobRefs(job *Job) error {
	if err := job.Validate(); err != nil {
		return err
	}
	if !s.app.folders.Exists(job.FolderID) {
		return validf("the chosen backup folder does not exist")
	}
	if !s.app.repos.Exists(job.RepositoryID) {
		return validf("the chosen storage repository does not exist")
	}
	return nil
}

func (s *Server) handleJobCreate(w http.ResponseWriter, r *http.Request) {
	var job Job
	if !decodeJSON(w, r, &job) {
		return
	}
	if err := s.validateJobRefs(&job); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	created, err := s.app.jobs.Create(job)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": s.viewOf(created)})
}

func (s *Server) handleJobGet(w http.ResponseWriter, r *http.Request) {
	job, ok := s.app.jobs.Get(r.PathValue("id"))
	if !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": s.viewOf(job)})
}

func (s *Server) handleJobUpdate(w http.ResponseWriter, r *http.Request) {
	var job Job
	if !decodeJSON(w, r, &job) {
		return
	}
	if err := s.validateJobRefs(&job); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	updated, err := s.app.jobs.Update(r.PathValue("id"), job)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": s.viewOf(updated)})
}

func (s *Server) handleJobDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.app.jobs.Delete(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// jobNames formats a short human list of job names for reference-conflict messages.
func jobNames(jobs []Job) string {
	names := make([]string, 0, len(jobs))
	for _, j := range jobs {
		names = append(names, `"`+j.Name+`"`)
	}
	switch len(names) {
	case 0:
		return "no jobs"
	case 1:
		return "job " + names[0]
	default:
		return "jobs " + strings.Join(names, ", ")
	}
}
