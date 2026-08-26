//go:build windows

package core

import (
	"os"
	"os/exec"
	"syscall"
)

// configureSysProcAttr is a no-op on Windows, which does not use POSIX process
// groups. Cancellation still works via exec.Cmd's Cancel/WaitDelay.
func configureSysProcAttr(cmd *exec.Cmd) {}

// signalProcessGroup best-effort kills the process on Windows (no process-group
// semantics here). sig is ignored; the process is terminated.
func signalProcessGroup(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// processAlive reports whether a process with the given pid currently exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess succeeds for any pid; a zero-signal is unavailable,
	// so this is best-effort. Reconcile still marks runs interrupted regardless.
	return p != nil
}
