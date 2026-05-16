// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewRepoViewTools_Catalogue(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod": "module example.com/x\n",
	})
	tools, exec := NewRepoViewTools(view)
	if exec == nil {
		t.Fatal("executor is nil")
	}
	wantNames := []string{"read_file", "grep", "list_dir", "check_ref"}
	if len(tools) != len(wantNames) {
		t.Fatalf("tool count = %d, want %d", len(tools), len(wantNames))
	}
	for i, want := range wantNames {
		if tools[i].Name != want {
			t.Errorf("tools[%d].Name = %q, want %q", i, tools[i].Name, want)
		}
		if tools[i].Description == "" {
			t.Errorf("tools[%d] (%s) has empty description", i, tools[i].Name)
		}
		// Each InputSchema must be valid JSON describing an object schema.
		var schema map[string]any
		if err := json.Unmarshal(tools[i].InputSchema, &schema); err != nil {
			t.Errorf("tools[%d] (%s) InputSchema is not valid JSON: %v", i, tools[i].Name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tools[%d] (%s) InputSchema type = %v, want \"object\"", i, tools[i].Name, schema["type"])
		}
	}
}

func TestExecReadFile_HappyPath(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"src/main.go": "package main\nfunc main() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("read_file", json.RawMessage(`{"path":"src/main.go"}`))
	if isErr {
		t.Fatalf("isError = true, payload: %s", out)
	}
	var result struct {
		Path    string `json:"path"`
		Size    int    `json:"size"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if result.Path != "src/main.go" || result.Size == 0 || !strings.Contains(result.Content, "func main") {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestExecReadFile_NotInView(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("read_file", json.RawMessage(`{"path":"ghost.go"}`))
	if !isErr {
		t.Errorf("expected isError=true for unknown path; payload: %s", out)
	}
	if !strings.Contains(string(out), "not in the repo view") {
		t.Errorf("expected ReadFile error in payload, got: %s", out)
	}
}

func TestExecReadFile_BinaryRejected(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"data.bin": "abc\x00def",
	})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("read_file", json.RawMessage(`{"path":"data.bin"}`))
	if !isErr {
		t.Errorf("expected isError=true for binary file; payload: %s", out)
	}
	if !strings.Contains(string(out), "binary") {
		t.Errorf("expected 'binary' in error payload, got: %s", out)
	}
}

func TestExecReadFile_MissingPathArg(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("read_file", json.RawMessage(`{}`))
	if !isErr {
		t.Errorf("expected isError=true for missing path; payload: %s", out)
	}
	if !strings.Contains(string(out), "path is required") {
		t.Errorf("expected 'path is required' in payload, got: %s", out)
	}
}

func TestExecGrep_HappyPath(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"a.go": "package a\nfunc Hello() {}\n",
		"b.go": "package b\nfunc World() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("grep", json.RawMessage(`{"pattern":"^func "}`))
	if isErr {
		t.Fatalf("isError = true, payload: %s", out)
	}
	var result struct {
		Pattern   string `json:"pattern"`
		Total     int    `json:"total"`
		Truncated bool   `json:"truncated"`
		Hits      []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if result.Total != 2 || len(result.Hits) != 2 || result.Truncated {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestExecGrep_PathPrefixFilters(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"cmd/app/main.go":  "package main\nfunc main() {}\n",
		"internal/x/x.go":  "package x\nfunc X() {}\n",
		"internal/y/y.go":  "package y\nfunc Y() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	out, _ := exec("grep", json.RawMessage(`{"pattern":"^func ","path_prefix":"internal/"}`))
	var result struct {
		Total int `json:"total"`
		Hits  []struct {
			Path string `json:"path"`
		} `json:"hits"`
	}
	_ = json.Unmarshal(out, &result)
	if result.Total != 2 {
		t.Errorf("path_prefix filter: got %d hits, want 2; payload: %s", result.Total, out)
	}
	for _, h := range result.Hits {
		if !strings.HasPrefix(h.Path, "internal/") {
			t.Errorf("hit %q escaped path_prefix filter", h.Path)
		}
	}
}

func TestExecGrep_HitCapTruncates(t *testing.T) {
	// 60 matching lines in one file → grepHitCap=50 truncates.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("x\n")
	}
	view := viewFromFiles(t, map[string]string{"many.txt": sb.String()})
	_, exec := NewRepoViewTools(view)
	out, _ := exec("grep", json.RawMessage(`{"pattern":"^x$"}`))
	var result struct {
		Total     int  `json:"total"`
		Truncated bool `json:"truncated"`
	}
	_ = json.Unmarshal(out, &result)
	if result.Total != grepHitCap || !result.Truncated {
		t.Errorf("hit cap: got total=%d truncated=%v, want %d/true; payload: %s",
			result.Total, result.Truncated, grepHitCap, out)
	}
}

func TestExecGrep_InvalidPattern(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("grep", json.RawMessage(`{"pattern":"(unclosed"}`))
	if !isErr {
		t.Errorf("expected isError=true for invalid regexp; payload: %s", out)
	}
}

func TestExecListDir_Root(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod":           "module m\n",
		"README.md":        "# r\n",
		"cmd/app/main.go":  "package main\n",
		"internal/x/x.go":  "package x\n",
	})
	_, exec := NewRepoViewTools(view)
	out, _ := exec("list_dir", json.RawMessage(`{"path":""}`))
	var result struct {
		Entries []string `json:"entries"`
		Count   int      `json:"count"`
	}
	_ = json.Unmarshal(out, &result)
	want := []string{"README.md", "cmd/", "go.mod", "internal/"}
	if result.Count != len(want) {
		t.Errorf("root count = %d, want %d; entries: %v", result.Count, len(want), result.Entries)
	}
	for i, w := range want {
		if i >= len(result.Entries) || result.Entries[i] != w {
			t.Errorf("entries[%d] = %q, want %q (full: %v)", i, result.Entries[i], w, result.Entries)
		}
	}
}

func TestExecListDir_Subdir(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"internal/mcp/server.go":  "package mcp\n",
		"internal/mcp/tools.go":   "package mcp\n",
		"internal/mcp/sub/a.go":   "package sub\n",
		"internal/other.go":       "package internal\n",
	})
	_, exec := NewRepoViewTools(view)
	out, _ := exec("list_dir", json.RawMessage(`{"path":"internal/mcp"}`))
	var result struct {
		Entries []string `json:"entries"`
	}
	_ = json.Unmarshal(out, &result)
	want := []string{"server.go", "sub/", "tools.go"}
	if len(result.Entries) != len(want) {
		t.Fatalf("subdir entries = %v, want %v", result.Entries, want)
	}
	for i, w := range want {
		if result.Entries[i] != w {
			t.Errorf("entries[%d] = %q, want %q", i, result.Entries[i], w)
		}
	}
}

func TestExecListDir_Recursive(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"internal/mcp/server.go": "package mcp\n",
		"internal/mcp/sub/a.go":  "package sub\n",
		"internal/mcp/sub/b.go":  "package sub\n",
	})
	_, exec := NewRepoViewTools(view)
	out, _ := exec("list_dir", json.RawMessage(`{"path":"internal/mcp","recursive":true}`))
	var result struct {
		Entries []string `json:"entries"`
	}
	_ = json.Unmarshal(out, &result)
	want := []string{"server.go", "sub/a.go", "sub/b.go"}
	if len(result.Entries) != len(want) {
		t.Fatalf("recursive entries = %v, want %v", result.Entries, want)
	}
	for i, w := range want {
		if result.Entries[i] != w {
			t.Errorf("entries[%d] = %q, want %q", i, result.Entries[i], w)
		}
	}
}

func TestExecListDir_Empty(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, _ := exec("list_dir", json.RawMessage(`{"path":"does/not/exist"}`))
	var result struct {
		Entries []string `json:"entries"`
		Count   int      `json:"count"`
	}
	_ = json.Unmarshal(out, &result)
	if result.Count != 0 {
		t.Errorf("non-existent dir should return 0 entries; got %v", result.Entries)
	}
}

func TestExecCheckRef_CleanRef(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod":          "module example.com/x\n",
		"internal/foo.go": "package foo\n\nfunc Build() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("check_ref", json.RawMessage(`{"ref":"internal/foo.go:Build"}`))
	if isErr {
		t.Fatalf("isError=true; payload: %s", out)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if !result.OK {
		t.Errorf("expected ok=true for clean ref, payload: %s", out)
	}
}

func TestExecCheckRef_CleanPackageForm(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod":          "module example.com/x\n",
		"internal/foo.go": "package foo\nfunc Build() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	// Package-form ref: directory + symbol. Verifier greps the dir.
	out, isErr := exec("check_ref", json.RawMessage(`{"ref":"internal:Build"}`))
	if isErr {
		t.Fatalf("isError=true; payload: %s", out)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if !result.OK {
		t.Errorf("expected ok=true for package-form ref, payload: %s", out)
	}
}

func TestExecCheckRef_MissingFileHardError(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod": "module example.com/x\n",
	})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("check_ref", json.RawMessage(`{"ref":"no/such/path.go"}`))
	if isErr {
		t.Fatalf("isError=true; payload: %s", out)
	}
	var result struct {
		OK       bool   `json:"ok"`
		Severity string `json:"severity"`
		Kind     string `json:"kind"`
		Detail   string `json:"detail"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if result.OK {
		t.Errorf("expected ok=false for missing path, payload: %s", out)
	}
	if result.Severity != "error" {
		t.Errorf("severity = %q, want \"error\"", result.Severity)
	}
	if result.Kind != "missing-file" {
		t.Errorf("kind = %q, want \"missing-file\"", result.Kind)
	}
}

func TestExecCheckRef_WrongFileSiblingHasSymbolWarning(t *testing.T) {
	// File `pkg/wrong.go` does not exist, but the symbol Build lives in
	// `pkg/real.go` — the sibling-fallback downgrades to weak-match,
	// which check_ref surfaces as severity:"warning".
	view := viewFromFiles(t, map[string]string{
		"go.mod":       "module example.com/x\n",
		"pkg/real.go":  "package pkg\nfunc Build() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("check_ref", json.RawMessage(`{"ref":"pkg/wrong.go:Build"}`))
	if isErr {
		t.Fatalf("isError=true; payload: %s", out)
	}
	var result struct {
		OK       bool   `json:"ok"`
		Severity string `json:"severity"`
		Kind     string `json:"kind"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if result.OK {
		t.Errorf("expected ok=false for weak-match, payload: %s", out)
	}
	if result.Severity != "warning" {
		t.Errorf("severity = %q, want \"warning\"", result.Severity)
	}
	if result.Kind != "weak-match" {
		t.Errorf("kind = %q, want \"weak-match\"", result.Kind)
	}
}

func TestExecCheckRef_MissingSymbol(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod":       "module example.com/x\n",
		"pkg/real.go":  "package pkg\nfunc Build() {}\n",
	})
	_, exec := NewRepoViewTools(view)
	// File exists, symbol does not (and no sibling has it either).
	out, isErr := exec("check_ref", json.RawMessage(`{"ref":"pkg/real.go:Ghost"}`))
	if isErr {
		t.Fatalf("isError=true; payload: %s", out)
	}
	var result struct {
		OK       bool   `json:"ok"`
		Severity string `json:"severity"`
		Kind     string `json:"kind"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v; payload: %s", err, out)
	}
	if result.OK || result.Severity != "error" || result.Kind != "missing-symbol" {
		t.Errorf("expected error/missing-symbol, got %+v (payload: %s)", result, out)
	}
}

func TestExecCheckRef_MissingRefArg(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("check_ref", json.RawMessage(`{}`))
	if !isErr {
		t.Errorf("expected isError=true when ref missing; payload: %s", out)
	}
	if !strings.Contains(string(out), "ref is required") {
		t.Errorf("expected 'ref is required' in payload, got: %s", out)
	}
}

func TestExecutor_UnknownTool(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("nuke", json.RawMessage(`{}`))
	if !isErr {
		t.Errorf("expected isError=true for unknown tool; payload: %s", out)
	}
	if !strings.Contains(string(out), "unknown tool") {
		t.Errorf("expected 'unknown tool' in payload, got: %s", out)
	}
}

func TestExecutor_InvalidJSON(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"a.go": "package a\n"})
	_, exec := NewRepoViewTools(view)
	out, isErr := exec("read_file", json.RawMessage(`{not json`))
	if !isErr {
		t.Errorf("expected isError=true for malformed input; payload: %s", out)
	}
}
