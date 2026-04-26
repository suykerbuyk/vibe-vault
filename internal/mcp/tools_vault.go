// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Generic vault-relative file accessor MCP tools (vv_vault_*).
//
// All eight tools (read/list/exists/sha256/write/edit/delete/move) operate on
// vault-relative paths and resolve the absolute vault root from
// cfg.VaultPath captured at construction time per D13. Tools deliberately do
// NOT accept a runtime vault_path parameter; that would defeat the
// MCP-as-gatekeeper property. Tests inject by constructing
// config.Config{VaultPath: tempPath} and passing to the constructor.
//
// Path validation, symlink-escape protection (via filepath.EvalSymlinks),
// .git-segment refusal (case-insensitive), atomic writes, and compare-and-set
// semantics are implemented in package internal/vaultfs and reused here. This
// file is a thin JSON-Schema + (de)serialisation wrapper.

package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/vaultfs"
)

// NewVaultReadTool creates the vv_vault_read tool.
//
// Returns the file's content along with size, sha256, and mtime. Default size
// cap is 1 MiB; max_bytes accepts up to 10 MiB. Path is vault-relative and is
// rejected if it contains "..", null bytes, control characters, is empty, is
// absolute, or resolves to vault root. Symlinks whose realpath escapes the
// vault are rejected.
func NewVaultReadTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_read",
			Description: "Read a file at a vault-relative path. Returns " +
				"{content, bytes, sha256, mtime}. Default cap 1 MiB; pass " +
				"max_bytes (up to 10 MiB) for larger files. Path is vault-" +
				"relative (e.g. 'Projects/foo/agentctx/notes/x.md'); absolute " +
				"paths, '..' segments, control characters, and symlinks " +
				"escaping the vault are rejected.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative file path. Required."
					},
					"max_bytes": {
						"type": "integer",
						"description": "Optional read cap, max 10485760 (10 MiB). Default 1048576 (1 MiB)."
					}
				},
				"required": ["path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path     string `json:"path"`
				MaxBytes int64  `json:"max_bytes"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			res, err := vaultfs.Read(cfg.VaultPath, args.Path, args.MaxBytes)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// NewVaultListTool creates the vv_vault_list tool.
//
// Lists one level under the given path. Per D17, .git entries are filtered
// out case-insensitively. include_sha256 (default false) opts into per-file
// hashing — opt-in keeps large-dir listings cheap (Q5).
func NewVaultListTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_list",
			Description: "List entries one level under a vault-relative " +
				"directory. Returns {entries: [{name, type, bytes, sha256?}]}. " +
				".git entries are hidden (case-insensitive). Pass " +
				"include_sha256=true to compute per-file hashes (default off " +
				"for cheap large-dir listings).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative directory path. Required."
					},
					"include_sha256": {
						"type": "boolean",
						"description": "If true, compute SHA-256 for each file entry. Default false."
					}
				},
				"required": ["path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path          string `json:"path"`
				IncludeSha256 bool   `json:"include_sha256"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			entries, err := vaultfs.List(cfg.VaultPath, args.Path, args.IncludeSha256)
			if err != nil {
				return "", err
			}
			if entries == nil {
				entries = []vaultfs.Entry{}
			}
			return marshalVaultResult(struct {
				Entries []vaultfs.Entry `json:"entries"`
			}{Entries: entries})
		},
	}
}

