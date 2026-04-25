// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/narrative"
)

const maxSummaryLen = 2000

// ExtractNarrative builds a Narrative from a Zed thread.
// Zed threads don't have compact boundaries, so the result is a single Segment.
func ExtractNarrative(thread *Thread) *narrative.Narrative {
	if thread == nil || len(thread.Messages) == 0 {
		return nil
	}

	narr := &narrative.Narrative{
		Title:   thread.Title,
		Summary: capString(derefString(thread.DetailedSummary), maxSummaryLen),
	}

	// Use thread summary (DB column) as fallback if detailed_summary is null
	if narr.Summary == "" && thread.Summary != "" {
		narr.Summary = capString(thread.Summary, maxSummaryLen)
	}

	// Build activities from tool_use blocks in agent messages
	var activities []narrative.Activity
	for _, msg := range thread.Messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type != "tool_use" {
				continue
			}
			act := classifyZedToolUse(c, msg.ToolResults)
			if act != nil {
				activities = append(activities, *act)
			}
		}
	}

	// Single segment with all activities
	seg := narrative.Segment{
		Index:       0,
		Activities:  activities,
		UserRequest: firstUserMessage(thread.Messages),
	}
	narr.Segments = []narrative.Segment{seg}

	// Extract commits from terminal output
	narr.Commits = extractCommitsFromThread(thread)

	// Infer tag from activities
	narr.Tag = inferTagFromActivities(activities)

	// Work performed rendering
	narr.WorkPerformed = narrative.RenderWorkPerformed(narr.Segments)

	return narr
}

// classifyZedToolUse maps a Zed tool_use content block to a narrative Activity.
// toolResults provides the agent message's tool results for error detection.
func classifyZedToolUse(c ZedContent, toolResults map[string]ZedToolResult) *narrative.Activity {
	canonical := NormalizeTool(c.ToolName)

	// Check for errors from tool results
	isErr := false
	if tr, ok := toolResults[c.ToolID]; ok {
		isErr = tr.IsError
	}

	switch canonical {
	case "Write":
		path := inputStr(c.Input, "file_path")
		if path == "" {
			path = inputStr(c.Input, "path")
		}
		return &narrative.Activity{
			Kind:        narrative.KindFileCreate,
			Description: "Created `" + shortenPath(path) + "`",
			Tool:        canonical,
			IsError:     isErr,
		}

	case "Edit":
		path := inputStr(c.Input, "file_path")
		if path == "" {
			path = inputStr(c.Input, "path")
		}
		return &narrative.Activity{
			Kind:        narrative.KindFileModify,
			Description: "Modified `" + shortenPath(path) + "`",
			Tool:        canonical,
			IsError:     isErr,
		}

	case "Bash":
		cmd := inputStr(c.Input, "command")
		if cmd == "" {
			cmd = inputStr(c.Input, "cmd")
		}
		act := classifyBashActivity(cmd, canonical)
		act.IsError = isErr
		return act

	case "Read", "Grep", "Glob", "ListDir":
		return &narrative.Activity{
			Kind:        narrative.KindExplore,
			Description: "Read/searched codebase (" + canonical + ")",
			Tool:        canonical,
		}

	case "Thinking":
		return nil // Skip thinking blocks

	default:
		return &narrative.Activity{
			Kind:        narrative.KindCommand,
			Description: "Used " + canonical,
			Tool:        canonical,
			IsError:     isErr,
		}
	}
}

// classifyBashActivity classifies a bash command into an activity.
func classifyBashActivity(cmd, tool string) *narrative.Activity {
	lower := strings.ToLower(cmd)

	if isTestCmd(lower) {
		return &narrative.Activity{
			Kind:        narrative.KindTestRun,
			Description: "Ran tests",
			Tool:        tool,
			Detail:      truncateStr(cmd, 120),
		}
	}

	if strings.Contains(lower, "git commit") {
		msg := extractCommitMsg(cmd)
		desc := "Committed changes"
		if msg != "" {
			desc = "Committed: \"" + truncateStr(msg, 60) + "\""
		}
		return &narrative.Activity{
			Kind:        narrative.KindGitCommit,
			Description: desc,
			Tool:        tool,
		}
	}

	if strings.Contains(lower, "git push") {
		return &narrative.Activity{
			Kind:        narrative.KindGitPush,
			Description: "Pushed to remote",
			Tool:        tool,
		}
	}

	if isBuildCmd(lower) {
		return &narrative.Activity{
			Kind:        narrative.KindBuild,
			Description: "Built project",
			Tool:        tool,
			Detail:      truncateStr(cmd, 120),
		}
	}

	return &narrative.Activity{
		Kind:        narrative.KindCommand,
		Description: "Ran `" + truncateStr(firstLineStr(cmd), 60) + "`",
		Tool:        tool,
		Detail:      truncateStr(cmd, 120),
	}
}

