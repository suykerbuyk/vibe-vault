// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"encoding/json"
	"fmt"
	"time"
)

// Thread represents a Zed agent panel thread, combining DB-level fields
// (from the threads table) with the zstd-decompressed JSON payload.
type Thread struct {
	// DB-sourced fields (set after SQL scan, not in JSON payload).
	// Note: worktree_branch and parent_id are always NULL in current Zed versions
	// but parsed for forward compatibility.
	ID             string
	Summary        string
	UpdatedAt      time.Time
	WorktreeBranch string
	ParentID       string

	// JSON payload fields.
	Title             string                `json:"title"`
	Model             *ZedModel             `json:"model"`
	Messages          []ZedMessage          `json:"-"` // custom unmarshal
	RawMessages       []json.RawMessage     `json:"messages"`
	DetailedSummary   *string               `json:"detailed_summary"`
	ProjectSnapshot   *ProjectSnapshot      `json:"initial_project_snapshot"`
	RequestTokenUsage map[string]TokenUsage `json:"request_token_usage"`
	Version           string                `json:"version"`
	CompletionMode    *string               `json:"completion_mode"`
	Profile           *string               `json:"profile"`
}

// ZedMessage represents a single message in a thread.
// Zed uses Rust-style enum serialization: {"User": {...}} or {"Agent": {...}}.
type ZedMessage struct {
	Role    string // "user" or "assistant" (normalized from User/Agent)
	Content []ZedContent

	// Agent-only fields.
	ToolResults map[string]ZedToolResult // keyed by tool_use_id

	// User-only fields.
	UserID string // message UUID
}

// ZedContent is a polymorphic content block.
// Zed uses enum discriminators: {"Text": "..."}, {"ToolUse": {...}}, etc.
type ZedContent struct {
	Type string // "text", "thinking", "tool_use", "mention", "image"

	// text
	Text string

	// thinking
	Thinking  string
	Signature string

	// tool_use
	ToolName string
	ToolID   string
	Input    any
	RawInput string

	// mention
	MentionURI     *MentionURI
	MentionContent string
}

// MentionURI represents a file/selection/thread reference in a user message.
type MentionURI struct {
	Type    string // "file", "selection", "thread"
	AbsPath string
	// Selection-specific
	LineStart int
	LineEnd   int
	// Thread-specific
	ThreadID   string
	ThreadName string
}

// ZedToolResult pairs a tool_use ID with its result from the Agent message.
type ZedToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	ToolName  string `json:"tool_name"`
	IsError   bool   `json:"is_error"`
	Content   string // extracted from {"Text": "..."} wrapper
	Output    string // raw output text (may be nested)
}

// TokenUsage tracks token consumption for a single request.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheReads   int `json:"cache_read_input_tokens"`
	CacheWrites  int `json:"cache_creation_input_tokens"`
}

// ZedModel identifies the model used in a thread.
type ZedModel struct {
	Provider string `json:"provider"` // e.g. "anthropic", "xai"
	Model    string `json:"model"`    // e.g. "claude-sonnet-4-5-20250514"
}

// ProjectSnapshot captures the workspace state at thread creation.
type ProjectSnapshot struct {
	WorktreeSnapshots []WorktreeSnapshot `json:"worktree_snapshots"`
	Timestamp         string             `json:"timestamp"`
}

// WorktreeSnapshot captures a single worktree's state.
type WorktreeSnapshot struct {
	WorktreePath string `json:"abs_path"`
	GitBranch    string `json:"branch"`
	GitSHA       string `json:"sha"`
	Diff         string `json:"diff"`
}

// --- Custom JSON unmarshaling for Rust-style enums ---

// UnmarshalMessages parses the Rust-style enum message array into ZedMessages.
func UnmarshalMessages(rawMessages []json.RawMessage) ([]ZedMessage, error) {
	var messages []ZedMessage
	for i, raw := range rawMessages {
		msg, err := unmarshalMessage(raw)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", i, err)
		}
		if msg != nil {
			messages = append(messages, *msg)
		}
	}
	return messages, nil
}

// unmarshalMessage parses a single {"User": {...}} or {"Agent": {...}} envelope.
func unmarshalMessage(raw json.RawMessage) (*ZedMessage, error) {
	// Try as string first (e.g. "Resume")
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return nil, nil // skip string markers like "Resume"
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}

	if userData, ok := envelope["User"]; ok {
		return unmarshalUserMessage(userData)
	}
	if agentData, ok := envelope["Agent"]; ok {
		return unmarshalAgentMessage(agentData)
	}

	return nil, nil // unknown message type, skip
}

func unmarshalUserMessage(data json.RawMessage) (*ZedMessage, error) {
	var user struct {
		ID      string            `json:"id"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}

	content, err := unmarshalContentBlocks(user.Content)
	if err != nil {
		return nil, err
	}

	return &ZedMessage{
		Role:    "user",
		Content: content,
		UserID:  user.ID,
	}, nil
}

func unmarshalAgentMessage(data json.RawMessage) (*ZedMessage, error) {
	var agent struct {
		Content     []json.RawMessage          `json:"content"`
		ToolResults map[string]json.RawMessage `json:"tool_results"`
	}
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("unmarshal agent: %w", err)
	}

	content, err := unmarshalContentBlocks(agent.Content)
	if err != nil {
		return nil, err
	}

	toolResults := make(map[string]ZedToolResult)
	for id, raw := range agent.ToolResults {
		var tr struct {
			ToolUseID string          `json:"tool_use_id"`
			ToolName  string          `json:"tool_name"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
			Output    json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(raw, &tr); err != nil {
			continue
		}
		result := ZedToolResult{
			ToolUseID: tr.ToolUseID,
			ToolName:  tr.ToolName,
			IsError:   tr.IsError,
		}
		result.Content = extractTextFromEnum(tr.Content)
		result.Output = extractOutputText(tr.Output)
		toolResults[id] = result
	}

	return &ZedMessage{
		Role:        "assistant",
		Content:     content,
		ToolResults: toolResults,
	}, nil
}

