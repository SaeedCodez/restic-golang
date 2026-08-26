package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RunSink receives the normalized events of a running restic operation. The
// coordinator implements it to persist events durably and broadcast them live;
// tests implement it to capture events. Progress is last-value-wins (streamed
// and stored on the run, never appended to the log); Log entries are the durable,
// human-meaningful timeline; Summary is the final result; PID reports the restic
// process id for crash reconciliation.
type RunSink interface {
	Log(level, stream, message string)
	Progress(p Progress)
	Summary(s Summary)
	PID(pid int, startToken string)
}

// Runner abstracts every restic operation the app performs. All restic access
// goes through this interface, so the entire run pipeline can be exercised with
// a fake and no restic binary installed.
type Runner interface {
	// Available reports whether the restic binary is on PATH.
	Available() bool
	// Version returns `restic version` output.
	Version(ctx context.Context) (string, error)
	// Test verifies the repository is reachable and the password is correct.
	Test(ctx context.Context, repo *Repository) TestResult
	// Init creates a new repository, returning restic's output.
	Init(ctx context.Context, repo *Repository) (string, error)
	// Snapshots lists snapshots, optionally filtered to a single tag.
	Snapshots(ctx context.Context, repo *Repository, tag string) ([]Snapshot, error)
	// Unlock removes stale locks left by a crashed/killed restic (restic unlock).
	Unlock(ctx context.Context, repo *Repository) error
	// Backup streams a backup of source into repo, tagging each with tags, and
	// returns restic's exit code. err is non-nil only if restic could not be
	// started; a non-zero exit is reported via the code, not err.
	Backup(ctx context.Context, repo *Repository, source string, tags []string, sink RunSink) (int, error)
	// Restore streams a restore of snapshotID into target, returning the exit code.
	Restore(ctx context.Context, repo *Repository, snapshotID, target string, sink RunSink) (int, error)
	// Forget applies a keep-policy to snapshots with the given tag, then prunes
	// unreferenced data (`restic forget --prune`). Returns restic's exit code.
	Forget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error)
}

// resticRunner is the real Runner: it shells out to the restic CLI.
type resticRunner struct{}

func newResticRunner() *resticRunner { return &resticRunner{} }

func (*resticRunner) Available() bool { return ResticInstalled() }

func (*resticRunner) Version(ctx context.Context) (string, error) { return resticVersion(ctx) }

// resticCommand builds a restic *exec.Cmd for a repository: repo flag prepended,
// credentials injected via env, graceful-stop signaling wired up (SIGINT with a
// hard-kill fallback), and its own process group for orphan handling.
func resticCommand(ctx context.Context, repo *Repository, args ...string) (*exec.Cmd, error) {
	repoStr, err := repo.Repo()
	if err != nil {
		return nil, err
	}
	full := append([]string{"-r", repoStr}, args...)
	cmd := exec.CommandContext(ctx, resticBinary, full...)
	cmd.Env = repo.Env()
	// Stop = SIGINT: restic finalizes, removes its lock, and writes no partial
	// snapshot. WaitDelay force-kills if it does not exit within the grace window.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	configureSysProcAttr(cmd)
	return cmd, nil
}

func (*resticRunner) Test(ctx context.Context, repo *Repository) TestResult {
	cmd, err := resticCommand(ctx, repo, "cat", "config")
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil {
		return TestResult{OK: true, Initialized: true, Message: "Connected successfully. The repository is reachable and the password is correct."}
	}
	switch classifyRepoError(text) {
	case errWrongPassword:
		return TestResult{Initialized: true, Message: "Wrong repository password.", Detail: text}
	case errNotInitialized:
		return TestResult{Initialized: false, Message: "No repository found at this location yet. You can initialize one.", Detail: text}
	default:
		return TestResult{Message: "Could not reach the repository.", Detail: text}
	}
}

func (*resticRunner) Init(ctx context.Context, repo *Repository) (string, error) {
	if repo.BackendType == "Local" && strings.TrimSpace(repo.LocalPath) != "" {
		if err := os.MkdirAll(repo.LocalPath, 0o755); err != nil {
			return "", fmt.Errorf("could not create local repository directory: %w", err)
		}
	}
	cmd, err := resticCommand(ctx, repo, "init")
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		low := strings.ToLower(text)
		if strings.Contains(low, "already initialized") || strings.Contains(low, "already exists") {
			return text, fmt.Errorf("the repository is already initialized")
		}
		return text, fmt.Errorf("restic init failed: %s", firstLine(text))
	}
	return text, nil
}

func (*resticRunner) Snapshots(ctx context.Context, repo *Repository, tag string) ([]Snapshot, error) {
	args := []string{"snapshots", "--json"}
	if strings.TrimSpace(tag) != "" {
		args = append(args, "--tag", tag)
	}
	cmd, err := resticCommand(ctx, repo, args...)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, classifyRepoError(string(out))
	}
	return decodeSnapshots(out)
}

func (*resticRunner) Unlock(ctx context.Context, repo *Repository) error {
	// `restic unlock` (without --remove-all) removes only stale locks, so it is
	// safe to run defensively; a genuinely-live lock from another process is left
	// alone.
	cmd, err := resticCommand(ctx, repo, "unlock")
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic unlock failed: %s", firstLine(strings.TrimSpace(string(out))))
	}
	return nil
}

func (*resticRunner) Backup(ctx context.Context, repo *Repository, source string, tags []string, sink RunSink) (int, error) {
	args := []string{"backup", source, "--json"}
	for _, tg := range tags {
		if strings.TrimSpace(tg) != "" {
			args = append(args, "--tag", tg)
		}
	}
	return streamRestic(ctx, repo, KindBackup, sink, args...)
}

