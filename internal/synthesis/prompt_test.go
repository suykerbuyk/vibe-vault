package synthesis

import (
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/noteparse"
)

func TestBuildPrompt_AllSections(t *testing.T) {
	input := &Input{
		SessionNote: &noteparse.Note{
			Date:        "2026-03-27",
			Summary:     "Implemented synthesis agent",
			Tag:         "implementation",
			Decisions:   []string{"Use single LLM call"},
			OpenThreads: []string{"Need integration tests"},
			FilesChanged: []string{"internal/synthesis/prompt.go"},
		},
		GitDiff:     "+func buildUserPrompt() {}\n",
		KnowledgeMD: "# Knowledge\n\n## Decisions\n\n- Use Go for all tools\n",
		ResumeMD:    "# Resume\n\n## Current State\n\nWorking on synthesis\n",
		RecentHistory: []HistoryEntry{
			{Date: "2026-03-26", Tag: "implementation", Summary: "Built mdutil"},
		},
		TaskSummaries: []TaskSummary{
			{Name: "synthesis-agent", Title: "Session Synthesis Agent", Status: "In Progress"},
		},
	}

	prompt := buildUserPrompt(input)

	for _, want := range []string{
		"## Session Summary",
		"2026-03-27",
		"Implemented synthesis agent",
		"## Key Decisions",
		"Use single LLM call",
		"## Open Threads",
		"Need integration tests",
		"## Files Changed",
		"internal/synthesis/prompt.go",
		"## Git Diff",
		"+func buildUserPrompt",
		"## Current Knowledge",
		"## Current Resume",
		"## Recent History",
		"Built mdutil",
		"## Active Tasks",
		"synthesis-agent",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing %q in prompt", want)
		}
	}
}

func TestBuildPrompt_EmptyKnowledge(t *testing.T) {
	input := &Input{
		SessionNote: &noteparse.Note{Summary: "test"},
		KnowledgeMD: "",
	}
	prompt := buildUserPrompt(input)
	if !strings.Contains(prompt, "will be created if learnings provided") {
		t.Error("missing empty knowledge hint")
	}
}

func TestBuildPrompt_EmptyResume(t *testing.T) {
	input := &Input{
		SessionNote: &noteparse.Note{Summary: "test"},
		ResumeMD:    "",
	}
	prompt := buildUserPrompt(input)
	if !strings.Contains(prompt, "(not present)") {
		t.Error("missing resume absent indicator")
	}
}

func TestBuildPrompt_GitDiffTruncation(t *testing.T) {
	// buildUserPrompt doesn't truncate — gather.go does. Just verify it renders.
	diff := strings.Repeat("+line\n", 2000)
	input := &Input{
		SessionNote: &noteparse.Note{Summary: "test"},
		GitDiff:     diff,
	}
	prompt := buildUserPrompt(input)
	if !strings.Contains(prompt, "```diff") {
		t.Error("missing diff fence")
	}
}

func TestBuildPrompt_NoTasks(t *testing.T) {
	input := &Input{
		SessionNote:   &noteparse.Note{Summary: "test"},
		TaskSummaries: nil,
	}
	prompt := buildUserPrompt(input)
	if strings.Contains(prompt, "## Active Tasks") {
		t.Error("should omit tasks section when empty")
	}
}

func TestBuildPrompt_NoHistory(t *testing.T) {
	input := &Input{
		SessionNote:   &noteparse.Note{Summary: "test"},
		RecentHistory: nil,
	}
	prompt := buildUserPrompt(input)
	if strings.Contains(prompt, "## Recent History") {
		t.Error("should omit history section when empty")
	}
}

func TestBuildPrompt_NumberedBullets(t *testing.T) {
	input := &Input{
		SessionNote: &noteparse.Note{Summary: "test"},
		KnowledgeMD: "# Knowledge\n\n## Decisions\n\n- First decision\n- Second decision\n",
	}
	prompt := buildUserPrompt(input)
	if !strings.Contains(prompt, "[0] - First decision") {
		t.Error("missing [0] prefix on first bullet")
	}
	if !strings.Contains(prompt, "[1] - Second decision") {
		t.Error("missing [1] prefix on second bullet")
	}
}

func TestBuildPrompt_NoCommitsNoDiff(t *testing.T) {
	input := &Input{
		SessionNote: &noteparse.Note{Summary: "test"},
		GitDiff:     "",
	}
	prompt := buildUserPrompt(input)
	if !strings.Contains(prompt, "(no commits)") {
		t.Error("missing no-commits indicator")
	}
}

func TestNumberBullets_ResetsBetweenSections(t *testing.T) {
	md := "## A\n\n- one\n- two\n\n## B\n\n- alpha\n"
	result := numberBullets(md)
	if !strings.Contains(result, "[0] - one") {
		t.Error("section A bullet 0 wrong")
	}
	if !strings.Contains(result, "[1] - two") {
		t.Error("section A bullet 1 wrong")
	}
	if !strings.Contains(result, "[0] - alpha") {
		t.Error("section B bullet 0 should reset to [0]")
	}
}
