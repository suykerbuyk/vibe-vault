// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package wrapmetrics

import (
	"strings"
	"testing"
)

// TestComputeWrapStats_HappyPath: 6 dispatches across 3 iterations, two
// tiers, two escalation reasons; assert all aggregate fields.
func TestComputeWrapStats_HappyPath(t *testing.T) {
	dispatch := []DispatchLine{
		// Iter 1: sonnet ok, single tier.
		mkDL(1, "sonnet", "ok", "", 1000),
		// Iter 2: sonnet escalates, opus ok.
		mkDL(2, "sonnet", "escalate", "semantic_presence_failure", 800),
		mkDL(2, "opus", "ok", "", 2200),
		// Iter 3: sonnet escalates twice.
		mkDL(3, "sonnet", "escalate", "multi_match_ambiguity", 700),
		mkDL(3, "sonnet", "escalate", "semantic_presence_failure", 900),
		mkDL(3, "opus", "ok", "", 2400),
	}

	s := ComputeWrapStats(dispatch, nil)

	if s.TotalDispatches != 6 {
		t.Errorf("TotalDispatches = %d, want 6", s.TotalDispatches)
	}
	if s.IterationCount != 3 {
		t.Errorf("IterationCount = %d, want 3", s.IterationCount)
	}
	if s.EscalateCount != 3 {
		t.Errorf("EscalateCount = %d, want 3", s.EscalateCount)
	}
	if s.EscalationRate < 0.49 || s.EscalationRate > 0.51 {
		t.Errorf("EscalationRate = %.2f, want ~0.50", s.EscalationRate)
	}
	if s.MedianDurationByTier["sonnet"].Count != 4 {
		t.Errorf("sonnet count = %d, want 4", s.MedianDurationByTier["sonnet"].Count)
	}
	if s.MedianDurationByTier["opus"].Count != 2 {
		t.Errorf("opus count = %d, want 2", s.MedianDurationByTier["opus"].Count)
	}
	// Top reason should be semantic_presence_failure (n=2).
	if len(s.TopEscalateReasons) == 0 || s.TopEscalateReasons[0].Reason != "semantic_presence_failure" {
		t.Errorf("top reason = %+v, want semantic_presence_failure", s.TopEscalateReasons)
	}
	if s.TopEscalateReasons[0].Count != 2 {
		t.Errorf("top reason count = %d, want 2", s.TopEscalateReasons[0].Count)
	}
}

// TestComputeWrapStats_Empty handles no input.
func TestComputeWrapStats_Empty(t *testing.T) {
	s := ComputeWrapStats(nil, nil)
	if s.TotalDispatches != 0 {
		t.Errorf("TotalDispatches = %d, want 0", s.TotalDispatches)
	}
	if s.EscalationRate != 0 {
		t.Errorf("EscalationRate = %.2f, want 0", s.EscalationRate)
	}
}

// TestFormatWrapStats_HappyPath asserts key headlines render.
func TestFormatWrapStats_HappyPath(t *testing.T) {
	dispatch := []DispatchLine{
		mkDL(1, "sonnet", "ok", "", 1000),
		mkDL(2, "sonnet", "escalate", "semantic_presence_failure", 800),
		mkDL(2, "opus", "ok", "", 2200),
	}
	s := ComputeWrapStats(dispatch, nil)
	text := FormatWrapStats(s)

	for _, want := range []string{
		"wrap dispatch stats",
		"median duration per tier",
		"sonnet:",
		"opus:",
		"escalation rate",
		"semantic_presence_failure",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, text)
		}
	}
}

// TestFormatWrapStats_EmptyEverything: empty inputs produce the
// "no data yet" sentinel string the test plan asks for.
func TestFormatWrapStats_EmptyEverything(t *testing.T) {
	s := ComputeWrapStats(nil, nil)
	out := FormatWrapStats(s)
	if !strings.Contains(out, "no data yet") {
		t.Errorf("expected sentinel; got: %q", out)
	}
}

// TestFormatWrapStats_DriftSection asserts the drift trends block
// appears alongside dispatch stats when both jsonl files have data.
func TestFormatWrapStats_DriftSection(t *testing.T) {
	dispatch := []DispatchLine{
		mkDL(1, "sonnet", "ok", "", 1000),
	}
	drift := []Line{
		{Field: "iteration_narrative", DriftBytes: 12},
		{Field: "iteration_narrative", DriftBytes: 8},
		{Field: "commit_msg", DriftBytes: 4},
	}
	s := ComputeWrapStats(dispatch, drift)
	out := FormatWrapStats(s)

	for _, want := range []string{
		"wrap dispatch stats",
		"wrap drift trends",
		"iteration_narrative:",
		"commit_msg:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestFormatWrapStats_DispatchEmptyDriftPresent: only drift data; the
// dispatch section should still emit a sentinel rather than panic.
func TestFormatWrapStats_DispatchEmptyDriftPresent(t *testing.T) {
	drift := []Line{{Field: "iteration_narrative", DriftBytes: 5}}
	s := ComputeWrapStats(nil, drift)
	out := FormatWrapStats(s)
	if !strings.Contains(out, "no dispatch data yet") {
		t.Errorf("expected dispatch sentinel; got: %q", out)
	}
	if !strings.Contains(out, "wrap drift trends") {
		t.Errorf("expected drift section; got: %q", out)
	}
}

// mkDL constructs a one-tier-attempt DispatchLine for table tests.
func mkDL(iter int, tier, outcome, reason string, durMs int64) DispatchLine {
	return DispatchLine{
		Iter: iter,
		TS:   "2026-04-25T17:00:00Z",
		TierAttempts: []TierAttempt{{
			Tier:           tier,
			ProviderModel:  "anthropic:claude-" + tier + "-stub",
			DurationMs:     durMs,
			Outcome:        outcome,
			EscalateReason: reason,
		}},
		TotalDurationMs: durMs,
	}
}
