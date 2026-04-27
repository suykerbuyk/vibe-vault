// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package wrapdispatch implements the server-side wrap-executor dispatch loop
// (Phase 3c, Architecture A1 of the wrap-model-tiering plan).
//
// Dispatch runs one tier of the wrap-executor against a previously-prepared
// skeleton: it builds a ToolsRequest exposing two tools to the executor —
// vv_synthesize_wrap_bundle (routed via direct Go call to the caller-supplied
// SynthesizeFunc per OQ-5, NOT a re-entrant MCP roundtrip) and
// wrap_executor_finish (an in-loop terminal signal demoted from a registered
// MCP tool per H4-v3) — drives the multi-turn loop via the injected
// AgenticProvider, captures the executor's wrap_executor_finish call args
// into Outcome.Outputs / Outcome.EscalateReason, and emits stderr-style
// progress lines per LLM tool-call (OQ-6).
//
// The package is pure Go; the MCP adapter lives in
// internal/mcp/tools_wrap_dispatch.go.
package wrapdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// Default safety cap on the executor tool-call loop. The provider's own cap
// is treated as 10 by AnthropicAgentic; we use the same default so a
// pathological model emitting endless tool_use turns is broken consistently.
const defaultMaxIterations = 10

// SynthesizeFunc is the OQ-5 direct-helper signature for the executor's
// vv_synthesize_wrap_bundle tool calls. The dispatch loop routes the
// executor's tool_use to this function rather than re-entering the MCP
// server's tool-dispatch surface. The function must be safe to call multiple
// times per dispatch (the executor may iterate prose drafts before finishing).
type SynthesizeFunc func(ctx context.Context, skeletonJSON, prose json.RawMessage) (bundle json.RawMessage, err error)

// PriorAttempt describes a previous tier's outcome, used to feed escalation
// context into the next tier's user prompt. The Outputs field is permitted
// for future use (no escalation prompt today inlines them — the prompt only
// surfaces the EscalateReason to keep token usage in check).
type PriorAttempt struct {
	Tier           string          `json:"tier"`
	Outputs        json.RawMessage `json:"outputs,omitempty"`
	EscalateReason string          `json:"escalate_reason,omitempty"`
}

// Request is the input to Dispatch.
type Request struct {
	// SkeletonPath is the on-disk path returned by vv_prepare_wrap_skeleton.
	// Carried for diagnostics and progress logging only — Dispatch reads the
	// skeleton bytes from SkeletonJSON.
	SkeletonPath string

	// SkeletonSha256 is the digest from the handle, carried for diagnostics.
	// The MCP adapter performs the compare-and-set guard before populating
	// SkeletonJSON; Dispatch itself does not re-verify.
	SkeletonSha256 string

	// SkeletonJSON is the pre-loaded skeleton body. Required.
	SkeletonJSON json.RawMessage

	// Tier names this attempt's tier (e.g. "haiku", "sonnet", "opus"). Used
	// only for progress logging today; the provider:model resolution lives
	// in the MCP adapter (Phase 4 will read it from config).
	Tier string

	// ProviderModel is the resolved provider:model string (e.g.
	// "anthropic:claude-sonnet-4-6"). Used for progress logging and recorded
	// in the returned Metrics.
	ProviderModel string

	// AgentDefinition is the pre-fetched agent catalogue entry. Required.
	AgentDefinition *agentregistry.AgentDefinition

	// PriorAttempts feeds escalation context into the user prompt. Empty for
	// the first tier; one entry per failed prior tier on subsequent calls.
	PriorAttempts []PriorAttempt

	// Provider is the AgenticProvider that drives the multi-turn loop.
	// Required. Tests inject a mock; production wires AnthropicAgentic.
	Provider llm.AgenticProvider

	// SynthesizeFn is the OQ-5 direct-helper closure that handles the
	// executor's vv_synthesize_wrap_bundle tool calls without re-entering MCP.
	// Required.
	SynthesizeFn SynthesizeFunc

	// ProgressFn receives one line per LLM tool-call invocation. Optional —
	// defaults to a no-op when nil. The MCP adapter wires this to stderr
	// per OQ-6.
	ProgressFn func(line string)

	// MaxIterations caps the tool-call loop. Zero means defaultMaxIterations.
	MaxIterations int
}

