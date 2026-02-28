package prose

import (
	"fmt"
	"strings"

	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/transcript"
)

// Turn represents a user or assistant prose contribution.
type Turn struct {
	Role string // "user" or "assistant"
	Text string
}

// Marker represents a brief inline notation for a tool action.
type Marker struct {
	Text string // e.g. "Created `foo.go`", "Ran tests (success)"
}

// Element is either a Turn or Marker in sequence.
type Element struct {
	Turn   *Turn
	Marker *Marker
}

// Section groups elements under a user request heading.
type Section struct {
	UserRequest string    // first meaningful user message
	Elements    []Element // interleaved turns and markers
}

// Dialogue is the extracted prose content for a session.
type Dialogue struct {
	Sections []Section
}

const (
	fillerMaxChars = 120
	userMaxChars   = 500
)

// Extract pulls prose dialogue from a transcript.
// Returns nil if no meaningful prose is found.
func Extract(t *transcript.Transcript, cwd string) *Dialogue {
	if t == nil || len(t.Entries) == 0 {
		return nil
	}

	segments := narrative.SegmentEntries(t.Entries)
	resultMap := narrative.BuildToolResultMap(t.Entries)

	var sections []Section
	for _, seg := range segments {
		sec := extractSection(seg, resultMap, cwd)
		if sec != nil {
			sections = append(sections, *sec)
		}
	}

	if len(sections) == 0 {
		return nil
	}

	return &Dialogue{Sections: sections}
}

// extractSection processes one segment into a Section.
func extractSection(entries []transcript.Entry, resultMap map[string]*narrative.ToolResult, cwd string) *Section {
	var elements []Element
	var userRequest string

	for _, e := range entries {
		if e.Type == "system" || e.IsMeta {
			continue
		}
		if e.Message == nil {
			continue
		}

		switch e.Message.Role {
		case "user":
			elems := processUserEntry(e, &userRequest)
			elements = append(elements, elems...)

		case "assistant":
			elems := processAssistantEntry(e, resultMap, cwd)
			elements = append(elements, elems...)
		}
	}

	if len(elements) == 0 {
		return nil
	}

	return &Section{
		UserRequest: userRequest,
		Elements:    elements,
	}
}

// processUserEntry extracts turns from a user entry.
func processUserEntry(e transcript.Entry, userRequest *string) []Element {
	blocks := transcript.ContentBlocks(e.Message)

	// Skip tool_result entries entirely
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return nil
		}
	}

	// Check for PlanContent
	if e.PlanContent != "" {
		text := e.PlanContent
		if *userRequest == "" {
			*userRequest = truncate(firstLine(text), 120)
		}
		return []Element{{Turn: &Turn{Role: "user", Text: text}}}
	}

	// Extract text content
	text := extractUserText(e)
	if text == "" {
		return nil
	}

	// Skip noise
	if narrative.IsNoiseMessage(text) {
		return nil
	}

	if *userRequest == "" {
		*userRequest = truncate(firstLine(text), 120)
	}

	// Cap user text
	if len(text) > userMaxChars {
		text = text[:userMaxChars] + " [...]"
	}

	return []Element{{Turn: &Turn{Role: "user", Text: text}}}
}

// processAssistantEntry extracts turns and markers from an assistant entry.
func processAssistantEntry(e transcript.Entry, resultMap map[string]*narrative.ToolResult, cwd string) []Element {
	blocks := transcript.ContentBlocks(e.Message)

	// Collect text and tool_use blocks separately
	var textParts []string
	var toolUses []transcript.ContentBlock
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				textParts = append(textParts, strings.TrimSpace(b.Text))
			}
		case "tool_use":
			toolUses = append(toolUses, b)
		}
		// Skip thinking blocks
	}

	fullText := strings.Join(textParts, "\n\n")
	hasToolUse := len(toolUses) > 0

	var elements []Element

	// Filler filter: short text + tool_use present → skip text
	if hasToolUse && len(fullText) < fillerMaxChars {
		// Don't emit the text turn, only markers
	} else if fullText != "" {
		elements = append(elements, Element{Turn: &Turn{Role: "assistant", Text: fullText}})
	}

	// Generate markers from tool uses
	for _, tu := range toolUses {
		result := resultMap[tu.ID]
		marker := classifyMarker(tu, result, cwd)
		if marker != nil {
			elements = append(elements, Element{Marker: marker})
		}
	}

	return elements
}