// unmarshalContentBlocks parses Rust-style enum content blocks.
func unmarshalContentBlocks(raws []json.RawMessage) ([]ZedContent, error) {
	var blocks []ZedContent
	for _, raw := range raws {
		block, err := unmarshalContentBlock(raw)
		if err != nil {
			continue // skip unparseable blocks
		}
		if block != nil {
			blocks = append(blocks, *block)
		}
	}
	return blocks, nil
}

// unmarshalContentBlock parses {"Text": "..."}, {"ToolUse": {...}}, etc.
func unmarshalContentBlock(raw json.RawMessage) (*ZedContent, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}

	// Text: {"Text": "string value"}
	if textRaw, ok := envelope["Text"]; ok {
		var text string
		if err := json.Unmarshal(textRaw, &text); err != nil {
			return nil, err
		}
		return &ZedContent{Type: "text", Text: text}, nil
	}

	// Thinking: {"Thinking": {"text": "...", "signature": "..."}}
	if thinkRaw, ok := envelope["Thinking"]; ok {
		var think struct {
			Text      string `json:"text"`
			Signature string `json:"signature"`
		}
		if err := json.Unmarshal(thinkRaw, &think); err != nil {
			return nil, err
		}
		return &ZedContent{Type: "thinking", Thinking: think.Text, Signature: think.Signature}, nil
	}

	// ToolUse: {"ToolUse": {"id": "...", "name": "...", "input": {...}, "raw_input": "..."}}
	if tuRaw, ok := envelope["ToolUse"]; ok {
		var tu struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
			RawInput string          `json:"raw_input"`
		}
		if err := json.Unmarshal(tuRaw, &tu); err != nil {
			return nil, err
		}
		var input any
		if len(tu.Input) > 0 {
			_ = json.Unmarshal(tu.Input, &input)
		}
		return &ZedContent{
			Type:     "tool_use",
			ToolID:   tu.ID,
			ToolName: tu.Name,
			Input:    input,
			RawInput: tu.RawInput,
		}, nil
	}

	// Mention: {"Mention": {"uri": <URI enum>, "content": "..."}}
	if mentionRaw, ok := envelope["Mention"]; ok {
		var mention struct {
			URI     json.RawMessage `json:"uri"`
			Content string          `json:"content"`
		}
		if err := json.Unmarshal(mentionRaw, &mention); err != nil {
			return nil, err
		}
		uri := parseMentionURI(mention.URI)
		return &ZedContent{
			Type:           "mention",
			MentionURI:     uri,
			MentionContent: mention.Content,
		}, nil
	}

	// Image: {"Image": {...}} — skip for now
	if _, ok := envelope["Image"]; ok {
		return &ZedContent{Type: "image"}, nil
	}

	return nil, nil
}

// parseMentionURI parses the URI enum: {"File": {"abs_path": "..."}}, {"Selection": {...}}, {"Thread": {...}}
func parseMentionURI(raw json.RawMessage) *MentionURI {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}

	if fileRaw, ok := envelope["File"]; ok {
		var f struct {
			AbsPath string `json:"abs_path"`
		}
		if err := json.Unmarshal(fileRaw, &f); err == nil {
			return &MentionURI{Type: "file", AbsPath: f.AbsPath}
		}
	}

	if selRaw, ok := envelope["Selection"]; ok {
		var s struct {
			AbsPath   string `json:"abs_path"`
			LineRange struct {
				Start int `json:"start"`
				End   int `json:"end"`
			} `json:"line_range"`
		}
		if err := json.Unmarshal(selRaw, &s); err == nil {
			return &MentionURI{
				Type:      "selection",
				AbsPath:   s.AbsPath,
				LineStart: s.LineRange.Start,
				LineEnd:   s.LineRange.End,
			}
		}
	}

	if threadRaw, ok := envelope["Thread"]; ok {
		var t struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(threadRaw, &t); err == nil {
			return &MentionURI{Type: "thread", ThreadID: t.ID, ThreadName: t.Name}
		}
	}

	return nil
}

// extractTextFromEnum extracts text from {"Text": "..."} wrapper or plain string.
func extractTextFromEnum(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try as {"Text": "..."}
	var envelope map[string]string
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if text, ok := envelope["Text"]; ok {
			return text
		}
	}
	// Try as plain string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// extractOutputText extracts text from tool result output (string, {"Text": "..."}, or complex object).
func extractOutputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try as plain string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try as {"Text": "..."}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if textRaw, ok := envelope["Text"]; ok {
			var text string
			if err := json.Unmarshal(textRaw, &text); err == nil {
				return text
			}
		}
	}
	// Complex object — return raw JSON for potential future use
	return string(raw)
}
