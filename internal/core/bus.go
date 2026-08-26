package core

// eventBus is the seam between the run pipeline (which produces durable events)
// and live delivery to browsers. The run store and coordinator publish through
// it; today a no-op backs it, and Step 7 swaps in the SSE broadcaster. Keeping
// this behind an interface means the durable pipeline never depends on any
// browser being connected — live delivery is a pure accelerator on top of the
// durable record.
type eventBus interface {
	// publishLog delivers a newly-appended log line for a run.
	publishLog(runID string, line LogLine)
	// publishProgress delivers the latest (last-value-wins) progress for a run.
	publishProgress(runID string, p Progress)
	// publishRun delivers a run-level state change (status transition / finish),
	// carrying a snapshot of the run for lists, badges and the run view.
	publishRun(run *Run)
}

// noopBus discards everything. Used until the SSE broadcaster is wired in.
type noopBus struct{}

func (noopBus) publishLog(string, LogLine)       {}
func (noopBus) publishProgress(string, Progress) {}
func (noopBus) publishRun(*Run)                  {}