// classifyMarker generates a brief marker for a tool use.
func classifyMarker(tu transcript.ContentBlock, result *narrative.ToolResult, cwd string) *Marker {
	isErr := result != nil && result.IsError

	switch tu.Name {
	case "Write":
		path := inputStr(tu.Input, "file_path")
		return &Marker{Text: fmt.Sprintf("Created `%s`", shortenPath(path, cwd))}

	case "Edit":
		path := inputStr(tu.Input, "file_path")
		return &Marker{Text: fmt.Sprintf("Modified `%s`", shortenPath(path, cwd))}

	case "NotebookEdit":
		path := inputStr(tu.Input, "notebook_path")
		return &Marker{Text: fmt.Sprintf("Modified `%s`", shortenPath(path, cwd))}

	case "Bash":
		cmd := inputStr(tu.Input, "command")
		return classifyBashMarker(cmd, isErr)

	case "ExitPlanMode":
		return &Marker{Text: "Plan approved"}

	default:
		if isErr {
			errText := "tool error"
			if result != nil && result.Content != "" {
				errText = truncate(firstLine(result.Content), 80)
			}
			return &Marker{Text: fmt.Sprintf("Error: %s", errText)}
		}
		return nil
	}
}

// classifyBashMarker generates a marker for a Bash command.
func classifyBashMarker(cmd string, isErr bool) *Marker {
	lower := strings.ToLower(cmd)

	// Test commands
	if isTestCommand(lower) {
		status := "success"
		if isErr {
			status = "failed"
		}
		return &Marker{Text: fmt.Sprintf("Ran tests (%s)", status)}
	}

	// Git commit
	if strings.Contains(lower, "git commit") {
		msg := extractCommitMessage(cmd)
		if msg != "" {
			return &Marker{Text: fmt.Sprintf("Committed: \"%s\"", truncate(msg, 60))}
		}
		return &Marker{Text: "Committed changes"}
	}

	// Git push
	if strings.Contains(lower, "git push") {
		return &Marker{Text: "Pushed to remote"}
	}

	// Other bash commands don't get markers (they'd be noise)
	return nil
}

// isTestCommand checks if a command is a test invocation.
func isTestCommand(lower string) bool {
	testPatterns := []string{
		"go test", "npm test", "npm run test", "npx jest", "npx vitest",
		"pytest", "python -m pytest", "cargo test",
		"make test", "make integration", "make check",
		"jest", "mocha", "vitest", "bun test",
	}
	for _, p := range testPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// extractCommitMessage extracts -m "msg" from a git commit command.
func extractCommitMessage(cmd string) string {
	idx := strings.Index(cmd, "-m ")
	if idx < 0 {
		idx = strings.Index(cmd, "-m\"")
		if idx < 0 {
			return ""
		}
	}
	rest := cmd[idx+2:]
	rest = strings.TrimLeft(rest, " ")
	if len(rest) == 0 {
		return ""
	}
	quote := rest[0]
	if quote == '"' || quote == '\'' {
		end := strings.IndexByte(rest[1:], quote)
		if end >= 0 {
			return rest[1 : end+1]
		}
		return rest[1:]
	}
	if sp := strings.Index(rest, " -"); sp > 0 {
		return rest[:sp]
	}
	return rest
}

// extractUserText gets text content from a user entry.
func extractUserText(e transcript.Entry) string {
	if e.Message == nil {
		return ""
	}
	if s, ok := e.Message.Content.(string); ok && s != "" {
		return strings.TrimSpace(s)
	}
	blocks := transcript.ContentBlocks(e.Message)
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return strings.TrimSpace(b.Text)
		}
	}
	return ""
}

// --- Helpers ---

func inputStr(input interface{}, key string) string {
	m, ok := input.(map[string]interface{})
	if !ok {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func shortenPath(path, cwd string) string {
	if cwd != "" && strings.HasPrefix(path, cwd+"/") {
		return path[len(cwd)+1:]
	}
	return path
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}
