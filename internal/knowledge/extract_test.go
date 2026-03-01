package knowledge

import (
	"testing"

	"github.com/johns/vibe-vault/internal/friction"
	"github.com/johns/vibe-vault/internal/prose"
)

func TestPairCorrections(t *testing.T) {
	dialogue := &prose.Dialogue{
		Sections: []prose.Section{
			{
				UserRequest: "Add auth",
				Elements: []prose.Element{
					{Turn: &prose.Turn{Role: "user", Text: "Add JWT auth"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "I'll implement basic auth with sessions."}},
					{Turn: &prose.Turn{Role: "user", Text: "No, I said JWT not sessions"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "Switching to JWT tokens with RS256."}},
					{Marker: &prose.Marker{Text: "Modified `auth.go`"}},
					{Turn: &prose.Turn{Role: "user", Text: "Looks good, what about refresh tokens?"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "Adding refresh token rotation."}},
				},
			},
		},
	}

	fr := &friction.Result{
		Corrections: []friction.Correction{
			{TurnIndex: 1, Text: "No, I said JWT not sessions", Pattern: "negation"},
		},
	}

	pairs := PairCorrections(fr, dialogue)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}

	p := pairs[0]
	if p.UserText != "No, I said JWT not sessions" {
		t.Errorf("unexpected UserText: %q", p.UserText)
	}
	if p.Pattern != "negation" {
		t.Errorf("unexpected Pattern: %q", p.Pattern)
	}
	if p.Resolution == "" {
		t.Error("expected non-empty Resolution")
	}
	// Resolution should contain the assistant response and marker
	if !contains(p.Resolution, "Switching to JWT") {
		t.Errorf("Resolution should contain assistant response, got: %q", p.Resolution)
	}
}

func TestPairCorrections_NilInputs(t *testing.T) {
	if pairs := PairCorrections(nil, nil); pairs != nil {
		t.Errorf("expected nil for nil inputs, got %v", pairs)
	}

	if pairs := PairCorrections(&friction.Result{}, nil); pairs != nil {
		t.Errorf("expected nil for nil dialogue, got %v", pairs)
	}

	if pairs := PairCorrections(nil, &prose.Dialogue{}); pairs != nil {
		t.Errorf("expected nil for nil friction, got %v", pairs)
	}
}

func TestPairCorrections_MultipleSections(t *testing.T) {
	dialogue := &prose.Dialogue{
		Sections: []prose.Section{
			{
				Elements: []prose.Element{
					{Turn: &prose.Turn{Role: "user", Text: "First request"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "Done."}},
				},
			},
			{
				Elements: []prose.Element{
					{Turn: &prose.Turn{Role: "user", Text: "Actually, do it differently"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "Changed approach to X."}},
				},
			},
		},
	}

	fr := &friction.Result{
		Corrections: []friction.Correction{
			{TurnIndex: 1, Text: "Actually, do it differently", Pattern: "redirect"},
		},
	}

	pairs := PairCorrections(fr, dialogue)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Pattern != "redirect" {
		t.Errorf("unexpected pattern: %q", pairs[0].Pattern)
	}
	if !contains(pairs[0].Resolution, "Changed approach") {
		t.Errorf("resolution should span across sections, got: %q", pairs[0].Resolution)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
