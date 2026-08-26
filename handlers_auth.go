package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type passwordBody struct {
	Password string `json:"password"`
}

type changePasswordBody struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleAuthStatus is public: the UI uses it to choose setup vs login vs app.
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	configured := s.app.auth.Configured()
	authenticated := false
	if configured {
		if c, err := r.Cookie(sessionCookieName); err == nil {
			authenticated = s.app.sessions.Valid(c.Value)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"setupRequired":  !configured,
		"authenticated":  authenticated,
	})
}

// handleAuthSetup sets the password on first open and starts a session.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_json", "Request body must be JSON.")
		return
	}
	password := strings.TrimSpace(body.Password)
	if err := s.app.auth.SetupPassword(password); err != nil {
		writeAuthError(w, err)
		return
	}
	if err := s.startSession(w); err != nil {
		errorJSON(w, http.StatusInternalServerError, "session", "Password was set, but creating a session failed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true})
}

// handleAuthLogin verifies the password and sets the session cookie.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.app.auth.Configured() {
		errorJSON(w, http.StatusConflict, "setup_required", "Set up a password first.")
		return
	}
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_json", "Request body must be JSON.")
		return
	}
	if !s.app.auth.CheckPassword(body.Password) {
		errorJSON(w, http.StatusUnauthorized, "bad_password", "Incorrect password.")
		return
	}
	if err := s.startSession(w); err != nil {
		errorJSON(w, http.StatusInternalServerError, "session", "Could not create a session.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true})
}

// handleAuthLogout clears the session cookie.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.app.sessions.Revoke(c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAuthChangePassword updates the login password (requires a valid session).
func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	var body changePasswordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_json", "Request body must be JSON.")
		return
	}
	if err := s.app.auth.ChangePassword(body.CurrentPassword, strings.TrimSpace(body.NewPassword)); err != nil {
		writeAuthError(w, err)
		return
	}
	// Drop other sessions; keep the caller logged in.
	s.app.sessions.KeepOnly(sessionToken(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) startSession(w http.ResponseWriter) error {
	token, err := s.app.sessions.Create()
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch e := err.(type) {
	case *ValidationError:
		errorJSON(w, http.StatusBadRequest, "invalid", e.Msg)
	case *ConflictError:
		errorJSON(w, http.StatusConflict, "conflict", e.Msg)
	default:
		errorJSON(w, http.StatusInternalServerError, "error", err.Error())
	}
}

// sessionToken returns the cookie value if present.
func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