// Outcome is the result of a single Dispatch call. Outputs and EscalateReason
// are mutually exclusive: success populates Outputs only; any escalation path
// populates EscalateReason only.
type Outcome struct {
	Outputs        json.RawMessage
	EscalateReason string
	Metrics        DispatchMetrics
}

// DispatchMetrics is the per-tier dispatch summary written to the response
// envelope. Phase 4 will additionally persist these to a metrics log via
// `vv stats wrap`; for v1 they round-trip through the MCP response only.
type DispatchMetrics struct {
	ProviderModel string `json:"provider_model"`
	DurationMs    int64  `json:"duration_ms"`
	InputTokens   int    `json:"input_tokens"`
	OutputTokens  int    `json:"output_tokens"`
	ToolCallCount int    `json:"tool_call_count"`
}

// terminalArgs captures the wrap_executor_finish call arguments emitted by
// the executor at end-of-dispatch. Outputs is RawMessage so the dispatcher
// can pass it through to the orchestrator without parsing the prose schema.
type terminalArgs struct {
	Status  string          `json:"status"`
	Reason  string          `json:"reason,omitempty"`
	Outputs json.RawMessage `json:"outputs,omitempty"`
}

// Dispatch runs one tier of the wrap-executor loop. Returns Outcome with
// Outputs OR EscalateReason (never both). Validation and bookkeeping errors
// surface as the function's error return; provider/model failures are wrapped
// as EscalateReason "provider_error: ..." so the caller can progress to the
// next tier rather than abort the whole dispatch.
func Dispatch(ctx context.Context, req Request) (Outcome, error) {
	if req.Provider == nil {
		return Outcome{}, fmt.Errorf("Request.Provider is required")
	}
	if req.SynthesizeFn == nil {
		return Outcome{}, fmt.Errorf("Request.SynthesizeFn is required")
	}
	if req.AgentDefinition == nil {
		return Outcome{}, fmt.Errorf("Request.AgentDefinition is required")
	}
	if len(req.SkeletonJSON) == 0 {
		return Outcome{}, fmt.Errorf("Request.SkeletonJSON is required")
	}

	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}
	progress := req.ProgressFn
	if progress == nil {
		progress = func(string) {}
	}

	// Compose the executor's user prompt: pretty-printed skeleton, optional
	// prior-attempt escalation context, and the closing instruction. We keep
	// prior-attempt rendering to the escalation reason only (not full prior
	// outputs) so the prompt stays bounded across multi-tier escalation.
	userPrompt, err := buildUserPrompt(req.SkeletonJSON, req.PriorAttempts)
	if err != nil {
		return Outcome{}, fmt.Errorf("build user prompt: %w", err)
	}

	tools := []llm.ToolSpec{
		{
			Name:        "vv_synthesize_wrap_bundle",
			Description: "Validate proposed prose against the cached skeleton. Returns the full bundle.",
			InputSchema: json.RawMessage(synthesizeToolSchema),
		},
		{
			Name:        "wrap_executor_finish",
			Description: "Terminal signal. Call exactly once at end of dispatch. status=\"ok\" with outputs={...} on success; status=\"escalate\" with reason=\"...\" if irreducibly ambiguous.",
			InputSchema: json.RawMessage(finishToolSchema),
		},
	}

	metrics := DispatchMetrics{ProviderModel: req.ProviderModel}
	start := time.Now()

	var terminal *terminalArgs

	executor := func(name string, input json.RawMessage) (json.RawMessage, bool) {
		metrics.ToolCallCount++
		elapsed := time.Since(start).Seconds()
		progress(fmt.Sprintf(
			"dispatch tier=%s model=%s t=%.1fs tool_call=%d name=%s",
			req.Tier, req.ProviderModel, elapsed, metrics.ToolCallCount, name,
		))
		switch name {
		case "vv_synthesize_wrap_bundle":
			// Per the dispatch spec the synth tool input is {prose: <opaque>}.
			// We unpack the wrapper, hand the inner prose blob to the
			// caller-supplied SynthesizeFn, and surface either the bundle or
			// a structured error back to the model so it can iterate.
			var args struct {
				Prose json.RawMessage `json:"prose"`
			}
			if jerr := json.Unmarshal(input, &args); jerr != nil {
				return mustJSON(map[string]string{
					"error": fmt.Sprintf("invalid arguments: %v", jerr),
				}), true
			}
			bundle, sErr := req.SynthesizeFn(ctx, req.SkeletonJSON, args.Prose)
			if sErr != nil {
				return mustJSON(map[string]string{
					"error": sErr.Error(),
				}), true
			}
			return bundle, false
		case "wrap_executor_finish":
			var ta terminalArgs
			if jerr := json.Unmarshal(input, &ta); jerr != nil {
				return mustJSON(map[string]string{
					"error": fmt.Sprintf("invalid arguments: %v", jerr),
				}), true
			}
			terminal = &ta
			return mustJSON(map[string]bool{"acknowledged": true}), false
		default:
			return mustJSON(map[string]string{
				"error": fmt.Sprintf("tool not allowed: %s", name),
			}), true
		}
	}

	toolsReq := llm.ToolsRequest{
		System: req.AgentDefinition.SystemPrompt,
		Messages: []llm.ToolsMessage{
			{
				Role: "user",
				Content: []llm.ContentBlock{
					{Type: "text", Text: userPrompt},
				},
			},
		},
		Tools:         tools,
		MaxIterations: maxIter,
		ToolExecutor:  executor,
	}

	resp, runErr := req.Provider.RunTools(ctx, toolsReq)
	metrics.DurationMs = time.Since(start).Milliseconds()
	if resp != nil {
		metrics.InputTokens = resp.Usage.InputTokens
		metrics.OutputTokens = resp.Usage.OutputTokens
	}

	if runErr != nil {
		return Outcome{
			EscalateReason: "provider_error: " + runErr.Error(),
			Metrics:        metrics,
		}, nil
	}

	if resp != nil && resp.StopReason == "max_tokens" {
		// Loop hit the safety cap or the model itself ran out of tokens.
		// Decision 8 lumps cap-fires under the mcp_tool_error_after_retry
		// family; the more accurate label here is max_iterations_exceeded.
		return Outcome{
			EscalateReason: "max_iterations_exceeded",
			Metrics:        metrics,
		}, nil
	}

	if terminal == nil {
		// Model emitted end_turn without invoking wrap_executor_finish.
		// Decision 8's "missing terminal signal" trigger.
		return Outcome{
			EscalateReason: "missing_terminal_signal",
			Metrics:        metrics,
		}, nil
	}

	switch terminal.Status {
	case "escalate":
		reason := terminal.Reason
		if reason == "" {
			reason = "self_reported_confusion"
		}
		return Outcome{
			EscalateReason: reason,
			Metrics:        metrics,
		}, nil
	case "ok":
		if len(terminal.Outputs) == 0 || string(terminal.Outputs) == "null" {
			return Outcome{
				EscalateReason: "ok_without_outputs",
				Metrics:        metrics,
			}, nil
		}
		return Outcome{
			Outputs: terminal.Outputs,
			Metrics: metrics,
		}, nil
	default:
		return Outcome{
			EscalateReason: fmt.Sprintf("unknown_terminal_status: %q", terminal.Status),
			Metrics:        metrics,
		}, nil
	}
}

