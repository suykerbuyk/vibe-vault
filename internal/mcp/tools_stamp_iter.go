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
// .vibe-vault/last-iter at the project root. The file is the canonical
// project-side anchor used by Stage 1 of the next wrap; every wrap MUST
// stamp. project_path is required and must be absolute.
func NewStampIterTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_stamp_iter",
			Description: "Write the current iteration number to .vibe-vault/last-iter " +
				"at the project root. Atomically replaces the file's content with " +
				"<iter_n>\\n. The file is the canonical project-side anchor for the " +
				"wrap pipeline — every wrap MUST stamp. Returns " +
				"{project_path, iter, bytes_written}.",
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

			if _, err := resolveProject(args.Project); err != nil {
				return "", err
			}

			dest := filepath.Join(args.ProjectPath, ".vibe-vault", "last-iter")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return "", fmt.Errorf("create parent directory: %w", err)
			}

			data := []byte(strconv.Itoa(args.Iter) + "\n")
			if err := atomicfile.Write(cfg.VaultPath, dest, data); err != nil {
				return "", fmt.Errorf("write last-iter: %w", err)
			}

			result := struct {
				ProjectPath  string `json:"project_path"`
				Iter         int    `json:"iter"`
				BytesWritten int    `json:"bytes_written"`
			}{
				ProjectPath:  dest,
				Iter:         args.Iter,
				BytesWritten: len(data),
			}
			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
