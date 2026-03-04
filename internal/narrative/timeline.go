// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package narrative

import (
	"fmt"
	"strings"
)

// RenderTimeline generates a timestamped activity timeline from segments.
// Returns empty string for trivial sessions (<=5 activities).
func RenderTimeline(segments []Segment) string {
	// Count total activities
	total := 0
	for _, seg := range segments {
		total += len(seg.Activities)
	}
	if total <= 5 {
		return ""
	}

	var b strings.Builder
	for _, seg := range segments {
		for _, a := range seg.Activities {
			ts := a.Timestamp
			if ts.IsZero() {
				continue
			}
			timeStr := ts.Format("15:04")

			icon := activityIcon(a.Kind)
			b.WriteString(fmt.Sprintf("%s  %s %s\n", timeStr, icon, a.Description))
		}
	}

	return b.String()
}

func activityIcon(kind ActivityKind) string {
	switch kind {
	case KindFileCreate:
		return "Created"
	case KindFileModify:
		return "Modified"
	case KindTestRun:
		return "Tests"
	case KindGitCommit:
		return "Committed"
	case KindGitPush:
		return "Pushed"
	case KindBuild:
		return "Built"
	case KindCommand:
		return "Ran"
	case KindDecision:
		return "Decided"
	case KindPlanMode:
		return "Planned"
	case KindDelegation:
		return "Delegated"
	case KindExplore:
		return "Explored"
	case KindError:
		return "Error"
	default:
		return ""
	}
}
