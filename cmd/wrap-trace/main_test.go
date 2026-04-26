// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// goldenTranscript is a minimal synthetic wrap session for golden-test purposes.
// It contains:
//   - A bootstrap turn (context loading)
//   - A vv_append_iteration turn (iterations.md narrative)
//   - Two vv_update_resume turns (open-threads rewrite, large output)
//   - A Write turn (commit.msg body)
//   - A Read turn before write (read-before-write ceremony)
//   - Bash turns (staging)
//   - A ToolSearch turn (schema load)
//   - A vv_capture_session turn (retelling)
const goldenTranscript = `{"type":"user","uuid":"u1","timestamp":"2026-04-25T10:00:00Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":"/wrap"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-04-25T10:00:01Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t1","name":"mcp__plugin_vibe-vault_vibe-vault__vv_bootstrap_context","input":{}}],"usage":{"input_tokens":500,"output_tokens":75,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"user","uuid":"u2","timestamp":"2026-04-25T10:00:05Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"context loaded"}]}}
{"type":"assistant","uuid":"a2","timestamp":"2026-04-25T10:00:06Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t2","name":"mcp__plugin_vibe-vault_vibe-vault__vv_append_iteration","input":{"project":"myproject","iteration":10,"title":"feat shipped","narrative":"Long narrative..."}}],"usage":{"input_tokens":2000,"output_tokens":4000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"user","uuid":"u3","timestamp":"2026-04-25T10:01:00Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"iteration appended"}]}}
{"type":"assistant","uuid":"a3","timestamp":"2026-04-25T10:01:01Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t3","name":"mcp__plugin_vibe-vault_vibe-vault__vv_update_resume","input":{"project":"myproject","content":"full resume rewrite"}}],"usage":{"input_tokens":8000,"output_tokens":12000,"cache_creation_input_tokens":0,"cache_read_input_tokens":5000}}}
{"type":"user","uuid":"u4","timestamp":"2026-04-25T10:04:00Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","content":"resume updated"}]}}
{"type":"assistant","uuid":"a4","timestamp":"2026-04-25T10:04:01Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t4","name":"Write","input":{"file_path":"/home/user/project/commit.msg","content":"feat(x): ship feature\n\nDetails."}}],"usage":{"input_tokens":6000,"output_tokens":3000,"cache_creation_input_tokens":0,"cache_read_input_tokens":4000}}}
{"type":"user","uuid":"u5","timestamp":"2026-04-25T10:05:00Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4","content":"file written"}]}}
{"type":"assistant","uuid":"a5","timestamp":"2026-04-25T10:05:01Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t5","name":"Read","input":{"file_path":"/home/user/project/commit.msg"}}],"usage":{"input_tokens":5500,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":4500}}}
{"type":"user","uuid":"u6","timestamp":"2026-04-25T10:05:10Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t5","content":"file content"}]}}
{"type":"assistant","uuid":"a6","timestamp":"2026-04-25T10:05:11Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t6","name":"Bash","input":{"command":"git add -A && git commit -F commit.msg"}}],"usage":{"input_tokens":4000,"output_tokens":200,"cache_creation_input_tokens":0,"cache_read_input_tokens":3000}}}
{"type":"user","uuid":"u7","timestamp":"2026-04-25T10:05:30Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t6","content":"[main abc1234] feat(x): ship feature"}]}}
{"type":"assistant","uuid":"a7","timestamp":"2026-04-25T10:05:31Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t7","name":"ToolSearch","input":{"query":"capture_session"}}],"usage":{"input_tokens":3500,"output_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":2500}}}
{"type":"user","uuid":"u8","timestamp":"2026-04-25T10:05:40Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t7","content":"schema loaded"}]}}
{"type":"assistant","uuid":"a8","timestamp":"2026-04-25T10:05:41Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"t8","name":"mcp__plugin_vibe-vault_vibe-vault__vv_capture_session","input":{"summary":"shipped feature"}}],"usage":{"input_tokens":4000,"output_tokens":1500,"cache_creation_input_tokens":0,"cache_read_input_tokens":3000}}}
{"type":"user","uuid":"u9","timestamp":"2026-04-25T10:06:00Z","sessionId":"test-wrap","cwd":"/home/user/project","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t8","content":"session captured"}]}}`

