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

func TestInferSummary_CommitPrefix(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindFileCreate},
			{Kind: KindFileCreate},
			{Kind: KindFileModify},
			{Kind: KindTestRun},
		}},
	}
	commits := []Commit{{SHA: "abc1234", Message: "feat: add JWT authentication"}}
	got := inferSummary(segments, "Add JWT authentication", commits)
	if !strings.Contains(got, "feat:") {
		t.Errorf("expected feat prefix, got %q", got)
	}
	if !strings.Contains(got, "add JWT authentication") {
		t.Errorf("expected commit subject, got %q", got)
	}
	if !strings.Contains(got, "2+1 files") {
		t.Errorf("expected condensed file count, got %q", got)
	}
	if !strings.Contains(got, "tests pass") {
		t.Errorf("expected tests pass, got %q", got)
	}
}

func TestInferSummary_TitleFallback(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindFileModify},
			{Kind: KindTestRun},
		}},
	}
	got := inferSummary(segments, "Implement the login page", nil)
	if !strings.Contains(got, "Implement the login page") {
		t.Errorf("expected title as subject, got %q", got)
	}
	if !strings.Contains(got, "tests pass") {
		t.Errorf("expected tests pass, got %q", got)
	}
}

func TestInferSummary_TestsFailed(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindTestRun, IsError: true},
		}},
	}
	got := inferSummary(segments, "Fix tests", nil)
	if !strings.Contains(got, "tests fail") {
		t.Errorf("expected tests fail, got %q", got)
	}
}

func TestInferSummary_MixedTests(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindTestRun, IsError: true},
			{Kind: KindTestRun, IsError: false},
		}},
	}
	got := inferSummary(segments, "Fix tests", nil)
	if !strings.Contains(got, "mixed tests") {
		t.Errorf("expected mixed tests, got %q", got)
	}
}

func TestInferSummary_CommitAndPush(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindFileModify},
			{Kind: KindGitCommit},
			{Kind: KindGitPush},
		}},
	}
	got := inferSummary(segments, "Deploy changes", nil)
	if !strings.Contains(got, "pushed") {
		t.Errorf("expected pushed, got %q", got)
	}
}

func TestInferSummary_CommitOnly(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindFileModify},
			{Kind: KindGitCommit},
		}},
	}
	got := inferSummary(segments, "Save changes", nil)
	if !strings.Contains(got, "committed") {
		t.Errorf("expected committed, got %q", got)
	}
}

func TestInferSummary_ErrorRecoveries(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindTestRun, IsError: true, Recovered: true},
			{Kind: KindFileModify},
			{Kind: KindTestRun},
		}},
	}
	got := inferSummary(segments, "Fix the bug", nil)
	if !strings.Contains(got, "resolved 1 errors") {
		t.Errorf("expected recovery, got %q", got)
	}
}

func TestInferSummary_NoActivities(t *testing.T) {
	segments := []Segment{{}}
	got := inferSummary(segments, "", nil)
	if got != "Claude Code session." {
		t.Errorf("got %q", got)
	}
}

func TestInferSummary_SessionTitle(t *testing.T) {
	// "Session" title should be treated as empty
	segments := []Segment{
		{Activities: []Activity{{Kind: KindFileModify}}},
	}
	got := inferSummary(segments, "Session", nil)
	// Should still produce something from outcomes
	if !strings.Contains(got, "1 files") {
		t.Errorf("expected file count, got %q", got)
	}
}

func TestInferIntentPrefix_FromCommit(t *testing.T) {
	commits := []Commit{{SHA: "abc", Message: "fix: handle nil pointer"}}
	got := inferIntentPrefix(nil, commits)
	if got != "fix" {
		t.Errorf("got %q, want fix", got)
	}
}

func TestInferIntentPrefix_FromActivities(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindFileCreate},
			{Kind: KindTestRun},
		}},
	}
	got := inferIntentPrefix(segments, nil)
	if got != "feat" {
		t.Errorf("got %q, want feat", got)
	}
}

func TestInferIntentPrefix_PlanMode(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindPlanMode},
			{Kind: KindExplore},
		}},
	}
	got := inferIntentPrefix(segments, nil)
	if got != "plan" {
		t.Errorf("got %q, want plan", got)
	}
}

func TestInferIntentPrefix_Explore(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindExplore},
			{Kind: KindExplore},
		}},
	}
	got := inferIntentPrefix(segments, nil)
	if got != "explore" {
		t.Errorf("got %q, want explore", got)
	}
}

func TestInferSubject_FromCommit(t *testing.T) {
	commits := []Commit{{SHA: "abc", Message: "feat: add auth handler"}}
	got := inferSubject("Implement authentication", commits)
	if got != "add auth handler" {
		t.Errorf("got %q, want %q", got, "add auth handler")
	}
}

func TestInferSubject_FromTitle(t *testing.T) {
	got := inferSubject("Implement authentication", nil)
	if got != "Implement authentication" {
		t.Errorf("got %q", got)
	}
}

func TestInferSubject_EmptyTitle(t *testing.T) {
	got := inferSubject("Session", nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractConventionalPrefix(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"feat: add auth", "feat"},
		{"fix(auth): nil pointer", "fix"},
		{"refactor: clean up code", "refactor"},
		{"docs: update README", "docs"},
		{"just some message", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := extractConventionalPrefix(tc.msg)
		if got != tc.want {
			t.Errorf("extractConventionalPrefix(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

func TestStripConventionalPrefix(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"feat: add auth", "add auth"},
		{"fix(auth): nil pointer", "nil pointer"},
		{"just a message", "just a message"},
	}
	for _, tc := range tests {
		got := stripConventionalPrefix(tc.msg)
		if got != tc.want {
			t.Errorf("stripConventionalPrefix(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

func TestFormatOutcomes(t *testing.T) {
	segments := []Segment{
		{Activities: []Activity{
			{Kind: KindFileCreate},
			{Kind: KindFileCreate},
			{Kind: KindFileModify},
			{Kind: KindTestRun},
			{Kind: KindGitCommit},
		}},
	}
	got := formatOutcomes(segments)
	if !strings.Contains(got, "2+1 files") {
		t.Errorf("expected 2+1 files, got %q", got)
	}
	if !strings.Contains(got, "tests pass") {
		t.Errorf("expected tests pass, got %q", got)
	}
	if !strings.Contains(got, "committed") {
		t.Errorf("expected committed, got %q", got)
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
		got := IsNoiseMessage(tc.msg)
		if got != tc.want {
			t.Errorf("IsNoiseMessage(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}
