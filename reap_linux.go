//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
)

// isOwnResticProcess reports whether pid is a live restic process, verified via
// /proc so a recycled pid is never mistaken for our orphaned restic child (which
// would risk killing an unrelated process).
func isOwnResticProcess(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	// cmdline is NUL-separated; the first field is the executable path.
	exe := string(data)
	if i := strings.IndexByte(exe, 0); i >= 0 {
		exe = exe[:i]
	}
	return strings.Contains(strings.ToLower(exe), "restic")
}
