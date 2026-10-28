package core

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This file is the single home for the app's persisted domain types. The app is
// built around four user-managed entities — Repository, Folder, Database, Job —
// plus a Run (one execution of any long-running operation) and its append-only
// LogLine stream. Runs and log lines are owned by the RunStore; the entities are
// owned by Postgres-backed stores (see pg_entities.go).

// Meta is the common identity/timestamp block embedded in every user-managed
// entity. Embedding it flattens id/name/createdAt/updatedAt into the entity's
// JSON. getMeta lets stores read and stamp identity fields uniformly.
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

	// The settings below are saved through their own endpoints (SetCheckSchedule,
	// SetSpeedLimit); creating or editing a repository leaves them as they are.

	// CheckSchedule, when enabled, checks the repository with `restic check` on
	// its own.
	CheckSchedule *RepoCheckSchedule `json:"checkSchedule,omitempty"`
	// UploadLimitKiB and DownloadLimitKiB cap how fast restic sends data to and
	// reads data from this repository, in KiB/s (restic's --limit-upload and
	// --limit-download); 0 means no limit.
	UploadLimitKiB   int `json:"uploadLimitKiB"`
	DownloadLimitKiB int `json:"downloadLimitKiB"`
}

// maxSpeedLimitKiB bounds a speed limit at 10 GiB/s, far above any real link.
const maxSpeedLimitKiB = 10 << 20

// ValidateSpeedLimit checks an upload or download limit in KiB/s.
func ValidateSpeedLimit(kib int) error {
	if kib < 0 || kib > maxSpeedLimitKiB {
		return validf("a speed limit must be between 0 (no limit) and %d KiB/s", maxSpeedLimitKiB)
	}
	return nil
}

// limitArgs are restic's global flags for the repository's speed limits.
func (r *Repository) limitArgs() []string {
	var args []string
	if r.UploadLimitKiB > 0 {
		args = append(args, "--limit-upload", strconv.Itoa(r.UploadLimitKiB))
	}
	if r.DownloadLimitKiB > 0 {
		args = append(args, "--limit-download", strconv.Itoa(r.DownloadLimitKiB))
	}
	return args
}

// ---- Repository check schedule ----------------------------------------------

// How often a scheduled repository check runs.
const (
	CheckEveryDay   = "daily"
	CheckEveryWeek  = "weekly"
	CheckEveryMonth = "monthly"
)

// defaultCheckAt is when a scheduled check runs if no time is set: a quiet
// hour, since backups to the repository wait while it is checked.
const defaultCheckAt = "04:00"

// checkReadDataPattern is a percentage of the stored data for a check to read.
var checkReadDataPattern = regexp.MustCompile(`^([1-9][0-9]?|100)%$`)

// RepoCheckSchedule checks a repository on its own, so damage in its stored
// data shows up before a restore needs that data. The first check runs at the
// first At after the schedule was saved (Since); after that, at the first At
// once Every has passed since the last check, a manual check included.
type RepoCheckSchedule struct {
	Enabled bool   `json:"enabled"`
	Every   string `json:"every"` // daily | weekly | monthly
	At      string `json:"at"`    // "HH:MM", local time
	// ReadData is "" to check the repository's structure only, or a percentage
	// ("5%") of the stored data to download and verify each time.
	ReadData string `json:"readData,omitempty"`
	// Since is when the schedule was saved; the server sets it.
	Since time.Time `json:"since"`
}

// Normalize fills in defaults: weekly, at defaultCheckAt.
func (c *RepoCheckSchedule) Normalize() {
	c.Every = strings.ToLower(strings.TrimSpace(c.Every))
	if c.Every == "" {
		c.Every = CheckEveryWeek
	}
	c.At = strings.TrimSpace(c.At)
	if c.At == "" {
		c.At = defaultCheckAt
	}
	c.ReadData = strings.TrimSpace(c.ReadData)
}

// Validate checks a normalized schedule.
func (c *RepoCheckSchedule) Validate() error {
	if c.period() == 0 {
		return validf(`how often must be "daily", "weekly" or "monthly"`)
	}
	if _, _, err := parseClock(c.At); err != nil {
		return validf("the check time must be HH:MM, e.g. 04:00")
	}
	if c.ReadData != "" && !checkReadDataPattern.MatchString(c.ReadData) {
		return validf(`the data to read must be empty or a percentage from 1%% to 100%%, e.g. "5%%"`)
	}
	return nil
}

