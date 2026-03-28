// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package inject

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/mdutil"
	"github.com/johns/vibe-vault/internal/trends"
)

// Section names in priority order (truncation drops from the end).
const (
	SectionSummary   = "summary"
	SectionSessions  = "sessions"
	SectionThreads   = "threads"
	SectionDecisions = "decisions"
	SectionFriction  = "friction"
)

// AllSections lists sections in priority order — truncation drops from the end.
var AllSections = []string{
	SectionSummary,
	SectionSessions,
	SectionThreads,
	SectionDecisions,
	SectionFriction,
}

// Inject configuration constants.
const (
	recentSessionCap  = 5     // max recent sessions in output
	decisionDaysWindow = 30   // days to look back for decisions
	decisionCap       = 10    // max decisions in output
	threadCap         = 10    // max open threads in output
	defaultMaxTokens  = 2000  // default token budget for output
)

// Opts configures the inject build.
type Opts struct {
	Project   string
	Format    string   // "md" or "json" (default "md")
	Sections  []string // nil = all sections
	MaxTokens int      // default 2000
}

// SessionItem is a condensed session for injection output.
type SessionItem struct {
	Date    string `json:"date"`
	Title   string `json:"title"`
	Tag     string `json:"tag,omitempty"`
	Summary string `json:"summary,omitempty"`
	Source  string `json:"source,omitempty"`
}

// FrictionSummary is a one-line friction overview.
type FrictionSummary struct {
	Direction string  `json:"direction"`
	Average   float64 `json:"average"`
}

// Result is the structured inject output.
type Result struct {
	Project   string          `json:"project"`
	Summary   string          `json:"summary,omitempty"`
	Sessions  []SessionItem   `json:"sessions,omitempty"`
	Threads   []string        `json:"threads,omitempty"`
	Decisions []string        `json:"decisions,omitempty"`
	Friction  *FrictionSummary `json:"friction,omitempty"`
}

// Build assembles a Result from index entries and trend data.
func Build(entries map[string]index.SessionEntry, trendResult trends.Result, opts Opts) Result {
	r := Result{Project: opts.Project}

	projectEntries := projectEntriesByDateDesc(entries, opts.Project)

	if len(projectEntries) > 0 {
		r.Summary = projectEntries[0].Summary
	}

	// Sessions: last N, newest first (already sorted newest first)
	cap := recentSessionCap
	if len(projectEntries) < cap {
		cap = len(projectEntries)
	}
	for _, e := range projectEntries[:cap] {
		item := SessionItem{
			Date:    e.Date,
			Title:   e.Title,
			Tag:     e.Tag,
			Summary: e.Summary,
		}
		if src := e.SourceName(); src != "claude-code" {
			item.Source = src
		}
		r.Sessions = append(r.Sessions, item)
	}

	// Open threads from recent sessions, resolved filtered out
	r.Threads = openThreads(projectEntries, recentSessionCap)

	// Decisions from recent window, deduped
	r.Decisions = recentDecisions(projectEntries, decisionDaysWindow)

	// Friction from trends
	r.Friction = frictionFromTrends(trendResult)

	return r
}

