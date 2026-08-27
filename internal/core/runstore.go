package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// progressFlushInterval bounds how often a running operation's throttled progress
// is written to Postgres. Live progress streams every tick; the durable snapshot
// only needs to be recent enough that a page loaded mid-run shows a sensible bar.
const progressFlushInterval = time.Second

const runCols = `id, kind, status, job_id, repository_id, job_name, folder_path, repo_name,
	params, started_at, finished_at, pid, pid_start, progress, summary, exit_code, error`

// RunStore owns durable run records and append-only logs in Postgres.
//
// live holds in-process copies of runs that currently have a handle (or were
// recently mutated) so Get/list see unflushed progress. The table is the source
// of truth across restarts.
type RunStore struct {
	Pool *pgxpool.Pool
	Bus  eventBus

	mu   sync.RWMutex
	live map[string]*Run
}

func newRunStore(pool *pgxpool.Pool, bus eventBus) *RunStore {
	if bus == nil {
		bus = noopBus{}
	}
	return &RunStore{Pool: pool, Bus: bus, live: map[string]*Run{}}
}

// newRunID returns a time-sortable run id, so a lexical listing is already in
// chronological order.
func newRunID(now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(b[:])
}

func (s *RunStore) Get(id string) (*Run, bool) {
	s.mu.RLock()
	if run, ok := s.live[id]; ok {
		cp := run.clone()
		s.mu.RUnlock()
		return cp, true
	}
	s.mu.RUnlock()
	return s.getDB(id)
}

func (s *RunStore) getDB(id string) (*Run, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	run, err := scanRun(s.Pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs WHERE id = $1`, id))
	if err != nil {
		return nil, false
	}
	return run, true
}

// list returns clones of all runs matching keep, newest first.
func (s *RunStore) list(keep func(*Run) bool) []*Run {
	runs, _ := s.Query("", "", "", 0, 0)
	if keep == nil {
		return runs
	}
	out := make([]*Run, 0, len(runs))
	for _, run := range runs {
		if keep(run) {
			out = append(out, run)
		}
	}
	return out
}

// Query returns runs matching optional status/kind/jobId filters, newest first.
// total is the match count before limit/offset (0 limit means no cap; offset is
// ignored when limit is 0).
func (s *RunStore) Query(status, kind, jobID string, limit, offset int) ([]*Run, int) {
	where, args := runWhere(status, kind, jobID)
	ctx, cancel := dbCtx()
	defer cancel()

	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM runs`+where, args...).Scan(&total); err != nil {
		return nil, 0
	}

	q := `SELECT ` + runCols + ` FROM runs` + where + ` ORDER BY started_at DESC, id DESC`
	if limit > 0 {
		args = append(args, limit)
		q += ` LIMIT $` + strconv.Itoa(len(args))
		if offset > 0 {
			args = append(args, offset)
			q += ` OFFSET $` + strconv.Itoa(len(args))
		}
	}
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, total
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return overlayRuns(s, out), total
		}
		out = append(out, run)
	}
	return overlayRuns(s, out), total
}

func runWhere(status, kind, jobID string) (string, []any) {
	var clauses []string
	var args []any
	add := func(sql string, v any) {
		args = append(args, v)
		clauses = append(clauses, sql+"$"+strconv.Itoa(len(args)))
	}
	switch status {
	case "", "all":
	case "active":
		clauses = append(clauses, `status IN ('starting','running')`)
	case "finished":
		clauses = append(clauses, `status IN ('success','success_warnings','failed','canceled','interrupted')`)
	default:
		add(`status = `, status)
	}
	if kind != "" {
		add(`kind = `, kind)
	}
	if jobID != "" {
		add(`job_id = `, jobID)
	}
	if len(clauses) == 0 {
		return "", args
	}
	w := " WHERE " + clauses[0]
	for i := 1; i < len(clauses); i++ {
		w += " AND " + clauses[i]
	}
	return w, args
}

func overlayRuns(s *RunStore, runs []*Run) []*Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.live) == 0 {
		return runs
	}
	out := make([]*Run, len(runs))
	for i, r := range runs {
		if live, ok := s.live[r.ID]; ok {
			out[i] = live.clone()
		} else {
			out[i] = r
		}
	}
	return out
}

// newerRun is the single ordering rule for run history: most recently started
// first, with the id breaking ties (run ids are time-sortable).
func newerRun(a, b *Run) bool {
	if a.StartedAt.Equal(b.StartedAt) {
		return a.ID > b.ID
	}
	return a.StartedAt.After(b.StartedAt)
}

