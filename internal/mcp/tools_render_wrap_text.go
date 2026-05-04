// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// marshalIterStateNoHTMLEscape encodes the iter_state struct as
// indented JSON WITHOUT Go's default HTML-escaping pass. The pretty-
// printed JSON appears in the LLM prompt verbatim — operators and
// the LLM both need to see the literal `<`, `>`, `&` characters
// (D2 regression: prove vibe-vault is not the source of XML mangling).
func marshalIterStateNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encoder appends a trailing newline; strip it so the prompt
	// substitution does not introduce a stray blank line.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Wrap-render kind discriminators.
const (
	WrapRenderKindIterNarrative              = "iter_narrative"
	WrapRenderKindCommitMsg                  = "commit_msg"
	WrapRenderKindIterNarrativeAndCommitMsg  = "iter_narrative_and_commit_msg"
)

// wrapRenderIterStateInput is the iter_state portion of the request.
// Fields mirror the Direction-C D3 schema: server-minimal facts the
// slash command computed from git/filesystem and bundled into the call.
type wrapRenderIterStateInput struct {
	IterN                int                       `json:"iter_n"`
	Branch               string                    `json:"branch"`
	LastIterAnchorSha    string                    `json:"last_iter_anchor_sha"`
	CommitsSinceLastIter []wrapRenderCommitInput   `json:"commits_since_last_iter"`
	FilesChanged         []string                  `json:"files_changed"`
	TaskDeltas           wrapRenderTaskDeltasInput `json:"task_deltas"`
	TestCounts           wrapRenderTestCountsInput `json:"test_counts"`
}

// wrapRenderCommitInput is one (sha, subject) pair the slash command
// derived from `git log <anchor>..HEAD`.
type wrapRenderCommitInput struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// wrapRenderTaskDeltasInput is the slash-command-supplied task delta
// triple (added/retired/cancelled task slugs).
type wrapRenderTaskDeltasInput struct {
	Added     []string `json:"added"`
	Retired   []string `json:"retired"`
	Cancelled []string `json:"cancelled"`
}

// wrapRenderTestCountsInput is the slash-command-supplied test count
// triple. Lint count of 0 is the goal.
type wrapRenderTestCountsInput struct {
	Unit        int `json:"unit"`
	Integration int `json:"integration"`
	Lint        int `json:"lint"`
}

// wrapRenderProjectContextInput is the parallel-fetched project_context
// bundle: resume_state from vv_get_resume, recent_iterations from
// vv_get_iterations, open_threads parsed/derived, friction_trends from
// vv_get_friction_trends. The raw JSON pass-through preserves voice and
// back-reference fidelity for the LLM.
type wrapRenderProjectContextInput struct {
	ResumeState       string          `json:"resume_state"`
	RecentIterations  string          `json:"recent_iterations"`
	OpenThreads       []string        `json:"open_threads"`
	FrictionTrends    json.RawMessage `json:"friction_trends"`
}

// wrapRenderRequest is the full vv_render_wrap_text request shape.
//
// VaultSideNarrativeSeed is an optional orchestrator-supplied prose
// field that the renderer treats as load-bearing ground truth in the
// narrative_body and summary. It carries context that does not live in
// the commit graph (verification milestones, multi-iter dispatch arcs,
// post-merge reconciliation framing, carried-thread instance counts).
// Empty / omitted is the default; non-empty seed substitutes into the
// `iter_narrative` and `iter_narrative_and_commit_msg` prompt templates
// per D3 of the vault-side-narrative-seed task. The `omitempty` JSON
// tag pins empty-vs-omitted equivalence per L3 (both render as the
// `(none provided)` placeholder per MC1). Hard-errors at validation
// when supplied with kind=commit_msg per D2.
type wrapRenderRequest struct {
	Kind                   string                        `json:"kind"`
	Tier                   string                        `json:"tier"`
	ProjectName            string                        `json:"project_name"`
	IterState              wrapRenderIterStateInput      `json:"iter_state"`
	ProjectContext         wrapRenderProjectContextInput `json:"project_context"`
	VaultSideNarrativeSeed string                        `json:"vault_side_narrative_seed,omitempty"`
}

