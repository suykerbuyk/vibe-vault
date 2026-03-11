// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package effectiveness

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/index"
)

func TestAnalyze_EmptyIndex(t *testing.T) {
	r := Analyze(map[string]index.SessionEntry{}, "")
	if len(r.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(r.Projects))
	}
}

func TestAnalyze_NoContextData(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2026-03-01", Iteration: 1},
		"s2": {SessionID: "s2", Project: "p", Date: "2026-03-02", Iteration: 1},
	}
	r := Analyze(entries, "")
	if len(r.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(r.Projects))
	}
	if r.Projects[0].Confidence != "insufficient" {
		t.Errorf("confidence = %q, want insufficient", r.Projects[0].Confidence)
	}
	if r.Projects[0].WithContext != 0 {
		t.Errorf("with_context = %d, want 0", r.Projects[0].WithContext)
	}
}

func TestAnalyze_CohortAssignment(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2026-03-01", Iteration: 1, FrictionScore: 40,
			Context: &index.ContextAvailable{HistorySessions: 0}},
		"s2": {SessionID: "s2", Project: "p", Date: "2026-03-02", Iteration: 1, FrictionScore: 30,
			Context: &index.ContextAvailable{HistorySessions: 5}},
		"s3": {SessionID: "s3", Project: "p", Date: "2026-03-03", Iteration: 1, FrictionScore: 20,
			Context: &index.ContextAvailable{HistorySessions: 15}},
		"s4": {SessionID: "s4", Project: "p", Date: "2026-03-04", Iteration: 1, FrictionScore: 10,
			Context: &index.ContextAvailable{HistorySessions: 35}},
	}
	r := Analyze(entries, "")
	if len(r.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(r.Projects))
	}

	cohortMap := make(map[string]Cohort)
	for _, c := range r.Projects[0].Cohorts {
		cohortMap[c.Label] = c
	}

	if c, ok := cohortMap["none (0)"]; !ok || c.Sessions != 1 {
		t.Errorf("none cohort: got %+v", cohortMap["none (0)"])
	}
	if c, ok := cohortMap["early (1-10)"]; !ok || c.Sessions != 1 {
		t.Errorf("early cohort: got %+v", cohortMap["early (1-10)"])
	}
	if c, ok := cohortMap["building (11-30)"]; !ok || c.Sessions != 1 {
		t.Errorf("building cohort: got %+v", cohortMap["building (11-30)"])
	}
	if c, ok := cohortMap["mature (30+)"]; !ok || c.Sessions != 1 {
		t.Errorf("mature cohort: got %+v", cohortMap["mature (30+)"])
	}
}

func TestAnalyze_NegativeCorrelation(t *testing.T) {
	// Friction decreasing as context increases
	entries := make(map[string]index.SessionEntry)
	for i := 0; i < 30; i++ {
		id := strings.Repeat("a", 1) + string(rune('a'+i%26)) + string(rune('0'+i/26))
		entries[id] = index.SessionEntry{
			SessionID: id, Project: "p", Date: "2026-03-01", Iteration: i + 1,
			FrictionScore: 50 - i, // decreasing friction
			Context:       &index.ContextAvailable{HistorySessions: i * 2},
		}
	}
	r := Analyze(entries, "")
	if r.Projects[0].Correlation >= 0 {
		t.Errorf("expected negative correlation, got %.3f", r.Projects[0].Correlation)
	}
}

func TestAnalyze_ProjectFilter(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "alpha", Date: "2026-03-01", Iteration: 1,
			Context: &index.ContextAvailable{HistorySessions: 0}},
		"s2": {SessionID: "s2", Project: "beta", Date: "2026-03-01", Iteration: 1,
			Context: &index.ContextAvailable{HistorySessions: 0}},
	}
	r := Analyze(entries, "alpha")
	if len(r.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(r.Projects))
	}
	if r.Projects[0].Project != "alpha" {
		t.Errorf("project = %q, want alpha", r.Projects[0].Project)
	}
}

func TestPearsonR(t *testing.T) {
	tests := []struct {
		name string
		xs   []float64
		ys   []float64
		want float64
	}{
		{"perfect positive", []float64{1, 2, 3, 4, 5}, []float64{2, 4, 6, 8, 10}, 1.0},
		{"perfect negative", []float64{1, 2, 3, 4, 5}, []float64{10, 8, 6, 4, 2}, -1.0},
		{"zero", []float64{1, 2, 3, 4, 5}, []float64{3, 3, 3, 3, 3}, 0.0},
		{"too few points", []float64{1, 2}, []float64{1, 2}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pearsonR(tt.xs, tt.ys)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("pearsonR = %.3f, want %.3f", got, tt.want)
			}
		})
	}
}

func TestBackfillContext_PopulatesEmpty(t *testing.T) {
	dir := t.TempDir()
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2026-03-01", Iteration: 1},
		"s2": {SessionID: "s2", Project: "p", Date: "2026-03-01", Iteration: 2},
		"s3": {SessionID: "s3", Project: "p", Date: "2026-03-02", Iteration: 1},
	}}
	_ = dir // just for temp directory convention

	result := idx.BackfillContext(false)
	if result.Updated != 3 {
		t.Errorf("updated = %d, want 3", result.Updated)
	}
	if result.Skipped != 0 {
		t.Errorf("skipped = %d, want 0", result.Skipped)
	}

	// Verify chronological ordering: s1=0, s2=1, s3=2
	if idx.Entries["s1"].Context.HistorySessions != 0 {
		t.Errorf("s1 HistorySessions = %d, want 0", idx.Entries["s1"].Context.HistorySessions)
	}
	if idx.Entries["s2"].Context.HistorySessions != 1 {
		t.Errorf("s2 HistorySessions = %d, want 1", idx.Entries["s2"].Context.HistorySessions)
	}
	if idx.Entries["s3"].Context.HistorySessions != 2 {
		t.Errorf("s3 HistorySessions = %d, want 2", idx.Entries["s3"].Context.HistorySessions)
	}
}

