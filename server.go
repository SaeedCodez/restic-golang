package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Server holds the shared state for all HTTP handlers.
type Server struct {
	app *App

	// Legacy demo state, kept while the single-config backup/restore path is
	// migrated to jobs and runs. Removed once runs replace it.
	store *ConfigStore
	hub   *Hub
}

func newServer(app *App, store *ConfigStore, hub *Hub) *Server {
	return &Server{app: app, store: store, hub: hub}
}

// routes registers every endpoint on a fresh mux. Static UI files are served
// from the embedded filesystem; everything under /api is handled here.
func (s *Server) routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()

	// Entity management (repositories, folders, jobs).
	mux.HandleFunc("GET /api/repositories", s.handleRepoList)
	mux.HandleFunc("POST /api/repositories", s.handleRepoCreate)
	mux.HandleFunc("GET /api/repositories/{id}", s.handleRepoGet)
	mux.HandleFunc("PUT /api/repositories/{id}", s.handleRepoUpdate)
	mux.HandleFunc("DELETE /api/repositories/{id}", s.handleRepoDelete)

	mux.HandleFunc("GET /api/folders", s.handleFolderList)
	mux.HandleFunc("POST /api/folders", s.handleFolderCreate)
	mux.HandleFunc("GET /api/folders/{id}", s.handleFolderGet)
	mux.HandleFunc("PUT /api/folders/{id}", s.handleFolderUpdate)
	mux.HandleFunc("DELETE /api/folders/{id}", s.handleFolderDelete)

	mux.HandleFunc("GET /api/jobs", s.handleJobList)
	mux.HandleFunc("POST /api/jobs", s.handleJobCreate)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJobGet)
	mux.HandleFunc("PUT /api/jobs/{id}", s.handleJobUpdate)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleJobDelete)

	// Legacy demo endpoints (single config). Being migrated to jobs/runs.
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/test", s.handleTest)
	mux.HandleFunc("/api/init", s.handleInit)
	mux.HandleFunc("/api/backup", s.handleBackup)
	mux.HandleFunc("/api/snapshots", s.handleSnapshots)
	mux.HandleFunc("/api/restore", s.handleRestore)
	mux.HandleFunc("/api/download", s.handleDownload)
	mux.HandleFunc("/api/events", s.handleEvents)

	// Everything else is the single-page UI.
	mux.Handle("/", static)
	return mux
}

// ---- small helpers ---------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorJSON writes a structured error the UI can react to. code lets the UI
// special-case situations like "no_restic" or "not_initialized".
func errorJSON(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "code": code, "error": msg})
}

// requireRestic returns false (after writing a friendly error) if restic is
// missing. Every handler that shells out to restic calls this first.
func (s *Server) requireRestic(w http.ResponseWriter) bool {
	if !resticInstalled() {
		errorJSON(w, http.StatusServiceUnavailable, "no_restic",
			"restic is not installed or not on your PATH. Install it (e.g. `brew install restic` or see https://restic.net) and restart this app.")
		return false
	}
	return true
}

// ---- /api/status -----------------------------------------------------------

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	busy, busyOp := s.hub.status()

	resp := map[string]any{
		"ok":              true,
		"resticInstalled": resticInstalled(),
		"busy":            busy,
		"busyOp":          busyOp,
		"backendType":     cfg.BackendType,
		"configValid":     cfg.Validate() == nil,
	}
	if resp["resticInstalled"].(bool) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if v, err := resticVersion(ctx); err == nil {
			resp["resticVersion"] = v
		}
	}
	if repo, err := cfg.Repository(); err == nil {
		resp["repository"] = repo
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- /api/settings ---------------------------------------------------------

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s.store.Get()})
	case http.MethodPost:
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			errorJSON(w, http.StatusBadRequest, "bad_request", "Could not read settings: "+err.Error())
			return
		}
		if cfg.BackendType != "S3" && cfg.BackendType != "Local" {
			errorJSON(w, http.StatusBadRequest, "bad_request", `Backend type must be "S3" or "Local".`)
			return
		}
		if err := s.store.Set(cfg); err != nil {
			errorJSON(w, http.StatusInternalServerError, "save_failed", "Could not save settings: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s.store.Get()})
	default:
		w.Header().Set("Allow", "GET, POST")
		errorJSON(w, http.StatusMethodNotAllowed, "method", "Method not allowed.")
	}
}

