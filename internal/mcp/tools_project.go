// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
)

// NewGetProjectRootTool creates the vv_get_project_root tool.
// It wraps meta.ProjectRoot for AI-side project discovery,
// defaulting cwd to the agent's working directory and resolving
// the vault path from config.
func NewGetProjectRootTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_get_project_root",
			Description: "Discover the project root for the current working directory by walking up " +
				"the directory tree, checking for agentctx/ (preferred) then .git/ at each level. " +
				"Returns the project root path. Fails with an actionable error if the matched " +
				"directory is the vault root — pass a cwd from inside a project subdirectory.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd": {
						"type": "string",
						"description": "Working directory to start the search from. Defaults to the agent's CWD."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				CWD string `json:"cwd"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			cwd := args.CWD
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return "", fmt.Errorf("get working directory: %w", err)
				}
			}

			root, err := meta.ProjectRoot(cwd, cfg.VaultPath)
			if err != nil {
				if errors.Is(err, meta.ErrIsVaultRoot) {
					return "", fmt.Errorf("matched vault root %q, not a project root; "+
						"pass cwd from inside a project subdirectory", cfg.VaultPath)
				}
				return "", err
			}

			result := struct {
				ProjectRoot string `json:"project_root"`
				CWD         string `json:"cwd"`
			}{
				ProjectRoot: root,
				CWD:         cwd,
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}
