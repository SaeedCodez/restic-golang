package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
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
