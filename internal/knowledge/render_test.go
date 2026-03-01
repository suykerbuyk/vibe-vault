package knowledge

import (
	"strings"
	"testing"
)

func TestRenderNote_Lesson(t *testing.T) {
	note := Note{
		Type:          "lesson",
		Title:         "Don't use json:\"-\" for fields present in source data",
		Summary:       "Don't use json:\"-\" to skip deserialization of fields that exist in source",
		Body:          "When a field exists in the source JSON, using `json:\"-\"` silently drops it.\nInstead, use a separate struct or manual parsing.",
		Project:       "vibe-vault",
		Date:          "2026-02-28",
		SourceSession: "2026-02-28-01",
		Confidence:    0.85,
		Category:      "serialization",
	}

	md := RenderNote(note)

	// Check frontmatter fields
	checks := []string{
		"date: 2026-02-28",
		"type: lesson",
		"project: vibe-vault",
		"domain: personal",
		"status: active",
		"confidence: 0.85",
		`category: "serialization"`,
		`- "[[2026-02-28-01]]"`,
		"- knowledge",
		"- lesson",
		"- serialization",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Errorf("missing in output: %q", c)
		}
	}

	// Check body structure
	if !strings.Contains(md, "# Don't use json:\"-\"") {
		t.Error("missing title")
	}
	if !strings.Contains(md, "## What Was Learned") {
		t.Error("lesson should have 'What Was Learned' section")
	}
}

func TestRenderNote_Decision(t *testing.T) {
	note := Note{
		Type:          "decision",
		Title:         "Use separate LLM call for knowledge extraction",
		Summary:       "Chose separate LLM call over extending enrichment prompt",
		Body:          "Enrichment is skipped when prose dialogue exists, but high-friction sessions are the most interesting for lessons.",
		Project:       "vibe-vault",
		Date:          "2026-02-28",
		SourceSession: "2026-02-28-01",
		Confidence:    0.9,
		Category:      "architecture",
	}

	md := RenderNote(note)

	if !strings.Contains(md, "type: decision") {
		t.Error("missing type: decision")
	}
	if !strings.Contains(md, "## Context") {
		t.Error("decision should have 'Context' section")
	}
	if strings.Contains(md, "## What Was Learned") {
		t.Error("decision should NOT have 'What Was Learned' section")
	}
}

func TestRenderNote_EscapesYAML(t *testing.T) {
	note := Note{
		Type:       "lesson",
		Title:      "Test",
		Summary:    `She said "hello" and \n`,
		Body:       "Body text.",
		Project:    "test",
		Date:       "2026-01-01",
		Confidence: 0.8,
	}

	md := RenderNote(note)

	if !strings.Contains(md, `\\n`) {
		t.Error("backslash should be escaped in YAML")
	}
	if !strings.Contains(md, `\"hello\"`) {
		t.Error("quotes should be escaped in YAML")
	}
}
