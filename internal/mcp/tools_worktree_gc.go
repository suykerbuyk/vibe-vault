// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/worktreegc"
)

// NewWorktreeGCTool creates the vv_worktree_gc MCP tool.
//
// The tool reaps stale subagent worktrees with safe-by-default capture
// verification. It mirrors the CLI front-end (`vv worktree gc`) but
// takes an explicit project_path so an agent in any cwd can target a
// specific repo.
//
// Algorithm and safety-property details live in
// internal/worktreegc/worktreegc.go's package doc.
func NewWorktreeGCTool(_ config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_worktree_gc",
			Description: "Reap stale subagent worktrees with safe-by-default capture verification. " +
				"Default-branch-aware via git symbolic-ref. " +
				"Use dry_run for read-only inspection.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project_path": {
						"type": "string",
						"description": "Absolute path to the repository whose worktrees should be inspected."
					},
					"dry_run": {
						"type": "boolean",
						"description": "When true, emit would-reap verdicts without destructive changes."
					},
					"candidate_parents": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Branch names to use as authoritative parents for capture verification. When omitted, defaults to [resolved-default-branch, current-branch]."
					},
					"force_uncaptured": {
						"type": "boolean",
						"description": "When true, reap even if the worktree branch contains commits not present on any candidate parent."
					}
				},
				"required": ["project_path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				ProjectPath      string   `json:"project_path"`
				DryRun           bool     `json:"dry_run"`
				CandidateParents []string `json:"candidate_parents"`
				ForceUncaptured  bool     `json:"force_uncaptured"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.ProjectPath == "" {
				return "", fmt.Errorf("project_path is required")
			}

			opts := worktreegc.Options{
				DryRun:           args.DryRun,
				CandidateParents: args.CandidateParents,
				ForceUncaptured:  args.ForceUncaptured,
			}
			res, err := worktreegc.Run(args.ProjectPath, opts)
			if err != nil {
				return "", err
			}

			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal result: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
