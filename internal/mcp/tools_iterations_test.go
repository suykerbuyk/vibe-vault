// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

const fixtureIterations = `# testproj — Project History

## Iteration Narratives

### Iteration 1 — First thing (2026-01-01)

Narrative for iteration one.

Second paragraph.

### Iteration 2 — Second thing (2026-01-02)

Narrative for iteration two.

### Iteration 3 — Third thing with — an em dash (2026-01-03)

Narrative for iteration three.

### Iteration 4 — Fourth (2026-01-04)

Narrative for iteration four.

### Iteration 5 — Fifth (2026-01-05)

Narrative for iteration five.
`

type iterationsResponse struct {
	Project    string      `json:"project"`
	Total      int         `json:"total"`
	Returned   int         `json:"returned"`
	Iterations []Iteration `json:"iterations"`
}

func parseIterationsResponse(t *testing.T, raw string) iterationsResponse {
	t.Helper()
	var resp iterationsResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, raw)
	}
	return resp
}

func TestGetIterationsBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})

	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	resp := parseIterationsResponse(t, result)
	if resp.Total != 5 {
		t.Errorf("total = %d, want 5", resp.Total)
	}
	if resp.Returned != 5 {
		t.Errorf("returned = %d, want 5", resp.Returned)
	}
	if len(resp.Iterations) != 5 {
		t.Fatalf("iterations len = %d, want 5", len(resp.Iterations))
	}
	if resp.Iterations[0].Number != 5 {
		t.Errorf("newest-first: first iteration = %d, want 5", resp.Iterations[0].Number)
	}
	if resp.Iterations[4].Number != 1 {
		t.Errorf("newest-first: last iteration = %d, want 1", resp.Iterations[4].Number)
	}
	if resp.Iterations[0].Narrative != "" {
		t.Errorf("default format should omit narrative; got %q", resp.Iterations[0].Narrative)
	}
}

func TestGetIterationsTableFormatOmitsNarrativeInJSON(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"table"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	// Verify at the serialized-JSON level that "narrative" key is absent for
	// table format. omitempty + empty string should suppress it entirely —
	// that's the whole point of the compact format.
	if strings.Contains(result, `"narrative"`) {
		t.Errorf("table format should not emit \"narrative\" key; got: %s", result)
	}
}

func TestGetIterationsFullFormat(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"full","limit":1}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	if len(resp.Iterations) != 1 {
		t.Fatalf("expected 1 iteration, got %d", len(resp.Iterations))
	}
	if !strings.Contains(resp.Iterations[0].Narrative, "Narrative for iteration five") {
		t.Errorf("full format should include narrative body; got %q", resp.Iterations[0].Narrative)
	}
}

func TestGetIterationsLimit(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","limit":2}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	if resp.Total != 5 {
		t.Errorf("total = %d, want 5", resp.Total)
	}
	if resp.Returned != 2 {
		t.Errorf("returned = %d, want 2", resp.Returned)
	}
	if resp.Iterations[0].Number != 5 || resp.Iterations[1].Number != 4 {
		t.Errorf("limit=2 should return iters 5 then 4; got %d, %d",
			resp.Iterations[0].Number, resp.Iterations[1].Number)
	}
}

func TestGetIterationsSinceIteration(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","since_iteration":3}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	// total is the pre-filter count
	if resp.Total != 5 {
		t.Errorf("total = %d, want 5 (pre-filter)", resp.Total)
	}
	if resp.Returned != 3 {
		t.Errorf("returned = %d, want 3 (iters 3,4,5)", resp.Returned)
	}
	nums := []int{resp.Iterations[0].Number, resp.Iterations[1].Number, resp.Iterations[2].Number}
	if nums[0] != 5 || nums[1] != 4 || nums[2] != 3 {
		t.Errorf("since_iteration=3 newest-first should yield 5,4,3; got %v", nums)
	}
}

func TestGetIterationsSinceIterationWithLimit(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","since_iteration":2,"limit":2}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	if resp.Returned != 2 {
		t.Errorf("returned = %d, want 2 (since=2 yields 2,3,4,5 then limit=2 caps to 5,4)", resp.Returned)
	}
	if resp.Iterations[0].Number != 5 || resp.Iterations[1].Number != 4 {
		t.Errorf("expected 5,4; got %d,%d", resp.Iterations[0].Number, resp.Iterations[1].Number)
	}
}

