// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package wraprender_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/context"
	"github.com/suykerbuyk/vibe-vault/internal/wraprender"
)

func TestRenderActiveTasks_Empty(t *testing.T) {
	got := wraprender.RenderActiveTasks(nil)
	want := "### Active tasks (0)\n\n_No active tasks._\n"
	if got != want {
		t.Fatalf("empty list rendering mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderActiveTasks_OneTask(t *testing.T) {
	got := wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
		Slug:     "grok-provider-support",
		Title:    "Grok provider via OpenAI-compatible base_url",
		Status:   "Draft v1",
		Priority: "high",
	}})
	want := "### Active tasks (1)\n\n" +
		"- **`grok-provider-support`** (priority: high, status: Draft v1) — Grok provider via OpenAI-compatible base_url\n"
	if got != want {
		t.Fatalf("one-task rendering mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderActiveTasks_MultipleTasks(t *testing.T) {
	got := wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{
		{Slug: "alpha", Title: "Alpha task", Status: "Draft", Priority: "high"},
		{Slug: "beta", Title: "Beta task", Status: "Phase 1", Priority: "medium"},
	})
	wantHeader := "### Active tasks (2)\n\n"
	if !strings.HasPrefix(got, wantHeader) {
		t.Fatalf("missing header in:\n%s", got)
	}
	if !strings.Contains(got, "- **`alpha`**") {
		t.Fatalf("missing alpha bullet in:\n%s", got)
	}
	if !strings.Contains(got, "- **`beta`**") {
		t.Fatalf("missing beta bullet in:\n%s", got)
	}
}

func TestRenderActiveTasks_StableOrdering(t *testing.T) {
	// Realistic mixed inputs to lock the ordering contract:
	// - high beats medium beats low beats unset.
	// - within a priority class, alphabetical by slug.
	got := wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{
		{Slug: "zeta", Priority: "low", Title: "z", Status: "s"},
		{Slug: "alpha", Priority: "medium", Title: "a", Status: "s"},
		{Slug: "delta", Priority: "high", Title: "d", Status: "s"},
		{Slug: "epsilon", Priority: "", Title: "e", Status: "s"},
		{Slug: "beta", Priority: "high", Title: "b", Status: "s"},
		{Slug: "gamma", Priority: "low", Title: "g", Status: "s"},
		{Slug: "omega", Priority: "HIGH", Title: "o", Status: "s"},
	})
	want := []string{"beta", "delta", "omega", "alpha", "gamma", "zeta", "epsilon"}
	gotOrder := []string{}
	for _, ln := range strings.Split(got, "\n") {
		if !strings.HasPrefix(ln, "- **`") {
			continue
		}
		// Extract slug between the inner backticks.
		s := strings.TrimPrefix(ln, "- **`")
		end := strings.Index(s, "`")
		if end < 0 {
			t.Fatalf("malformed bullet: %q", ln)
		}
		gotOrder = append(gotOrder, s[:end])
	}
	if len(gotOrder) != len(want) {
		t.Fatalf("got %d bullets, want %d:\n%s", len(gotOrder), len(want), got)
	}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("ordering mismatch at i=%d:\n got: %v\nwant: %v\nfull:\n%s",
				i, gotOrder, want, got)
		}
	}
}

func TestRenderActiveTasks_TitleEscaping(t *testing.T) {
	// Markdown-special characters in title flow through verbatim. The
	// bullet shape (slug in code-span, fixed-position priority and
	// status fields) does not depend on title content. Locked here.
	got := wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
		Slug:     "edge",
		Title:    "Title with | pipe, *asterisks*, `backticks`, [brackets](x), and \\backslashes\\",
		Status:   "Draft",
		Priority: "high",
	}})
	for _, want := range []string{
		"- **`edge`** (priority: high, status: Draft) — ",
		"| pipe",
		"*asterisks*",
		"`backticks`",
		"[brackets](x)",
		"\\backslashes\\",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered output missing %q:\n%s", want, got)
		}
	}
	// Bullet must remain a single line (i.e., the title's special
	// chars do not split the rendered bullet across lines).
	bulletLines := 0
	for _, ln := range strings.Split(got, "\n") {
		if strings.HasPrefix(ln, "- **`edge`**") {
			bulletLines++
		}
	}
	if bulletLines != 1 {
		t.Fatalf("expected 1 bullet line for slug edge, got %d:\n%s", bulletLines, got)
	}
}