// ---- /api/test -------------------------------------------------------------

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	cfg := s.store.Get()
	if err := cfg.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_config", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res := resticTest(ctx, &cfg)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          res.OK,
		"initialized": res.Initialized,
		"message":     res.Message,
		"detail":      res.Detail,
	})
}

// ---- /api/init -------------------------------------------------------------

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorJSON(w, http.StatusMethodNotAllowed, "method", "Use POST.")
		return
	}
	if !s.requireRestic(w) {
		return
	}
	cfg := s.store.Get()
	if err := cfg.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_config", err.Error())
		return
	}
	if !s.hub.begin("init") {
		errorJSON(w, http.StatusConflict, "busy", "Another operation is running. Please wait for it to finish.")
		return
	}
	defer s.hub.end()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := resticInit(ctx, &cfg)
	if err != nil {
		errorJSON(w, http.StatusOK, "init_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Repository initialized successfully.", "detail": out})
}

// ---- /api/backup -----------------------------------------------------------

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorJSON(w, http.StatusMethodNotAllowed, "method", "Use POST.")
		return
	}
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", "Could not read request: "+err.Error())
		return
	}
	source := strings.TrimSpace(body.Source)

	cfg := s.store.Get()
	if err := cfg.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_config", err.Error())
		return
	}
	if err := validateSourceDir(source); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_path", err.Error())
		return
	}

	if !s.hub.begin("backup") {
		errorJSON(w, http.StatusConflict, "busy", "A backup or restore is already running. Please wait for it to finish.")
		return
	}

	// Run the backup in the background; progress streams over SSE.
	go func() {
		defer s.hub.end()
		s.hub.Send(Event{"type": "started", "op": "backup", "message": "Backing up " + source, "source": source})

		ctx := context.Background()
		err := runStreaming(ctx, &cfg, "backup", s.hub, "backup", source, "--json")
		if err != nil {
			s.hub.Send(Event{"type": "log", "op": "backup", "level": "error", "message": "Backup failed: " + err.Error()})
			s.hub.Send(Event{"type": "done", "op": "backup", "ok": false, "message": "Backup failed."})
			return
		}
		s.hub.Send(Event{"type": "done", "op": "backup", "ok": true, "message": "Backup complete."})
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "message": "Backup started."})
}

// ---- /api/snapshots --------------------------------------------------------

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if !s.requireRestic(w) {
		return
	}
	cfg := s.store.Get()
	if err := cfg.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_config", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	snaps, err := resticSnapshots(ctx, &cfg)
	if err != nil {
		if errors.Is(err, errNotInitialized) {
			errorJSON(w, http.StatusOK, "not_initialized", "No repository found yet. Initialize one in Settings to start backing up.")
			return
		}
		errorJSON(w, http.StatusOK, "restic_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "snapshots": snaps})
}

// ---- /api/restore ----------------------------------------------------------

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorJSON(w, http.StatusMethodNotAllowed, "method", "Use POST.")
		return
	}
	if !s.requireRestic(w) {
		return
	}
	var body struct {
		SnapshotID string `json:"snapshotId"`
		Target     string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_request", "Could not read request: "+err.Error())
		return
	}
	snapID := strings.TrimSpace(body.SnapshotID)
	target := strings.TrimSpace(body.Target)

	cfg := s.store.Get()
	if err := cfg.Validate(); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_config", err.Error())
		return
	}
	if snapID == "" {
		errorJSON(w, http.StatusBadRequest, "bad_request", "Please choose a snapshot to restore.")
		return
	}
	if target == "" {
		errorJSON(w, http.StatusBadRequest, "bad_path", "Please provide a target folder for the restore.")
		return
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		errorJSON(w, http.StatusBadRequest, "bad_path", "Could not create target folder: "+err.Error())
		return
	}

	if !s.hub.begin("restore") {
		errorJSON(w, http.StatusConflict, "busy", "A backup or restore is already running. Please wait for it to finish.")
		return
	}

	go func() {
		defer s.hub.end()
		s.hub.Send(Event{"type": "started", "op": "restore", "message": "Restoring " + shortID(snapID) + " to " + target, "target": target})

		ctx := context.Background()
		err := runStreaming(ctx, &cfg, "restore", s.hub, "restore", snapID, "--target", target, "--json")
		if err != nil {
			s.hub.Send(Event{"type": "log", "op": "restore", "level": "error", "message": "Restore failed: " + err.Error()})
			s.hub.Send(Event{"type": "done", "op": "restore", "ok": false, "message": "Restore failed."})
			return
		}
		s.hub.Send(Event{"type": "done", "op": "restore", "ok": true, "message": "Restore complete: " + target})
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "message": "Restore started."})
}

