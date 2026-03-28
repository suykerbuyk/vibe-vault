// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package inject

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/mdutil"
	"github.com/johns/vibe-vault/internal/trends"
)

func makeEntry(id, project, date string, iter int) index.SessionEntry {
	return index.SessionEntry{
		SessionID: id,
		Project:   project,
		Date:      date,
		Iteration: iter,
		Title:     "Session " + id,
		Summary:   "Summary for " + id,
	}
}

func TestBuildEmpty(t *testing.T) {
	r := Build(nil, trends.Result{}, Opts{Project: "test"})
	if r.Project != "test" {
		t.Errorf("project = %q, want %q", r.Project, "test")
	}
	if r.Summary != "" {
		t.Errorf("summary = %q, want empty", r.Summary)
	}
	if len(r.Sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(r.Sessions))
	}
}

func TestBuildSummary(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2027-01-01", Iteration: 1, Summary: "old summary"},
		"s2": {SessionID: "s2", Project: "p", Date: "2027-01-02", Iteration: 1, Summary: "latest summary"},
	}
	r := Build(entries, trends.Result{}, Opts{Project: "p"})
	if r.Summary != "latest summary" {
		t.Errorf("summary = %q, want %q", r.Summary, "latest summary")
	}
}

func TestBuildSessions(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	for i := 1; i <= 7; i++ {
		id := "s" + string(rune('0'+i))
		date := time.Date(2027, 1, i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		entries[id] = makeEntry(id, "p", date, 1)
	}

	r := Build(entries, trends.Result{}, Opts{Project: "p"})
	if len(r.Sessions) != 5 {
		t.Errorf("sessions = %d, want 5", len(r.Sessions))
	}
	// Newest first
	if r.Sessions[0].Date != "2027-01-07" {
		t.Errorf("first session date = %q, want 2027-01-07", r.Sessions[0].Date)
	}
}

func TestBuildSessionsFewEntries(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "p", "2027-01-01", 1),
		"s2": makeEntry("s2", "p", "2027-01-02", 1),
	}
	r := Build(entries, trends.Result{}, Opts{Project: "p"})
	if len(r.Sessions) != 2 {
		t.Errorf("sessions = %d, want 2", len(r.Sessions))
	}
}

func TestBuildOpenThreads(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "p", Date: "2027-01-01", Iteration: 1,
			OpenThreads: []string{"unresolved thread", "authentication tokens need rotation"},
			Decisions:   []string{"implemented authentication token rotation using JWT"},
		},
	}
	r := Build(entries, trends.Result{}, Opts{Project: "p"})
	if len(r.Threads) != 1 {
		t.Errorf("threads = %d, want 1", len(r.Threads))
	}
	if len(r.Threads) > 0 && r.Threads[0] != "unresolved thread" {
		t.Errorf("thread = %q, want %q", r.Threads[0], "unresolved thread")
	}
}

func TestBuildDecisions(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	old := time.Now().AddDate(0, 0, -60).Format("2006-01-02")

	entries := map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "p", Date: today, Iteration: 1,
			Decisions: []string{"recent decision", "another recent"},
		},
		"s2": {
			SessionID: "s2", Project: "p", Date: old, Iteration: 1,
			Decisions: []string{"old decision"},
		},
		"s3": {
			SessionID: "s3", Project: "p", Date: today, Iteration: 2,
			Decisions: []string{"recent decision"}, // duplicate
		},
	}
	r := Build(entries, trends.Result{}, Opts{Project: "p"})
	if len(r.Decisions) != 2 {
		t.Errorf("decisions = %d, want 2 (old excluded, dup excluded)", len(r.Decisions))
	}
}

func TestBuildFriction(t *testing.T) {
	tr := trends.Result{
		Metrics: []trends.MetricTrend{
			{Name: "friction", Direction: "improving", OverallAvg: 22.5},
		},
	}
	r := Build(nil, tr, Opts{Project: "p"})
	if r.Friction == nil {
		t.Fatal("friction is nil")
	}
	if r.Friction.Direction != "improving" {
		t.Errorf("direction = %q, want %q", r.Friction.Direction, "improving")
	}
	if r.Friction.Average != 22.5 {
		t.Errorf("average = %v, want 22.5", r.Friction.Average)
	}
}

