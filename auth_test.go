package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthSetupLoginAndProtectsAPI(t *testing.T) {
	app := testApp(t, newResticRunner())
	h := newServer(app).routes(http.NotFoundHandler())

	// Before setup, protected APIs refuse.
	if code := doJSON(t, h, "GET", "/api/status", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("status before setup: code=%d, want 401", code)
	}

	var status struct {
		SetupRequired bool `json:"setupRequired"`
		Authenticated bool `json:"authenticated"`
	}
	if code := doJSON(t, h, "GET", "/api/auth/status", nil, &status); code != http.StatusOK {
		t.Fatalf("auth status: code=%d", code)
	}
	if !status.SetupRequired || status.Authenticated {
		t.Fatalf("expected setupRequired=true authenticated=false, got %+v", status)
	}

	// Short passwords are rejected.
	if code := doJSON(t, h, "POST", "/api/auth/setup", map[string]any{"password": "short"}, nil); code != http.StatusBadRequest {
		t.Fatalf("short password: code=%d, want 400", code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/setup", jsonBody(t, map[string]any{"password": "secret1"}))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: code=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie := cookieNamed(rec, sessionCookieName)
	if cookie == nil || cookie.Value == "" {
		t.Fatal("setup did not set a session cookie")
	}

	// Setup again must fail.
	if code := doJSON(t, h, "POST", "/api/auth/setup", map[string]any{"password": "another"}, nil); code != http.StatusConflict {
		t.Fatalf("second setup: code=%d, want 409", code)
	}

	// Authenticated request succeeds.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/status", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with session: code=%d", rec.Code)
	}

	// Wrong password on login.
	if code := doJSON(t, h, "POST", "/api/auth/login", map[string]any{"password": "nope"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("bad login: code=%d, want 401", code)
	}

	// Logout clears access.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/status", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status after logout: code=%d, want 401", rec.Code)
	}

	// Login again works.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/login", jsonBody(t, map[string]any{"password": "secret1"}))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: code=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie = cookieNamed(rec, sessionCookieName)
	if cookie == nil {
		t.Fatal("login did not set a session cookie")
	}

	// Change password.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/auth/password", jsonBody(t, map[string]any{
		"currentPassword": "secret1",
		"newPassword":     "secret2",
	}))
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Old password fails; new one works. Current session still valid.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/status", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after password change: code=%d", rec.Code)
	}
	if code := doJSON(t, h, "POST", "/api/auth/login", map[string]any{"password": "secret1"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old password still works: code=%d", code)
	}
	if code := doJSON(t, h, "POST", "/api/auth/login", map[string]any{"password": "secret2"}, nil); code != http.StatusOK {
		t.Fatalf("new password login: code=%d", code)
	}
}

func TestAuthPersistsAcrossRestart(t *testing.T) {
	app := testApp(t, newResticRunner())
	if err := app.auth.SetupPassword("persisted"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	app2 := testApp(t, newResticRunner())
	if !app2.auth.Configured() {
		t.Fatal("password was not persisted")
	}
	if !app2.auth.CheckPassword("persisted") {
		t.Fatal("persisted password did not verify")
	}
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
