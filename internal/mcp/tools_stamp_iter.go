// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/suykerbuyk/vibe-vault/internal/atomicfile"
	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// NewStampIterTool creates the vv_stamp_iter tool.
// It atomically writes the current iteration number to
// .vibe-vault/last-iter at the project root AND a JSON snapshot of the
// vault-side tasks/ tree partitioned by directory to
// .vibe-vault/last-tasks-snapshot.json. The first file is the canonical
// project-side anchor used by Stage 1 of the next wrap; the second is
// the anchor against which vv_collect_wrap_state computes task_deltas
// (per the C3-v6 fix in the wrap-mcp-offload PR). Every wrap MUST stamp.
// project_path is required and must be absolute.
func NewStampIterTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_stamp_iter",
			Description: "Write the current iteration number to .vibe-vault/last-iter " +
				"AND a snapshot of the vault-side tasks/ tree (active/done/cancelled " +
				"slug sets) to .vibe-vault/last-tasks-snapshot.json at the project " +
				"root. Atomically replaces both files. last-iter is the canonical " +
				"project-side anchor for the wrap pipeline; last-tasks-snapshot.json " +
				"is the anchor for vv_collect_wrap_state's task_deltas computation. " +
				"Every wrap MUST stamp. Returns " +
				"{project_path, iter, bytes_written, snapshot_path, snapshot_bytes}.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"project_path": {
						"type": "string",
						"description": "Absolute path to the project root. Required."
					},
					"iter": {
						"type": "integer",
						"minimum": 1,
						"description": "The iteration number to stamp (>= 1)."
					}
				},
				"required": ["project_path", "iter"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project     string `json:"project"`
				ProjectPath string `json:"project_path"`
				Iter        int    `json:"iter"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.ProjectPath == "" {
				return "", fmt.Errorf("project_path is required; obtain it via vv_get_project_root")
			}
			if !filepath.IsAbs(args.ProjectPath) {
				return "", fmt.Errorf("project_path must be absolute, got %q", args.ProjectPath)
			}
			if args.Iter < 1 {
				return "", fmt.Errorf("iter must be >= 1, got %d", args.Iter)
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			stateDir := filepath.Join(args.ProjectPath, ".vibe-vault")
			if mkErr := os.MkdirAll(stateDir, 0o755); mkErr != nil {
				return "", fmt.Errorf("create parent directory: %w", mkErr)
			}

			dest := filepath.Join(stateDir, "last-iter")
			data := []byte(strconv.Itoa(args.Iter) + "\n")
			if writeErr := atomicfile.Write(cfg.VaultPath, dest, data); writeErr != nil {
				return "", fmt.Errorf("write last-iter: %w", writeErr)
			}

			// Write the tasks snapshot alongside last-iter (C3-v6).
			// The snapshot anchors vv_collect_wrap_state's task_deltas
			// computation at the next wrap; failures here are
			// surfaced even though last-iter already landed —
			// silently dropping the snapshot would re-create the
			// inherited bug C3-v6 fixes.
			snapshotPath := filepath.Join(stateDir, "last-tasks-snapshot.json")
			snapshotBytes, err := writeTasksSnapshot(cfg, project, args.ProjectPath, args.Iter, snapshotPath)
			if err != nil {
				return "", fmt.Errorf("write last-tasks-snapshot: %w", err)
			}

			result := struct {
				ProjectPath    string `json:"project_path"`
				Iter           int    `json:"iter"`
				BytesWritten   int    `json:"bytes_written"`
				SnapshotPath   string `json:"snapshot_path"`
				SnapshotBytes  int    `json:"snapshot_bytes"`
			}{
				ProjectPath:    dest,
				Iter:           args.Iter,
				BytesWritten:   len(data),
				SnapshotPath:   snapshotPath,
				SnapshotBytes:  snapshotBytes,
			}
			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}

// writeTasksSnapshot enumerates the live state of the vault-side tasks/
// tree for project and writes the resulting slug sets to snapshotPath as
// JSON. The snapshot's anchor_sha records the prior wrap's anchor (read
// from .vibe-vault/last-iter's git history at projectPath); when the
// project hasn't yet recorded an anchor the field is left empty. The
// snapshot is the C3-v6 ground-truth for vv_collect_wrap_state's
// task_deltas computation: every successful wrap rewrites this file so
// the next /wrap can compute set differences against last-wrap state
// rather than walking project-repo git history.
//
// Returns the byte count actually written and any I/O error. A missing
// vault tasks directory degrades to an empty-snapshot write rather than
// erroring; first-bootstrap projects shouldn't fail their first wrap on
// account of a missing tasks/ tree.
func writeTasksSnapshot(cfg config.Config, project, projectPath string, iterN int, snapshotPath string) (int, error) {
	tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")
	active, done, cancelled, err := enumerateLiveTasksFS(tasksDir)
	if err != nil {
		return 0, fmt.Errorf("enumerate live tasks: %w", err)
	}
	if active == nil {
		active = []string{}
	}
	if done == nil {
		done = []string{}
	}
	if cancelled == nil {
		cancelled = []string{}
	}

	// anchor_sha reflects the prior wrap's anchor (best-effort);
	// vv_collect_wrap_state does not consume this field, but it's
	// useful for debugging / drift detection in `git diff`.
	anchorSHA, _ := lastIterAnchorSha(projectPath)

	snap := lastTasksSnapshot{
		IterN:     iterN,
		AnchorSHA: anchorSHA,
		Active:    active,
		Done:      done,
		Cancelled: cancelled,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal snapshot: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(cfg.VaultPath, snapshotPath, data); err != nil {
		return 0, fmt.Errorf("write snapshot: %w", err)
	}
	return len(data), nil
}