// Describe is a short summary, e.g. "weekly at 04:00, reading 5% of the data".
func (c *RepoCheckSchedule) Describe() string {
	s := c.Every + " at " + c.At
	if c.ReadData != "" {
		s += ", reading " + c.ReadData + " of the data"
	}
	return s
}

// period is the time between scheduled checks.
func (c *RepoCheckSchedule) period() time.Duration {
	switch c.Every {
	case CheckEveryDay:
		return 24 * time.Hour
	case CheckEveryWeek:
		return 7 * 24 * time.Hour
	case CheckEveryMonth:
		return 30 * 24 * time.Hour
	default:
		return 0
	}
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
	// Relative to what would be unclear (the app's working directory), and an
	// absolute path also cannot start with "-" and be taken for an option.
	if !filepath.IsAbs(f.Path) {
		return errors.New("the folder path must be absolute, e.g. /data/site")
	}
	return nil
}

// ---- Database --------------------------------------------------------------

// Database is a reusable, named database to back up: one database inside a
// database server container. It is dumped and restored with the server's own
// tools via docker exec, so no credentials are stored here.
type Database struct {
	Meta
	Engine string `json:"engine"` // postgres | mysql | mariadb
	// Container is the container's name when the source was saved. Coolify can
	// recreate a container under a new name, so the owner fields below are what
	// finds it again.
	Container string `json:"container"`
	// OwnerKind/OwnerUUID name the resource that runs the container (a Coolify
	// application, service or database, or a compose project) and OwnerService
	// the container's part in it.
	OwnerKind    string `json:"ownerKind,omitempty"`
	OwnerUUID    string `json:"ownerUuid,omitempty"`
	OwnerService string `json:"ownerService,omitempty"`
	// DatabaseName is the database inside the server.
	DatabaseName string `json:"database"`
}

// Validate checks that the database source is usable.
func (d *Database) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("a database source name is required")
	}
	if !validEngine(d.Engine) {
		return errors.New(`engine must be "postgres", "mysql" or "mariadb"`)
	}
	if strings.TrimSpace(d.Container) == "" {
		return errors.New("a database container is required")
	}
	if strings.TrimSpace(d.DatabaseName) == "" {
		return errors.New("a database name is required")
	}
	return validateDBName(d.DatabaseName)
}

// Describe is a short label for the source, e.g. "MariaDB shop on shared-db".
func (d *Database) Describe() string {
	return engineLabel(d.Engine) + " " + d.DatabaseName + " on " + d.Container
}

// ---- Job -------------------------------------------------------------------

// ScheduleKind is how often an automatic backup fires.
const (
	ScheduleHourly = "hourly"
	ScheduleEvery  = "every"
	ScheduleDaily  = "daily"
	ScheduleWeekly = "weekly"
)

// ScheduleState is a derived label for a job's automatic-backup health,
// computed for the API — never persisted.
const (
	ScheduleStateOff       = "off"
	ScheduleStateScheduled = "scheduled"
	ScheduleStateRunning   = "running"
	ScheduleStateOverdue   = "overdue"
)

// Trigger values record why a backup Run started (stored on Run.Params["trigger"]).
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
)

// JobSchedule is an optional automatic-backup cadence attached to a Job.
// Missing/nil means the job is manual-only. Time-of-day fields use the
// machine's local zone.
type JobSchedule struct {
	Enabled  bool   `json:"enabled"`
	Kind     string `json:"kind"`               // hourly | every | daily | weekly
	Every    string `json:"every,omitempty"`    // Kind=="every": Go duration, e.g. "6h"
	At       string `json:"at,omitempty"`       // "HH:MM" local time for daily/weekly
	Weekdays []int  `json:"weekdays,omitempty"` // 0=Sunday … 6=Saturday for weekly
}

// Retention presets fill keep-* counts. "custom" means the stored numbers are
// authoritative.
const (
	RetentionPresetLight    = "light"
	RetentionPresetBalanced = "balanced"
	RetentionPresetLong     = "long"
	RetentionPresetCustom   = "custom"
)

