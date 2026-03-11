// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"fmt"
	"strings"
)

// NewSessionGuidelinesPrompt creates the vv_session_guidelines prompt.
// When requested by an MCP client, it returns instructions that tell the
// agent how and when to call vv_capture_session.
func NewSessionGuidelinesPrompt() Prompt {
	return Prompt{
		Definition: PromptDef{
			Name:        "vv_session_guidelines",
			Description: "Guidelines for recording development sessions with vibe-vault. Include this at session start to enable automatic session capture.",
			Arguments: []PromptArg{
				{
					Name:        "project",
					Description: "Project name for context-specific guidance.",
					Required:    false,
				},
			},
		},
		Handler: func(args map[string]string) (PromptsGetResult, error) {
			text := sessionGuidelinesText(args["project"])
			return PromptsGetResult{
				Description: "vibe-vault session capture guidelines",
				Messages: []PromptMessage{
					{
						Role:    "user",
						Content: ContentBlock{Type: "text", Text: text},
					},
				},
			}, nil
		},
	}
}

func sessionGuidelinesText(project string) string {
	var sb strings.Builder
	sb.WriteString(`## vibe-vault Session Capture

You have access to the vibe-vault MCP server for recording development sessions.

### When to capture
Call the vv_capture_session tool when:
- You are finishing a work session or conversation
- The user asks you to wrap up, save, or capture the session
- You have completed a significant unit of work

### How to capture
Call vv_capture_session with:
- summary (required): 2-3 sentences describing what was accomplished
- tag: one of: implementation, debugging, refactor, exploration, review, docs, planning
- decisions: key technical decisions made (array of strings)
- files_changed: files you created or modified (array of strings)
- open_threads: unresolved items or follow-up work (array of strings)
- model: your model identifier if known

### Quality guidelines
- Write summaries that would help a developer resuming this work tomorrow
- Capture decisions with enough context to understand the "why"
- List open threads as actionable items, not vague notes
`)

	if project != "" {
		fmt.Fprintf(&sb, "\n### Project context\nYou are working on the %q project.\n", project)
	}

	return sb.String()
}
