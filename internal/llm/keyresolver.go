// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// ResolveAPIKey returns the API key for the named provider with config-first /
// env-fallback / actionable-error precedence.
//
// Provider names are the lowercase short forms used by `[wrap.tiers]` and
// `[enrichment].provider`: "anthropic", "openai", "google". Tier order:
//
//  1. providers.<P>.APIKey (from config.toml) wins if non-empty.
//  2. os.Getenv(envVarFor(provider)) — fallback for operators with env-var
//     setup.
//  3. Both empty → return an actionable error naming both
//     `vv config set-key <provider> <key>` and the provider's env var, so
//     operators in either setup style get unambiguous guidance.
//
// Unknown provider names return an error naming the supported set.
//
// This resolver is shared between `NewProvider` (hook + synthesis) and the
// MCP wrap-dispatch handler — they route by different axes
// (enrichment.provider vs tier-string-prefix) but share resolution semantics.
func ResolveAPIKey(provider string, providers config.ProvidersConfig) (string, error) {
	envVar := envVarFor(provider)
	if envVar == "" {
		return "", fmt.Errorf("unknown provider %q; supported: anthropic, openai, google", provider)
	}

	if k := configKeyFor(provider, providers); k != "" {
		return k, nil
	}
	if k := os.Getenv(envVar); k != "" {
		return k, nil
	}

	return "", fmt.Errorf(
		"no API key configured for %s; run 'vv config set-key %s <key>' or set %s",
		provider, provider, envVar)
}

// envVarFor maps a provider short-name to its conventional API-key env var.
// Returns "" for unknown providers.
func envVarFor(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "google":
		return "GOOGLE_API_KEY"
	default:
		return ""
	}
}

// configKeyFor selects the configured api_key for the named provider.
// Returns "" if the provider is unknown or its key field is unset.
func configKeyFor(provider string, providers config.ProvidersConfig) string {
	switch provider {
	case "anthropic":
		return providers.Anthropic.APIKey
	case "openai":
		return providers.OpenAI.APIKey
	case "google":
		return providers.Google.APIKey
	default:
		return ""
	}
}
