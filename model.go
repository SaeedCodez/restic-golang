package main

import (
	"errors"
	"os"
	"strings"
	"time"
)

// This file is the single home for the app's persisted domain types. The app is
// built around four user-managed entities — Repository, Folder, Job — plus a Run
// (one execution of any long-running operation) and its append-only LogLine
// stream. Runs and log lines are owned by the RunStore; the three entities are
// owned by generic EntityStores (see store.go).

// Meta is the common identity/timestamp block embedded in every user-managed
// entity. Embedding it flattens id/name/createdAt/updatedAt into the entity's
// JSON, and its pointer method getMeta lets the generic EntityStore read and
// stamp any entity uniformly.
type Meta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (m *Meta) getMeta() *Meta { return m }

// ---- Repository ------------------------------------------------------------

// Repository is a named restic storage location. The backend-mapping logic
// (Repo/Env) is lifted from the demo's single Config, now per-repository.
//
// Note: as in the demo, this is a single-user local tool, so the repository
// password and the S3 secret key are stored in plaintext. Repository is the one
// choke point that reads them, so encrypting at rest later is a localized change.
type Repository struct {
	Meta

	// BackendType selects how the repository is addressed: "Local" or "S3".
	BackendType string `json:"backendType"`

	// Local backend.
	LocalPath string `json:"localPath,omitempty"`

	// S3 backend.
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Region    string `json:"region,omitempty"`
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`

	// Common: the restic repository password used for encryption (required).
	Password string `json:"password"`
}

// Repo returns the restic repository string for this backend, e.g.
// "s3:https://s3.amazonaws.com/my-bucket" or "/path/to/local/repo".
func (r *Repository) Repo() (string, error) {
	switch r.BackendType {
	case "S3":
		ep := strings.TrimSpace(r.Endpoint)
		bucket := strings.TrimSpace(r.Bucket)
		if ep == "" || bucket == "" {
			return "", errors.New("S3 endpoint and bucket are both required")
		}
		ep = strings.TrimSuffix(ep, "/")
		return "s3:" + ep + "/" + bucket, nil
	case "Local":
		p := strings.TrimSpace(r.LocalPath)
		if p == "" {
			return "", errors.New("a local repository directory path is required")
		}
		return p, nil
	default:
		return "", errors.New(`backend type must be "S3" or "Local"`)
	}
}

// Validate checks that the repository is complete enough to run restic against.
func (r *Repository) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("a repository name is required")
	}
	if r.BackendType != "S3" && r.BackendType != "Local" {
		return errors.New(`backend type must be "S3" or "Local"`)
	}
	if strings.TrimSpace(r.Password) == "" {
		return errors.New("a repository password is required")
	}
	_, err := r.Repo()
	return err
}

// Env builds the environment for a restic command targeting this repository.
// Credentials are passed here, via the process environment, never on the command
// line. We start from the current process environment (so PATH etc. are
// preserved) but strip any keys we are about to set, so restic sees unambiguous
// values. RESTIC_PROGRESS_FPS is pinned low so --json status lines stay a trickle
// rather than a firehose the durable log would have to absorb.
func (r *Repository) Env() []string {
	managed := map[string]bool{
		"RESTIC_PASSWORD":       true,
		"RESTIC_REPOSITORY":     true,
		"RESTIC_PROGRESS_FPS":   true,
		"AWS_ACCESS_KEY_ID":     true,
		"AWS_SECRET_ACCESS_KEY": true,
		"AWS_DEFAULT_REGION":    true,
	}

	env := make([]string, 0, len(os.Environ())+6)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 && managed[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}

	env = append(env, "RESTIC_PASSWORD="+r.Password)
	env = append(env, "RESTIC_PROGRESS_FPS=2")
	if r.BackendType == "S3" {
		env = append(env, "AWS_ACCESS_KEY_ID="+r.AccessKey)
		env = append(env, "AWS_SECRET_ACCESS_KEY="+r.SecretKey)
		if strings.TrimSpace(r.Region) != "" {
			env = append(env, "AWS_DEFAULT_REGION="+r.Region)
		}
	}
	return env
}

// ---- Folder ----------------------------------------------------------------

// Folder is a reusable, named source directory to back up.
type Folder struct {
	Meta
	Path string `json:"path"`
}

// Validate checks that the folder is usable.
func (f *Folder) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return errors.New("a folder name is required")
	}
	if strings.TrimSpace(f.Path) == "" {
		return errors.New("a folder path is required")
	}
	return nil
}

// ---- Job -------------------------------------------------------------------

// Job is the core concept: a saved, named pairing of one Folder and one
// Repository. It is the thing the user runs, views and returns to.
type Job struct {
	Meta
	FolderID     string `json:"folderId"`
	RepositoryID string `json:"repositoryId"`
}

// ResticTag is the immutable per-job restic tag stamped on every snapshot this
// job creates, so a job's snapshots are discoverable from the repository itself
// (restic snapshots --tag) even if this app's state is lost. It is derived from
// the job's immutable id, so it is stable across renames without being stored.
func (j *Job) ResticTag() string { return "resticweb-job:" + j.ID }

// Validate checks that the job names a folder and a repository.
func (j *Job) Validate() error {
	if strings.TrimSpace(j.Name) == "" {
		return errors.New("a job name is required")
	}
	if strings.TrimSpace(j.FolderID) == "" {
		return errors.New("please choose a backup folder for this job")
	}
	if strings.TrimSpace(j.RepositoryID) == "" {
		return errors.New("please choose a storage repository for this job")
	}
	return nil
}

// ---- Run -------------------------------------------------------------------

// RunKind is the class of long-running operation a Run represents. Everything
// long-running is a Run, so all are tracked, viewed and stopped the same way.
type RunKind string

const (
	KindBackup   RunKind = "backup"
	KindRestore  RunKind = "restore"
	KindInit     RunKind = "init"
	KindDownload RunKind = "download"
	KindUnlock   RunKind = "unlock"
)

// RunStatus is the lifecycle state of a Run. The durable Run record is the sole
// source of truth for whether an operation is running; a fresh process never
// inherits a live handle, so any persisted "running" is provably stale and gets
// reconciled to "interrupted" on startup.
type RunStatus string

const (
	StatusStarting        RunStatus = "starting"
	StatusRunning         RunStatus = "running"
	StatusSuccess         RunStatus = "success"
	StatusSuccessWarnings RunStatus = "success_warnings"
	StatusFailed          RunStatus = "failed"
	StatusCanceled        RunStatus = "canceled"
	StatusInterrupted     RunStatus = "interrupted"
)

// Terminal reports whether the status is a final state (the run is over).
func (s RunStatus) Terminal() bool {
	switch s {
	case StatusSuccess, StatusSuccessWarnings, StatusFailed, StatusCanceled, StatusInterrupted:
		return true
	}
	return false
}

// Active reports whether the status means work is (or should be) in progress.
func (s RunStatus) Active() bool {
	return s == StatusStarting || s == StatusRunning
}

// Progress is the latest, last-value-wins progress of a running operation. It is
// stored on the Run (overwritten in place, throttled) and streamed live, but is
// never appended to the durable log — that keeps logs readable.
type Progress struct {
	Percent     float64 `json:"percent"`
	FilesDone   int64   `json:"filesDone"`
	TotalFiles  int64   `json:"totalFiles"`
	BytesDone   int64   `json:"bytesDone"`
	TotalBytes  int64   `json:"totalBytes"`
	CurrentFile string  `json:"currentFile,omitempty"`
}

// Summary is what a run actually stored/restored, taken from restic's --json
// summary message. Typed fields carry the headline numbers.
type Summary struct {
	SnapshotID          string  `json:"snapshotId,omitempty"`
	FilesNew            int64   `json:"filesNew"`
	FilesChanged        int64   `json:"filesChanged"`
	FilesUnmodified     int64   `json:"filesUnmodified"`
	DirsNew             int64   `json:"dirsNew"`
	DirsChanged         int64   `json:"dirsChanged"`
	DirsUnmodified      int64   `json:"dirsUnmodified"`
	DataAdded           int64   `json:"dataAdded"`
	TotalFilesProcessed int64   `json:"totalFilesProcessed"`
	TotalBytesProcessed int64   `json:"totalBytesProcessed"`
	FilesRestored       int64   `json:"filesRestored,omitempty"`
	BytesRestored       int64   `json:"bytesRestored,omitempty"`
	TotalDuration       float64 `json:"totalDuration"`
}

// Run is one execution of any long-running operation — the durable unit of
// history. Job/folder/repository labels are denormalized (captured at launch) so
// a run stays fully self-describing even after the entities it referenced are
// edited or deleted.
type Run struct {
	ID     string    `json:"id"`
	Kind   RunKind   `json:"kind"`
	Status RunStatus `json:"status"`

	JobID        string `json:"jobId,omitempty"`
	RepositoryID string `json:"repositoryId"`

	// Denormalized display fields, frozen at launch.
	JobName    string `json:"jobName,omitempty"`
	FolderPath string `json:"folderPath,omitempty"`
	RepoName   string `json:"repoName"`

	// Params records what this run actually did (e.g. source, target, snapshotId).
	Params map[string]string `json:"params,omitempty"`

	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`

	Progress Progress `json:"progress"`
	Summary  *Summary `json:"summary,omitempty"`

	ExitCode *int   `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`
}

