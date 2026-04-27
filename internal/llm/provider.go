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

	var base Provider

	switch enrich.Provider {
	case "openai", "":
		base, err = NewOpenAI(enrich.BaseURL, apiKey, enrich.Model)
	case "anthropic":
		base, err = NewAnthropic(enrich.BaseURL, apiKey, enrich.Model)
	case "google":
		base, err = NewGoogle(apiKey, enrich.Model)
	default:
		return nil, fmt.Errorf("unknown LLM provider: %q", enrich.Provider)
	}
	if err != nil {
		return nil, err
	}

	// Wrap with retry logic for transient failures.
	return WithRetry(base), nil
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