// runStats is a job's history at a glance: its most recent run and how many runs
// it has in total.
type runStats struct {
	Last  *Run
	Count int
}

func (s *RunStore) statsByJob() map[string]runStats {
	ctx, cancel := dbCtx()
	defer cancel()
	out := map[string]runStats{}

	rows, err := s.Pool.Query(ctx, `SELECT job_id, COUNT(*) FROM runs WHERE job_id IS NOT NULL AND job_id <> '' GROUP BY job_id`)
	if err != nil {
		return out
	}
	for rows.Next() {
		var jobID string
		var n int
		if err := rows.Scan(&jobID, &n); err != nil {
			rows.Close()
			return out
		}
		out[jobID] = runStats{Count: n}
	}
	rows.Close()

	rows, err = s.Pool.Query(ctx, `SELECT DISTINCT ON (job_id) `+runCols+`
		FROM runs WHERE job_id IS NOT NULL AND job_id <> ''
		ORDER BY job_id, started_at DESC, id DESC`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return out
		}
		st := out[run.JobID]
		st.Last = overlayOne(s, run)
		out[run.JobID] = st
	}
	return out
}

func (s *RunStore) statsForJob(jobID string) runStats {
	if jobID == "" {
		return runStats{}
	}
	ctx, cancel := dbCtx()
	defer cancel()
	var n int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM runs WHERE job_id = $1`, jobID).Scan(&n)
	run, err := scanRun(s.Pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs WHERE job_id = $1
		ORDER BY started_at DESC, id DESC LIMIT 1`, jobID))
	st := runStats{Count: n}
	if err == nil {
		st.Last = overlayOne(s, run)
	}
	return st
}

func overlayOne(s *RunStore, run *Run) *Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if live, ok := s.live[run.ID]; ok {
		return live.clone()
	}
	return run
}

func (s *RunStore) backupTiming(jobID string) (lastAttempt *time.Time, lastOK bool, lastSuccess *time.Time) {
	if jobID == "" {
		return nil, false, nil
	}
	ctx, cancel := dbCtx()
	defer cancel()

	pick := func(onlyOK bool) *Run {
		q := `SELECT ` + runCols + ` FROM runs
			WHERE job_id = $1 AND kind = $2 AND status IN ('success','success_warnings','failed','canceled','interrupted')`
		args := []any{jobID, string(KindBackup)}
		if onlyOK {
			q = `SELECT ` + runCols + ` FROM runs
				WHERE job_id = $1 AND kind = $2 AND status IN ('success','success_warnings')`
		}
		q += ` ORDER BY COALESCE(finished_at, started_at) DESC, id DESC LIMIT 1`
		run, err := scanRun(s.Pool.QueryRow(ctx, q, args...))
		if err != nil {
			return nil
		}
		return run
	}

	if best := pick(false); best != nil {
		t := best.FinishedAt
		if t == nil {
			t = &best.StartedAt
		}
		cp := t.UTC()
		lastAttempt = &cp
		lastOK = best.Status == StatusSuccess || best.Status == StatusSuccessWarnings
	}
	if best := pick(true); best != nil {
		t := best.FinishedAt
		if t == nil {
			t = &best.StartedAt
		}
		cp := t.UTC()
		lastSuccess = &cp
	}
	return lastAttempt, lastOK, lastSuccess
}

func newerFinished(a, b *Run) bool {
	at := a.StartedAt
	if a.FinishedAt != nil {
		at = *a.FinishedAt
	}
	bt := b.StartedAt
	if b.FinishedAt != nil {
		bt = *b.FinishedAt
	}
	if at.Equal(bt) {
		return a.ID > b.ID
	}
	return at.After(bt)
}

func (s *RunStore) runsForJob(jobID string) []*Run {
	runs, _ := s.Query("", "", jobID, 0, 0)
	return runs
}

func (s *RunStore) ActiveRuns() []*Run {
	runs, _ := s.Query("active", "", "", 0, 0)
	return runs
}

func (s *RunStore) ReadLog(id string, afterSeq int64) ([]LogLine, error) {
	if _, ok := s.Get(id); !ok {
		return nil, notFoundf("run not found")
	}
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `
		SELECT seq, ts, stream, level, message
		FROM run_log_lines WHERE run_id = $1 AND seq > $2 ORDER BY seq`, id, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogLine{}
	for rows.Next() {
		var ll LogLine
		var stream *string
		if err := rows.Scan(&ll.Seq, &ll.TS, &stream, &ll.Level, &ll.Message); err != nil {
			continue
		}
		if stream != nil {
			ll.Stream = *stream
		}
		out = append(out, ll)
	}
	return out, nil
}

