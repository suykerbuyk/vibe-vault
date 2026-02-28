package prose

import (
	"strings"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/transcript"
)

var ts = time.Date(2027, 6, 15, 10, 0, 0, 0, time.UTC)

func makeUserEntry(content string) transcript.Entry {
	return transcript.Entry{
		Type:      "user",
		Timestamp: ts,
		CWD:       "/home/dev/project",
		Message: &transcript.Message{
			Role:    "user",
			Content: content,
		},
	}
}

func makeAssistantEntry(text string, toolUses ...transcript.ContentBlock) transcript.Entry {
	var msgContent interface{} = text
	if len(toolUses) > 0 {
		blocks := []interface{}{
			map[string]interface{}{"type": "text", "text": text},
		}
		for _, tu := range toolUses {
			block := map[string]interface{}{
				"type": "tool_use",
				"id":   tu.ID,
				"name": tu.Name,
			}
			if tu.Input != nil {
				block["input"] = tu.Input
			}
			blocks = append(blocks, block)
		}
		msgContent = blocks
	}

	return transcript.Entry{
		Type:      "assistant",
		Timestamp: ts,
		CWD:       "/home/dev/project",
		Message: &transcript.Message{
			Role:    "assistant",
			Content: msgContent,
		},
	}
}

func makeToolResultEntry(toolUseID string, isError bool, content string) transcript.Entry {
	return transcript.Entry{
		Type:      "user",
		Timestamp: ts,
		Message: &transcript.Message{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": toolUseID,
					"is_error":    isError,
					"content":     content,
				},
			},
		},
	}
}

func makeSystemEntry(subtype string) transcript.Entry {
	return transcript.Entry{
		Type:      "system",
		Subtype:   subtype,
		Timestamp: ts,
	}
}

func makeMetaEntry(content string) transcript.Entry {
	e := makeUserEntry(content)
	e.IsMeta = true
	return e
}

func makeThinkingAssistant(thinking, text string) transcript.Entry {
	return transcript.Entry{
		Type:      "assistant",
		Timestamp: ts,
		CWD:       "/home/dev/project",
		Message: &transcript.Message{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": thinking},
				map[string]interface{}{"type": "text", "text": text},
			},
		},
	}
}

func makePlanEntry(planContent string) transcript.Entry {
	e := makeUserEntry("Implement the following plan:")
	e.PlanContent = planContent
	return e
}

func extract(entries ...transcript.Entry) *Dialogue {
	t := &transcript.Transcript{Entries: entries}
	return Extract(t, "/home/dev/project")
}

// --- Tests ---

func TestExtract_Nil(t *testing.T) {
	d := Extract(nil, "/tmp")
	if d != nil {
		t.Error("expected nil for nil transcript")
	}
}

func TestExtract_Empty(t *testing.T) {
	d := Extract(&transcript.Transcript{}, "/tmp")
	if d != nil {
		t.Error("expected nil for empty transcript")
	}
}

func TestExtract_SingleUserTurn(t *testing.T) {
	d := extract(makeUserEntry("Add authentication to the API"))
	if d == nil {
		t.Fatal("expected dialogue")
	}
	if len(d.Sections) != 1 {
		t.Fatalf("sections: got %d, want 1", len(d.Sections))
	}
	sec := d.Sections[0]
	if len(sec.Elements) != 1 {
		t.Fatalf("elements: got %d, want 1", len(sec.Elements))
	}
	el := sec.Elements[0]
	if el.Turn == nil || el.Turn.Role != "user" {
		t.Error("expected user turn")
	}
	if el.Turn.Text != "Add authentication to the API" {
		t.Errorf("text: got %q", el.Turn.Text)
	}
}