func TestBackfillContext_SkipsExisting(t *testing.T) {
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2026-03-01", Iteration: 1,
			Context: &index.ContextAvailable{HistorySessions: 99}},
		"s2": {SessionID: "s2", Project: "p", Date: "2026-03-02", Iteration: 1},
	}}

	// Without overwrite: skip existing
	result := idx.BackfillContext(false)
	if result.Updated != 1 {
		t.Errorf("updated = %d, want 1", result.Updated)
	}
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	// s1 should keep its original value
	if idx.Entries["s1"].Context.HistorySessions != 99 {
		t.Errorf("s1 HistorySessions = %d, want 99 (preserved)", idx.Entries["s1"].Context.HistorySessions)
	}

	// With overwrite: update all
	result = idx.BackfillContext(true)
	if result.Updated != 2 {
		t.Errorf("overwrite updated = %d, want 2", result.Updated)
	}
	if idx.Entries["s1"].Context.HistorySessions != 0 {
		t.Errorf("s1 HistorySessions after overwrite = %d, want 0", idx.Entries["s1"].Context.HistorySessions)
	}
}

func TestBackfillContext_MultiProject(t *testing.T) {
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"a1": {SessionID: "a1", Project: "alpha", Date: "2026-03-01", Iteration: 1},
		"a2": {SessionID: "a2", Project: "alpha", Date: "2026-03-02", Iteration: 1},
		"b1": {SessionID: "b1", Project: "beta", Date: "2026-03-01", Iteration: 1},
		"b2": {SessionID: "b2", Project: "beta", Date: "2026-03-02", Iteration: 1},
	}}

	idx.BackfillContext(false)

	// Each project should count independently
	if idx.Entries["a1"].Context.HistorySessions != 0 {
		t.Errorf("a1 = %d, want 0", idx.Entries["a1"].Context.HistorySessions)
	}
	if idx.Entries["a2"].Context.HistorySessions != 1 {
		t.Errorf("a2 = %d, want 1", idx.Entries["a2"].Context.HistorySessions)
	}
	if idx.Entries["b1"].Context.HistorySessions != 0 {
		t.Errorf("b1 = %d, want 0", idx.Entries["b1"].Context.HistorySessions)
	}
	if idx.Entries["b2"].Context.HistorySessions != 1 {
		t.Errorf("b2 = %d, want 1", idx.Entries["b2"].Context.HistorySessions)
	}
}

func TestBackfillContext_SortOrder(t *testing.T) {
	// Same date, different iterations
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"s3": {SessionID: "s3", Project: "p", Date: "2026-03-01", Iteration: 3},
		"s1": {SessionID: "s1", Project: "p", Date: "2026-03-01", Iteration: 1},
		"s2": {SessionID: "s2", Project: "p", Date: "2026-03-01", Iteration: 2},
	}}

	idx.BackfillContext(false)

	if idx.Entries["s1"].Context.HistorySessions != 0 {
		t.Errorf("s1 = %d, want 0", idx.Entries["s1"].Context.HistorySessions)
	}
	if idx.Entries["s2"].Context.HistorySessions != 1 {
		t.Errorf("s2 = %d, want 1", idx.Entries["s2"].Context.HistorySessions)
	}
	if idx.Entries["s3"].Context.HistorySessions != 2 {
		t.Errorf("s3 = %d, want 2", idx.Entries["s3"].Context.HistorySessions)
	}
}

func TestBackfillContext_HasHistoryFalse(t *testing.T) {
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"s1": {SessionID: "s1", Project: "p", Date: "2026-03-01", Iteration: 1},
	}}

	idx.BackfillContext(false)

	ctx := idx.Entries["s1"].Context
	if ctx == nil {
		t.Fatal("context should not be nil")
	}
	if ctx.HasHistory {
		t.Error("HasHistory should be false")
	}
	if ctx.HasKnowledge {
		t.Error("HasKnowledge should be false")
	}
}

func TestFormat(t *testing.T) {
	r := Result{
		Projects: []ProjectReport{
			{
				Project:       "myproject",
				TotalSessions: 10,
				WithContext:   8,
				Cohorts: []Cohort{
					{Label: "none (0)", Sessions: 2, AvgFriction: 35.0},
					{Label: "early (1-10)", Sessions: 6, AvgFriction: 25.0},
				},
				Correlation: -0.45,
				Confidence:  "low",
				Summary:     "Context shows negative correlation (r=-0.45) with friction.",
			},
		},
	}

	output := Format(r)
	if !strings.Contains(output, "myproject") {
		t.Error("output should contain project name")
	}
	if !strings.Contains(output, "none (0)") {
		t.Error("output should contain cohort label")
	}
	if !strings.Contains(output, "-0.450") {
		t.Error("output should contain correlation value")
	}

	// Verify JSON roundtrip
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed Result
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Projects) != 1 {
		t.Errorf("roundtrip projects = %d, want 1", len(parsed.Projects))
	}
}
