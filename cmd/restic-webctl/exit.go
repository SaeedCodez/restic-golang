package main

import "fmt"

// Stable exit codes for agents and scripts.
const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitAuth    = 3
	exitNotFound = 4
	exitConflict = 5
)

// apiError is a structured failure from the server or CLI logic.
type apiError struct {
	Status  int
	Code    string
	Message string
	Body    map[string]any
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
	return fmt.Sprintf("HTTP %d", e.Status)
}

func exitCodeOf(err error) int {
	if err == nil {
		return exitOK
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
		switch {
		case ae.Status == 401:
			return exitAuth
		case ae.Status == 404:
			return exitNotFound
		case ae.Status == 409:
			return exitConflict
		}
	}
	if _, ok := err.(*usageError); ok {
		return exitUsage
	}
	return exitError
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}