// wrapRenderResponse is the rendered prose returned to the slash
// command. Fields are populated based on the kind discriminator: the
// narrative_* pair for iter_narrative, the commit_* pair for
// commit_msg, and all four for iter_narrative_and_commit_msg.
//
// Summary is the optional 1-sentence digest (≤200 chars, no newlines)
// emitted for narrative kinds so the slash command can pass it back to
// vv_append_iteration as front-matter (D4). For commit_msg kind the
// field is always zeroed in zeroNonKindFields. For narrative kinds, the
// renderer auto-fills it from the first paragraph of NarrativeBody when
// the LLM omits it (Path A, D10) — keeping omitempty so a degenerate
// "no body" case still serialises cleanly.
type wrapRenderResponse struct {
	NarrativeTitle  string `json:"narrative_title,omitempty"`
	NarrativeBody   string `json:"narrative_body,omitempty"`
	Summary         string `json:"summary,omitempty"`
	CommitSubject   string `json:"commit_subject,omitempty"`
	CommitProseBody string `json:"commit_prose_body,omitempty"`
}

// wrapRenderProviderFactory is the test seam that maps a tier label to
// a Provider implementation. Production wires through cfg.Wrap.Tiers
// resolution; tests inject a recording mock.
var wrapRenderProviderFactory = func(cfg config.Config, tier string) (llm.Provider, error) {
	if len(cfg.Wrap.Tiers) == 0 {
		return nil, fmt.Errorf(
			"[wrap.tiers] not configured; add a [wrap.tiers] section to ~/.config/vibe-vault/config.toml")
	}
	pm, ok := cfg.Wrap.Tiers[tier]
	if !ok {
		return nil, fmt.Errorf("unknown tier %q (define it in [wrap.tiers])", tier)
	}
	parts := strings.SplitN(pm, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid provider:model %q (expected \"<provider>:<model>\")", pm)
	}
	provider, model := parts[0], parts[1]
	apiKey, err := llm.ResolveAPIKey(provider, cfg.Providers)
	if err != nil {
		return nil, err
	}
	switch provider {
	case "anthropic":
		return llm.NewAnthropic("", apiKey, model)
	case "openai":
		return llm.NewOpenAI("", apiKey, model)
	case "google":
		return llm.NewGoogle(apiKey, model)
	default:
		return nil, fmt.Errorf("unsupported provider %q in [wrap.tiers]", provider)
	}
}

// validateWrapRenderRequest enforces input shape rules per the D3
// schema. Returns the first error found; the slash command is expected
// to send well-formed input, but operators sometimes drive the tool by
// hand for one-off renders.
func validateWrapRenderRequest(req wrapRenderRequest) error {
	switch req.Kind {
	case WrapRenderKindIterNarrative,
		WrapRenderKindCommitMsg,
		WrapRenderKindIterNarrativeAndCommitMsg:
		// ok
	default:
		return fmt.Errorf("invalid kind %q (must be one of: iter_narrative, commit_msg, iter_narrative_and_commit_msg)", req.Kind)
	}
	if req.Tier == "" {
		return fmt.Errorf("tier is required")
	}
	if req.ProjectName == "" {
		return fmt.Errorf("project_name is required")
	}
	if req.IterState.IterN <= 0 {
		return fmt.Errorf("iter_state.iter_n must be a positive integer")
	}
	if len(req.VaultSideNarrativeSeed) > vaultSideNarrativeSeedMaxChars {
		return fmt.Errorf("vault_side_narrative_seed exceeds %d character limit (got %d); split context across multiple wraps if needed",
			vaultSideNarrativeSeedMaxChars, len(req.VaultSideNarrativeSeed))
	}
	if req.Kind == WrapRenderKindCommitMsg && req.VaultSideNarrativeSeed != "" {
		return fmt.Errorf("vault_side_narrative_seed is not supported for kind=commit_msg; commit messages are mechanically derived from iter_state")
	}
	return nil
}

