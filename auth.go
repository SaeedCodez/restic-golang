package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"crypto/pbkdf2"
)

const (
	authFileName       = "auth.json"
	minPasswordLength  = 6
	pbkdf2Iterations   = 210_000
	pbkdf2KeyLen       = 32
	passwordSaltLen    = 16
	sessionTokenBytes  = 32
	sessionCookieName  = "restic_session"
	sessionTTL         = 30 * 24 * time.Hour
)

// AuthStore persists the single app-login password (hashed) under the data dir.
type AuthStore struct {
	mu   sync.RWMutex
	path string
	salt []byte
	hash []byte
}

type authFile struct {
	Salt      string    `json:"salt"`
	Hash      string    `json:"hash"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func loadAuthStore(path string) (*AuthStore, error) {
	s := &AuthStore{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	var f authFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("could not parse auth store %q: %w", path, err)
	}
	salt, err := hex.DecodeString(f.Salt)
	if err != nil || len(salt) == 0 {
		return nil, fmt.Errorf("auth store %q has an invalid salt", path)
	}
	hash, err := hex.DecodeString(f.Hash)
	if err != nil || len(hash) == 0 {
		return nil, fmt.Errorf("auth store %q has an invalid hash", path)
	}
	s.salt = salt
	s.hash = hash
	return s, nil
}

func (s *AuthStore) Configured() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.hash) > 0 && len(s.salt) > 0
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return validf("password must be at least %d characters", minPasswordLength)
	}
	return nil
}

func hashPassword(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
}

func (s *AuthStore) saveLocked(salt, hash []byte) error {
	f := authFile{
		Salt:      hex.EncodeToString(salt),
		Hash:      hex.EncodeToString(hash),
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.salt = salt
	s.hash = hash
	return nil
}

// SetupPassword sets the initial password. It fails if one is already configured.
func (s *AuthStore) SetupPassword(password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.hash) > 0 {
		return conflictf("a password is already set; log in and change it from Settings")
	}
	salt := make([]byte, passwordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := hashPassword(password, salt)
	if err != nil {
		return err
	}
	return s.saveLocked(salt, hash)
}

// CheckPassword reports whether password matches the stored hash.
func (s *AuthStore) CheckPassword(password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.hash) == 0 || len(s.salt) == 0 {
		return false
	}
	hash, err := hashPassword(password, s.salt)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(hash, s.hash) == 1
}

// ChangePassword verifies the current password and stores a new one.
func (s *AuthStore) ChangePassword(current, next string) error {
	if err := validatePassword(next); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.hash) == 0 {
		return validf("no password is set yet; use setup instead")
	}
	curHash, err := hashPassword(current, s.salt)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(curHash, s.hash) != 1 {
		return validf("current password is incorrect")
	}
	salt := make([]byte, passwordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := hashPassword(next, salt)
	if err != nil {
		return err
	}
	return s.saveLocked(salt, hash)
}

// SessionManager holds in-memory login sessions (re-login after process restart).
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]time.Time)}
}

func (m *SessionManager) Create() (string, error) {
	var b [sessionTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	m.sessions[token] = time.Now().Add(sessionTTL)
	return token, nil
}

func (m *SessionManager) Valid(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *SessionManager) Revoke(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (m *SessionManager) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]time.Time)
}

// KeepOnly revokes every session except token, and refreshes that session's expiry.
func (m *SessionManager) KeepOnly(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]time.Time)
	if token != "" {
		m.sessions[token] = time.Now().Add(sessionTTL)
	}
}

func (m *SessionManager) pruneLocked() {
	now := time.Now()
	for tok, exp := range m.sessions {
		if now.After(exp) {
			delete(m.sessions, tok)
		}
	}
}
