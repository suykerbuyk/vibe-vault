package narrative

import (
	"strings"
	"testing"
)

func TestRenderWorkPerformed_Empty(t *testing.T) {
	got := RenderWorkPerformed([]Segment{{}})
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderWorkPerformed_SingleSegment(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindFileCreate, Description: "Created `handler.go`"},
				{Kind: KindTestRun, Description: "Ran tests (success)"},
				{Kind: KindGitCommit, Description: `Committed: "feat: add handler"`},
			},
		},
	}
	got := RenderWorkPerformed(segments)
	if !strings.Contains(got, "- Created `handler.go`") {
		t.Errorf("missing file create line")
	}
	if !strings.Contains(got, "- Ran tests (success)") {
		t.Errorf("missing test line")
	}
	if !strings.Contains(got, "- Committed:") {
		t.Errorf("missing commit line")
	}
	// Should NOT have segment headers in single-segment
	if strings.Contains(got, "### Segment") {
		t.Errorf("unexpected segment header in single-segment output")
	}
}

func TestRenderWorkPerformed_MultiSegment(t *testing.T) {
	segments := []Segment{
		{
			UserRequest: "Implement JWT auth",
			Activities: []Activity{
				{Kind: KindFileCreate, Description: "Created `auth.go`"},
			},
		},
		{
			UserRequest: "Add refresh tokens",
			Activities: []Activity{
				{Kind: KindFileCreate, Description: "Created `refresh.go`"},
				{Kind: KindGitCommit, Description: `Committed: "feat: refresh tokens"`},
			},
		},
	}
	got := RenderWorkPerformed(segments)
	if !strings.Contains(got, "### Segment 1") {
		t.Errorf("missing segment 1 header")
	}
	if !strings.Contains(got, "### Segment 2") {
		t.Errorf("missing segment 2 header")
	}
	if !strings.Contains(got, `> "Implement JWT auth"`) {
		t.Errorf("missing segment 1 user request")
	}
	if !strings.Contains(got, `> "Add refresh tokens"`) {
		t.Errorf("missing segment 2 user request")
	}
}

func TestRenderWorkPerformed_SkipsEmptySegments(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindFileCreate, Description: "Created `handler.go`"},
			},
		},
		{Activities: nil}, // empty segment
		{
			Activities: []Activity{
				{Kind: KindTestRun, Description: "Ran tests (success)"},
			},
		},
	}
	got := RenderWorkPerformed(segments)
	if !strings.Contains(got, "### Segment 1") {
		t.Errorf("missing segment 1")
	}
	if !strings.Contains(got, "### Segment 2") {
		t.Errorf("missing segment 2")
	}
	// Should not have segment 3 (empty was skipped)
	if strings.Contains(got, "### Segment 3") {
		t.Errorf("unexpected segment 3 for empty segment")
	}
}

func TestRenderWorkPerformed_FilteredLongSession(t *testing.T) {
	// Create >50 activities to trigger filtering
	var activities []Activity
	for i := 0; i < 40; i++ {
		activities = append(activities, Activity{Kind: KindCommand, Description: "Ran `cmd` (success)"})
	}
	// Add some important ones
	activities = append(activities,
		Activity{Kind: KindFileCreate, Description: "Created `main.go`"},
		Activity{Kind: KindTestRun, Description: "Ran tests (success)"},
		Activity{Kind: KindGitCommit, Description: `Committed: "done"`},
	)
	// Push over 50
	for i := 0; i < 10; i++ {
		activities = append(activities, Activity{Kind: KindCommand, Description: "Ran `extra` (success)"})
	}

	segments := []Segment{{Activities: activities}}
	got := RenderWorkPerformed(segments)

	// Important activities always present
	if !strings.Contains(got, "Created `main.go`") {
		t.Error("missing file create in filtered output")
	}
	if !strings.Contains(got, "Ran tests (success)") {
		t.Error("missing test in filtered output")
	}
	if !strings.Contains(got, "Committed:") {
		t.Error("missing commit in filtered output")
	}
	// Should have "and N more" for commands
	if !strings.Contains(got, "... and") {
		t.Error("missing overflow indicator for filtered commands")
	}
}
