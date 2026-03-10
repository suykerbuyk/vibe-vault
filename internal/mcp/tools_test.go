// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/index"
)

// writeTestVault creates a vault directory with a session-index.json and optional
// vault files (e.g., "Projects/myproj/knowledge.md" → content).
// Returns a config.Config with VaultPath set to the temp vault root.
func writeTestVault(t *testing.T, entries map[string]index.SessionEntry, vaultFiles map[string]string) config.Config {
	t.Helper()
	vaultRoot := t.TempDir()
	stateDir := filepath.Join(vaultRoot, ".vibe-vault")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal test index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "session-index.json"), data, 0o644); err != nil {
		t.Fatalf("write test index: %v", err)
	}
	for relPath, content := range vaultFiles {
		absPath := filepath.Join(vaultRoot, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("mkdir vault file dir: %v", err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write vault file: %v", err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.VaultPath = vaultRoot
	return cfg
}

// writeTestIndex creates a session-index.json in a vault layout. Backward-compat wrapper.
func writeTestIndex(t *testing.T, entries map[string]index.SessionEntry) config.Config {
	t.Helper()
	return writeTestVault(t, entries, nil)
}

func TestGetProjectContextBasic(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
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

	tool := NewGetProjectContextTool(cfg)
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
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "proj", Date: "2027-06-15",
			Title: "Work", Summary: "Did stuff", CreatedAt: time.Now(),
			Decisions: []string{"Use Go"}, OpenThreads: []string{"Fix bug"},
		},
	})

	tool := NewGetProjectContextTool(cfg)
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
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetProjectContextTool(cfg)
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

func TestGetProjectContextDefaultMaxTokens(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", CreatedAt: time.Now()},
	})
	cfg.MCP.DefaultMaxTokens = 500

	tool := NewGetProjectContextTool(cfg)
	// Call without max_tokens — should use config default
	_, err := tool.Handler(json.RawMessage(`{"project":"p"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
}

func TestListProjectsBasic(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "alpha", Date: "2027-06-15", CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "alpha", Date: "2027-06-14", CreatedAt: time.Now()},
		"s3": {SessionID: "s3", Project: "beta", Date: "2027-06-10", CreatedAt: time.Now()},
	})

	tool := NewListProjectsTool(cfg)
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
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewListProjectsTool(cfg)
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

// --- search_sessions tests ---

func TestSearchSessionsQueryFilter(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", Title: "Add OAuth login", CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "p", Date: "2027-06-14", Title: "Fix CI pipeline", CreatedAt: time.Now()},
	})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"query":"oauth"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sessions))
	}
	if sessions[0]["title"] != "Add OAuth login" {
		t.Errorf("title = %v", sessions[0]["title"])
	}
}

func TestSearchSessionsProjectFilter(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "alpha", Date: "2027-06-15", Title: "Work", CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "beta", Date: "2027-06-14", Title: "Work", CreatedAt: time.Now()},
	})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"alpha"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sessions))
	}
}

func TestSearchSessionsDateFilter(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", Title: "New", CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "p", Date: "2027-06-10", Title: "Old", CreatedAt: time.Now()},
		"s3": {SessionID: "s3", Project: "p", Date: "2027-06-01", Title: "Oldest", CreatedAt: time.Now()},
	})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"date_from":"2027-06-09","date_to":"2027-06-12"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sessions))
	}
	if sessions[0]["title"] != "Old" {
		t.Errorf("title = %v, want Old", sessions[0]["title"])
	}
}

func TestSearchSessionsFrictionFilter(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", Title: "High", FrictionScore: 50, CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "p", Date: "2027-06-14", Title: "Low", FrictionScore: 10, CreatedAt: time.Now()},
	})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"min_friction":30}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sessions))
	}
}

func TestSearchSessionsFileFilter(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", Title: "Go", FilesChanged: []string{"main.go", "util.go"}, CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "p", Date: "2027-06-14", Title: "JS", FilesChanged: []string{"index.js"}, CreatedAt: time.Now()},
	})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"files":["*.go"]}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sessions))
	}
	if sessions[0]["title"] != "Go" {
		t.Errorf("title = %v, want Go", sessions[0]["title"])
	}
}

func TestSearchSessionsMaxResults(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("s%d", i)
		entries[id] = index.SessionEntry{
			SessionID: id, Project: "p",
			Date: fmt.Sprintf("2027-06-%02d", i+1), Title: "Work", CreatedAt: time.Now(),
		}
	}
	cfg := writeTestIndex(t, entries)

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"max_results":5}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 5 {
		t.Errorf("expected 5 results, got %d", len(sessions))
	}
}

func TestSearchSessionsEmpty(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"query":"nothing"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 0 {
		t.Errorf("expected 0 results, got %d", len(sessions))
	}
}

func TestSearchSessionsCombinedFilters(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "alpha", Date: "2027-06-15", Title: "OAuth login", FrictionScore: 50, FilesChanged: []string{"auth.go"}, CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "alpha", Date: "2027-06-14", Title: "OAuth config", FrictionScore: 10, FilesChanged: []string{"config.go"}, CreatedAt: time.Now()},
		"s3": {SessionID: "s3", Project: "beta", Date: "2027-06-15", Title: "OAuth fix", FrictionScore: 60, CreatedAt: time.Now()},
	})

	tool := NewSearchSessionsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"query":"oauth","project":"alpha","min_friction":30}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var sessions []map[string]interface{}
	json.Unmarshal([]byte(result), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sessions))
	}
	if sessions[0]["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", sessions[0]["session_id"])
	}
}

// --- get_knowledge tests ---

func TestGetKnowledgeBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproj/knowledge.md": "# MyProj Knowledge\n\nSome facts here.",
	})

	tool := NewGetKnowledgeTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "MyProj Knowledge") {
		t.Errorf("result = %q, want to contain MyProj Knowledge", result)
	}
}

func TestGetKnowledgeMissing(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetKnowledgeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for missing knowledge.md")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

func TestGetKnowledgePathTraversal(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetKnowledgeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../../etc"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("error = %v, want 'invalid project name'", err)
	}
}

func TestGetKnowledgeEmptyProject(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetKnowledgeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":""}`))
	if err == nil {
		t.Fatal("expected error for empty project")
	}
}

