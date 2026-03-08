// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package narrative

import (
	"fmt"
	"strings"
)

// RenderWorkPerformed renders the "Work Performed" section markdown.
func RenderWorkPerformed(segments []Segment) string {
	// Count total activities
	total := 0
	for _, seg := range segments {
		total += len(seg.Activities)
	}
	if total == 0 {
		return ""
	}

	var b strings.Builder

	if len(segments) == 1 {
		// Single segment: flat list
		renderActivities(&b, segments[0].Activities, total)
	} else {
		// Multi-segment: grouped with headers
		nonEmpty := 0
		for _, seg := range segments {
			if len(seg.Activities) > 0 {
				nonEmpty++
			}
		}

		segNum := 0
		for _, seg := range segments {
			if len(seg.Activities) == 0 {
				continue
			}
			segNum++

			if nonEmpty > 1 {
				fmt.Fprintf(&b, "### Segment %d\n", segNum)
				if seg.UserRequest != "" {
					fmt.Fprintf(&b, "> \"%s\"\n", seg.UserRequest)
				}
				b.WriteString("\n")
			}

			renderActivities(&b, seg.Activities, total)
			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

// renderActivities writes activity lines, with filtering for long sessions.
func renderActivities(b *strings.Builder, activities []Activity, totalSession int) {
	if totalSession > 50 {
		renderFilteredActivities(b, activities)
		return
	}

	for _, a := range activities {
		fmt.Fprintf(b, "- %s\n", a.Description)
	}
}

// renderFilteredActivities writes a filtered view for long sessions.
func renderFilteredActivities(b *strings.Builder, activities []Activity) {
	commandCount := 0
	commandMax := 5

	for _, a := range activities {
		// Always keep: file ops, tests, git ops, decisions, errors, plan mode, delegation
		switch a.Kind {
		case KindFileCreate, KindFileModify, KindTestRun, KindGitCommit, KindGitPush,
			KindDecision, KindPlanMode, KindDelegation, KindBuild:
			fmt.Fprintf(b, "- %s\n", a.Description)
			continue
		case KindError:
			fmt.Fprintf(b, "- %s\n", a.Description)
			continue
		case KindExplore:
			fmt.Fprintf(b, "- %s\n", a.Description)
			continue
		}

		if a.IsError {
			fmt.Fprintf(b, "- %s\n", a.Description)
			continue
		}

		// General commands: cap at commandMax
		if a.Kind == KindCommand {
			commandCount++
			if commandCount <= commandMax {
				fmt.Fprintf(b, "- %s\n", a.Description)
			}
		}
	}

	// Summary for omitted commands
	omitted := commandCount - commandMax
	if omitted > 0 {
		fmt.Fprintf(b, "- ... and %d more commands\n", omitted)
	}
}
