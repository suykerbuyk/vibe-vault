package stats

import (
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/index"
)

func makeEntry(id, project, model, date, tag string, tokIn, tokOut, msgs, tools, dur int, files []string) index.SessionEntry {
	return index.SessionEntry{
		SessionID:    id,
		Project:      project,
		Model:        model,
		Date:         date,
		Tag:          tag,
		TokensIn:     tokIn,
		TokensOut:    tokOut,
		Messages:     msgs,
		ToolUses:     tools,
		Duration:     dur,
		FilesChanged: files,
	}
}

func TestCompute_Empty(t *testing.T) {
	s := Compute(map[string]index.SessionEntry{}, "")
	if s.TotalSessions != 0 {
		t.Errorf("TotalSessions = %d, want 0", s.TotalSessions)
	}
	if s.AvgTokensInPerMsg != 0 {
		t.Errorf("AvgTokensInPerMsg = %f, want 0", s.AvgTokensInPerMsg)
	}
	if s.AvgToolsPerSession != 0 {
		t.Errorf("AvgToolsPerSession = %f, want 0", s.AvgToolsPerSession)
	}
}

func TestCompute_SingleEntry(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "implementation", 1000, 500, 10, 5, 30, []string{"a.go"}),
	}

	s := Compute(entries, "")

	if s.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d", s.TotalSessions)
	}
	if s.TotalTokensIn != 1000 {
		t.Errorf("TotalTokensIn = %d", s.TotalTokensIn)
	}
	if s.TotalTokensOut != 500 {
		t.Errorf("TotalTokensOut = %d", s.TotalTokensOut)
	}
	if s.TotalMessages != 10 {
		t.Errorf("TotalMessages = %d", s.TotalMessages)
	}
	if s.TotalToolUses != 5 {
		t.Errorf("TotalToolUses = %d", s.TotalToolUses)
	}
	if s.TotalDuration != 30 {
		t.Errorf("TotalDuration = %d", s.TotalDuration)
	}
	if s.ActiveProjects != 1 {
		t.Errorf("ActiveProjects = %d", s.ActiveProjects)
	}
}

func TestCompute_MultipleEntries(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj-a", "opus", "2026-02-25", "implementation", 1000, 500, 10, 5, 30, nil),
		"s2": makeEntry("s2", "proj-a", "opus", "2026-02-26", "debugging", 2000, 800, 20, 15, 45, nil),
		"s3": makeEntry("s3", "proj-b", "sonnet", "2026-02-26", "implementation", 500, 200, 5, 3, 15, nil),
	}

	s := Compute(entries, "")

	if s.TotalSessions != 3 {
		t.Errorf("TotalSessions = %d", s.TotalSessions)
	}
	if s.TotalTokensIn != 3500 {
		t.Errorf("TotalTokensIn = %d", s.TotalTokensIn)
	}
	if s.TotalTokensOut != 1500 {
		t.Errorf("TotalTokensOut = %d", s.TotalTokensOut)
	}
	if s.TotalMessages != 35 {
		t.Errorf("TotalMessages = %d", s.TotalMessages)
	}
	if s.TotalToolUses != 23 {
		t.Errorf("TotalToolUses = %d", s.TotalToolUses)
	}
	if s.TotalDuration != 90 {
		t.Errorf("TotalDuration = %d", s.TotalDuration)
	}
	if s.ActiveProjects != 2 {
		t.Errorf("ActiveProjects = %d", s.ActiveProjects)
	}
}

func TestCompute_Averages(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "", 1000, 400, 10, 20, 60, nil),
		"s2": makeEntry("s2", "proj", "opus", "2026-02-26", "", 2000, 600, 20, 10, 30, nil),
	}

	s := Compute(entries, "")

	// 3000 / 30 = 100
	if s.AvgTokensInPerMsg != 100 {
		t.Errorf("AvgTokensInPerMsg = %f, want 100", s.AvgTokensInPerMsg)
	}
	// 1000 / 30 = 33.33
	if int(s.AvgTokensOutPerMsg*100) != 3333 {
		t.Errorf("AvgTokensOutPerMsg = %f, want ~33.33", s.AvgTokensOutPerMsg)
	}
	// 30 / 2 = 15
	if s.AvgToolsPerSession != 15 {
		t.Errorf("AvgToolsPerSession = %f, want 15", s.AvgToolsPerSession)
	}
	// 90 / 2 = 45
	if s.AvgDuration != 45 {
		t.Errorf("AvgDuration = %f, want 45", s.AvgDuration)
	}
}

