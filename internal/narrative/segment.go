package narrative

import "github.com/johns/vibe-vault/internal/transcript"

// segmentEntries splits transcript entries at compact_boundary system messages.
// Returns one or more slices of entries. Boundary entries are excluded from both segments.
func segmentEntries(entries []transcript.Entry) [][]transcript.Entry {
	var segments [][]transcript.Entry
	var current []transcript.Entry

	for _, e := range entries {
		if e.Type == "system" && e.Subtype == "compact_boundary" {
			if len(current) > 0 {
				segments = append(segments, current)
			}
			current = nil
			continue
		}
		current = append(current, e)
	}

	// Final segment
	if len(current) > 0 {
		segments = append(segments, current)
	}

	// Ensure at least one empty segment for empty input
	if len(segments) == 0 {
		segments = append(segments, nil)
	}

	return segments
}
