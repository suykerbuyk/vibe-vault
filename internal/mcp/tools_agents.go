// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
)

// NewGetAgentDefinitionTool creates the vv_get_agent_definition MCP tool.
//
// V2-PORTABILITY SCAFFOLDING: this tool is NOT part of v1's wrap dispatch
// flow. v1's vv_wrap_dispatch handler (Phase 3) reads the registry directly
// via agentregistry.Lookup() — no MCP round-trip is needed when orchestrator
// and dispatcher live in the same process. The tool exists so external
// orchestrators that do not embed the vibe-vault binary can still discover
// the same agent catalogue (system prompt, tool whitelist, escalation
// triggers, recommended model class) over the standard MCP transport.
//
// Operators inspecting `vv mcp check --tools` see this tool listed; the
// description below makes its v2-only status explicit so it is not mistaken
// for part of the live wrap path.
func NewGetAgentDefinitionTool() Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_get_agent_definition",
			Description: "Return a named agent definition (system prompt, tool whitelist, " +
				"forbidden-tool list, escalation triggers, recommended model class, and " +
				"canonical sha256). " +
				"V2-PORTABILITY SCAFFOLDING: v1's vv_wrap_dispatch reads the registry " +
				"directly via in-process Go call — this MCP tool exists for external " +
				"orchestrators that do not embed the vibe-vault binary. It is not " +
				"consumed by v1's wrap flow. " +
				"Errors with `agent \"<name>\" not found` if the requested agent is not " +
				"registered.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string",
						"description": "Registered agent name (e.g. \"wrap-executor\")."
					}
				},
				"required": ["name"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Name string `json:"name"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Name == "" {
				return "", fmt.Errorf("name is required")
			}

			def, err := agentregistry.Lookup(args.Name)
			if err != nil {
				return "", err
			}

			data, err := json.MarshalIndent(def, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal definition: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}
