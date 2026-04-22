package stats

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

func testEntries() map[string]index.SessionEntry {
	return map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "alpha", Date: "2026-03-01",
			Title: "Build API", Tag: "implementation", Model: "claude-opus-4-6",
			Branch: "feat/api", Duration: 30, TokensIn: 5000, TokensOut: 2000,
			Messages: 10, ToolUses: 15, FrictionScore: 25, Corrections: 2,
			EstimatedCostUSD: 0.12,
		},
		"s2": {
			SessionID: "s2", Project: "beta", Date: "2026-03-02",
			Title: "Fix tests", Tag: "debugging", Model: "claude-opus-4-6",
			Branch: "main", Duration: 15, TokensIn: 3000, TokensOut: 1000,
			Messages: 6, ToolUses: 8, FrictionScore: 40, Corrections: 3,
			EstimatedCostUSD: 0.08,
		},
		"s3": {
			SessionID: "s3", Project: "alpha", Date: "2026-03-02",
			Title: "Add auth", Tag: "implementation", Model: "claude-opus-4-6",
			Branch: "feat/auth", Duration: 45, TokensIn: 8000, TokensOut: 4000,
			Messages: 20, ToolUses: 25, FrictionScore: 10, Corrections: 0,
		},
	}
}

func TestExportEntries_All(t *testing.T) {
	entries := ExportEntries(testEntries(), "")
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	// Sorted by date, then session_id
	if entries[0].Date != "2026-03-01" {
		t.Errorf("first entry date = %q, want 2026-03-01", entries[0].Date)
	}
	if entries[2].Date != "2026-03-02" {
		t.Errorf("last entry date = %q, want 2026-03-02", entries[2].Date)
	}
}

func TestExportEntries_Filtered(t *testing.T) {
	entries := ExportEntries(testEntries(), "alpha")
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Project != "alpha" {
			t.Errorf("entry project = %q, want alpha", e.Project)
		}
	}
}

func TestExportEntries_Empty(t *testing.T) {
	entries := ExportEntries(map[string]index.SessionEntry{}, "")
	if len(entries) != 0 {
		t.Fatalf("len = %d, want 0", len(entries))
	}
}

func TestExportJSON_Data(t *testing.T) {
	entries := ExportEntries(testEntries(), "alpha")
	out, err := ExportJSON(entries)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	var parsed []ExportEntry
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("parsed len = %d, want 2", len(parsed))
	}
	if parsed[0].Title != "Build API" {
		t.Errorf("first title = %q, want Build API", parsed[0].Title)
	}
}

func TestExportJSON_Empty(t *testing.T) {
	out, err := ExportJSON([]ExportEntry{})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	if !strings.Contains(out, "[]") {
		t.Errorf("empty JSON should be [], got: %s", out)
	}
}

func TestExportCSV_Header(t *testing.T) {
	out, err := ExportCSV([]ExportEntry{})
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line (header only), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "date") || !strings.Contains(lines[0], "friction_score") {
		t.Errorf("header missing expected columns: %s", lines[0])
	}
}

func TestExportCSV_Data(t *testing.T) {
	entries := ExportEntries(testEntries(), "")
	out, err := ExportCSV(entries)
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// header + 3 data rows
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	// First data row should be s1 (earliest date)
	if !strings.Contains(lines[1], "2026-03-01") {
		t.Errorf("first data row missing date: %s", lines[1])
	}
	if !strings.Contains(lines[1], "Build API") {
		t.Errorf("first data row missing title: %s", lines[1])
	}
}

func TestExportCSV_Filtered(t *testing.T) {
	entries := ExportEntries(testEntries(), "beta")
	out, err := ExportCSV(entries)
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// header + 1 data row
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "beta") {
		t.Errorf("data row missing project: %s", lines[1])
	}
}