// NewVaultExistsTool creates the vv_vault_exists tool.
//
// Reports whether a vault-relative path exists. Dangling symlinks and links
// whose realpath escapes the vault are reported as {exists: false} per D3.
func NewVaultExistsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_exists",
			Description: "Check whether a vault-relative path exists. Returns " +
				"{exists, type} where type is 'file', 'dir', or '' when not " +
				"present. Dangling symlinks and links whose realpath escapes " +
				"the vault report exists=false.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative path. Required."
					}
				},
				"required": ["path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			res, err := vaultfs.Exists(cfg.VaultPath, args.Path)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// NewVaultSha256Tool creates the vv_vault_sha256 tool.
//
// Returns a file's SHA-256, size, and mtime without transferring its content
// over MCP. Useful for compare-and-set workflows on large files where the
// caller only needs the fingerprint.
func NewVaultSha256Tool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_sha256",
			Description: "Compute SHA-256 of a vault-relative file without " +
				"transferring content over MCP. Returns {sha256, bytes, " +
				"mtime}. Bandwidth-friendly for large-file compare-and-set " +
				"flows.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative file path. Required."
					}
				},
				"required": ["path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			res, err := vaultfs.Sha256(cfg.VaultPath, args.Path)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// NewVaultWriteTool creates the vv_vault_write tool.
//
// Atomic write of arbitrary content to a vault-relative path. Refuses any
// path with a .git segment (case-insensitive) per D8. Optional
// expected_sha256 enables compare-and-set per D6: when supplied and the
// file's current SHA differs, the call returns ErrShaConflict and the file is
// untouched. Parent directories are created implicitly per D9.
func NewVaultWriteTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_write",
			Description: "Atomically write content to a vault-relative file. " +
				"Returns {bytes, sha256, replaced_sha256?}. Refuses any path " +
				"whose segments include '.git' (case-insensitive). Parent " +
				"directories are created as needed. Pass expected_sha256 for " +
				"compare-and-set: if the file's current SHA differs, the " +
				"write is refused and the file is unchanged.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative file path. Required."
					},
					"content": {
						"type": "string",
						"description": "File content. Required (use empty string to truncate)."
					},
					"expected_sha256": {
						"type": "string",
						"description": "Optional compare-and-set guard: file's current SHA-256 must match or write is refused."
					}
				},
				"required": ["path", "content"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path           string `json:"path"`
				Content        string `json:"content"`
				ExpectedSha256 string `json:"expected_sha256"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			res, err := vaultfs.Write(cfg.VaultPath, args.Path, args.Content, args.ExpectedSha256)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// NewVaultEditTool creates the vv_vault_edit tool.
//
// String-replace edit. Per Q1 (locked), if old_string occurs more than once
// the call fails with an error suggesting replace_all=true. Refuses .git
// paths and supports compare-and-set via expected_sha256.
func NewVaultEditTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_edit",
			Description: "Replace old_string with new_string in a vault-" +
				"relative file. Returns {bytes, sha256, replacements}. If " +
				"old_string occurs more than once, the call fails unless " +
				"replace_all=true (mirrors Claude Code Edit semantics). " +
				"Refuses '.git' paths. Pass expected_sha256 for compare-and-" +
				"set.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative file path. Required."
					},
					"old_string": {
						"type": "string",
						"description": "String to replace. Required, non-empty."
					},
					"new_string": {
						"type": "string",
						"description": "Replacement. May be empty (deletes occurrences)."
					},
					"replace_all": {
						"type": "boolean",
						"description": "Replace all occurrences. Default false (multi-occurrence is rejected)."
					},
					"expected_sha256": {
						"type": "string",
						"description": "Optional compare-and-set guard."
					}
				},
				"required": ["path", "old_string"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path           string `json:"path"`
				OldString      string `json:"old_string"`
				NewString      string `json:"new_string"`
				ReplaceAll     bool   `json:"replace_all"`
				ExpectedSha256 string `json:"expected_sha256"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			if args.OldString == "" {
				return "", fmt.Errorf("old_string is required")
			}
			res, err := vaultfs.Edit(cfg.VaultPath, args.Path, args.OldString, args.NewString, args.ReplaceAll, args.ExpectedSha256)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// NewVaultDeleteTool creates the vv_vault_delete tool.
//
// Deletes a single file. Per D10, directory delete is out of scope in v1 and
// returns an informative error. Refuses .git paths and supports
// compare-and-set via expected_sha256.
func NewVaultDeleteTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_delete",
			Description: "Delete a file at a vault-relative path. Returns " +
				"{removed}. Refuses directories (file-only in v1) and any " +
				"path whose segments include '.git' (case-insensitive). Pass " +
				"expected_sha256 for compare-and-set.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Vault-relative file path. Required."
					},
					"expected_sha256": {
						"type": "string",
						"description": "Optional compare-and-set guard."
					}
				},
				"required": ["path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Path           string `json:"path"`
				ExpectedSha256 string `json:"expected_sha256"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			res, err := vaultfs.Delete(cfg.VaultPath, args.Path, args.ExpectedSha256)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// NewVaultMoveTool creates the vv_vault_move tool.
//
// Renames a file. Per Q3 (locked), same source/destination is an error.
// Refuses .git paths on both endpoints and refuses to overwrite an existing
// destination.
func NewVaultMoveTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_vault_move",
			Description: "Rename a vault-relative file from from_path to " +
				"to_path. Returns {moved}. Refuses to overwrite an existing " +
				"destination. Refuses '.git' paths on either endpoint. " +
				"Refuses same-source-and-destination (caller bug). Parent " +
				"directories on the destination side are created implicitly.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from_path": {
						"type": "string",
						"description": "Source vault-relative path. Required."
					},
					"to_path": {
						"type": "string",
						"description": "Destination vault-relative path. Required."
					}
				},
				"required": ["from_path", "to_path"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				FromPath string `json:"from_path"`
				ToPath   string `json:"to_path"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.FromPath == "" {
				return "", fmt.Errorf("from_path is required")
			}
			if args.ToPath == "" {
				return "", fmt.Errorf("to_path is required")
			}
			res, err := vaultfs.Move(cfg.VaultPath, args.FromPath, args.ToPath)
			if err != nil {
				return "", err
			}
			return marshalVaultResult(res)
		},
	}
}

// marshalVaultResult JSON-encodes res with two-space indent and a trailing
// newline, matching the convention of the other vv_* tool handlers.
func marshalVaultResult(res any) (string, error) {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(data) + "\n", nil
}
