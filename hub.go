package main

import (
	"encoding/json"
	"sync"
)

// Event is a single message broadcast to the browser over SSE. Using a map
// keeps the JSON shape flexible while staying easy to construct in handlers.
type Event map[string]any

// historyLimit caps how many recent events are retained for replay, so a long
// backup that emits thousands of status lines does not grow memory without bound.
const historyLimit = 1000

// Hub fans out events to all connected SSE clients and enforces that only one
// long-running restic operation (backup, restore, download, init) runs at a
// time. It also keeps a short history of the current operation so a browser
// that connects late — or reloads mid-operation — is caught up immediately.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	history [][]byte
	busy    bool
	busyOp  string
}

func newHub() *Hub {
	return &Hub{subs: make(map[chan []byte]struct{})}
}

// subscribe registers a new client and returns its channel plus a snapshot of
// the current event history to replay.
func (h *Hub) subscribe() (chan []byte, [][]byte) {
	ch := make(chan []byte, historyLimit+64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	hist := make([][]byte, len(h.history))
	copy(hist, h.history)
	h.mu.Unlock()
	return ch, hist
}

// unsubscribe removes a client and closes its channel exactly once.
func (h *Hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Send marshals an event, appends it to history and delivers it to every
// connected client. Delivery is non-blocking: a client that cannot keep up
// simply misses intermediate updates (it can always re-read state via the API).
func (h *Hub) Send(ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.history = append(h.history, data)
	if len(h.history) > historyLimit {
		h.history = h.history[len(h.history)-historyLimit:]
	}
	for ch := range h.subs {
		select {
		case ch <- data:
		default:
		}
	}
	h.mu.Unlock()
}

// begin marks an operation as running. It returns false if one is already in
// progress, which the caller turns into a clear "busy" response. On success it
// resets the event history so the new operation starts with a clean slate, and
// broadcasts a "busy" event so the UI can disable conflicting actions.
func (h *Hub) begin(op string) bool {
	h.mu.Lock()
	if h.busy {
		h.mu.Unlock()
		return false
	}
	h.busy = true
	h.busyOp = op
	h.history = nil
	h.mu.Unlock()

	h.Send(Event{"type": "busy", "busy": true, "op": op})
	return true
}

// end clears the running state and notifies clients.
func (h *Hub) end() {
	h.mu.Lock()
	op := h.busyOp
	h.busy = false
	h.busyOp = ""
	h.mu.Unlock()

	h.Send(Event{"type": "busy", "busy": false, "op": op})
}

// status reports whether an operation is running and which one.
func (h *Hub) status() (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.busy, h.busyOp
}