func TestCompute_AveragesDivisionByZero(t *testing.T) {
	// Entry with zero messages
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "", 1000, 500, 0, 5, 30, nil),
	}

	s := Compute(entries, "")
	if s.AvgTokensInPerMsg != 0 {
		t.Errorf("AvgTokensInPerMsg = %f, want 0 (div by zero guarded)", s.AvgTokensInPerMsg)
	}
}

func TestCompute_ProjectFilter(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj-a", "opus", "2026-02-25", "", 1000, 500, 10, 5, 30, nil),
		"s2": makeEntry("s2", "proj-b", "opus", "2026-02-25", "", 2000, 800, 20, 15, 45, nil),
	}

	s := Compute(entries, "proj-a")

	if s.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", s.TotalSessions)
	}
	if s.TotalTokensIn != 1000 {
		t.Errorf("TotalTokensIn = %d, want 1000", s.TotalTokensIn)
	}
}

func TestCompute_ProjectBreakdown(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj-a", "opus", "2026-02-25", "", 1000, 500, 10, 5, 30, nil),
		"s2": makeEntry("s2", "proj-a", "opus", "2026-02-26", "", 1000, 500, 10, 5, 30, nil),
		"s3": makeEntry("s3", "proj-b", "opus", "2026-02-26", "", 500, 200, 5, 3, 15, nil),
	}

	s := Compute(entries, "")

	if len(s.Projects) != 2 {
		t.Fatalf("Projects len = %d", len(s.Projects))
	}
	// proj-a has 2 sessions, should be first
	if s.Projects[0].Name != "proj-a" {
		t.Errorf("Projects[0].Name = %q, want proj-a", s.Projects[0].Name)
	}
	if s.Projects[0].Sessions != 2 {
		t.Errorf("Projects[0].Sessions = %d, want 2", s.Projects[0].Sessions)
	}
	if s.Projects[1].Name != "proj-b" {
		t.Errorf("Projects[1].Name = %q, want proj-b", s.Projects[1].Name)
	}
}

func TestCompute_ModelBreakdown(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "", 1000, 500, 10, 5, 30, nil),
		"s2": makeEntry("s2", "proj", "opus", "2026-02-26", "", 2000, 800, 20, 15, 45, nil),
		"s3": makeEntry("s3", "proj", "sonnet", "2026-02-26", "", 500, 200, 5, 3, 15, nil),
	}

	s := Compute(entries, "")

	if len(s.Models) != 2 {
		t.Fatalf("Models len = %d", len(s.Models))
	}
	// opus has 2 sessions, should be first
	if s.Models[0].Name != "opus" {
		t.Errorf("Models[0].Name = %q, want opus", s.Models[0].Name)
	}
	if s.Models[0].Sessions != 2 {
		t.Errorf("Models[0].Sessions = %d", s.Models[0].Sessions)
	}
	// 3000 tokens / 30 messages = 100
	if s.Models[0].TokPerMsg != 100 {
		t.Errorf("Models[0].TokPerMsg = %f, want 100", s.Models[0].TokPerMsg)
	}
}

func TestCompute_TagBreakdown(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "implementation", 0, 0, 0, 0, 0, nil),
		"s2": makeEntry("s2", "proj", "opus", "2026-02-26", "implementation", 0, 0, 0, 0, 0, nil),
		"s3": makeEntry("s3", "proj", "opus", "2026-02-27", "debugging", 0, 0, 0, 0, 0, nil),
	}

	s := Compute(entries, "")

	if len(s.Tags) != 2 {
		t.Fatalf("Tags len = %d", len(s.Tags))
	}
	// implementation: 2/3 = 66%, debugging: 1/3 = 33%
	if s.Tags[0].Name != "implementation" {
		t.Errorf("Tags[0].Name = %q", s.Tags[0].Name)
	}
	if s.Tags[0].Count != 2 {
		t.Errorf("Tags[0].Count = %d", s.Tags[0].Count)
	}
	if int(s.Tags[0].Percent) != 66 {
		t.Errorf("Tags[0].Percent = %f, want ~66", s.Tags[0].Percent)
	}
}

