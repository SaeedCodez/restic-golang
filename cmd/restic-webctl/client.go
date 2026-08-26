package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"restic-web/internal/core"
)

// CLI is the shared runtime for every command. It opens Postgres and a core.App
// directly — no HTTP round-trip, no session cookie.
type CLI struct {
	cfg  *config
	app  *core.App
	pool *pgxpool.Pool
}

func newCLI(cfg *config) (*CLI, error) {
	dsn := core.ResolveDSN(cfg.database)
	if dsn == "" {
		return nil, fmt.Errorf("%s", core.MissingDSNMessage())
	}
	pool, err := core.OpenDB(context.Background(), dsn)
	if err != nil {
		return nil, err
	}
	app, err := core.NewApp(cfg.dataDir, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &CLI{cfg: cfg, app: app, pool: pool}, nil
}

func (c *CLI) close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

// repoView is a repository with secrets redacted for CLI JSON/human output.
type repoView struct {
	core.Repository
	Password     string `json:"password,omitempty"`
	SecretKey    string `json:"secretKey,omitempty"`
	HasPassword  bool   `json:"hasPassword"`
	HasSecretKey bool   `json:"hasSecretKey"`
}

func repoViewOf(repo core.Repository) repoView {
	v := repoView{
		Repository:   repo,
		HasPassword:  strings.TrimSpace(repo.Password) != "",
		HasSecretKey: strings.TrimSpace(repo.SecretKey) != "",
	}
	v.Repository.Password = ""
	v.Repository.SecretKey = ""
	return v
}

// jobView enriches a job with display fields for list/get output.
type jobView struct {
	core.Job
	FolderName    string    `json:"folderName"`
	FolderPath    string    `json:"folderPath"`
	RepoName      string    `json:"repoName"`
	Tag           string    `json:"tag"`
	LastRun       *core.Run `json:"lastRun,omitempty"`
	RunCount      int       `json:"runCount"`
	ScheduleState string    `json:"scheduleState"`
}

func (c *CLI) viewOf(job core.Job) jobView {
	v := jobView{
		Job:           job,
		Tag:           job.ResticTag(),
		ScheduleState: core.ScheduleStateOff,
	}
	if f, ok := c.app.Folders.Get(job.FolderID); ok {
		v.FolderName = f.Name
		v.FolderPath = f.Path
	}
	if repo, ok := c.app.Repos.Get(job.RepositoryID); ok {
		v.RepoName = repo.Name
	}
	if job.Schedule != nil && job.Schedule.Enabled {
		v.ScheduleState = core.ScheduleStateScheduled
		for _, run := range c.app.Runs.ActiveRuns() {
			if run.JobID == job.ID && run.Kind == core.KindBackup {
				v.ScheduleState = core.ScheduleStateRunning
				break
			}
		}
	}
	runs, total := c.app.Runs.Query("", "", job.ID, 1)
	v.RunCount = total
	if len(runs) > 0 {
		v.LastRun = runs[0]
	}
	return v
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

func jobNames(jobs []core.Job) string {
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
