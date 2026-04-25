package stats

import (
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/narrative"
)

func TestAnalyzeTools_NoActivities(t *testing.T) {
	result := AnalyzeTools(nil)
	if result != nil {
		t.Error("expected nil for no segments")
	}
}

func TestAnalyzeTools_AllSuccess(t *testing.T) {
	segments := []narrative.Segment{{
		Activities: []narrative.Activity{
			{Tool: "Read", Description: "Read `main.go`"},
			{Tool: "Edit", Description: "Modified `main.go`"},
			{Tool: "Write", Description: "Created `new.go`"},
		},
	}}

	result := AnalyzeTools(segments)
	// No errors or struggles → should return nil (not interesting)
	if result != nil {
		t.Error("expected nil when no errors or struggles")
	}
}

func TestAnalyzeTools_WithErrors(t *testing.T) {
	segments := []narrative.Segment{{
		Activities: []narrative.Activity{
			{Tool: "Bash", Description: "Tests", IsError: true},
			{Tool: "Bash", Description: "Tests", IsError: true, Recovered: true},
			{Tool: "Bash", Description: "Tests"},
			{Tool: "Read", Description: "Read `main.go`"},
		},
	}}

	result := AnalyzeTools(segments)
	if result == nil {
		t.Fatal("expected non-nil result with errors")
	}

	// Find Bash metric
	var bash *ToolMetric
	for i := range result.Tools {
		if result.Tools[i].Name == "Bash" {
			bash = &result.Tools[i]
			break
		}
	}
	if bash == nil {
		t.Fatal("expected Bash metric")
	}
	if bash.Uses != 3 {
		t.Errorf("Bash.Uses = %d, want 3", bash.Uses)
	}
	if bash.Errors != 2 {
		t.Errorf("Bash.Errors = %d, want 2", bash.Errors)
	}
	if bash.Recoveries != 1 {
		t.Errorf("Bash.Recoveries = %d, want 1", bash.Recoveries)
	}
	// Success rate: 1/3 = 33%
	if int(bash.SuccessRate) != 33 {
		t.Errorf("Bash.SuccessRate = %.0f%%, want 33%%", bash.SuccessRate)
	}
}

func TestAnalyzeTools_StruggleDetection(t *testing.T) {
	segments := []narrative.Segment{{
		Activities: []narrative.Activity{
			{Tool: "Read", Description: "Read `config.go`"},
			{Tool: "Edit", Description: "Modified `config.go`", IsError: true},
			{Tool: "Read", Description: "Read `config.go`"},
			{Tool: "Edit", Description: "Modified `config.go`", IsError: true},
			{Tool: "Read", Description: "Read `config.go`"},
			{Tool: "Edit", Description: "Modified `config.go`"},
		},
	}}

	result := AnalyzeTools(segments)
	if result == nil {
		t.Fatal("expected non-nil result with struggles")
	}

	if len(result.Struggles) != 1 {
		t.Fatalf("expected 1 struggle, got %d", len(result.Struggles))
	}
	if result.Struggles[0].File != "config.go" {
		t.Errorf("struggle file = %q, want config.go", result.Struggles[0].File)
	}
	if result.Struggles[0].Cycles != 3 {
		t.Errorf("struggle cycles = %d, want 3", result.Struggles[0].Cycles)
	}
}

func TestAnalyzeTools_NoStruggleBelowThreshold(t *testing.T) {
	segments := []narrative.Segment{{
		Activities: []narrative.Activity{
			{Tool: "Read", Description: "Read `config.go`"},
			{Tool: "Edit", Description: "Modified `config.go`"},
			{Tool: "Read", Description: "Read `config.go`"},
			{Tool: "Edit", Description: "Modified `config.go`", IsError: true},
		},
	}}

	result := AnalyzeTools(segments)
	if result == nil {
		t.Fatal("expected non-nil (has errors)")
	}
	if len(result.Struggles) != 0 {
		t.Errorf("expected 0 struggles (only 2 cycles), got %d", len(result.Struggles))
	}
}

func TestRenderToolEffectiveness(t *testing.T) {
	te := &ToolEffectiveness{
		Tools: []ToolMetric{
			{Name: "Bash", Uses: 10, Errors: 3, SuccessRate: 70},
		},
		Struggles: []StrugglePattern{
			{File: "config.go", Cycles: 4},
		},
	}

	rendered := RenderToolEffectiveness(te)
	if rendered == "" {
		t.Fatal("expected non-empty render")
	}

	// Check it contains expected content
	for _, want := range []string{"Bash", "10", "3", "70%", "config.go", "4 edit cycles"} {
		if !contains(rendered, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestRenderToolEffectiveness_Nil(t *testing.T) {
	if got := RenderToolEffectiveness(nil); got != "" {
		t.Errorf("nil should render empty, got %q", got)
	}
}

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Created `internal/auth/handler.go`", "internal/auth/handler.go"},
		{"Modified `main.go`", "main.go"},
		{"Read `config.go`", "config.go"},
		{"no backticks here", ""},
		{"single `backtick", ""},
	}

	for _, tt := range tests {
		got := extractFilePath(tt.input)
		if got != tt.want {
			t.Errorf("extractFilePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCountEditCycles(t *testing.T) {
	tests := []struct {
		ops  []string
		want int
	}{
		{[]string{"Read", "Edit"}, 1},
		{[]string{"Read", "Write"}, 1},
		{[]string{"Read", "Edit", "Read", "Edit", "Read", "Edit"}, 3},
		{[]string{"Read", "Read", "Edit"}, 1},
		{[]string{"Edit", "Read"}, 0},
		{nil, 0},
	}

	for _, tt := range tests {
		got := countEditCycles(tt.ops)
		if got != tt.want {
			t.Errorf("countEditCycles(%v) = %d, want %d", tt.ops, got, tt.want)
		}
	}
}

// suppress unused import warning
var _ = time.Now

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