// extractCommitsFromThread scans terminal/bash tool results for git commit output.
func extractCommitsFromThread(thread *Thread) []narrative.Commit {
	var commits []narrative.Commit

	for _, msg := range thread.Messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type != "tool_use" {
				continue
			}
			canonical := NormalizeTool(c.ToolName)
			if canonical != "Bash" {
				continue
			}
			cmd := inputStr(c.Input, "command")
			if cmd == "" {
				cmd = inputStr(c.Input, "cmd")
			}
			if !strings.Contains(strings.ToLower(cmd), "git commit") {
				continue
			}
			// Look up tool result on the same agent message
			tr, ok := msg.ToolResults[c.ToolID]
			if !ok || tr.IsError {
				continue
			}
			output := bestToolOutput(tr)
			sha, commitMsg := parseCommitOutput(output)
			if sha != "" {
				commits = append(commits, narrative.Commit{SHA: sha, Message: commitMsg})
			}
		}
	}

	return commits
}

// parseCommitOutput extracts SHA and message from git commit output.
// Expected format: "[branch sha] message"
func parseCommitOutput(output string) (sha, msg string) {
	line := firstLineStr(output)
	open := strings.IndexByte(line, '[')
	cl := strings.IndexByte(line, ']')
	if open < 0 || cl < 0 || cl <= open {
		return "", ""
	}

	bracket := line[open+1 : cl]
	parts := strings.Fields(bracket)
	if len(parts) < 2 {
		return "", ""
	}
	candidate := parts[len(parts)-1]

	if len(candidate) < 7 {
		return "", ""
	}
	for _, c := range candidate {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return "", ""
		}
	}

	if cl+2 < len(line) {
		msg = strings.TrimSpace(line[cl+1:])
	}
	return candidate, msg
}

// firstUserMessage extracts the first user text from messages.
func firstUserMessage(messages []ZedMessage) string {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				text := strings.TrimSpace(c.Text)
				return truncateStr(firstLineStr(text), 120)
			}
		}
	}
	return ""
}

// inferTagFromActivities infers an activity tag from the tool usage pattern.
func inferTagFromActivities(activities []narrative.Activity) string {
	if len(activities) == 0 {
		return "explore"
	}

	var creates, modifies, tests, commits, explores int
	for _, a := range activities {
		switch a.Kind {
		case narrative.KindFileCreate:
			creates++
		case narrative.KindFileModify:
			modifies++
		case narrative.KindTestRun:
			tests++
		case narrative.KindGitCommit:
			commits++
		case narrative.KindExplore:
			explores++
		}
	}

	total := len(activities)
	if creates > total/3 {
		return "build"
	}
	if tests > total/4 {
		return "test"
	}
	if modifies > total/3 {
		return "refactor"
	}
	if explores > total/2 {
		return "explore"
	}
	if commits > 0 && (creates+modifies) > 0 {
		return "build"
	}
	return "explore"
}

// --- helpers ---

func inputStr(input any, key string) string {
	m, ok := input.(map[string]any)
	if !ok {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func shortenPath(path string) string {
	if path == "" {
		return "file"
	}
	// Take last two path components for brevity
	parts := strings.Split(path, "/")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return path
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func firstLineStr(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}

func capString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func extractCommitMsg(cmd string) string {
	idx := strings.Index(cmd, "-m ")
	if idx < 0 {
		idx = strings.Index(cmd, "-m\"")
		if idx < 0 {
			return ""
		}
	}
	rest := strings.TrimLeft(cmd[idx+2:], " ")
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

func isTestCmd(lower string) bool {
	for _, p := range []string{
		"go test", "npm test", "npm run test", "npx jest", "npx vitest",
		"pytest", "python -m pytest", "cargo test",
		"make test", "make integration", "make check",
		"jest", "mocha", "vitest", "bun test",
	} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func isBuildCmd(lower string) bool {
	for _, p := range []string{
		"make build", "make all", "go build", "npm run build",
		"cargo build", "make install",
	} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
