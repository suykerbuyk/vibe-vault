// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// TestNewProvider_ConfigKeyForwarded asserts NewProvider resolves through
// ResolveAPIKey: when env is empty but config carries a key, construction
// succeeds (i.e. the config key was the resolved value handed to the
// underlying provider constructor — the resolver tests already cover the
// resolution semantics, so here we just lock the wiring).
func TestNewProvider_ConfigKeyForwarded(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	enrich := config.EnrichmentConfig{
		Enabled:  true,
		Provider: "anthropic",
		Model:    "claude-sonnet-4-6",
	}
	providers := config.ProvidersConfig{
		Anthropic: config.ProviderConfig{APIKey: "TESTKEY"},
	}

	got, err := NewProvider(enrich, providers)
	if err != nil {
		t.Fatalf("NewProvider with config key + empty env: unexpected error %v", err)
	}
	if got == nil {
		t.Fatalf("NewProvider returned nil provider despite resolved config key")
	}
}