func TestFormatMarkdown(t *testing.T) {
	r := Result{
		Project: "myproj",
		Summary: "Did some work",
		Sessions: []SessionItem{
			{Date: "2027-01-01", Title: "Session 1", Tag: "feature"},
		},
		Threads:   []string{"open thread 1"},
		Decisions: []string{"decided X"},
		Friction:  &FrictionSummary{Direction: "stable", Average: 15.0},
	}

	out := FormatMarkdown(r, nil)

	for _, want := range []string{
		"# Context: myproj",
		"## Summary",
		"Did some work",
		"## Recent Sessions",
		"**2027-01-01** Session 1 #feature",
		"## Open Threads",
		"open thread 1",
		"## Decisions",
		"decided X",
		"## Friction",
		"stable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestFormatJSON(t *testing.T) {
	r := Result{
		Project: "myproj",
		Summary: "test summary",
		Sessions: []SessionItem{
			{Date: "2027-01-01", Title: "S1"},
		},
	}

	out, err := FormatJSON(r, nil)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["project"] != "myproj" {
		t.Errorf("project = %v, want myproj", parsed["project"])
	}
	if parsed["summary"] != "test summary" {
		t.Errorf("summary = %v, want 'test summary'", parsed["summary"])
	}
}

func TestRenderTokenBudget(t *testing.T) {
	r := Result{
		Project: "p",
		Summary: "short",
		Sessions: []SessionItem{
			{Date: "2027-01-01", Title: "S1", Summary: strings.Repeat("word ", 100)},
		},
		Threads:   []string{strings.Repeat("thread ", 100)},
		Decisions: []string{strings.Repeat("decision ", 100)},
		Friction:  &FrictionSummary{Direction: "stable", Average: 10},
	}

	// Very low budget should drop sections
	out, err := Render(r, Opts{Project: "p", Format: "md", MaxTokens: 50})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// Should still have project header
	if !strings.Contains(out, "# Context: p") {
		t.Error("missing context header")
	}

	// Full render should have more sections
	full, _ := Render(r, Opts{Project: "p", Format: "md", MaxTokens: 10000})
	if len(out) >= len(full) {
		t.Errorf("truncated output (%d) should be shorter than full (%d)", len(out), len(full))
	}
}

func TestRenderSectionsFilter(t *testing.T) {
	r := Result{
		Project:   "p",
		Summary:   "my summary",
		Sessions:  []SessionItem{{Date: "2027-01-01", Title: "S1"}},
		Threads:   []string{"thread1"},
		Decisions: []string{"decision1"},
	}

	out, err := Render(r, Opts{
		Project:   "p",
		Format:    "md",
		Sections:  []string{"summary", "sessions"},
		MaxTokens: 5000,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(out, "## Summary") {
		t.Error("missing summary section")
	}
	if !strings.Contains(out, "## Recent Sessions") {
		t.Error("missing sessions section")
	}
	if strings.Contains(out, "## Open Threads") {
		t.Error("threads section should not appear")
	}
	if strings.Contains(out, "## Decisions") {
		t.Error("decisions section should not appear")
	}
}

func TestEstimateTokens(t *testing.T) {
	text := "one two three four five"
	tokens := estimateTokens(text)
	// 5 words × 1.3 = 6.5 → 6
	if tokens < 6 || tokens > 7 {
		t.Errorf("estimateTokens(%q) = %d, want ~6", text, tokens)
	}

	if estimateTokens("") != 0 {
		t.Errorf("estimateTokens('') = %d, want 0", estimateTokens(""))
	}
}

func TestOpenThreadsResolution(t *testing.T) {
	entries := []index.SessionEntry{
		{
			SessionID: "s1", Project: "p", Date: "2027-01-01", Iteration: 1,
			OpenThreads: []string{
				"implement caching layer for API",
				"investigate memory leak in worker",
			},
			Decisions: []string{
				"implemented caching layer using Redis",
			},
		},
	}

	threads := openThreads(entries, 5)
	// "caching layer" overlaps between thread and decision (2 significant words)
	// "memory leak worker" should remain
	if len(threads) != 1 {
		t.Errorf("threads = %d, want 1", len(threads))
	}
	if len(threads) > 0 && !strings.Contains(threads[0], "memory leak") {
		t.Errorf("thread = %q, want the memory leak thread", threads[0])
	}
}

func TestSignificantWords(t *testing.T) {
	words := mdutil.SignificantWords("This should filter short and stop words from text!")

	// "this" → stop word, "should" → stop word, "filter" → kept (6 chars),
	// "short" → kept, "stop" → kept, "words" → kept, "from" → stop word,
	// "text" → kept
	for _, want := range []string{"filter", "short", "stop", "words", "text"} {
		found := false
		for _, w := range words {
			if w == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing significant word %q, got %v", want, words)
		}
	}

	// Stop words should be excluded
	for _, notwant := range []string{"this", "should", "from"} {
		for _, w := range words {
			if w == notwant {
				t.Errorf("stop word %q should not appear in %v", notwant, words)
			}
		}
	}

	// Short words (< 4 chars) should be excluded
	for _, w := range words {
		if len(w) < 4 {
			t.Errorf("short word %q should not appear", w)
		}
	}
}

