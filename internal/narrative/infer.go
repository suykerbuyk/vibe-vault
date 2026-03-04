// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package narrative

import (
	"fmt"
	"strings"

	"github.com/johns/vibe-vault/internal/transcript"
)

// Inference thresholds and limits.
const (
	maxTitleLen     = 80  // maximum title length before truncation
	truncatedTitleLen = 77  // truncated title length (leaves room for "...")
	maxThreads     = 5   // cap on open threads
	maxDecisions   = 5   // cap on extracted decisions
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
		if len(first) > maxTitleLen {
			first = first[:truncatedTitleLen] + "..."
		}
		if !IsNoiseMessage(first) {
			return first
		}
	}

	// Fall back to activity-derived title before generic "Session"
	if title := titleFromActivities(segments); title != "" {
		return title
	}

	return "Session"
}

// titleFromActivities derives a fallback title from activity patterns when user
// request and first message are both absent or noise.
func titleFromActivities(segments []Segment) string {
	var planModes, creates, modifies, reads, total int
	var firstFile string

	for _, seg := range segments {
		for _, a := range seg.Activities {
			total++
			switch a.Kind {
			case KindPlanMode:
				planModes++
			case KindFileCreate:
				creates++
				if firstFile == "" {
					firstFile = extractBacktickContent(a.Description)
				}
			case KindFileModify:
				modifies++
				if firstFile == "" {
					firstFile = extractBacktickContent(a.Description)
				}
			case KindExplore:
				reads++
			}
		}
	}

	if total == 0 {
		return ""
	}

	// Planning session: plan mode dominant, few writes
	if planModes > 0 && (creates+modifies) <= 2 {
		return "Planning session"
	}

	// File work: mention the first file touched
	if creates > 0 && firstFile != "" {
		return fmt.Sprintf("Work on `%s`", firstFile)
	}
	if modifies > 0 && firstFile != "" {
		return fmt.Sprintf("Work on `%s`", firstFile)
	}

	// Read-only: exploration
	if reads > 0 && creates == 0 && modifies == 0 {
		return "Codebase exploration"
	}

	return ""
}

// extractBacktickContent extracts content between backticks from a string.
// Returns the first backtick-enclosed content, or empty string if none.
func extractBacktickContent(s string) string {
	start := strings.IndexByte(s, '`')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(s[start+1:], '`')
	if end < 0 {
		return ""
	}
	return s[start+1 : start+1+end]
}

// inferSummary builds a semantic summary: "prefix: subject (outcomes)"
func inferSummary(segments []Segment, title string, commits []Commit) string {
	prefix := inferIntentPrefix(segments, commits)
	subject := inferSubject(title, commits)
	outcomes := formatOutcomes(segments)

	if subject == "" && outcomes == "" {
		return "Claude Code session."
	}

	var b strings.Builder

	if prefix != "" && subject != "" {
		b.WriteString(prefix)
		b.WriteString(": ")
		b.WriteString(subject)
	} else if subject != "" {
		b.WriteString(subject)
	} else if prefix != "" {
		b.WriteString(prefix)
	}

	if outcomes != "" {
		if b.Len() > 0 {
			b.WriteString(" (")
			b.WriteString(outcomes)
			b.WriteString(")")
		} else {
			b.WriteString(outcomes)
		}
	}

	return b.String()
}

