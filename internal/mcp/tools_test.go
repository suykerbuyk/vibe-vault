// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/index"
)

// writeTestIndex creates a session-index.json in a temp directory.
func writeTestIndex(t *testing.T, entries map[string]index.SessionEntry) string {
	t.Helper()
	stateDir := t.TempDir()
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal test index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "session-index.json"), data, 0o644); err != nil {
		t.Fatalf("write test index: %v", err)
	}
	return stateDir
}

func TestGetProjectContextBasic(t *testing.T) {
	stateDir := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "myproject", Date: "2027-06-15",
			Title: "Add login", Summary: "Implemented OAuth login",
			Tag: "feature", CreatedAt: time.Now(),
		},
		"s2": {
			SessionID: "s2", Project: "myproject", Date: "2027-06-14",
			Title: "Setup CI", Summary: "Configured GitHub Actions",
			CreatedAt: time.Now().Add(-24 * time.Hour),
		},
	})

	tool := NewGetProjectContextTool(stateDir)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproject"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\nresult: %s", err, result)
	}
	if parsed["project"] != "myproject" {
		t.Errorf("project = %v, want myproject", parsed["project"])
	}
	sessions, ok := parsed["sessions"].([]interface{})
	if !ok {
		t.Fatal("expected sessions array")
	}
	if len(sessions) != 2 {
		t.Errorf("sessions count = %d, want 2", len(sessions))
	}
}

func TestGetProjectContextWithSections(t *testing.T) {
	stateDir := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "proj", Date: "2027-06-15",
			Title: "Work", Summary: "Did stuff", CreatedAt: time.Now(),
			Decisions: []string{"Use Go"}, OpenThreads: []string{"Fix bug"},
		},
	})

	tool := NewGetProjectContextTool(stateDir)
	result, err := tool.Handler(json.RawMessage(`{"project":"proj","sections":["summary","sessions"]}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal([]byte(result), &parsed)

	if _, ok := parsed["threads"]; ok {
		t.Error("threads should be filtered out")
	}
	if _, ok := parsed["decisions"]; ok {
		t.Error("decisions should be filtered out")
	}
}

func TestGetProjectContextEmptyIndex(t *testing.T) {
	stateDir := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetProjectContextTool(stateDir)
	result, err := tool.Handler(json.RawMessage(`{"project":"empty"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["project"] != "empty" {
		t.Errorf("project = %v, want empty", parsed["project"])
	}
}

func TestListProjectsBasic(t *testing.T) {
	stateDir := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "alpha", Date: "2027-06-15", CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "alpha", Date: "2027-06-14", CreatedAt: time.Now()},
		"s3": {SessionID: "s3", Project: "beta", Date: "2027-06-10", CreatedAt: time.Now()},
	})

	tool := NewListProjectsTool(stateDir)
	result, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var projects []map[string]interface{}
	if err := json.Unmarshal([]byte(result), &projects); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	// Sorted alphabetically
	if projects[0]["name"] != "alpha" {
		t.Errorf("first project = %v, want alpha", projects[0]["name"])
	}
	if projects[1]["name"] != "beta" {
		t.Errorf("second project = %v, want beta", projects[1]["name"])
	}
	if projects[0]["session_count"].(float64) != 2 {
		t.Errorf("alpha session_count = %v, want 2", projects[0]["session_count"])
	}
}

func TestListProjectsEmptyIndex(t *testing.T) {
	stateDir := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewListProjectsTool(stateDir)
	result, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var projects []map[string]interface{}
	if err := json.Unmarshal([]byte(result), &projects); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}
