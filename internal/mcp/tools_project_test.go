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

// --- vv_get_project_root tests ---

func TestGetProjectRoot_GitDir(t *testing.T) {
	// Set up a fake project with a .git directory.
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(projectRoot, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewGetProjectRootTool(cfg)

	params, _ := json.Marshal(map[string]string{"cwd": sub})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if got["project_root"] != projectRoot {
		t.Errorf("project_root = %q, want %q", got["project_root"], projectRoot)
	}
}

func TestGetProjectRoot_AgentctxDir(t *testing.T) {
	// Set up a project with agentctx/ but no .git/.
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, "agentctx"), 0o755); err != nil {
		t.Fatalf("mkdir agentctx: %v", err)
	}
	sub := filepath.Join(projectRoot, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewGetProjectRootTool(cfg)

	params, _ := json.Marshal(map[string]string{"cwd": sub})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if got["project_root"] != projectRoot {
		t.Errorf("project_root = %q, want %q", got["project_root"], projectRoot)
	}
}

func TestGetProjectRoot_VaultRootRefused(t *testing.T) {
	// vault root has .git — accessing from inside vault root directly.
	vaultRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	cfg.VaultPath = vaultRoot

	tool := NewGetProjectRootTool(cfg)
	params, _ := json.Marshal(map[string]string{"cwd": vaultRoot})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for vault root, got nil")
	}
	if !strings.Contains(err.Error(), "vault root") {
		t.Errorf("error = %q, want mention of vault root", err)
	}
}
