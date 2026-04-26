// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// --- vv_set_commit_msg tests ---

func TestSetCommitMsg_CreatesMissing(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/.keep": "",
	})

	// Project root with no pre-existing commit.msg.
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	tool := NewSetCommitMsgTool(cfg)
	params, _ := json.Marshal(map[string]string{
		"project":      "myproject",
		"project_path": projectRoot,
		"content":      "feat(foo): add bar\n\nThis is a commit message.\n",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result), &got); unmarshalErr != nil {
		t.Fatalf("invalid JSON: %v\n%s", unmarshalErr, result)
	}

	// Verify vault copy.
	vaultPath := filepath.Join(cfg.VaultPath, "Projects", "myproject", "agentctx", "commit.msg")
	data, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read vault commit.msg: %v", err)
	}
	if !strings.Contains(string(data), "feat(foo): add bar") {
		t.Errorf("vault commit.msg = %q, want content with 'feat(foo): add bar'", string(data))
	}

	// Verify project-root copy.
	projPath := filepath.Join(projectRoot, "commit.msg")
	data2, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("read project-root commit.msg: %v", err)
	}
	if string(data2) != string(data) {
		t.Errorf("project-root commit.msg differs from vault copy")
	}

	// Verify returned JSON.
	if bytes, ok := got["bytes_written"].(float64); !ok || int(bytes) == 0 {
		t.Errorf("bytes_written = %v, want > 0", got["bytes_written"])
	}
}

func TestSetCommitMsg_OverwritesExisting(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/commit.msg": "old message\n",
	})

	projectRoot := t.TempDir()
	// Write an existing commit.msg in the project root too.
	if err := os.WriteFile(filepath.Join(projectRoot, "commit.msg"), []byte("old project message\n"), 0o644); err != nil {
		t.Fatalf("write old project commit.msg: %v", err)
	}

	tool := NewSetCommitMsgTool(cfg)
	params, _ := json.Marshal(map[string]string{
		"project":      "myproject",
		"project_path": projectRoot,
		"content":      "new message\n",
	})
	_, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vaultPath := filepath.Join(cfg.VaultPath, "Projects", "myproject", "agentctx", "commit.msg")
	data, _ := os.ReadFile(vaultPath)
	if string(data) != "new message\n" {
		t.Errorf("vault commit.msg = %q, want %q", string(data), "new message\n")
	}

	projData, _ := os.ReadFile(filepath.Join(projectRoot, "commit.msg"))
	if string(projData) != "new message\n" {
		t.Errorf("project commit.msg = %q, want %q", string(projData), "new message\n")
	}
}

func TestSetCommitMsg_ProjectPathRequired(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewSetCommitMsgTool(cfg)

	params, _ := json.Marshal(map[string]string{
		"project": "myproject",
		"content": "some message",
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for missing project_path")
	}
	if !strings.Contains(err.Error(), "project_path is required") {
		t.Errorf("error = %q, want 'project_path is required'", err)
	}
}

func TestSetCommitMsg_ContentRequired(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewSetCommitMsgTool(cfg)

	params, _ := json.Marshal(map[string]string{
		"project":      "myproject",
		"project_path": "/some/path",
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("error = %q, want 'content is required'", err)
	}
}

func TestSetCommitMsg_PartialSuccessDiagnostic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/.keep": "",
	})

	// Make project root's commit.msg a directory so the rename fails.
	badRoot := t.TempDir()
	blockingPath := filepath.Join(badRoot, "commit.msg")
	if err := os.MkdirAll(blockingPath, 0o755); err != nil {
		t.Fatalf("create blocking dir: %v", err)
	}

	tool := NewSetCommitMsgTool(cfg)
	params, _ := json.Marshal(map[string]string{
		"project":      "myproject",
		"project_path": badRoot,
		"content":      "test message\n",
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error when project-root copy fails")
	}
	// Error must mention both vault landing and project path failure.
	if !strings.Contains(err.Error(), "vault commit.msg written") {
		t.Errorf("error = %q, want mention of vault commit.msg written", err)
	}
	if !strings.Contains(err.Error(), "project-root copy failed") {
		t.Errorf("error = %q, want mention of project-root copy failed", err)
	}

	// The vault copy must have landed despite the project-root failure.
	vaultPath := filepath.Join(cfg.VaultPath, "Projects", "myproject", "agentctx", "commit.msg")
	if _, statErr := os.Stat(vaultPath); statErr != nil {
		t.Errorf("vault commit.msg should exist after partial success, stat err: %v", statErr)
	}
}
