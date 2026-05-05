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
//
// Table-driven across all 4 supported providers so adding a new provider
// can't accidentally bypass the config-first resolution path.
func TestNewProvider_ConfigKeyForwarded(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		envVar   string
		model    string
		setKey   func(*config.ProvidersConfig)
	}{
		{
			name:     "anthropic",
			provider: "anthropic",
			envVar:   "ANTHROPIC_API_KEY",
			model:    "claude-sonnet-4-6",
			setKey:   func(p *config.ProvidersConfig) { p.Anthropic.APIKey = "TESTKEY" },
		},
		{
			name:     "openai",
			provider: "openai",
			envVar:   "OPENAI_API_KEY",
			model:    "gpt-4o-mini",
			setKey:   func(p *config.ProvidersConfig) { p.OpenAI.APIKey = "TESTKEY" },
		},
		{
			name:     "google",
			provider: "google",
			envVar:   "GOOGLE_API_KEY",
			model:    "gemini-2.0-flash",
			setKey:   func(p *config.ProvidersConfig) { p.Google.APIKey = "TESTKEY" },
		},
		{
			name:     "grok",
			provider: "grok",
			envVar:   "XAI_API_KEY",
			model:    "grok-3-mini-fast",
			setKey:   func(p *config.ProvidersConfig) { p.Grok.APIKey = "TESTKEY" },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.envVar, "")

			enrich := config.EnrichmentConfig{
				Enabled:  true,
				Provider: tc.provider,
				Model:    tc.model,
			}
			var providers config.ProvidersConfig
			tc.setKey(&providers)

			got, err := NewProvider(enrich, providers)
			if err != nil {
				t.Fatalf("NewProvider(%q) with config key + empty env: unexpected error %v", tc.provider, err)
			}
			if got == nil {
				t.Fatalf("NewProvider(%q) returned nil provider despite resolved config key", tc.provider)
			}
		})
	}
}

// TestResolveBaseURL_Precedence locks the Decision C precedence rule from
// grok-provider-support v3:
//
//   - providers.<P>.base_url (when non-empty) overrides enrichment.base_url
//   - providers.<P>.base_url empty falls back to enrichment.base_url so
//     legacy operators on the default config (provider = "openai" +
//     enrichment.base_url = ".../x.ai/...") keep working unchanged
//   - both empty returns "", letting each NewX constructor fall back to its
//     own canonical URL
//
// Table-driven across all 4 providers (and the empty-string-as-openai
// alias) so future drift on any one branch trips a test.
func TestResolveBaseURL_Precedence(t *testing.T) {
	const (
		fromEnrich  = "https://from-enrichment.test/v1"
		fromPerProv = "https://from-provider.test/v1"
	)

	cases := []struct {
		name      string
		provider  string
		enrich    string
		providers config.ProvidersConfig
		want      string
	}{
		{
			name:     "anthropic per-provider wins",
			provider: "anthropic",
			enrich:   fromEnrich,
			providers: config.ProvidersConfig{
				Anthropic: config.ProviderConfig{BaseURL: fromPerProv},
			},
			want: fromPerProv,
		},
		{
			name:     "anthropic falls back to enrichment",
			provider: "anthropic",
			enrich:   fromEnrich,
			want:     fromEnrich,
		},
		{
			name:     "openai per-provider wins",
			provider: "openai",
			enrich:   fromEnrich,
			providers: config.ProvidersConfig{
				OpenAI: config.ProviderConfig{BaseURL: fromPerProv},
			},
			want: fromPerProv,
		},
		{
			name:     "openai falls back to enrichment",
			provider: "openai",
			enrich:   fromEnrich,
			want:     fromEnrich,
		},
		{
			name:     "empty provider name routes via openai branch",
			provider: "",
			enrich:   fromEnrich,
			providers: config.ProvidersConfig{
				OpenAI: config.ProviderConfig{BaseURL: fromPerProv},
			},
			want: fromPerProv,
		},
		{
			name:     "google per-provider wins",
			provider: "google",
			enrich:   fromEnrich,
			providers: config.ProvidersConfig{
				Google: config.ProviderConfig{BaseURL: fromPerProv},
			},
			want: fromPerProv,
		},
		{
			name:     "google falls back to enrichment",
			provider: "google",
			enrich:   fromEnrich,
			want:     fromEnrich,
		},
		{
			name:     "grok per-provider wins",
			provider: "grok",
			enrich:   fromEnrich,
			providers: config.ProvidersConfig{
				Grok: config.ProviderConfig{BaseURL: fromPerProv},
			},
			want: fromPerProv,
		},
		{
			name:     "grok falls back to enrichment",
			provider: "grok",
			enrich:   fromEnrich,
			want:     fromEnrich,
		},
		{
			name:     "both empty yields empty (NewX uses canonical URL)",
			provider: "grok",
			enrich:   "",
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveBaseURL(tc.provider, tc.enrich, tc.providers)
			if got != tc.want {
				t.Errorf("resolveBaseURL(%q, %q, ...) = %q, want %q",
					tc.provider, tc.enrich, got, tc.want)
			}
		})
	}
}

// TestNewProvider_GrokRouting locks the wiring: enrich.Provider = "grok"
// resolves the API key via the providers.grok block (env empty) and
// constructs successfully under the new "grok" switch case.
func TestNewProvider_GrokRouting(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")

	enrich := config.EnrichmentConfig{
		Enabled:  true,
		Provider: "grok",
		Model:    "grok-3-mini-fast",
		// Leave enrich.BaseURL empty so NewGrok's default
		// (GrokDefaultBaseURL) flows through.
	}
	providers := config.ProvidersConfig{
		Grok: config.ProviderConfig{APIKey: "TESTKEY"},
	}

	got, err := NewProvider(enrich, providers)
	if err != nil {
		t.Fatalf("NewProvider(grok): %v", err)
	}
	if got == nil {
		t.Fatal("NewProvider(grok) returned nil provider")
	}
}
