// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// File tools_wrap_dispatch.go is the thin MCP adapter around
// internal/wrapdispatch.Dispatch (Phase 3c, Architecture A1 — server-side
// dispatch entry point).
//
// The orchestrator (Phase 4 will rewrite commands/wrap.md for this) calls
// vv_wrap_dispatch(skeleton_handle, tier, agent_name, [prior_attempts]) once
// per tier; this handler:
//
//  1. Resolves tier → provider:model. v1 hardcodes the resolution; Phase 4
//     replaces resolveTier() with config-driven lookup. The hardcoded values
//     mirror the recommended_model_class entries shipped in
//     internal/agentregistry/agents/*.md.
//  2. Looks up the agent definition via agentregistry.Lookup() — direct Go
//     call, NOT the vv_get_agent_definition MCP tool. The MCP tool is
//     v2-portability scaffolding for orchestrators that don't embed the
//     vibe-vault binary; v1 always reads the registry in-process.
//  3. Instantiates an llm.AgenticProvider via providerFactory (a test seam:
//     production wires AnthropicAgentic against the live API; tests override
//     the factory to inject a scripted mock).
//  4. Builds a SynthesizeFunc closure that wraps FillBundle directly. Per
//     OQ-5 the executor's vv_synthesize_wrap_bundle tool calls route through
//     this Go function — NOT a re-entrant MCP roundtrip.
//  5. Calls wrapdispatch.Dispatch(ctx, req) with a stderr ProgressFn (OQ-6
//     directive: stdio transport accommodates 1-3 minute durations; emit a
//     progress line per LLM tool-call for operator UX).
//  6. Returns {outputs?, escalate_reason?, dispatch_metrics} to the
//     orchestrator. outputs and escalate_reason are mutually exclusive.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
	"github.com/suykerbuyk/vibe-vault/internal/wrapdispatch"
	"github.com/suykerbuyk/vibe-vault/internal/wrapmetrics"
)

// dispatchLineWriter is a test seam for the dispatch-metrics writer. The
// production implementation is wrapmetrics.WriteDispatchLine; tests
// override it to assert the line shape without touching the filesystem.
//
// Best-effort by design: a write failure logs nothing and does NOT fail
// the dispatch — telemetry loss is preferable to wrap aborts.
var dispatchLineWriter = wrapmetrics.WriteDispatchLine

// outcomeKind classifies a wrapdispatch.Outcome into the DispatchLine
// outcome label vocabulary ("ok" | "escalate" | "error"). Provider-error
// escalations are tagged "error" so dashboards can split infra from
// model-quality failures.
func outcomeKind(o wrapdispatch.Outcome) string {
	if len(o.Outputs) > 0 {
		return "ok"
	}
	if strings.HasPrefix(o.EscalateReason, "provider_error:") {
		return "error"
	}
	return "escalate"
}

// providerFactory builds the llm.AgenticProvider given a "provider:model"
// string and an API key. Production code wires the real Anthropic provider;
// tests override this seam to inject a scripted mock without speaking HTTP.
//
// The seam is a package-level variable (not an injected dependency on the
// Tool struct) because the existing MCP tool factory pattern returns a
// caller-bound Tool with a closed-over Handler — overriding a package-level
// var is the smallest deviation from that pattern.
var providerFactory = defaultProviderFactory

// SetProviderFactoryForTesting overrides the wrap-dispatch provider factory
// for the duration of a test. Pass nil to restore the production default.
// Cross-package test code (e.g. integration tests in test/) calls this to
// inject a scripted mock without speaking HTTP to the real Anthropic API.
//
// This helper is intentionally exported so test packages outside internal/mcp
// can reach the seam; in-package tests override providerFactory directly.
func SetProviderFactoryForTesting(fn func(providerModel, apiKey string) (llm.AgenticProvider, error)) {
	if fn == nil {
		providerFactory = defaultProviderFactory
		return
	}
	providerFactory = fn
}

// defaultProviderFactory is the production provider factory. It supports
// only the "anthropic:<model>" provider:model form for v1; Phase 4 will
// extend this when config schema work lands.
func defaultProviderFactory(providerModel, apiKey string) (llm.AgenticProvider, error) {
	parts := strings.SplitN(providerModel, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid provider:model %q (expected \"<provider>:<model>\")", providerModel)
	}
	provider, model := parts[0], parts[1]
	switch provider {
	case "anthropic":
		return llm.NewAnthropicAgentic("", apiKey, model)
	default:
		return nil, fmt.Errorf("v1 supports only anthropic providers, got %q", provider)
	}
}

