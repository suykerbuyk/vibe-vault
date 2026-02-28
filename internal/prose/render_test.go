package prose

import (
	"strings"
	"testing"
)

func TestRender_Empty(t *testing.T) {
	if got := Render(nil); got != "" {
		t.Errorf("nil dialogue: got %q, want empty", got)
	}
	if got := Render(&Dialogue{}); got != "" {
		t.Errorf("empty dialogue: got %q, want empty", got)
	}
}

func TestRender_SingleUserTurn(t *testing.T) {
	d := &Dialogue{
		Sections: []Section{{
			UserRequest: "Add auth",
			Elements: []Element{
				{Turn: &Turn{Role: "user", Text: "Add authentication to the API"}},
			},
		}},
	}
	out := Render(d)
	if !strings.Contains(out, "> **User:** Add authentication to the API") {
		t.Errorf("missing user blockquote, got:\n%s", out)
	}
}

func TestRender_UserAndAssistant(t *testing.T) {
	d := &Dialogue{
		Sections: []Section{{
			UserRequest: "Add auth",
			Elements: []Element{
				{Turn: &Turn{Role: "user", Text: "Add JWT authentication"}},
				{Turn: &Turn{Role: "assistant", Text: "I'll implement JWT authentication for the API."}},
			},
		}},
	}
	out := Render(d)
	if !strings.Contains(out, "> **User:** Add JWT authentication") {
		t.Errorf("missing user turn, got:\n%s", out)
	}
	if !strings.Contains(out, "I'll implement JWT authentication for the API.") {
		t.Errorf("missing assistant turn, got:\n%s", out)
	}
}

func TestRender_Markers(t *testing.T) {
	d := &Dialogue{
		Sections: []Section{{
			Elements: []Element{
				{Turn: &Turn{Role: "user", Text: "Do it"}},
				{Marker: &Marker{Text: "Created `handler.go`"}},
			},
		}},
	}
	out := Render(d)
	if !strings.Contains(out, "*Created `handler.go`*") {
		t.Errorf("missing italic marker, got:\n%s", out)
	}
}

func TestRender_ConsecutiveMarkers(t *testing.T) {
	d := &Dialogue{
		Sections: []Section{{
			Elements: []Element{
				{Turn: &Turn{Role: "user", Text: "Create files"}},
				{Marker: &Marker{Text: "Created `a.go`"}},
				{Marker: &Marker{Text: "Created `b.go`"}},
				{Marker: &Marker{Text: "Created `c.go`"}},
			},
		}},
	}
	out := Render(d)
	// Consecutive markers should not have blank lines between them
	if strings.Contains(out, "*Created `a.go`*\n\n*Created `b.go`*") {
		t.Error("consecutive markers should not have blank lines between them")
	}
	if !strings.Contains(out, "*Created `a.go`*\n*Created `b.go`*\n*Created `c.go`*") {
		t.Errorf("consecutive markers should be grouped, got:\n%s", out)
	}
}

func TestRender_MultiSection(t *testing.T) {
	d := &Dialogue{
		Sections: []Section{
			{
				UserRequest: "Add auth",
				Elements: []Element{
					{Turn: &Turn{Role: "user", Text: "Add auth"}},
				},
			},
			{
				UserRequest: "Fix bug",
				Elements: []Element{
					{Turn: &Turn{Role: "user", Text: "Fix the login bug"}},
				},
			},
		},
	}
	out := Render(d)
	if !strings.Contains(out, "### Segment 1") {
		t.Errorf("missing segment 1 header, got:\n%s", out)
	}
	if !strings.Contains(out, "### Segment 2") {
		t.Errorf("missing segment 2 header, got:\n%s", out)
	}
	if !strings.Contains(out, `"Add auth"`) {
		t.Errorf("missing section 1 quote, got:\n%s", out)
	}
	if !strings.Contains(out, `"Fix bug"`) {
		t.Errorf("missing section 2 quote, got:\n%s", out)
	}
}

func TestRender_SingleSection(t *testing.T) {
	d := &Dialogue{
		Sections: []Section{{
			UserRequest: "Only task",
			Elements: []Element{
				{Turn: &Turn{Role: "user", Text: "Only task"}},
			},
		}},
	}
	out := Render(d)
	if strings.Contains(out, "### Segment") {
		t.Errorf("single section should not have segment header, got:\n%s", out)
	}
}

func TestRender_UserTruncation(t *testing.T) {
	longText := strings.Repeat("x", 600)
	d := &Dialogue{
		Sections: []Section{{
			Elements: []Element{
				{Turn: &Turn{Role: "user", Text: longText + " [...]"}},
			},
		}},
	}
	out := Render(d)
	if !strings.Contains(out, "[...]") {
		t.Error("truncated text should show [...] in output")
	}
}