func TestCompute_TopFiles(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	// Create 10 sessions all touching "hot.go" and only 2 touching "cold.go"
	for i := 0; i < 10; i++ {
		id := string(rune('a'+i)) + "-s"
		files := []string{"hot.go"}
		if i < 2 {
			files = append(files, "cold.go")
		}
		entries[id] = makeEntry(id, "proj", "opus", "2026-02-25", "", 0, 0, 0, 0, 0, files)
	}

	s := Compute(entries, "")

	// Global threshold is 10, hot.go has exactly 10
	if len(s.TopFiles) != 1 {
		t.Fatalf("TopFiles len = %d, want 1", len(s.TopFiles))
	}
	if s.TopFiles[0].Path != "hot.go" {
		t.Errorf("TopFiles[0].Path = %q", s.TopFiles[0].Path)
	}
}

func TestCompute_TopFilesProjectThreshold(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	for i := 0; i < 3; i++ {
		id := string(rune('a'+i)) + "-s"
		entries[id] = makeEntry(id, "proj", "opus", "2026-02-25", "", 0, 0, 0, 0, 0, []string{"hot.go"})
	}

	s := Compute(entries, "proj")

	// Project threshold is 3
	if len(s.TopFiles) != 1 {
		t.Fatalf("TopFiles len = %d, want 1", len(s.TopFiles))
	}
}

func TestCompute_MonthlyTrend(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-01-15", "", 1000, 500, 10, 5, 30, nil),
		"s2": makeEntry("s2", "proj", "opus", "2026-01-20", "", 2000, 800, 20, 15, 45, nil),
		"s3": makeEntry("s3", "proj", "opus", "2026-02-10", "", 500, 200, 5, 3, 15, nil),
	}

	s := Compute(entries, "")

	if len(s.Monthly) != 2 {
		t.Fatalf("Monthly len = %d, want 2", len(s.Monthly))
	}
	// Recent first
	if s.Monthly[0].Month != "2026-02" {
		t.Errorf("Monthly[0].Month = %q, want 2026-02", s.Monthly[0].Month)
	}
	if s.Monthly[0].Sessions != 1 {
		t.Errorf("Monthly[0].Sessions = %d", s.Monthly[0].Sessions)
	}
	if s.Monthly[1].Month != "2026-01" {
		t.Errorf("Monthly[1].Month = %q, want 2026-01", s.Monthly[1].Month)
	}
	if s.Monthly[1].Sessions != 2 {
		t.Errorf("Monthly[1].Sessions = %d", s.Monthly[1].Sessions)
	}
	if s.Monthly[1].TokensIn != 3000 {
		t.Errorf("Monthly[1].TokensIn = %d, want 3000", s.Monthly[1].TokensIn)
	}
}

func TestFormat_Overview(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "implementation", 50000, 20000, 10, 5, 90, nil),
	}
	s := Compute(entries, "")
	out := Format(s, "")

	for _, want := range []string{"Overview", "Averages", "Models", "Activity Tags", "Monthly Trend"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing section %q", want)
		}
	}
	if !strings.Contains(out, "sessions") {
		t.Error("output missing 'sessions'")
	}
}

func TestFormat_Empty(t *testing.T) {
	s := Compute(map[string]index.SessionEntry{}, "")
	out := Format(s, "")

	if !strings.Contains(out, "No sessions found") {
		t.Errorf("empty format should show 'No sessions found', got: %s", out)
	}
}

func TestFormat_EmptyWithProject(t *testing.T) {
	s := Compute(map[string]index.SessionEntry{}, "nonexistent")
	out := Format(s, "nonexistent")

	if !strings.Contains(out, "No sessions found") {
		t.Errorf("empty format should show 'No sessions found', got: %s", out)
	}
	if !strings.Contains(out, "nonexistent") {
		t.Error("should mention project name")
	}
}

func TestFormat_ProjectOmitsProjectsSection(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "opus", "2026-02-25", "", 1000, 500, 10, 5, 30, nil),
	}
	s := Compute(entries, "proj")
	out := Format(s, "proj")

	if strings.Contains(out, "\nProjects\n") {
		t.Error("Projects section should be omitted when filtering by project")
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{9999, "9,999"},
		{10000, "10.0K"},
		{52300, "52.3K"},
		{999999, "1000.0K"},
		{1000000, "1.0M"},
		{14200000, "14.2M"},
	}

	for _, tt := range tests {
		got := formatTokens(tt.input)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0m"},
		{1, "1m"},
		{59, "59m"},
		{60, "1h"},
		{90, "1h 30m"},
		{150, "2h 30m"},
		{1440, "24h"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.input)
		if got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
	}

	for _, tt := range tests {
		got := formatInt(tt.input)
		if got != tt.want {
			t.Errorf("formatInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
