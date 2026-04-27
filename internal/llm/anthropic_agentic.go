// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// defaultMaxIterations is the safety cap on the tool-call loop applied when
// the caller passes MaxIterations == 0. Ten iterations is plenty for the
// wrap-executor's expected tool-use depth (a few resume reads, a few writes,
// finalize) and small enough that runaway loops never burn meaningful
// money before tripping the breaker.
const defaultMaxIterations = 10

// AnthropicAgentic implements AgenticProvider for the Anthropic Messages
// API tool-use wire format. It shares HTTP plumbing with the single-turn
// Anthropic provider via *anthropicHTTPCore and delegates the text-only
// ChatCompletion path to an embedded *Anthropic — so AnthropicAgentic
// satisfies both Provider and AgenticProvider without duplicating the
// single-turn implementation.
type AnthropicAgentic struct {
	*anthropicHTTPCore
	text *Anthropic // delegates ChatCompletion; shares the same HTTP core
}

// NewAnthropicAgentic creates an AnthropicAgentic provider. The signature
// mirrors NewAnthropic so callers swap one for the other without changing
// argument lists.
func NewAnthropicAgentic(baseURL, apiKey, model string) (*AnthropicAgentic, error) {
	core := newAnthropicHTTPCore(baseURL, apiKey, model, nil)
	return &AnthropicAgentic{
		anthropicHTTPCore: core,
		text: &Anthropic{
			anthropicHTTPCore: core,
		},
	}, nil
}

// Name returns the provider identifier. Identical to Anthropic.Name() so
// Available() and config-driven dispatch don't need to distinguish.
func (a *AnthropicAgentic) Name() string { return "anthropic" }

// ChatCompletion delegates to the embedded *Anthropic so AnthropicAgentic
// satisfies the Provider interface in addition to AgenticProvider. The two
// providers share the same anthropicHTTPCore instance so a single TCP
// connection pool is reused across single-turn and tool-use calls.
func (a *AnthropicAgentic) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	return a.text.ChatCompletion(ctx, req)
}

// RunTools drives the multi-turn tool-use loop against the Anthropic
// Messages API. On each iteration:
//
//  1. Marshal the conversation + tool catalogue into the Anthropic
//     wire-format request body.
//  2. POST it via the shared HTTP core.
//  3. Decode the response. If stop_reason == "tool_use", invoke the
//     caller-supplied ToolExecutor for each tool_use block, append the
//     assistant turn and a fresh user turn carrying the tool_result blocks,
//     and loop.
//  4. Otherwise return the final ToolsResponse with normalised StopReason.
//
// The loop terminates at MaxIterations (treated as defaultMaxIterations
// when the caller passes 0) with StopReason == "max_tokens" and the last
// assistant content — this is the safety-cap-breaker contract the tests
// assert against.
func (a *AnthropicAgentic) RunTools(ctx context.Context, req ToolsRequest) (*ToolsResponse, error) {
	if req.ToolExecutor == nil {
		return nil, fmt.Errorf("ToolsRequest.ToolExecutor is required")
	}
	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}

	model := req.Model
	if model == "" {
		model = a.model
	}

	// Translate the caller's seed conversation into the wire format. We keep
	// our own working slice so we can append assistant + tool_result turns
	// across iterations without mutating the caller's input.
	wireMessages := make([]anthropicAgenticMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		wireMessages = append(wireMessages, toWireMessage(m))
	}

	wireTools := make([]anthropicToolSpec, 0, len(req.Tools))
	for _, t := range req.Tools {
		wireTools = append(wireTools, anthropicToolSpec(t))
	}

	var lastAssistant []anthropicAgenticContentBlock
	var lastUsage UsageStats

	for iter := 0; iter < maxIter; iter++ {
		body := anthropicAgenticRequest{
			Model:     model,
			MaxTokens: 4096,
			System:    req.System,
			Messages:  wireMessages,
			Tools:     wireTools,
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}

		resp, err := a.do(ctx, payload, nil)
		if err != nil {
			return nil, err
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}
		if isTransientStatus(resp.StatusCode) {
			return nil, &TransientError{Err: fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))}
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
		}

		var decoded anthropicAgenticResponse
		if err := json.Unmarshal(respBody, &decoded); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		if decoded.Error != nil {
			return nil, fmt.Errorf("API error: %s", decoded.Error.Message)
		}

		lastAssistant = decoded.Content
		lastUsage = UsageStats{
			InputTokens:  decoded.Usage.InputTokens,
			OutputTokens: decoded.Usage.OutputTokens,
		}

		if decoded.StopReason != "tool_use" {
			return &ToolsResponse{
				StopReason: normalizeStopReason(decoded.StopReason),
				Content:    fromWireBlocks(decoded.Content),
				Usage:      lastUsage,
			}, nil
		}

		// stop_reason == "tool_use" — append assistant turn (verbatim) and
		// dispatch every tool_use block in this turn before looping.
		wireMessages = append(wireMessages, anthropicAgenticMessage{
			Role:    "assistant",
			Content: decoded.Content,
		})

		toolResults := make([]anthropicAgenticContentBlock, 0)
		for _, block := range decoded.Content {
			if block.Type != "tool_use" {
				continue
			}
			output, isError := req.ToolExecutor(block.Name, block.Input)
			// Anthropic requires tool_result.content to be a JSON string or a
			// list of content blocks — raw JSON objects/arrays are rejected with
			// a 400. Re-encode non-string executor output as a JSON string so
			// the wire format is valid regardless of what the tool returns.
			toolResultContent := output
			if len(output) > 0 && output[0] != '"' {
				toolResultContent, _ = json.Marshal(string(output))
			}
			toolResults = append(toolResults, anthropicAgenticContentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   toolResultContent,
				IsError:   isError,
			})
		}
		wireMessages = append(wireMessages, anthropicAgenticMessage{
			Role:    "user",
			Content: toolResults,
		})
	}

	// Loop hit the safety cap — surface the last assistant content with the
	// max_tokens stop reason so callers can detect runaway loops.
	return &ToolsResponse{
		StopReason: "max_tokens",
		Content:    fromWireBlocks(lastAssistant),
		Usage:      lastUsage,
	}, nil
}

