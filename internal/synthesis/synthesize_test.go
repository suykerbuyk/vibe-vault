package synthesis

import (
	"context"
	"errors"
	"testing"

	"github.com/johns/vibe-vault/internal/llm"
	"github.com/johns/vibe-vault/internal/noteparse"
)

type mockProvider struct {
	response *llm.Response
	err      error
	calls    int
}

func (m *mockProvider) ChatCompletion(_ context.Context, _ llm.Request) (*llm.Response, error) {
	m.calls++
	return m.response, m.err
}

func (m *mockProvider) Name() string { return "mock" }

func TestSynthesize_FullResult(t *testing.T) {
	resp := `{
		"learnings": [{"section": "Decisions", "entry": "Use mdutil for shared utils"}],
		"stale_entries": [{"file": "knowledge.md", "section": "Patterns", "index": 0, "entry": "old pattern", "reason": "superseded"}],
		"resume_update": {"current_state": "Building synthesis", "open_threads": "Integration tests"},
		"task_updates": [{"name": "synthesis-agent", "action": "update_status", "status": "Phase 4 done", "reason": "LLM call complete"}],
		"reasoning": "Session focused on synthesis agent implementation"
	}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	result, err := Synthesize(context.Background(), mp, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Learnings) != 1 {
		t.Errorf("learnings: got %d, want 1", len(result.Learnings))
	}
	if len(result.StaleEntries) != 1 {
		t.Errorf("stale entries: got %d, want 1", len(result.StaleEntries))
	}
	if result.ResumeUpdate == nil {
		t.Error("expected resume update")
	}
	if len(result.TaskUpdates) != 1 {
		t.Errorf("task updates: got %d, want 1", len(result.TaskUpdates))
	}
	if mp.calls != 1 {
		t.Errorf("calls: got %d, want 1", mp.calls)
	}
}

func TestSynthesize_EmptyResult(t *testing.T) {
	resp := `{"learnings":[],"stale_entries":[],"resume_update":null,"task_updates":[],"reasoning":"nothing to do"}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	result, err := Synthesize(context.Background(), mp, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Learnings) != 0 {
		t.Errorf("expected empty learnings, got %d", len(result.Learnings))
	}
}

func TestSynthesize_NilProvider(t *testing.T) {
	result, err := Synthesize(context.Background(), nil, &Input{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil result for nil provider")
	}
}

func TestSynthesize_LLMError(t *testing.T) {
	mp := &mockProvider{err: errors.New("API timeout")}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	_, err := Synthesize(context.Background(), mp, input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSynthesize_InvalidJSON(t *testing.T) {
	mp := &mockProvider{response: &llm.Response{Content: "not json"}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	_, err := Synthesize(context.Background(), mp, input)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSynthesize_InvalidSection(t *testing.T) {
	resp := `{"learnings":[{"section":"Invalid","entry":"test"}],"stale_entries":[],"task_updates":[],"reasoning":""}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	result, err := Synthesize(context.Background(), mp, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Learnings) != 0 {
		t.Errorf("invalid section should be dropped, got %d learnings", len(result.Learnings))
	}
}

func TestSynthesize_InvalidTaskAction(t *testing.T) {
	resp := `{"learnings":[],"stale_entries":[],"task_updates":[{"name":"t","action":"delete","status":"","reason":""}],"reasoning":""}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	result, err := Synthesize(context.Background(), mp, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TaskUpdates) != 0 {
		t.Errorf("invalid action should be dropped, got %d", len(result.TaskUpdates))
	}
}

func TestSynthesize_InvalidStaleFile(t *testing.T) {
	resp := `{"learnings":[],"stale_entries":[{"file":"other.md","section":"A","index":0,"entry":"","reason":""}],"task_updates":[],"reasoning":""}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	result, err := Synthesize(context.Background(), mp, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.StaleEntries) != 0 {
		t.Errorf("invalid file should be dropped, got %d", len(result.StaleEntries))
	}
}

func TestSynthesize_NegativeIndex(t *testing.T) {
	resp := `{"learnings":[],"stale_entries":[{"file":"knowledge.md","section":"A","index":-1,"entry":"","reason":""}],"task_updates":[],"reasoning":""}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}
	input := &Input{SessionNote: &noteparse.Note{Summary: "test"}}

	result, err := Synthesize(context.Background(), mp, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.StaleEntries) != 0 {
		t.Errorf("negative index should be dropped, got %d", len(result.StaleEntries))
	}
}
