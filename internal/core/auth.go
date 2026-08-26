package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"

	"crypto/pbkdf2"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	minPasswordLength = 6
	pbkdf2Iterations  = 210_000
	pbkdf2KeyLen      = 32
	passwordSaltLen   = 16
	sessionTokenBytes = 32
	sessionCookieName = "restic_session"
	sessionTTL        = 30 * 24 * time.Hour
)

// AuthStore persists the single app-login password (hashed) in Postgres.
type AuthStore struct {
	mu   sync.RWMutex
	Pool *pgxpool.Pool
	salt []byte
	hash []byte
}

func loadAuthStore(pool *pgxpool.Pool) (*AuthStore, error) {
	s := &AuthStore{Pool: pool}
	ctx, cancel := dbCtx()
	defer cancel()
	var salt, hash []byte
	err := pool.QueryRow(ctx, `SELECT salt, hash FROM auth WHERE id = 1`).Scan(&salt, &hash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return s, nil
		}
		return nil, err
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
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO auth (id, salt, hash, updated_at) VALUES (1, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET salt = EXCLUDED.salt, hash = EXCLUDED.hash, updated_at = EXCLUDED.updated_at`,
		salt, hash, time.Now().UTC(),
	)
	if err != nil {
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
