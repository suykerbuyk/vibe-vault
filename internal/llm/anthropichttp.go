// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
)

// anthropicHTTPCore holds the shared HTTP plumbing for Anthropic Messages API
// callers. Both the single-turn Anthropic provider and the multi-turn
// AnthropicAgentic provider embed *anthropicHTTPCore so they share connection
// management, header construction, and endpoint routing in one place.
//
// The core is intentionally body-agnostic: callers marshal the request body
// (single-turn vs. tool-use are wire-format-different) and parse the response
// body themselves. The core is responsible only for wrapping a []byte payload
// in an HTTP request with the correct headers and dispatching it.
type anthropicHTTPCore struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// newAnthropicHTTPCore builds a shared HTTP core. If client is nil a default
// *http.Client with no per-request timeout is supplied — matching the existing
// behaviour of NewAnthropic so callers see identical timeout semantics. If
// baseURL is empty it defaults to the public Anthropic endpoint. Trailing
// slashes on baseURL are trimmed so endpoint construction never doubles them.
func newAnthropicHTTPCore(baseURL, apiKey, model string, client *http.Client) *anthropicHTTPCore {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	if client == nil {
		client = &http.Client{}
	}
	return &anthropicHTTPCore{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  client,
	}
}

// do sends a POST to <baseURL>/v1/messages with the supplied JSON body. The
// three required Anthropic headers (anthropic-version, x-api-key,
// content-type) are set automatically. Any extraHeaders are applied last so
// callers may override defaults or add new headers (e.g. anthropic-beta for
// tool-use beta features). Caller is responsible for closing the returned
// response body and for interpreting the status code.
func (c *anthropicHTTPCore) do(ctx context.Context, body []byte, extraHeaders map[string]string) (*http.Response, error) {
	url := c.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("content-type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	return resp, nil
}
