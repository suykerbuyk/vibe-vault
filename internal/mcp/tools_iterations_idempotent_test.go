// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// fixedNow is a deterministic timestamp used by the idempotency tests
// so revision-block front-matter is byte-identical across runs.
var fixedNow = time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)

// withFixedNow swaps `nowFunc` to a deterministic clock for the
// duration of the test, restoring the previous value on cleanup.
func withFixedNow(t *testing.T, ts time.Time) {
	t.Helper()
	prev := nowFunc
	nowFunc = func() time.Time { return ts }
	t.Cleanup(func() { nowFunc = prev })
}

// stripProvenance removes the optional `<!-- recorded: ... -->`
// trailer from a string so test assertions stay stable across hosts.
func stripProvenance(s string) string {
	idx := strings.Index(s, "<!-- recorded:")
	if idx < 0 {
		return s
	}
	end := strings.Index(s[idx:], "-->")
	if end < 0 {
		return s
	}
	return strings.TrimRight(s[:idx]+s[idx+end+3:], "\n \t") + "\n"
}

// TestAppendIterationIdempotent_HeadingAbsent — Case 1 of the
// content-addressable contract. The new iter heading is not in the
// existing iterations.md, so the call appends normally.
func TestAppendIterationIdempotent_HeadingAbsent(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	out, err := tool.Handler(json.RawMessage(
		`{"project":"testproj","iteration":7,"title":"First","narrative":"Body line.","date":"2026-05-05"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var resp struct {
		Action     string `json:"action"`
		Iteration  int    `json:"iteration"`
		Idempotent bool   `json:"idempotent"`
	}
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("unmarshal: %v\nout=%s", jerr, out)
	}
	if resp.Action != "appended" || resp.Iteration != 7 || resp.Idempotent {
		t.Errorf("response = %+v; want {appended, iter 7, !idempotent}", resp)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	if !strings.Contains(string(data), "### Iteration 7 — First (2026-05-05)") {
		t.Errorf("file missing heading; got:\n%s", string(data))
	}
}

// TestAppendIterationIdempotent_BodyIdentical — Case 2 of the
// contract. A second call with byte-identical body for the same iter
// must be a no-op: file contents unchanged, response carries
// idempotent=true.
func TestAppendIterationIdempotent_BodyIdentical(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	args := `{"project":"testproj","iteration":12,"title":"Once","narrative":"The body line.","date":"2026-05-05","summary":"Short summary.","shape":"bookkeeping"}`

	if _, err := tool.Handler(json.RawMessage(args)); err != nil {
		t.Fatalf("first handler: %v", err)
	}
	dataAfter1, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))

	out2, err := tool.Handler(json.RawMessage(args))
	if err != nil {
		t.Fatalf("second handler: %v", err)
	}
	var resp struct {
		Action     string `json:"action"`
		Iteration  int    `json:"iteration"`
		Idempotent bool   `json:"idempotent"`
	}
	if jerr := json.Unmarshal([]byte(out2), &resp); jerr != nil {
		t.Fatalf("unmarshal: %v", jerr)
	}
	if resp.Action != "idempotent" || !resp.Idempotent || resp.Iteration != 12 {
		t.Errorf("response = %+v; want {idempotent, iter 12}", resp)
	}
	dataAfter2, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	if string(dataAfter1) != string(dataAfter2) {
		t.Errorf("idempotent call mutated file:\nbefore:\n%s\nafter:\n%s",
			stripProvenance(string(dataAfter1)), stripProvenance(string(dataAfter2)))
	}
}

// TestAppendIterationIdempotent_BodyDiffers — Case 3 of the
// contract. The second call carries a different narrative; the tool
// appends a `### Iteration N (revision K)` block beneath the
// original. The original heading + body are untouched.
func TestAppendIterationIdempotent_BodyDiffers(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	args1 := `{"project":"testproj","iteration":42,"title":"Original","narrative":"First version body.","date":"2026-05-05"}`
	args2 := `{"project":"testproj","iteration":42,"title":"Original","narrative":"REFINED version body, with new prose.","date":"2026-05-05"}`

	if _, err := tool.Handler(json.RawMessage(args1)); err != nil {
		t.Fatalf("first handler: %v", err)
	}
	out2, err := tool.Handler(json.RawMessage(args2))
	if err != nil {
		t.Fatalf("second handler: %v", err)
	}
	var resp struct {
		Action     string `json:"action"`
		Iteration  int    `json:"iteration"`
		Revision   int    `json:"revision"`
		Idempotent bool   `json:"idempotent"`
		Heading    string `json:"heading"`
	}
	if jerr := json.Unmarshal([]byte(out2), &resp); jerr != nil {
		t.Fatalf("unmarshal: %v", jerr)
	}
	if resp.Action != "revised" || resp.Iteration != 42 || resp.Revision != 2 || resp.Idempotent {
		t.Errorf("response = %+v; want {revised, iter 42, revision 2}", resp)
	}
	if !strings.HasPrefix(resp.Heading, "### Iteration 42 (revision 2) — ") {
		t.Errorf("heading = %q, want '### Iteration 42 (revision 2) — ...'", resp.Heading)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	content := string(data)
	if !strings.Contains(content, "### Iteration 42 — Original") {
		t.Errorf("original heading missing or rewritten; got:\n%s", content)
	}
	if !strings.Contains(content, "First version body.") {
		t.Errorf("original body missing — revision must NOT mutate the original; got:\n%s", content)
	}
	if !strings.Contains(content, "### Iteration 42 (revision 2) — Original") {
		t.Errorf("revision heading missing; got:\n%s", content)
	}
	if !strings.Contains(content, "REFINED version body, with new prose.") {
		t.Errorf("revision body missing; got:\n%s", content)
	}
	if !strings.Contains(content, "revises: 42") || !strings.Contains(content, "revised_at: 2026-05-05T12:00:00Z") {
		t.Errorf("revision front-matter missing 'revises'/'revised_at'; got:\n%s", content)
	}
}

// TestAppendIterationIdempotent_RevisionCounterIncrement — Case 3
// follow-up. A third call with yet another divergent body bumps the
// revision counter to 3, not 2.
func TestAppendIterationIdempotent_RevisionCounterIncrement(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	for i, narrative := range []string{"v1 body.", "v2 body.", "v3 body."} {
		payload, _ := json.Marshal(map[string]any{
			"project":   "testproj",
			"iteration": 42,
			"title":     "Triple",
			"narrative": narrative,
			"date":      "2026-05-05",
		})
		out, err := tool.Handler(payload)
		if err != nil {
			t.Fatalf("call %d handler: %v", i+1, err)
		}
		var resp struct {
			Action   string `json:"action"`
			Revision int    `json:"revision"`
		}
		if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
			t.Fatalf("unmarshal: %v", jerr)
		}
		switch i {
		case 0:
			if resp.Action != "appended" {
				t.Errorf("first call: action=%q, want appended", resp.Action)
			}
		case 1:
			if resp.Action != "revised" || resp.Revision != 2 {
				t.Errorf("second call: action=%q rev=%d; want revised+rev=2", resp.Action, resp.Revision)
			}
		case 2:
			if resp.Action != "revised" || resp.Revision != 3 {
				t.Errorf("third call: action=%q rev=%d; want revised+rev=3", resp.Action, resp.Revision)
			}
		}
	}
	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	content := string(data)
	if !strings.Contains(content, "### Iteration 42 — Triple") {
		t.Errorf("original heading missing; got:\n%s", content)
	}
	if !strings.Contains(content, "### Iteration 42 (revision 2) — Triple") {
		t.Errorf("revision 2 heading missing; got:\n%s", content)
	}
	if !strings.Contains(content, "### Iteration 42 (revision 3) — Triple") {
		t.Errorf("revision 3 heading missing; got:\n%s", content)
	}
}

// TestAppendIterationIdempotent_TrailingNewlineNormalization — the
// whitespace-tolerance edge. A second call with the same body but
// extra trailing newline differences (single vs. double) must still
// classify as idempotent.
func TestAppendIterationIdempotent_TrailingNewlineNormalization(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	if _, err := tool.Handler(json.RawMessage(
		`{"project":"testproj","iteration":99,"title":"WS","narrative":"Body line.","date":"2026-05-05"}`)); err != nil {
		t.Fatalf("first: %v", err)
	}

	// Second call: same narrative but with extra trailing newlines —
	// the writer strips trailing whitespace before storing, so the
	// canonical body should still match.
	out, err := tool.Handler(json.RawMessage(
		`{"project":"testproj","iteration":99,"title":"WS","narrative":"Body line.\n\n\n","date":"2026-05-05"}`))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	var resp struct {
		Action     string `json:"action"`
		Idempotent bool   `json:"idempotent"`
	}
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("unmarshal: %v", jerr)
	}
	if resp.Action != "idempotent" || !resp.Idempotent {
		t.Errorf("trailing-newline divergence misclassified: action=%q idempotent=%v",
			resp.Action, resp.Idempotent)
	}
}

// TestAppendIterationIdempotent_InternalWhitespaceDiffers — defense
// case ensuring internal whitespace is NOT normalized. Two bodies
// that differ only by a leading-space change on a list bullet must
// classify as divergent (rev block append), not idempotent.
//
// Rendered markdown is whitespace-semantic — a tab vs. two spaces
// can change the rendered list nesting, so silently treating those
// as identical would lose information.
func TestAppendIterationIdempotent_InternalWhitespaceDiffers(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	if _, err := tool.Handler(json.RawMessage(
		`{"project":"testproj","iteration":50,"title":"WS","narrative":"- bullet one\n  - nested","date":"2026-05-05"}`)); err != nil {
		t.Fatalf("first: %v", err)
	}
	out, err := tool.Handler(json.RawMessage(
		`{"project":"testproj","iteration":50,"title":"WS","narrative":"- bullet one\n    - nested","date":"2026-05-05"}`))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	var resp struct {
		Action string `json:"action"`
	}
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("unmarshal: %v", jerr)
	}
	if resp.Action == "idempotent" {
		t.Error("internal whitespace divergence wrongly normalized to idempotent")
	}
	if resp.Action != "revised" {
		t.Errorf("expected revised, got %q", resp.Action)
	}
}

// TestAppendIterationIdempotent_DownstreamParsersDedupeRevisions
// verifies the Part 3 audit deliverable: when iterations.md contains
// an original block plus one or more revision blocks for the same N,
// every downstream consumer (vv_get_iterations table/full/summary,
// project-history-tail row collector, nextIterFromIterationsMD) emits
// one entry per N — never one per heading.
func TestAppendIterationIdempotent_DownstreamParsersDedupeRevisions(t *testing.T) {
	withFixedNow(t, fixedNow)
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})
	tool := NewAppendIterationTool(cfg)
	// Append iter 1 (original), then iter 1 with divergent body
	// (-> revision 2), then iter 1 with another divergence (-> rev 3),
	// then iter 2 (original). Result: 4 H3 headings, 2 unique iters.
	for _, args := range []string{
		`{"project":"testproj","iteration":1,"title":"One","narrative":"v1.","date":"2026-05-05"}`,
		`{"project":"testproj","iteration":1,"title":"One","narrative":"v1 REFINED.","date":"2026-05-05"}`,
		`{"project":"testproj","iteration":1,"title":"One","narrative":"v1 again.","date":"2026-05-05"}`,
		`{"project":"testproj","iteration":2,"title":"Two","narrative":"v2.","date":"2026-05-05"}`,
	} {
		if _, err := tool.Handler(json.RawMessage(args)); err != nil {
			t.Fatalf("handler: %v", err)
		}
	}

	// nextIterFromIterationsMD must report N=3 (max iter is 2 + 1).
	n, err := nextIterFromIterationsMD(cfg.VaultPath, "testproj")
	if err != nil {
		t.Fatalf("nextIter: %v", err)
	}
	if n != 3 {
		t.Errorf("nextIterFromIterationsMD = %d, want 3 (max+1; revisions must not bump max)", n)
	}

	// vv_get_iterations table format must return exactly 2 entries
	// (one per unique iteration number).
	getTool := NewGetIterationsTool(cfg)
	out, err := getTool.Handler(json.RawMessage(`{"project":"testproj","format":"table"}`))
	if err != nil {
		t.Fatalf("get table: %v", err)
	}
	var resp struct {
		Total      int         `json:"total"`
		Returned   int         `json:"returned"`
		Iterations []Iteration `json:"iterations"`
	}
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("unmarshal table: %v", jerr)
	}
	if resp.Total != 2 || resp.Returned != 2 || len(resp.Iterations) != 2 {
		t.Errorf("table format fragmented revisions: total=%d returned=%d len=%d (want 2/2/2)",
			resp.Total, resp.Returned, len(resp.Iterations))
	}
	// Iter 1 entry must be the highest-revision narrative (last-wins).
	for _, it := range resp.Iterations {
		if it.Number == 1 && it.Revision != 3 {
			t.Errorf("iter 1 dedupe last-wins broken: revision=%d, want 3", it.Revision)
		}
	}

	// vv_get_iterations summary format must also dedupe.
	out, err = getTool.Handler(json.RawMessage(`{"project":"testproj","format":"summary"}`))
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	var sresp struct {
		Total      int                `json:"total"`
		Returned   int                `json:"returned"`
		Iterations []IterationSummary `json:"iterations"`
	}
	if jerr := json.Unmarshal([]byte(out), &sresp); jerr != nil {
		t.Fatalf("unmarshal summary: %v", jerr)
	}
	if sresp.Total != 2 || len(sresp.Iterations) != 2 {
		t.Errorf("summary format fragmented revisions: total=%d len=%d (want 2/2)",
			sresp.Total, len(sresp.Iterations))
	}

	// collectHistoryRows (project-history-tail renderer) must emit
	// one row per N.
	rows, err := collectHistoryRows(cfg, "testproj", 10)
	if err != nil {
		t.Fatalf("collectHistoryRows: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("project-history-tail fragmented revisions: rows=%d (want 2)", len(rows))
	}
}

