package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- typed errors ----------------------------------------------------------
//
// Handlers map these to HTTP status codes: ConflictError -> 409, NotFoundError
// -> 404, ValidationError -> 400.

type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

type NotFoundError struct{ Msg string }

func (e *NotFoundError) Error() string { return e.Msg }

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func conflictf(format string, a ...any) error { return &ConflictError{Msg: fmt.Sprintf(format, a...)} }
func notFoundf(format string, a ...any) error { return &NotFoundError{Msg: fmt.Sprintf(format, a...)} }
func validf(format string, a ...any) error    { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

// ---- id generation ---------------------------------------------------------

// newID returns a short, random, url-safe id for an entity.
func newID() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; fall back to a time-derived id.
		return "id" + strconv.FormatInt(time.Now().UTC().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// ---- generic entity store --------------------------------------------------

// entityPtr constrains the store to types whose pointer exposes a *Meta via the
// embedded Meta block. This lets one generic store manage repositories, folders
// and jobs identically.
type entityPtr[T any] interface {
	*T
	getMeta() *Meta
}

// EntityStore persists a slice of entities to a single JSON file and guards
// concurrent access. Entities are all-scalar structs, so value copies handed out
// by Get/List are true copies that callers cannot use to mutate stored state.
// It generalizes the demo's ConfigStore (atomic temp+rename), now with fsync of
// the file and its directory so a crash cannot expose a half-written store.
type EntityStore[T any, PT entityPtr[T]] struct {
	mu    sync.RWMutex
	path  string
	kind  string
	items []T
}

// loadEntityStore reads the store file at path. A missing or empty file yields an
// empty store (written on the first Create).
func loadEntityStore[T any, PT entityPtr[T]](path, kind string) (*EntityStore[T, PT], error) {
	s := &EntityStore[T, PT]{path: path, kind: kind}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.items); err != nil {
		return nil, fmt.Errorf("could not parse %s store %q: %w", kind, path, err)
	}
	return s, nil
}

func (s *EntityStore[T, PT]) metaOf(i int) *Meta { return PT(&s.items[i]).getMeta() }

// List returns all entities, sorted by name (case-insensitive), as copies.
func (s *EntityStore[T, PT]) List() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]T, len(s.items))
	copy(out, s.items)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(PT(&out[i]).getMeta().Name) < strings.ToLower(PT(&out[j]).getMeta().Name)
	})
	return out
}

// Get returns a copy of the entity with the given id.
func (s *EntityStore[T, PT]) Get(id string) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.items {
		if s.metaOf(i).ID == id {
			return s.items[i], true
		}
	}
	var zero T
	return zero, false
}

// Exists reports whether an entity with the given id is present.
func (s *EntityStore[T, PT]) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

// Count returns the number of stored entities.
func (s *EntityStore[T, PT]) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Create assigns an id and timestamps, enforces name uniqueness, appends the
// entity and persists. It returns the stored entity (with id/timestamps set).
func (s *EntityStore[T, PT]) Create(item T) (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero T
	m := PT(&item).getMeta()
	name := strings.TrimSpace(m.Name)
	if name == "" {
		return zero, validf("a %s name is required", s.kind)
	}
	if s.nameTakenLocked(name, "") {
		return zero, conflictf("a %s named %q already exists", s.kind, name)
	}

	now := time.Now().UTC()
	m.ID = newID()
	m.Name = name
	m.CreatedAt = now
	m.UpdatedAt = now

	s.items = append(s.items, item)
	if err := s.saveLocked(); err != nil {
		s.items = s.items[:len(s.items)-1]
		return zero, err
	}
	return item, nil
}

// Update replaces the entity with the given id, preserving CreatedAt, enforcing
// name uniqueness among the others, and stamping UpdatedAt.
func (s *EntityStore[T, PT]) Update(id string, item T) (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero T
	idx := -1
	for i := range s.items {
		if s.metaOf(i).ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return zero, notFoundf("%s not found", s.kind)
	}

	m := PT(&item).getMeta()
	name := strings.TrimSpace(m.Name)
	if name == "" {
		return zero, validf("a %s name is required", s.kind)
	}
	if s.nameTakenLocked(name, id) {
		return zero, conflictf("a %s named %q already exists", s.kind, name)
	}

	prev := s.metaOf(idx)
	m.ID = id
	m.Name = name
	m.CreatedAt = prev.CreatedAt
	m.UpdatedAt = time.Now().UTC()

	old := s.items[idx]
	s.items[idx] = item
	if err := s.saveLocked(); err != nil {
		s.items[idx] = old
		return zero, err
	}
	return item, nil
}

// Delete removes the entity with the given id.
func (s *EntityStore[T, PT]) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.items {
		if s.metaOf(i).ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return notFoundf("%s not found", s.kind)
	}

	old := s.items
	s.items = append(s.items[:idx:idx], s.items[idx+1:]...)
	if err := s.saveLocked(); err != nil {
		s.items = old
		return err
	}
	return nil
}

func (s *EntityStore[T, PT]) nameTakenLocked(name, exceptID string) bool {
	lower := strings.ToLower(name)
	for i := range s.items {
		m := s.metaOf(i)
		if m.ID == exceptID {
			continue
		}
		if strings.ToLower(m.Name) == lower {
			return true
		}
	}
	return false
}

func (s *EntityStore[T, PT]) saveLocked() error {
	return writeJSONFileAtomic(s.path, s.items)
}

// ---- atomic file helpers ---------------------------------------------------

// writeJSONFileAtomic marshals v (indented) and writes it atomically.
func writeJSONFileAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// writeFileAtomic writes data to a temp file in the same directory, fsyncs it,
// then renames it over path and fsyncs the directory. The rename is atomic, so a
// reader never sees a partial file, and the fsyncs make the result durable across
// power loss (not just a clean process exit).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir fsyncs a directory so a rename within it is durable. Best-effort:
// some filesystems do not support directory fsync and return an error we ignore.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
