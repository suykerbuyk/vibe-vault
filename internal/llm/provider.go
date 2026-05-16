// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// NewProvider creates a Provider from enrichment config and the layered
// providers config block.
//
// Returns (nil, nil) when enrichment is disabled (the operator opted out).
// When enrichment is enabled, the API key is resolved via ResolveAPIKey
// (config-first, env-fallback, actionable-error-on-both-empty); a missing
// key surfaces as an error so callers can guide the operator at use time.
func NewProvider(enrich config.EnrichmentConfig, providers config.ProvidersConfig) (Provider, error) {
	if !enrich.Enabled {
		return nil, nil
	}

	// Normalize the provider name. The legacy switch below treats "" as
	// "openai"; ResolveAPIKey requires a strict provider name from the
	// supported set, so we collapse the empty form before resolving.
	provider := enrich.Provider
	if provider == "" {
		provider = "openai"
	}

	apiKey, err := ResolveAPIKey(provider, providers)
	if err != nil {
		return nil, err
	}

	// Resolve the effective base URL for the provider per Decision C of
	// grok-provider-support v3: providers.<P>.base_url (when non-empty)
	// overrides enrichment.base_url. Operators on the legacy default config
	// (provider = "openai" + enrichment.base_url = ".../x.ai/...") keep
	// working unchanged because enrichment.base_url is still consulted as
	// the fallback.
	baseURL := resolveBaseURL(provider, enrich.BaseURL, providers)

	var base Provider

	switch enrich.Provider {
	case "openai", "":
		base, err = NewOpenAI(baseURL, apiKey, enrich.Model)
	case "anthropic":
		base, err = NewAnthropic(baseURL, apiKey, enrich.Model)
	case "google":
		base, err = NewGoogle(apiKey, enrich.Model)
	case "grok":
		base, err = NewGrok(baseURL, apiKey, enrich.Model)
	default:
		return nil, fmt.Errorf("unknown LLM provider: %q", enrich.Provider)
	}
	if err != nil {
		return nil, err
	}

	// Wrap with retry logic for transient failures.
	return WithRetry(base), nil
}

// NewAgenticProvider creates an AgenticProvider (multi-turn tool-use)
// from the providers config block. Today this is Anthropic-only; OpenAI-
// compatible RunTools is deferred per the flowdoc-gen-source-ingestion
// task anti-scope.
//
// The Anthropic API key is resolved regardless of enrich.Provider so an
// operator with a grok-configured single-shot enrichment can still take
// the agentic path for `vv flowdoc gen` by configuring an Anthropic key
// — the auto-strategy in cmd/vv/flowdoc.go preflights with this same
// lookup. A missing key returns the same actionable error as the
// single-shot path.
//
// The model argument is the per-call model (typically picked by the
// caller — `vv flowdoc gen` uses defaultAgenticModel when no --model is
// passed) rather than enrich.Model, because the agentic mode needs an
// Anthropic model name that may differ from the operator's default
// single-shot model.
func NewAgenticProvider(model string, providers config.ProvidersConfig) (AgenticProvider, error) {
	apiKey, err := ResolveAPIKey("anthropic", providers)
	if err != nil {
		return nil, fmt.Errorf("agentic mode requires an Anthropic API key: %w", err)
	}
	baseURL := resolveBaseURL("anthropic", "", providers)
	return NewAnthropicAgentic(baseURL, apiKey, model)
}

// resolveBaseURL implements the Decision C precedence rule:
// providers.<P>.base_url > enrichment.base_url > "" (let each NewX
// constructor fall back to its own canonical URL).
//
// Per the v3 plan: enrichment.base_url is retained, NOT deprecated; legacy
// operators on the default config (provider = "openai" + enrichment.base_url
// pointing at xAI) keep working unchanged. The new providers.<P>.base_url
// field is the preferred location for per-provider overrides going forward.
func resolveBaseURL(provider, enrichBaseURL string, providers config.ProvidersConfig) string {
	var perProvider string
	switch provider {
	case "anthropic":
		perProvider = providers.Anthropic.BaseURL
	case "openai", "":
		perProvider = providers.OpenAI.BaseURL
	case "google":
		perProvider = providers.Google.BaseURL
	case "grok":
		perProvider = providers.Grok.BaseURL
	}
	if perProvider != "" {
		return perProvider
	}
	return enrichBaseURL
}

// Available reports the availability state of LLM enrichment.
// Returns (provider, model, reason). If reason is non-empty, LLM is unavailable.
func Available(cfg config.EnrichmentConfig) (provider, model, reason string) {
	if !cfg.Enabled {
		return "", "", "not configured"
	}

	provider = cfg.Provider
	if provider == "" {
		provider = "openai"
	}
	model = cfg.Model

	keyEnv := cfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = config.DefaultAPIKeyEnv(cfg.Provider)
	}
	if keyEnv == "" {
		return provider, model, "api_key_env not configured"
	}
	apiKey := os.Getenv(keyEnv)
	if apiKey == "" {
		return provider, model, fmt.Sprintf("%s not set", keyEnv)
	}

	return provider, model, ""
}
