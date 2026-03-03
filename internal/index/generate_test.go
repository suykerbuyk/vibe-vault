// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateContext_WritesHistoryMd(t *testing.T) {
	vaultPath := t.TempDir()
	stateDir := filepath.Join(vaultPath, ".vibe-vault")

	idx, _ := Load(stateDir)
	idx.Add(SessionEntry{
		SessionID: "gen-001",
		NotePath:  "Projects/myproject/sessions/2026-03-01-01.md",
		Project:   "myproject",
		Date:      "2026-03-01",
		Iteration: 1,
		Summary:   "Added auto-index",
		CreatedAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
	})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}

	result, err := GenerateContext(idx, vaultPath, nil)
	if err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}

	if result.ProjectsUpdated != 1 {
		t.Errorf("ProjectsUpdated = %d, want 1", result.ProjectsUpdated)
	}

	historyPath := filepath.Join(vaultPath, "Projects", "myproject", "history.md")
	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read history.md: %v", err)
	}

	content := string(data)
	if !contains(content, "project: myproject") {
		t.Error("missing project frontmatter")
	}
	if !contains(content, "[[2026-03-01-01]]") {
		t.Error("missing session wikilink")
	}
}

func TestGenerateContext_WritesKnowledgeMd(t *testing.T) {
	vaultPath := t.TempDir()
	stateDir := filepath.Join(vaultPath, ".vibe-vault")

	idx, _ := Load(stateDir)
	idx.Add(SessionEntry{
		SessionID: "gen-k1", NotePath: "Projects/proj1/sessions/2026-03-01-01.md",
		Project: "proj1", Date: "2026-03-01", Iteration: 1,
	})
	idx.Add(SessionEntry{
		SessionID: "gen-k2", NotePath: "Projects/proj2/sessions/2026-03-01-01.md",
		Project: "proj2", Date: "2026-03-01", Iteration: 1,
	})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}

	// Two knowledge notes from different projects in the same category
	summaries := []KnowledgeSummary{
		{
			Type: "lesson", Title: "Always test edge cases",
			Summary: "Edge case testing prevents regressions",
			Project: "proj1", Category: "testing", Date: "2026-03-01",
			NotePath: "Knowledge/learnings/2026-03-01-edge-cases.md",
		},
		{
			Type: "lesson", Title: "Use table-driven tests",
			Summary: "Table-driven tests improve coverage",
			Project: "proj2", Category: "testing", Date: "2026-03-01",
			NotePath: "Knowledge/learnings/2026-03-01-table-tests.md",
		},
	}

	result, err := GenerateContext(idx, vaultPath, summaries)
	if err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}

	if !result.KnowledgeWritten {
		t.Error("expected KnowledgeWritten = true")
	}

	crossPath := filepath.Join(vaultPath, "Knowledge", "_knowledge.md")
	data, err := os.ReadFile(crossPath)
	if err != nil {
		t.Fatalf("read _knowledge.md: %v", err)
	}

	content := string(data)
	if !contains(content, "## testing") {
		t.Error("missing testing category")
	}
}

func TestGenerateContext_NoSessions(t *testing.T) {
	vaultPath := t.TempDir()
	stateDir := filepath.Join(vaultPath, ".vibe-vault")

	idx, _ := Load(stateDir)

	result, err := GenerateContext(idx, vaultPath, nil)
	if err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}

	if result.ProjectsUpdated != 0 {
		t.Errorf("ProjectsUpdated = %d, want 0", result.ProjectsUpdated)
	}
	if result.KnowledgeWritten {
		t.Error("expected KnowledgeWritten = false for empty index")
	}
}

func TestGenerateContext_MultipleProjects(t *testing.T) {
	vaultPath := t.TempDir()
	stateDir := filepath.Join(vaultPath, ".vibe-vault")

	idx, _ := Load(stateDir)
	idx.Add(SessionEntry{
		SessionID: "gen-m1", NotePath: "Projects/alpha/sessions/2026-03-01-01.md",
		Project: "alpha", Date: "2026-03-01", Iteration: 1, Summary: "Alpha work",
		CreatedAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
	})
	idx.Add(SessionEntry{
		SessionID: "gen-m2", NotePath: "Projects/beta/sessions/2026-03-01-01.md",
		Project: "beta", Date: "2026-03-01", Iteration: 1, Summary: "Beta work",
		CreatedAt: time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC),
	})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}

	result, err := GenerateContext(idx, vaultPath, nil)
	if err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}

	if result.ProjectsUpdated != 2 {
		t.Errorf("ProjectsUpdated = %d, want 2", result.ProjectsUpdated)
	}

	for _, project := range []string{"alpha", "beta"} {
		historyPath := filepath.Join(vaultPath, "Projects", project, "history.md")
		if _, err := os.Stat(historyPath); err != nil {
			t.Errorf("history.md missing for project %s: %v", project, err)
		}
	}
}