// FormatMarkdown renders the result as markdown, filtered by sections.
func FormatMarkdown(r Result, sections []string) string {
	if len(sections) == 0 {
		sections = AllSections
	}
	sectionSet := make(map[string]bool, len(sections))
	for _, s := range sections {
		sectionSet[s] = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Context: %s\n", r.Project)

	if sectionSet[SectionSummary] && r.Summary != "" {
		fmt.Fprintf(&b, "\n## Summary\n\n%s\n", r.Summary)
	}

	if sectionSet[SectionSessions] && len(r.Sessions) > 0 {
		b.WriteString("\n## Recent Sessions\n\n")
		for _, s := range r.Sessions {
			line := fmt.Sprintf("- **%s** %s", s.Date, s.Title)
			if s.Source != "" {
				line += fmt.Sprintf(" [%s]", s.Source)
			}
			if s.Tag != "" {
				line += fmt.Sprintf(" #%s", s.Tag)
			}
			if s.Summary != "" {
				line += fmt.Sprintf(" — %s", s.Summary)
			}
			b.WriteString(line + "\n")
		}
	}

	if sectionSet[SectionThreads] && len(r.Threads) > 0 {
		b.WriteString("\n## Open Threads\n\n")
		for _, t := range r.Threads {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}

	if sectionSet[SectionDecisions] && len(r.Decisions) > 0 {
		b.WriteString("\n## Decisions\n\n")
		for _, d := range r.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}

	if sectionSet[SectionFriction] && r.Friction != nil {
		fmt.Fprintf(&b, "\n## Friction\n\nTrend: %s (avg %.1f)\n",
			r.Friction.Direction, r.Friction.Average)
	}

	return b.String()
}

// FormatJSON renders the result as JSON, filtered by sections.
func FormatJSON(r Result, sections []string) (string, error) {
	filtered := filterResult(r, sections)
	data, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(data) + "\n", nil
}

// Render formats the result and applies token-budget truncation.
func Render(r Result, opts Opts) (string, error) {
	format := opts.Format
	if format == "" {
		format = "md"
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	sections := opts.Sections
	if len(sections) == 0 {
		sections = AllSections
	}

	// Render and truncate loop: drop lowest-priority section until within budget
	for len(sections) > 0 {
		var output string
		var err error
		switch format {
		case "json":
			output, err = FormatJSON(r, sections)
		default:
			output = FormatMarkdown(r, sections)
		}
		if err != nil {
			return "", err
		}

		if estimateTokens(output) <= maxTokens || len(sections) <= 1 {
			return output, nil
		}

		// Drop lowest-priority section (last in slice)
		sections = sections[:len(sections)-1]
	}

	// Should not reach here, but return empty context header
	return fmt.Sprintf("# Context: %s\n", r.Project), nil
}

// estimateTokens approximates token count as word count × 1.3.
func estimateTokens(text string) int {
	words := len(strings.Fields(text))
	return int(float64(words) * 1.3)
}

// --- unexported helpers ---

func projectEntriesByDateDesc(entries map[string]index.SessionEntry, project string) []index.SessionEntry {
	var result []index.SessionEntry
	for _, e := range entries {
		if e.Project == project {
			result = append(result, e)
		}
	}
	// Sort newest first (by date desc, then iteration desc)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Date != result[j].Date {
			return result[i].Date > result[j].Date
		}
		return result[i].Iteration > result[j].Iteration
	})
	return result
}

func recentDecisions(entries []index.SessionEntry, days int) []string {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	seen := make(map[string]bool)
	var decisions []string
	for _, e := range entries {
		if e.Date < cutoff {
			continue
		}
		for _, d := range e.Decisions {
			if !seen[d] {
				seen[d] = true
				decisions = append(decisions, d)
			}
		}
	}

	if len(decisions) < decisionCap {
		return decisions
	}
	return decisions[:decisionCap]
}

func openThreads(entries []index.SessionEntry, n int) []string {
	// entries are already sorted newest first
	cap := n
	if len(entries) < cap {
		cap = len(entries)
	}
	recent := entries[:cap]

	// Collect all decisions for resolution checking
	var allDecisions []string
	for _, e := range entries {
		allDecisions = append(allDecisions, e.Decisions...)
	}

	seen := make(map[string]bool)
	var threads []string

	for _, e := range recent {
		for _, t := range e.OpenThreads {
			if seen[t] {
				continue
			}
			seen[t] = true
			if isResolvedByDecisions(t, allDecisions) {
				continue
			}
			threads = append(threads, t)
		}
	}

	if len(threads) < threadCap {
		return threads
	}
	return threads[:threadCap]
}

func frictionFromTrends(r trends.Result) *FrictionSummary {
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			return &FrictionSummary{
				Direction: m.Direction,
				Average:   m.OverallAvg,
			}
		}
	}
	return nil
}

// isResolvedByDecisions checks if a thread has significant word overlap with any decision.
func isResolvedByDecisions(thread string, decisions []string) bool {
	threadWords := mdutil.SignificantWords(thread)
	if len(threadWords) == 0 {
		return false
	}
	for _, d := range decisions {
		decisionWords := mdutil.SignificantWords(d)
		if mdutil.Overlap(threadWords, decisionWords) >= 2 {
			return true
		}
	}
	return false
}


// filterResult returns a copy of Result with only the requested sections populated.
func filterResult(r Result, sections []string) Result {
	if len(sections) == 0 {
		return r
	}
	sectionSet := make(map[string]bool, len(sections))
	for _, s := range sections {
		sectionSet[s] = true
	}

	filtered := Result{Project: r.Project}
	if sectionSet[SectionSummary] {
		filtered.Summary = r.Summary
	}
	if sectionSet[SectionSessions] {
		filtered.Sessions = r.Sessions
	}
	if sectionSet[SectionThreads] {
		filtered.Threads = r.Threads
	}
	if sectionSet[SectionDecisions] {
		filtered.Decisions = r.Decisions
	}
	if sectionSet[SectionFriction] {
		filtered.Friction = r.Friction
	}
	return filtered
}
