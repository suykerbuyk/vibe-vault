package friction

import (
	"testing"

	"github.com/johns/vibe-vault/internal/prose"
)

func makeDialogue(turns ...prose.Element) *prose.Dialogue {
	return &prose.Dialogue{
		Sections: []prose.Section{
			{UserRequest: "test", Elements: turns},
		},
	}
}

func userTurn(text string) prose.Element {
	return prose.Element{Turn: &prose.Turn{Role: "user", Text: text}}
}

func assistantTurn(text string) prose.Element {
	return prose.Element{Turn: &prose.Turn{Role: "assistant", Text: text}}
}

func TestDetectCorrections_Nil(t *testing.T) {
	corrections := DetectCorrections(nil)
	if len(corrections) != 0 {
		t.Errorf("expected 0 corrections for nil dialogue, got %d", len(corrections))
	}
}

func TestDetectCorrections_Negation(t *testing.T) {
	d := makeDialogue(
		userTurn("no, that's not what I wanted"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "negation" {
		t.Errorf("pattern = %q, want negation", corrections[0].Pattern)
	}
}

func TestDetectCorrections_Redirect(t *testing.T) {
	d := makeDialogue(
		userTurn("actually, use a different approach"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "redirect" {
		t.Errorf("pattern = %q, want redirect", corrections[0].Pattern)
	}
}

func TestDetectCorrections_Undo(t *testing.T) {
	d := makeDialogue(
		userTurn("please revert those changes"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "undo" {
		t.Errorf("pattern = %q, want undo", corrections[0].Pattern)
	}
}

func TestDetectCorrections_Quality(t *testing.T) {
	d := makeDialogue(
		userTurn("that doesn't work at all"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "quality" {
		t.Errorf("pattern = %q, want quality", corrections[0].Pattern)
	}
}

func TestDetectCorrections_Repetition(t *testing.T) {
	d := makeDialogue(
		userTurn("as i said, use the existing interface"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "repetition" {
		t.Errorf("pattern = %q, want repetition", corrections[0].Pattern)
	}
}

func TestDetectCorrections_ShortNegation(t *testing.T) {
	longAssistant := assistantTurn("Here is a very detailed explanation of the implementation that goes on for quite a while, covering multiple aspects of the architecture and design decisions that were made during the development process, including the rationale behind each choice.")
	d := makeDialogue(
		longAssistant,
		userTurn("no"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "short-negation" {
		t.Errorf("pattern = %q, want short-negation", corrections[0].Pattern)
	}
}

func TestDetectCorrections_NoFalsePositives(t *testing.T) {
	d := makeDialogue(
		userTurn("Please implement the login page"),
		userTurn("Add error handling too"),
		userTurn("Looks good, ship it"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 0 {
		t.Errorf("expected 0 corrections, got %d: %+v", len(corrections), corrections)
	}
}

func TestDetectCorrections_Multiple(t *testing.T) {
	d := makeDialogue(
		userTurn("no, that's wrong"),
		assistantTurn("Let me fix that."),
		userTurn("actually, use the other pattern"),
		assistantTurn("Switching approach."),
		userTurn("that still doesn't work"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 3 {
		t.Errorf("expected 3 corrections, got %d", len(corrections))
	}
}

func TestDetectCorrections_StopRedirect(t *testing.T) {
	d := makeDialogue(
		userTurn("stop, go back to the previous approach"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) < 1 {
		t.Fatalf("expected >= 1 correction, got %d", len(corrections))
	}
}

func TestDetectCorrections_WaitRedirect(t *testing.T) {
	d := makeDialogue(
		userTurn("wait, let me think about this"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "redirect" {
		t.Errorf("pattern = %q, want redirect", corrections[0].Pattern)
	}
}

func TestDetectCorrections_StillBroken(t *testing.T) {
	d := makeDialogue(
		userTurn("the build is still broken after your changes"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "quality" {
		t.Errorf("pattern = %q, want quality", corrections[0].Pattern)
	}
}

func TestDetectCorrections_RollBack(t *testing.T) {
	d := makeDialogue(
		userTurn("let's roll back to the previous version"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "undo" {
		t.Errorf("pattern = %q, want undo", corrections[0].Pattern)
	}
}

func TestDetectCorrections_IMeant(t *testing.T) {
	d := makeDialogue(
		userTurn("I meant the other file, not this one"),
	)
	corrections := DetectCorrections(d)
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Pattern != "negation" {
		t.Errorf("pattern = %q, want negation", corrections[0].Pattern)
	}
}
