package narrative

import (
	"strings"
	"testing"
	"time"
)

func TestRenderTimeline_Empty(t *testing.T) {
	result := RenderTimeline(nil)
	if result != "" {
		t.Errorf("expected empty for nil segments, got %q", result)
	}
}

func TestRenderTimeline_TrivialSession(t *testing.T) {
	// <= 5 activities should return empty
	segments := []Segment{{
		Activities: []Activity{
			{Timestamp: time.Date(2026, 2, 22, 14, 56, 0, 0, time.UTC), Kind: KindFileCreate, Description: "Created `main.go`"},
			{Timestamp: time.Date(2026, 2, 22, 14, 57, 0, 0, time.UTC), Kind: KindTestRun, Description: "Tests passed"},
		},
	}}

	result := RenderTimeline(segments)
	if result != "" {
		t.Errorf("expected empty for trivial session, got %q", result)
	}
}

func TestRenderTimeline_Renders(t *testing.T) {
	base := time.Date(2026, 2, 22, 14, 56, 0, 0, time.UTC)
	segments := []Segment{{
		Activities: []Activity{
			{Timestamp: base, Kind: KindExplore, Description: "Explored 5 files"},
			{Timestamp: base.Add(1 * time.Minute), Kind: KindFileCreate, Description: "Created `discover.go`"},
			{Timestamp: base.Add(2 * time.Minute), Kind: KindFileModify, Description: "Modified `main.go`"},
			{Timestamp: base.Add(3 * time.Minute), Kind: KindTestRun, Description: "Tests passed (go test)"},
			{Timestamp: base.Add(4 * time.Minute), Kind: KindTestRun, Description: "Tests failed", IsError: true},
			{Timestamp: base.Add(5 * time.Minute), Kind: KindGitCommit, Description: "Committed: feat: backfill"},
		},
	}}

	result := RenderTimeline(segments)
	if result == "" {
		t.Fatal("expected non-empty timeline for >5 activities")
	}

	// Check timestamps are present
	if !strings.Contains(result, "14:56") {
		t.Error("missing start timestamp")
	}
	if !strings.Contains(result, "15:01") {
		t.Error("missing last timestamp")
	}

	// Check icons
	if !strings.Contains(result, "Explored") {
		t.Error("missing Explored icon")
	}
	if !strings.Contains(result, "Created") {
		t.Error("missing Created icon")
	}
	if !strings.Contains(result, "Committed") {
		t.Error("missing Committed icon")
	}

	// Check descriptions
	if !strings.Contains(result, "discover.go") {
		t.Error("missing file reference")
	}
}

func TestRenderTimeline_SkipsZeroTimestamp(t *testing.T) {
	base := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	activities := make([]Activity, 7)
	for i := range activities {
		activities[i] = Activity{
			Timestamp:   base.Add(time.Duration(i) * time.Minute),
			Kind:        KindCommand,
			Description: "Ran command",
		}
	}
	// One has zero timestamp
	activities[3].Timestamp = time.Time{}

	segments := []Segment{{Activities: activities}}
	result := RenderTimeline(segments)

	// Should have 6 lines (skipping the zero-timestamp one)
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 6 {
		t.Errorf("expected 6 lines, got %d", len(lines))
	}
}

func TestActivityIcon(t *testing.T) {
	tests := []struct {
		kind ActivityKind
		want string
	}{
		{KindFileCreate, "Created"},
		{KindFileModify, "Modified"},
		{KindTestRun, "Tests"},
		{KindGitCommit, "Committed"},
		{KindGitPush, "Pushed"},
		{KindBuild, "Built"},
		{KindCommand, "Ran"},
		{KindDecision, "Decided"},
		{KindPlanMode, "Planned"},
		{KindDelegation, "Delegated"},
		{KindExplore, "Explored"},
		{KindError, "Error"},
		{ActivityKind(99), ""},
	}

	for _, tt := range tests {
		got := activityIcon(tt.kind)
		if got != tt.want {
			t.Errorf("activityIcon(%d) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