// --- get_session_detail tests ---

func TestGetSessionDetailBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproj/sessions/2027-06-15-01.md": "---\ntitle: Add OAuth\n---\nSession content here.",
	})

	tool := NewGetSessionDetailTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj","date":"2027-06-15"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "Add OAuth") {
		t.Errorf("result = %q, want to contain 'Add OAuth'", result)
	}
}

func TestGetSessionDetailIteration(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproj/sessions/2027-06-15-01.md": "first",
		"Projects/myproj/sessions/2027-06-15-02.md": "second",
	})

	tool := NewGetSessionDetailTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj","date":"2027-06-15","iteration":2}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != "second" {
		t.Errorf("result = %q, want 'second'", result)
	}
}

func TestGetSessionDetailMissing(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetSessionDetailTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"p","date":"2027-06-15"}`))
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestGetSessionDetailPathTraversal(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetSessionDetailTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../../etc","date":"2027-06-15"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestGetSessionDetailBadDate(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetSessionDetailTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"p","date":"not-a-date"}`))
	if err == nil {
		t.Fatal("expected error for bad date format")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("error = %v, want 'invalid date format'", err)
	}
}

// --- get_friction_trends tests ---

func TestGetFrictionTrendsBasic(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", FrictionScore: 30, CreatedAt: time.Now()},
		"s2": {SessionID: "s2", Project: "p", Date: "2027-06-14", FrictionScore: 20, CreatedAt: time.Now()},
	})

	tool := NewGetFrictionTrendsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"p"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["project"] != "p" {
		t.Errorf("project = %v, want p", parsed["project"])
	}
	if parsed["total_sessions"].(float64) != 2 {
		t.Errorf("total_sessions = %v, want 2", parsed["total_sessions"])
	}
}

func TestGetFrictionTrendsEmpty(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{})

	tool := NewGetFrictionTrendsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal([]byte(result), &parsed)
	if parsed["total_sessions"].(float64) != 0 {
		t.Errorf("total_sessions = %v, want 0", parsed["total_sessions"])
	}
}

func TestGetFrictionTrendsCustomWeeks(t *testing.T) {
	cfg := writeTestIndex(t, map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-06-15", FrictionScore: 30, CreatedAt: time.Now()},
	})

	tool := NewGetFrictionTrendsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"weeks":4}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal([]byte(result), &parsed)
	if parsed["display_weeks"].(float64) != 4 {
		t.Errorf("display_weeks = %v, want 4", parsed["display_weeks"])
	}
}

// --- validateProjectName tests ---

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"myproject", false},
		{"my-project", false},
		{"", true},
		{"../etc", true},
		{"foo/bar", true},
		{`foo\bar`, true},
		{"a..b", true},
	}
	for _, tt := range tests {
		err := validateProjectName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateProjectName(%q) err=%v, wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}