func TestGetIterationsEmDashInTitle(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","since_iteration":3,"limit":1}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	// Iter 3 title is "Third thing with — an em dash" — the em dash inside
	// the title must not be swallowed by the regex separator.
	// But since=3,limit=1 returns iter 5 (newest-first of 3,4,5). Re-query for iter 3:
	result, err = tool.Handler(json.RawMessage(`{"project":"testproj","since_iteration":3,"limit":3}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp = parseIterationsResponse(t, result)
	var iter3 *Iteration
	for i := range resp.Iterations {
		if resp.Iterations[i].Number == 3 {
			iter3 = &resp.Iterations[i]
			break
		}
	}
	if iter3 == nil {
		t.Fatal("iter 3 not found in response")
	}
	if iter3.Title != "Third thing with — an em dash" {
		t.Errorf("iter 3 title = %q, want %q", iter3.Title, "Third thing with — an em dash")
	}
	if iter3.Date != "2026-01-03" {
		t.Errorf("iter 3 date = %q, want 2026-01-03", iter3.Date)
	}
}

func TestGetIterationsMissing(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewGetIterationsTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"noproject"}`))
	if err == nil {
		t.Fatal("expected error for missing iterations.md")
	}
	if !strings.Contains(err.Error(), "iterations.md not found") {
		t.Errorf("error = %v, want 'iterations.md not found'", err)
	}
}

func TestGetIterationsPathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewGetIterationsTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../../etc"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("error = %v, want 'invalid project name'", err)
	}
}

func TestGetIterationsInvalidFormat(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"csv"}`))
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("error = %v, want 'invalid format'", err)
	}
}

func TestGetIterationsInvalidLimit(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","limit":0}`))
	if err == nil {
		t.Fatal("expected error for limit=0")
	}
	if !strings.Contains(err.Error(), "limit must be >= 1") {
		t.Errorf("error = %v, want 'limit must be >= 1'", err)
	}
}

func TestGetIterationsMalformedHeadingSkipped(t *testing.T) {
	// A malformed heading (no date, or wrong level) should not panic and
	// should not be included in the output.
	content := `# Project History

### Iteration 1 — Real iter (2026-01-01)
Body.

### Iteration 2 — Missing date
Body.

## Iteration 3 — Wrong heading level (2026-01-03)
Body.

### Iteration 4 — Real again (2026-01-04)
Body.
`
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"full"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2 (iter 1 and iter 4; malformed skipped)", resp.Total)
	}
	nums := make(map[int]bool)
	for _, it := range resp.Iterations {
		nums[it.Number] = true
	}
	if !nums[1] || !nums[4] {
		t.Errorf("expected iters 1 and 4 present; got %v", nums)
	}
	if nums[2] || nums[3] {
		t.Errorf("expected iters 2,3 skipped (malformed); got %v", nums)
	}
}

// TestParseIterationsStripsProvenanceTrailer is a regression gate on the
// Phase 3 provenanceTrailerRE strip: the HTML-comment provenance trailer
// vv_append_iteration writes into iterations.md must not appear in the
// narrative returned by parseIterations. Without this the round-trip leaks
// forensic metadata back into agent prompts.
func TestParseIterationsStripsProvenanceTrailer(t *testing.T) {
	content := "### Iteration 42 — Test (2026-04-24)\n\nBody text.\n\n<!-- recorded: host=foo user=bar -->\n"
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	if got[0].Narrative != "Body text." {
		t.Errorf("Narrative = %q, want %q", got[0].Narrative, "Body text.")
	}
	if got[0].Number != 42 {
		t.Errorf("Number = %d, want 42", got[0].Number)
	}
	if got[0].Title != "Test" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Test")
	}
}

