// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package wrapmetrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// makeDispatchLine returns a minimal DispatchLine for a given iter.
func makeDispatchLine(iter int, tier string, outcome string) DispatchLine {
	return DispatchLine{
		Iter:                   iter,
		TS:                     "2026-04-25T17:00:00Z",
		AgentDefinitionSha256:  "deadbeef",
		AgentDefinitionVersion: "1.0",
		TierAttempts: []TierAttempt{{
			Tier:          tier,
			ProviderModel: "anthropic:claude-" + tier + "-stub",
			DurationMs:    1234,
			Outcome:       outcome,
			InputTokens:   100,
			OutputTokens:  50,
		}},
		ModelUsed:       tier,
		EscalatedFrom:   nil,
		TotalDurationMs: 1234,
	}
}

// TestWriteDispatchLine_AppendsAtomic writes 3 lines then reads them back
// and asserts content + order.
func TestWriteDispatchLine_AppendsAtomic(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	for i := 1; i <= 3; i++ {
		if err := WriteDispatchLine(makeDispatchLine(i, "sonnet", "ok")); err != nil {
			t.Fatalf("WriteDispatchLine %d: %v", i, err)
		}
	}

	lines, err := ReadDispatchLines(0)
	if err != nil {
		t.Fatalf("ReadDispatchLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("len = %d, want 3", len(lines))
	}
	for i, ln := range lines {
		if ln.Iter != i+1 {
			t.Errorf("lines[%d].Iter = %d, want %d", i, ln.Iter, i+1)
		}
		if len(ln.TierAttempts) != 1 || ln.TierAttempts[0].Tier != "sonnet" {
			t.Errorf("lines[%d].TierAttempts unexpected: %+v", i, ln.TierAttempts)
		}
	}
}

// TestWriteDispatchLine_ConcurrentSafe drives 10 goroutines writing 10
// lines each (total 100) and asserts no torn lines.
func TestWriteDispatchLine_ConcurrentSafe(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	const goroutines = 10
	const perGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines*perGoroutine)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				line := makeDispatchLine(g*perGoroutine+i+1, "sonnet", "ok")
				if err := WriteDispatchLine(line); err != nil {
					errCh <- fmt.Errorf("goroutine %d line %d: %w", g, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("write: %v", err)
	}

	lines, err := ReadDispatchLines(0)
	if err != nil {
		t.Fatalf("ReadDispatchLines: %v", err)
	}
	if len(lines) != goroutines*perGoroutine {
		t.Fatalf("len = %d, want %d", len(lines), goroutines*perGoroutine)
	}
}

// TestReadDispatchLines_Limit writes 50 lines and reads only the most
// recent 10.
func TestReadDispatchLines_Limit(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	for i := 1; i <= 50; i++ {
		if err := WriteDispatchLine(makeDispatchLine(i, "sonnet", "ok")); err != nil {
			t.Fatalf("WriteDispatchLine %d: %v", i, err)
		}
	}

	lines, err := ReadDispatchLines(10)
	if err != nil {
		t.Fatalf("ReadDispatchLines: %v", err)
	}
	if len(lines) != 10 {
		t.Fatalf("len = %d, want 10", len(lines))
	}
	if lines[0].Iter != 41 {
		t.Errorf("first line iter = %d, want 41 (most-recent 10 of 50)", lines[0].Iter)
	}
	if lines[9].Iter != 50 {
		t.Errorf("last line iter = %d, want 50", lines[9].Iter)
	}
}

// TestReadDispatchLines_HandlesPartialLine writes a torn final line and
// asserts the reader skips it without erroring.
func TestReadDispatchLines_HandlesPartialLine(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	if err := WriteDispatchLine(makeDispatchLine(1, "sonnet", "ok")); err != nil {
		t.Fatalf("WriteDispatchLine: %v", err)
	}

	// Append a non-newline-terminated half-line to simulate a crash mid-flush.
	path, err := DispatchPath()
	if err != nil {
		t.Fatalf("DispatchPath: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, werr := f.WriteString(`{"iter":2,"ts":"2026-04-25T17:00:00Z","tier_at`); werr != nil {
		t.Fatalf("write torn: %v", werr)
	}
	f.Close()

	lines, err := ReadDispatchLines(0)
	if err != nil {
		t.Fatalf("ReadDispatchLines: %v", err)
	}
	if len(lines) != 1 || lines[0].Iter != 1 {
		t.Errorf("got %d lines (first iter=%d), want 1 line iter=1 (torn final skipped)",
			len(lines), func() int {
				if len(lines) > 0 {
					return lines[0].Iter
				}
				return -1
			}())
	}
}

// TestDispatchPath_LivesInWrapMetricsCacheDir asserts the file is sibling
// to the existing wrap-metrics.jsonl, not in some unrelated location.
func TestDispatchPath_LivesInWrapMetricsCacheDir(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	cacheDir, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	dPath, err := DispatchPath()
	if err != nil {
		t.Fatalf("DispatchPath: %v", err)
	}

	wantPath := filepath.Join(cacheDir, DispatchActiveFile)
	if dPath != wantPath {
		t.Errorf("DispatchPath = %q, want %q", dPath, wantPath)
	}
	if filepath.Dir(dPath) != filepath.Dir(filepath.Join(cacheDir, ActiveFile)) {
		t.Errorf("dispatch + drift files in different dirs: %q vs %q",
			filepath.Dir(dPath), filepath.Dir(filepath.Join(cacheDir, ActiveFile)))
	}
	if !strings.HasSuffix(dPath, "wrap-dispatch.jsonl") {
		t.Errorf("DispatchPath suffix unexpected: %q", dPath)
	}
}

// TestReadDispatchLines_MissingFile returns nil with no error.
func TestReadDispatchLines_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	lines, err := ReadDispatchLines(0)
	if err != nil {
		t.Fatalf("ReadDispatchLines on missing file: %v", err)
	}
	if lines != nil {
		t.Errorf("lines = %v, want nil", lines)
	}
}