// TestAppendIterationIdempotent_ProvenanceTrailerStrippedFromCompare
// — re-running the same wrap from a different host (with a different
// `<!-- recorded: host=... -->` trailer) must still classify as
// idempotent. The trailer is provenance-only and is not part of the
// content-addressable surface.
func TestAppendIterationIdempotent_ProvenanceTrailerStrippedFromCompare(t *testing.T) {
	withFixedNow(t, fixedNow)
	// Pre-populate iterations.md with a fake existing block carrying
	// a host-A provenance trailer. The next handler call simulates
	// host B running the same wrap.
	existing := "# Iterations\n\n### Iteration 88 — Multi-host (2026-05-05)\n\nShared body.\n\n<!-- recorded: host=hostA user=u1 -->\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": existing,
	})
	tool := NewAppendIterationTool(cfg)
	out, err := tool.Handler(json.RawMessage(
		`{"project":"testproj","iteration":88,"title":"Multi-host","narrative":"Shared body.","date":"2026-05-05"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var resp struct {
		Action     string `json:"action"`
		Idempotent bool   `json:"idempotent"`
	}
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("unmarshal: %v", jerr)
	}
	if !resp.Idempotent || resp.Action != "idempotent" {
		t.Errorf("multi-host provenance-only divergence misclassified: %+v", resp)
	}
}
