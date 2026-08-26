package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// App is the central container of the server's dependencies: the three entity
// stores, the run store, the restic runner and the coordinator. Every handler
// reaches its collaborators through one place.
type App struct {
	DataDir          string
	Pool             *pgxpool.Pool
	RetainRunsPerJob int

	Repos   *RepoStore
	Folders *FolderStore
	Jobs    *JobStore

	Auth     *AuthStore
	Sessions *SessionManager

	Runs   *RunStore
	Runner Runner
	Bus    eventBus
	Bcast  *broadcaster
	Coord  *Coordinator
	Sched  *Scheduler
}

// NewApp builds an App backed by the real restic runner.
func NewApp(dataDir string, pool *pgxpool.Pool) (*App, error) {
	return NewAppWithRunner(dataDir, pool, newResticRunner())
}

// NewAppWithRunner builds an App with a caller-supplied Runner, so tests can
// inject a fake and exercise the whole pipeline with no restic binary.
func NewAppWithRunner(dataDir string, pool *pgxpool.Pool, runner Runner) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("could not create data directory %q: %w", dataDir, err)
	}

	bcast := newBroadcaster()
	bus := eventBus(bcast)
	auth, err := loadAuthStore(pool)
	if err != nil {
		return nil, err
	}

	app := &App{
		DataDir:  dataDir,
		Pool:     pool,
		Repos:    newRepoStore(pool),
		Folders:  newFolderStore(pool),
		Jobs:     newJobStore(pool),
		Auth:     auth,
		Sessions: newSessionManager(),
		Runs:     newRunStore(pool, bus),
		Runner:   runner,
		Bus:      bus,
		Bcast:    bcast,
	}
	app.Coord = newCoordinator(app, app.Runs, runner, bus)
	app.Sched = newScheduler(app)
	return app, nil
}

// JobsUsingRepository returns the jobs that reference the given repository id.
func (a *App) JobsUsingRepository(repoID string) []Job {
	var out []Job
	for _, j := range a.Jobs.List() {
		if j.RepositoryID == repoID {
			out = append(out, j)
		}
	}
	return out
}

// JobsUsingFolder returns the jobs that reference the given folder id.
func (a *App) JobsUsingFolder(folderID string) []Job {
	var out []Job
	for _, j := range a.Jobs.List() {
		if j.FolderID == folderID {
			out = append(out, j)
		}
	}
	return out
}

// resolveJob returns a job together with its folder and repository, or an error
// if any of them is missing (e.g. the folder or repository was deleted).
func (a *App) resolveJob(jobID string) (Job, Folder, Repository, error) {
	job, ok := a.Jobs.Get(jobID)
	if !ok {
		return Job{}, Folder{}, Repository{}, notFoundf("job not found")
	}
	folder, ok := a.Folders.Get(job.FolderID)
	if !ok {
		return job, Folder{}, Repository{}, validf("this job's backup folder no longer exists; edit the job to pick another")
	}
	repo, ok := a.Repos.Get(job.RepositoryID)
	if !ok {
		return job, folder, Repository{}, validf("this job's storage repository no longer exists; edit the job to pick another")
	}
	return job, folder, repo, nil
}

// importLegacyConfig performs a one-time migration from the demo's single-config
// file: if there are no repositories yet and a legacy config.json holds a valid
// configuration, it is imported as a repository named "Default". This lets an
// existing demo user land softly. It is best-effort and never fatal.
func (a *App) ImportLegacyConfig(configPath string) error {
	if a.Repos.Count() > 0 {
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
	if _, err := a.Repos.Create(repo); err != nil {
		return err
	}
	return nil
}
