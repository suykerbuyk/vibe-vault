// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/wrapmetrics"
)

// setVibeVaultHome pins $VIBE_VAULT_HOME so wrapmetrics.CacheDir resolves
// to a temp dir for the test duration.
func setVibeVaultHome(t *testing.T, dir string) {
	t.Helper()
	orig, had := os.LookupEnv("VIBE_VAULT_HOME")
	t.Setenv("VIBE_VAULT_HOME", dir)
	t.Cleanup(func() {
		if had {
			os.Setenv("VIBE_VAULT_HOME", orig)
		} else {
			os.Unsetenv("VIBE_VAULT_HOME")
		}
	})
}

// TestStatsWrap_HappyPath: write 3 dispatch lines + 2 drift lines, run
// computeStatsWrap, assert key headlines appear.
func TestStatsWrap_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)

	for _, dl := range []wrapmetrics.DispatchLine{
		mkLine(1, "sonnet", "ok", "", 1000),
		mkLine(2, "sonnet", "escalate", "semantic_presence_failure", 800),
		mkLine(2, "opus", "ok", "", 2200),
	} {
		if err := wrapmetrics.WriteDispatchLine(dl); err != nil {
			t.Fatalf("write dispatch: %v", err)
		}
	}

	if err := wrapmetrics.AppendBundleLines("h", "u", "/cwd", "proj", 1, []wrapmetrics.Line{
		{Field: "iteration_narrative", DriftBytes: 12},
		{Field: "iteration_narrative", DriftBytes: 8},
	}); err != nil {
		t.Fatalf("append drift: %v", err)
	}

	out, err := computeStatsWrap(0)
	if err != nil {
		t.Fatalf("computeStatsWrap: %v", err)
	}
	for _, want := range []string{
		"wrap dispatch stats",
		"sonnet:",
		"opus:",
		"escalation rate",
		"semantic_presence_failure",
		"wrap drift trends",
		"iteration_narrative:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestStatsWrap_EmptyJsonl: empty cache dir produces "no data yet".
func TestStatsWrap_EmptyJsonl(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)

	out, err := computeStatsWrap(0)
	if err != nil {
		t.Fatalf("computeStatsWrap: %v", err)
	}
	if !strings.Contains(out, "no data yet") {
		t.Errorf("expected sentinel; got: %q", out)
	}
}

// TestStatsWrap_ReadsBothJsonlFiles asserts both sections render even
// when one of the files is empty/missing.
func TestStatsWrap_ReadsBothJsonlFiles(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)

	// Drift only: dispatch section should report "no dispatch data yet"
	// while drift section appears.
	if err := wrapmetrics.AppendBundleLines("h", "u", "/cwd", "proj", 1, []wrapmetrics.Line{
		{Field: "commit_msg", DriftBytes: 4},
	}); err != nil {
		t.Fatalf("append drift: %v", err)
	}

	out, err := computeStatsWrap(0)
	if err != nil {
		t.Fatalf("computeStatsWrap: %v", err)
	}
	if !strings.Contains(out, "no dispatch data yet") {
		t.Errorf("expected dispatch sentinel; got: %q", out)
	}
	if !strings.Contains(out, "wrap drift trends") {
		t.Errorf("expected drift section; got: %q", out)
	}
}

// mkLine is a tiny test fixture for dispatch lines.
func mkLine(iter int, tier, outcome, reason string, durMs int64) wrapmetrics.DispatchLine {
	return wrapmetrics.DispatchLine{
		Iter: iter,
		TS:   "2026-04-25T17:00:00Z",
		TierAttempts: []wrapmetrics.TierAttempt{{
			Tier:           tier,
			ProviderModel:  "anthropic:claude-" + tier + "-stub",
			DurationMs:     durMs,
			Outcome:        outcome,
			EscalateReason: reason,
		}},
		TotalDurationMs: durMs,
	}
}
