//go:build !windows

package core

import (
	"os/exec"
	"syscall"
)

// configureSysProcAttr starts restic in its own process group so the app can
// signal the whole group when stopping a run or reaping an orphan after a crash.
// (We deliberately do NOT use Pdeathsig: in Go it is delivered when the OS thread
// that spawned the child exits, which can happen while the app is still alive and
// would kill a healthy backup. Orphan handling is done by reconcile-on-startup.)
func configureSysProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalProcessGroup sends sig to the process group led by pid. With Setpgid the
// child is its own group leader, so its pgid equals its pid.
func signalProcessGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}

// processAlive reports whether a process with the given pid currently exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Signal 0 performs error checking without sending a signal.
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