// JobRetention is an optional snapshot keep-policy on a Job. When enabled,
// successful backups are followed by a retention run (`restic forget --prune`)
// scoped to this job's tag. Missing/nil means off and never configured.
type JobRetention struct {
	Enabled bool   `json:"enabled"`
	Preset  string `json:"preset,omitempty"` // light | balanced | long | custom

	KeepLast       int `json:"keepLast,omitempty"`
	KeepHourly     int `json:"keepHourly,omitempty"`
	KeepDaily      int `json:"keepDaily,omitempty"`
	KeepWeekly     int `json:"keepWeekly,omitempty"`
	KeepMonthly    int `json:"keepMonthly,omitempty"`
	KeepWithinDays int `json:"keepWithinDays,omitempty"` // optional; becomes restic --keep-within Nd
}

// RetentionPresetValues returns the keep counts for a named preset.
func RetentionPresetValues(preset string) JobRetention {
	switch preset {
	case RetentionPresetLight:
		return JobRetention{Preset: RetentionPresetLight, KeepLast: 24, KeepDaily: 7}
	case RetentionPresetBalanced:
		return JobRetention{Preset: RetentionPresetBalanced, KeepLast: 24, KeepDaily: 7, KeepWeekly: 4}
	case RetentionPresetLong:
		return JobRetention{Preset: RetentionPresetLong, KeepLast: 48, KeepDaily: 14, KeepWeekly: 8, KeepMonthly: 12}
	default:
		return JobRetention{Preset: RetentionPresetCustom}
	}
}

// Normalize applies a named preset's counts (custom is left as-is) and trims
// negative values to zero.
func (r *JobRetention) Normalize() {
	if r == nil {
		return
	}
	switch r.Preset {
	case RetentionPresetLight, RetentionPresetBalanced, RetentionPresetLong:
		p := RetentionPresetValues(r.Preset)
		r.KeepLast = p.KeepLast
		r.KeepHourly = p.KeepHourly
		r.KeepDaily = p.KeepDaily
		r.KeepWeekly = p.KeepWeekly
		r.KeepMonthly = p.KeepMonthly
		// keepWithinDays is only meaningful on custom
		r.KeepWithinDays = 0
	case "", RetentionPresetCustom:
		if r.Preset == "" {
			r.Preset = RetentionPresetCustom
		}
	}
	if r.KeepLast < 0 {
		r.KeepLast = 0
	}
	if r.KeepHourly < 0 {
		r.KeepHourly = 0
	}
	if r.KeepDaily < 0 {
		r.KeepDaily = 0
	}
	if r.KeepWeekly < 0 {
		r.KeepWeekly = 0
	}
	if r.KeepMonthly < 0 {
		r.KeepMonthly = 0
	}
	if r.KeepWithinDays < 0 {
		r.KeepWithinDays = 0
	}
}

// HasKeepRule reports whether at least one keep-* / keep-within rule is set.
func (r *JobRetention) HasKeepRule() bool {
	if r == nil {
		return false
	}
	return r.KeepLast > 0 || r.KeepHourly > 0 || r.KeepDaily > 0 ||
		r.KeepWeekly > 0 || r.KeepMonthly > 0 || r.KeepWithinDays > 0
}

// Validate checks that an enabled retention policy is usable. Disabled policies
// are always valid (config may be kept while paused).
func (r *JobRetention) Validate() error {
	if r == nil || !r.Enabled {
		return nil
	}
	switch r.Preset {
	case RetentionPresetLight, RetentionPresetBalanced, RetentionPresetLong, RetentionPresetCustom, "":
	default:
		return errors.New(`retention preset must be "light", "balanced", "long", or "custom"`)
	}
	r.Normalize()
	if !r.HasKeepRule() {
		return errors.New("retention needs at least one keep rule (last, hourly, daily, weekly, monthly, or within days)")
	}
	if r.KeepWithinDays > 36500 {
		return errors.New("keep within days is too large")
	}
	const maxKeep = 100000
	for _, n := range []int{r.KeepLast, r.KeepHourly, r.KeepDaily, r.KeepWeekly, r.KeepMonthly} {
		if n > maxKeep {
			return errors.New("a keep count is too large")
		}
	}
	return nil
}

