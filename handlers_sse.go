package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// This file holds the Server-Sent Events endpoints. There are two streams:
//
//   GET /api/runs/{id}/events  detailed progress + log for one run, with durable
//                              backlog replay and seq-based resume.
//   GET /api/events            run-level state changes for lists and badges.
//
// Live delivery is a pure accelerator: the durable run record and log are the
// source of truth, so a late joiner, a refresh, a second tab or a reconnect all
// reconstruct exact state and never depend on browser memory.

const sseKeepalive = 15 * time.Second

func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering, if any
}

func writeSSEMessage(w io.Writer, msg sseMessage) {
	if msg.isLog {
		fmt.Fprintf(w, "id: %d\n", msg.seq)
	}
	fmt.Fprintf(w, "data: %s\n\n", msg.data)
}

// sseResumeSeq returns the log seq to resume after: the EventSource Last-Event-ID
// header (set automatically on reconnect) or an explicit ?after= query, else 0.
func sseResumeSeq(r *http.Request) int64 {
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.app.runs.Get(id); !ok {
		errorJSON(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	setSSEHeaders(w)

	afterSeq := sseResumeSeq(r)

	// Subscribe BEFORE reading the backlog, so a log line appended during replay
	// is captured on the channel and delivered afterward (deduped by seq) rather
	// than lost in the gap between "finished reading backlog" and "started
	// tailing".
	ch := s.app.bcast.subscribeRun(id)
	defer s.app.bcast.unsubscribeRun(id, ch)

	fmt.Fprint(w, ": connected\n\n")

	// Current record first, so a late joiner immediately has status + progress.
	if run, ok := s.app.runs.Get(id); ok {
		if data, err := json.Marshal(map[string]any{"type": "run", "run": run}); err == nil {
			writeSSEMessage(w, sseMessage{data: data})
		}
	}

	// Durable backlog: every log line after the resume point.
	maxSeq := afterSeq
	if lines, err := s.app.runs.ReadLog(id, afterSeq); err == nil {
		for _, ln := range lines {
			if data, err := json.Marshal(map[string]any{"type": "log", "runId": id, "line": ln}); err == nil {
				writeSSEMessage(w, sseMessage{isLog: true, seq: ln.Seq, data: data})
			}
			if ln.Seq > maxSeq {
				maxSeq = ln.Seq
			}
		}
	}
	flusher.Flush()

	ticker := time.NewTicker(sseKeepalive)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg.isLog {
				if msg.seq <= maxSeq {
					continue // already delivered via backlog
				}
				maxSeq = msg.seq
			}
			writeSSEMessage(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	setSSEHeaders(w)

	ch := s.app.bcast.subscribeGlobal()
	defer s.app.bcast.unsubscribeGlobal(ch)

	fmt.Fprint(w, ": connected\n\n")

	// Snapshot the currently-active runs AFTER subscribing, so the client's live
	// badge/list is seeded from the stream itself and never depends on a separate
	// status fetch racing the subscription.
	for _, run := range s.app.runs.activeRuns() {
		if data, err := json.Marshal(map[string]any{"type": "run", "run": run}); err == nil {
			writeSSEMessage(w, sseMessage{data: data})
		}
	}
	flusher.Flush()

	ticker := time.NewTicker(sseKeepalive)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			writeSSEMessage(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