// clone returns a deep copy of the run so callers can never mutate stored state
// through a shared reference (Run holds a map and pointers).
func (r *Run) clone() *Run {
	cp := *r
	if r.Params != nil {
		cp.Params = make(map[string]string, len(r.Params))
		for k, v := range r.Params {
			cp.Params[k] = v
		}
	}
	if r.FinishedAt != nil {
		t := *r.FinishedAt
		cp.FinishedAt = &t
	}
	if r.Summary != nil {
		s := *r.Summary
		cp.Summary = &s
	}
	if r.ExitCode != nil {
		c := *r.ExitCode
		cp.ExitCode = &c
	}
	return &cp
}

// DurationSeconds returns how long the run took (or has been running).
func (r *Run) DurationSeconds() float64 {
	end := time.Now().UTC()
	if r.FinishedAt != nil {
		end = *r.FinishedAt
	}
	d := end.Sub(r.StartedAt).Seconds()
	if d < 0 {
		return 0
	}
	return d
}

// LogLine is one entry in a run's append-only log. Seq is a per-run monotonic
// counter assigned by a single serialized appender, so it doubles as the SSE
// event id used to resume a stream without gaps or duplicates.
type LogLine struct {
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Stream  string    `json:"stream,omitempty"` // stdout | stderr | system
	Level   string    `json:"level"`            // info | warn | error | ok
	Message string    `json:"message"`
}