// Describe returns a short human label, e.g. "last 24, 7 daily, 4 weekly".
func (r *JobRetention) Describe() string {
	if r == nil {
		return ""
	}
	parts := make([]string, 0, 6)
	if r.KeepLast > 0 {
		parts = append(parts, "last "+strconv.Itoa(r.KeepLast))
	}
	if r.KeepHourly > 0 {
		parts = append(parts, strconv.Itoa(r.KeepHourly)+" hourly")
	}
	if r.KeepDaily > 0 {
		parts = append(parts, strconv.Itoa(r.KeepDaily)+" daily")
	}
	if r.KeepWeekly > 0 {
		parts = append(parts, strconv.Itoa(r.KeepWeekly)+" weekly")
	}
	if r.KeepMonthly > 0 {
		parts = append(parts, strconv.Itoa(r.KeepMonthly)+" monthly")
	}
	if r.KeepWithinDays > 0 {
		parts = append(parts, "within "+strconv.Itoa(r.KeepWithinDays)+"d")
	}
	return strings.Join(parts, ", ")
}

// Job is the core concept: a saved, named pairing of one source — a Folder or a
// Database — and one Repository. It is the thing the user runs, views and
// returns to. An optional Schedule makes the same job fire automatically while
// the process is running. An optional Retention policy forgets old snapshots
// for this job after backups.
type Job struct {
	Meta
	FolderID     string        `json:"folderId,omitempty"`
	DatabaseID   string        `json:"databaseId,omitempty"`
	RepositoryID string        `json:"repositoryId"`
	Schedule     *JobSchedule  `json:"schedule,omitempty"`
	Retention    *JobRetention `json:"retention,omitempty"`
}

// ResticTag is the immutable per-job restic tag stamped on every snapshot this
// job creates, so a job's snapshots are discoverable from the repository itself
// (restic snapshots --tag) even if this app's state is lost. It is derived from
// the job's immutable id, so it is stable across renames without being stored.
func (j *Job) ResticTag() string { return "resticweb-job:" + j.ID }

// IsDatabase reports whether the job backs up a database rather than a folder.
func (j *Job) IsDatabase() bool { return j.DatabaseID != "" }

