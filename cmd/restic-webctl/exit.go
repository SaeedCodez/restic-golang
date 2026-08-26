package main

import (
	"errors"
	"fmt"

	"restic-web/internal/core"
)

// Stable exit codes for agents and scripts.
const (
	exitOK       = 0
	exitError    = 1
	exitUsage    = 2
	exitAuth     = 3
	exitNotFound = 4
	exitConflict = 5
)

// apiError is a structured CLI failure (kept for constructed errors with codes).
type apiError struct {
	Code    string
	Message string
	Extra   map[string]any
}

func (e *apiError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "error"
}

func exitCodeOf(err error) int {
	if err == nil {
		return exitOK
	}
	if _, ok := err.(*usageError); ok {
		return exitUsage
	}
	if ae, ok := err.(*apiError); ok {
		switch ae.Code {
		case "unauthorized", "bad_password", "setup_required":
			return exitAuth
		case "not_found":
			return exitNotFound
		case "busy", "conflict", "not_active":
			return exitConflict
		}
		return exitError
	}
	var busy *core.BusyError
	if errors.As(err, &busy) {
		return exitConflict
	}
	var conflict *core.ConflictError
	if errors.As(err, &conflict) {
		return exitConflict
	}
	var nf *core.NotFoundError
	if errors.As(err, &nf) {
		return exitNotFound
	}
	var ve *core.ValidationError
	if errors.As(err, &ve) {
		return exitError
	}
	if errors.Is(err, core.ErrRunNotActive) {
		return exitConflict
	}
	return exitError
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}
