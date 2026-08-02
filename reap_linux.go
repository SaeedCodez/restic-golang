//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procStartToken returns a stable token identifying the process instance: its
// start time (field 22 of /proc/<pid>/stat). Because a start time is fixed at
// creation, a recycled pid — which belongs to a process created later — cannot
// produce the same token, so comparing it rules out pid reuse.
func procStartToken(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(data)
	// The comm field (field 2) is parenthesized and may contain spaces, so parse
	// after the final ')'. Fields after it start at field 3 (state); starttime is
	// field 22, i.e. index 19 of that remainder.
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return ""
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

// isOwnResticProcess reports whether pid is the SAME live restic process we
// launched: its current start token must match the recorded one (ruling out pid
// reuse) and its executable must look like restic. A missing token means we
// cannot verify identity, so we refuse to reap.
func isOwnResticProcess(pid int, startToken string) bool {
	if startToken == "" || procStartToken(pid) != startToken {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	exe := string(data)
	if j := strings.IndexByte(exe, 0); j >= 0 {
		exe = exe[:j]
	}
	// Match the executable's basename exactly, so our own "restic-web" (or any
	// other name that merely contains "restic") is never mistaken for the CLI.
	base := strings.ToLower(filepath.Base(exe))
	return base == "restic" || base == "restic.exe"
}
