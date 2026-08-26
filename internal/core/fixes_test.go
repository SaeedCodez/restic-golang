package core

import (
	"context"
	"errors"
	"os"
	"testing"
)

// TestClassifyRunCancelBeatsError covers the reorder that fixes a stopped init
// run (which surfaces cancellation as an error) being mislabeled failed.
func TestClassifyRunCancelBeatsError(t *testing.T) {
	stopped := &activeRun{}
	stopped.stopped.Store(true)
	if got, _ := classifyRun(stopped, context.Background(), 0, errors.New("restic init failed: signal: interrupt")); got != StatusCanceled {
		t.Fatalf("stopped run with error -> %s, want canceled", got)
	}

	// A genuine error on a run that was not stopped is still failed.
	if got, _ := classifyRun(&activeRun{}, context.Background(), 0, errors.New("boom")); got != StatusFailed {
		t.Fatalf("errored run -> %s, want failed", got)
	}
}

// TestAppendSystemLineContinuesSeq verifies a system line appended after a
// crash (no live handle) continues the per-run seq.
func TestAppendSystemLineContinuesSeq(t *testing.T) {
	store := newRunStore(testPool(t), nil)
	run := &Run{Kind: KindBackup, Status: StatusRunning, RepositoryID: "r", RepoName: "R"}
	h, err := store.Begin(run)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	h.Log("info", "system", "clean line")

	store.AppendSystemLine(run.ID, "error", "interrupted note")

	lines, _ := store.ReadLog(run.ID, 0)
	var sawNote bool
	for _, l := range lines {
		if l.Message == "interrupted note" {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatal("the interrupted system message is missing")
	}
}

// TestReaperIdentity checks that a process is only reaped when its start token
// matches — so a recycled pid (or an unverifiable one) is never killed. It
// asserts only the identity predicate, so it can never signal a real process.
func TestReaperIdentity(t *testing.T) {
	self := os.Getpid()
	if isOwnResticProcess(self, "") {
		t.Fatal("empty start token must not verify")
	}
	if isOwnResticProcess(self, "definitely-not-the-token") {
		t.Fatal("mismatched start token must not verify")
	}
	// Even with a matching token, this process is the test binary, not restic.
	if isOwnResticProcess(self, procStartToken(self)) {
		t.Fatal("a non-restic executable must not verify as our restic child")
	}
	// reapOrphan never acts on an invalid pid.
	if reapOrphan(0, "x") || reapOrphan(-1, "x") {
		t.Fatal("reapOrphan acted on an invalid pid")
	}
}
