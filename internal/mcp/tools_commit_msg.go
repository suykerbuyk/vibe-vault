// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// NewSetCommitMsgTool creates the vv_set_commit_msg tool.
// It atomically writes commit.msg to the vault archive path and the
// project root. project_path is required and explicit — callers obtain
// it via vv_get_project_root. On project-root-copy failure, returns an
// actionable diagnostic (vault write may have landed — partial-success
// is observable, not silent).
func NewSetCommitMsgTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_set_commit_msg",
			Description: "Write commit.msg to both the vault archive " +
				"(<vault>/Projects/<project>/agentctx/commit.msg) and the project root " +
				"(<project_path>/commit.msg). project_path is required — obtain it via " +
				"vv_get_project_root. On project-root-copy failure an actionable diagnostic " +
				"is returned; the vault copy may have already landed. " +
				"Returns {vault_path, project_path, bytes_written}.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"project_path": {
						"type": "string",
						"description": "Absolute path to the project root. Required. Obtain via vv_get_project_root."
					},
					"content": {
						"type": "string",
						"description": "The full commit message content to write."
					}
				},
				"required": ["project_path", "content"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project     string `json:"project"`
				ProjectPath string `json:"project_path"`
				Content     string `json:"content"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.ProjectPath == "" {
				return "", fmt.Errorf("project_path is required; obtain it via vv_get_project_root")
			}
			if args.Content == "" {
				return "", fmt.Errorf("content is required")
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			// Vault-side path.
			vaultDest := vaultCommitMsgPath(cfg.VaultPath, project)
			absVaultDest, err := vaultPrefixCheck(vaultDest, cfg.VaultPath)
			if err != nil {
				return "", fmt.Errorf("vault path check: %w", err)
			}

			data := []byte(args.Content)

			// Step 1: atomic write to vault.
			writeErr := atomicWriteCommitMsg(absVaultDest, data)
			if writeErr != nil {
				return "", fmt.Errorf("write vault commit.msg: %w", writeErr)
			}

			// Step 2: atomic copy to project root.
			projDest := projectCommitMsgPath(args.ProjectPath)
			copyErr := atomicWriteCommitMsg(projDest, data)
			if copyErr != nil {
				return "", fmt.Errorf(
					"vault commit.msg written to %q but project-root copy failed for %q: %w "+
						"(vault copy landed; copy manually: cp %q %q)",
					absVaultDest, projDest, copyErr, absVaultDest, projDest)
			}

			result := struct {
				VaultPath    string `json:"vault_path"`
				ProjectPath  string `json:"project_path"`
				BytesWritten int    `json:"bytes_written"`
			}{
				VaultPath:    absVaultDest,
				ProjectPath:  projDest,
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

// vaultCommitMsgPath returns the vault-side path for commit.msg.
func vaultCommitMsgPath(vaultPath, project string) string {
	return joinPath(vaultPath, "Projects", project, "agentctx", "commit.msg")
}

// projectCommitMsgPath returns the project-root-side path for commit.msg.
func projectCommitMsgPath(projectPath string) string {
	return joinPath(projectPath, "commit.msg")
}

// joinPath joins path components with "/" separator.
func joinPath(elem ...string) string {
	if len(elem) == 0 {
		return ""
	}
	result := elem[0]
	for _, e := range elem[1:] {
		if result == "" {
			result = e
			continue
		}
		if e == "" {
			continue
		}
		result = result + "/" + e
	}
	return result
}

// atomicWriteCommitMsg writes data to path atomically via a temp file.
// Creates parent directories as needed.
func atomicWriteCommitMsg(path string, data []byte) error {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}
	tmp, err := os.CreateTemp(dirOf(path), ".vv-commit-msg-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	removeTemp = false
	return nil
}

// dirOf returns the directory component of path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
