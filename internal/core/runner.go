package core

import (
	"bufio"
	"bytes"
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
	"sync/atomic"
	"time"
	"unicode/utf8"
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
	// Restore streams a restore of snapshotID into target, replacing the
	// destination: snapshot files overwrite, and files not in the snapshot are
	// deleted. include is passed as restic --include patterns; it is required
	// when target is "/" (restic refuses --target / --delete without a filter).
	Restore(ctx context.Context, repo *Repository, snapshotID, target string, include []string, sink RunSink) (int, error)
	// Forget applies a keep-policy to snapshots with the given tag, then prunes
	// unreferenced data (`restic forget --prune`). Returns restic's exit code.
	Forget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error)
	// PreviewForget reports what Forget would keep and remove, changing
	// nothing (`restic forget --dry-run --no-lock`): it takes no lock, so it
	// also works while another process holds the repository.
	PreviewForget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error)
	// ForgetSnapshots removes the given snapshot IDs, then prunes unreferenced
	// data. An empty list is a no-op success (nothing to forget).
	ForgetSnapshots(ctx context.Context, repo *Repository, snapshotIDs []string, sink RunSink) (int, error)
	// DropSnapshots forgets the given snapshot IDs without pruning: the data
	// only they used stays in the repository until the next prune reclaims it.
	// An empty list is a no-op success.
	DropSnapshots(ctx context.Context, repo *Repository, snapshotIDs []string, sink RunSink) (int, error)
	// Check verifies repository integrity (`restic check`). readData is "" for a
	// structure-only check, "all" for --read-data, or a --read-data-subset value
	// such as "10%". Exit code 1 means restic found errors (or could not run).
	Check(ctx context.Context, repo *Repository, readData string, sink RunSink) (int, error)
	// BackupStdin backs up what produce writes as one file named filename
	// (`restic backup --stdin`). If produce fails, restic is stopped before it
	// sees the end of its input, so no snapshot is saved, and err reports why.
	// A failure of restic itself is reported by the exit code, as in Backup.
	BackupStdin(ctx context.Context, repo *Repository, filename string, tags []string, produce func(w io.Writer) error, sink RunSink) (int, error)
	// Dump streams one file of a snapshot to consume (`restic dump`). A
	// non-zero exit code means restic did not deliver the whole file; err
	// reports a failure of consume or of starting restic.
	Dump(ctx context.Context, repo *Repository, snapshotID, file string, consume func(r io.Reader) error, sink RunSink) (int, error)
	// List returns every node of a snapshot (`restic ls`).
	List(ctx context.Context, repo *Repository, snapshotID string) ([]LsNode, error)
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

type runSinkKey struct{}

// withRunSink marks ctx as belonging to a run, so non-streamed restic calls made
// with it (listing snapshots before a restore or forget) record their process id
// on the run, as streamed ones do. Stop from another process signals that id;
// without it, such a step could not be stopped from the UI or another CLI.
func withRunSink(ctx context.Context, sink RunSink) context.Context {
	return context.WithValue(ctx, runSinkKey{}, sink)
}

// errInterrupted means restic exited after a SIGINT that did not come from
// ctx, i.e. a stop requested by another process. The run counts as canceled.
var errInterrupted = errors.New("restic was interrupted")

// outputSeparately runs cmd and returns its stdout and stderr apart. restic
// writes --json results to stdout and backend retry warnings to stderr; mixing
// the two would corrupt the JSON whenever the backend needed a retry.
func outputSeparately(ctx context.Context, cmd *exec.Cmd) (stdout, stderr []byte, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	if sink, ok := ctx.Value(runSinkKey{}).(RunSink); ok && cmd.Process != nil {
		sink.PID(cmd.Process.Pid, procStartToken(cmd.Process.Pid))
	}
	err = cmd.Wait()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// outputError explains a failed non-streamed restic call: stopped by ctx,
// interrupted by another process, not started at all, or a restic failure.
func outputError(ctx context.Context, what string, stdout, stderr []byte, err error) error {
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return newStoppedError(ctx, what, stderr)
	case exitCode(err) == resticExitInterrupted:
		return errInterrupted
	case !errors.As(err, &exitErr):
		return fmt.Errorf("could not run %s: %w", what, err)
	default:
		return classifyRepoError(string(stderr) + "\n" + string(stdout))
	}
}