// ---- /api/download ---------------------------------------------------------
//
// Download restores the snapshot to a temporary directory, streams a zip of its
// contents to the browser, then removes the temporary directory.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !resticInstalled() {
		http.Error(w, "restic is not installed.", http.StatusServiceUnavailable)
		return
	}
	snapID := strings.TrimSpace(r.URL.Query().Get("id"))
	if snapID == "" {
		http.Error(w, "Missing snapshot id.", http.StatusBadRequest)
		return
	}
	cfg := s.store.Get()
	if err := cfg.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !s.hub.begin("download") {
		http.Error(w, "A backup or restore is already running. Please wait for it to finish.", http.StatusConflict)
		return
	}
	defer s.hub.end()

	s.hub.Send(Event{"type": "log", "op": "download", "level": "info", "message": "Preparing download of snapshot " + shortID(snapID) + "..."})

	tmp, err := os.MkdirTemp("", "restic-download-")
	if err != nil {
		http.Error(w, "Could not create temporary directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmp)

	// Restore into the temp dir (errors here can still return a proper status).
	cmd, err := command(r.Context(), &cfg, "restore", snapID, "--target", tmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := "Could not restore snapshot for download: " + firstLine(strings.TrimSpace(string(out)))
		s.hub.Send(Event{"type": "log", "op": "download", "level": "error", "message": msg})
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	// Stream the zip. Once headers are written we are committed to a 200.
	filename := "snapshot-" + shortID(snapID) + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	if err := zipDir(w, tmp); err != nil {
		// Headers are already sent; the best we can do is log it.
		s.hub.Send(Event{"type": "log", "op": "download", "level": "error", "message": "Error while building zip: " + err.Error()})
		return
	}
	s.hub.Send(Event{"type": "log", "op": "download", "level": "info", "message": "Download ready: " + filename})
}

// ---- /api/events (Server-Sent Events) --------------------------------------

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is not supported by this server.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering, if any

	ch, history := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	fmt.Fprint(w, ": connected\n\n")
	for _, msg := range history {
		writeSSE(w, msg)
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
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
			writeSSE(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, data []byte) {
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// ---- validation / utility --------------------------------------------------

// validateSourceDir checks that source is a usable folder to back up.
func validateSourceDir(source string) error {
	if source == "" {
		return errors.New("please enter the absolute path of the folder you want to back up")
	}
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("that folder does not exist: %s", source)
	}
	if err != nil {
		return fmt.Errorf("could not read that path: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("that path is a file, not a folder: %s", source)
	}
	return nil
}

// shortID returns a short, readable form of a snapshot id for messages.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// zipDir writes a zip archive of everything under root to w, using paths
// relative to root as the entry names.
func zipDir(w io.Writer, root string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Zip entries always use forward slashes.
		name := filepath.ToSlash(rel)

		// Skip anything that is not a regular file (symlinks, sockets, etc.).
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(info.Mode())
		header.Modified = info.ModTime()

		zf, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(zf, f)
		return err
	})
}