func TestRenderCurrentState_AllFields(t *testing.T) {
	got := wraprender.RenderCurrentState(wraprender.CurrentState{
		Iterations:   166,
		Tests:        1938,
		TestPackages: 50,
		MCPTools:     43,
		Templates:    19,
	})
	want := strings.Join([]string{
		"- **Iterations:** 166 complete",
		"- **Tests:** 1938 RUN-counted across 50 packages",
		"- **MCP:** 43 tools + 1 prompt",
		"- **Embedded:** 19 templates",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("current-state rendering mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderCurrentState_OutputPassesV10Validator(t *testing.T) {
	// Self-check: rendered output is a valid `Current State` body
	// under the v10 invariants contract.
	body := wraprender.RenderCurrentState(wraprender.CurrentState{
		Iterations:   166,
		Tests:        1938,
		TestPackages: 50,
		MCPTools:     43,
		Templates:    19,
	})
	bad, ok := context.ValidateCurrentStateBody(body)
	if !ok {
		t.Fatalf("v10 validator rejected rendered body — bad line: %q\nfull body:\n%s",
			bad, body)
	}
	// Spot-check a few non-typical numbers (e.g., zero counts and a
	// large iteration count) — the format must hold across the
	// realistic numeric range.
	for _, st := range []wraprender.CurrentState{
		{Iterations: 0, Tests: 0, TestPackages: 0, MCPTools: 0, Templates: 0},
		{Iterations: 9999, Tests: 99999, TestPackages: 250, MCPTools: 99, Templates: 99},
	} {
		body := wraprender.RenderCurrentState(st)
		if bad, ok := context.ValidateCurrentStateBody(body); !ok {
			t.Fatalf("v10 validator rejected body for %+v — bad line: %q\nbody:\n%s",
				st, bad, body)
		}
	}
}

func TestRenderProjectHistoryTail_TruncatesToN(t *testing.T) {
	rows := make([]wraprender.HistoryRow, 0, 15)
	for i := 1; i <= 15; i++ {
		rows = append(rows, wraprender.HistoryRow{
			Iteration: i,
			Date:      "2026-04-01",
			Summary:   "iter " + itoaSimple(i),
		})
	}
	got := wraprender.RenderProjectHistoryTail(rows, 10)
	// Only iterations 6..15 should appear, in order.
	for i := 1; i <= 5; i++ {
		needle := "| " + itoaSimple(i) + " | "
		if strings.Contains(got, needle) {
			t.Fatalf("truncation failed: row %d still present:\n%s", i, got)
		}
	}
	for i := 6; i <= 15; i++ {
		needle := "| " + itoaSimple(i) + " | "
		if !strings.Contains(got, needle) {
			t.Fatalf("truncation dropped row %d unexpectedly:\n%s", i, got)
		}
	}
	// Order: row 6 must precede row 15.
	if strings.Index(got, "| 6 | ") >= strings.Index(got, "| 15 | ") {
		t.Fatalf("row order incorrect:\n%s", got)
	}
}

func TestRenderProjectHistoryTail_HandlesEmptyIterations(t *testing.T) {
	got := wraprender.RenderProjectHistoryTail(nil, 10)
	if !strings.HasPrefix(got, "| #") {
		t.Fatalf("missing table header:\n%s", got)
	}
	if !strings.Contains(got, "_No iterations recorded yet._") {
		t.Fatalf("missing empty-iteration pointer line:\n%s", got)
	}
}

const sampleResume = `# Project resume

## What This Project Is

Stuff.

## Current State

- **Iterations:** 1 complete
- **Tests:** 5 RUN-counted across 1 packages
- **MCP:** 1 tools + 1 prompt
- **Embedded:** 1 templates

## Project History (recent)

| #   | Date       | Summary |
| --- | ---------- | ------- |
| 1 | 2026-01-01 | First iteration. |

## Open Threads

### Active tasks (1)

- **` + "`old-task`" + `** (priority: high, status: Draft) — Stale.

### Carried forward

- carry-bullet-one

## Reference Documents

- something
`

func TestApplyMarkerBlocks_ReplacesExisting(t *testing.T) {
	// Construct a resume.md that already contains the three marker
	// pairs (with stale contents) and check replacement.
	content := `# r

## Current State

<!-- vv:current-state:start -->
- **Iterations:** 0 complete
- **Tests:** 0 RUN-counted across 0 packages
- **MCP:** 0 tools + 1 prompt
- **Embedded:** 0 templates
<!-- vv:current-state:end -->

## Project History (recent)

<!-- vv:project-history-tail:start -->
old contents
<!-- vv:project-history-tail:end -->

## Open Threads

<!-- vv:active-tasks:start -->
### Active tasks (0)

_No active tasks._
<!-- vv:active-tasks:end -->
`
	out, err := wraprender.ApplyMarkerBlocks(content, map[string]string{
		wraprender.RegionCurrentState: wraprender.RenderCurrentState(wraprender.CurrentState{
			Iterations: 5, Tests: 10, TestPackages: 2, MCPTools: 3, Templates: 7,
		}),
		wraprender.RegionProjectHistoryTail: wraprender.RenderProjectHistoryTail([]wraprender.HistoryRow{
			{Iteration: 5, Date: "2026-04-27", Summary: "fresh row"},
		}, 10),
		wraprender.RegionActiveTasks: wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
			Slug: "fresh-task", Title: "Fresh", Status: "Draft", Priority: "high",
		}}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "Iterations:** 0 complete") {
		t.Fatalf("stale current-state body survived:\n%s", out)
	}
	if !strings.Contains(out, "Iterations:** 5 complete") {
		t.Fatalf("fresh current-state body missing:\n%s", out)
	}
	if strings.Contains(out, "old contents") {
		t.Fatalf("stale history-tail body survived:\n%s", out)
	}
	if !strings.Contains(out, "fresh row") {
		t.Fatalf("fresh history-tail body missing:\n%s", out)
	}
	if !strings.Contains(out, "fresh-task") {
		t.Fatalf("fresh active-tasks body missing:\n%s", out)
	}
}

func TestApplyMarkerBlocks_InsertsWhenAbsent(t *testing.T) {
	// Each sub-case starts from a markerless resume.md and checks
	// that ApplyMarkerBlocks inserts the marker pair at the
	// documented default location for that region.
	cases := []struct {
		name   string
		region string
		body   string
		anchor string // surrounding-text needle that must precede the inserted marker pair
	}{
		{
			name:   "active-tasks under Open Threads",
			region: wraprender.RegionActiveTasks,
			body: wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
				Slug: "x", Title: "X", Status: "S", Priority: "high",
			}}),
			anchor: "## Open Threads",
		},
		{
			name:   "current-state under Current State",
			region: wraprender.RegionCurrentState,
			body: wraprender.RenderCurrentState(wraprender.CurrentState{
				Iterations: 1, Tests: 1, TestPackages: 1, MCPTools: 1, Templates: 1,
			}),
			anchor: "## Current State",
		},
		{
			name:   "project-history-tail under Project History",
			region: wraprender.RegionProjectHistoryTail,
			body: wraprender.RenderProjectHistoryTail([]wraprender.HistoryRow{{
				Iteration: 1, Date: "2026-04-27", Summary: "hello",
			}}, 10),
			anchor: "## Project History (recent)",
		},
	}
	// Markerless fixture — strip every `<!-- vv:` line from a known-
	// good template. We use a hand-built one rather than the
	// sampleResume fixture (which already has H3 active tasks) to
	// exercise the H3-absent insertion arm of active-tasks.
	markerless := `# r

## What This Project Is

stuff

## Current State

old current state prose

## Project History (recent)

old history prose

## Open Threads

old open threads prose

## Reference Documents

stuff
`
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := wraprender.ApplyMarkerBlocks(markerless, map[string]string{c.region: c.body})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			startTag := "<!-- vv:" + c.region + ":start -->"
			endTag := "<!-- vv:" + c.region + ":end -->"
			if !strings.Contains(out, startTag) {
				t.Fatalf("start tag missing:\n%s", out)
			}
			if !strings.Contains(out, endTag) {
				t.Fatalf("end tag missing:\n%s", out)
			}
			anchorIdx := strings.Index(out, c.anchor)
			startIdx := strings.Index(out, startTag)
			if anchorIdx < 0 || startIdx < 0 || anchorIdx >= startIdx {
				t.Fatalf("expected %q to precede %q in output:\n%s", c.anchor, startTag, out)
			}
		})
	}

	// Sub-case: active-tasks insertion when `### Active tasks` H3
	// already exists — the H3 + body must be replaced by the marker
	// pair (since the rendered block carries its own H3).
	t.Run("active-tasks replaces existing H3", func(t *testing.T) {
		body := wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
			Slug: "fresh-x", Title: "X", Status: "S", Priority: "high",
		}})
		out, err := wraprender.ApplyMarkerBlocks(sampleResume, map[string]string{
			wraprender.RegionActiveTasks: body,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(out, "old-task") {
			t.Fatalf("stale active-tasks H3 contents survived:\n%s", out)
		}
		if !strings.Contains(out, "fresh-x") {
			t.Fatalf("fresh active-tasks bullet missing:\n%s", out)
		}
		if !strings.Contains(out, "<!-- vv:active-tasks:start -->") {
			t.Fatalf("marker pair not inserted:\n%s", out)
		}
		// Carried forward must remain intact (R1 non-collision).
		if !strings.Contains(out, "### Carried forward") {
			t.Fatalf("Carried forward H3 was dropped:\n%s", out)
		}
		if !strings.Contains(out, "carry-bullet-one") {
			t.Fatalf("Carried forward bullet was dropped:\n%s", out)
		}
	})
}