// resolveTier maps a tier label to a provider:model string by reading
// the operator's [wrap.tiers] map from config. Phase 4 lifted the
// previously-hardcoded fallback into config; if Tiers is empty or the
// requested tier is missing the error message points the operator at
// the [wrap.tiers] section so they can fix the config rather than
// re-read code.
func resolveTier(cfg config.Config, tier string) (string, error) {
	if len(cfg.Wrap.Tiers) == 0 {
		return "", fmt.Errorf(
			"[wrap.tiers] not configured; add a [wrap.tiers] section to ~/.config/vibe-vault/config.toml")
	}
	if pm, ok := cfg.Wrap.Tiers[tier]; ok {
		return pm, nil
	}
	return "", fmt.Errorf("unknown tier %q (define it in [wrap.tiers])", tier)
}

// dispatchOutputEnvelope is the JSON shape returned to the orchestrator.
// outputs and escalate_reason are mutually exclusive. dispatch_metrics is
// always populated (with at minimum provider_model + duration_ms) so the
// caller can record telemetry even when the tier escalates.
type dispatchOutputEnvelope struct {
	Outputs         json.RawMessage             `json:"outputs,omitempty"`
	EscalateReason  string                      `json:"escalate_reason,omitempty"`
	DispatchMetrics wrapdispatch.DispatchMetrics `json:"dispatch_metrics"`
}

