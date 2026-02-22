package transcript

import (
	"encoding/json"
	"strings"

	"github.com/johns/sesscap/internal/sanitize"
)

func computeStats(entries []Entry) Stats {
	s := Stats{
		FilesRead:    make(map[string]bool),
		FilesWritten: make(map[string]bool),
		ToolCounts:   make(map[string]int),
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

	return s
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
