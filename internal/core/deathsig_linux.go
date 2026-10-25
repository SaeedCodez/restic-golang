//go:build linux

package core

import (
	"os/exec"
	"syscall"
)

// killWithParent has the kernel SIGKILL cmd's process when the thread that
// started it exits. Pdeathsig follows the thread, not the process, so the
// caller must keep the starting goroutine locked to its OS thread
// (runtime.LockOSThread) until the child has exited; then the thread can only
// go away with the whole process.
func killWithParent(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
