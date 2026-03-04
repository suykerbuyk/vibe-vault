// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package transcript

import (
	"encoding/json"
	"strings"

	"github.com/johns/vibe-vault/internal/sanitize"
)

func computeStats(entries []Entry) Stats {
	s := Stats{
		FilesRead:    make(map[string]bool),
		FilesWritten: make(map[string]bool),
		ToolCounts:   make(map[string]int),
	}

	branchSet := make(map[string]bool)
	snapshotFiles := make(map[string]bool)

	// Build UUID set for session continuity detection
	uuidSet := make(map[string]bool)
	for _, e := range entries {
		if e.UUID != "" {
			uuidSet[e.UUID] = true
		}
	}

	for _, e := range entries {
		// Track time bounds
		if !e.Timestamp.IsZero() {
			if s.StartTime.IsZero() || e.Timestamp.Before(s.StartTime) {
				s.StartTime = e.Timestamp
			}
			if s.EndTime.IsZero() || e.Timestamp.After(s.EndTime) {
				s.EndTime = e.Timestamp
			}
		}

		// Session metadata from first entry that has it
		if s.SessionID == "" && e.SessionID != "" {
			s.SessionID = e.SessionID
		}
		if s.CWD == "" && e.CWD != "" {
			s.CWD = e.CWD
		}
		if s.GitBranch == "" && e.GitBranch != "" {
			s.GitBranch = e.GitBranch
		}

		// Track all unique branches (Task 18)
		if e.GitBranch != "" {
			branchSet[e.GitBranch] = true
		}

		// Track CC version from first entry (Task 18)
		if s.CCVersion == "" && e.Version != "" {
			s.CCVersion = e.Version
		}

		// Parse file-history-snapshot entries for ground truth file tracking (Task 17)
		if e.Type == "file-history-snapshot" {
			extractSnapshotFiles(&e, snapshotFiles)
			continue
		}

		// Parse system entries for turn_duration and compaction (Tasks 16, 18)
		if e.Type == "system" {
			if e.Subtype == "turn_duration" {
				if dur := extractTurnDuration(&e); dur > 0 {
					s.TurnDurations = append(s.TurnDurations, dur)
				}
			}
			if e.Subtype == "compact_boundary" {
				if extractCompactionTrigger(&e) == "auto" {
					s.AutoCompactions++
				}
			}
			continue
		}

		if e.Message == nil {
			continue
		}

		switch e.Message.Role {
		case "user":
			// Only count actual user messages, not tool results
			blocks := ContentBlocks(e.Message)
			isToolResult := false
			for _, b := range blocks {
				if b.Type == "tool_result" {
					isToolResult = true
					break
				}
			}
			if !isToolResult {
				s.UserMessages++
			}

		case "assistant":
			s.AssistantMessages++

			if s.Model == "" && e.Message.Model != "" && !strings.HasPrefix(e.Message.Model, "<") {
				s.Model = e.Message.Model
			}

			// Token usage
			if u := e.Message.Usage; u != nil {
				s.InputTokens += u.InputTokens
				s.OutputTokens += u.OutputTokens
				s.CacheReads += u.CacheReadInputTokens
				s.CacheWrites += u.CacheCreationInputTokens
			}

			// Thinking block metrics (Task 15)
			if thinking := ThinkingContent(e.Message); thinking != "" {
				s.ThinkingBlocks++
				s.ThinkingTokens += len(thinking)
			}

			// Tool uses and file tracking
			for _, tu := range ToolUses(e.Message) {
				s.ToolUses++
				s.ToolCounts[tu.Name]++
				trackFiles(&s, tu)
			}
		}
	}

	if !s.StartTime.IsZero() && !s.EndTime.IsZero() {
		s.Duration = s.EndTime.Sub(s.StartTime)
	}

	// Finalize turn duration stats
	if len(s.TurnDurations) > 0 {
		total := 0
		for _, d := range s.TurnDurations {
			total += d
			if d > s.MaxTurnDuration {
				s.MaxTurnDuration = d
			}
		}
		s.AvgTurnDuration = total / len(s.TurnDurations)
	}

	// Finalize snapshot files
	for f := range snapshotFiles {
		s.FilesModifiedBySnapshot = append(s.FilesModifiedBySnapshot, f)
	}

	// Finalize branches
	for b := range branchSet {
		s.Branches = append(s.Branches, b)
	}

	// Detect external parent UUID (indicates /continue session)
	for _, e := range entries {
		if e.ParentUUID != "" && !uuidSet[e.ParentUUID] {
			s.ParentUUID = e.ParentUUID
			break
		}
	}

	return s
}

// extractSnapshotFiles extracts file paths from file-history-snapshot entries.
func extractSnapshotFiles(e *Entry, files map[string]bool) {
	// file-history-snapshot entries store data in a top-level field.
	// We parse the raw JSON to extract trackedFileBackups keys.
	// The entry's raw JSON has been unmarshaled into Entry, but the
	// tracked files are in an extra field we need to access.
	// Since Entry doesn't have a catch-all field, we rely on the
	// ToolUseResult field or parse from the entry's content.
	// In Claude Code's format, file-history-snapshot has a "content"
	// field with trackedFileBackups as a map of filepath->content.
	if e.Message != nil {
		blocks := ContentBlocks(e.Message)
		for _, b := range blocks {
			if b.Input != nil {
				if m, ok := b.Input.(map[string]interface{}); ok {
					for k := range m {
						files[k] = true
					}
				}
			}
		}
	}
}

