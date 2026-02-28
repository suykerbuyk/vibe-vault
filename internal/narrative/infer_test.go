package narrative

import (
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/transcript"
)

func TestInferTitle_FromSegmentRequest(t *testing.T) {
	segments := []Segment{
		{UserRequest: "Implement JWT authentication"},
	}
	got := inferTitle(segments, &transcript.Transcript{})
	if got != "Implement JWT authentication" {
		t.Errorf("got %q", got)
	}
}

func TestInferTitle_SkipsEmptySegments(t *testing.T) {
	segments := []Segment{
		{UserRequest: ""},
		{UserRequest: "Fix the auth bug"},
	}
	got := inferTitle(segments, &transcript.Transcript{})
	if got != "Fix the auth bug" {
		t.Errorf("got %q", got)
	}
}

func TestInferTitle_FallsBackToTranscript(t *testing.T) {
	segments := []Segment{
		{UserRequest: ""},
	}
	tr := &transcript.Transcript{
		Entries: []transcript.Entry{
			{
				Type: "user",
				Message: &transcript.Message{
					Role:    "user",
					Content: "Add refresh token support",
				},
			},
		},
	}
	got := inferTitle(segments, tr)
	if got != "Add refresh token support" {
		t.Errorf("got %q", got)
	}
}

func TestInferTitle_FallsBackToSession(t *testing.T) {
	segments := []Segment{
		{UserRequest: ""},
	}
	tr := &transcript.Transcript{}
	got := inferTitle(segments, tr)
	if got != "Session" {
		t.Errorf("got %q", got)
	}
}

func TestInferSummary_FilesAndTests(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindFileCreate},
				{Kind: KindFileCreate},
				{Kind: KindFileModify},
				{Kind: KindTestRun},
			},
		},
	}
	got := inferSummary(segments)
	if !strings.Contains(got, "Created 2") {
		t.Errorf("expected created count, got %q", got)
	}
	if !strings.Contains(got, "modified 1") {
		t.Errorf("expected modified count, got %q", got)
	}
	if !strings.Contains(got, "All tests passed") {
		t.Errorf("expected test pass, got %q", got)
	}
}

func TestInferSummary_TestsFailed(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindTestRun, IsError: true},
			},
		},
	}
	got := inferSummary(segments)
	if !strings.Contains(got, "Tests failed") {
		t.Errorf("expected test fail, got %q", got)
	}
}

func TestInferSummary_MixedTests(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindTestRun, IsError: true},
				{Kind: KindTestRun, IsError: false},
			},
		},
	}
	got := inferSummary(segments)
	if !strings.Contains(got, "mixed results") {
		t.Errorf("expected mixed results, got %q", got)
	}
}

func TestInferSummary_CommitAndPush(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindFileModify},
				{Kind: KindGitCommit},
				{Kind: KindGitPush},
			},
		},
	}
	got := inferSummary(segments)
	if !strings.Contains(got, "committed and pushed") {
		t.Errorf("expected commit+push, got %q", got)
	}
}

func TestInferSummary_CommitOnly(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindFileModify},
				{Kind: KindGitCommit},
			},
		},
	}
	got := inferSummary(segments)
	if !strings.Contains(got, "Changes committed.") {
		t.Errorf("expected commit only, got %q", got)
	}
}

func TestInferSummary_ErrorRecoveries(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindTestRun, IsError: true, Recovered: true},
				{Kind: KindFileModify},
				{Kind: KindTestRun},
			},
		},
	}
	got := inferSummary(segments)
	if !strings.Contains(got, "Resolved 1 errors") {
		t.Errorf("expected recovery, got %q", got)
	}
}

func TestInferSummary_NoActivities(t *testing.T) {
	segments := []Segment{{}}
	got := inferSummary(segments)
	if got != "Claude Code session." {
		t.Errorf("got %q", got)
	}
}

func TestInferTag_Implementation(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindFileCreate},
				{Kind: KindFileModify},
				{Kind: KindTestRun},
			},
		},
	}
	got := inferTag(segments)
	if got != "implementation" {
		t.Errorf("got %q, want implementation", got)
	}
}

func TestInferTag_Planning(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindPlanMode},
				{Kind: KindExplore},
			},
		},
	}
	got := inferTag(segments)
	if got != "planning" {
		t.Errorf("got %q, want planning", got)
	}
}

func TestInferTag_Debugging(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindTestRun, IsError: true, Recovered: true},
				{Kind: KindFileModify},
				{Kind: KindTestRun},
			},
		},
	}
	got := inferTag(segments)
	// Writes + tests = implementation takes priority
	if got != "implementation" {
		t.Errorf("got %q, want implementation", got)
	}
}

func TestInferTag_Research(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindExplore},
				{Kind: KindExplore},
			},
		},
	}
	got := inferTag(segments)
	if got != "research" {
		t.Errorf("got %q, want research", got)
	}
}

func TestInferTag_Exploration(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindExplore},
				{Kind: KindExplore},
				{Kind: KindExplore},
				{Kind: KindExplore},
				{Kind: KindExplore},
				{Kind: KindExplore},
			},
		},
	}
	got := inferTag(segments)
	if got != "exploration" {
		t.Errorf("got %q, want exploration", got)
	}
}

func TestInferTag_Empty(t *testing.T) {
	segments := []Segment{{}}
	got := inferTag(segments)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestInferOpenThreads(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindTestRun, IsError: true, Description: "Ran tests (failed)", Detail: "go test ./..."},
				{Kind: KindCommand, IsError: true, Description: "Ran `deploy.sh` (failed)", Detail: "deploy.sh"},
			},
		},
	}
	got := inferOpenThreads(segments)
	if len(got) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(got))
	}
}

func TestInferOpenThreads_SkipsRecovered(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindTestRun, IsError: true, Recovered: true, Description: "Ran tests (failed)"},
				{Kind: KindCommand, IsError: true, Description: "Ran `cmd` (failed)", Detail: "cmd"},
			},
		},
	}
	got := inferOpenThreads(segments)
	if len(got) != 1 {
		t.Fatalf("expected 1 thread (recovered excluded), got %d", len(got))
	}
}

func TestInferOpenThreads_CappedAt5(t *testing.T) {
	var activities []Activity
	for i := 0; i < 10; i++ {
		activities = append(activities, Activity{Kind: KindCommand, IsError: true, Detail: "fail"})
	}
	segments := []Segment{{Activities: activities}}
	got := inferOpenThreads(segments)
	if len(got) != 5 {
		t.Fatalf("expected 5 threads (capped), got %d", len(got))
	}
}

func TestExtractDecisions(t *testing.T) {
	segments := []Segment{
		{
			Activities: []Activity{
				{Kind: KindDecision, Detail: "Which database should we use?"},
				{Kind: KindFileCreate},
				{Kind: KindDecision, Detail: "REST or GraphQL?"},
			},
		},
	}
	got := extractDecisions(segments, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(got))
	}
	if got[0] != "Which database should we use?" {
		t.Errorf("decision[0]: got %q", got[0])
	}
}

func TestIsNoiseMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"/restart", true},
		{"ok", true},
		{"Implement the following plan:", true},
		{"Execute the plan", true},
		{"restart", true},
		{"```\ncode block\n```", true},
		{"caveat: something", true},
		{"Fix the login bug", false},
		{"Add unit tests for the handler", false},
	}
	for _, tc := range tests {
		got := isNoiseMessage(tc.msg)
		if got != tc.want {
			t.Errorf("isNoiseMessage(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}
