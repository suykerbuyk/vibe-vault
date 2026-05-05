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

// TestStampIter_WritesTasksSnapshot verifies that vv_stamp_iter writes
// .vibe-vault/last-tasks-snapshot.json alongside .vibe-vault/last-iter
// (C3-v6 fix in the wrap-mcp-offload PR). The snapshot's slug sets must
// reflect the live state of <vault>/Projects/<project>/agentctx/tasks/{,
// done/, cancelled/} at stamp time; vv_collect_wrap_state diffs the
// next wrap's live FS against this snapshot to compute task_deltas.
func TestStampIter_WritesTasksSnapshot(t *testing.T) {
	// Seed the vault-side tasks/ tree partitioned by directory so we
	// can verify each partition's slug set landed in the snapshot.
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tasksDir := filepath.Join(cfg.VaultPath, "Projects", "myproj", "agentctx", "tasks")
	if err := os.MkdirAll(filepath.Join(tasksDir, "done"), 0o755); err != nil {
		t.Fatalf("mkdir done: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tasksDir, "cancelled"), 0o755); err != nil {
		t.Fatalf("mkdir cancelled: %v", err)
	}
	for path, body := range map[string]string{
		filepath.Join(tasksDir, "active-one.md"):              "x",
		filepath.Join(tasksDir, "active-two.md"):              "x",
		filepath.Join(tasksDir, "done", "shipped-one.md"):     "x",
		filepath.Join(tasksDir, "cancelled", "killed-one.md"): "x",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	projectRoot := t.TempDir()
	tool := NewStampIterTool(cfg)
	resultJSON, err := tool.Handler(stampIterArgs(t, "myproj", projectRoot, 218))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The handler's response must include both file paths and byte counts.
	var got map[string]any
	if unmarshalErr := json.Unmarshal([]byte(resultJSON), &got); unmarshalErr != nil {
		t.Fatalf("invalid JSON: %v\n%s", unmarshalErr, resultJSON)
	}
	wantSnapshotPath := filepath.Join(projectRoot, ".vibe-vault", "last-tasks-snapshot.json")
	if got["snapshot_path"] != wantSnapshotPath {
		t.Errorf("snapshot_path = %v, want %q", got["snapshot_path"], wantSnapshotPath)
	}
	if sb, ok := got["snapshot_bytes"].(float64); !ok || int(sb) <= 0 {
		t.Errorf("snapshot_bytes = %v, want > 0", got["snapshot_bytes"])
	}

	// last-iter still landed (cohabitation with the snapshot).
	lastIterPath := filepath.Join(projectRoot, ".vibe-vault", "last-iter")
	lastIterData, err := os.ReadFile(lastIterPath)
	if err != nil {
		t.Fatalf("read last-iter: %v", err)
	}
	if string(lastIterData) != "218\n" {
		t.Errorf("last-iter = %q, want %q", string(lastIterData), "218\n")
	}

	// The snapshot exists and parses as the C3-v6 schema.
	snapshotData, err := os.ReadFile(wantSnapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap lastTasksSnapshot
	if err := json.Unmarshal(snapshotData, &snap); err != nil {
		t.Fatalf("parse snapshot: %v\n%s", err, snapshotData)
	}
	if snap.IterN != 218 {
		t.Errorf("snap.IterN = %d, want 218", snap.IterN)
	}
	if !equalUnordered(snap.Active, []string{"active-one", "active-two"}) {
		t.Errorf("snap.Active = %v, want [active-one active-two]", snap.Active)
	}
	if !equalUnordered(snap.Done, []string{"shipped-one"}) {
		t.Errorf("snap.Done = %v, want [shipped-one]", snap.Done)
	}
	if !equalUnordered(snap.Cancelled, []string{"killed-one"}) {
		t.Errorf("snap.Cancelled = %v, want [killed-one]", snap.Cancelled)
	}
}

// TestStampIter_SnapshotMissingTasksDir verifies that vv_stamp_iter
// degrades gracefully when the vault-side tasks/ tree is missing —
// first-bootstrap projects must not fail their first wrap on this. The
// snapshot is still written, just with empty slug arrays.
func TestStampIter_SnapshotMissingTasksDir(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	projectRoot := t.TempDir()

	tool := NewStampIterTool(cfg)
	if _, err := tool.Handler(stampIterArgs(t, "bootstrap-proj", projectRoot, 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snapshotPath := filepath.Join(projectRoot, ".vibe-vault", "last-tasks-snapshot.json")
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap lastTasksSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("parse snapshot: %v\n%s", err, data)
	}
	if snap.IterN != 1 {
		t.Errorf("snap.IterN = %d, want 1", snap.IterN)
	}
	if len(snap.Active) != 0 || len(snap.Done) != 0 || len(snap.Cancelled) != 0 {
		t.Errorf("expected empty slug sets, got active=%v done=%v cancelled=%v",
			snap.Active, snap.Done, snap.Cancelled)
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
