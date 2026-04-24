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