func writeTempTranscript(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "wrap-trace-test-*.jsonl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestAnalyse_GoldenWrap(t *testing.T) {
	tr, err := transcript.Parse(strings.NewReader(goldenTranscript))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	result, err := analyse(tr)
	if err != nil {
		t.Fatalf("analyse: %v", err)
	}

	// Verify the wrap window was found and total token counts are correct.
	// vv_append_iteration turn: outTok=4000
	// vv_update_resume turn: outTok=12000
	// Write (commit.msg) turn: outTok=3000
	// Read turn: outTok=100
	// Bash turn: outTok=200
	// ToolSearch turn: outTok=50
	// vv_capture_session turn: outTok=1500
	// Total: 4000+12000+3000+100+200+50+1500 = 20850
	// (bootstrap turn outTok=75 is before wrap window, excluded)
	wantOutTok := 4000 + 12000 + 3000 + 100 + 200 + 50 + 1500
	if result.Total.outTokens != wantOutTok {
		t.Errorf("total out tokens = %d, want %d", result.Total.outTokens, wantOutTok)
	}

	// Find phase rows by label
	phaseByLabel := make(map[string]phaseRow)
	for _, r := range result.Rows {
		phaseByLabel[r.label] = r
	}

	// iterations.md narrative: vv_append_iteration → outTok=4000
	if r, ok := phaseByLabel["Compose iterations.md narrative"]; !ok {
		t.Error("missing 'Compose iterations.md narrative' phase")
	} else if r.outTokens != 4000 {
		t.Errorf("iterations out tokens = %d, want 4000", r.outTokens)
	} else if r.toolTrips != 1 {
		t.Errorf("iterations tool trips = %d, want 1", r.toolTrips)
	}

	// Open Threads / resume: vv_update_resume → outTok=12000
	if r, ok := phaseByLabel["Compose Open Threads / resume"]; !ok {
		t.Error("missing 'Compose Open Threads / resume' phase")
	} else if r.outTokens != 12000 {
		t.Errorf("resume out tokens = %d, want 12000", r.outTokens)
	}

	// commit.msg body: Write → outTok=3000
	if r, ok := phaseByLabel["Compose commit.msg body"]; !ok {
		t.Error("missing 'Compose commit.msg body' phase")
	} else if r.outTokens != 3000 {
		t.Errorf("commit.msg out tokens = %d, want 3000", r.outTokens)
	}

	// vv_capture_session: outTok=1500
	if r, ok := phaseByLabel["vv_capture_session retelling"]; !ok {
		t.Error("missing 'vv_capture_session retelling' phase")
	} else if r.outTokens != 1500 {
		t.Errorf("capture out tokens = %d, want 1500", r.outTokens)
	}

	// Total tool trips: 7 (one per tool call in wrap window)
	if result.Total.toolTrips != 7 {
		t.Errorf("total tool trips = %d, want 7", result.Total.toolTrips)
	}
}

func TestMeasure_GoldenFile(t *testing.T) {
	path := writeTempTranscript(t, goldenTranscript)

	result, err := Measure(path)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}

	if len(result.Rows) == 0 {
		t.Error("expected at least one phase row")
	}
	if result.Total.outTokens == 0 {
		t.Error("expected non-zero total output tokens")
	}

	// Verify JSON serialisation doesn't panic and produces valid JSON.
	b, err := result.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if len(b) == 0 {
		t.Error("MarshalJSON returned empty")
	}
}

func TestCanonicalTool(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"mcp__plugin_vibe-vault_vibe-vault__vv_append_iteration", "vv_append_iteration"},
		{"mcp__plugin_vibe-vault_vibe-vault__vv_update_resume", "vv_update_resume"},
		{"mcp__plugin_vibe-vault_vibe-vault__vv_capture_session", "vv_capture_session"},
		{"Write", "Write"},
		{"Bash", "Bash"},
		{"ToolSearch", "ToolSearch"},
	}
	for _, tc := range tests {
		got := canonicalTool(tc.in)
		if got != tc.want {
			t.Errorf("canonicalTool(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