// extractTurnDuration extracts duration in ms from a turn_duration system entry.
func extractTurnDuration(e *Entry) int {
	if e.Message == nil {
		return 0
	}
	// turn_duration entries typically have the duration in content
	if s, ok := e.Message.Content.(string); ok {
		// Try to parse as a number
		var dur int
		for _, c := range s {
			if c >= '0' && c <= '9' {
				dur = dur*10 + int(c-'0')
			} else if dur > 0 {
				break
			}
		}
		return dur
	}
	return 0
}

// extractCompactionTrigger returns the trigger type from a compact_boundary entry.
func extractCompactionTrigger(e *Entry) string {
	if e.Message == nil {
		return ""
	}
	if s, ok := e.Message.Content.(string); ok {
		if strings.Contains(s, "auto") {
			return "auto"
		}
	}
	return ""
}

// trackFiles extracts file paths from tool inputs to track reads and writes.
func trackFiles(s *Stats, tu ContentBlock) {
	input, ok := tu.Input.(map[string]interface{})
	if !ok {
		return
	}

	switch tu.Name {
	case "Read":
		if p, ok := input["file_path"].(string); ok {
			s.FilesRead[p] = true
		}
	case "Write":
		if p, ok := input["file_path"].(string); ok {
			s.FilesWritten[p] = true
		}
	case "Edit":
		if p, ok := input["file_path"].(string); ok {
			s.FilesWritten[p] = true
		}
	case "NotebookEdit":
		if p, ok := input["notebook_path"].(string); ok {
			s.FilesWritten[p] = true
		}
	case "Bash":
		trackBashFiles(s, input)
	}
}

// trackBashFiles extracts file paths from bash commands that write files.
func trackBashFiles(s *Stats, input map[string]interface{}) {
	cmd, ok := input["command"].(string)
	if !ok {
		return
	}

	// Track git commits as a signal
	if strings.Contains(cmd, "git commit") {
		s.ToolCounts["git-commit"]++
	}

	// Track files created via common patterns
	_ = cmd // Future: parse redirect targets, mkdir, etc.
}

// UserText extracts all user-authored text from the transcript (not tool results).
func UserText(t *Transcript) string {
	var parts []string
	for _, e := range t.Entries {
		if e.Message == nil || e.Message.Role != "user" {
			continue
		}
		blocks := ContentBlocks(e.Message)
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		// Also handle string content
		if s, ok := e.Message.Content.(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// AssistantText extracts all assistant text from the transcript.
func AssistantText(t *Transcript) string {
	var parts []string
	for _, e := range t.Entries {
		if e.Message == nil || e.Message.Role != "assistant" {
			continue
		}
		if text := TextContent(e.Message); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// FirstUserMessage returns the first meaningful user-authored text message.
// Skips resume/context-loading instructions to find the actual task.
func FirstUserMessage(t *Transcript) string {
	var first string
	for _, e := range t.Entries {
		if e.Message == nil || e.Message.Role != "user" {
			continue
		}

		var text string
		// String content
		if s, ok := e.Message.Content.(string); ok && s != "" {
			text = s
		}
		// Array content — find first text block
		if text == "" {
			blocks := ContentBlocks(e.Message)
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					text = b.Text
					break
				}
			}
		}

		if text == "" {
			continue
		}

		// Strip XML tags from Claude Code
		text = sanitize.StripTags(text)
		if text == "" {
			continue
		}

		// Save first message as fallback
		if first == "" {
			first = text
		}

		// Skip resume/context instructions and short confirmations
		lower := strings.ToLower(text)
		if isResumeMessage(lower) || isConfirmation(lower) {
			continue
		}

		return text
	}
	return first
}

// isResumeMessage detects common resume/context-loading instructions that
// shouldn't be used as session titles.
func isResumeMessage(lower string) bool {
	// Slash commands
	if strings.HasPrefix(lower, "/") {
		return true
	}
	// System-injected caveats
	if strings.HasPrefix(lower, "caveat:") {
		return true
	}
	// Messages about resume.md or @resume
	if strings.Contains(lower, "@resume") || strings.Contains(lower, "resume.md") {
		return true
	}
	return false
}

// isTrivialMessage detects short confirmatory, greeting, or farewell messages
// that shouldn't be used as session titles.
func isConfirmation(lower string) bool {
	if len(lower) > 80 {
		return false
	}

	trivials := []string{
		"yes", "yeah", "yep", "yup", "ok", "okay", "sure",
		"go ahead", "do it", "proceed", "correct", "right",
		"that's right", "sounds good", "looks good", "lgtm",
		"fix them", "yes,", "no,",
		// Greetings
		"hi", "hello", "hey", "good morning", "good afternoon",
		// Farewells
		"bye", "goodbye", "see ya", "see you", "thanks", "thank you",
		"cheers", "later", "ttyl", "good night",
		// Acknowledgments
		"got it", "understood", "noted", "perfect", "great", "nice",
		"awesome", "cool", "good", "fine", "alright",
	}
	for _, t := range trivials {
		if lower == t || lower == t+"!" || lower == t+"." {
			return true
		}
		if strings.HasPrefix(lower, t+",") || strings.HasPrefix(lower, t+" ") {
			// Only skip if message is short overall
			if len(lower) < 80 {
				return true
			}
		}
	}
	return false
}

// Summary produces a JSON-serializable summary of stats.
func (s Stats) Summary() map[string]interface{} {
	m := map[string]interface{}{
		"session_id":         s.SessionID,
		"model":              s.Model,
		"git_branch":         s.GitBranch,
		"duration_minutes":   int(s.Duration.Minutes()),
		"user_messages":      s.UserMessages,
		"assistant_messages": s.AssistantMessages,
		"tool_uses":          s.ToolUses,
		"input_tokens":       s.InputTokens,
		"output_tokens":      s.OutputTokens,
	}
	b, _ := json.Marshal(m)
	var result map[string]interface{}
	_ = json.Unmarshal(b, &result)
	return result
}
