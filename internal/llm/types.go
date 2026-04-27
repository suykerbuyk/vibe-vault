// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"encoding/json"
)

// Provider abstracts an LLM chat completion backend.
// Implementations exist for OpenAI-compatible APIs, Anthropic, and Google Gemini.
type Provider interface {
	// ChatCompletion sends a single chat completion request.
	ChatCompletion(ctx context.Context, req Request) (*Response, error)

	// Name returns the provider name (e.g. "openai", "anthropic", "google").
	Name() string
}

// Request holds the parameters for a chat completion call.
type Request struct {
	Model       string
	System      string  // system prompt
	UserPrompt  string  // user message
	Temperature float64 // 0.0–1.0
	JSONMode    bool    // request JSON-formatted output
}

// Response holds the result of a chat completion call.
type Response struct {
	Content string // raw text response from the model
}

// AgenticProvider extends Provider with multi-turn tool-use support.
// Implementations drive the tool-call loop: they handle the model's tool_use
// blocks, dispatch them to the caller-supplied ToolExecutor, fold the
// results back into the conversation, and iterate until the model emits a
// terminal stop reason or the safety cap is hit. Tool execution itself is
// caller-supplied — the provider never touches MCP, RPC, or any other tool
// transport directly.
type AgenticProvider interface {
	Provider
	RunTools(ctx context.Context, req ToolsRequest) (*ToolsResponse, error)
}

// ToolsRequest is the input to AgenticProvider.RunTools. The caller supplies
// the conversation seed (System + Messages), the tool catalogue (Tools), the
// dispatcher (ToolExecutor), and an optional MaxIterations safety cap. A
// zero MaxIterations is treated as 10 by implementations.
type ToolsRequest struct {
	Model         string
	System        string
	Messages      []ToolsMessage
	Tools         []ToolSpec
	MaxIterations int          // safety cap on the tool-call loop; 0 means default (10)
	ToolExecutor  ToolExecutor // caller-supplied dispatcher
}

// ToolsMessage is a single conversation turn carrying mixed content blocks.
// Role is "user" | "assistant" | "tool". Content blocks may interleave plain
// text with tool_use (assistant) and tool_result (user) blocks per the
// Anthropic Messages API tool-use wire format.
type ToolsMessage struct {
	Role    string         // "user" | "assistant" | "tool"
	Content []ContentBlock // mixed text + tool_use + tool_result
}

// ContentBlock is a polymorphic record covering the three block types the
// provider needs to round-trip through the conversation. Fields are populated
// based on Type — readers should switch on Type before reading payload
// fields.
type ContentBlock struct {
	Type       string          // "text" | "tool_use" | "tool_result"
	Text       string          // when Type == "text"
	ToolUseID  string          // when Type == "tool_use" or "tool_result"
	ToolName   string          // when Type == "tool_use"
	ToolInput  json.RawMessage // when Type == "tool_use"
	ToolResult json.RawMessage // when Type == "tool_result"
	IsError    bool            // when Type == "tool_result"
}

// ToolSpec describes one tool the model is allowed to call. InputSchema is
// the raw JSON Schema describing the tool's input shape (forwarded verbatim
// to the provider wire format — Anthropic, OpenAI, and Google all accept
// JSON Schema with minor variations).
type ToolSpec struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON schema for tool input
}

// ToolExecutor is the caller-supplied dispatcher that runs a tool call. The
// provider invokes it once per tool_use block emitted by the model. The
// returned output is forwarded as the tool_result payload; isError == true
// is forwarded as the wire-level "is_error" flag so the model can recover
// from a failed tool invocation in subsequent turns.
type ToolExecutor func(name string, input json.RawMessage) (output json.RawMessage, isError bool)

// UsageStats reports input/output token counts for a single provider call.
// Fields are zero when the provider does not return usage data.
type UsageStats struct {
	InputTokens  int
	OutputTokens int
}

// ToolsResponse is the terminal output of AgenticProvider.RunTools. StopReason
// is normalised to one of "stop" (the model emitted end_turn or stop_sequence),
// "tool_use" (loop exited mid-iteration — only happens via abnormal termination,
// not on success), or "max_tokens" (the safety cap fired or the model itself
// hit a token cap). Content holds the final assistant turn's blocks.
type ToolsResponse struct {
	StopReason string // "stop" | "tool_use" | "max_tokens"
	Content    []ContentBlock
	Usage      UsageStats
}
