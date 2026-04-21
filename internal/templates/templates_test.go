// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package templates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	reg := New()
	entries := reg.List()
	if len(entries) != 23 {
		t.Errorf("expected 23 entries, got %d", len(entries))
		for _, e := range entries {
			t.Logf("  %s", e.RelPath)
		}
	}
}

func TestDefaultContent(t *testing.T) {
	reg := New()

	// Known template returns content
	content, ok := reg.DefaultContent("agentctx/workflow.md")
	if !ok {
		t.Fatal("expected agentctx/workflow.md to exist")
	}
	if len(content) == 0 {
		t.Error("expected non-empty content")
	}

	// Unknown template returns false
	_, ok = reg.DefaultContent("nonexistent.md")
	if ok {
		t.Error("expected false for unknown template")
	}
}

func TestDefaultContentReturnsCopy(t *testing.T) {
	reg := New()
	a, _ := reg.DefaultContent("agentctx/workflow.md")
	b, _ := reg.DefaultContent("agentctx/workflow.md")

	// Mutating the first slice should not affect the second
	if len(a) > 0 {
		a[0] = 0xFF
	}
	if len(b) > 0 && b[0] == 0xFF {
		t.Error("DefaultContent returned a shared slice instead of a copy")
	}
}

func TestHas(t *testing.T) {
	reg := New()
	if !reg.Has("agentctx/workflow.md") {
		t.Error("expected Has to return true for known template")
	}
	if reg.Has("nonexistent.md") {
		t.Error("expected Has to return false for unknown template")
	}
}

func TestCompare(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	// Use explicit template names so we don't depend on sort order
	const (
		defaultTmpl    = "README.md"
		customizedTmpl = "session-summary.md"
		missingTmpl    = "agentctx/workflow.md"
	)

	// Write default content for one template
	defaultContent, _ := reg.DefaultContent(defaultTmpl)
	defaultPath := filepath.Join(dir, defaultTmpl)
	os.MkdirAll(filepath.Dir(defaultPath), 0o755)
	os.WriteFile(defaultPath, defaultContent, 0o644)

	// Write customized content for another
	customPath := filepath.Join(dir, customizedTmpl)
	os.MkdirAll(filepath.Dir(customPath), 0o755)
	os.WriteFile(customPath, []byte("custom content"), 0o644)

	// Leave missingTmpl absent

	statuses := reg.Compare(dir)

	// Build a lookup by relPath for assertions
	byPath := make(map[string]Status)
	for _, fs := range statuses {
		byPath[fs.Entry.RelPath] = fs.Status
	}

	if got := byPath[defaultTmpl]; got != StatusDefault {
		t.Errorf("%s: expected default, got %s", defaultTmpl, got)
	}
	if got := byPath[customizedTmpl]; got != StatusCustomized {
		t.Errorf("%s: expected customized, got %s", customizedTmpl, got)
	}
	if got := byPath[missingTmpl]; got != StatusMissing {
		t.Errorf("%s: expected missing, got %s", missingTmpl, got)
	}
}

func TestReset(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	const relPath = "session-summary.md"

	// Reset creates a missing file
	a, err := reg.Reset(dir, relPath)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if a.Action != "created" {
		t.Errorf("expected action=created, got %s", a.Action)
	}

	// Verify content matches default
	written, _ := os.ReadFile(filepath.Join(dir, relPath))
	expected, _ := reg.DefaultContent(relPath)
	if string(written) != string(expected) {
		t.Error("written content does not match default")
	}

	// Modify and reset again — should be "reset"
	os.WriteFile(filepath.Join(dir, relPath), []byte("modified"), 0o644)
	a, err = reg.Reset(dir, relPath)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if a.Action != "reset" {
		t.Errorf("expected action=reset, got %s", a.Action)
	}
}

func TestResetAll(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	actions, err := reg.ResetAll(dir)
	if err != nil {
		t.Fatalf("reset all: %v", err)
	}

	if len(actions) != 23 {
		t.Errorf("expected 23 actions, got %d", len(actions))
	}

	// All should now be "default"
	for _, fs := range reg.Compare(dir) {
		if fs.Status != StatusDefault {
			t.Errorf("%s: expected default after reset, got %s", fs.Entry.RelPath, fs.Status)
		}
	}
}

func TestResetUnknown(t *testing.T) {
	reg := New()
	_, err := reg.Reset(t.TempDir(), "nonexistent.md")
	if err == nil {
		t.Error("expected error for unknown template")
	}
}
