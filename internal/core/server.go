package core

import (
	"archive/zip"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Server holds the shared state for all HTTP handlers: everything lives on App.
type Server struct {
	app *App
}

func NewServer(app *App) *Server {
	return &Server{app: app}
}

// routes registers every endpoint on a fresh mux. Static UI files are served
// from the embedded filesystem; everything under /api is handled here.
// API routes (except auth setup/login/status) require a valid session cookie.
func (s *Server) Routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()

	// Auth: status/setup/login are public; password change and logout need a session.
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("POST /api/auth/password", s.handleAuthChangePassword)

	// App-wide status.
	mux.HandleFunc("GET /api/status", s.handleStatus)

	// Entity management (repositories, folders, jobs).
	mux.HandleFunc("GET /api/repositories", s.handleRepoList)
	mux.HandleFunc("POST /api/repositories", s.handleRepoCreate)
	mux.HandleFunc("GET /api/repositories/{id}", s.handleRepoGet)
	mux.HandleFunc("PUT /api/repositories/{id}", s.handleRepoUpdate)
	mux.HandleFunc("DELETE /api/repositories/{id}", s.handleRepoDelete)
	mux.HandleFunc("POST /api/repositories/{id}/test", s.handleRepoTest)
	mux.HandleFunc("POST /api/repositories/{id}/init", s.handleRepoInit)
	mux.HandleFunc("POST /api/repositories/{id}/unlock", s.handleRepoUnlock)
	mux.HandleFunc("GET /api/repositories/{id}/snapshots", s.handleRepoSnapshots)
	mux.HandleFunc("POST /api/repositories/{id}/restore", s.handleRepoRestore)
	mux.HandleFunc("POST /api/repositories/{id}/download", s.handleRepoDownload)
	mux.HandleFunc("POST /api/repositories/{id}/forget", s.handleRepoForget)
	mux.HandleFunc("POST /api/repositories/{id}/reset", s.handleRepoReset)

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
	mux.HandleFunc("GET /api/jobs/{id}/snapshots", s.handleJobSnapshots)

	// Runs: every long-running operation is a run, watched the same way.
	mux.HandleFunc("POST /api/jobs/{id}/run", s.handleJobRun)
	mux.HandleFunc("POST /api/jobs/{id}/retention", s.handleJobRetention)
	mux.HandleFunc("POST /api/jobs/{id}/forget", s.handleJobForget)
	mux.HandleFunc("GET /api/jobs/{id}/runs", s.handleJobRuns)
	mux.HandleFunc("GET /api/runs", s.handleRunList)
	mux.HandleFunc("GET /api/runs/{id}", s.handleRunGet)
	mux.HandleFunc("GET /api/runs/{id}/log", s.handleRunLog)
	mux.HandleFunc("POST /api/runs/{id}/stop", s.handleRunStop)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	mux.HandleFunc("GET /api/runs/{id}/download", s.handleRunDownload)

	// Live activity stream for lists and badges.
	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Everything else is the single-page UI.
	mux.Handle("/", static)
	return s.requireAuth(mux)
}

// publicAPIPaths do not require a session cookie.
var publicAPIPaths = map[string]bool{
	"/api/auth/status": true,
	"/api/auth/setup":  true,
	"/api/auth/login":  true,
	"/api/auth/logout": true,
}

// requireAuth gates /api/* behind a configured password and a valid session.
// The embedded UI itself stays public so the login and setup pages can load.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") || publicAPIPaths[path] {
			next.ServeHTTP(w, r)
			return
		}
		if !s.app.Auth.Configured() {
			errorJSON(w, http.StatusUnauthorized, "setup_required",
				"Set a login password on the setup page before using the app.")
			return
		}
		if !s.app.Sessions.Valid(sessionToken(r)) {
			errorJSON(w, http.StatusUnauthorized, "unauthorized", "Please log in.")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	if !s.app.Runner.Available() {
		errorJSON(w, http.StatusServiceUnavailable, "no_restic",
			"restic is not installed or not on your PATH. Install it (e.g. `brew install restic` or see https://restic.net) and restart this app.")
		return false
	}
	return true
}

// ---- shared utilities ------------------------------------------------------

// shortID returns a short, readable form of a snapshot id for messages.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// zipDir writes a zip archive of everything under root to w, using paths
// relative to root as the entry names.
// ZipDir writes a zip archive of everything under root to w.
func ZipDir(w io.Writer, root string) error {
	return zipDir(w, root)
}

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
		name := filepath.ToSlash(rel)

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
