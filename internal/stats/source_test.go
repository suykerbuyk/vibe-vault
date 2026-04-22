// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package stats

import (
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

func TestCompute_SourceBreakdown(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"cc1": {SessionID: "cc1", Project: "proj", Source: "", TokensIn: 1000, Duration: 30},
		"cc2": {SessionID: "cc2", Project: "proj", Source: "", TokensIn: 2000, Duration: 45},
		"z1":  {SessionID: "z1", Project: "proj", Source: "zed", TokensIn: 500, Duration: 15},
	}

	s := Compute(entries, "")

	if len(s.Sources) != 2 {
		t.Fatalf("Sources len = %d, want 2", len(s.Sources))
	}

	// claude-code has 2 sessions, should be first
	if s.Sources[0].Name != "claude-code" {
		t.Errorf("Sources[0].Name = %q, want claude-code", s.Sources[0].Name)
	}
	if s.Sources[0].Sessions != 2 {
		t.Errorf("Sources[0].Sessions = %d, want 2", s.Sources[0].Sessions)
	}
	if s.Sources[0].TokensIn != 3000 {
		t.Errorf("Sources[0].TokensIn = %d, want 3000", s.Sources[0].TokensIn)
	}
	if s.Sources[0].Duration != 75 {
		t.Errorf("Sources[0].Duration = %d, want 75", s.Sources[0].Duration)
	}

	if s.Sources[1].Name != "zed" {
		t.Errorf("Sources[1].Name = %q, want zed", s.Sources[1].Name)
	}
	if s.Sources[1].Sessions != 1 {
		t.Errorf("Sources[1].Sessions = %d, want 1", s.Sources[1].Sessions)
	}
}

func TestCompute_SourceBreakdown_SingleSource(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"cc1": {SessionID: "cc1", Project: "proj"},
		"cc2": {SessionID: "cc2", Project: "proj"},
	}

	s := Compute(entries, "")

	if len(s.Sources) != 1 {
		t.Fatalf("Sources len = %d, want 1", len(s.Sources))
	}
	if s.Sources[0].Name != "claude-code" {
		t.Errorf("Sources[0].Name = %q, want claude-code", s.Sources[0].Name)
	}
}

func TestFormat_SourcesSection(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"cc1": {SessionID: "cc1", Project: "proj", Model: "opus", Date: "2026-03-01", Source: "", TokensIn: 1000, Duration: 30, Messages: 5},
		"z1":  {SessionID: "z1", Project: "proj", Model: "sonnet", Date: "2026-03-01", Source: "zed", TokensIn: 500, Duration: 15, Messages: 3},
	}

	s := Compute(entries, "")
	out := Format(s, "")

	if !strings.Contains(out, "Sources") {
		t.Error("output should contain Sources section when multiple sources")
	}
	if !strings.Contains(out, "claude-code") {
		t.Error("output should contain claude-code in Sources section")
	}
	if !strings.Contains(out, "zed") {
		t.Error("output should contain zed in Sources section")
	}
}

func TestFormat_SourcesSection_OmittedSingleSource(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"cc1": {SessionID: "cc1", Project: "proj", Model: "opus", Date: "2026-03-01", Messages: 5},
	}

	s := Compute(entries, "")
	out := Format(s, "")

	if strings.Contains(out, "\nSources\n") {
		t.Error("Sources section should be omitted with single source")
	}
}

func TestFormat_SourcesSection_OmittedWithProjectFilter(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"cc1": {SessionID: "cc1", Project: "proj", Model: "opus", Date: "2026-03-01", Source: "", Messages: 5},
		"z1":  {SessionID: "z1", Project: "proj", Model: "sonnet", Date: "2026-03-01", Source: "zed", Messages: 3},
	}

	s := Compute(entries, "proj")
	out := Format(s, "proj")

	if strings.Contains(out, "\nSources\n") {
		t.Error("Sources section should be omitted when filtering by project")
	}
}
