package narrative

import (
	"fmt"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/transcript"
)

// Extract processes a transcript and produces a Narrative with activities,
// inferred title, summary, tag, and rendered WorkPerformed section.
func Extract(t *transcript.Transcript, cwd string) *Narrative {
	if t == nil || len(t.Entries) == 0 {
		return nil
	}

	rawSegments := SegmentEntries(t.Entries)

	var segments []Segment
	for i, raw := range rawSegments {
		activities := extractActivities(raw, cwd)
		activities = aggregateExploration(activities)
		detectRecoveries(activities)

		seg := Segment{
			Index:       i,
			Activities:  activities,
			UserRequest: firstUserRequest(raw),
		}

		// Time bounds
		for _, e := range raw {
			if e.Timestamp.IsZero() {
				continue
			}
			if seg.StartTime.IsZero() || e.Timestamp.Before(seg.StartTime) {
				seg.StartTime = e.Timestamp
			}
			if seg.EndTime.IsZero() || e.Timestamp.After(seg.EndTime) {
				seg.EndTime = e.Timestamp
			}
		}

		segments = append(segments, seg)
	}

	narr := &Narrative{
		Segments: segments,
	}

	narr.Title = inferTitle(segments, t)
	narr.Summary = inferSummary(segments)
	narr.Tag = inferTag(segments)
	narr.Decisions = extractDecisions(segments, t.Entries)
	narr.OpenThreads = inferOpenThreads(segments)
	narr.WorkPerformed = RenderWorkPerformed(segments)

	return narr
}

// ToolResult pairs a tool_use ID with its result information.
type ToolResult struct {
	IsError bool
	Content string
}

// BuildToolResultMap pairs tool_use IDs with their results from tool_result blocks.
func BuildToolResultMap(entries []transcript.Entry) map[string]*ToolResult {
	results := make(map[string]*ToolResult)

	for _, e := range entries {
		if e.Message == nil {
			continue
		}
		blocks := transcript.ContentBlocks(e.Message)
		for _, b := range blocks {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				tr := &ToolResult{
					IsError: b.IsError,
				}
				// Extract content string
				switch c := b.Content.(type) {
				case string:
					tr.Content = c
				case []interface{}:
					for _, item := range c {
						if m, ok := item.(map[string]interface{}); ok {
							if text, ok := m["text"].(string); ok {
								tr.Content = text
								break
							}
						}
					}
				}
				results[b.ToolUseID] = tr
			}
		}
	}

	return results
}

// extractActivities processes entries from one segment into activities.
func extractActivities(entries []transcript.Entry, cwd string) []Activity {
	resultMap := BuildToolResultMap(entries)
	var activities []Activity

	for _, e := range entries {
		if e.Message == nil || e.Message.Role != "assistant" {
			continue
		}

		toolUses := transcript.ToolUses(e.Message)
		for _, tu := range toolUses {
			result := resultMap[tu.ID]
			act := classifyToolUse(tu, result, e.Timestamp, cwd)
			if act != nil {
				activities = append(activities, *act)
			}
		}
	}

	return activities
}

// classifyToolUse maps a tool call to an Activity.
func classifyToolUse(tu transcript.ContentBlock, result *ToolResult, ts time.Time, cwd string) *Activity {
	isErr := result != nil && result.IsError

	switch tu.Name {
	case "Write":
		path := inputStr(tu.Input, "file_path")
		return &Activity{
			Timestamp:   ts,
			Kind:        KindFileCreate,
			Description: fmt.Sprintf("Created `%s`", shortenPath(path, cwd)),
			Tool:        "Write",
			IsError:     isErr,
		}

	case "Edit":
		path := inputStr(tu.Input, "file_path")
		return &Activity{
			Timestamp:   ts,
			Kind:        KindFileModify,
			Description: fmt.Sprintf("Modified `%s`", shortenPath(path, cwd)),
			Tool:        "Edit",
			IsError:     isErr,
		}

	case "NotebookEdit":
		path := inputStr(tu.Input, "notebook_path")
		return &Activity{
			Timestamp:   ts,
			Kind:        KindFileModify,
			Description: fmt.Sprintf("Modified `%s`", shortenPath(path, cwd)),
			Tool:        "NotebookEdit",
			IsError:     isErr,
		}

	case "Bash":
		cmd := inputStr(tu.Input, "command")
		return ClassifyBashCommand(cmd, result, ts, cwd)

	case "EnterPlanMode":
		return &Activity{
			Timestamp:   ts,
			Kind:        KindPlanMode,
			Description: "Entered plan mode",
			Tool:        "EnterPlanMode",
		}

	case "ExitPlanMode":
		return &Activity{
			Timestamp:   ts,
			Kind:        KindPlanMode,
			Description: "Plan approved",
			Tool:        "ExitPlanMode",
		}

	case "AskUserQuestion":
		question := extractQuestion(tu.Input)
		desc := "Decision point"
		if question != "" {
			desc = fmt.Sprintf("Decision: %s", truncateStr(question, 80))
		}
		return &Activity{
			Timestamp:   ts,
			Kind:        KindDecision,
			Description: desc,
			Tool:        "AskUserQuestion",
			Detail:      question,
		}

	case "Task":
		taskDesc := inputStr(tu.Input, "description")
		desc := "Delegated task"
		if taskDesc != "" {
			desc = fmt.Sprintf("Delegated: %s", truncateStr(taskDesc, 80))
		}
		return &Activity{
			Timestamp:   ts,
			Kind:        KindDelegation,
			Description: desc,
			Tool:        "Task",
		}

	case "Read", "Grep", "Glob", "WebFetch", "WebSearch":
		return &Activity{
			Timestamp:   ts,
			Kind:        KindExplore,
			Description: fmt.Sprintf("Read/searched codebase (%s)", tu.Name),
			Tool:        tu.Name,
		}

	default:
		return &Activity{
			Timestamp:   ts,
			Kind:        KindCommand,
			Description: fmt.Sprintf("Used %s", tu.Name),
			Tool:        tu.Name,
			IsError:     isErr,
		}
	}
}