// buildUserPrompt assembles the executor's seed user message. The skeleton is
// pretty-printed for human-readability (tokens are not the bottleneck — the
// skeleton tops out at a few KB), prior-attempt reasons are listed inline,
// and the closing instruction directs the model to use the two exposed tools.
func buildUserPrompt(skeleton json.RawMessage, prior []PriorAttempt) (string, error) {
	var pretty bytes
	if err := indentJSON(&pretty, skeleton); err != nil {
		return "", err
	}

	out := "Skeleton:\n```json\n" + pretty.String() + "\n```\n\n"
	if len(prior) > 0 {
		out += "Prior tier attempts:\n"
		for _, p := range prior {
			reason := p.EscalateReason
			if reason == "" {
				reason = "(no reason recorded)"
			}
			out += fmt.Sprintf("  - tier=%s escalated: %s\n", p.Tier, reason)
		}
		out += "\n"
	}
	out += "Draft prose fields per the skeleton, then call wrap_executor_finish(status=\"ok\", outputs={...}). " +
		"If anything is irreducibly ambiguous, call wrap_executor_finish(status=\"escalate\", reason=\"...\")."
	return out, nil
}

// bytes is a minimal stringer-like buffer kept private to avoid pulling in
// the bytes package just for one Indent call. We use json.Indent's []byte
// destination via a short helper.
type bytes struct{ b []byte }

