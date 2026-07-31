package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// App is the central container of the server's dependencies. It owns the three
// entity stores today; later steps add the run store, restic runner and
// coordinator here so every handler reaches its collaborators through one place.
type App struct {
	dataDir string

	repos   *EntityStore[Repository, *Repository]
	folders *EntityStore[Folder, *Folder]
	jobs    *EntityStore[Job, *Job]
}

// newApp loads (or initializes) all entity stores under dataDir.
func newApp(dataDir string) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("could not create data directory %q: %w", dataDir, err)
	}

	repos, err := loadEntityStore[Repository, *Repository](filepath.Join(dataDir, "repositories.json"), "repository")
	if err != nil {
		return nil, err
	}
	folders, err := loadEntityStore[Folder, *Folder](filepath.Join(dataDir, "folders.json"), "folder")
	if err != nil {
		return nil, err
	}
	jobs, err := loadEntityStore[Job, *Job](filepath.Join(dataDir, "jobs.json"), "job")
	if err != nil {
		return nil, err
	}

	return &App{dataDir: dataDir, repos: repos, folders: folders, jobs: jobs}, nil
}

// jobsUsingRepository returns the jobs that reference the given repository id.
func (a *App) jobsUsingRepository(repoID string) []Job {
	var out []Job
	for _, j := range a.jobs.List() {
		if j.RepositoryID == repoID {
			out = append(out, j)
		}
	}
	return out
}

// jobsUsingFolder returns the jobs that reference the given folder id.
func (a *App) jobsUsingFolder(folderID string) []Job {
	var out []Job
	for _, j := range a.jobs.List() {
		if j.FolderID == folderID {
			out = append(out, j)
		}
	}
	return out
}

// resolveJob returns a job together with its folder and repository, or an error
// if any of them is missing (e.g. the folder or repository was deleted).
func (a *App) resolveJob(jobID string) (Job, Folder, Repository, error) {
	job, ok := a.jobs.Get(jobID)
	if !ok {
		return Job{}, Folder{}, Repository{}, notFoundf("job not found")
	}
	folder, ok := a.folders.Get(job.FolderID)
	if !ok {
		return job, Folder{}, Repository{}, validf("this job's backup folder no longer exists; edit the job to pick another")
	}
	repo, ok := a.repos.Get(job.RepositoryID)
	if !ok {
		return job, folder, Repository{}, validf("this job's storage repository no longer exists; edit the job to pick another")
	}
	return job, folder, repo, nil
}

// importLegacyConfig performs a one-time migration from the demo's single-config
// file: if there are no repositories yet and a legacy config.json holds a valid
// configuration, it is imported as a repository named "Default". This lets an
// existing demo user land softly. It is best-effort and never fatal.
func (a *App) importLegacyConfig(configPath string) error {
	if a.repos.Count() > 0 {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var legacy struct {
		BackendType string `json:"backendType"`
		Endpoint    string `json:"endpoint"`
		Bucket      string `json:"bucket"`
		Region      string `json:"region"`
		AccessKey   string `json:"accessKey"`
		SecretKey   string `json:"secretKey"`
		LocalPath   string `json:"localPath"`
		Password    string `json:"password"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil // not a config we understand; skip quietly
	}
	if strings.TrimSpace(legacy.Password) == "" {
		return nil // incomplete demo config; nothing worth importing
	}

	repo := Repository{
		Meta:        Meta{Name: "Default"},
		BackendType: legacy.BackendType,
		Endpoint:    legacy.Endpoint,
		Bucket:      legacy.Bucket,
		Region:      legacy.Region,
		AccessKey:   legacy.AccessKey,
		SecretKey:   legacy.SecretKey,
		LocalPath:   legacy.LocalPath,
		Password:    legacy.Password,
	}
	if repo.BackendType == "" {
		repo.BackendType = "Local"
	}
	if err := repo.Validate(); err != nil {
		return nil // not complete enough to import; skip
	}
	if _, err := a.repos.Create(repo); err != nil {
		return err
	}
	return nil
}
