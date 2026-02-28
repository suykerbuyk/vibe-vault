package friction

import (
	"testing"

	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/prose"
	"github.com/johns/vibe-vault/internal/transcript"
)

func TestAnalyze_NilInputs(t *testing.T) {
	result := Analyze(nil, nil, transcript.Stats{}, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Score != 0 {
		t.Errorf("score = %d, want 0 for nil inputs", result.Score)
	}
}

func TestAnalyze_CorrectionsOnly(t *testing.T) {
	dialogue := &prose.Dialogue{
		Sections: []prose.Section{
			{
				UserRequest: "test",
				Elements: []prose.Element{
					{Turn: &prose.Turn{Role: "user", Text: "no, that's wrong"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "Let me fix it."}},
					{Turn: &prose.Turn{Role: "user", Text: "actually, use the other one"}},
				},
			},
		},
	}
	stats := transcript.Stats{
		UserMessages:      5,
		InputTokens:       1000,
		OutputTokens:      500,
		FilesWritten:      map[string]bool{"a.go": true},
	}

	result := Analyze(dialogue, nil, stats, nil)
	if result.Signals.Corrections != 2 {
		t.Errorf("corrections = %d, want 2", result.Signals.Corrections)
	}
	if result.Score == 0 {
		t.Error("expected non-zero score with corrections")
	}
}

func TestAnalyze_NarrativeSignals(t *testing.T) {
	narr := &narrative.Narrative{
		Segments: []narrative.Segment{
			{
				Activities: []narrative.Activity{
					{Kind: narrative.KindFileModify, Description: "Modified `a.go`"},
					{Kind: narrative.KindFileModify, Description: "Modified `a.go`"},
					{Kind: narrative.KindFileModify, Description: "Modified `a.go`"},
					{Kind: narrative.KindTestRun, IsError: true},
					{Kind: narrative.KindError, IsError: true},
				},
			},
		},
	}
	stats := transcript.Stats{
		UserMessages: 3,
		InputTokens:  5000,
		OutputTokens: 2000,
		FilesWritten: map[string]bool{"a.go": true},
	}

	result := Analyze(nil, narr, stats, nil)
	if result.Signals.FileRetryDensity == 0 {
		t.Error("expected non-zero file retry density")
	}
	if result.Signals.ErrorCycleDensity == 0 {
		t.Error("expected non-zero error cycle density")
	}
}

func TestAnalyze_RecurringThreads(t *testing.T) {
	narr := &narrative.Narrative{
		OpenThreads: []string{"fix authentication system error"},
	}
	priorThreads := []string{"fix authentication system problem"}

	result := Analyze(nil, narr, transcript.Stats{}, priorThreads)
	if !result.Signals.RecurringThreads {
		t.Error("expected recurring threads to be detected")
	}
}

func TestAnalyze_NoRecurringThreads(t *testing.T) {
	narr := &narrative.Narrative{
		OpenThreads: []string{"add unit tests"},
	}
	priorThreads := []string{"fix authentication system"}

	result := Analyze(nil, narr, transcript.Stats{}, priorThreads)
	if result.Signals.RecurringThreads {
		t.Error("expected no recurring threads")
	}
}

func TestAnalyze_Combined(t *testing.T) {
	dialogue := &prose.Dialogue{
		Sections: []prose.Section{
			{
				UserRequest: "test",
				Elements: []prose.Element{
					{Turn: &prose.Turn{Role: "user", Text: "no, that's not right"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "fixing..."}},
					{Turn: &prose.Turn{Role: "user", Text: "still broken, revert it"}},
				},
			},
		},
	}
	narr := &narrative.Narrative{
		Segments: []narrative.Segment{
			{
				Activities: []narrative.Activity{
					{Kind: narrative.KindFileModify, Description: "Modified `a.go`"},
					{Kind: narrative.KindTestRun, IsError: true},
				},
			},
		},
		OpenThreads: []string{"fix broken build"},
	}
	stats := transcript.Stats{
		UserMessages: 5,
		InputTokens:  10000,
		OutputTokens: 5000,
		FilesWritten: map[string]bool{"a.go": true},
	}
	priorThreads := []string{"fix broken build system"}

	result := Analyze(dialogue, narr, stats, priorThreads)
	if result.Score == 0 {
		t.Error("expected non-zero combined score")
	}
	if len(result.Summary) == 0 {
		t.Error("expected non-empty summary")
	}
	if result.Signals.Corrections < 2 {
		t.Errorf("corrections = %d, want >= 2", result.Signals.Corrections)
	}
}

func TestAnalyze_BuildSummary(t *testing.T) {
	signals := Signals{
		Corrections:       3,
		CorrectionDensity: 0.30,
		TokensPerFile:     30000,
		FileRetryDensity:  0.40,
		ErrorCycleDensity: 0.15,
		RecurringThreads:  true,
	}
	summary := buildSummary(signals, 3, 10)
	if len(summary) < 3 {
		t.Errorf("expected >= 3 summary lines, got %d", len(summary))
	}
}

func TestHasRecurringThreads(t *testing.T) {
	prior := []string{"implement authentication system"}
	current := []string{"authentication system still broken"}
	if !hasRecurringThreads(prior, current) {
		t.Error("expected recurring thread match")
	}
}

func TestHasRecurringThreads_NoMatch(t *testing.T) {
	prior := []string{"implement authentication system"}
	current := []string{"add unit tests for handler"}
	if hasRecurringThreads(prior, current) {
		t.Error("expected no recurring thread match")
	}
}

func TestSignificantWords(t *testing.T) {
	words := significantWords("the authentication system for users")
	// "the" < 4 chars, "for" < 4 chars
	// Should get: "authentication", "system", "users"
	if len(words) != 3 {
		t.Errorf("expected 3 words, got %d: %v", len(words), words)
	}
}