func (s *RunStore) prune(keepPerJob int) {
	if keepPerJob <= 0 {
		return
	}
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `
		DELETE FROM runs
		WHERE id IN (
			SELECT id FROM (
				SELECT id, status,
					ROW_NUMBER() OVER (
						PARTITION BY COALESCE(job_id, '')
						ORDER BY started_at DESC, id DESC
					) AS rn
				FROM runs
			) t
			WHERE rn > $1 AND status NOT IN ('starting', 'running')
		)
		RETURNING id`, keepPerJob)
	if err != nil {
		return
	}
	defer rows.Close()
	s.mu.Lock()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			delete(s.live, id)
		}
	}
	s.mu.Unlock()
}

func (s *RunStore) deleteRun(id string) {
	ctx, cancel := dbCtx()
	defer cancel()
	_, _ = s.Pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, id)
	s.mu.Lock()
	delete(s.live, id)
	s.mu.Unlock()
}

func (s *RunStore) AppendSystemLine(id, level, message string) {
	line := LogLine{TS: time.Now().UTC(), Stream: "system", Level: level, Message: message}
	ctx, cancel := dbCtx()
	defer cancel()
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO run_log_lines (run_id, seq, ts, stream, level, message)
		SELECT $1, COALESCE(MAX(seq), 0) + 1, $2, $3, $4, $5
		FROM run_log_lines WHERE run_id = $1
		RETURNING seq`,
		id, line.TS, line.Stream, line.Level, line.Message,
	).Scan(&line.Seq)
	if err != nil {
		return
	}
	s.Bus.publishLog(id, line)
}

func (s *RunStore) reconcile(reap func(pid int, startToken string) bool) []string {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `SELECT id, repository_id, pid, pid_start FROM runs WHERE status IN ('starting','running')`)
	if err != nil {
		return nil
	}
	type item struct {
		id, repoID, startToken string
		pid                    int
	}
	var items []item
	for rows.Next() {
		var it item
		var pid *int
		var start *string
		if err := rows.Scan(&it.id, &it.repoID, &pid, &start); err != nil {
			continue
		}
		if pid != nil {
			it.pid = *pid
		}
		if start != nil {
			it.startToken = *start
		}
		items = append(items, it)
	}
	rows.Close()

	repoSet := map[string]bool{}
	for _, it := range items {
		if reap != nil && reap(it.pid, it.startToken) {
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
		s.dropLive(it.id)
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

func (s *RunStore) Begin(run *Run) (*runHandle, error) {
	now := time.Now().UTC()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	run.ID = newRunID(run.StartedAt)
	if run.Status == "" {
		run.Status = StatusStarting
	}

	if err := s.insertRun(run); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.live[run.ID] = run.clone()
	s.mu.Unlock()

	s.Bus.publishRun(run.clone())
	return &runHandle{store: s, id: run.ID}, nil
}

func (s *RunStore) mutate(id string, persist bool, fn func(*Run)) *Run {
	s.mu.Lock()
	run, ok := s.live[id]
	s.mu.Unlock()
	if !ok {
		run, ok = s.getDB(id)
		if !ok {
			return nil
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.live[id]; exists {
		run = existing
	} else {
		s.live[id] = run
	}
	fn(run)
	if persist {
		_ = s.updateRun(run)
	}
	return run.clone()
}

func (s *RunStore) dropLive(id string) {
	s.mu.Lock()
	delete(s.live, id)
	s.mu.Unlock()
}

func (s *RunStore) insertRun(run *Run) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := s.Pool.Exec(ctx, `INSERT INTO runs (`+runCols+`) VALUES (
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		runWriteArgs(run)...,
	)
	return err
}

func (s *RunStore) updateRun(run *Run) error {
	ctx, cancel := dbCtx()
	defer cancel()
	args := runWriteArgs(run)
	_, err := s.Pool.Exec(ctx, `UPDATE runs SET
		kind=$2, status=$3, job_id=$4, repository_id=$5, job_name=$6, folder_path=$7, repo_name=$8,
		params=$9, started_at=$10, finished_at=$11, pid=$12, pid_start=$13, progress=$14, summary=$15,
		exit_code=$16, error=$17
		WHERE id=$1`, args...)
	return err
}

