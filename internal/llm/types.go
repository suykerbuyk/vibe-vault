// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
)

// Provider abstracts an LLM chat completion backend.
// Implementations exist for OpenAI-compatible APIs, Anthropic, and Google Gemini.
//
// Direction-C Phase 4 retired AgenticProvider (multi-turn tool-use) and
// its supporting types (ToolsRequest, ToolsResponse, ToolsMessage,
// ContentBlock, ToolSpec, ToolExecutor) — they were exclusively consumed
// by the dispatch ladder, which is gone. Future agentic features should
// re-introduce as needed.
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
