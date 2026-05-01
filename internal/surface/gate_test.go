// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package surface

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureGateStderr swaps gateStderr for a pipe and returns a function that
// closes the writer and yields the captured bytes. Each test that uses it
// must call the returned reader-fn before asserting.
func captureGateStderr(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := gateStderr
	gateStderr = w
	t.Cleanup(func() { gateStderr = old })
	return func() string {
		_ = w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		return buf.String()
	}
}

func seedAboveBinary(t *testing.T, vault string) string {
	t.Helper()
	projDir := filepath.Join(vault, "Projects", "p1", "agentctx")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteStamp(projDir, MCPSurfaceVersion+1, "ffeeddcc"); err != nil {
		t.Fatalf("seed stamp: %v", err)
	}
	return projDir
}

func seedAtBinary(t *testing.T, vault string) {
	t.Helper()
	projDir := filepath.Join(vault, "Projects", "p1", "agentctx")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteStamp(projDir, MCPSurfaceVersion, "00112233"); err != nil {
		t.Fatalf("seed stamp: %v", err)
	}
}

func TestEnforceFailStop_Pass(t *testing.T) {
	vault := t.TempDir()
	seedAtBinary(t, vault)
	read := captureGateStderr(t)

	if err := EnforceFailStop(vault); err != nil {
		t.Fatalf("EnforceFailStop: %v", err)
	}
	out := read()
	if out != "" {
		t.Errorf("expected no stderr, got %q", out)
	}
}

func TestEnforceFailStop_Fail(t *testing.T) {
	vault := t.TempDir()
	seedAboveBinary(t, vault)
	read := captureGateStderr(t)

	err := EnforceFailStop(vault)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if _, ok := err.(*IncompatibleError); !ok {
		t.Errorf("expected *IncompatibleError, got %T", err)
	}
	out := read()
	if out != "" {
		t.Errorf("expected no stderr (caller logs), got %q", out)
	}
}

func TestEnforceFailStop_WarnOverride(t *testing.T) {
	vault := t.TempDir()
	seedAboveBinary(t, vault)
	t.Setenv("VV_SURFACE_GATE", "warn")
	read := captureGateStderr(t)

	if err := EnforceFailStop(vault); err != nil {
		t.Fatalf("expected nil under VV_SURFACE_GATE=warn, got %v", err)
	}
	out := read()
	if !strings.Contains(out, "this binary supports MCP surface") {
		t.Errorf("expected warning to stderr, got %q", out)
	}
	// Single-line emission: one trailing newline ⇒ exactly one line break.
	if strings.Count(out, "\n") < 1 {
		t.Errorf("expected at least one newline (single warning), got %q", out)
	}
}

func TestEnforceFailStop_VaultUnreachable(t *testing.T) {
	read := captureGateStderr(t)
	if err := EnforceFailStop(""); err != nil {
		t.Fatalf("empty vault path should be no-op, got %v", err)
	}
	if err := EnforceFailStop(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing vault path should be no-op, got %v", err)
	}
	if out := read(); out != "" {
		t.Errorf("unreachable vault should not write to stderr, got %q", out)
	}
}

func TestEnforceWarnOnly_Pass(t *testing.T) {
	vault := t.TempDir()
	seedAtBinary(t, vault)
	read := captureGateStderr(t)

	EnforceWarnOnly(vault)
	if out := read(); out != "" {
		t.Errorf("expected no stderr on clean vault, got %q", out)
	}
}

func TestEnforceWarnOnly_Fail(t *testing.T) {
	vault := t.TempDir()
	seedAboveBinary(t, vault)
	read := captureGateStderr(t)

	EnforceWarnOnly(vault)
	out := read()
	if !strings.Contains(out, "this binary supports MCP surface") {
		t.Errorf("expected warning text, got %q", out)
	}
}

func TestEnforceWarnOnly_QuietSuppresses(t *testing.T) {
	vault := t.TempDir()
	seedAboveBinary(t, vault)
	t.Setenv("VV_SURFACE_QUIET", "1")
	read := captureGateStderr(t)

	EnforceWarnOnly(vault)
	if out := read(); out != "" {
		t.Errorf("expected silence under VV_SURFACE_QUIET=1, got %q", out)
	}
}

func TestEnforceWarnOnly_VaultUnreachable(t *testing.T) {
	read := captureGateStderr(t)
	EnforceWarnOnly("")
	EnforceWarnOnly(filepath.Join(t.TempDir(), "nope"))
	if out := read(); out != "" {
		t.Errorf("unreachable vault should be silent, got %q", out)
	}
}
