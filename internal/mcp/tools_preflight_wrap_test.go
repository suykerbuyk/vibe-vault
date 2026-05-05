// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

// withSurfaceCheck swaps the package-level surfaceCheckCompatible seam
// for the duration of t. Pairs with the surface-fail and surface-pass
// fixtures below.
func withSurfaceCheck(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := surfaceCheckCompatible
	surfaceCheckCompatible = fn
	t.Cleanup(func() { surfaceCheckCompatible = orig })
}

// TestPreflightWrap_AllClean covers the common-case happy path: surface
// passes, vault is clean, project is clean. Expect ok=true with no
// warnings and no errors.
func TestPreflightWrap_AllClean(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	withSurfaceCheck(t, func(_ string) error { return nil })

	tool := NewPreflightWrapTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res PreflightWrapResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, out)
	}
	if !res.OK {
		t.Errorf("ok = false, want true (no errors); res = %+v", res)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want empty", res.Warnings)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors = %v, want empty", res.Errors)
	}
}

// TestPreflightWrap_VaultDirty covers surface-pass + vault-dirty.
// Expect ok=true, exactly one warning (vault_dirty), no errors.
func TestPreflightWrap_VaultDirty(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	// Leave an uncommitted file in the vault.
	if err := os.WriteFile(filepath.Join(cfg.VaultPath, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty: %v", err)
	}

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	withSurfaceCheck(t, func(_ string) error { return nil })

	tool := NewPreflightWrapTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res PreflightWrapResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !res.OK {
		t.Errorf("ok = false, want true (warnings don't gate); res = %+v", res)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors = %v, want empty", res.Errors)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Check != "vault_dirty" {
		t.Errorf("warnings = %v, want exactly one vault_dirty entry", res.Warnings)
	}
}

// TestPreflightWrap_ProjectDirty covers surface-pass + project-dirty.
// Expect ok=true, exactly one warning (project_dirty), no errors.
func TestPreflightWrap_ProjectDirty(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	if err := os.WriteFile(filepath.Join(projDir, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	t.Chdir(projDir)

	withSurfaceCheck(t, func(_ string) error { return nil })

	tool := NewPreflightWrapTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res PreflightWrapResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !res.OK {
		t.Errorf("ok = false, want true; res = %+v", res)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors = %v, want empty", res.Errors)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Check != "project_dirty" {
		t.Errorf("warnings = %v, want exactly one project_dirty entry", res.Warnings)
	}
}

// TestPreflightWrap_BothDirty covers surface-pass + vault-dirty +
// project-dirty. Expect ok=true with both warnings present.
func TestPreflightWrap_BothDirty(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	if err := os.WriteFile(filepath.Join(cfg.VaultPath, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty: %v", err)
	}

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	if err := os.WriteFile(filepath.Join(projDir, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	t.Chdir(projDir)

	withSurfaceCheck(t, func(_ string) error { return nil })

	tool := NewPreflightWrapTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res PreflightWrapResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !res.OK {
		t.Errorf("ok = false, want true; res = %+v", res)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors = %v, want empty", res.Errors)
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("warnings = %v, want 2 entries", res.Warnings)
	}
	gotChecks := map[string]bool{}
	for _, w := range res.Warnings {
		gotChecks[w.Check] = true
	}
	if !gotChecks["vault_dirty"] || !gotChecks["project_dirty"] {
		t.Errorf("warnings = %v, want both vault_dirty and project_dirty", res.Warnings)
	}
}

// TestPreflightWrap_SurfaceFail covers the gating case: surface check
// returns IncompatibleError. Expect ok=false, exactly one error
// (surface), no warnings (clean fixtures).
func TestPreflightWrap_SurfaceFail(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	withSurfaceCheck(t, func(_ string) error {
		return &surface.IncompatibleError{
			BinarySurface: 16,
			VaultSurface:  17,
			StampDir:      "/fake/vault/Projects/myproj/agentctx",
			LastWriter:    "abcd1234",
		}
	})

	tool := NewPreflightWrapTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res PreflightWrapResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.OK {
		t.Errorf("ok = true, want false (surface fail gates); res = %+v", res)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want empty", res.Warnings)
	}
	if len(res.Errors) != 1 || res.Errors[0].Check != "surface" {
		t.Fatalf("errors = %v, want exactly one surface entry", res.Errors)
	}
}