// NewWrapDispatchTool creates the vv_wrap_dispatch MCP tool.
//
// This is Architecture A1's server-side dispatch entry point: the
// orchestrator calls it once per tier, the handler runs the wrap-executor
// loop in-process (LLM tool-use multi-turn) and returns the executor's
// terminal outputs (or escalation reason). Designed for stdio transport's
// 1-3 minute dispatch budget; emits progress lines to stderr per OQ-6.
func NewWrapDispatchTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_wrap_dispatch",
			Description: "Run one tier of the wrap-executor loop server-side (Architecture A1). " +
				"Given a previously-prepared skeleton_handle, a tier label (haiku|sonnet|opus), " +
				"and an agent_name from the in-process agent registry, this tool resolves the " +
				"tier to a provider:model, instantiates an Anthropic agentic provider, runs the " +
				"executor against the cached skeleton with two tools exposed (vv_synthesize_wrap_bundle " +
				"routed via direct Go call to FillBundle, and an in-loop wrap_executor_finish " +
				"terminal signal), and returns either the executor's outputs or an escalation " +
				"reason. dispatch_metrics is always populated. Emits a stderr progress line per " +
				"LLM tool-call. Phase 4 will wire orchestrator-side calls from commands/wrap.md.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"skeleton_handle": {
						"type": "object",
						"description": "{iter, skeleton_path, skeleton_sha256} object returned by vv_prepare_wrap_skeleton.",
						"properties": {
							"iter":            {"type": "integer"},
							"skeleton_path":   {"type": "string"},
							"skeleton_sha256": {"type": "string"}
						},
						"required": ["iter", "skeleton_path", "skeleton_sha256"]
					},
					"tier": {
						"type": "string",
						"description": "Tier label (e.g. haiku|sonnet|opus). The tier->model mapping is read from [wrap.tiers] in ~/.config/vibe-vault/config.toml; operators may define additional tier labels."
					},
					"agent_name": {
						"type": "string",
						"description": "Registered agent name (e.g. \"wrap-executor\") looked up via agentregistry.Lookup."
					},
					"prior_attempts": {
						"type": "array",
						"description": "Optional. Prior tier outcomes for escalation context (only escalate_reason is rendered into the prompt today).",
						"items": {
							"type": "object",
							"properties": {
								"tier":            {"type": "string"},
								"outputs":         {"type": "object"},
								"escalate_reason": {"type": "string"}
							}
						}
					},
					"max_iterations": {
						"type": "integer",
						"description": "Optional safety cap on the executor tool-call loop. Defaults to 10."
					}
				},
				"required": ["skeleton_handle", "tier", "agent_name"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				SkeletonHandle SkeletonHandle              `json:"skeleton_handle"`
				Tier           string                      `json:"tier"`
				AgentName      string                      `json:"agent_name"`
				PriorAttempts  []wrapdispatch.PriorAttempt `json:"prior_attempts"`
				MaxIterations  int                         `json:"max_iterations"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			// Validate skeleton_handle. We require all three fields here even
			// though loadSkeletonByHandle accepts an empty sha — the handle
			// returned by vv_prepare_wrap_skeleton always carries one, and a
			// missing sha would silently disable the compare-and-set guard.
			if args.SkeletonHandle.Iter <= 0 {
				return "", fmt.Errorf("skeleton_handle.iter must be > 0")
			}
			if args.SkeletonHandle.SkeletonPath == "" {
				return "", fmt.Errorf("skeleton_handle.skeleton_path is required")
			}
			if args.SkeletonHandle.SkeletonSHA256 == "" {
				return "", fmt.Errorf("skeleton_handle.skeleton_sha256 is required")
			}
			if args.Tier == "" {
				return "", fmt.Errorf("tier is required")
			}
			if args.AgentName == "" {
				return "", fmt.Errorf("agent_name is required")
			}

			// Read + sha-verify the skeleton file (compare-and-set guard).
			data, err := wrapbundlecache.Read(args.SkeletonHandle.SkeletonPath)
			if err != nil {
				return "", fmt.Errorf("read skeleton: %w", err)
			}
			gotSum := sha256.Sum256(data)
			if hex.EncodeToString(gotSum[:]) != args.SkeletonHandle.SkeletonSHA256 {
				return "", fmt.Errorf("skeleton cache file modified after handle issued (sha mismatch)")
			}

			// Resolve tier → provider:model from config.
			providerModel, err := resolveTier(cfg, args.Tier)
			if err != nil {
				return "", err
			}

			// Look up agent definition directly. NOT vv_get_agent_definition.
			agentDef, err := agentregistry.Lookup(args.AgentName)
			if err != nil {
				return "", err
			}

			// Extract the provider prefix from the tier-resolved
			// "<provider>:<model>" string and resolve its API key via the
			// shared layered resolver: providers.<P>.api_key from config
			// wins, env var is the fallback, both-empty returns an
			// actionable error pointing at vv config set-key + the env var.
			tierProvider, _, _ := strings.Cut(providerModel, ":")
			apiKey, err := llm.ResolveAPIKey(tierProvider, cfg.Providers)
			if err != nil {
				return "", err
			}

			// Instantiate the AgenticProvider via the test-seam factory.
			provider, err := providerFactory(providerModel, apiKey)
			if err != nil {
				return "", fmt.Errorf("provider init: %w", err)
			}

			// Build the OQ-5 direct-helper SynthesizeFunc. The executor's
			// vv_synthesize_wrap_bundle tool calls route through this closure
			// — NOT a re-entrant MCP roundtrip. The closure unmarshals the
			// model's prose payload, hands it to FillBundle, and returns the
			// resulting WrapBundle as JSON.
			synth := func(_ context.Context, skeletonJSON json.RawMessage, prose json.RawMessage) (json.RawMessage, error) {
				var skel WrapSkeleton
				if jerr := json.Unmarshal(skeletonJSON, &skel); jerr != nil {
					return nil, fmt.Errorf("parse skeleton: %w", jerr)
				}
				var pf proseInputArgs
				if len(prose) > 0 {
					if jerr := json.Unmarshal(prose, &pf); jerr != nil {
						return nil, fmt.Errorf("parse prose: %w", jerr)
					}
				}
				bundle := FillBundle(skel, pf.toProseFields())
				return json.Marshal(bundle)
			}

			progress := func(line string) {
				fmt.Fprintln(os.Stderr, "[wrap-dispatch] "+line)
			}

			// Run the dispatch.
			outcome, dispErr := wrapdispatch.Dispatch(context.Background(), wrapdispatch.Request{
				SkeletonPath:    args.SkeletonHandle.SkeletonPath,
				SkeletonSha256:  args.SkeletonHandle.SkeletonSHA256,
				SkeletonJSON:    data,
				Tier:            args.Tier,
				ProviderModel:   providerModel,
				AgentDefinition: agentDef,
				PriorAttempts:   args.PriorAttempts,
				Provider:        provider,
				SynthesizeFn:    synth,
				ProgressFn:      progress,
				MaxIterations:   args.MaxIterations,
			})
			if dispErr != nil {
				return "", fmt.Errorf("dispatch: %w", dispErr)
			}

			// Best-effort dispatch-metrics jsonl emission (Phase 4).
			// Each vv_wrap_dispatch invocation runs ONE tier so each
			// jsonl record carries exactly one TierAttempt; the
			// orchestrator's natural-language ladder logic accumulates
			// the multi-tier picture, and `vv stats wrap` aggregates by
			// iter to reconstruct it. Telemetry loss is preferred to
			// wrap aborts, so writer errors are intentionally swallowed.
			kind := outcomeKind(outcome)
			modelUsed := ""
			if kind == "ok" {
				modelUsed = args.Tier
			}
			line := wrapmetrics.DispatchLine{
				Iter:                   args.SkeletonHandle.Iter,
				TS:                     time.Now().UTC().Format(time.RFC3339),
				AgentDefinitionSha256:  agentDef.Sha256,
				AgentDefinitionVersion: agentDef.Version,
				TierAttempts: []wrapmetrics.TierAttempt{{
					Tier:           args.Tier,
					ProviderModel:  providerModel,
					DurationMs:     outcome.Metrics.DurationMs,
					Outcome:        kind,
					InputTokens:    outcome.Metrics.InputTokens,
					OutputTokens:   outcome.Metrics.OutputTokens,
					EscalateReason: outcome.EscalateReason,
				}},
				ModelUsed:       modelUsed,
				EscalatedFrom:   nil, // orchestrator reconstructs across calls
				TotalDurationMs: outcome.Metrics.DurationMs,
			}
			_ = dispatchLineWriter(line)

			env := dispatchOutputEnvelope{
				Outputs:         outcome.Outputs,
				EscalateReason:  outcome.EscalateReason,
				DispatchMetrics: outcome.Metrics,
			}
			out, err := json.MarshalIndent(env, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal envelope: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