// renderUserPrompt returns the kind-specific user prompt with all
// {{placeholder}} tokens substituted. Verbatim string substitution
// (NOT JSON escaping) — we want the LLM to see the raw values.
func renderUserPrompt(req wrapRenderRequest) (string, error) {
	var tmpl string
	switch req.Kind {
	case WrapRenderKindIterNarrative:
		tmpl = wrapRenderUserPromptIterNarrative
	case WrapRenderKindCommitMsg:
		tmpl = wrapRenderUserPromptCommitMsg
	case WrapRenderKindIterNarrativeAndCommitMsg:
		tmpl = wrapRenderUserPromptIterNarrativeAndCommitMsg
	default:
		return "", fmt.Errorf("unknown kind %q", req.Kind)
	}

	iterStateJSON, err := marshalIterStateNoHTMLEscape(req.IterState)
	if err != nil {
		return "", fmt.Errorf("marshal iter_state: %w", err)
	}
	frictionJSON := req.ProjectContext.FrictionTrends
	if len(frictionJSON) == 0 {
		frictionJSON = json.RawMessage("{}")
	}

	seedOrPlaceholder := req.VaultSideNarrativeSeed
	if seedOrPlaceholder == "" {
		seedOrPlaceholder = "(none provided)"
	}

	rendered := tmpl
	rendered = strings.ReplaceAll(rendered, "{{project_name}}", req.ProjectName)
	rendered = strings.ReplaceAll(rendered, "{{iter_n}}", fmt.Sprintf("%d", req.IterState.IterN))
	rendered = strings.ReplaceAll(rendered, "{{iter_state_json}}", string(iterStateJSON))
	rendered = strings.ReplaceAll(rendered, "{{resume_state}}", req.ProjectContext.ResumeState)
	rendered = strings.ReplaceAll(rendered, "{{recent_iterations}}", req.ProjectContext.RecentIterations)
	rendered = strings.ReplaceAll(rendered, "{{open_threads}}", strings.Join(req.ProjectContext.OpenThreads, ", "))
	rendered = strings.ReplaceAll(rendered, "{{friction_trends_json}}", string(frictionJSON))
	rendered = strings.ReplaceAll(rendered, "{{vault_side_narrative_seed}}", seedOrPlaceholder)
	return rendered, nil
}

// callWrapRenderProvider runs a single ChatCompletion against the
// resolved provider, parses the response into wrapRenderResponse, and
// returns it. The LLM is instructed to emit JSON only; we still defend
// against trailing whitespace and code-fence wrappers.
func callWrapRenderProvider(ctx context.Context, provider llm.Provider, model, system, user string) (wrapRenderResponse, error) {
	resp, err := provider.ChatCompletion(ctx, llm.Request{
		Model:       model,
		System:      system,
		UserPrompt:  user,
		Temperature: 0.2,
		JSONMode:    true,
	})
	if err != nil {
		return wrapRenderResponse{}, fmt.Errorf("provider call: %w", err)
	}
	body := strings.TrimSpace(resp.Content)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var out wrapRenderResponse
	if jerr := json.Unmarshal([]byte(body), &out); jerr != nil {
		return wrapRenderResponse{}, fmt.Errorf("parse provider JSON: %w (content: %s)", jerr, truncateForError(body))
	}
	return out, nil
}

