// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

// GrokDefaultBaseURL is xAI's canonical OpenAI-compatible endpoint.
// Used by NewGrok when the caller does not pass an explicit override.
const GrokDefaultBaseURL = "https://api.x.ai/v1"

// NewGrok creates a Provider for xAI's Grok via its OpenAI-compatible API.
//
// Grok speaks the OpenAI chat-completions wire format, so the implementation
// is a thin wrapper around NewOpenAI: the only meaningful difference is the
// default base URL (GrokDefaultBaseURL vs OpenAI proper). Callers may pass an
// explicit baseURL to point at a regional endpoint, a self-hosted mirror, or
// a test server; an empty baseURL falls back to GrokDefaultBaseURL.
//
// Returns *OpenAI directly (rather than a wrapper type) because there is no
// behavioral divergence to encapsulate; a separate file exists for symmetry
// with google.go / anthropic.go and discoverability.
func NewGrok(baseURL, apiKey, model string) (*OpenAI, error) {
	if baseURL == "" {
		baseURL = GrokDefaultBaseURL
	}
	return NewOpenAI(baseURL, apiKey, model)
}