// TestParseIterationsWithoutTrailer pins the no-op behaviour: an iteration
// block that has never been stamped (legacy content) must round-trip
// unchanged. If this breaks the strip regex has over-reached.
func TestParseIterationsWithoutTrailer(t *testing.T) {
	content := "### Iteration 7 — Legacy (2026-01-07)\n\nPlain body.\n\nSecond paragraph.\n"
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	want := "Plain body.\n\nSecond paragraph."
	if got[0].Narrative != want {
		t.Errorf("Narrative = %q, want %q", got[0].Narrative, want)
	}
}

// TestParseIterationsMalformedTrailer verifies the strip regex degrades
// gracefully on malformed / mispositioned trailers:
//   - a trailer-shaped comment in the middle of narrative survives
//     (regex is anchored to \z),
//   - an unterminated comment at the end does not match and is left in
//     place — it may be ugly, but it must not panic.
func TestParseIterationsMalformedTrailer(t *testing.T) {
	t.Run("trailer_mid_narrative_not_stripped", func(t *testing.T) {
		content := "### Iteration 8 — Mid (2026-01-08)\n\nBefore.\n\n<!-- recorded: host=x user=y -->\n\nAfter.\n"
		got := parseIterations(content)
		if len(got) != 1 {
			t.Fatalf("parseIterations returned %d entries, want 1", len(got))
		}
		if !strings.Contains(got[0].Narrative, "<!-- recorded:") {
			t.Errorf("mid-narrative trailer should remain; got %q", got[0].Narrative)
		}
		if !strings.Contains(got[0].Narrative, "After.") {
			t.Errorf("content after mid-narrative trailer should remain; got %q", got[0].Narrative)
		}
	})

	t.Run("unterminated_trailer_survives_without_panic", func(t *testing.T) {
		// Defence in depth: the regex must not hang or panic on an
		// unterminated comment. parseIterations returning normally is the
		// assertion.
		content := "### Iteration 9 — Broken (2026-01-09)\n\nBody.\n\n<!-- recorded: host=foo user=bar\n"
		got := parseIterations(content)
		if len(got) != 1 {
			t.Fatalf("parseIterations returned %d entries, want 1", len(got))
		}
		// Unterminated comment doesn't match the anchored strip regex, so the
		// fragment is retained inside the narrative. That's the reasonable
		// fallback; we just assert no panic and a populated entry.
		if got[0].Narrative == "" {
			t.Errorf("narrative should contain body text on malformed trailer; got empty")
		}
	})
}

// TestParseIterationsStripsFourTokenTrailer is the Phase 6.4 regression gate
// for the extended trailer shape introduced in Phase 6.1: the strip regex
// must handle the four-token "host=H user=U cwd=C origin=P" form without
// leaking any token back into the narrative.
func TestParseIterationsStripsFourTokenTrailer(t *testing.T) {
	content := "### Iteration 100 — Four token (2026-04-24)\n\nBody text for four-token trailer.\n\n<!-- recorded: host=H user=U cwd=/some/cwd origin=myproj -->\n"
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	if got[0].Narrative != "Body text for four-token trailer." {
		t.Errorf("Narrative = %q, want %q", got[0].Narrative, "Body text for four-token trailer.")
	}
	for _, leak := range []string{"cwd=", "origin=", "<!-- recorded:", "host=", "user=", "/some/cwd", "myproj"} {
		if strings.Contains(got[0].Narrative, leak) {
			t.Errorf("Narrative leaked %q: %q", leak, got[0].Narrative)
		}
	}
}

// TestParseIterationsStripsPartialTrailer exercises the space-separated
// conditional-token format. Phase 6.1's provenanceTrailer omits individual
// tokens when their value is empty, so real trailers may carry any subset
// of host/user/cwd/origin. The strip regex is value-agnostic ([^\n]*) —
// this test pins that it stays value-agnostic regardless of token subset.
func TestParseIterationsStripsPartialTrailer(t *testing.T) {
	cases := []struct {
		name    string
		trailer string
	}{
		{"only_cwd_and_origin", "<!-- recorded: cwd=/only/cwd origin=onlyorigin -->"},
		{"only_host_and_cwd", "<!-- recorded: host=H cwd=/host/cwd -->"},
		{"only_origin", "<!-- recorded: origin=solo -->"},
		{"only_cwd", "<!-- recorded: cwd=/only -->"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := "### Iteration 101 — Partial (2026-04-24)\n\nPartial body.\n\n" + tc.trailer + "\n"
			got := parseIterations(content)
			if len(got) != 1 {
				t.Fatalf("parseIterations returned %d entries, want 1", len(got))
			}
			if got[0].Narrative != "Partial body." {
				t.Errorf("Narrative = %q, want %q", got[0].Narrative, "Partial body.")
			}
			for _, leak := range []string{"cwd=", "origin=", "host=", "user=", "<!-- recorded:"} {
				if strings.Contains(got[0].Narrative, leak) {
					t.Errorf("%s: Narrative leaked %q: %q", tc.name, leak, got[0].Narrative)
				}
			}
		})
	}
}