func runWriteArgs(run *Run) []any {
	return []any{
		run.ID, string(run.Kind), string(run.Status),
		emptyToNil(run.JobID), run.RepositoryID, emptyToNil(run.JobName), emptyToNil(run.FolderPath), run.RepoName,
		mapJSON(run.Params), run.StartedAt, run.FinishedAt, zeroIntToNil(run.PID), emptyToNil(run.PIDStart),
		mustJSON(run.Progress), summaryJSON(run.Summary), run.ExitCode, emptyToNil(run.Error),
	}
}

func scanRun(row interface{ Scan(dest ...any) error }) (*Run, error) {
	var r Run
	var jobID, jobName, folderPath, pidStart, errMsg *string
	var params, progress, summary []byte
	var pid *int
	err := row.Scan(
		&r.ID, &r.Kind, &r.Status, &jobID, &r.RepositoryID, &jobName, &folderPath, &r.RepoName,
		&params, &r.StartedAt, &r.FinishedAt, &pid, &pidStart, &progress, &summary, &r.ExitCode, &errMsg,
	)
	if err != nil {
		return nil, err
	}
	if jobID != nil {
		r.JobID = *jobID
	}
	if jobName != nil {
		r.JobName = *jobName
	}
	if folderPath != nil {
		r.FolderPath = *folderPath
	}
	if pid != nil {
		r.PID = *pid
	}
	if pidStart != nil {
		r.PIDStart = *pidStart
	}
	if errMsg != nil {
		r.Error = *errMsg
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &r.Params)
	}
	if len(progress) > 0 {
		_ = json.Unmarshal(progress, &r.Progress)
	}
	if len(summary) > 0 {
		var sum Summary
		if json.Unmarshal(summary, &sum) == nil {
			r.Summary = &sum
		}
	}
	return &r, nil
}

func mapJSON(m map[string]string) any {
	if len(m) == 0 {
		return nil
	}
	return mustJSON(m)
}

func summaryJSON(s *Summary) any {
	if s == nil {
		return nil
	}
	return mustJSON(s)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func zeroIntToNil(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// runHandle is the live side of one running operation. It implements RunSink:
// the coordinator hands it to the Runner, which streams events through it into
// the durable record and the event bus.
type runHandle struct {
	store *RunStore
	id    string

	logMu sync.Mutex
	seq   int64
	done  bool

	progMu    sync.Mutex
	lastFlush time.Time
}

func (h *runHandle) Log(level, stream, message string) {
	h.logMu.Lock()
	if h.done {
		h.logMu.Unlock()
		return
	}
	h.seq++
	line := LogLine{Seq: h.seq, TS: time.Now().UTC(), Stream: stream, Level: level, Message: message}
	ctx, cancel := dbCtx()
	_, err := h.store.Pool.Exec(ctx, `
		INSERT INTO run_log_lines (run_id, seq, ts, stream, level, message)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		h.id, line.Seq, line.TS, emptyToNil(line.Stream), line.Level, line.Message,
	)
	cancel()
	if err != nil {
		h.seq-- // keep seq in 1:1 correspondence with durable lines
		h.logMu.Unlock()
		return
	}
	h.logMu.Unlock()
	h.store.Bus.publishLog(h.id, line)
}

func (h *runHandle) Progress(p Progress) {
	h.progMu.Lock()
	now := time.Now()
	flush := now.Sub(h.lastFlush) >= progressFlushInterval
	if flush {
		h.lastFlush = now
	}
	h.progMu.Unlock()

	h.store.mutate(h.id, flush, func(r *Run) { r.Progress = p })
	h.store.Bus.publishProgress(h.id, p)
}

func (h *runHandle) Summary(s Summary) {
	cp := s
	h.store.mutate(h.id, true, func(r *Run) { r.Summary = &cp })
}

func (h *runHandle) PID(pid int, startToken string) {
	h.store.mutate(h.id, true, func(r *Run) { r.PID = pid; r.PIDStart = startToken })
}

func (h *runHandle) setStatus(status RunStatus) {
	run := h.store.mutate(h.id, true, func(r *Run) { r.Status = status })
	if run != nil {
		h.store.Bus.publishRun(run)
	}
}

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
	h.done = true
	h.logMu.Unlock()
	h.store.dropLive(h.id)

	if run != nil {
		h.store.Bus.publishRun(run)
	}
	return run
}
