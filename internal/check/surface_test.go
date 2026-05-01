// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

func TestCheckSurface_NoVault(t *testing.T) {
	cfg := config.Config{VaultPath: ""}
	got := CheckSurface(cfg)
	if got.Status != Warn {
		t.Fatalf("status: got %v, want Warn", got.Status)
	}
	if got.Detail != "no vault configured" {
		t.Fatalf("detail: got %q, want %q", got.Detail, "no vault configured")
	}
	if got.Name != "surface" {
		t.Fatalf("name: got %q, want %q", got.Name, "surface")
	}
}

func TestCheckSurface_VaultBelowOrEqual(t *testing.T) {
	vault := t.TempDir()
	// Build a Projects/<p>/agentctx/.surface stamp at the binary's version.
	stampDir := filepath.Join(vault, "Projects", "demo", "agentctx")
	if err := os.MkdirAll(stampDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "surface = " + itoa(surface.MCPSurfaceVersion) + "\n" +
		"last_writer = \"abc12345\"\n" +
		"last_write_at = \"2026-01-01T00:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(stampDir, ".surface"), []byte(body), 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}

	cfg := config.Config{VaultPath: vault}
	got := CheckSurface(cfg)
	if got.Status != Pass {
		t.Fatalf("status: got %v, want Pass; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "binary v") {
		t.Fatalf("detail: got %q, want to contain 'binary v'", got.Detail)
	}
}

func TestCheckSurface_VaultAhead(t *testing.T) {
	vault := t.TempDir()
	stampDir := filepath.Join(vault, "Projects", "demo", "agentctx")
	if err := os.MkdirAll(stampDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "surface = 99\n" +
		"last_writer = \"abc12345\"\n" +
		"last_write_at = \"2030-01-01T00:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(stampDir, ".surface"), []byte(body), 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}

	cfg := config.Config{VaultPath: vault}
	got := CheckSurface(cfg)
	if got.Status != Fail {
		t.Fatalf("status: got %v, want Fail; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, stampDir) {
		t.Fatalf("detail: got %q, want to contain stamp dir %q", got.Detail, stampDir)
	}
	if !strings.Contains(got.Detail, "v99") {
		t.Fatalf("detail: got %q, want to contain 'v99'", got.Detail)
	}
}

func TestCheckSurface_VaultUnreachable(t *testing.T) {
	// surface.CheckCompatible returns nil when vaultPath does not exist
	// (best-effort), so this should Pass at the check layer too.
	cfg := config.Config{VaultPath: "/nonexistent/vault/path/does/not/exist"}
	got := CheckSurface(cfg)
	if got.Status != Pass {
		t.Fatalf("status: got %v, want Pass (CheckCompatible degrades to nil); detail=%q",
			got.Status, got.Detail)
	}
}

// itoa is a tiny helper to keep the test file free of fmt/strconv noise
// in places where only the integer is needed.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