// TestParseIterationsStripsTrailerWithMultilineBody regresses the end-of-string
// anchor: a multi-paragraph narrative preceding the four-token trailer must be
// preserved verbatim while only the trailer is excised. If the strip regex
// ever drops its \z anchor, interior paragraphs (which contain no "<!-- recorded:"
// fragments) would still be safe — but this test adds belt-and-suspenders
// coverage against future tightening that might swallow the final paragraph.
func TestParseIterationsStripsTrailerWithMultilineBody(t *testing.T) {
	content := "### Iteration 102 — Multiline (2026-04-24)\n\nFirst paragraph of body.\n\nSecond paragraph, with detail.\n\nThird paragraph — final thought.\n\n<!-- recorded: host=H user=U cwd=/multi/cwd origin=multiproj -->\n"
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	wantNarr := "First paragraph of body.\n\nSecond paragraph, with detail.\n\nThird paragraph — final thought."
	if got[0].Narrative != wantNarr {
		t.Errorf("Narrative = %q, want %q", got[0].Narrative, wantNarr)
	}
	for _, leak := range []string{"cwd=", "origin=", "<!-- recorded:", "/multi/cwd", "multiproj"} {
		if strings.Contains(got[0].Narrative, leak) {
			t.Errorf("Narrative leaked %q: %q", leak, got[0].Narrative)
		}
	}
}

// --- Phase A: parser strip + struct delta tests (6) ---

// TestParseIterationsStripsFrontmatter is the v2-C1 regression lock —
// front-matter must NOT leak into Iteration.Narrative or any narrative-
// derived consumer (resume.md project-history-tail in particular).
func TestParseIterationsStripsFrontmatter(t *testing.T) {
	content := `### Iteration 50 — Front-matter iter (2026-04-30)

---
shape: planning
summary: Filed plan v3 with 3 critical fixes.
---

The body opens here. No front-matter leakage allowed.

Second paragraph.
`
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	for _, leak := range []string{"---", "shape:", "summary:", "Filed plan v3"} {
		if strings.Contains(got[0].Narrative, leak) {
			t.Errorf("Narrative leaked %q: %q", leak, got[0].Narrative)
		}
	}
	if !strings.Contains(got[0].Narrative, "The body opens here.") {
		t.Errorf("Narrative missing body: %q", got[0].Narrative)
	}
	if !strings.Contains(got[0].Narrative, "Second paragraph.") {
		t.Errorf("Narrative missing second paragraph: %q", got[0].Narrative)
	}
}

// TestParseIterationsPopulatesFrontmatter pins that the parser exposes
// the structured fields via Iteration.Frontmatter.
func TestParseIterationsPopulatesFrontmatter(t *testing.T) {
	content := `### Iteration 51 — Iter with FM (2026-04-30)

---
shape: fresh-feature
summary: Two-tier vault staging shipped.
---

Body content here.
`
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	if got[0].Frontmatter == nil {
		t.Fatal("Frontmatter should be non-nil when block is present")
	}
	if got[0].Frontmatter["shape"] != "fresh-feature" {
		t.Errorf("Frontmatter[shape] = %q, want %q", got[0].Frontmatter["shape"], "fresh-feature")
	}
	if got[0].Frontmatter["summary"] != "Two-tier vault staging shipped." {
		t.Errorf("Frontmatter[summary] = %q, want %q",
			got[0].Frontmatter["summary"], "Two-tier vault staging shipped.")
	}
}

