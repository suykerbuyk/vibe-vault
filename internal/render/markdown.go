// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package render

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/narrative"
	"github.com/suykerbuyk/vibe-vault/internal/sanitize"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// SourceFallbackSummary returns the generic session fallback for a given source.
func SourceFallbackSummary(source string) string {
	if source == "zed" {
		return "Zed agent session"
	}
	return "Claude Code session"
}

// RelatedNote holds a related session link and the reason for the relation.
type RelatedNote struct {
	Name   string // wikilink target, e.g. "2026-02-25-01"
	Reason string // human-readable, e.g. "3 shared files, branch: feature/auth"
}

// NoteData holds everything needed to render a session note.
type NoteData struct {
	Date            string // YYYY-MM-DD
	Project         string
	Branch          string
	Domain          string
	Model           string
	SessionID       string
	Iteration       int
	Duration        int // minutes
	Messages        int // user + assistant
	InputTokens     int
	OutputTokens    int
	Title           string
	Summary         string
	PreviousNote    string // wikilink target, e.g. "2026-02-21-03"
	FilesChanged    []string
	Decisions       []string
	OpenThreads     []string
	EnrichedBy      string // model name, e.g. "grok-3-mini-fast"
	Tag             string // activity tag, e.g. "implementation"
	RelatedNotes    []RelatedNote
	ToolCounts      map[string]int
	TotalTools      int
	Status          string             // "completed" or "checkpoint"
	Commits         []narrative.Commit // Git commits extracted from tool output
	WorkPerformed   string             // Rendered markdown for Work Performed section
	ProseDialogue   string             // Rendered prose section (empty = use summary fallback)
	FrictionScore   int                // Composite friction score 0-100
	Corrections     int                // Count of detected user corrections
	FrictionSignals []string           // Human-readable friction signal descriptions

	// Phase 4: Extended data fields
	ThinkingBlocks      int      // count of thinking blocks
	CognitiveComplexity string   // low/medium/high based on thinking tokens
	ReasoningHighlights []string // extracted reasoning bullet points
	AvgTurnMs           int      // average turn duration in ms
	MaxTurnMs           int      // maximum turn duration in ms
	SessionName         string   // session slug/name
	CCVersion           string   // Claude Code version
	AllBranches         []string // all observed git branches
	AutoCompactions     int      // auto-compaction count
	Timeline            string   // rendered timeline section
	EstimatedCostUSD    float64  // estimated session cost in USD
	ToolEffectiveness   string   // rendered tool effectiveness section (empty = skip)
	ParentSession       string   // parent entry UUID (non-empty = /continue session)
	SessionTags         []string // pre-built tag list (e.g. ["vv-session", "implementation"])
	Source              string   // source identifier ("zed", etc.; empty = claude-code)
	Host                string   // hostname captured at write time (empty = resolver failed)
	User                string   // acting user captured at write time (empty = resolver failed)
	CWD                 string   // cwd captured at write time, home-compressed + sanitized (empty = resolver failed or inside vault)
	OriginProject       string   // project resolved from stamp cwd (empty = DetectProject saw no cwd)
}

