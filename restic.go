package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// This file holds the stateless restic helpers shared by the Runner: locating
// the binary, the snapshot and --json message shapes, snapshot decoding, and
// error classification. All restic *execution* lives behind the Runner interface
// (runner.go); nothing here shells out except the trivial install/version checks.

// resticBinary is the name of the restic executable, resolved from PATH.
const resticBinary = "restic"

// resticInstalled reports whether the restic binary is available on PATH.
func resticInstalled() bool {
	_, err := exec.LookPath(resticBinary)
	return err == nil
}

// resticVersion returns the output of `restic version`, or an error.
func resticVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, resticBinary, "version").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TestResult describes the outcome of a "Test connection" attempt.
type TestResult struct {
	OK          bool   `json:"ok"`
	Initialized bool   `json:"initialized"`
	Message     string `json:"message"`
	Detail      string `json:"detail,omitempty"`
}

// Snapshot is the simplified, UI-facing shape of a restic snapshot.
type Snapshot struct {
	ID        string   `json:"id"`
	ShortID   string   `json:"shortId"`
	Time      string   `json:"time"`
	Paths     []string `json:"paths"`
	Hostname  string   `json:"hostname"`
	Tags      []string `json:"tags"`
	SizeBytes int64    `json:"sizeBytes"`
	FileCount int64    `json:"fileCount"`
}

// resticSnapshot mirrors the JSON restic emits for `snapshots --json`.
type resticSnapshot struct {
	ID       string   `json:"id"`
	ShortID  string   `json:"short_id"`
	Time     string   `json:"time"`
	Paths    []string `json:"paths"`
	Hostname string   `json:"hostname"`
	Tags     []string `json:"tags"`
	// restic >= 0.17 includes the backup summary inside each snapshot.
	Summary *struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
		TotalFilesProcessed int64 `json:"total_files_processed"`
	} `json:"summary,omitempty"`
}

// decodeSnapshots parses the JSON output of `restic snapshots --json` into the
// UI-facing Snapshot shape.
func decodeSnapshots(out []byte) ([]Snapshot, error) {
	var raw []resticSnapshot
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("could not parse snapshot list: %w", err)
	}

	snaps := make([]Snapshot, 0, len(raw))
	for _, r := range raw {
		s := Snapshot{
			ID:       r.ID,
			ShortID:  r.ShortID,
			Time:     r.Time,
			Paths:    r.Paths,
			Hostname: r.Hostname,
			Tags:     r.Tags,
		}
		if r.Summary != nil {
			s.SizeBytes = r.Summary.TotalBytesProcessed
			s.FileCount = r.Summary.TotalFilesProcessed
		}
		snaps = append(snaps, s)
	}
	return snaps, nil
}

// classifyRepoError turns restic's stderr into a friendly error for repo reads.
func classifyRepoError(text string) error {
	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "wrong password") || strings.Contains(low, "no key found"):
		return errWrongPassword
	case strings.Contains(low, "unable to open config") ||
		strings.Contains(low, "is there a repository") ||
		strings.Contains(low, "does not exist") ||
		strings.Contains(low, "no such file") ||
		strings.Contains(low, "the specified bucket does not exist") ||
		strings.Contains(low, "unable to open repository"):
		return errNotInitialized
	default:
		return fmt.Errorf("%s", firstLine(strings.TrimSpace(text)))
	}
}

// Sentinel errors so handlers/runner can special-case common repo states.
var (
	errNotInitialized = errors.New("repository is not initialized")
	errWrongPassword  = errors.New("wrong repository password")
)

// resticMessage captures every field we care about across restic's --json
// "status", "summary" and "error" messages, for both backup and restore.
type resticMessage struct {
	MessageType string `json:"message_type"`

	// status (backup + restore)
	PercentDone  float64  `json:"percent_done"`
	TotalFiles   int64    `json:"total_files"`
	TotalBytes   int64    `json:"total_bytes"`
	CurrentFiles []string `json:"current_files"`

	// backup status / summary
	FilesDone int64 `json:"files_done"`
	BytesDone int64 `json:"bytes_done"`

	// restore status / summary
	FilesRestored int64 `json:"files_restored"`
	BytesRestored int64 `json:"bytes_restored"`

	// backup summary
	FilesNew            int64   `json:"files_new"`
	FilesChanged        int64   `json:"files_changed"`
	FilesUnmodified     int64   `json:"files_unmodified"`
	DirsNew             int64   `json:"dirs_new"`
	DirsChanged         int64   `json:"dirs_changed"`
	DirsUnmodified      int64   `json:"dirs_unmodified"`
	DataAdded           int64   `json:"data_added"`
	TotalFilesProcessed int64   `json:"total_files_processed"`
	TotalBytesProcessed int64   `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
	SnapshotID          string  `json:"snapshot_id"`

	// error
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	During string `json:"during"`
	Item   string `json:"item"`
}

// firstLine returns the first non-empty line of s, useful for terse errors.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(s)
}
