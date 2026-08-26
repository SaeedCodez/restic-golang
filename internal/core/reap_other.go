//go:build !linux

package core

// procStartToken cannot read a process start time without /proc, so it returns
// an empty token; reaping is then skipped (we never kill a pid we cannot verify).
func procStartToken(pid int) string { return "" }

// isOwnResticProcess cannot positively identify a process without /proc, so it
// reports false: non-Linux platforms rely on marking the run interrupted plus
// restic's own stale-lock handling (and a defensive `restic unlock`) rather than
// risk killing a recycled pid.
func isOwnResticProcess(pid int, startToken string) bool { return false }