func (b *bytes) String() string { return string(b.b) }
func (b *bytes) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func indentJSON(dst *bytes, src json.RawMessage) error {
	if len(src) == 0 {
		return nil
	}
	// Round-trip through encoding/json's Indent for stable, deterministic
	// formatting that matches what FillBundle / wrapbundlecache produce on
	// disk. We accept any valid JSON (object, array, or scalar) here.
	var raw any
	if err := json.Unmarshal(src, &raw); err != nil {
		return err
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	_, _ = dst.Write(out)
	return nil
}

// mustJSON marshals v into a json.RawMessage. The marshal targets are simple
// map shapes that cannot fail in practice, so any error is treated as a
// programmer mistake — we fall back to a hard-coded error blob so the model
// still receives valid JSON.
func mustJSON(v any) json.RawMessage {
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"error":"internal: marshal failure"}`)
	}
	return out
}

// synthesizeToolSchema is the JSON schema published to the executor for the
// vv_synthesize_wrap_bundle tool. It mirrors Phase 3a's MCP-level tool
// schema MINUS the skeleton_handle field (the dispatch loop already holds
// the skeleton; the model only needs to supply the prose draft).
const synthesizeToolSchema = `{
  "type": "object",
  "properties": {
    "prose": {
      "type": "object",
      "description": "Prose draft to fill into the cached skeleton. Mirrors the per-prose-field shape of vv_synthesize_wrap_bundle (iteration_narrative, iteration_title, prose_body, commit_subject, date, thread_bodies, carried_bodies, capture_summary, capture_tag, capture_decisions, capture_files_changed, capture_open_threads)."
    }
  },
  "required": ["prose"]
}`

// finishToolSchema is the JSON schema published to the executor for the
// in-loop wrap_executor_finish terminal signal (H4-v3 demoted from a
// registered MCP tool). The outputs map mirrors Phase 3a's prose fields plus
// capture_session payload — the dispatcher passes Outputs through verbatim
// so any field the executor populates reaches the orchestrator unchanged.
const finishToolSchema = `{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "enum": ["ok", "escalate"],
      "description": "ok: outputs are filled and ready to apply. escalate: this tier cannot complete; surface reason."
    },
    "reason": {
      "type": "string",
      "description": "Required when status=\"escalate\". Short label (e.g. \"semantic_presence_failure\")."
    },
    "outputs": {
      "type": "object",
      "description": "Required when status=\"ok\". Prose fields + capture_session payload to apply.",
      "properties": {
        "iteration_narrative":   {"type": "string"},
        "iteration_title":       {"type": "string"},
        "prose_body":            {"type": "string"},
        "commit_subject":        {"type": "string"},
        "date":                  {"type": "string"},
        "thread_bodies":         {"type": "object"},
        "carried_bodies":        {"type": "object"},
        "capture_summary":       {"type": "string"},
        "capture_tag":           {"type": "string"},
        "capture_decisions":     {"type": "array", "items": {"type": "string"}},
        "capture_files_changed": {"type": "array", "items": {"type": "string"}},
        "capture_open_threads":  {"type": "array", "items": {"type": "string"}}
      }
    }
  },
  "required": ["status"]
}`