// SessionNote renders a full Obsidian markdown note from NoteData.
func SessionNote(d NoteData) string {
	var b strings.Builder

	// Frontmatter
	b.WriteString("---\n")
	fmt.Fprintf(&b, "date: %s\n", d.Date)
	b.WriteString("type: session\n")
	fmt.Fprintf(&b, "project: %s\n", d.Project)
	if d.Branch != "" {
		fmt.Fprintf(&b, "branch: %s\n", d.Branch)
	}
	fmt.Fprintf(&b, "domain: %s\n", d.Domain)
	if d.Model != "" {
		fmt.Fprintf(&b, "model: %s\n", d.Model)
	}
	if d.Source != "" {
		fmt.Fprintf(&b, "source: %s\n", d.Source)
	}
	fmt.Fprintf(&b, "session_id: \"%s\"\n", d.SessionID)
	fmt.Fprintf(&b, "iteration: %d\n", d.Iteration)
	fmt.Fprintf(&b, "duration_minutes: %d\n", d.Duration)
	fmt.Fprintf(&b, "messages: %d\n", d.Messages)
	fmt.Fprintf(&b, "tokens_in: %d\n", d.InputTokens)
	fmt.Fprintf(&b, "tokens_out: %d\n", d.OutputTokens)
	if d.TotalTools > 0 {
		fmt.Fprintf(&b, "tool_uses: %d\n", d.TotalTools)
		var toolNames []string
		for name := range d.ToolCounts {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		fmt.Fprintf(&b, "tools: [%s]\n", strings.Join(toolNames, ", "))
	}
	status := d.Status
	if status == "" {
		status = "completed"
	}
	fmt.Fprintf(&b, "status: %s\n", status)
	if len(d.Commits) > 0 {
		var shas []string
		for _, c := range d.Commits {
			shas = append(shas, c.SHA)
		}
		fmt.Fprintf(&b, "commits: [%s]\n", strings.Join(shas, ", "))
	}
	if d.FrictionScore > 0 {
		fmt.Fprintf(&b, "friction_score: %d\n", d.FrictionScore)
	}
	if d.Corrections > 0 {
		fmt.Fprintf(&b, "corrections: %d\n", d.Corrections)
	}
	if d.ThinkingBlocks > 0 {
		fmt.Fprintf(&b, "thinking_blocks: %d\n", d.ThinkingBlocks)
	}
	if d.CognitiveComplexity != "" {
		fmt.Fprintf(&b, "cognitive_complexity: %s\n", d.CognitiveComplexity)
	}
	if d.AvgTurnMs > 0 {
		fmt.Fprintf(&b, "avg_turn_ms: %d\n", d.AvgTurnMs)
	}
	if d.MaxTurnMs > 0 {
		fmt.Fprintf(&b, "max_turn_ms: %d\n", d.MaxTurnMs)
	}
	if d.SessionName != "" {
		fmt.Fprintf(&b, "session_name: \"%s\"\n", escapeYAML(d.SessionName))
	}
	if d.CCVersion != "" {
		fmt.Fprintf(&b, "claude_code_version: \"%s\"\n", d.CCVersion)
	}
	if len(d.AllBranches) > 1 {
		fmt.Fprintf(&b, "branches: [%s]\n", strings.Join(d.AllBranches, ", "))
	}
	if d.AutoCompactions > 0 {
		fmt.Fprintf(&b, "auto_compactions: %d\n", d.AutoCompactions)
	}
	if d.EstimatedCostUSD > 0 {
		fmt.Fprintf(&b, "estimated_cost_usd: %.2f\n", d.EstimatedCostUSD)
	}
	if len(d.SessionTags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(d.SessionTags, ", "))
	} else if d.Tag != "" {
		fmt.Fprintf(&b, "tags: [vv-session, %s]\n", d.Tag)
	} else {
		b.WriteString("tags: [vv-session]\n")
	}
	if d.Host != "" {
		fmt.Fprintf(&b, "host: %s\n", d.Host)
	}
	if d.User != "" {
		fmt.Fprintf(&b, "user: %s\n", d.User)
	}
	if d.CWD != "" {
		fmt.Fprintf(&b, "cwd: %s\n", d.CWD)
	}
	if d.OriginProject != "" {
		fmt.Fprintf(&b, "origin_project: %s\n", d.OriginProject)
	}
	fmt.Fprintf(&b, "summary: \"%s\"\n", escapeYAML(d.Summary))
	if d.PreviousNote != "" {
		fmt.Fprintf(&b, "previous: \"[[%s]]\"\n", d.PreviousNote)
	}
	if d.ParentSession != "" {
		fmt.Fprintf(&b, "parent_session: \"%s\"\n", d.ParentSession)
	}
	if len(d.RelatedNotes) > 0 {
		b.WriteString("related: [")
		for i, r := range d.RelatedNotes {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "\"[[%s]]\"", r.Name)
		}
		b.WriteString("]\n")
	}
	b.WriteString("---\n\n")

	// Title
	fmt.Fprintf(&b, "# %s\n\n", d.Title)

	// Session Dialogue / What Happened
	if d.ProseDialogue != "" {
		b.WriteString("## Session Dialogue\n\n")
		b.WriteString(d.ProseDialogue)
		b.WriteString("\n")
	} else {
		b.WriteString("## What Happened\n\n")
		fmt.Fprintf(&b, "%s\n\n", d.Summary)
	}

	// What Changed
	if len(d.FilesChanged) > 0 {
		b.WriteString("## What Changed\n\n")
		for _, f := range d.FilesChanged {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}

	// Commits
	if len(d.Commits) > 0 {
		b.WriteString("## Commits\n\n")
		for _, c := range d.Commits {
			fmt.Fprintf(&b, "- `%s` %s\n", c.SHA, c.Message)
		}
		b.WriteString("\n")
	}

	// Work Performed
	if d.WorkPerformed != "" {
		b.WriteString("## Work Performed\n\n")
		b.WriteString(d.WorkPerformed)
		b.WriteString("\n")
	}

	// Tool Usage
	if d.TotalTools > 0 {
		b.WriteString("## Tool Usage\n\n")
		fmt.Fprintf(&b, "**Total: %d tool calls**\n\n", d.TotalTools)
		b.WriteString("| Tool | Count |\n")
		b.WriteString("|------|-------|\n")
		var toolNames []string
		for name := range d.ToolCounts {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		for _, name := range toolNames {
			fmt.Fprintf(&b, "| %s | %d |\n", name, d.ToolCounts[name])
		}
		b.WriteString("\n")
	}

	// Tool Effectiveness (Task 20 — only rendered when interesting patterns found)
	if d.ToolEffectiveness != "" {
		b.WriteString("## Tool Effectiveness\n\n")
		b.WriteString(d.ToolEffectiveness)
	}

	// Key Decisions
	if len(d.Decisions) > 0 {
		b.WriteString("## Key Decisions\n\n")
		for _, d := range d.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		b.WriteString("\n")
	}

	// Open Threads
	if len(d.OpenThreads) > 0 {
		b.WriteString("## Open Threads\n\n")
		for _, t := range d.OpenThreads {
			fmt.Fprintf(&b, "- [ ] %s\n", t)
		}
		b.WriteString("\n")
	}

	// Reasoning Highlights (Task 15)
	if len(d.ReasoningHighlights) > 0 {
		b.WriteString("## Reasoning Highlights\n\n")
		for _, rh := range d.ReasoningHighlights {
			fmt.Fprintf(&b, "- %s\n", rh)
		}
		b.WriteString("\n")
	}

	// Timeline (Task 21)
	if d.Timeline != "" {
		b.WriteString("## Timeline\n\n")
		b.WriteString(d.Timeline)
		b.WriteString("\n")
	}

	// Friction Signals
	if d.FrictionScore >= 15 && len(d.FrictionSignals) > 0 {
		b.WriteString("## Friction Signals\n\n")
		fmt.Fprintf(&b, "**Friction score: %d/100**\n\n", d.FrictionScore)
		for _, sig := range d.FrictionSignals {
			fmt.Fprintf(&b, "- %s\n", sig)
		}
		b.WriteString("\n")
	}

	// Related Sessions
	if len(d.RelatedNotes) > 0 {
		b.WriteString("## Related Sessions\n\n")
		for _, r := range d.RelatedNotes {
			fmt.Fprintf(&b, "- [[%s]] — %s\n", r.Name, r.Reason)
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("---\n")
	if d.EnrichedBy != "" {
		fmt.Fprintf(&b, "*vv v0.1.0 | enriched by %s*\n", d.EnrichedBy)
	} else {
		b.WriteString("*vv v0.1.0*\n")
	}

	return b.String()
}

// NoteDataFromTranscript builds NoteData from parsed transcript data and session metadata.
func NoteDataFromTranscript(t *transcript.Transcript, project, domain, branch, sessionID string, iteration int, previous string) NoteData {
	s := t.Stats

	date := s.StartTime.Format("2006-01-02")
	if date == "0001-01-01" {
		date = time.Now().Format("2006-01-02")
	}

	firstMsg := transcript.FirstUserMessage(t)
	title := titleFromFirstMessage(firstMsg)
	summary := title
	if summary == "Session" {
		summary = "Claude Code session" // may be overridden by SourceFallbackSummary
	}

	// Total input tokens = direct + cache reads + cache writes
	totalInput := s.InputTokens + s.CacheReads + s.CacheWrites

	// Collect changed files (written/edited), strip common prefix
	var filesChanged []string
	for f := range s.FilesWritten {
		filesChanged = append(filesChanged, shortenPath(f, s.CWD))
	}
	sort.Strings(filesChanged)

	// Cognitive complexity based on thinking tokens relative to output tokens (Task 15)
	var cogComplexity string
	if s.ThinkingTokens > 0 {
		ratio := float64(s.ThinkingTokens) / float64(max(s.OutputTokens, 1))
		switch {
		case ratio > 2.0:
			cogComplexity = "high"
		case ratio > 0.5:
			cogComplexity = "medium"
		default:
			cogComplexity = "low"
		}
	}

	return NoteData{
		Date:                date,
		Project:             project,
		Branch:              branch,
		Domain:              domain,
		Model:               s.Model,
		SessionID:           sessionID,
		Iteration:           iteration,
		Duration:            int(s.Duration.Minutes()),
		Messages:            s.UserMessages + s.AssistantMessages,
		InputTokens:         totalInput,
		OutputTokens:        s.OutputTokens,
		Title:               title,
		Summary:             summary,
		PreviousNote:        previous,
		FilesChanged:        filesChanged,
		ToolCounts:          s.ToolCounts,
		TotalTools:          s.ToolUses,
		ThinkingBlocks:      s.ThinkingBlocks,
		CognitiveComplexity: cogComplexity,
		AvgTurnMs:           s.AvgTurnDuration,
		MaxTurnMs:           s.MaxTurnDuration,
		CCVersion:           s.CCVersion,
		AllBranches:         s.Branches,
		AutoCompactions:     s.AutoCompactions,
	}
}

// SlotKind identifies which session-filename format was matched by
// ParseSessionFilename. Mechanism 1 of session-slot-multihost-disambiguation
// supports three filename shapes: legacy counter, plain timestamp, and
// timestamp with sub-millisecond collision-retry suffix.
type SlotKind int

const (
	// SlotKindUnknown — filename did not match any known session-note pattern.
	SlotKindUnknown SlotKind = iota
	// SlotKindCounter — legacy "YYYY-MM-DD-NN.md" two-digit counter format.
	SlotKindCounter
	// SlotKindTimestamp — "YYYY-MM-DD-HHMMSSmmm.md" millisecond timestamp body, no suffix.
	SlotKindTimestamp
	// SlotKindTimestampSuffix — "YYYY-MM-DD-HHMMSSmmm-N.md" with collision-retry suffix (N ∈ 1..9).
	SlotKindTimestampSuffix
)

var (
	counterFilenameRE   = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(\d{2})\.md$`)
	timestampFilenameRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(\d{9})(-\d)?\.md$`)
)

// BuildTimestampFilename returns the canonical timestamp-format session
// filename for a given date and timestamp. suffix == 0 produces no trailing
// suffix; suffix in 1..9 produces "YYYY-MM-DD-HHMMSSmmm-N.md" for the
// sub-millisecond collision-retry path (Mechanism 1).
//
// date MUST be the YYYY-MM-DD string of t (computed via t.Format("2006-01-02"))
// — caller is responsible for matching date prefix to t. The single clock
// source in CaptureFromParsed (Phase 4) ensures this.
//
// suffix outside [0,9] is treated as 0; callers that exhaust retries fail
// upstream with an actionable error rather than overflow.
func BuildTimestampFilename(date string, t time.Time, suffix int) string {
	// HHMMSSmmm — 2 digits hour + 2 digits minute + 2 digits second + 3 digits millisecond.
	body := fmt.Sprintf("%02d%02d%02d%03d", t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1_000_000)
	if suffix > 0 && suffix <= 9 {
		return fmt.Sprintf("%s-%s-%d.md", date, body, suffix)
	}
	return fmt.Sprintf("%s-%s.md", date, body)
}

// ParseSessionFilename inspects a session-note filename and returns its
// kind plus extracted fields. ok == false on malformed input.
//
// For SlotKindCounter, value is the two-digit counter (e.g. "01").
// For SlotKindTimestamp / SlotKindTimestampSuffix, value is the timestamp
// body including any -N suffix (e.g. "143025123" or "143025123-1").
//
// Lex sort order over filenames satisfying ParseSessionFilename(ok=true)
// equals chronological order (Mechanism 1 invariant), assuming a fixed
// date prefix.
func ParseSessionFilename(name string) (date string, kind SlotKind, value string, ok bool) {
	if m := counterFilenameRE.FindStringSubmatch(name); m != nil {
		return m[1], SlotKindCounter, m[2], true
	}
	if m := timestampFilenameRE.FindStringSubmatch(name); m != nil {
		body := m[2]
		if m[3] != "" {
			// m[3] is "-N"; concatenate body + "-N" for the value.
			return m[1], SlotKindTimestampSuffix, body + m[3], true
		}
		return m[1], SlotKindTimestamp, body, true
	}
	return "", SlotKindUnknown, "", false
}

// NoteFilename returns the filename for a session note: YYYY-MM-DD-NN.md.
//
// Deprecated: use BuildTimestampFilename for write paths;
// ParseSessionFilename for read. Phase 4 of
// session-slot-multihost-disambiguation removes call sites; the helper is
// retained during the transition so existing callers continue to compile.
func NoteFilename(date string, iteration int) string {
	return fmt.Sprintf("%s-%02d.md", date, iteration)
}

// NoteRelPathTimestamp builds the project-relative session-note path for a
// timestamp-format filename. Layout depends on host:
//
//   - host == ""  → legacy flat layout: Projects/<p>/sessions/<filename>
//     This branch exists as a Phase 1.5 back-compat shim for the sole
//     remaining production caller in internal/session/capture.go; Phase 2
//     replaces that caller and removes the empty-host case.
//   - host != ""  → per-host layout: Projects/<p>/sessions/<host>/<date>/<filename>
//     Used by the wrap-time mirror to project staging-dir notes into the
//     shared vault under a host-isolated subtree.
//
// Callers MUST sanitize `host` via staging.SanitizeHostname before calling.
// The helper rejects unsanitized input defensively: if `host` contains '/'
// or any '.' run that resolves to '.' or '..' as a path segment, the
// function returns "" — callers must check for empty and fall back. The
// rejection rule is intentionally conservative; it does NOT reproduce the
// full SanitizeHostname allowlist (callers own that), only the path-escape
// guards.
func NoteRelPathTimestamp(project, host, date string, t time.Time, suffix int) string {
	filename := BuildTimestampFilename(date, t, suffix)
	if host == "" {
		// Phase 1.5 back-compat: flat layout for the legacy production
		// caller. Phase 2 will pass a sanitized host and this branch goes
		// away.
		return filepath.Join("Projects", project, "sessions", filename)
	}
	// Path-escape guard: a host segment containing '/' or resolving to '.'
	// or '..' would let the path traverse out of sessions/. Sanitization is
	// the caller's job (staging.SanitizeHostname), but defend anyway.
	if strings.ContainsRune(host, '/') || host == "." || host == ".." {
		return ""
	}
	return filepath.Join("Projects", project, "sessions", host, date, filename)
}

func titleFromFirstMessage(msg string) string {
	if msg == "" {
		return "Session"
	}

	msg = strings.TrimSpace(msg)

	// Take first line
	if idx := strings.IndexByte(msg, '\n'); idx > 0 {
		msg = msg[:idx]
	}

	// Skip trivials
	lower := strings.ToLower(msg)
	trivials := []string{"hi", "hello", "hey", "ok", "okay", "yes", "no", "thanks", "thank you", "y", "n"}
	for _, t := range trivials {
		if lower == t {
			return "Session"
		}
	}

	// Truncate
	if len(msg) > 80 {
		msg = msg[:77] + "..."
	}

	return msg
}

func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func shortenPath(path, cwd string) string {
	if cwd != "" && strings.HasPrefix(path, cwd+"/") {
		return path[len(cwd)+1:]
	}
	return sanitize.CompressHome(path)
}