// TestParseIterationsNoFrontmatterBackCompat is the regression lock for
// the 200+ existing iters that have no front-matter — they must round-
// trip with Frontmatter == nil and the narrative untouched.
func TestParseIterationsNoFrontmatterBackCompat(t *testing.T) {
	content := `### Iteration 52 — Legacy iter (2026-04-30)

Plain narrative without any front-matter block.

Second paragraph survives.
`
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	if got[0].Frontmatter != nil {
		t.Errorf("Frontmatter should be nil for legacy iter, got %v", got[0].Frontmatter)
	}
	want := "Plain narrative without any front-matter block.\n\nSecond paragraph survives."
	if got[0].Narrative != want {
		t.Errorf("Narrative = %q, want %q", got[0].Narrative, want)
	}
}

// TestParseIterationsMalformedFrontmatter exercises the "no closing ---"
// fallback: the parser must NOT error, the front-matter remains in the
// narrative, and Frontmatter is nil. (A warning is logged; we don't
// assert against the log output here — it would couple to global state.)
func TestParseIterationsMalformedFrontmatter(t *testing.T) {
	content := `### Iteration 53 — Bad FM (2026-04-30)

---
shape: planning
summary: Missing the closing fence below.

Body that becomes part of the unterminated block.
`
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	if got[0].Frontmatter != nil {
		t.Errorf("Frontmatter should be nil on malformed block, got %v", got[0].Frontmatter)
	}
	// The unclosed YAML stays inside the narrative (operators can spot
	// the drift visually).
	if !strings.Contains(got[0].Narrative, "---") {
		t.Errorf("malformed front-matter should remain in narrative; got %q", got[0].Narrative)
	}
	if !strings.Contains(got[0].Narrative, "shape: planning") {
		t.Errorf("unclosed YAML keys should remain in narrative; got %q", got[0].Narrative)
	}
}

// TestParseIterationsForwardCompatExtraKeys pins forward-compat: future
// schema additions land as extra keys in Frontmatter and are preserved
// (even if downstream ignores them).
func TestParseIterationsForwardCompatExtraKeys(t *testing.T) {
	content := `### Iteration 54 — Future iter (2026-04-30)

---
shape: planning
summary: Test extra-keys preservation.
future_field: some-value
another_extra: 42
---

Body.
`
	got := parseIterations(content)
	if len(got) != 1 {
		t.Fatalf("parseIterations returned %d entries, want 1", len(got))
	}
	if got[0].Frontmatter["future_field"] != "some-value" {
		t.Errorf("Frontmatter[future_field] = %q, want %q",
			got[0].Frontmatter["future_field"], "some-value")
	}
	if got[0].Frontmatter["another_extra"] != "42" {
		t.Errorf("Frontmatter[another_extra] = %q, want %q",
			got[0].Frontmatter["another_extra"], "42")
	}
}

// TestIterationFrontmatterNotSerialized is the v3-M5 json:"-" gate. The
// Frontmatter map must be invisible in serialized table or full responses
// regardless of population state.
func TestIterationFrontmatterNotSerialized(t *testing.T) {
	const fixtureFM = `# proj — Project History

### Iteration 1 — With FM (2026-04-30)

---
shape: planning
summary: A summary.
---

Body.
`
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureFM,
	})
	tool := NewGetIterationsTool(cfg)
	for _, format := range []string{"table", "full"} {
		result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"` + format + `"}`))
		if err != nil {
			t.Fatalf("handler error (format=%s): %v", format, err)
		}
		for _, leak := range []string{`"frontmatter"`, `"Frontmatter"`} {
			if strings.Contains(result, leak) {
				t.Errorf("format=%s leaked %q in JSON: %s", format, leak, result)
			}
		}
	}
}

// --- Phase A: format=summary handler tests (10) ---

type iterationSummaryResponse struct {
	Project    string             `json:"project"`
	Total      int                `json:"total"`
	Returned   int                `json:"returned"`
	Iterations []IterationSummary `json:"iterations"`
}

func parseSummaryResponse(t *testing.T, raw string) iterationSummaryResponse {
	t.Helper()
	var resp iterationSummaryResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, raw)
	}
	return resp
}

// Test 7: format=summary returns documented schema with shape/summary
// from front-matter and both *_present discriminators true.
func TestGetIterationsSummaryFromFrontmatter(t *testing.T) {
	content := `# proj