// Validate checks that the job names exactly one source (a folder or a
// database) and a repository, and that any attached schedule or retention
// policy is well-formed.
func (j *Job) Validate() error {
	if strings.TrimSpace(j.Name) == "" {
		return errors.New("a job name is required")
	}
	j.FolderID = strings.TrimSpace(j.FolderID)
	j.DatabaseID = strings.TrimSpace(j.DatabaseID)
	if j.FolderID == "" && j.DatabaseID == "" {
		return errors.New("please choose a backup folder or database for this job")
	}
	if j.FolderID != "" && j.DatabaseID != "" {
		return errors.New("a job backs up either a folder or a database, not both")
	}
	if strings.TrimSpace(j.RepositoryID) == "" {
		return errors.New("please choose a storage repository for this job")
	}
	if j.Schedule != nil {
		if err := j.Schedule.Validate(); err != nil {
			return err
		}
	}
	if j.Retention != nil {
		if err := j.Retention.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks that a schedule's kind and fields are usable.
func (s *JobSchedule) Validate() error {
	if s == nil {
		return nil
	}
	switch s.Kind {
	case ScheduleHourly:
		// no extra fields
	case ScheduleEvery:
		d, err := time.ParseDuration(strings.TrimSpace(s.Every))
		if err != nil || d < time.Hour {
			return errors.New(`"every" schedules need a duration of at least 1h (e.g. "6h")`)
		}
	case ScheduleDaily:
		if _, _, err := parseClock(s.At); err != nil {
			return err
		}
	case ScheduleWeekly:
		if _, _, err := parseClock(s.At); err != nil {
			return err
		}
		if len(s.Weekdays) == 0 {
			return errors.New("please choose at least one weekday for a weekly schedule")
		}
		seen := map[int]bool{}
		for _, d := range s.Weekdays {
			if d < 0 || d > 6 {
				return errors.New("weekdays must be between 0 (Sunday) and 6 (Saturday)")
			}
			if seen[d] {
				continue
			}
			seen[d] = true
		}
		// Normalize to a sorted unique list so storage is stable.
		out := make([]int, 0, len(seen))
		for d := 0; d <= 6; d++ {
			if seen[d] {
				out = append(out, d)
			}
		}
		s.Weekdays = out
	case "":
		return errors.New("a schedule kind is required (hourly, every, daily, or weekly)")
	default:
		return errors.New(`schedule kind must be "hourly", "every", "daily", or "weekly"`)
	}
	return nil
}

// parseClock parses "HH:MM" or "HH:MM:SS" (24h) into hour and minute.
func parseClock(at string) (hour, min int, err error) {
	at = strings.TrimSpace(at)
	if at == "" {
		return 0, 0, errors.New(`please set a time of day as "HH:MM" (e.g. "02:00")`)
	}
	parts := strings.Split(at, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, errors.New(`time of day must look like "HH:MM" (e.g. "02:00")`)
	}
	h, errH := parseIntRange(parts[0], 0, 23)
	m, errM := parseIntRange(parts[1], 0, 59)
	if errH != nil || errM != nil {
		return 0, 0, errors.New(`time of day must look like "HH:MM" (e.g. "02:00")`)
	}
	if len(parts) == 3 {
		if _, errS := parseIntRange(parts[2], 0, 59); errS != nil {
			return 0, 0, errors.New(`time of day must look like "HH:MM" (e.g. "02:00")`)
		}
	}
	return h, m, nil
}

func parseIntRange(s string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < lo || n > hi {
		return 0, errors.New("out of range")
	}
	return n, nil
}

// Describe returns a short human label for the schedule, e.g. "daily at 02:00".
func (s *JobSchedule) Describe() string {
	if s == nil {
		return ""
	}
	switch s.Kind {
	case ScheduleHourly:
		return "hourly"
	case ScheduleEvery:
		return "every " + strings.TrimSpace(s.Every)
	case ScheduleDaily:
		return "daily at " + strings.TrimSpace(s.At)
	case ScheduleWeekly:
		names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
		parts := make([]string, 0, len(s.Weekdays))
		for _, d := range s.Weekdays {
			if d >= 0 && d < len(names) {
				parts = append(parts, names[d])
			}
		}
		return "weekly on " + strings.Join(parts, ", ") + " at " + strings.TrimSpace(s.At)
	default:
		return s.Kind
	}
}

// ---- Run -------------------------------------------------------------------

// RunKind is the class of long-running operation a Run represents. Everything
// long-running is a Run, so all are tracked, viewed and stopped the same way.
type RunKind string

const (
	KindBackup    RunKind = "backup"
	KindRestore   RunKind = "restore"
	KindInit      RunKind = "init"
	KindDownload  RunKind = "download"
	KindUnlock    RunKind = "unlock"
	KindRetention RunKind = "retention"
	KindForget    RunKind = "forget"
	KindCheck     RunKind = "check"
	// KindRestoreDB replaces a database with a dump from a snapshot.
	KindRestoreDB RunKind = "restore_db"
	// KindSpeedTest measures how fast a repository takes and returns data.
	KindSpeedTest RunKind = "speedtest"
)

// TriggerAfterBackup marks a retention run started after a successful backup.
const TriggerAfterBackup = "after_backup"

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

	// Speed is the result of a speed test run; nil for every other kind.
	Speed *SpeedTest `json:"speed,omitempty"`
}

// SpeedTest is what a speed test measured. Upload and download are the time
// the test data took to move, with the cost of backing up and restoring a tiny
// file taken out; that cost is reported on its own as Overhead, the time every
// backup or restore spends opening the repository before any data moves.
type SpeedTest struct {
	SizeBytes           int64   `json:"sizeBytes"`
	UploadSeconds       float64 `json:"uploadSeconds"`
	DownloadSeconds     float64 `json:"downloadSeconds"`
	OverheadSeconds     float64 `json:"overheadSeconds"`
	UploadBytesPerSec   float64 `json:"uploadBytesPerSec"`
	DownloadBytesPerSec float64 `json:"downloadBytesPerSec"`
	// Score is 0–100; Rating names its band ("Excellent" … "Very slow").
	Score  int    `json:"score"`
	Rating string `json:"rating"`
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

	// PID is the restic child process id (its own process-group leader), persisted
	// so startup reconciliation can reap an orphan a crash may have left behind.
	// PIDStart is an opaque process-start token (Linux: /proc start time) used to
	// confirm the pid still refers to the SAME process before reaping, so a
	// recycled pid is never killed.
	PID      int    `json:"pid,omitempty"`
	PIDStart string `json:"pidStart,omitempty"`

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
		if r.Summary.Speed != nil {
			sp := *r.Summary.Speed
			s.Speed = &sp
		}
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
