// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"fmt"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/templates"
)

// templatePrompt builds a Prompt whose body is the verbatim default content
// of an embedded template, looked up by relative path under
// templates/agentctx/. The handler returns the same bytes that
// `vv command get <slug>` writes — single source of truth for slash-command
// bodies regardless of whether the client invokes via MCP prompts/get or the
// CLI shellout.
//
// Used by NewRestartPrompt and NewWrapPrompt; safe to add more prompts
// backed by additional agentctx/commands/*.md templates by calling this
// helper.
func templatePrompt(name, relPath, description string) Prompt {
	return Prompt{
		Definition: PromptDef{
			Name:        name,
			Description: description,
		},
		Handler: func(_ map[string]string) (PromptsGetResult, error) {
			body, ok := templates.New().DefaultContent(relPath)
			if !ok {
				return PromptsGetResult{}, fmt.Errorf("template not found: %s", relPath)
			}
			return PromptsGetResult{
				Description: description,
				Messages: []PromptMessage{
					{
						Role:    "user",
						Content: ContentBlock{Type: "text", Text: string(body)},
					},
				},
			}, nil
		},
	}
}

// NewRestartPrompt creates the vv_restart prompt. Its body is the canonical
// /restart slash-command playbook from
// templates/agentctx/commands/restart.md, delivered verbatim so MCP clients
// (Zed Agent panel, Claude Code, etc.) can drive a session-restore flow
// identical to the in-tree slash command.
func NewRestartPrompt() Prompt {
	return templatePrompt(
		"vv_restart",
		"agentctx/commands/restart.md",
		"Restore full session context: surface handshake, vault sync, orphan sweep, bootstrap, and active-task triage.",
	)
}

// NewWrapPrompt creates the vv_wrap prompt. Its body is the canonical /wrap
// slash-command orchestrator playbook from
// templates/agentctx/commands/wrap.md, delivered verbatim.
func NewWrapPrompt() Prompt {
	return templatePrompt(
		"vv_wrap",
		"agentctx/commands/wrap.md",
		"Wrap the current iteration: classify the work-unit shape, compose the iter narrative inline, and apply mutations to vault and project tree.",
	)
}

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

### How to use this prompt
This is session-start orientation. **Do NOT call vv_capture_session on this
invocation alone.** Wait until the user explicitly wraps up or you have
completed a coherent work unit (a feature, bug fix, refactor, review, or
investigation) and the user has confirmed they are done.

Mid-session invocation of this prompt — for example, an operator pasting
` + "`/vv_session_guidelines`" + ` to refresh context — should not trigger an
immediate capture. Capturing fragments produces low-value notes that
clutter the project history.

### When to capture
Call the vv_capture_session tool when:
- The user explicitly asks you to wrap up, save, capture, or finish
- You finish a coherent work unit AND the user has confirmed they are done
  (e.g., they accept your summary, they say "looks good", they commit, they
  move on to a different task)

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

### Context management (optional)
If you want to keep project context up-to-date as you work, these tools are available:
- vv_update_resume: Update the resume.md current-state and open-threads sections
- vv_append_iteration: Append a narrative to the iteration log when completing a work unit
- vv_manage_task: Create, update, or complete task files in the tasks directory

These are optional — session capture alone is sufficient for most workflows.
`)

	if project != "" {
		fmt.Fprintf(&sb, "\n### Project context\nYou are working on the %q project.\n", project)
	}

	return sb.String()
}