// ClassifyBashCommand classifies a Bash tool call based on the command text.
func ClassifyBashCommand(cmd string, result *ToolResult, ts time.Time, cwd string) *Activity {
	isErr := result != nil && result.IsError
	lower := strings.ToLower(cmd)

	// Test commands
	if isTestCommand(lower) {
		status := "success"
		if isErr {
			status = "failed"
		} else if result != nil {
			status = extractTestStatus(result.Content, isErr)
		}
		return &Activity{
			Timestamp:   ts,
			Kind:        KindTestRun,
			Description: fmt.Sprintf("Ran tests (%s)", status),
			Tool:        "Bash",
			IsError:     isErr,
			Detail:      truncateStr(cmd, 120),
		}
	}

	// Git commit
	if strings.Contains(lower, "git commit") {
		msg := extractCommitMessage(cmd)
		desc := "Committed changes"
		if msg != "" {
			desc = fmt.Sprintf("Committed: \"%s\"", truncateStr(msg, 60))
		}
		return &Activity{
			Timestamp:   ts,
			Kind:        KindGitCommit,
			Description: desc,
			Tool:        "Bash",
			IsError:     isErr,
		}
	}

	// Git push
	if strings.Contains(lower, "git push") {
		return &Activity{
			Timestamp:   ts,
			Kind:        KindGitPush,
			Description: "Pushed to remote",
			Tool:        "Bash",
			IsError:     isErr,
		}
	}

	// Build commands
	if isBuildCommand(lower) {
		status := "success"
		if isErr {
			status = "failed"
		}
		return &Activity{
			Timestamp:   ts,
			Kind:        KindBuild,
			Description: fmt.Sprintf("Built project (%s)", status),
			Tool:        "Bash",
			IsError:     isErr,
			Detail:      truncateStr(cmd, 120),
		}
	}

	// General command
	status := "success"
	if isErr {
		status = "failed"
	}
	return &Activity{
		Timestamp:   ts,
		Kind:        KindCommand,
		Description: fmt.Sprintf("Ran `%s` (%s)", truncateStr(firstLine(cmd), 60), status),
		Tool:        "Bash",
		IsError:     isErr,
		Detail:      truncateStr(cmd, 120),
	}
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

// isBuildCommand checks if a command is a build invocation.
func isBuildCommand(lower string) bool {
	buildPatterns := []string{
		"make build", "make all", "go build", "npm run build",
		"cargo build", "make install",
	}
	for _, p := range buildPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// extractTestStatus parses test output for pass/fail information.
func extractTestStatus(output string, isErr bool) string {
	if isErr {
		return "failed"
	}

	lower := strings.ToLower(output)

	// Go test output
	if strings.Contains(lower, "fail") {
		return "failed"
	}
	if strings.Contains(output, "PASS") || strings.Contains(output, "ok ") {
		return "success"
	}

	return "success"
}

// extractCommitMessage extracts the message from a git commit command.
func extractCommitMessage(cmd string) string {
	// Look for -m "message" or -m 'message'
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

	// Handle quoted messages
	quote := rest[0]
	if quote == '"' || quote == '\'' {
		end := strings.IndexByte(rest[1:], quote)
		if end >= 0 {
			return rest[1 : end+1]
		}
		return rest[1:]
	}

	// Unquoted: take until next flag or end
	if sp := strings.Index(rest, " -"); sp > 0 {
		return rest[:sp]
	}
	return rest
}

// aggregateExploration collapses consecutive exploration activities into one.
func aggregateExploration(activities []Activity) []Activity {
	if len(activities) == 0 {
		return activities
	}

	var result []Activity
	exploreCount := 0
	exploreFiles := 0

	flushExplore := func() {
		if exploreCount > 0 {
			result = append(result, Activity{
				Kind:        KindExplore,
				Description: fmt.Sprintf("Explored codebase (%d lookups)", exploreFiles),
				Tool:        "explore",
			})
			exploreCount = 0
			exploreFiles = 0
		}
	}

	for _, a := range activities {
		if a.Kind == KindExplore {
			exploreCount++
			exploreFiles++
		} else {
			flushExplore()
			result = append(result, a)
		}
	}
	flushExplore()

	return result
}

// detectRecoveries marks error→fix patterns where an error was followed
// by a successful retry of the same kind.
func detectRecoveries(activities []Activity) {
	for i := 0; i < len(activities); i++ {
		if !activities[i].IsError {
			continue
		}
		// Look ahead for recovery (same kind, not error)
		for j := i + 1; j < len(activities) && j <= i+3; j++ {
			if activities[j].Kind == activities[i].Kind && !activities[j].IsError {
				activities[i].Recovered = true
				break
			}
		}
	}
}

// firstUserRequest finds the first meaningful user message in a set of entries.
func firstUserRequest(entries []transcript.Entry) string {
	for _, e := range entries {
		if e.IsMeta {
			continue
		}
		if e.Message == nil || e.Message.Role != "user" {
			continue
		}

		// Skip tool results
		blocks := transcript.ContentBlocks(e.Message)
		isToolResult := false
		for _, b := range blocks {
			if b.Type == "tool_result" {
				isToolResult = true
				break
			}
		}
		if isToolResult {
			continue
		}

		text := extractUserText(e)
		if text == "" {
			continue
		}

		// Skip noise patterns
		if IsNoiseMessage(text) {
			continue
		}

		return truncateStr(firstLine(text), 120)
	}
	return ""
}

// extractQuestion gets the question text from an AskUserQuestion input.
func extractQuestion(input interface{}) string {
	m, ok := input.(map[string]interface{})
	if !ok {
		return ""
	}
	questions, ok := m["questions"]
	if !ok {
		return ""
	}
	qList, ok := questions.([]interface{})
	if !ok || len(qList) == 0 {
		return ""
	}
	first, ok := qList[0].(map[string]interface{})
	if !ok {
		return ""
	}
	q, _ := first["question"].(string)
	return q
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

// IsNoiseMessage detects messages that shouldn't be used as user requests.
func IsNoiseMessage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Slash commands
	if strings.HasPrefix(lower, "/") {
		return true
	}

	// System-injected
	if strings.HasPrefix(lower, "caveat:") {
		return true
	}

	// Resume references
	if strings.Contains(lower, "@resume") || strings.Contains(lower, "resume.md") {
		return true
	}

	// Plan instructions
	if strings.HasPrefix(lower, "implement the following plan:") {
		return true
	}
	if strings.HasPrefix(lower, "execute the plan") {
		return true
	}

	// Trivial messages
	trivials := []string{
		"yes", "yeah", "yep", "yup", "ok", "okay", "sure",
		"go ahead", "do it", "proceed", "correct", "right",
		"hi", "hello", "hey", "restart", "continue",
		"thanks", "thank you", "bye", "goodbye",
		"got it", "understood", "noted", "perfect", "great",
		"nice", "awesome", "cool", "good", "fine", "alright",
	}
	for _, t := range trivials {
		if lower == t || lower == t+"!" || lower == t+"." {
			return true
		}
	}

	// Pure code blocks (starts with ```)
	if strings.HasPrefix(text, "```") {
		return true
	}

	return false
}

// ExtractCommits scans all transcript entries for successful git commit tool
// calls and extracts the SHA and message from the tool result output.
func ExtractCommits(entries []transcript.Entry) []Commit {
	resultMap := BuildToolResultMap(entries)
	var commits []Commit

	for _, e := range entries {
		if e.Message == nil || e.Message.Role != "assistant" {
			continue
		}

		toolUses := transcript.ToolUses(e.Message)
		for _, tu := range toolUses {
			if tu.Name != "Bash" {
				continue
			}
			cmd := inputStr(tu.Input, "command")
			if !strings.Contains(strings.ToLower(cmd), "git commit") {
				continue
			}
			result := resultMap[tu.ID]
			if result == nil || result.IsError {
				continue
			}
			sha, msg := parseCommitResult(result.Content)
			if sha != "" {
				commits = append(commits, Commit{SHA: sha, Message: msg})
			}
		}
	}

	return commits
}

// parseCommitResult extracts SHA and message from git commit output.
// Expected format: "[branch sha] message"
func parseCommitResult(output string) (sha, msg string) {
	line := firstLine(output)
	// Find bracketed section
	open := strings.IndexByte(line, '[')
	close := strings.IndexByte(line, ']')
	if open < 0 || close < 0 || close <= open {
		return "", ""
	}

	bracket := line[open+1 : close]
	// SHA is the last space-delimited word inside brackets
	parts := strings.Fields(bracket)
	if len(parts) < 2 {
		return "", ""
	}
	candidate := parts[len(parts)-1]

	// Validate: >= 7 chars, all hex
	if len(candidate) < 7 {
		return "", ""
	}
	for _, c := range candidate {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "", ""
		}
	}

	// Message is everything after "] "
	msg = ""
	if close+2 < len(line) {
		msg = strings.TrimSpace(line[close+1:])
	}

	return candidate, msg
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

func truncateStr(s string, maxLen int) string {
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