// inferIntentPrefix determines the conventional commit prefix from commits or activity patterns.
func inferIntentPrefix(segments []Segment, commits []Commit) string {
	// Priority 1: conventional prefix from last commit message (the deliverable)
	if len(commits) > 0 {
		msg := commits[len(commits)-1].Message
		if p := extractConventionalPrefix(msg); p != "" {
			return p
		}
	}

	// Priority 2: derive from activity patterns
	var writes, tests, errors, planModes, reads int
	for _, seg := range segments {
		for _, a := range seg.Activities {
			switch a.Kind {
			case KindFileCreate, KindFileModify:
				writes++
			case KindTestRun:
				tests++
			case KindError:
				errors++
			case KindPlanMode:
				planModes++
			case KindExplore:
				reads++
			}
			if a.IsError && !a.Recovered {
				errors++
			}
		}
	}

	total := 0
	for _, seg := range segments {
		total += len(seg.Activities)
	}
	if total == 0 {
		return ""
	}

	// Plan mode dominant
	if planModes > 0 && writes <= 2 {
		return "plan"
	}
	// Errors + writes = debugging/fix
	if errors > 0 && writes > 0 && tests > 0 {
		return "fix"
	}
	// Writes + tests = implementation
	if writes > 0 && tests > 0 {
		return "feat"
	}
	// Writes without tests
	if writes > 0 {
		return "feat"
	}
	// Reads dominant
	if reads > 0 && writes == 0 {
		return "explore"
	}

	return ""
}

// extractConventionalPrefix extracts a conventional commit prefix from a message.
func extractConventionalPrefix(msg string) string {
	prefixes := []string{"feat", "fix", "refactor", "docs", "test", "chore", "style", "perf", "ci", "build"}
	lower := strings.ToLower(msg)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p+":") || strings.HasPrefix(lower, p+"(") {
			return p
		}
	}
	return ""
}

// inferSubject determines the best subject for the summary.
func inferSubject(title string, commits []Commit) string {
	// Priority 1: last commit message body (stripped of prefix) — the deliverable
	if len(commits) > 0 {
		msg := commits[len(commits)-1].Message
		subject := stripConventionalPrefix(msg)
		if subject != "" {
			return truncateStr(subject, maxTitleLen)
		}
	}

	// Priority 2: title from user request
	if title != "" && title != "Session" {
		return truncateStr(title, maxTitleLen)
	}

	return ""
}

// stripConventionalPrefix removes "feat: ", "fix(scope): " etc. from a commit message.
func stripConventionalPrefix(msg string) string {
	// Check for "prefix: body" or "prefix(scope): body"
	idx := strings.Index(msg, ": ")
	if idx > 0 && idx < 30 {
		prefix := strings.ToLower(msg[:idx])
		// Validate it looks like a conventional prefix (possibly with scope)
		base := prefix
		if p := strings.IndexByte(base, '('); p > 0 {
			base = base[:p]
		}
		conventionals := []string{"feat", "fix", "refactor", "docs", "test", "chore", "style", "perf", "ci", "build"}
		for _, c := range conventionals {
			if base == c {
				return strings.TrimSpace(msg[idx+2:])
			}
		}
	}
	return msg
}

// formatOutcomes builds a condensed parenthetical outcomes string.
func formatOutcomes(segments []Segment) string {
	var created, modified, testRuns, testFails, commits, pushes, recoveries int

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
			if a.Recovered {
				recoveries++
			}
		}
	}

	var parts []string

	// File changes: "3+12 files" or "3 new" or "12 modified"
	if created > 0 && modified > 0 {
		parts = append(parts, fmt.Sprintf("%d+%d files", created, modified))
	} else if created > 0 {
		parts = append(parts, fmt.Sprintf("%d files", created))
	} else if modified > 0 {
		parts = append(parts, fmt.Sprintf("%d files", modified))
	}

	// Test results
	if testRuns > 0 {
		if testFails == 0 {
			parts = append(parts, "tests pass")
		} else if testFails == testRuns {
			parts = append(parts, "tests fail")
		} else {
			parts = append(parts, "mixed tests")
		}
	}

	// Git operations
	if commits > 0 && pushes > 0 {
		parts = append(parts, "pushed")
	} else if commits > 0 {
		parts = append(parts, "committed")
	}

	// Error recoveries
	if recoveries > 0 {
		parts = append(parts, fmt.Sprintf("resolved %d errors", recoveries))
	}

	return strings.Join(parts, ", ")
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

	if len(threads) > maxThreads {
		threads = threads[:maxThreads]
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

	if len(decisions) > maxDecisions {
		decisions = decisions[:maxDecisions]
	}

	return decisions
}