// modelForTier reads the provider:model string for a tier from cfg
// and returns just the model portion. Used to populate llm.Request.Model.
func modelForTier(cfg config.Config, tier string) string {
	pm := cfg.Wrap.Tiers[tier]
	parts := strings.SplitN(pm, ":", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return pm
}

// NewRenderWrapTextTool creates the vv_render_wrap_text MCP tool.
//
// Per Direction-C decision D3, this is the unified rendering tool: a
// single MCP tool, single LLM call, with a `kind:` discriminator
// selecting one of three prompt templates (iter_narrative, commit_msg,
// iter_narrative_and_commit_msg). Consumes only the `Provider`
// interface (single-turn ChatCompletion).
func NewRenderWrapTextTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_render_wrap_text",
			Description: "Render iter narrative and/or commit-message prose for /wrap. Single LLM call, no orchestration. " +
				"kind selects the output: 'iter_narrative' (narrative only), 'commit_msg' (commit subject + body only), " +
				"or 'iter_narrative_and_commit_msg' (both). tier maps to [wrap.tiers] in config (e.g., 'sonnet', 'opus'). " +
				"iter_state and project_context are slash-command-supplied bundles; the renderer does not fetch them itself.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"kind": {
						"type": "string",
						"enum": ["iter_narrative", "commit_msg", "iter_narrative_and_commit_msg"],
						"description": "Output discriminator."
					},
					"tier": {
						"type": "string",
						"description": "Tier label resolved via [wrap.tiers] in config (e.g. 'haiku', 'sonnet', 'opus')."
					},
					"project_name": {
						"type": "string",
						"description": "Project name for prompt templating."
					},
					"iter_state": {
						"type": "object",
						"description": "Server-minimal + slash-command-computed iter facts.",
						"properties": {
							"iter_n":                   {"type": "integer"},
							"branch":                   {"type": "string"},
							"last_iter_anchor_sha":     {"type": "string"},
							"commits_since_last_iter":  {"type": "array", "items": {"type": "object"}},
							"files_changed":            {"type": "array", "items": {"type": "string"}},
							"task_deltas":              {"type": "object"},
							"test_counts":              {"type": "object"}
						}
					},
					"project_context": {
						"type": "object",
						"description": "Parallel-fetched context bundle: resume_state, recent_iterations, open_threads, friction_trends."
					}
				},
				"required": ["kind", "tier", "project_name", "iter_state"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var req wrapRenderRequest
			if len(params) > 0 {
				if err := json.Unmarshal(params, &req); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if err := validateWrapRenderRequest(req); err != nil {
				return "", err
			}

			userPrompt, err := renderUserPrompt(req)
			if err != nil {
				return "", err
			}

			provider, err := wrapRenderProviderFactory(cfg, req.Tier)
			if err != nil {
				return "", err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			resp, err := callWrapRenderProvider(ctx, provider, modelForTier(cfg, req.Tier),
				wrapRenderSystemPreamble, userPrompt)
			if err != nil {
				return "", err
			}

			if zerr := zeroNonKindFields(req.Kind, &resp); zerr != nil {
				return "", zerr
			}

			out, err := marshalIterStateNoHTMLEscape(resp)
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}

// summaryMaxChars is the renderer's hard cap on the optional Summary
// field. Mirrored by vv_append_iteration's surface validation so the
// front-matter writer cannot accept anything the renderer would have
// rejected.
const summaryMaxChars = 200

// vaultSideNarrativeSeedMaxChars is the hard cap on the optional
// VaultSideNarrativeSeed argument per D4 of the
// vault-side-narrative-seed task. ~1K tokens at typical ratios.
// Validation enforced in validateWrapRenderRequest.
const vaultSideNarrativeSeedMaxChars = 4096

// validateRenderSummary enforces the single-line, ≤200-char contract on
// a non-empty Summary value. Returns an error matching the Path A
// rejection rules (D4 / D10 / H5).
func validateRenderSummary(summary string) error {
	if summary == "" {
		return nil
	}
	if strings.Contains(summary, "\n") {
		return fmt.Errorf("provider returned multi-line summary (must be single-line)")
	}
	if len(summary) > summaryMaxChars {
		return fmt.Errorf("provider returned summary >%d characters (got %d)", summaryMaxChars, len(summary))
	}
	return nil
}

// zeroNonKindFields enforces the kind→fields contract: iter_narrative
// kind must NOT carry commit_subject/commit_prose_body (the LLM may
// leak them); commit_msg kind must NOT carry narrative_*. The joint
// kind requires all four fields and errors when any are empty.
//
// For narrative kinds (iter_narrative, iter_narrative_and_commit_msg)
// the optional Summary field is validated and, when empty, auto-filled
// via truncateForSummary(NarrativeBody, 200) — Path A per H5/D10. The
// renderer never opus-retries on a missing summary; the silent fallback
// is the contract.
//
// For commit_msg kind the Summary field is always zeroed even if the
// LLM leaks it, mirroring the narrative_* zeroing.
func zeroNonKindFields(kind string, resp *wrapRenderResponse) error {
	switch kind {
	case WrapRenderKindIterNarrative:
		resp.CommitSubject = ""
		resp.CommitProseBody = ""
		if resp.NarrativeTitle == "" || resp.NarrativeBody == "" {
			return fmt.Errorf("provider returned empty narrative_title or narrative_body for iter_narrative kind")
		}
		if err := validateRenderSummary(resp.Summary); err != nil {
			return err
		}
		if resp.Summary == "" {
			resp.Summary = truncateForSummary(resp.NarrativeBody, summaryMaxChars)
		}
	case WrapRenderKindCommitMsg:
		resp.NarrativeTitle = ""
		resp.NarrativeBody = ""
		resp.Summary = ""
		if resp.CommitSubject == "" || resp.CommitProseBody == "" {
			return fmt.Errorf("provider returned empty commit_subject or commit_prose_body for commit_msg kind")
		}
		if strings.Contains(resp.CommitSubject, "\n") {
			return fmt.Errorf("provider returned multi-line commit_subject (must be single-line)")
		}
	case WrapRenderKindIterNarrativeAndCommitMsg:
		if resp.NarrativeTitle == "" || resp.NarrativeBody == "" ||
			resp.CommitSubject == "" || resp.CommitProseBody == "" {
			return fmt.Errorf(
				"provider returned incomplete output for iter_narrative_and_commit_msg kind " +
					"(all four fields required)")
		}
		if strings.Contains(resp.CommitSubject, "\n") {
			return fmt.Errorf("provider returned multi-line commit_subject (must be single-line)")
		}
		if err := validateRenderSummary(resp.Summary); err != nil {
			return err
		}
		if resp.Summary == "" {
			resp.Summary = truncateForSummary(resp.NarrativeBody, summaryMaxChars)
		}
	}
	return nil
}
