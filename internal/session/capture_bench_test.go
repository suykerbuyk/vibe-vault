// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// build50TurnFixture writes a 50-turn (100-message) JSONL transcript
// to a tempdir and returns the path. The fixture is shaped like a
// realistic Claude Code session: alternating user/assistant turns,
// every other assistant turn carries a tool_use block, modest token
// usage, valid UUIDs and timestamps. The shape matters for the
// downstream parse-narrative-prose-extract pipeline; a fixture that
// triggers no tool-use code paths would understate the parse cost.
func build50TurnFixture(b *testing.B) string {
	b.Helper()
	var sb strings.Builder
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		uts := base.Add(time.Duration(i*60) * time.Second).Format(time.RFC3339)
		fmt.Fprintf(&sb,
			`{"type":"user","uuid":"u%d","sessionId":"bench-50turn","timestamp":"%s","cwd":"/home/dev/benchproj","gitBranch":"main","message":{"role":"user","content":"Step %d: continue working on the feature"}}`+"\n",
			i, uts, i)
		ats := base.Add(time.Duration(i*60+30) * time.Second).Format(time.RFC3339)
		if i%2 == 0 {
			fmt.Fprintf(&sb,
				`{"type":"assistant","uuid":"a%d","sessionId":"bench-50turn","timestamp":"%s","cwd":"/home/dev/benchproj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Working on step %d."},{"type":"tool_use","id":"tu%d","name":"Edit","input":{"file_path":"/home/dev/benchproj/src/file_%d.go","old_string":"// step","new_string":"// step %d done"}}],"usage":{"input_tokens":300,"output_tokens":120,"cache_creation_input_tokens":50,"cache_read_input_tokens":150}}}`+"\n",
				i, ats, i, i, i, i)
		} else {
			fmt.Fprintf(&sb,
				`{"type":"assistant","uuid":"a%d","sessionId":"bench-50turn","timestamp":"%s","cwd":"/home/dev/benchproj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Step %d acknowledged; continuing."}],"usage":{"input_tokens":250,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}`+"\n",
				i, ats, i)
		}
	}
	dir := b.TempDir()
	path := filepath.Join(dir, "bench-50turn.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkCapture measures the cost of the full Capture pipeline
// on a 50-turn JSONL fixture. Establishes a perf-neutral baseline
// for the γ Phase 1 routing fork — running this before and after
// the refactor must show throughput within ±5% of the pre-refactor
// number.
//
// Run with:
//
//	go test ./internal/session/ -run='^$' -bench=BenchmarkCapture -benchtime=10x
func BenchmarkCapture(b *testing.B) {
	transcriptPath := build50TurnFixture(b)
	cfg := config.Config{VaultPath: b.TempDir()}
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "benchproj", "sessions"), 0o755)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each iteration uses a fresh session id so dedup does
		// not short-circuit the work — the routing fork's cost
		// is what we want to measure, not dedup-skip latency.
		idx := &index.Index{Entries: make(map[string]index.SessionEntry)}
		opts := CaptureOpts{
			TranscriptPath: transcriptPath,
			SessionID:      fmt.Sprintf("bench-50turn-%d", i),
			CWD:            "/home/dev/benchproj",
			Index:          idx,
			SkipEnrichment: true,
		}
		if _, err := Capture(opts, cfg); err != nil {
			b.Fatalf("Capture: %v", err)
		}
	}
}
