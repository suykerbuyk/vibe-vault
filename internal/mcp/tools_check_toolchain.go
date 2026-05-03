// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/check"
)

// NewCheckToolchainTool exposes check.CheckToolchain() to MCP clients.
//
// Constructor takes no cfg argument — toolchain probing inspects PATH for
// dev binaries (go, golangci-lint, gh, make, git) and never touches the
// vault. Mirrors NewGetAgentDefinitionTool() precedent for vault-
// independent tools in the registered set.
//
// Wire shape: returns a JSON array of {name, status, detail} objects, one
// per probed binary. status is lowercased ("pass" / "warn") to match the
// projection used by `vv check --json` (internal/check/json.go). The probe
// never emits a "fail" status — missing binaries and broken --version
// invocations both surface as "warn".
//
// Motivation: avoids a second `vv` startup + JSON parse round-trip on
// every /restart Phase-0 handshake. Agents call this MCP tool directly
// instead of shelling out to `vv check --json` to read the toolchain
// section.
func NewCheckToolchainTool() Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_check_toolchain",
			Description: "Probe required dev binaries (go, golangci-lint, gh, make, git) " +
				"and report install presence + first --version line per binary. Each " +
				"result has Name 'tool:<bin>' and Status 'pass' (binary resolves and " +
				"--version exits 0) or 'warn' (binary missing or --version fails). " +
				"Returns a JSON array of {name, status, detail} entries.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {}
			}`),
		},
		Handler: func(_ json.RawMessage) (string, error) {
			results := check.CheckToolchain()
			// Project to MCP wire shape: lowercase status, name/status/detail keys.
			type entry struct {
				Name   string `json:"name"`
				Status string `json:"status"`
				Detail string `json:"detail"`
			}
			entries := make([]entry, 0, len(results))
			for _, r := range results {
				entries = append(entries, entry{
					Name:   r.Name,
					Status: strings.ToLower(r.Status.String()),
					Detail: r.Detail,
				})
			}
			data, err := json.Marshal(entries)
			if err != nil {
				return "", fmt.Errorf("marshal toolchain results: %w", err)
			}
			return string(data), nil
		},
	}
}
