package core

import (
	"context"
	"path/filepath"
	"strings"
)

// resolveResticRestoreTarget picks the --target passed to restic.
//
// Snapshots from this app store absolute paths (e.g. /docker-volumes/…/_data).
// restic restores as target + snapshot_path, so restoring "back into" that same
// folder with --target <folder> nests another copy of the path under itself.
// When the requested destination equals a snapshot path, use "/" so files land
// at their original absolute locations (in-place restore).
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
