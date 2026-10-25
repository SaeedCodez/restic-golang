//go:build !linux

package core

import "os/exec"

// killWithParent is Linux-only (Pdeathsig); elsewhere it does nothing.
func killWithParent(cmd *exec.Cmd) {}
