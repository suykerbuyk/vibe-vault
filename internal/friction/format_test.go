package friction

import (
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/index"
)

func TestComputeProjectFriction_Empty(t *testing.T) {
	entries := map[string]index.SessionEntry{}
	result := ComputeProjectFriction(entries, "")
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestComputeProjectFriction_SingleProject(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {Project: "proj", FrictionScore: 30, Corrections: 2},
		"s2": {Project: "proj", FrictionScore: 50, Corrections: 4},
	}
	result := ComputeProjectFriction(entries, "")
	if len(result) != 1 {
		t.Fatalf("expected 1 project, got %d", len(result))
	}
	if result[0].Sessions != 2 {
		t.Errorf("sessions = %d, want 2", result[0].Sessions)
	}
	if result[0].TotalCorrections != 6 {
		t.Errorf("corrections = %d, want 6", result[0].TotalCorrections)
	}
	if result[0].MaxScore != 50 {
		t.Errorf("max = %d, want 50", result[0].MaxScore)
	}
	if result[0].HighFriction != 1 {
		t.Errorf("high friction = %d, want 1", result[0].HighFriction)
	}
}

func TestComputeProjectFriction_ProjectFilter(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {Project: "proj-a", FrictionScore: 30, Corrections: 2},
		"s2": {Project: "proj-b", FrictionScore: 50, Corrections: 4},
	}
	result := ComputeProjectFriction(entries, "proj-a")
	if len(result) != 1 {
		t.Fatalf("expected 1 project, got %d", len(result))
	}
	if result[0].Project != "proj-a" {
		t.Errorf("project = %q, want proj-a", result[0].Project)
	}
}

func TestComputeProjectFriction_SkipsZero(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": {Project: "proj", FrictionScore: 0, Corrections: 0},
	}
	result := ComputeProjectFriction(entries, "")
	if len(result) != 0 {
		t.Errorf("expected 0 results for zero-friction entries, got %d", len(result))
	}
}

func TestFormat_Empty(t *testing.T) {
	out := Format(nil, 0, "")
	if !strings.Contains(out, "No friction data") {
		t.Error("expected no data message")
	}
}

func TestFormat_SingleProject(t *testing.T) {
	projects := []ProjectFriction{
		{Project: "myproject", Sessions: 3, AvgScore: 35, MaxScore: 50, TotalCorrections: 8, HighFriction: 1},
	}
	out := Format(projects, 10, "")
	if !strings.Contains(out, "myproject") {
		t.Error("missing project name")
	}
	if !strings.Contains(out, "3 / 10") {
		t.Error("missing session count")
	}
	if !strings.Contains(out, "corrections") {
		t.Error("missing corrections")
	}
}

func TestFormat_ProjectFilter(t *testing.T) {
	projects := []ProjectFriction{
		{Project: "myproject", Sessions: 2, AvgScore: 25, MaxScore: 30, TotalCorrections: 3},
	}
	out := Format(projects, 5, "myproject")
	if !strings.Contains(out, "Friction Analysis: myproject") {
		t.Error("missing project filter in header")
	}
}

func TestFormat_MultiProject(t *testing.T) {
	projects := []ProjectFriction{
		{Project: "proj-a", Sessions: 3, AvgScore: 45, MaxScore: 60, TotalCorrections: 10, HighFriction: 2},
		{Project: "proj-b", Sessions: 2, AvgScore: 20, MaxScore: 30, TotalCorrections: 3},
	}
	out := Format(projects, 10, "")
	if !strings.Contains(out, "proj-a") {
		t.Error("missing proj-a")
	}
	if !strings.Contains(out, "proj-b") {
		t.Error("missing proj-b")
	}
	if !strings.Contains(out, "Projects") {
		t.Error("missing Projects section")
	}
}