// stoppedError is a restic call that ended because its context did (a timeout
// or a cancel). It unwraps to ctx.Err(), so callers can tell a timeout apart.
type stoppedError struct {
	msg   string
	cause error
}

func (e *stoppedError) Error() string { return e.msg }
func (e *stoppedError) Unwrap() error { return e.cause }

// newStoppedError describes what restic was doing when ctx ended. Once
// interrupted, restic prints "signal interrupt received" and, if it was still
// opening the repository, "unable to open config file: context canceled"; the
// latter would be misread as "not initialized", so all of that is dropped. The
// last line before it (often a backend retry warning) is kept: it usually says
// what restic was waiting on.
func newStoppedError(ctx context.Context, what string, output []byte) error {
	msg := what + " was canceled"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		msg = what + " timed out"
	}
	if last := lastResticNote(output); last != "" {
		msg += " (last restic output: " + last + ")"
	}
	return &stoppedError{msg: msg, cause: ctx.Err()}
}

// beforeInterrupt returns restic's output up to its reaction to being
// interrupted ("signal interrupt received" and the shutdown errors after it).
func beforeInterrupt(output []byte) string {
	text := string(output)
	if i := strings.Index(text, "signal interrupt received"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// lastResticNote returns the last line restic printed before reacting to being
// interrupted.
func lastResticNote(output []byte) string {
	lines := strings.Split(beforeInterrupt(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.Contains(l, "context canceled") {
			return l
		}
	}
	return ""
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
	if ctx.Err() != nil {
		return TestResult{Message: "Gave up before the repository answered: " + newStoppedError(ctx, "restic cat config", out).Error() + ".", Detail: beforeInterrupt(out)}
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

func (r *resticRunner) Snapshots(ctx context.Context, repo *Repository, tag string) ([]Snapshot, error) {
	args := []string{"snapshots", "--json"}
	if strings.TrimSpace(tag) != "" {
		args = append(args, "--tag", tag)
	}
	cmd, err := resticCommand(ctx, repo, args...)
	if err != nil {
		return nil, err
	}
	out, stderr, err := outputSeparately(ctx, cmd)
	if err != nil {
		return nil, outputError(ctx, "restic snapshots", out, stderr, err)
	}
	snaps, missing, err := decodeSnapshots(out)
	if err != nil {
		return nil, err
	}
	// Older restic versions (and pre-0.17 backups) omit the snapshot summary.
	// Fill Size/Files via `restic stats` so the UI does not show "—".
	if err := r.fillMissingSnapshotSizes(ctx, repo, snaps, missing); err != nil {
		return nil, err
	}
	return snaps, nil
}

// fillMissingSnapshotSizes runs `restic stats --mode restore-size` for snapshots
// that have no stored summary. Failures are ignored per-snapshot so a slow or
// unreachable stats call does not break the list; only an interruption (a stop
// from another process) ends it, returning errInterrupted.
func (r *resticRunner) fillMissingSnapshotSizes(ctx context.Context, repo *Repository, snaps []Snapshot, missing []int) error {
	if len(missing) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var interrupted atomic.Bool
	const workers = 4
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, idx := range missing {
		idx := idx
		if idx < 0 || idx >= len(snaps) || snaps[idx].ID == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			size, files, err := r.snapshotStats(ctx, repo, snaps[idx].ID)
			if errors.Is(err, errInterrupted) {
				interrupted.Store(true)
				cancel()
			}
			if err != nil {
				return
			}
			mu.Lock()
			snaps[idx].SizeBytes = int64Ptr(size)
			snaps[idx].FileCount = int64Ptr(files)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if interrupted.Load() {
		return errInterrupted
	}
	return nil
}

func (*resticRunner) snapshotStats(ctx context.Context, repo *Repository, snapshotID string) (size, files int64, err error) {
	cmd, err := resticCommand(ctx, repo, "stats", "--mode", "restore-size", "--json", "--", snapshotID)
	if err != nil {
		return 0, 0, err
	}
	out, stderr, err := outputSeparately(ctx, cmd)
	if err != nil {
		return 0, 0, outputError(ctx, "restic stats", out, stderr, err)
	}
	// restic may print progress lines before the JSON object; take the last '{…}'.
	raw := strings.TrimSpace(string(out))
	if i := strings.LastIndex(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	var st resticStats
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return 0, 0, fmt.Errorf("could not parse snapshot stats: %w", err)
	}
	return st.TotalSize, st.TotalFileCount, nil
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
		if ctx.Err() != nil {
			return newStoppedError(ctx, "restic unlock", out)
		}
		return fmt.Errorf("restic unlock failed: %s", firstLine(strings.TrimSpace(string(out))))
	}
	return nil
}

func (*resticRunner) Backup(ctx context.Context, repo *Repository, source string, tags []string, sink RunSink) (int, error) {
	return streamRestic(ctx, repo, KindBackup, sink, backupArgs(source, tags)...)
}

// backupArgs builds `restic backup` arguments. Like every restic call that
// takes a user value as a positional argument, the value goes after "--", so
// one starting with "-" can never be read as a restic option.
func backupArgs(source string, tags []string) []string {
	args := []string{"backup", "--json"}
	for _, tg := range tags {
		if strings.TrimSpace(tg) != "" {
			args = append(args, "--tag", tg)
		}
	}
	return append(args, "--", source)
}

func (*resticRunner) Restore(ctx context.Context, repo *Repository, snapshotID, target string, include []string, sink RunSink) (int, error) {
	args, err := restoreArgs(snapshotID, target, include)
	if err != nil {
		return 0, err
	}
	return streamRestic(ctx, repo, KindRestore, sink, args...)
}

func (*resticRunner) Forget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error) {
	args, err := forgetArgs(tag, policy)
	if err != nil {
		return -1, err
	}
	return streamRestic(ctx, repo, KindRetention, sink, args...)
}

func (*resticRunner) PreviewForget(ctx context.Context, repo *Repository, tag string, policy JobRetention, sink RunSink) (int, error) {
	args, err := previewForgetArgs(tag, policy)
	if err != nil {
		return -1, err
	}
	return streamRestic(ctx, repo, KindRetention, sink, args...)
}

// previewForgetArgs is forgetArgs as a dry run: nothing is forgotten or
// pruned, and no lock is taken.
func previewForgetArgs(tag string, policy JobRetention) ([]string, error) {
	args, err := forgetArgs(tag, policy)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a == "--prune" {
			out = append(out, "--dry-run", "--no-lock")
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// forgetSnapshotBatchSize caps how many snapshot ids are passed to one
// `restic forget` so we stay under ARG_MAX on large repositories.
const forgetSnapshotBatchSize = 100

type forgetBatch struct {
	IDs   []string
	Prune bool
}

func uniqueNonEmpty(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func planForgetBatches(ids []string, batchSize int) []forgetBatch {
	cleaned := uniqueNonEmpty(ids)
	if len(cleaned) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = forgetSnapshotBatchSize
	}
	out := make([]forgetBatch, 0, (len(cleaned)+batchSize-1)/batchSize)
	for i := 0; i < len(cleaned); i += batchSize {
		end := i + batchSize
		if end > len(cleaned) {
			end = len(cleaned)
		}
		chunk := append([]string(nil), cleaned[i:end]...)
		out = append(out, forgetBatch{IDs: chunk, Prune: end == len(cleaned)})
	}
	return out
}

func forgetSnapshotArgs(ids []string, prune bool) []string {
	args := []string{"forget", "--json"}
	if prune {
		args = append(args, "--prune")
	}
	args = append(args, "--")
	return append(args, ids...)
}

func (*resticRunner) ForgetSnapshots(ctx context.Context, repo *Repository, snapshotIDs []string, sink RunSink) (int, error) {
	batches := planForgetBatches(snapshotIDs, forgetSnapshotBatchSize)
	if len(batches) == 0 {
		sink.Log("info", "system", "No snapshots to forget.")
		return 0, nil
	}
	var last int
	for _, b := range batches {
		if err := ctx.Err(); err != nil {
			return resticExitInterrupted, nil
		}
		msg := fmt.Sprintf("Forgetting %d snapshot(s)", len(b.IDs))
		if b.Prune {
			msg += " and pruning unused data"
		}
		sink.Log("info", "system", msg+"…")
		code, err := streamRestic(ctx, repo, KindForget, sink, forgetSnapshotArgs(b.IDs, b.Prune)...)
		last = code
		if err != nil {
			return code, err
		}
		if code != 0 && code != resticExitWarnings {
			return code, nil
		}
	}
	return last, nil
}

func (*resticRunner) DropSnapshots(ctx context.Context, repo *Repository, snapshotIDs []string, sink RunSink) (int, error) {
	var last int
	for _, b := range planForgetBatches(snapshotIDs, forgetSnapshotBatchSize) {
		if err := ctx.Err(); err != nil {
			return resticExitInterrupted, nil
		}
		code, err := streamRestic(ctx, repo, KindForget, sink, forgetSnapshotArgs(b.IDs, false)...)
		last = code
		if err != nil || (code != 0 && code != resticExitWarnings) {
			return code, err
		}
	}
	return last, nil
}

func (*resticRunner) Check(ctx context.Context, repo *Repository, readData string, sink RunSink) (int, error) {
	args, err := checkArgs(readData)
	if err != nil {
		return -1, err
	}
	// Plain (non --json) output: restic's human lines read well in the run log
	// and the format is the same across restic versions.
	return streamRestic(ctx, repo, KindCheck, sink, args...)
}

// checkArgs builds `restic check` arguments. readData is "" (structure only),
// "all" (--read-data), or a --read-data-subset value ("n/t", "x%", or a size).
func checkArgs(readData string) ([]string, error) {
	readData = strings.TrimSpace(readData)
	switch {
	case readData == "":
		return []string{"check"}, nil
	case readData == "all":
		return []string{"check", "--read-data"}, nil
	case strings.HasPrefix(readData, "-") || strings.ContainsAny(readData, " \t\n"):
		return nil, fmt.Errorf("invalid read-data subset %q", readData)
	default:
		return []string{"check", "--read-data-subset", readData}, nil
	}
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
//
// Older restic builds (e.g. Debian bookworm's 0.14) return exit 1 with a
// human-readable stderr message instead of the modern 10/11/12 codes. We
// accumulate streamed text and remap those failures so auto-init and
// classifyRun keep working.
func streamRestic(ctx context.Context, repo *Repository, kind RunKind, sink RunSink, args ...string) (int, error) {
	cmd, err := resticCommand(ctx, repo, args...)
	if err != nil {
		return -1, err
	}
	return streamResticCmd(cmd, kind, sink, nil)
}

// streamResticCmd is streamRestic for a prepared command. started, if set, is
// called right after restic starts (e.g. to begin feeding its stdin).
func streamResticCmd(cmd *exec.Cmd, kind RunKind, sink RunSink, started func()) (int, error) {
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
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		sink.PID(pid, procStartToken(pid))
	}
	if started != nil {
		started()
	}

	quiet := startQuietNotes(func(silent time.Duration) {
		sink.Log("info", "system", fmt.Sprintf(
			"No output from restic for %s; it is still running (pid %d), most likely waiting on the storage backend.",
			silent.Round(time.Second), pid))
	})

	var (
		wg      sync.WaitGroup
		diagMu  sync.Mutex
		diagBuf strings.Builder
	)
	capture := func(line string) {
		diagMu.Lock()
		if diagBuf.Len() < 8*1024 {
			diagBuf.WriteString(line)
			diagBuf.WriteByte('\n')
		}
		diagMu.Unlock()
	}

	// stderr: restic prints human-readable warnings and fatal errors here.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			quiet.touch()
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			capture(line)
			// restic may emit structured JSON (e.g. an exit_error) on stderr; route
			// it through the mapper so it reads cleanly instead of as raw JSON.
			if strings.HasPrefix(line, "{") {
				var m resticMessage
				if err := json.Unmarshal([]byte(line), &m); err == nil && m.MessageType != "" {
					mapResticMessage(kind, &m, sink)
					continue
				}
			}
			sink.Log("warn", "stderr", clipLogLine(line))
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
			quiet.touch()
			line := sc.Bytes()
			var m resticMessage
			if err := json.Unmarshal(line, &m); err != nil {
				if kind == KindRetention || kind == KindForget {
					if notes, ok := forgetSummary(line); ok {
						for _, n := range notes {
							sink.Log("info", "stdout", n)
						}
						continue
					}
				}
				if t := clipLogLine(strings.TrimSpace(string(line))); t != "" {
					capture(t)
					sink.Log("info", "stdout", t)
				}
				continue
			}
			if m.MessageType == "exit_error" || m.MessageType == "error" {
				capture(string(line))
			}
			mapResticMessage(kind, &m, sink)
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	wg.Wait()
	quiet.stop()
	code := exitCode(cmd.Wait())
	if code != 0 && code != resticExitWarnings && code != resticExitNotInitialized &&
		code != resticExitLocked && code != resticExitBadPassword && code != resticExitInterrupted {
		diagMu.Lock()
		text := diagBuf.String()
		diagMu.Unlock()
		if mapped := classifyResticFailure(text); mapped != 0 {
			code = mapped
		}
	}
	return code, nil
}

// restic --json prints nothing while it opens the repository, takes its lock and
// loads the index, so on a slow backend a healthy run can look dead in the log
// for many minutes (and a stuck one looks the same). Runs note each long silence
// after quietNoticeAfter, then each time the silence doubles.
var (
	quietNoticeAfter = time.Minute
	quietPoll        = 5 * time.Second
)

// quietNotes calls note when restic has been silent for quietNoticeAfter, then
// again at 2x, 4x, … that long; touch (new output) starts the count over.
type quietNotes struct {
	last          atomic.Int64 // unix nanos of the latest output
	done, stopped chan struct{}
}

func startQuietNotes(note func(silent time.Duration)) *quietNotes {
	q := &quietNotes{done: make(chan struct{}), stopped: make(chan struct{})}
	q.touch()
	go q.watch(note)
	return q
}

func (q *quietNotes) touch() { q.last.Store(time.Now().UnixNano()) }

// stop ends the notes and waits, so none is logged after the caller moves on.
func (q *quietNotes) stop() {
	close(q.done)
	<-q.stopped
}

func (q *quietNotes) watch(note func(silent time.Duration)) {
	defer close(q.stopped)
	t := time.NewTicker(quietPoll)
	defer t.Stop()
	var since int64
	next := quietNoticeAfter
	for {
		select {
		case <-q.done:
			return
		case <-t.C:
		}
		last := q.last.Load()
		if last != since {
			since, next = last, quietNoticeAfter
		}
		if silent := time.Since(time.Unix(0, last)); silent >= next {
			note(silent)
			next *= 2
		}
	}
}

// maxLogLine caps one run-log line taken verbatim from restic, so an unexpected
// blob of output cannot bloat the log (and Postgres) or bury the useful lines.
const maxLogLine = 4 * 1024

func clipLogLine(s string) string {
	if len(s) <= maxLogLine {
		return s
	}
	cut := maxLogLine
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf(" … (truncated, %d bytes in all)", len(s))
}

// forgetGroup is one group of `restic forget --json` output: the snapshots of
// one host/paths/tags combination that the policy keeps and removes.
type forgetGroup struct {
	Host   string             `json:"host"`
	Paths  []string           `json:"paths"`
	Keep   []forgetGroupEntry `json:"keep"`
	Remove []forgetGroupEntry `json:"remove"`
}

type forgetGroupEntry struct {
	ID string `json:"id"`
}

// forgetMaxIDs caps how many removed snapshot ids one summary line lists.
const forgetMaxIDs = 10

// forgetSummary turns `restic forget --json` output (one JSON array of groups,
// each snapshot with its full backup summary, easily 100KB) into one short line
// per group. ok is false if line is not that output.
func forgetSummary(line []byte) (notes []string, ok bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '[' {
		return nil, false
	}
	var groups []forgetGroup
	if err := json.Unmarshal(line, &groups); err != nil {
		return nil, false
	}
	if len(groups) == 0 {
		return []string{"No snapshots matched; nothing to forget."}, true
	}
	for _, g := range groups {
		note := fmt.Sprintf("Keeping %d snapshot(s), removing ", len(g.Keep))
		if len(g.Remove) == 0 {
			note += "none."
		} else {
			ids := make([]string, 0, forgetMaxIDs)
			for i, r := range g.Remove {
				if i == forgetMaxIDs {
					break
				}
				ids = append(ids, shortID(r.ID))
			}
			note += fmt.Sprintf("%d: %s", len(g.Remove), strings.Join(ids, ", "))
			if more := len(g.Remove) - len(ids); more > 0 {
				note += fmt.Sprintf(" and %d more", more)
			}
			note += "."
		}
		if len(groups) > 1 {
			note = fmt.Sprintf("%s (%s): %s", g.Host, strings.Join(g.Paths, ", "), note)
		}
		notes = append(notes, note)
	}
	return notes, true
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
