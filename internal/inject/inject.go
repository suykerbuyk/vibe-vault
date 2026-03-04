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
	"github.com/johns/vibe-vault/internal/trends"
)

// Section names in priority order (truncation drops from the end).
const (
	SectionSummary   = "summary"
	SectionSessions  = "sessions"
	SectionThreads   = "threads"
	SectionDecisions = "decisions"
	SectionFriction  = "friction"
	SectionKnowledge = "knowledge"
)

// AllSections lists sections in priority order — truncation drops from the end.
var AllSections = []string{
	SectionSummary,
	SectionSessions,
	SectionThreads,
	SectionDecisions,
	SectionFriction,
	SectionKnowledge,
}

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
}

// FrictionSummary is a one-line friction overview.
type FrictionSummary struct {
	Direction string  `json:"direction"`
	Average   float64 `json:"average"`
}

// KnowledgeItem is a condensed knowledge note for injection output.
type KnowledgeItem struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Date    string `json:"date"`
}

// Result is the structured inject output.
type Result struct {
	Project   string          `json:"project"`
	Summary   string          `json:"summary,omitempty"`
	Sessions  []SessionItem   `json:"sessions,omitempty"`
	Threads   []string        `json:"threads,omitempty"`
	Decisions []string        `json:"decisions,omitempty"`
	Friction  *FrictionSummary `json:"friction,omitempty"`
	Knowledge []KnowledgeItem `json:"knowledge,omitempty"`
}

// Build assembles a Result from index entries, knowledge summaries, and trend data.
func Build(entries map[string]index.SessionEntry, knowledge []index.KnowledgeSummary, trendResult trends.Result, opts Opts) Result {
	r := Result{Project: opts.Project}

	projectEntries := projectEntriesByDateDesc(entries, opts.Project)

	if len(projectEntries) > 0 {
		r.Summary = projectEntries[0].Summary
	}

	// Sessions: last 5, newest first (already sorted newest first)
	cap := 5
	if len(projectEntries) < cap {
		cap = len(projectEntries)
	}
	for _, e := range projectEntries[:cap] {
		r.Sessions = append(r.Sessions, SessionItem{
			Date:    e.Date,
			Title:   e.Title,
			Tag:     e.Tag,
			Summary: e.Summary,
		})
	}

	// Open threads from last 5 sessions, resolved filtered out
	r.Threads = openThreads(projectEntries, 5)

	// Decisions from last 30 days, deduped
	r.Decisions = recentDecisions(projectEntries, 30)

	// Friction from trends
	r.Friction = frictionFromTrends(trendResult)

	// Knowledge
	r.Knowledge = relevantKnowledge(knowledge, opts.Project, 5)

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
	b.WriteString(fmt.Sprintf("# Context: %s\n", r.Project))

	if sectionSet[SectionSummary] && r.Summary != "" {
		b.WriteString(fmt.Sprintf("\n## Summary\n\n%s\n", r.Summary))
	}

	if sectionSet[SectionSessions] && len(r.Sessions) > 0 {
		b.WriteString("\n## Recent Sessions\n\n")
		for _, s := range r.Sessions {
			line := fmt.Sprintf("- **%s** %s", s.Date, s.Title)
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
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
	}

	if sectionSet[SectionDecisions] && len(r.Decisions) > 0 {
		b.WriteString("\n## Decisions\n\n")
		for _, d := range r.Decisions {
			b.WriteString(fmt.Sprintf("- %s\n", d))
		}
	}

	if sectionSet[SectionFriction] && r.Friction != nil {
		b.WriteString(fmt.Sprintf("\n## Friction\n\nTrend: %s (avg %.1f)\n",
			r.Friction.Direction, r.Friction.Average))
	}

	if sectionSet[SectionKnowledge] && len(r.Knowledge) > 0 {
		b.WriteString("\n## Knowledge\n\n")
		for _, k := range r.Knowledge {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", k.Type, k.Title, k.Summary))
		}
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
		maxTokens = 2000
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

	cap := 10
	if len(decisions) < cap {
		cap = len(decisions)
	}
	return decisions[:cap]
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

	threadCap := 10
	if len(threads) < threadCap {
		threadCap = len(threads)
	}
	return threads[:threadCap]
}

func frictionFromTrends(r trends.Result) *FrictionSummary {
	for _, m := range r.Metrics {
		if m.Name == "Friction" {
			return &FrictionSummary{
				Direction: m.Direction,
				Average:   m.OverallAvg,
			}
		}
	}
	return nil
}

func relevantKnowledge(summaries []index.KnowledgeSummary, project string, n int) []KnowledgeItem {
	var filtered []index.KnowledgeSummary
	for _, k := range summaries {
		if k.Project == project || k.Project == "" {
			filtered = append(filtered, k)
		}
	}

	// Sort by date descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Date > filtered[j].Date
	})

	cap := n
	if len(filtered) < cap {
		cap = len(filtered)
	}

	var items []KnowledgeItem
	for _, k := range filtered[:cap] {
		items = append(items, KnowledgeItem{
			Type:    k.Type,
			Title:   k.Title,
			Summary: k.Summary,
			Date:    k.Date,
		})
	}
	return items
}

// isResolvedByDecisions checks if a thread has significant word overlap with any decision.
func isResolvedByDecisions(thread string, decisions []string) bool {
	threadWords := significantWords(thread)
	if len(threadWords) == 0 {
		return false
	}
	for _, d := range decisions {
		decisionWords := significantWords(d)
		overlap := wordOverlap(threadWords, decisionWords)
		if overlap >= 2 {
			return true
		}
	}
	return false
}

// significantWords extracts words ≥4 chars, excluding stop words.
func significantWords(s string) []string {
	words := strings.Fields(strings.ToLower(s))
	var result []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'`()[]{}—-")
		if len(w) >= 4 && !stopWords[w] {
			result = append(result, w)
		}
	}
	return result
}

var stopWords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true,
	"have": true, "been": true, "were": true, "will": true,
	"would": true, "could": true, "should": true, "what": true,
	"when": true, "where": true, "which": true, "their": true,
	"there": true, "these": true, "those": true, "them": true,
	"then": true, "than": true, "some": true, "also": true,
	"into": true, "each": true, "make": true, "like": true,
	"just": true, "over": true, "such": true, "only": true,
	"very": true, "more": true, "most": true, "other": true,
	"about": true, "after": true, "before": true, "being": true,
	"between": true, "does": true, "doing": true, "done": true,
}

func wordOverlap(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, w := range a {
		set[w] = true
	}
	count := 0
	for _, w := range b {
		if set[w] {
			count++
		}
	}
	return count
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
	if sectionSet[SectionKnowledge] {
		filtered.Knowledge = r.Knowledge
	}
	return filtered
}
