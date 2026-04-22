// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package inject

import (
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/trends"
)

func TestBuild_ZedSessions(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"cc1": {SessionID: "cc1", Project: "p", Date: "2027-01-02", Iteration: 1, Title: "CC Session", Source: ""},
		"z1":  {SessionID: "z1", Project: "p", Date: "2027-01-01", Iteration: 1, Title: "Zed Session", Source: "zed"},
	}

	r := Build(entries, trends.Result{}, Opts{Project: "p"})

	if len(r.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(r.Sessions))
	}

	// CC session (newest) should have no source
	if r.Sessions[0].Source != "" {
		t.Errorf("Sessions[0].Source = %q, want empty for claude-code", r.Sessions[0].Source)
	}

	// Zed session should have source = "zed"
	if r.Sessions[1].Source != "zed" {
		t.Errorf("Sessions[1].Source = %q, want %q", r.Sessions[1].Source, "zed")
	}
}

func TestFormatMarkdown_SourceIndicator(t *testing.T) {
	r := Result{
		Project: "p",
		Sessions: []SessionItem{
			{Date: "2027-01-02", Title: "CC Session"},
			{Date: "2027-01-01", Title: "Zed Session", Source: "zed"},
		},
	}

	out := FormatMarkdown(r, []string{SectionSessions})

	if !strings.Contains(out, "Zed Session [zed]") {
		t.Errorf("markdown should contain [zed] marker, got:\n%s", out)
	}
	// CC session should NOT have a source marker
	if strings.Contains(out, "CC Session [") {
		t.Error("claude-code session should not have source marker")
	}
}
