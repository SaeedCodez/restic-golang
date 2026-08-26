package core

import (
	"encoding/json"
	"sync"
)

// sseMessage is one pre-marshaled event ready to write to an SSE stream. For log
// events seq is set and used both as the SSE id (so Last-Event-ID resume works)
// and to de-duplicate the seam between a stream's replayed backlog and its live
// tail.
type sseMessage struct {
	isLog bool
	seq   int64
	data  []byte
}

// broadcaster is the live-delivery side of the event bus. It fans out per-run
// events to that run's subscribers and run-level state changes to a global
// stream (for lists and badges). It holds no history — the durable run store is
// the source of truth for catch-up — so a browser that misses a live event
// recovers it by re-reading the run's record and log. Delivery is best-effort:
// a subscriber that cannot keep up drops events and resyncs from the store.
type broadcaster struct {
	mu         sync.Mutex
	runSubs    map[string]map[chan sseMessage]struct{}
	globalSubs map[chan sseMessage]struct{}
}

func newBroadcaster() *broadcaster {
	return &broadcaster{
		runSubs:    map[string]map[chan sseMessage]struct{}{},
		globalSubs: map[chan sseMessage]struct{}{},
	}
}

const sseBuffer = 256

func (b *broadcaster) subscribeRun(id string) chan sseMessage {
	ch := make(chan sseMessage, sseBuffer)
	b.mu.Lock()
	m := b.runSubs[id]
	if m == nil {
		m = map[chan sseMessage]struct{}{}
		b.runSubs[id] = m
	}
	m[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broadcaster) unsubscribeRun(id string, ch chan sseMessage) {
	b.mu.Lock()
	if m := b.runSubs[id]; m != nil {
		if _, ok := m[ch]; ok {
			delete(m, ch)
			close(ch)
			if len(m) == 0 {
				delete(b.runSubs, id)
			}
		}
	}
	b.mu.Unlock()
}

func (b *broadcaster) subscribeGlobal() chan sseMessage {
	ch := make(chan sseMessage, sseBuffer)
	b.mu.Lock()
	b.globalSubs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broadcaster) unsubscribeGlobal(ch chan sseMessage) {
	b.mu.Lock()
	if _, ok := b.globalSubs[ch]; ok {
		delete(b.globalSubs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// send delivers msg to a set of subscribers without blocking: a full buffer
// drops the message (the subscriber will resync from the durable store).
func send(subs map[chan sseMessage]struct{}, msg sseMessage) {
	for ch := range subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (b *broadcaster) sendRun(id string, msg sseMessage) {
	b.mu.Lock()
	send(b.runSubs[id], msg)
	b.mu.Unlock()
}

// ---- eventBus implementation ----------------------------------------------

func (b *broadcaster) publishLog(runID string, line LogLine) {
	data, err := json.Marshal(map[string]any{"type": "log", "runId": runID, "line": line})
	if err != nil {
		return
	}
	b.sendRun(runID, sseMessage{isLog: true, seq: line.Seq, data: data})
}

func (b *broadcaster) publishProgress(runID string, p Progress) {
	data, err := json.Marshal(map[string]any{"type": "progress", "runId": runID, "progress": p})
	if err != nil {
		return
	}
	b.sendRun(runID, sseMessage{data: data})
}

func (b *broadcaster) publishRun(run *Run) {
	data, err := json.Marshal(map[string]any{"type": "run", "run": run})
	if err != nil {
		return
	}
	msg := sseMessage{data: data}
	b.mu.Lock()
	send(b.runSubs[run.ID], msg)
	send(b.globalSubs, msg)
	b.mu.Unlock()
}
