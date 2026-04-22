// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"fmt"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/prose"
)

const (
	proseFillerMaxChars = 120
	proseUserMaxChars   = 500
)

// ExtractDialogue builds a prose.Dialogue from a Zed thread.
// User messages extract Text and inline Mentions as @path.
// Agent messages extract Text only (skip Thinking, ToolUse details).
func ExtractDialogue(thread *Thread) *prose.Dialogue {
	if thread == nil || len(thread.Messages) == 0 {
		return nil
	}

	// Group messages into sections by user request boundaries.
	var sections []prose.Section
	var currentElements []prose.Element
	var currentUserRequest string

	for _, msg := range thread.Messages {
		switch msg.Role {
		case "user":
			// New user message starts a new section (if we have accumulated elements).
			if len(currentElements) > 0 {
				sections = append(sections, prose.Section{
					UserRequest: currentUserRequest,
					Elements:    currentElements,
				})
				currentElements = nil
				currentUserRequest = ""
			}

			text := extractUserText(msg)
			if text == "" {
				continue
			}

			if currentUserRequest == "" {
				currentUserRequest = truncateStr(firstLineStr(text), 120)
			}

			if len(text) > proseUserMaxChars {
				text = text[:proseUserMaxChars] + " [...]"
			}

			currentElements = append(currentElements, prose.Element{
				Turn: &prose.Turn{Role: "user", Text: text},
			})

		case "assistant":
			elems := processAssistantMessage(msg)
			currentElements = append(currentElements, elems...)
		}
	}

	// Flush final section
	if len(currentElements) > 0 {
		sections = append(sections, prose.Section{
			UserRequest: currentUserRequest,
			Elements:    currentElements,
		})
	}

	if len(sections) == 0 {
		return nil
	}

	return &prose.Dialogue{Sections: sections}
}

// extractUserText builds text from user message content blocks.
func extractUserText(msg ZedMessage) string {
	var parts []string
	for _, c := range msg.Content {
		switch c.Type {
		case "text":
			if t := strings.TrimSpace(c.Text); t != "" {
				parts = append(parts, t)
			}
		case "mention":
			path := mentionPath(c)
			parts = append(parts, "@"+path)
		}
	}
	return strings.Join(parts, " ")
}

// processAssistantMessage extracts prose elements from an assistant message.
func processAssistantMessage(msg ZedMessage) []prose.Element {
	var textParts []string
	var toolUses []ZedContent
	for _, c := range msg.Content {
		switch c.Type {
		case "text":
			if t := strings.TrimSpace(c.Text); t != "" {
				textParts = append(textParts, t)
			}
		case "tool_use":
			toolUses = append(toolUses, c)
			// Skip thinking blocks
		}
	}

	fullText := strings.Join(textParts, "\n\n")
	hasToolUse := len(toolUses) > 0

	var elements []prose.Element

	// Filler filter: short text + tool_use present → skip text
	if hasToolUse && len(fullText) < proseFillerMaxChars {
		// Don't emit the text turn, only markers
	} else if fullText != "" {
		elements = append(elements, prose.Element{
			Turn: &prose.Turn{Role: "assistant", Text: fullText},
		})
	}

	// Generate markers from tool uses
	for _, tu := range toolUses {
		marker := classifyToolMarker(tu, msg.ToolResults)
		if marker != nil {
			elements = append(elements, prose.Element{Marker: marker})
		}
	}

	return elements
}

// classifyToolMarker generates a brief marker for a Zed tool use.
func classifyToolMarker(c ZedContent, toolResults map[string]ZedToolResult) *prose.Marker {
	canonical := NormalizeTool(c.ToolName)
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
		return &prose.Marker{Text: fmt.Sprintf("Created `%s`", shortenPath(path))}

	case "Edit":
		path := inputStr(c.Input, "file_path")
		if path == "" {
			path = inputStr(c.Input, "path")
		}
		return &prose.Marker{Text: fmt.Sprintf("Modified `%s`", shortenPath(path))}

	case "Bash":
		cmd := inputStr(c.Input, "command")
		if cmd == "" {
			cmd = inputStr(c.Input, "cmd")
		}
		return classifyBashToolMarker(cmd, isErr)

	default:
		if isErr {
			return &prose.Marker{Text: fmt.Sprintf("Error: %s failed", canonical)}
		}
		return nil
	}
}

// classifyBashToolMarker generates a marker for a bash tool use.
func classifyBashToolMarker(cmd string, isErr bool) *prose.Marker {
	lower := strings.ToLower(cmd)

	if isTestCmd(lower) {
		status := "success"
		if isErr {
			status = "failed"
		}
		return &prose.Marker{Text: fmt.Sprintf("Ran tests (%s)", status)}
	}
	if strings.Contains(lower, "git commit") {
		msg := extractCommitMsg(cmd)
		if msg != "" {
			return &prose.Marker{Text: fmt.Sprintf("Committed: \"%s\"", truncateStr(msg, 60))}
		}
		return &prose.Marker{Text: "Committed changes"}
	}
	if strings.Contains(lower, "git push") {
		return &prose.Marker{Text: "Pushed to remote"}
	}
	return nil
}
