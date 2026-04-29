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

// --- vv_stamp_iter tests ---

// stampIterArgs returns JSON params for invoking the vv_stamp_iter tool.
func stampIterArgs(t *testing.T, project, projectPath string, iter int) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"project":      project,
		"project_path": projectPath,
		"iter":         iter,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return data
}

func TestStampIter_Basic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	tool := NewStampIterTool(cfg)
	result, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, 7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result), &got); unmarshalErr != nil {
		t.Fatalf("invalid JSON: %v\n%s", unmarshalErr, result)
	}

	wantPath := filepath.Join(projectRoot, ".vibe-vault", "last-iter")
	if got["project_path"] != wantPath {
		t.Errorf("project_path = %v, want %q", got["project_path"], wantPath)
	}
	if iter, ok := got["iter"].(float64); !ok || int(iter) != 7 {
		t.Errorf("iter = %v, want 7", got["iter"])
	}
	if bw, ok := got["bytes_written"].(float64); !ok || int(bw) != 2 {
		t.Errorf("bytes_written = %v, want 2", got["bytes_written"])
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read last-iter: %v", err)
	}
	if string(data) != "7\n" {
		t.Errorf("last-iter content = %q, want %q", string(data), "7\n")
	}
}

func TestStampIter_Idempotent(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	tool := NewStampIterTool(cfg)
	if _, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, 12)); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, 12)); err != nil {
		t.Fatalf("second call: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".vibe-vault", "last-iter"))
	if err != nil {
		t.Fatalf("read last-iter: %v", err)
	}
	if string(data) != "12\n" {
		t.Errorf("last-iter after idempotent calls = %q, want %q", string(data), "12\n")
	}
}

func TestStampIter_RejectsZero(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	tool := NewStampIterTool(cfg)
	_, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, 0))
	if err == nil {
		t.Fatal("expected error for iter=0")
	}
	if !strings.Contains(err.Error(), "iter must be >= 1") {
		t.Errorf("error = %q, want 'iter must be >= 1'", err)
	}
}

func TestStampIter_RejectsNegative(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	tool := NewStampIterTool(cfg)
	_, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, -3))
	if err == nil {
		t.Fatal("expected error for negative iter")
	}
	if !strings.Contains(err.Error(), "iter must be >= 1") {
		t.Errorf("error = %q, want 'iter must be >= 1'", err)
	}
}

func TestStampIter_RejectsMissingProjectPath(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewStampIterTool(cfg)
	params, _ := json.Marshal(map[string]any{
		"project": "myproject",
		"iter":    1,
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for missing project_path")
	}
	if !strings.Contains(err.Error(), "project_path is required") {
		t.Errorf("error = %q, want 'project_path is required'", err)
	}
}

func TestStampIter_RejectsRelativeProjectPath(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewStampIterTool(cfg)
	_, err := tool.Handler(stampIterArgs(t, "myproject", "relative/path", 1))
	if err == nil {
		t.Fatal("expected error for relative project_path")
	}
	if !strings.Contains(err.Error(), "must be absolute") {
		t.Errorf("error = %q, want 'must be absolute'", err)
	}
}

func TestStampIter_CreatesParentDir(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	// Sanity: .vibe-vault should not exist yet under this fresh temp dir.
	stateDir := filepath.Join(projectRoot, ".vibe-vault")
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("expected .vibe-vault to not exist; stat err: %v", err)
	}

	tool := NewStampIterTool(cfg)
	if _, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, 4)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("stat .vibe-vault: %v", err)
	}
	if !info.IsDir() {
		t.Errorf(".vibe-vault is not a directory")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "last-iter")); err != nil {
		t.Errorf("last-iter not created: %v", err)
	}
}

func TestStampIter_AtomicReplace(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	stateDir := filepath.Join(projectRoot, ".vibe-vault")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	dest := filepath.Join(stateDir, "last-iter")
	if err := os.WriteFile(dest, []byte("99\n"), 0o644); err != nil {
		t.Fatalf("seed last-iter: %v", err)
	}

	tool := NewStampIterTool(cfg)
	if _, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, 100)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read last-iter: %v", err)
	}
	if string(data) != "100\n" {
		t.Errorf("last-iter = %q, want %q", string(data), "100\n")
	}

	// No leftover temp files in the parent dir.
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".vv-tmp-") {
			t.Errorf("found leftover temp file: %s", e.Name())
		}
	}
}

func TestStampIter_ContentExactlyIntegerNewline(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	cases := []struct {
		iter int
		want string
	}{
		{1, "1\n"},
		{42, "42\n"},
		{1000, "1000\n"},
	}
	for _, tc := range cases {
		projectRoot := t.TempDir()
		tool := NewStampIterTool(cfg)
		if _, err := tool.Handler(stampIterArgs(t, "myproject", projectRoot, tc.iter)); err != nil {
			t.Fatalf("iter=%d: unexpected error: %v", tc.iter, err)
		}
		data, err := os.ReadFile(filepath.Join(projectRoot, ".vibe-vault", "last-iter"))
		if err != nil {
			t.Fatalf("iter=%d: read last-iter: %v", tc.iter, err)
		}
		if string(data) != tc.want {
			t.Errorf("iter=%d: content = %q, want %q", tc.iter, string(data), tc.want)
		}
	}
}
