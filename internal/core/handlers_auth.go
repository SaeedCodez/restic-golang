package core

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// authBodyLimit caps auth request bodies: these endpoints answer without a
// session.
const authBodyLimit = 64 << 10

type passwordBody struct {
	Password string `json:"password"`
	// SetupToken is read by first-run setup only.
	SetupToken string `json:"setupToken,omitempty"`
}

type changePasswordBody struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleAuthStatus is public: the UI uses it to choose setup vs login vs app.
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	configured := s.app.Auth.Configured()
	authenticated := false
	if configured {
		if c, err := r.Cookie(sessionCookieName); err == nil {
			authenticated = s.app.Sessions.Valid(c.Value)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"setupRequired": !configured,
		"authenticated": authenticated,
	})
}

// handleAuthSetup sets the password on first open and starts a session. It
// needs the setup token the server printed to its log (see NewSetupToken).
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	key := loginClientKey(r)
	if s.refuseLockedOut(w, key) {
		return
	}
	var body passwordBody
	if !decodeAuthBody(w, r, &body) {
		return
	}
	// Once a password is set, SetupPassword refuses with a conflict instead.
	if !s.app.Auth.Configured() && !s.app.CheckSetupToken(body.SetupToken) {
		s.logins.fail(key)
		errorJSON(w, http.StatusForbidden, "bad_setup_token",
			"The setup token is missing or wrong. The server prints it in its log when it starts with no password set.")
		return
	}
	password := strings.TrimSpace(body.Password)
	if err := s.app.Auth.SetupPassword(password); err != nil {
		writeAuthError(w, err)
		return
	}
	s.logins.succeed(key)
	if err := s.startSession(w, r); err != nil {
		errorJSON(w, http.StatusInternalServerError, "session", "Password was set, but creating a session failed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true})
}

// handleAuthLogin verifies the password and sets the session cookie.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.app.Auth.Configured() {
		errorJSON(w, http.StatusConflict, "setup_required", "Set up a password first.")
		return
	}
	key := loginClientKey(r)
	if s.refuseLockedOut(w, key) {
		return
	}
	var body passwordBody
	if !decodeAuthBody(w, r, &body) {
		return
	}
	if !s.app.Auth.CheckPassword(body.Password) {
		s.logins.fail(key)
		errorJSON(w, http.StatusUnauthorized, "bad_password", "Incorrect password.")
		return
	}
	s.logins.succeed(key)
	if err := s.startSession(w, r); err != nil {
		errorJSON(w, http.StatusInternalServerError, "session", "Could not create a session.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true})
}

// handleAuthLogout clears the session cookie.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.app.Sessions.Revoke(c.Value)
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAuthChangePassword updates the login password (requires a valid session).
func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	var body changePasswordBody
	if !decodeAuthBody(w, r, &body) {
		return
	}
	if err := s.app.Auth.ChangePassword(body.CurrentPassword, strings.TrimSpace(body.NewPassword)); err != nil {
		writeAuthError(w, err)
		return
	}
	// Drop other sessions; keep the caller logged in.
	s.app.Sessions.KeepOnly(sessionToken(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// decodeAuthBody reads a capped JSON body into dst, writing a 400 on failure.
func decodeAuthBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, authBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid_json", "Request body must be JSON.")
		return false
	}
	return true
}

// refuseLockedOut answers 429 when the client has guessed wrong too often.
func (s *Server) refuseLockedOut(w http.ResponseWriter, key string) bool {
	wait := s.logins.wait(key)
	if wait <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	errorJSON(w, http.StatusTooManyRequests, "too_many_attempts",
		fmt.Sprintf("Too many wrong attempts. Try again in %d minute(s).", int(math.Ceil(wait.Minutes()))))
	return true
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) error {
	token, err := s.app.Sessions.Create()
	if err != nil {
		return err
	}
	http.SetCookie(w, sessionCookie(r, token, int(sessionTTL.Seconds())))
	return nil
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", -1))
}

// sessionCookie is the session cookie with its attributes. SameSite=Strict
// keeps it off every request another site starts, even a link the user
// follows; the UI's own requests come from this origin, so they always carry
// it. Over HTTPS it is also Secure, so it never travels in plain text.
func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
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
