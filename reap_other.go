//go:build !linux

package main

// isOwnResticProcess cannot positively identify a restic process on platforms
// without /proc, so it reports false: rather than risk killing a recycled pid,
// non-Linux platforms rely on marking the run interrupted plus restic's own
// stale-lock handling (and a defensive `restic unlock`) on the next operation.
func isOwnResticProcess(pid int) bool { return false }
