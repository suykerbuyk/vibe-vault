// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"fmt"
	"os"

	"github.com/johns/vibe-vault/internal/config"
)

// NewProvider creates a Provider from enrichment config.
// Returns (nil, nil) if enrichment is disabled or the API key is not set.
func NewProvider(cfg config.EnrichmentConfig) (Provider, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, nil
	}

	var base Provider
	var err error

	switch cfg.Provider {
	case "openai", "":
		base, err = NewOpenAI(cfg.BaseURL, apiKey, cfg.Model)
	case "anthropic":
		base, err = NewAnthropic(cfg.BaseURL, apiKey, cfg.Model)
	case "google":
		base, err = NewGoogle(apiKey, cfg.Model)
	default:
		return nil, fmt.Errorf("unknown LLM provider: %q", cfg.Provider)
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

	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return provider, model, fmt.Sprintf("%s not set", cfg.APIKeyEnv)
	}

	return provider, model, ""
}