func (*resticRunner) Restore(ctx context.Context, repo *Repository, snapshotID, target string, sink RunSink) (int, error) {
	return streamRestic(ctx, repo, KindRestore, sink, "restore", snapshotID, "--target", target, "--json")
}

func (*resticRunner) Forget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error) {
	args, err := forgetArgs(tag, policy)
	if err != nil {
		return -1, err
	}
	return streamRestic(ctx, repo, KindRetention, sink, args...)
}

// forgetArgs builds `restic forget --prune` arguments for a job-scoped policy.
func forgetArgs(tag string, policy JobRetention) ([]string, error) {
	policy.Normalize()
	if strings.TrimSpace(tag) == "" {
		return nil, fmt.Errorf("a job tag is required for retention")
	}
	if !policy.HasKeepRule() {
		return nil, fmt.Errorf("retention needs at least one keep rule")
	}
	args := []string{"forget", "--json", "--prune", "--tag", tag}
	if policy.KeepLast > 0 {
		args = append(args, "--keep-last", strconv.Itoa(policy.KeepLast))
	}
	if policy.KeepHourly > 0 {
		args = append(args, "--keep-hourly", strconv.Itoa(policy.KeepHourly))
	}
	if policy.KeepDaily > 0 {
		args = append(args, "--keep-daily", strconv.Itoa(policy.KeepDaily))
	}
	if policy.KeepWeekly > 0 {
		args = append(args, "--keep-weekly", strconv.Itoa(policy.KeepWeekly))
	}
	if policy.KeepMonthly > 0 {
		args = append(args, "--keep-monthly", strconv.Itoa(policy.KeepMonthly))
	}
	if policy.KeepWithinDays > 0 {
		args = append(args, "--keep-within", strconv.Itoa(policy.KeepWithinDays)+"d")
	}
	return args, nil
}

// streamRestic runs a restic command that emits --json progress, forwarding
// normalized events to sink. It returns restic's exit code; err is set only if
// the process could not be started.
func streamRestic(ctx context.Context, repo *Repository, kind RunKind, sink RunSink, args ...string) (int, error) {
	cmd, err := resticCommand(ctx, repo, args...)
	if err != nil {
		return -1, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("could not start restic: %w", err)
	}
	if cmd.Process != nil {
		sink.PID(cmd.Process.Pid, procStartToken(cmd.Process.Pid))
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
			// restic may emit structured JSON (e.g. an exit_error) on stderr; route
			// it through the mapper so it reads cleanly instead of as raw JSON.
			if strings.HasPrefix(line, "{") {
				var m resticMessage
				if err := json.Unmarshal([]byte(line), &m); err == nil && m.MessageType != "" {
					mapResticMessage(kind, &m, sink)
					continue
				}
			}
			sink.Log("warn", "stderr", line)
		}
		// Keep draining even if the scanner aborted (e.g. an over-long line), so
		// restic can never block writing to a full pipe and wedge cmd.Wait.
		_, _ = io.Copy(io.Discard, stderr)
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
					sink.Log("info", "stdout", t)
				}
				continue
			}
			mapResticMessage(kind, &m, sink)
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	wg.Wait()
	return exitCode(cmd.Wait()), nil
}

// mapResticMessage converts one parsed restic --json message into sink events.
func mapResticMessage(kind RunKind, m *resticMessage, sink RunSink) {
	switch m.MessageType {
	case "status":
		filesDone, bytesDone := m.FilesDone, m.BytesDone
		if kind == KindRestore {
			// Field names differ slightly across restic versions for restore.
			if m.FilesRestored > 0 {
				filesDone = m.FilesRestored
			}
			if m.BytesRestored > 0 {
				bytesDone = m.BytesRestored
			}
		}
		p := Progress{
			Percent:    m.PercentDone,
			FilesDone:  filesDone,
			TotalFiles: m.TotalFiles,
			BytesDone:  bytesDone,
			TotalBytes: m.TotalBytes,
		}
		if len(m.CurrentFiles) > 0 {
			p.CurrentFile = m.CurrentFiles[0]
		}
		sink.Progress(p)

	case "summary":
		s := Summary{TotalDuration: m.TotalDuration}
		if kind == KindRestore {
			s.FilesRestored = m.FilesRestored
			s.BytesRestored = m.BytesRestored
			s.TotalFilesProcessed = m.TotalFiles
			s.TotalBytesProcessed = m.TotalBytes
		} else {
			s.SnapshotID = m.SnapshotID
			s.FilesNew = m.FilesNew
			s.FilesChanged = m.FilesChanged
			s.FilesUnmodified = m.FilesUnmodified
			s.DirsNew = m.DirsNew
			s.DirsChanged = m.DirsChanged
			s.DirsUnmodified = m.DirsUnmodified
			s.DataAdded = m.DataAdded
			s.TotalFilesProcessed = m.TotalFilesProcessed
			s.TotalBytesProcessed = m.TotalBytesProcessed
		}
		sink.Summary(s)

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
		sink.Log("error", "stdout", msg)

	case "exit_error":
		// A fatal error restic reports as JSON (e.g. repository not initialized).
		msg := strings.TrimSpace(m.Message)
		if msg == "" {
			msg = "fatal error"
		}
		sink.Log("error", "stdout", firstLine(msg))
	}
}

// exitCode extracts the process exit code from cmd.Wait's error: 0 for success,
// the real code for a normal non-zero exit, or -1 if the process was killed by a
// signal or the error is not an exit error.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
