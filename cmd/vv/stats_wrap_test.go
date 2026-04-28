// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
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

// scopeWrapBundleCache pins the wrap-skeleton cache base to a temp dir
// so the new "wrap skeleton cache" section in computeStatsWrap renders
// against test-local state instead of the host's real cache. The seam
// is reset at test cleanup so cross-test pollution can't leak.
func scopeWrapBundleCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wrapbundlecache.SetCacheDirForTesting(dir)
	t.Cleanup(func() { wrapbundlecache.SetCacheDirForTesting("") })
	return dir
}

// TestStatsWrap_HappyPath: write 3 dispatch lines + 2 drift lines, run
// computeStatsWrap, assert key headlines appear. Also asserts the new
// "wrap skeleton cache" section renders with the empty-cache sentinel
// since this test seeds no skeletons.
func TestStatsWrap_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)
	scopeWrapBundleCache(t)

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
		"wrap skeleton cache",
		"(no skeletons cached yet)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestStatsWrap_EmptyJsonl: empty cache dir produces "no data yet" plus
// the unconditional "wrap skeleton cache" section with the empty-cache
// sentinel.
func TestStatsWrap_EmptyJsonl(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)
	scopeWrapBundleCache(t)

	out, err := computeStatsWrap(0)
	if err != nil {
		t.Fatalf("computeStatsWrap: %v", err)
	}
	if !strings.Contains(out, "no data yet") {
		t.Errorf("expected sentinel; got: %q", out)
	}
	if !strings.Contains(out, "wrap skeleton cache") {
		t.Errorf("expected skeleton cache section header; got: %q", out)
	}
	if !strings.Contains(out, "(no skeletons cached yet)") {
		t.Errorf("expected empty-cache sentinel; got: %q", out)
	}
}

// TestStatsWrap_ReadsBothJsonlFiles asserts both sections render even
// when one of the files is empty/missing — and the new skeleton cache
// section renders unconditionally below them.
func TestStatsWrap_ReadsBothJsonlFiles(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)
	scopeWrapBundleCache(t)

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
	if !strings.Contains(out, "wrap skeleton cache") {
		t.Errorf("expected skeleton cache section header; got: %q", out)
	}
	if !strings.Contains(out, "(no skeletons cached yet)") {
		t.Errorf("expected empty-cache sentinel; got: %q", out)
	}
}

// TestStatsWrap_RendersSkeletonCache seeds skeletons for two projects via
// wrapbundlecache.Write and asserts the rendered cache section names
// both projects with the correct skeleton counts and total bytes.
func TestStatsWrap_RendersSkeletonCache(t *testing.T) {
	tmp := t.TempDir()
	setVibeVaultHome(t, tmp)
	scopeWrapBundleCache(t)

	// alpha: 2 skeletons, 6 bytes total.
	for _, iter := range []int{10, 11} {
		if _, _, err := wrapbundlecache.Write("alpha", iter, []byte("aaa")); err != nil {
			t.Fatalf("seed alpha/%d: %v", iter, err)
		}
	}
	// beta: 1 skeleton, 4 bytes total.
	if _, _, err := wrapbundlecache.Write("beta", 5, []byte("bbbb")); err != nil {
		t.Fatalf("seed beta/5: %v", err)
	}

	out, err := computeStatsWrap(0)
	if err != nil {
		t.Fatalf("computeStatsWrap: %v", err)
	}
	if !strings.Contains(out, "wrap skeleton cache") {
		t.Fatalf("missing section header; got:\n%s", out)
	}
	// Section sentinel for empty cache must NOT appear once seeded.
	if strings.Contains(out, "(no skeletons cached yet)") {
		t.Errorf("empty-cache sentinel rendered despite seeded skeletons; got:\n%s", out)
	}
	// Locate the cache section so the row asserts can't accidentally
	// match unrelated text earlier in the report.
	idx := strings.Index(out, "wrap skeleton cache")
	if idx < 0 {
		t.Fatalf("cache section header not found; got:\n%s", out)
	}
	cacheBlock := out[idx:]
	for _, want := range []string{
		"alpha",
		"beta",
	} {
		if !strings.Contains(cacheBlock, want) {
			t.Errorf("cache section missing project %q\n--- got ---\n%s", want, cacheBlock)
		}
	}
	// alpha row: 2 skeletons, 6 bytes, oldest 10, newest 11. Assert each
	// integer appears in the alpha line.
	alphaLine := findLineContaining(cacheBlock, "alpha")
	if alphaLine == "" {
		t.Fatalf("alpha row not found in cache section:\n%s", cacheBlock)
	}
	for _, tok := range []string{" 2 ", " 6 ", " 10 ", " 11"} {
		if !strings.Contains(alphaLine, tok) {
			t.Errorf("alpha row missing token %q\n--- alpha line ---\n%q", tok, alphaLine)
		}
	}
	betaLine := findLineContaining(cacheBlock, "beta")
	if betaLine == "" {
		t.Fatalf("beta row not found in cache section:\n%s", cacheBlock)
	}
	for _, tok := range []string{" 1 ", " 4 ", " 5 "} {
		if !strings.Contains(betaLine, tok) {
			t.Errorf("beta row missing token %q\n--- beta line ---\n%q", tok, betaLine)
		}
	}
}

// findLineContaining returns the first newline-bounded line in s that
// contains needle, or "" if none does. Used by the cache-section
// assertion to scope substring checks to a single row.
func findLineContaining(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
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
