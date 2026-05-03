// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNewCheckToolchainTool_Schema verifies the static tool definition: the
// canonical name, a non-empty description, and an InputSchema that parses as
// an empty-properties object schema (no required inputs).
func TestNewCheckToolchainTool_Schema(t *testing.T) {
	tool := NewCheckToolchainTool()

	if tool.Definition.Name != "vv_check_toolchain" {
		t.Errorf("Name = %q, want %q", tool.Definition.Name, "vv_check_toolchain")
	}
	if tool.Definition.Description == "" {
		t.Error("Description must be non-empty")
	}

	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(tool.Definition.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema does not parse as JSON: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("schema.type = %q, want %q", schema.Type, "object")
	}
	if len(schema.Properties) != 0 {
		t.Errorf("schema.properties expected empty, got %d entries", len(schema.Properties))
	}
	if len(schema.Required) != 0 {
		t.Errorf("schema.required expected empty, got %v", schema.Required)
	}
}

// TestNewCheckToolchainTool_HandlerReturnsValidJSON invokes the handler with
// nil arguments (the schema declares no inputs) and asserts the response is a
// JSON array of objects with exactly the {name, status, detail} key set.
func TestNewCheckToolchainTool_HandlerReturnsValidJSON(t *testing.T) {
	tool := NewCheckToolchainTool()

	out, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("Handler output is not a JSON array: %v\n%s", err, out)
	}
	if len(entries) == 0 {
		t.Fatal("Handler returned empty array — expected one entry per toolchain spec")
	}

	wantKeys := map[string]bool{"name": true, "status": true, "detail": true}
	for i, e := range entries {
		if len(e) != len(wantKeys) {
			t.Errorf("entry[%d] has %d keys, want %d (%v)", i, len(e), len(wantKeys), e)
		}
		for k := range wantKeys {
			if _, ok := e[k]; !ok {
				t.Errorf("entry[%d] missing key %q", i, k)
			}
		}
	}
}

// TestNewCheckToolchainTool_HandlerStatusValues asserts that every entry's
// status is one of the lowercased values "pass" or "warn". The probe never
// emits "fail" — missing binaries and broken --version invocations both
// surface as "warn" per checkToolchainSpec().
func TestNewCheckToolchainTool_HandlerStatusValues(t *testing.T) {
	tool := NewCheckToolchainTool()

	out, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	var entries []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse handler output: %v", err)
	}

	for _, e := range entries {
		switch e.Status {
		case "pass", "warn":
			// expected
		default:
			t.Errorf("entry %q has unexpected status %q (want pass|warn)", e.Name, e.Status)
		}
	}
}

// TestNewCheckToolchainTool_HandlerNamesPrefixed asserts that every entry's
// name carries the canonical "tool:" prefix established by
// checkToolchainSpec() so wire consumers can route toolchain results
// unambiguously alongside other check namespaces.
func TestNewCheckToolchainTool_HandlerNamesPrefixed(t *testing.T) {
	tool := NewCheckToolchainTool()

	out, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse handler output: %v", err)
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name, "tool:") {
			t.Errorf("entry name %q lacks required %q prefix", e.Name, "tool:")
		}
	}
}

// TestNewCheckToolchainTool_HandlerOrderMatchesSpecs asserts the handler
// preserves the canonical probe order from check.toolchainSpecs. The literal
// sequence is hardcoded here because toolchainSpecs is package-private to
// internal/check; cross-package tests cannot import it. The order is part of
// the operator-facing contract (alignment with `vv check`'s human report and
// agent prompts that grep for specific lines), so a hardcoded assertion is
// the right level of coupling — drift would break agents downstream.
func TestNewCheckToolchainTool_HandlerOrderMatchesSpecs(t *testing.T) {
	tool := NewCheckToolchainTool()

	out, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse handler output: %v", err)
	}

	want := []string{
		"tool:go",
		"tool:golangci-lint",
		"tool:gh",
		"tool:make",
		"tool:git",
	}
	if len(entries) != len(want) {
		t.Fatalf("entry count = %d, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i].Name != w {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
}
