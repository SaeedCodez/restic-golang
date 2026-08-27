package core

import (
	"testing"
)

func TestDecodeSnapshotsWithSummary(t *testing.T) {
	out := []byte(`[
	  {
	    "id":"aabbcc","short_id":"aabb","time":"2026-01-01T00:00:00Z",
	    "paths":["/data"],"hostname":"host","tags":["t"],
	    "summary":{"total_bytes_processed":4096,"total_files_processed":12}
	  }
	]`)
	snaps, missing, err := decodeSnapshots(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || len(missing) != 0 {
		t.Fatalf("snaps=%d missing=%v", len(snaps), missing)
	}
	if snaps[0].SizeBytes == nil || *snaps[0].SizeBytes != 4096 {
		t.Fatalf("size: %+v", snaps[0].SizeBytes)
	}
	if snaps[0].FileCount == nil || *snaps[0].FileCount != 12 {
		t.Fatalf("files: %+v", snaps[0].FileCount)
	}
}

func TestDecodeSnapshotsWithoutSummary(t *testing.T) {
	out := []byte(`[
	  {"id":"aabbcc","short_id":"aabb","time":"2026-01-01T00:00:00Z","paths":["/data"],"hostname":"host","tags":[]}
	]`)
	snaps, missing, err := decodeSnapshots(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || len(missing) != 1 || missing[0] != 0 {
		t.Fatalf("snaps=%d missing=%v", len(snaps), missing)
	}
	if snaps[0].SizeBytes != nil || snaps[0].FileCount != nil {
		t.Fatalf("expected nil size/files, got %+v / %+v", snaps[0].SizeBytes, snaps[0].FileCount)
	}
}
