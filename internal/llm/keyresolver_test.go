// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// providersWithKey returns a ProvidersConfig with the named provider's
// api_key set to the given value and the other two providers empty.
func providersWithKey(t *testing.T, provider, key string) config.ProvidersConfig {
	t.Helper()
	var p config.ProvidersConfig
	switch provider {
	case "anthropic":
		p.Anthropic.APIKey = key
	case "openai":
		p.OpenAI.APIKey = key
	case "google":
		p.Google.APIKey = key
	default:
		t.Fatalf("providersWithKey: unknown provider %q", provider)
	}
	return p
}

func TestResolveAPIKey_ConfigWins(t *testing.T) {
	cases := []struct {
		provider string
		envVar   string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"google", "GOOGLE_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv(tc.envVar, "FROM_ENV")
			providers := providersWithKey(t, tc.provider, "FROM_CONFIG")
			got, err := ResolveAPIKey(tc.provider, providers)
			if err != nil {
				t.Fatalf("ResolveAPIKey: %v", err)
			}
			if got != "FROM_CONFIG" {
				t.Errorf("ResolveAPIKey(%q) = %q, want FROM_CONFIG (config must win)", tc.provider, got)
			}
		})
	}
}

func TestResolveAPIKey_EnvFallback(t *testing.T) {
	cases := []struct {
		provider string
		envVar   string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"google", "GOOGLE_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv(tc.envVar, "FROM_ENV")
			got, err := ResolveAPIKey(tc.provider, config.ProvidersConfig{})
			if err != nil {
				t.Fatalf("ResolveAPIKey: %v", err)
			}
			if got != "FROM_ENV" {
				t.Errorf("ResolveAPIKey(%q) = %q, want FROM_ENV (env fallback)", tc.provider, got)
			}
		})
	}
}

func TestResolveAPIKey_BothEmpty_ActionableError(t *testing.T) {
	cases := []struct {
		provider string
		envVar   string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"google", "GOOGLE_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			// Clear any ambient env so the resolver actually sees both empty.
			t.Setenv(tc.envVar, "")
			_, err := ResolveAPIKey(tc.provider, config.ProvidersConfig{})
			if err == nil {
				t.Fatalf("ResolveAPIKey(%q): expected error when config + env are both empty", tc.provider)
			}
			msg := err.Error()
			wantSetKey := "vv config set-key " + tc.provider
			if !strings.Contains(msg, wantSetKey) {
				t.Errorf("error %q must mention %q", msg, wantSetKey)
			}
			if !strings.Contains(msg, tc.envVar) {
				t.Errorf("error %q must mention env var %q", msg, tc.envVar)
			}
		})
	}
}

func TestResolveAPIKey_UnknownProvider(t *testing.T) {
	_, err := ResolveAPIKey("cohere", config.ProvidersConfig{})
	if err == nil {
		t.Fatalf("ResolveAPIKey(\"cohere\"): expected error for unknown provider")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown provider") {
		t.Errorf("error %q should say 'unknown provider'", msg)
	}
	for _, want := range []string{"anthropic", "openai", "google"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should list supported provider %q", msg, want)
		}
	}
}
