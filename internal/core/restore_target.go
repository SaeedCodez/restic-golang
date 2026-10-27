package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

// restorePlan is the restic restore invocation for a user-facing destination.
// Restore always replaces that destination (--delete): snapshot files overwrite,
// and files that are not in the snapshot are removed.
type restorePlan struct {
	// Target is restic's --target. "/" means write snapshot paths in place.
	Target string
	// Include is passed as restic --include filters. Required when Target is
	// "/" — restic refuses `--target / --delete` without a filter so a restore
	// cannot wipe the whole filesystem.
	Include []string
}

// planResticRestore picks --target and --include for a replace-restore.
//
// Snapshots from this app store absolute paths (e.g. /docker-volumes/…/_data).
// restic restores as target + snapshot_path, so restoring "back into" that same
// folder with --target <folder> nests another copy of the path under itself.
// When the requested destination equals a snapshot path, use "/" so files land
// at their original absolute locations (in-place restore), and --include the
// folder so --delete only affects that tree.
func planResticRestore(requested string, snapshotPaths []string) restorePlan {
	target := resolveResticRestoreTarget(requested, snapshotPaths)
	if !isResticRootTarget(target) {
		return restorePlan{Target: target}
	}
	req := filepath.Clean(strings.TrimSpace(requested))
	if req != "" && req != "/" {
		return restorePlan{Target: target, Include: []string{req}}
	}
	return restorePlan{Target: target, Include: cleanRestoreIncludes(snapshotPaths)}
}

// resolveResticRestoreTarget picks the --target passed to restic.
func resolveResticRestoreTarget(requested string, snapshotPaths []string) string {
	req := filepath.Clean(strings.TrimSpace(requested))
	if req == "" {
		return requested
	}
	for _, p := range snapshotPaths {
		if filepath.Clean(strings.TrimSpace(p)) == req {
			return "/"
		}
	}
	return requested
}

func isResticRootTarget(target string) bool {
	return filepath.Clean(strings.TrimSpace(target)) == "/"
}

func cleanRestoreIncludes(paths []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || p == "." {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// restoreArgs is the restic restore command (after -r). The destination is
// replaced: --delete removes files that are not in the snapshot.
func restoreArgs(snapshotID, target string, include []string) ([]string, error) {
	filters := cleanRestoreIncludes(include)
	if isResticRootTarget(target) && len(filters) == 0 {
		return nil, fmt.Errorf("cannot restore to / as a replacement without a path to limit what is deleted")
	}
	args := []string{"restore", "--target", target, "--delete", "--json"}
	for _, p := range filters {
		args = append(args, "--include", escapeResticPattern(p))
	}
	return append(args, "--", snapshotID), nil
}

// escapeResticPattern makes a path match only itself as a restic --include:
// restic reads includes as glob patterns, so a "[", "*" or "?" in a file name
// would otherwise also match (and --delete) its look-alike siblings.
func escapeResticPattern(p string) string {
	var b strings.Builder
	for _, r := range p {
		switch r {
		case '\\', '*', '?', '[':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// planResticRestoreSelection is planResticRestore limited to the selected
// snapshot paths: restic --include's each of them, so --delete only replaces
// what was selected. Every selected path must be in the snapshot and, for an
// in-place restore, inside the folder being restored.
func planResticRestoreSelection(requested string, snapshotPaths, selected []string) (restorePlan, error) {
	plan := planResticRestore(requested, snapshotPaths)
	sel := cleanRestoreIncludes(selected)
	if len(sel) == 0 {
		return plan, nil
	}
	req := filepath.Clean(strings.TrimSpace(requested))
	for _, p := range sel {
		if !filepath.IsAbs(p) {
			return restorePlan{}, fmt.Errorf("selected path %q must be absolute", p)
		}
		inSnapshot := false
		for _, sp := range snapshotPaths {
			if _, ok := pathWithin(p, filepath.Clean(sp)); ok {
				inSnapshot = true
				break
			}
		}
		if !inSnapshot {
			return restorePlan{}, fmt.Errorf("%s is not in this snapshot", p)
		}
		if isResticRootTarget(plan.Target) && req != "/" {
			if _, ok := pathWithin(p, req); !ok {
				return restorePlan{}, fmt.Errorf("%s is outside %s, the folder being restored", p, req)
			}
		}
	}
	plan.Include = sel
	return plan, nil
}
