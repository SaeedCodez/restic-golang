package core

import (
	"context"
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
	args := []string{"restore", snapshotID, "--target", target, "--delete", "--json"}
	for _, p := range filters {
		args = append(args, "--include", p)
	}
	return args, nil
}

// lookupSnapshotPaths returns the paths recorded on a snapshot, or nil if the
// snapshot cannot be resolved (caller falls back to the requested target).
func lookupSnapshotPaths(ctx context.Context, runner Runner, repo *Repository, snapshotID string) []string {
	snapshotID = strings.TrimSpace(snapshotID)
	if runner == nil || repo == nil || snapshotID == "" {
		return nil
	}
	snaps, err := runner.Snapshots(ctx, repo, "")
	if err != nil {
		return nil
	}
	var prefixMatch *Snapshot
	for i := range snaps {
		s := &snaps[i]
		if s.ID == snapshotID || s.ShortID == snapshotID {
			return s.Paths
		}
		if strings.HasPrefix(s.ID, snapshotID) {
			if prefixMatch != nil {
				// Ambiguous short prefix — do not guess.
				return nil
			}
			prefixMatch = s
		}
	}
	if prefixMatch != nil {
		return prefixMatch.Paths
	}
	return nil
}