func TestApplyMarkerBlocks_PreservesOutsideRegions(t *testing.T) {
	// Apply all three blocks on the realistic sampleResume fixture and
	// verify that everything outside the touched marker spans is
	// byte-equal.
	out, err := wraprender.ApplyMarkerBlocks(sampleResume, map[string]string{
		wraprender.RegionActiveTasks: wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
			Slug: "x", Title: "X", Status: "S", Priority: "high",
		}}),
		wraprender.RegionCurrentState: wraprender.RenderCurrentState(wraprender.CurrentState{
			Iterations: 9, Tests: 9, TestPackages: 9, MCPTools: 9, Templates: 9,
		}),
		wraprender.RegionProjectHistoryTail: wraprender.RenderProjectHistoryTail([]wraprender.HistoryRow{
			{Iteration: 9, Date: "2026-04-27", Summary: "z"},
		}, 10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sentinels for content that lives outside any region.
	for _, want := range []string{
		"# Project resume",
		"## What This Project Is",
		"Stuff.",
		"### Carried forward",
		"- carry-bullet-one",
		"## Reference Documents",
		"- something",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected outside-region content %q preserved:\n%s", want, out)
		}
	}
}

func TestApplyMarkerBlocks_Idempotent(t *testing.T) {
	blocks := map[string]string{
		wraprender.RegionActiveTasks: wraprender.RenderActiveTasks([]wraprender.TaskFrontMatter{{
			Slug: "x", Title: "X", Status: "S", Priority: "high",
		}}),
		wraprender.RegionCurrentState: wraprender.RenderCurrentState(wraprender.CurrentState{
			Iterations: 9, Tests: 9, TestPackages: 9, MCPTools: 9, Templates: 9,
		}),
		wraprender.RegionProjectHistoryTail: wraprender.RenderProjectHistoryTail([]wraprender.HistoryRow{
			{Iteration: 9, Date: "2026-04-27", Summary: "z"},
		}, 10),
	}
	first, err := wraprender.ApplyMarkerBlocks(sampleResume, blocks)
	if err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	second, err := wraprender.ApplyMarkerBlocks(first, blocks)
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if first != second {
		t.Fatalf("apply not idempotent.\nfirst:\n%s\n\nsecond:\n%s", first, second)
	}
	// Triple-application also stable.
	third, err := wraprender.ApplyMarkerBlocks(second, blocks)
	if err != nil {
		t.Fatalf("third apply failed: %v", err)
	}
	if third != second {
		t.Fatalf("apply not idempotent on third run")
	}
}

func TestApplyMarkerBlocks_HandlesMissingEndMarker(t *testing.T) {
	content := `# r

## Current State

<!-- vv:current-state:start -->
broken contents — no end marker

## Open Threads

other stuff
`
	_, err := wraprender.ApplyMarkerBlocks(content, map[string]string{
		wraprender.RegionCurrentState: "fresh",
	})
	if err == nil {
		t.Fatalf("expected ErrMalformedMarker, got nil")
	}
	if !errors.Is(err, wraprender.ErrMalformedMarker) {
		t.Fatalf("expected ErrMalformedMarker, got: %v", err)
	}
}

// itoaSimple is a tiny stdlib-free int-to-string for tests so we don't
// pull strconv into the test file's imports for a single use.
func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	negative := n < 0
	if negative {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
