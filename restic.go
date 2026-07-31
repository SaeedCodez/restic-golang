package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// resticBinary is the name of the restic executable. It is resolved from PATH.
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

// command builds a restic *exec.Cmd for the active repository, with the
// repository flag prepended and credentials injected via the environment.
func command(ctx context.Context, cfg *Config, args ...string) (*exec.Cmd, error) {
	repo, err := cfg.Repository()
	if err != nil {
		return nil, err
	}
	full := append([]string{"-r", repo}, args...)
	cmd := exec.CommandContext(ctx, resticBinary, full...)
	cmd.Env = cfg.Env()
	return cmd, nil
}

// TestResult describes the outcome of a "Test connection" attempt.
type TestResult struct {
	OK          bool   `json:"ok"`
	Initialized bool   `json:"initialized"`
	Message     string `json:"message"`
	Detail      string `json:"detail,omitempty"`
}

// resticTest runs `restic cat config` to verify the app can reach and decrypt
// the repository, classifying common failure modes into friendly messages.
func resticTest(ctx context.Context, cfg *Config) TestResult {
	cmd, err := command(ctx, cfg, "cat", "config")
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil {
		return TestResult{OK: true, Initialized: true, Message: "Connected successfully. The repository is reachable and the password is correct."}
	}

	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "wrong password") || strings.Contains(low, "no key found"):
		return TestResult{Initialized: true, Message: "Wrong repository password.", Detail: text}
	case strings.Contains(low, "unable to open config") ||
		strings.Contains(low, "is there a repository") ||
		strings.Contains(low, "does not exist") ||
		strings.Contains(low, "no such file") ||
		strings.Contains(low, "the specified bucket does not exist") ||
		strings.Contains(low, "unable to open repository"):
		return TestResult{Initialized: false, Message: "No repository found at this location yet. You can initialize one.", Detail: text}
	default:
		return TestResult{Message: "Could not reach the repository.", Detail: text}
	}
}

// resticInit runs `restic init` to create a new repository. For the Local
// backend it first ensures the target directory exists.
func resticInit(ctx context.Context, cfg *Config) (string, error) {
	if cfg.BackendType == "Local" && strings.TrimSpace(cfg.LocalPath) != "" {
		if err := os.MkdirAll(cfg.LocalPath, 0o755); err != nil {
			return "", fmt.Errorf("could not create local repository directory: %w", err)
		}
	}
	cmd, err := command(ctx, cfg, "init")
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(strings.ToLower(text), "already initialized") ||
			strings.Contains(strings.ToLower(text), "already exists") {
			return text, fmt.Errorf("the repository is already initialized")
		}
		return text, fmt.Errorf("restic init failed: %s", firstLine(text))
	}
	return text, nil
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

// resticSnapshots lists all snapshots in the repository.
func resticSnapshots(ctx context.Context, cfg *Config) ([]Snapshot, error) {
	cmd, err := command(ctx, cfg, "snapshots", "--json")
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, classifyRepoError(string(out))
	}
	return decodeSnapshots(out)
}

// decodeSnapshots parses the JSON output of `restic snapshots --json` into the
// UI-facing Snapshot shape. Shared by the legacy path and the Runner.
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

// runStreaming runs a restic command that emits --json progress, parses every
// line and forwards normalized events to the hub. The op is "backup" or
// "restore" and selects how status/summary fields are interpreted.
func runStreaming(ctx context.Context, cfg *Config, op string, hub *Hub, args ...string) error {
	cmd, err := command(ctx, cfg, args...)
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start restic: %w", err)
	}

	var wg sync.WaitGroup

	// stderr: restic prints human-readable warnings and fatal errors here.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			hub.Send(Event{"type": "log", "op": op, "level": "warn", "message": line})
		}
	}()

	// stdout: one JSON object per line.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			var m resticMessage
			if err := json.Unmarshal(line, &m); err != nil {
				if t := strings.TrimSpace(string(line)); t != "" {
					hub.Send(Event{"type": "log", "op": op, "level": "info", "message": t})
				}
				continue
			}
			emitMessage(hub, op, &m)
		}
	}()

	wg.Wait()
	return cmd.Wait()
}

// emitMessage converts one parsed restic message into a UI event on the hub.
func emitMessage(hub *Hub, op string, m *resticMessage) {
	switch m.MessageType {
	case "status":
		filesDone, bytesDone := m.FilesDone, m.BytesDone
		if op == "restore" {
			// Field names differ slightly across restic versions for restore.
			if m.FilesRestored > 0 {
				filesDone = m.FilesRestored
			}
			if m.BytesRestored > 0 {
				bytesDone = m.BytesRestored
			}
		}
		ev := Event{
			"type":       "status",
			"op":         op,
			"percent":    m.PercentDone,
			"filesDone":  filesDone,
			"totalFiles": m.TotalFiles,
			"bytesDone":  bytesDone,
			"totalBytes": m.TotalBytes,
		}
		if len(m.CurrentFiles) > 0 {
			ev["currentFile"] = m.CurrentFiles[0]
		}
		hub.Send(ev)

	case "summary":
		if op == "restore" {
			files := m.FilesRestored
			bytes := m.BytesRestored
			hub.Send(Event{"type": "summary", "op": op, "summary": Event{
				"filesRestored": files,
				"totalFiles":    m.TotalFiles,
				"bytesRestored": bytes,
				"totalBytes":    m.TotalBytes,
				"totalDuration": m.TotalDuration,
			}})
		} else {
			hub.Send(Event{"type": "summary", "op": op, "summary": Event{
				"filesNew":            m.FilesNew,
				"filesChanged":        m.FilesChanged,
				"filesUnmodified":     m.FilesUnmodified,
				"dirsNew":             m.DirsNew,
				"dirsChanged":         m.DirsChanged,
				"dirsUnmodified":      m.DirsUnmodified,
				"dataAdded":           m.DataAdded,
				"totalFilesProcessed": m.TotalFilesProcessed,
				"totalBytesProcessed": m.TotalBytesProcessed,
				"totalDuration":       m.TotalDuration,
				"snapshotId":          m.SnapshotID,
			}})
		}

	case "error":
		msg := m.Error.Message
		if msg == "" {
			msg = "unknown error"
		}
		if m.During != "" {
			msg = "during " + m.During + ": " + msg
		}
		if m.Item != "" {
			msg += " (" + m.Item + ")"
		}
		hub.Send(Event{"type": "log", "op": op, "level": "error", "message": msg})
	}
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
