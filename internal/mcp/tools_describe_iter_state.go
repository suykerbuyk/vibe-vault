// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// describeIterStateResult is the JSON shape returned by
// vv_describe_iter_state. Field semantics mirror Direction-C decision D6:
// the server returns only the four mechanically-computable facts; the
// slash command computes commits_since_last_iter, files_changed,
// task_deltas, and test_counts itself via git/filesystem.
//
// Note (Commit 1 of wrap-mcp-offload): the lower-level helpers used by
// describeIterState() — gitCmdRunner, vaultHasUncommittedWrites,
// lastIterAnchorSha, nextIterFromIterationsMD, iterNarrativeRe — have
// been relocated to gitprobe.go and wrapstate.go. They remain in the
// same package (internal/mcp), so identifiers resolve here without
// import changes. This file (and its test counterpart) will be deleted
// in Commit 2 when the surface bumps and the tool retires.
type describeIterStateResult struct {
	IterN                     int    `json:"iter_n"`
	Branch                    string `json:"branch"`
	VaultHasUncommittedWrites bool   `json:"vault_has_uncommitted_writes"`
	LastIterAnchorSha         string `json:"last_iter_anchor_sha,omitempty"`
}

// describeIterState computes the four-field state record for a project.
// It is the single source of truth for the vv_describe_iter_state tool.
func describeIterState(cfg config.Config, project string) (describeIterStateResult, error) {
	res := describeIterStateResult{}

	// iter_n: project-wide next iteration number derived from iterations.md.
	// The narrative file uses "### Iteration N" headers; the next iter is
	// max(N) + 1. Returns 1 for fresh projects with no iterations.md or no
	// matching headers.
	n, err := nextIterFromIterationsMD(cfg.VaultPath, project)
	if err != nil {
		return res, fmt.Errorf("next iter from iterations.md: %w", err)
	}
	res.IterN = n

	// branch: git rev-parse --abbrev-ref HEAD in the agent CWD.
	cwd, err := os.Getwd()
	if err != nil {
		return res, fmt.Errorf("get working directory: %w", err)
	}
	res.Branch = detectBranch(cwd)

	// vault_has_uncommitted_writes: git status --porcelain in the vault repo.
	dirty, derr := vaultHasUncommittedWrites(cfg.VaultPath)
	if derr != nil {
		return res, fmt.Errorf("vault git status: %w", derr)
	}
	res.VaultHasUncommittedWrites = dirty

	// last_iter_anchor_sha: SHA of the most recent commit that touched
	// the project's iter stamp file.
	sha, serr := lastIterAnchorSha(cwd)
	if serr != nil {
		return res, fmt.Errorf("last iter anchor sha: %w", serr)
	}
	res.LastIterAnchorSha = sha

	return res, nil
}

// NewDescribeIterStateTool creates the vv_describe_iter_state MCP tool.
//
// Per Direction-C decision D6, the server returns only the four
// mechanically-computable facts (iter_n, branch,
// vault_has_uncommitted_writes, last_iter_anchor_sha). Higher-level
// fields (commits_since_last_iter, files_changed, task_deltas,
// test_counts) are slash-command-computed and folded into the iter
// narrative the orchestrator writes inline.
func NewDescribeIterStateTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_describe_iter_state",
			Description: "Return a server-minimal iter-state record for the current project: " +
				"iter_n (project-wide next iteration-narrative number, derived from max `### Iteration N` in iterations.md + 1), branch (current git branch in agent CWD), " +
				"vault_has_uncommitted_writes (bool from `git status --porcelain` in the vault repo), " +
				"last_iter_anchor_sha (SHA of the most recent commit that wrote .vibe-vault/last-iter (the iter stamp file written by vv_stamp_iter); null/omitted if not found). " +
				"The slash command computes commits_since_last_iter, files_changed, task_deltas, and " +
				"test_counts itself via git/filesystem and folds them into the iter narrative it writes inline.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			res, err := describeIterState(cfg, project)
			if err != nil {
				return "", err
			}

			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