func TestExtract_SingleAssistantTurn(t *testing.T) {
	d := extract(
		makeUserEntry("Add auth"),
		makeAssistantEntry("I'll implement JWT authentication for the API. This involves creating a middleware that validates tokens on each request."),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	// Should have user turn + assistant turn
	elems := d.Sections[0].Elements
	if len(elems) != 2 {
		t.Fatalf("elements: got %d, want 2", len(elems))
	}
	if elems[1].Turn == nil || elems[1].Turn.Role != "assistant" {
		t.Error("expected assistant turn")
	}
}

func TestExtract_FillerFiltered(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Read",
	}
	d := extract(
		makeUserEntry("Fix the bug"),
		makeAssistantEntry("Let me read the file.", tu),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	// Should have user turn only — assistant text is filler (< 120 chars + has tool_use)
	// No marker for Read tool
	if len(elems) != 1 {
		t.Fatalf("elements: got %d, want 1 (filler should be filtered)", len(elems))
	}
	if elems[0].Turn == nil || elems[0].Turn.Role != "user" {
		t.Error("expected only user turn")
	}
}

func TestExtract_LongTextKept(t *testing.T) {
	longText := strings.Repeat("I'll implement this feature with careful attention to the existing patterns. ", 5)
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Write",
		Input: map[string]interface{}{
			"file_path": "/home/dev/project/handler.go",
		},
	}
	d := extract(
		makeUserEntry("Add handler"),
		makeAssistantEntry(longText, tu),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	// Should have user turn + assistant turn + marker
	hasAssistant := false
	for _, el := range elems {
		if el.Turn != nil && el.Turn.Role == "assistant" {
			hasAssistant = true
		}
	}
	if !hasAssistant {
		t.Error("long text with tool_use should be kept")
	}
}

func TestExtract_PureTextKept(t *testing.T) {
	d := extract(
		makeUserEntry("What does this do?"),
		makeAssistantEntry("Yes."), // short but no tool_use → kept
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 2 {
		t.Fatalf("elements: got %d, want 2", len(elems))
	}
	if elems[1].Turn == nil || elems[1].Turn.Role != "assistant" {
		t.Error("short pure-text assistant should be kept")
	}
}

func TestExtract_UserNoiseFiltered(t *testing.T) {
	d := extract(
		makeUserEntry("ok"),
		makeUserEntry("/restart"),
		makeUserEntry("Add the feature"),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 1 {
		t.Fatalf("elements: got %d, want 1", len(elems))
	}
	if elems[0].Turn.Text != "Add the feature" {
		t.Errorf("text: got %q", elems[0].Turn.Text)
	}
}

func TestExtract_ToolResultSkipped(t *testing.T) {
	d := extract(
		makeUserEntry("Add feature"),
		makeToolResultEntry("tu1", false, "success"),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	// Only user turn, tool result entry skipped
	if len(elems) != 1 {
		t.Fatalf("elements: got %d, want 1", len(elems))
	}
}

func TestExtract_ThinkingSkipped(t *testing.T) {
	d := extract(
		makeUserEntry("Plan this"),
		makeThinkingAssistant("Let me think deeply...", "Here's my approach to the problem."),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 2 {
		t.Fatalf("elements: got %d, want 2", len(elems))
	}
	// Assistant text should NOT contain thinking
	if strings.Contains(elems[1].Turn.Text, "think deeply") {
		t.Error("thinking text should be excluded")
	}
	if elems[1].Turn.Text != "Here's my approach to the problem." {
		t.Errorf("text: got %q", elems[1].Turn.Text)
	}
}

func TestExtract_SystemSkipped(t *testing.T) {
	d := extract(
		makeSystemEntry("init"),
		makeUserEntry("Add feature"),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 1 {
		t.Fatalf("elements: got %d, want 1", len(elems))
	}
}

func TestExtract_MetaSkipped(t *testing.T) {
	d := extract(
		makeMetaEntry("CLAUDE.md context loading"),
		makeUserEntry("Implement the handler"),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 1 {
		t.Fatalf("elements: got %d, want 1", len(elems))
	}
	if elems[0].Turn.Text != "Implement the handler" {
		t.Errorf("text: got %q", elems[0].Turn.Text)
	}
}

func TestExtract_PlanContent(t *testing.T) {
	d := extract(
		makePlanEntry("# Auth Plan\n\nUse JWT tokens with refresh flow."),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 1 {
		t.Fatalf("elements: got %d, want 1", len(elems))
	}
	if elems[0].Turn.Text != "# Auth Plan\n\nUse JWT tokens with refresh flow." {
		t.Errorf("text: got %q", elems[0].Turn.Text)
	}
	if d.Sections[0].UserRequest != "# Auth Plan" {
		t.Errorf("user request: got %q", d.Sections[0].UserRequest)
	}
}

func TestExtract_TurnOrder(t *testing.T) {
	d := extract(
		makeUserEntry("Step 1"),
		makeAssistantEntry("Response 1"),
		makeUserEntry("Step 2"),
		makeAssistantEntry("Response 2"),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	if len(elems) != 4 {
		t.Fatalf("elements: got %d, want 4", len(elems))
	}
	expected := []string{"user", "assistant", "user", "assistant"}
	for i, want := range expected {
		if elems[i].Turn == nil || elems[i].Turn.Role != want {
			t.Errorf("element %d: got role %v, want %s", i, elems[i].Turn, want)
		}
	}
}

func TestExtract_TestMarker(t *testing.T) {
	tu := transcript.ContentBlock{
		Type:  "tool_use",
		ID:    "tu1",
		Name:  "Bash",
		Input: map[string]interface{}{"command": "go test ./..."},
	}
	d := extract(
		makeUserEntry("Run tests"),
		makeAssistantEntry("Running tests.", tu),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	hasTestMarker := false
	for _, el := range elems {
		if el.Marker != nil && strings.Contains(el.Marker.Text, "Ran tests") {
			hasTestMarker = true
		}
	}
	if !hasTestMarker {
		t.Error("expected test marker")
	}
}

func TestExtract_CommitMarker(t *testing.T) {
	tu := transcript.ContentBlock{
		Type:  "tool_use",
		ID:    "tu1",
		Name:  "Bash",
		Input: map[string]interface{}{"command": `git commit -m "feat: add auth"`},
	}
	d := extract(
		makeUserEntry("Commit"),
		makeAssistantEntry("Committing.", tu),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	hasCommitMarker := false
	for _, el := range elems {
		if el.Marker != nil && strings.Contains(el.Marker.Text, "feat: add auth") {
			hasCommitMarker = true
		}
	}
	if !hasCommitMarker {
		t.Error("expected commit marker")
	}
}

func TestExtract_FileCreateMarker(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Write",
		Input: map[string]interface{}{
			"file_path": "/home/dev/project/src/handler.go",
		},
	}
	d := extract(
		makeUserEntry("Create handler"),
		makeAssistantEntry("Creating handler.", tu),
	)
	if d == nil {
		t.Fatal("expected dialogue")
	}
	elems := d.Sections[0].Elements
	hasCreateMarker := false
	for _, el := range elems {
		if el.Marker != nil && strings.Contains(el.Marker.Text, "Created `src/handler.go`") {
			hasCreateMarker = true
		}
	}
	if !hasCreateMarker {
		t.Error("expected file create marker")
	}
}

func TestExtract_SegmentBoundary(t *testing.T) {
	entries := []transcript.Entry{
		makeUserEntry("First task"),
		makeAssistantEntry("Working on first task."),
		makeSystemEntry("compact_boundary"),
		makeUserEntry("Second task"),
		makeAssistantEntry("Working on second task."),
	}
	tr := &transcript.Transcript{Entries: entries}
	d := Extract(tr, "/home/dev/project")
	if d == nil {
		t.Fatal("expected dialogue")
	}
	if len(d.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(d.Sections))
	}
	if d.Sections[0].UserRequest != "First task" {
		t.Errorf("section 0 user request: got %q", d.Sections[0].UserRequest)
	}
	if d.Sections[1].UserRequest != "Second task" {
		t.Errorf("section 1 user request: got %q", d.Sections[1].UserRequest)
	}
}

func TestExtract_UserTruncation(t *testing.T) {
	longText := strings.Repeat("x", 600)
	d := extract(makeUserEntry(longText))
	if d == nil {
		t.Fatal("expected dialogue")
	}
	text := d.Sections[0].Elements[0].Turn.Text
	if len(text) > userMaxChars+10 { // +10 for " [...]"
		t.Errorf("user text not truncated: len=%d", len(text))
	}
	if !strings.HasSuffix(text, "[...]") {
		t.Error("truncated text should end with [...]")
	}
}