### Iteration 1 — Has FM (2026-04-30)

---
shape: fresh-feature
summary: Summary from front-matter.
---

Body that should NOT appear in summary.
`
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	if len(resp.Iterations) != 1 {
		t.Fatalf("want 1 iter, got %d", len(resp.Iterations))
	}
	got := resp.Iterations[0]
	if got.Shape != "fresh-feature" || !got.ShapePresent {
		t.Errorf("Shape = %q, ShapePresent = %v; want fresh-feature, true", got.Shape, got.ShapePresent)
	}
	if got.Summary != "Summary from front-matter." || !got.SummaryPresent {
		t.Errorf("Summary = %q, SummaryPresent = %v; want 'Summary from front-matter.', true",
			got.Summary, got.SummaryPresent)
	}
	if got.Title != "Has FM" {
		t.Errorf("Title = %q, want 'Has FM'", got.Title)
	}
}

// Test 8: format=summary first-paragraph fallback when no front-matter.
func TestGetIterationsSummaryFallback(t *testing.T) {
	content := `# proj

### Iteration 1 — No FM (2026-04-30)

This is the first paragraph that becomes the fallback summary.

Second paragraph is ignored by the fallback.
`
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	if len(resp.Iterations) != 1 {
		t.Fatalf("want 1 iter, got %d", len(resp.Iterations))
	}
	got := resp.Iterations[0]
	if got.SummaryPresent {
		t.Error("SummaryPresent should be false when summary came from fallback")
	}
	if got.ShapePresent {
		t.Error("ShapePresent should be false when no front-matter")
	}
	if got.Shape != "" {
		t.Errorf("Shape should be empty without front-matter, got %q", got.Shape)
	}
	if got.Summary != "This is the first paragraph that becomes the fallback summary." {
		t.Errorf("Summary = %q, want fallback first paragraph", got.Summary)
	}
}

// Test 9: format=summary truncates >200-char first paragraph at word
// boundary with trailing ellipsis (v3-C5: word-boundary, not sentence).
func TestGetIterationsSummaryTruncatesAtWordBoundary(t *testing.T) {
	long := strings.Repeat("alpha bravo charlie ", 20) // ~400 chars
	content := "# proj\n\n### Iteration 1 — Long (2026-04-30)\n\n" + long + "\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	if len(resp.Iterations) != 1 {
		t.Fatalf("want 1 iter")
	}
	got := resp.Iterations[0].Summary
	if !strings.HasSuffix(got, "…") {
		t.Errorf("Summary should end with ellipsis when truncated; got %q", got)
	}
	// 200 chars max + the ellipsis (1 rune). The truncation cut at last
	// space before 200, so length-of-runes ≤ 200 + 1.
	if runes := []rune(got); len(runes) > 201 {
		t.Errorf("Summary too long: %d runes, want ≤201", len(runes))
	}
	// Word-boundary: the cut must not be mid-word; the rune before the
	// ellipsis is a non-space.
	if strings.Contains(got, " …") {
		t.Errorf("Summary should trim trailing space before ellipsis; got %q", got)
	}
}

// Test 10: format=summary strips leading **Bold:** prefix.
func TestGetIterationsSummaryStripsBoldPrefix(t *testing.T) {
	content := `# proj

### Iteration 1 — Bold prefix (2026-04-30)

**Shape:** **Inner:** the rest of the summary survives.
`
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	got := resp.Iterations[0].Summary
	// Only the FIRST **Bold:** prefix is stripped; the second remains
	// (acceptable per D8).
	if strings.HasPrefix(got, "**Shape:**") {
		t.Errorf("first **Bold:** prefix should be stripped; got %q", got)
	}
	if !strings.Contains(got, "**Inner:**") {
		t.Errorf("multi-prefix should retain inner **Bold:**; got %q", got)
	}
	if !strings.Contains(got, "the rest of the summary survives") {
		t.Errorf("summary content missing; got %q", got)
	}
}

// Test 11: format=summary handles markdown-list first paragraph.
func TestGetIterationsSummaryHandlesMarkdownList(t *testing.T) {
	content := `# proj

