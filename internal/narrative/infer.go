package narrative

import (
	"fmt"
	"strings"

	"github.com/johns/vibe-vault/internal/transcript"
)

// inferTitle picks the best session title from segment user requests and transcript.
func inferTitle(segments []Segment, t *transcript.Transcript) string {
	// Walk segments, use first meaningful user request
	for _, seg := range segments {
		if seg.UserRequest != "" {
			return seg.UserRequest
		}
	}

	// Fall back to transcript first user message
	first := transcript.FirstUserMessage(t)
	if first != "" {
		first = strings.TrimSpace(first)
		if idx := strings.IndexByte(first, '\n'); idx > 0 {
			first = first[:idx]
		}
		if len(first) > 80 {
			first = first[:77] + "..."
		}
		if !IsNoiseMessage(first) {
			return first
		}
	}

	return "Session"
}

// inferSummary builds a multi-sentence summary from activity statistics.
func inferSummary(segments []Segment) string {
	var created, modified, testRuns, testFails, commits, pushes, errors, recoveries int

	for _, seg := range segments {
		for _, a := range seg.Activities {
			switch a.Kind {
			case KindFileCreate:
				created++
			case KindFileModify:
				modified++
			case KindTestRun:
				testRuns++
				if a.IsError {
					testFails++
				}
			case KindGitCommit:
				commits++
			case KindGitPush:
				pushes++
			}
			if a.IsError {
				errors++
			}
			if a.Recovered {
				recoveries++
			}
		}
	}

	var sentences []string

	// File changes
	if created > 0 && modified > 0 {
		sentences = append(sentences, fmt.Sprintf("Created %d and modified %d files.", created, modified))
	} else if created > 0 {
		sentences = append(sentences, fmt.Sprintf("Created %d files.", created))
	} else if modified > 0 {
		sentences = append(sentences, fmt.Sprintf("Modified %d files.", modified))
	}

	// Test results
	if testRuns > 0 {
		if testFails == 0 {
			sentences = append(sentences, "All tests passed.")
		} else if testFails == testRuns {
			sentences = append(sentences, "Tests failed.")
		} else {
			sentences = append(sentences, "Tests had mixed results.")
		}
	}

	// Git operations
	if commits > 0 && pushes > 0 {
		sentences = append(sentences, "Changes committed and pushed.")
	} else if commits > 0 {
		sentences = append(sentences, "Changes committed.")
	}

	// Error recoveries
	if recoveries > 0 {
		sentences = append(sentences, fmt.Sprintf("Resolved %d errors.", recoveries))
	}

	if len(sentences) == 0 {
		// Count total activities
		total := 0
		for _, seg := range segments {
			total += len(seg.Activities)
		}
		if total > 0 {
			sentences = append(sentences, fmt.Sprintf("Performed %d actions.", total))
		} else {
			return "Claude Code session."
		}
	}

	return strings.Join(sentences, " ")
}

// inferTag classifies the session based on activity patterns.
func inferTag(segments []Segment) string {
	var writes, reads, tests, errors, planModes, explores int

	for _, seg := range segments {
		for _, a := range seg.Activities {
			switch a.Kind {
			case KindFileCreate, KindFileModify:
				writes++
			case KindExplore:
				reads++
			case KindTestRun:
				tests++
			case KindError:
				errors++
			case KindPlanMode:
				planModes++
			}
			if a.IsError && a.Recovered {
				errors++
			}
		}
		explores += reads
	}

	total := 0
	for _, seg := range segments {
		total += len(seg.Activities)
	}

	if total == 0 {
		return ""
	}

	// Plan mode dominant with few writes
	if planModes > 0 && writes <= 2 {
		return "planning"
	}

	// Heavy writes + tests = implementation
	if writes > 0 && tests > 0 {
		return "implementation"
	}

	// Error patterns + fixes = debugging
	if errors > 0 && writes > 0 {
		return "debugging"
	}

	// Writes without tests
	if writes > 0 {
		return "implementation"
	}

	// Many reads, zero writes
	if reads > 0 && writes == 0 {
		if total <= 5 {
			return "research"
		}
		return "exploration"
	}

	return ""
}

// inferOpenThreads extracts unresolved errors from activities.
func inferOpenThreads(segments []Segment) []string {
	var threads []string

	for _, seg := range segments {
		for _, a := range seg.Activities {
			if a.IsError && !a.Recovered {
				detail := a.Description
				if a.Detail != "" {
					detail = a.Detail
				}
				threads = append(threads, truncateStr(detail, 120))
			}
		}
	}

	// Cap at 5
	if len(threads) > 5 {
		threads = threads[:5]
	}

	return threads
}

// extractDecisions pairs AskUserQuestion activities with context.
func extractDecisions(segments []Segment, entries []transcript.Entry) []string {
	var decisions []string

	for _, seg := range segments {
		for _, a := range seg.Activities {
			if a.Kind == KindDecision && a.Detail != "" {
				decisions = append(decisions, truncateStr(a.Detail, 120))
			}
		}
	}

	// Cap at 5
	if len(decisions) > 5 {
		decisions = decisions[:5]
	}

	return decisions
}
