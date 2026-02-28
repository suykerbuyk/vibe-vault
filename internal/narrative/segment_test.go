package narrative

import (
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/transcript"
)

func TestSegmentEntries_NoCompaction(t *testing.T) {
	entries := []transcript.Entry{
		{Type: "user", UUID: "u1"},
		{Type: "assistant", UUID: "a1"},
		{Type: "user", UUID: "u2"},
	}
	segs := SegmentEntries(entries)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if len(segs[0]) != 3 {
		t.Errorf("expected 3 entries in segment, got %d", len(segs[0]))
	}
}

func TestSegmentEntries_SingleBoundary(t *testing.T) {
	entries := []transcript.Entry{
		{Type: "user", UUID: "u1"},
		{Type: "assistant", UUID: "a1"},
		{Type: "system", Subtype: "compact_boundary"},
		{Type: "user", UUID: "u2"},
		{Type: "assistant", UUID: "a2"},
	}
	segs := SegmentEntries(entries)
	if len(segs) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segs))
	}
	if len(segs[0]) != 2 {
		t.Errorf("segment 0: expected 2 entries, got %d", len(segs[0]))
	}
	if len(segs[1]) != 2 {
		t.Errorf("segment 1: expected 2 entries, got %d", len(segs[1]))
	}
}

func TestSegmentEntries_MultipleBoundaries(t *testing.T) {
	entries := []transcript.Entry{
		{Type: "user", UUID: "u1"},
		{Type: "system", Subtype: "compact_boundary"},
		{Type: "user", UUID: "u2"},
		{Type: "system", Subtype: "compact_boundary"},
		{Type: "user", UUID: "u3"},
	}
	segs := SegmentEntries(entries)
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}
}

func TestSegmentEntries_BoundaryExcluded(t *testing.T) {
	entries := []transcript.Entry{
		{Type: "user", UUID: "u1", Timestamp: time.Unix(100, 0)},
		{Type: "system", Subtype: "compact_boundary"},
		{Type: "user", UUID: "u2", Timestamp: time.Unix(200, 0)},
	}
	segs := SegmentEntries(entries)
	// Boundary entry should not appear in either segment
	for i, seg := range segs {
		for _, e := range seg {
			if e.Type == "system" && e.Subtype == "compact_boundary" {
				t.Errorf("segment %d contains boundary entry", i)
			}
		}
	}
}

func TestSegmentEntries_EmptyInput(t *testing.T) {
	segs := SegmentEntries(nil)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment for empty input, got %d", len(segs))
	}
	if len(segs[0]) != 0 {
		t.Errorf("expected empty segment, got %d entries", len(segs[0]))
	}
}