### Iteration 1 — List start (2026-04-30)

- item one in the list
- item two in the list
- item three in the list

Real prose paragraph.
`
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	got := resp.Iterations[0].Summary
	// First "paragraph" is the list, joined on space; no special-casing.
	if !strings.Contains(got, "item one in the list") {
		t.Errorf("list-as-paragraph should be in summary; got %q", got)
	}
	if got == "" {
		t.Errorf("summary should not be empty for list-shaped first paragraph")
	}
}

// Test 12: format=summary handles fenced-code first paragraph.
func TestGetIterationsSummaryHandlesCodeFence(t *testing.T) {
	content := "# proj\n\n### Iteration 1 — Fence (2026-04-30)\n\n```bash\ngo test ./...\nmake build\n```\n\nReal prose.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	got := resp.Iterations[0].Summary
	if got == "" {
		t.Errorf("summary should not be empty for code-fence first paragraph")
	}
	// First "paragraph" is the fenced block; joined on space.
	if !strings.Contains(got, "```bash") {
		t.Errorf("code fence content should be in summary; got %q", got)
	}
}

// Test 13: format=summary handles empty body.
func TestGetIterationsSummaryEmptyBody(t *testing.T) {
	content := "# proj\n\n### Iteration 1 — Empty (2026-04-30)\n\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	got := resp.Iterations[0]
	if got.Summary != "" {
		t.Errorf("empty body should give empty summary, got %q", got.Summary)
	}
	if got.SummaryPresent {
		t.Error("SummaryPresent must be false on empty body")
	}
}

// Test 14: format=summary handles only-trailer (planning iter shape).
func TestGetIterationsSummaryOnlyTrailer(t *testing.T) {
	content := "# proj\n\n### Iteration 1 — Trailer only (2026-04-30)\n\n<!-- recorded: host=H -->\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": content,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	got := resp.Iterations[0]
	// Trailer is stripped by parseIterations, so the body is empty;
	// summary stays empty.
	if got.Summary != "" {
		t.Errorf("trailer-only iter should have empty summary, got %q", got.Summary)
	}
	if got.SummaryPresent {
		t.Error("SummaryPresent must be false on trailer-only body")
	}
}

// Test 15: format=summary respects since_iteration filter.
func TestGetIterationsSummarySinceFilter(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary","since_iteration":3}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	if resp.Returned != 3 {
		t.Errorf("returned = %d, want 3 (iters 3,4,5)", resp.Returned)
	}
	for _, it := range resp.Iterations {
		if it.Number < 3 {
			t.Errorf("iter %d should have been filtered out", it.Number)
		}
	}
}

// Test 16: format=summary respects limit cap.
func TestGetIterationsSummaryLimit(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": fixtureIterations,
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","format":"summary","limit":2}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseSummaryResponse(t, result)
	if resp.Returned != 2 {
		t.Errorf("returned = %d, want 2 with limit=2", resp.Returned)
	}
	if resp.Iterations[0].Number != 5 || resp.Iterations[1].Number != 4 {
		t.Errorf("limit=2 should return iters 5,4 newest-first; got %d,%d",
			resp.Iterations[0].Number, resp.Iterations[1].Number)
	}
}

func TestGetIterationsEmptyVault(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# No iterations yet\n",
	})
	tool := NewGetIterationsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	resp := parseIterationsResponse(t, result)
	if resp.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Total)
	}
	if resp.Iterations == nil {
		t.Error("iterations slice should be empty, not nil (must serialize as [])")
	}
	if len(resp.Iterations) != 0 {
		t.Errorf("returned %d iterations, want 0", len(resp.Iterations))
	}
	// Verify the serialized JSON uses [] not null for empty iterations
	if !strings.Contains(result, `"iterations": []`) {
		t.Errorf("empty iterations should serialize as []; got %s", result)
	}
}
