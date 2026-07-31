package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// progressFlushInterval bounds how often a running operation's throttled progress
// is written to run.json. Live progress streams every tick; the durable snapshot
// only needs to be recent enough that a page loaded mid-run shows a sensible bar.
const progressFlushInterval = time.Second

// RunStore owns the on-disk runs tree:
//
//	<dir>/<runId>/run.json    the full, authoritative run record (atomic writes)
//	<dir>/<runId>/log.jsonl   the append-only, permanent log (one LogLine per line)
//
// The in-memory index mirrors run.json so "is anything running" and history
// listings are cheap reads. The durable record is the sole source of truth for a
// run's state; a runHandle streams a live operation's events into it.
type RunStore struct {
	dir string
	bus eventBus

	mu    sync.RWMutex
	index map[string]*Run
}

// newRunStore opens (creating if needed) the runs directory and loads the index.
func newRunStore(dir string, bus eventBus) (*RunStore, error) {
	if bus == nil {
		bus = noopBus{}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &RunStore{dir: dir, bus: bus, index: map[string]*Run{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *RunStore) runDir(id string) string  { return filepath.Join(s.dir, id) }
func (s *RunStore) runPath(id string) string { return filepath.Join(s.dir, id, "run.json") }
func (s *RunStore) logPath(id string) string { return filepath.Join(s.dir, id, "log.jsonl") }

// load reads every run.json under dir into the index. Unreadable entries are
// skipped so one corrupt run never blocks startup.
func (s *RunStore) load() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(s.runPath(e.Name()))
		if err != nil {
			continue
		}
		var run Run
		if err := json.Unmarshal(data, &run); err != nil {
			continue
		}
		if run.ID == "" {
			run.ID = e.Name()
		}
		s.index[run.ID] = &run
	}
	return nil
}

// newRunID returns a time-sortable run id, so a lexical directory listing is
// already in chronological order.
func newRunID(now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(b[:])
}

// Get returns a deep copy of the run with the given id.
func (s *RunStore) Get(id string) (*Run, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.index[id]
	if !ok {
		return nil, false
	}
	return run.clone(), true
}

// list returns clones of all runs matching keep, newest first.
func (s *RunStore) list(keep func(*Run) bool) []*Run {
	s.mu.RLock()
	out := make([]*Run, 0, len(s.index))
	for _, run := range s.index {
		if keep == nil || keep(run) {
			out = append(out, run.clone())
		}
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// runsForJob returns a job's run history, newest first.
func (s *RunStore) runsForJob(jobID string) []*Run {
	return s.list(func(r *Run) bool { return r.JobID == jobID })
}

// activeRuns returns runs currently marked as running/starting.
func (s *RunStore) activeRuns() []*Run {
	return s.list(func(r *Run) bool { return r.Status.Active() })
}

// ReadLog returns a run's log lines with Seq greater than afterSeq. A torn final
// line (from a crash mid-append) fails to parse and is skipped.
func (s *RunStore) ReadLog(id string, afterSeq int64) ([]LogLine, error) {
	if _, ok := s.Get(id); !ok {
		return nil, notFoundf("run not found")
	}
	f, err := os.Open(s.logPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return []LogLine{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := []LogLine{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ll LogLine
		if err := json.Unmarshal(sc.Bytes(), &ll); err != nil {
			continue // torn/partial line
		}
		if ll.Seq > afterSeq {
			out = append(out, ll)
		}
	}
	return out, nil
}

// writeRunLocked persists a run record atomically. Caller holds s.mu.
func (s *RunStore) writeRunLocked(run *Run) error {
	return writeJSONFileAtomic(s.runPath(run.ID), run)
}

// AppendSystemLine appends a system log line to a run's log, continuing the
// per-run seq. Used by reconcile, where no live handle exists to own the seq.
func (s *RunStore) AppendSystemLine(id, level, message string) {
	lines, _ := s.ReadLog(id, 0)
	next := int64(1)
	if n := len(lines); n > 0 {
		next = lines[n-1].Seq + 1
	}
	line := LogLine{Seq: next, TS: time.Now().UTC(), Stream: "system", Level: level, Message: message}
	data, err := json.Marshal(line)
	if err != nil {
		return
	}
	f, err := os.OpenFile(s.logPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
	_ = f.Sync()
	_ = f.Close()
	s.bus.publishLog(id, line)
}

// reconcile makes the durable records honest on startup: every run still marked
// active is marked interrupted (after reaping any orphaned restic child a crash
// left behind). It returns the repository ids that had interrupted runs, for a
// best-effort stale-lock cleanup. It runs before the server accepts traffic, so
// no concurrent run activity races it.
func (s *RunStore) reconcile(reap func(pid int) bool) []string {
	type item struct {
		id, repoID string
		pid        int
	}
	var items []item
	s.mu.RLock()
	for _, run := range s.index {
		if run.Status.Active() {
			items = append(items, item{run.ID, run.RepositoryID, run.PID})
		}
	}
	s.mu.RUnlock()

	repoSet := map[string]bool{}
	for _, it := range items {
		if reap != nil && reap(it.pid) {
			s.AppendSystemLine(it.id, "warn", "Reaped an orphaned restic process left running by a previous app instance.")
		}
		s.AppendSystemLine(it.id, "error", "The application was restarted while this run was in progress; marking it interrupted.")
		now := time.Now().UTC()
		s.mutate(it.id, true, func(r *Run) {
			r.Status = StatusInterrupted
			r.FinishedAt = &now
			if r.Error == "" {
				r.Error = "interrupted: the application restarted while this run was in progress"
			}
		})
		if it.repoID != "" {
			repoSet[it.repoID] = true
		}
	}

	repos := make([]string, 0, len(repoSet))
	for id := range repoSet {
		repos = append(repos, id)
	}
	return repos
}

// Begin creates a new run: it assigns a time-sortable id, writes run.json,
// opens the append-only log, indexes the run and returns a live handle the
// coordinator uses to stream events into the durable record.
func (s *RunStore) Begin(run *Run) (*runHandle, error) {
	now := time.Now().UTC()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	run.ID = newRunID(run.StartedAt)
	if run.Status == "" {
		run.Status = StatusStarting
	}

	if err := os.MkdirAll(s.runDir(run.ID), 0o700); err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(s.logPath(run.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	stored := run.clone()
	s.index[run.ID] = stored
	err = s.writeRunLocked(stored)
	s.mu.Unlock()
	if err != nil {
		logf.Close()
		return nil, err
	}

	s.bus.publishRun(stored.clone())
	return &runHandle{store: s, id: run.ID, logf: logf}, nil
}

// mutate applies fn to the stored run under the lock, persists it, and returns a
// clone. It is the single write path for run-record changes.
func (s *RunStore) mutate(id string, persist bool, fn func(*Run)) *Run {
	s.mu.Lock()
	run, ok := s.index[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	fn(run)
	if persist {
		_ = s.writeRunLocked(run)
	}
	clone := run.clone()
	s.mu.Unlock()
	return clone
}

// runHandle is the live side of one running operation. It implements RunSink:
// the coordinator hands it to the Runner, which streams events through it into
// the durable record and the event bus. It owns the log file and per-run seq
// counter, which a single serialized appender guards so seq stays monotonic even
// though restic's stdout and stderr are scanned concurrently.
type runHandle struct {
	store *RunStore
	id    string

	logMu sync.Mutex
	logf  *os.File
	seq   int64

	progMu    sync.Mutex
	lastFlush time.Time
}

// Log appends a durable, seq-numbered log line and publishes it live.
func (h *runHandle) Log(level, stream, message string) {
	h.logMu.Lock()
	h.seq++
	line := LogLine{Seq: h.seq, TS: time.Now().UTC(), Stream: stream, Level: level, Message: message}
	if h.logf != nil {
		if data, err := json.Marshal(line); err == nil {
			data = append(data, '\n')
			_, _ = h.logf.Write(data)
		}
	}
	h.logMu.Unlock()

	h.store.bus.publishLog(h.id, line)
}

// Progress updates the run's last-value-wins progress. It is streamed live every
// tick but only flushed to run.json on a throttle, so a page loaded mid-run sees
// a recent bar without hammering the disk.
func (h *runHandle) Progress(p Progress) {
	h.progMu.Lock()
	now := time.Now()
	flush := now.Sub(h.lastFlush) >= progressFlushInterval
	if flush {
		h.lastFlush = now
	}
	h.progMu.Unlock()

	h.store.mutate(h.id, flush, func(r *Run) { r.Progress = p })
	h.store.bus.publishProgress(h.id, p)
}

// Summary records the final result of a backup/restore (persisted immediately,
// since it is rare and meaningful).
func (h *runHandle) Summary(s Summary) {
	cp := s
	h.store.mutate(h.id, true, func(r *Run) { r.Summary = &cp })
}

// PID records the restic child's process id, persisted so a crash's orphan can
// be reaped on the next startup.
func (h *runHandle) PID(pid int) {
	h.store.mutate(h.id, true, func(r *Run) { r.PID = pid })
}

// setStatus transitions the run to a non-terminal status and publishes it.
func (h *runHandle) setStatus(status RunStatus) {
	run := h.store.mutate(h.id, true, func(r *Run) { r.Status = status })
	if run != nil {
		h.store.bus.publishRun(run)
	}
}

// finalize writes the terminal state as the single authoritative last write
// (status-first: run.json is atomic, so a run is only ever "running" or a final
// state), closes the log, and publishes the finished run.
func (h *runHandle) finalize(status RunStatus, exitCode int, errMsg string) *Run {
	now := time.Now().UTC()
	run := h.store.mutate(h.id, true, func(r *Run) {
		r.Status = status
		r.FinishedAt = &now
		code := exitCode
		r.ExitCode = &code
		r.Error = errMsg
		r.Progress.CurrentFile = ""
	})

	h.logMu.Lock()
	if h.logf != nil {
		_ = h.logf.Sync()
		_ = h.logf.Close()
		h.logf = nil
	}
	h.logMu.Unlock()

	if run != nil {
		h.store.bus.publishRun(run)
	}
	return run
}
