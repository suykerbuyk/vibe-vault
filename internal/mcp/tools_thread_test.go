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

// resumeWithThreads is a minimal resume.md fixture with an ## Open Threads section.
const resumeWithThreads = `# resume

## Current State

**Project:** test

## Open Threads

### alpha

alpha body

### beta — some context

beta body

### Carried forward

- item one

## Project History

nothing here
`

// resumeEmptyThreads has an empty ## Open Threads section.
const resumeEmptyThreads = `# resume

## Open Threads

## Project History

nothing here
`

// ── vv_thread_insert ─────────────────────────────────────────────────────────

func TestThreadInsert_ModeTop(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadInsertTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":  "proj",
		"position": map[string]any{"mode": "top"},
		"slug":     "new-thread",
		"body":     "new thread body",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res["bytes_written"].(float64) == 0 {
		t.Error("bytes_written should be non-zero")
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if !strings.Contains(got, "### new-thread") {
		t.Error("new thread heading missing")
	}
	// Inserted at top means it appears before alpha.
	newIdx := strings.Index(got, "### new-thread")
	alphaIdx := strings.Index(got, "### alpha")
	if newIdx >= alphaIdx {
		t.Errorf("top insert should precede alpha: new=%d alpha=%d", newIdx, alphaIdx)
	}
}

func TestThreadInsert_ModeBottom(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadInsertTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":  "proj",
		"position": map[string]any{"mode": "bottom"},
		"slug":     "new-thread",
		"body":     "new thread body",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if !strings.Contains(got, "### new-thread") {
		t.Error("new thread heading missing")
	}
	// Should appear after Carried forward.
	cfIdx := strings.Index(got, "### Carried forward")
	newIdx := strings.Index(got, "### new-thread")
	if newIdx <= cfIdx {
		t.Errorf("bottom insert should follow Carried forward: cf=%d new=%d", cfIdx, newIdx)
	}
}

func TestThreadInsert_ModeAfter(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadInsertTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":  "proj",
		"position": map[string]any{"mode": "after", "anchor_slug": "alpha"},
		"slug":     "middle",
		"body":     "middle body",
	})
	_, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	alphaIdx := strings.Index(got, "### alpha")
	midIdx := strings.Index(got, "### middle")
	betaIdx := strings.Index(got, "### beta")
	if alphaIdx >= midIdx || midIdx >= betaIdx {
		t.Errorf("after-alpha order wrong: alpha=%d mid=%d beta=%d", alphaIdx, midIdx, betaIdx)
	}
}

func TestThreadInsert_SlugAlreadyExists(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadInsertTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":  "proj",
		"position": map[string]any{"mode": "top"},
		"slug":     "alpha",
		"body":     "dup body",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want already-exists error, got %v", err)
	}
}

func TestThreadInsert_MissingSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadInsertTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":  "proj",
		"position": map[string]any{"mode": "top"},
		"slug":     "",
		"body":     "body",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("want slug-required error, got %v", err)
	}
}

func TestThreadInsert_ProjectNotFound(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewThreadInsertTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":  "ghost",
		"position": map[string]any{"mode": "top"},
		"slug":     "new",
		"body":     "body",
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("want error for missing project")
	}
}

// ── vv_thread_replace ────────────────────────────────────────────────────────

func TestThreadReplace_Basic(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadReplaceTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "alpha",
		"body":    "updated alpha body",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if !strings.Contains(got, "updated alpha body") {
		t.Error("missing updated content")
	}
	if strings.Contains(got, "\nalpha body\n") {
		t.Error("old alpha body should be replaced")
	}
	// Other threads preserved.
	if !strings.Contains(got, "beta body") {
		t.Error("beta body should be preserved")
	}
}

func TestThreadReplace_SlugWithEmDash(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadReplaceTool(c)
	// "beta — some context" → slug is "beta"
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "beta",
		"body":    "updated beta body",
	})
	_, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	if !strings.Contains(string(data), "updated beta body") {
		t.Error("missing updated content")
	}
}

func TestThreadReplace_SlugNotFound(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadReplaceTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "ghost",
		"body":    "body",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("want not-found error, got %v", err)
	}
	// Error should list available slugs.
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should list available slugs: %v", err)
	}
}

func TestThreadReplace_CarriedForwardRejected(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadReplaceTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "Carried forward",
		"body":    "new bullets",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("want carried-forward rejection, got %v", err)
	}
}

func TestThreadReplace_AmbiguousMultiMatch(t *testing.T) {
	doc := `# resume

## Open Threads

### dup

body1

### dup

body2

## History
`
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": doc,
	})
	tool := NewThreadReplaceTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "dup",
		"body":    "new body",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("multi-match should not return error, got: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res["candidates_warning"] == nil || res["candidates_warning"].(string) == "" {
		t.Error("multi-match should include candidates_warning")
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	if !strings.Contains(string(data), "new body") {
		t.Error("new body should be present")
	}
}

func TestThreadReplace_HTMLCommentInBodyRejected(t *testing.T) {
	doc := `# resume

## Open Threads

### alpha

<!-- marker -->
alpha body

## History
`
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": doc,
	})
	tool := NewThreadReplaceTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "alpha",
		"body":    "new body",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "marker preservation") {
		t.Fatalf("want marker-preservation error, got %v", err)
	}
}

func TestThreadReplace_MissingSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadReplaceTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"body":    "body",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("want slug-required error, got %v", err)
	}
}

// ── vv_thread_remove ─────────────────────────────────────────────────────────

func TestThreadRemove_Basic(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "alpha",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if strings.Contains(got, "### alpha") {
		t.Error("alpha heading should be gone")
	}
	if strings.Contains(got, "alpha body") {
		t.Error("alpha body should be gone")
	}
	if !strings.Contains(got, "beta body") {
		t.Error("beta section should be preserved")
	}
}

func TestThreadRemove_CarriedForwardRejected(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "Carried forward",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatalf("want carried-forward rejection, got %v", err)
	}
}

func TestThreadRemove_SlugNotFound(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "ghost",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("want not-found error, got %v", err)
	}
}

func TestThreadRemove_AmbiguousMultiMatch(t *testing.T) {
	doc := `# resume

## Open Threads

### dup

body1

### dup

body2

## History
`
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": doc,
	})
	tool := NewThreadRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "dup",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("multi-match should not error, got: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res["candidates_warning"] == nil || res["candidates_warning"].(string) == "" {
		t.Error("multi-match should include candidates_warning")
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	// Second dup body should survive.
	if !strings.Contains(string(data), "body2") {
		t.Error("second occurrence should survive")
	}
}

func TestThreadRemove_MissingSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithThreads,
	})
	tool := NewThreadRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("want slug-required error, got %v", err)
	}
}

func TestThreadRemove_EmptyOpenThreads(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeEmptyThreads,
	})
	tool := NewThreadRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "ghost",
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("want error for empty Open Threads section")
	}
}
