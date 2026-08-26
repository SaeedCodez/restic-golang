package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// testServer builds a Server backed by a fresh temp data dir and returns its
// HTTP handler, already authenticated for API calls.
func testServer(t *testing.T) http.Handler {
	t.Helper()
	return routesFor(t, testApp(t, newResticRunner()))
}

// routesFor returns the app's HTTP handler with a test session cookie injected
// on every request, so existing API tests stay focused on their own behaviour.
func routesFor(t *testing.T, app *App) http.Handler {
	t.Helper()
	if !app.Auth.Configured() {
		if err := app.Auth.SetupPassword("test-password"); err != nil {
			t.Fatalf("setup test auth: %v", err)
		}
	}
	token, err := app.Sessions.Create()
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}
	inner := NewServer(app).Routes(http.NotFoundHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(sessionCookieName); err != nil {
			r = r.Clone(r.Context())
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		inner.ServeHTTP(w, r)
	})
}

// doJSON performs an HTTP request with an optional JSON body and decodes the
// JSON response into out (if non-nil). It returns the status code.
func doJSON(t *testing.T, h http.Handler, method, path string, body any, out any) int {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil {
		_ = json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec.Code
}

func TestEntityCRUDFlow(t *testing.T) {
	h := testServer(t)

	// Create a repository.
	var repoResp struct {
		OK         bool       `json:"ok"`
		Repository Repository `json:"repository"`
	}
	if code := doJSON(t, h, "POST", "/api/repositories",
		map[string]any{"name": "Local", "backendType": "Local", "localPath": "/tmp/r", "password": "pw"},
		&repoResp); code != http.StatusOK || !repoResp.OK {
		t.Fatalf("create repo: code=%d resp=%+v", code, repoResp)
	}
	repoID := repoResp.Repository.ID
	if repoID == "" {
		t.Fatal("no repo id returned")
	}

	// Create a folder.
	var folderResp struct {
		OK     bool   `json:"ok"`
		Folder Folder `json:"folder"`
	}
	if code := doJSON(t, h, "POST", "/api/folders",
		map[string]any{"name": "Docs", "path": "/home/me/docs"}, &folderResp); code != http.StatusOK {
		t.Fatalf("create folder: code=%d", code)
	}
	folderID := folderResp.Folder.ID

	// Create a job referencing both.
	var jobResp struct {
		OK  bool    `json:"ok"`
		Job jobView `json:"job"`
	}
	if code := doJSON(t, h, "POST", "/api/jobs",
		map[string]any{"name": "Nightly", "folderId": folderID, "repositoryId": repoID}, &jobResp); code != http.StatusOK {
		t.Fatalf("create job: code=%d", code)
	}
	if jobResp.Job.RepoName != "Local" || jobResp.Job.FolderName != "Docs" {
		t.Fatalf("job view not enriched: %+v", jobResp.Job)
	}
	if jobResp.Job.Tag != "resticweb-job:"+jobResp.Job.ID {
		t.Fatalf("job tag wrong: %q", jobResp.Job.Tag)
	}
	jobID := jobResp.Job.ID

	// Deleting a repository referenced by a job must conflict.
	if code := doJSON(t, h, "DELETE", "/api/repositories/"+repoID, nil, nil); code != http.StatusConflict {
		t.Fatalf("delete referenced repo: code=%d, want 409", code)
	}
	// Same for the folder.
	if code := doJSON(t, h, "DELETE", "/api/folders/"+folderID, nil, nil); code != http.StatusConflict {
		t.Fatalf("delete referenced folder: code=%d, want 409", code)
	}

	// Delete the job, then the repo and folder succeed.
	if code := doJSON(t, h, "DELETE", "/api/jobs/"+jobID, nil, nil); code != http.StatusOK {
		t.Fatalf("delete job: code=%d", code)
	}
	if code := doJSON(t, h, "DELETE", "/api/repositories/"+repoID, nil, nil); code != http.StatusOK {
		t.Fatalf("delete repo: code=%d", code)
	}
}

func TestEntityValidationAndConflicts(t *testing.T) {
	h := testServer(t)

	// Missing password → 400.
	if code := doJSON(t, h, "POST", "/api/repositories",
		map[string]any{"name": "NoPass", "backendType": "Local", "localPath": "/x"}, nil); code != http.StatusBadRequest {
		t.Fatalf("repo missing password: code=%d, want 400", code)
	}

	// Create one, then a duplicate name → 409.
	doJSON(t, h, "POST", "/api/repositories",
		map[string]any{"name": "Dup", "backendType": "Local", "localPath": "/x", "password": "pw"}, nil)
	if code := doJSON(t, h, "POST", "/api/repositories",
		map[string]any{"name": "dup", "backendType": "Local", "localPath": "/y", "password": "pw"}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate repo name: code=%d, want 409", code)
	}

	// Job referencing a non-existent folder → 400.
	if code := doJSON(t, h, "POST", "/api/jobs",
		map[string]any{"name": "Bad", "folderId": "nope", "repositoryId": "nope"}, nil); code != http.StatusBadRequest {
		t.Fatalf("job bad refs: code=%d, want 400", code)
	}

	// GET a missing entity → 404.
	if code := doJSON(t, h, "GET", "/api/jobs/missing", nil, nil); code != http.StatusNotFound {
		t.Fatalf("get missing job: code=%d, want 404", code)
	}
}

func TestLegacyConfigImport(t *testing.T) {
	dir := t.TempDir()
	// Write a legacy config.json.
	cfgPath := filepath.Join(dir, "config.json")
	b, err := json.Marshal(map[string]any{
		"backendType": "Local", "localPath": "/srv/repo", "password": "pw",
	})
	if err != nil {
		t.Fatalf("marshal legacy config: %v", err)
	}
	if err := os.WriteFile(cfgPath, b, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	app := testApp(t, newResticRunner())
	if err := app.ImportLegacyConfig(cfgPath); err != nil {
		t.Fatalf("import: %v", err)
	}
	repos := app.Repos.List()
	if len(repos) != 1 || repos[0].Name != "Default" || repos[0].LocalPath != "/srv/repo" {
		t.Fatalf("import result: %+v", repos)
	}

	// Importing again must be a no-op (already have repositories).
	if err := app.ImportLegacyConfig(cfgPath); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if app.Repos.Count() != 1 {
		t.Fatalf("second import created duplicates: count=%d", app.Repos.Count())
	}
}
