// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"github.com/johns/vibe-vault/internal/transcript"
)

// toolNameMap normalizes Zed tool names to vibe-vault canonical names.
var toolNameMap = map[string]string{
	"terminal":            "Bash",
	"bash":                "Bash",
	"run_terminal_cmd":    "Bash",
	"run_terminal_command": "Bash",
	"read_file":           "Read",
	"read":                "Read",
	"edit_file":           "Edit",
	"str_replace_editor":  "Edit",
	"edit":                "Edit",
	"grep":                "Grep",
	"find_path":           "Glob",
	"list_directory":      "ListDir",
	"thinking":            "Thinking",
	"create_file":         "Write",
	"write_file":          "Write",
	"write_to_file":       "Write",
	"write":               "Write",
	"save_file":           "Save",
	"delete_path":         "Delete",
	"move_path":           "Move",
	"copy_path":           "Copy",
	"create_directory":    "Mkdir",
	"diagnostics":         "Diagnostics",
	"fetch":               "WebFetch",
	"web_search":          "WebSearch",
	"open":                "Open",
	"now":                 "Now",
	"restore_file_from_disk": "Restore",
}

// NormalizeTool maps a Zed tool name to a canonical vibe-vault name.
func NormalizeTool(name string) string {
	if canonical, ok := toolNameMap[name]; ok {
		return canonical
	}
	return name
}

// Convert transforms a Zed Thread into a transcript.Transcript.
func Convert(thread *Thread) (*transcript.Transcript, error) {
	if thread == nil {
		return nil, nil
	}

	var entries []transcript.Entry
	toolCounts := make(map[string]int)
	var userMsgs, assistantMsgs, toolUses, thinkingBlocks int

	sessionID := "zed:" + thread.ID
	model := modelString(thread.Model)

	for i, msg := range thread.Messages {
		entry := transcript.Entry{
			Type:      msg.Role,
			SessionID: sessionID,
			UUID:      thread.ID + "-" + itoa(i),
		}

		var contentBlocks []interface{}

		for _, c := range msg.Content {
			switch c.Type {
			case "text":
				if msg.Role == "user" {
					userMsgs++
				} else {
					assistantMsgs++
				}
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": c.Text,
				})

			case "thinking":
				thinkingBlocks++
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type":     "thinking",
					"thinking": c.Thinking,
				})

			case "tool_use":
				toolUses++
				canonical := NormalizeTool(c.ToolName)
				toolCounts[canonical]++
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type":  "tool_use",
					"id":    c.ToolID,
					"name":  canonical,
					"input": c.Input,
				})

			case "mention":
				// Inline mention as text with @ prefix
				path := mentionPath(c)
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": "@" + path,
				})

			case "image":
				// Skip image blocks
			}
		}

		// Convert agent tool_results to tool_result content blocks
		if msg.Role == "assistant" && len(msg.ToolResults) > 0 {
			for _, tr := range msg.ToolResults {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": tr.ToolUseID,
					"content":     bestToolOutput(tr),
					"is_error":    tr.IsError,
				})
			}
		}

		entry.Message = &transcript.Message{
			Role:    msg.Role,
			Model:   model,
			Content: toContentInterface(contentBlocks),
		}

		entries = append(entries, entry)
	}

	// Aggregate token usage from per-request map
	inputTokens, outputTokens, cacheReads, cacheWrites := aggregateTokenUsage(thread.RequestTokenUsage)

	// Build stats
	stats := transcript.Stats{
		SessionID:         sessionID,
		Model:             model,
		EndTime:           thread.UpdatedAt,
		UserMessages:      userMsgs,
		AssistantMessages: assistantMsgs,
		ToolUses:          toolUses,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		CacheReads:        cacheReads,
		CacheWrites:       cacheWrites,
		ToolCounts:        toolCounts,
		ThinkingBlocks:    thinkingBlocks,
	}

	// Set CWD from project snapshot if available
	if thread.ProjectSnapshot != nil && len(thread.ProjectSnapshot.WorktreeSnapshots) > 0 {
		stats.CWD = thread.ProjectSnapshot.WorktreeSnapshots[0].WorktreePath
	}

	// Set git branch
	if branch := threadBranch(thread); branch != "" {
		stats.GitBranch = branch
	}

	return &transcript.Transcript{
		Entries: entries,
		Stats:   stats,
	}, nil
}

// modelString formats a ZedModel as "provider/model".
func modelString(m *ZedModel) string {
	if m == nil {
		return "unknown"
	}
	if m.Provider == "" {
		return m.Model
	}
	if m.Model == "" {
		return m.Provider
	}
	return m.Provider + "/" + m.Model
}

// threadBranch extracts the git branch from thread metadata.
func threadBranch(t *Thread) string {
	if t.ProjectSnapshot != nil {
		for _, w := range t.ProjectSnapshot.WorktreeSnapshots {
			if w.GitBranch != "" {
				return w.GitBranch
			}
		}
	}
	return t.WorktreeBranch
}

// mentionPath extracts the file path from a mention content block.
func mentionPath(c ZedContent) string {
	if c.MentionURI != nil {
		if c.MentionURI.AbsPath != "" {
			return c.MentionURI.AbsPath
		}
		if c.MentionURI.ThreadName != "" {
			return "thread:" + c.MentionURI.ThreadName
		}
	}
	return "mention"
}

// bestToolOutput returns the best text representation of a tool result.
func bestToolOutput(tr ZedToolResult) string {
	if tr.Content != "" {
		return tr.Content
	}
	return tr.Output
}

// aggregateTokenUsage sums per-request token usage into totals.
func aggregateTokenUsage(usage map[string]TokenUsage) (input, output, reads, writes int) {
	for _, u := range usage {
		input += u.InputTokens
		output += u.OutputTokens
		reads += u.CacheReads
		writes += u.CacheWrites
	}
	return
}

// toContentInterface converts content blocks to the []interface{} format
// expected by transcript.ContentBlocks.
func toContentInterface(blocks []interface{}) interface{} {
	if len(blocks) == 0 {
		return ""
	}
	return blocks
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