// normalizeStopReason maps Anthropic's wire-level stop_reason values to the
// public surface's three-valued enum. "end_turn" and "stop_sequence" both
// collapse to "stop"; "max_tokens" passes through; "tool_use" passes through
// (only observed as a terminal value when the loop exits abnormally — the
// happy path recurses on tool_use, never returns it). Any unrecognised
// value falls back to "stop" rather than leaking provider-specific names.
func normalizeStopReason(wire string) string {
	switch wire {
	case "end_turn", "stop_sequence", "":
		return "stop"
	case "max_tokens":
		return "max_tokens"
	case "tool_use":
		return "tool_use"
	default:
		return "stop"
	}
}

// toWireMessage translates a public ToolsMessage into the Anthropic wire
// format. Role "tool" is a portability fiction — Anthropic encodes tool
// results as user-role messages with tool_result content blocks — so we
// rewrite it to "user" here. text/tool_use/tool_result blocks all
// round-trip through the conversion pair.
func toWireMessage(m ToolsMessage) anthropicAgenticMessage {
	role := m.Role
	if role == "tool" {
		role = "user"
	}
	wire := anthropicAgenticMessage{Role: role, Content: make([]anthropicAgenticContentBlock, 0, len(m.Content))}
	for _, b := range m.Content {
		wire.Content = append(wire.Content, toWireBlock(b))
	}
	return wire
}

// toWireBlock converts one public ContentBlock into wire format. Empty raw
// JSON fields default to a "{}" object for tool_use input and a "" string
// for tool_result content, matching what Anthropic accepts.
func toWireBlock(b ContentBlock) anthropicAgenticContentBlock {
	switch b.Type {
	case "text":
		return anthropicAgenticContentBlock{Type: "text", Text: b.Text}
	case "tool_use":
		input := b.ToolInput
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		return anthropicAgenticContentBlock{
			Type:  "tool_use",
			ID:    b.ToolUseID,
			Name:  b.ToolName,
			Input: input,
		}
	case "tool_result":
		content := b.ToolResult
		return anthropicAgenticContentBlock{
			Type:      "tool_result",
			ToolUseID: b.ToolUseID,
			Content:   content,
			IsError:   b.IsError,
		}
	default:
		return anthropicAgenticContentBlock{Type: b.Type, Text: b.Text}
	}
}

// fromWireBlocks converts a wire-format content slice back into the public
// ContentBlock shape. Used on the response path to expose the model's
// terminal turn to the caller.
func fromWireBlocks(blocks []anthropicAgenticContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, ContentBlock{Type: "text", Text: b.Text})
		case "tool_use":
			out = append(out, ContentBlock{
				Type:      "tool_use",
				ToolUseID: b.ID,
				ToolName:  b.Name,
				ToolInput: b.Input,
			})
		case "tool_result":
			out = append(out, ContentBlock{
				Type:       "tool_result",
				ToolUseID:  b.ToolUseID,
				ToolResult: b.Content,
				IsError:    b.IsError,
			})
		default:
			out = append(out, ContentBlock{Type: b.Type, Text: b.Text})
		}
	}
	return out
}

// Wire-format types for the tool-use Messages API.
//
// These are intentionally separate from anthropicMessage / anthropicRequest
// in anthropic.go: the single-turn provider uses a {role, content string}
// message shape, while tool-use requires {role, content []ContentBlock}.
// Sharing one type would force the simpler path through unnecessary block
// marshalling on every text-only call.

type anthropicAgenticRequest struct {
	Model     string                     `json:"model"`
	MaxTokens int                        `json:"max_tokens"`
	System    string                     `json:"system,omitempty"`
	Messages  []anthropicAgenticMessage  `json:"messages"`
	Tools     []anthropicToolSpec        `json:"tools,omitempty"`
}

type anthropicAgenticMessage struct {
	Role    string                          `json:"role"`
	Content []anthropicAgenticContentBlock  `json:"content"`
}

type anthropicAgenticContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`         // tool_use only
	Name      string          `json:"name,omitempty"`       // tool_use only
	Input     json.RawMessage `json:"input,omitempty"`      // tool_use only
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result only
	Content   json.RawMessage `json:"content,omitempty"`    // tool_result only
	IsError   bool            `json:"is_error,omitempty"`   // tool_result only
}

type anthropicToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicAgenticResponse struct {
	Content    []anthropicAgenticContentBlock `json:"content"`
	StopReason string                         `json:"stop_reason"`
	Usage      anthropicAgenticUsage          `json:"usage"`
	Error      *anthropicError                `json:"error,omitempty"`
}

type anthropicAgenticUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
