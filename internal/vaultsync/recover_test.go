// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package vaultsync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

func TestRecover_PopulatedRepo(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	// Three commits in past 7 days touching a Manual-class file with
	// distinct content. Then HEAD ends up at a different fourth content
	// — the three earlier commits' blobs all differ from HEAD's blob,
	// so all three are candidates.
	manual := "Projects/p/iterations.md"
	must := func(content, msg string) {
		t.Helper()
		writeAtPath(t, dir, manual, content)
		gitx.GitRun(t, dir, "add", "-A")
		gitx.GitRun(t, dir, "commit", "-m", msg)
	}

	must("from machine A iter 100", "iter 100 (A)")
	must("from machine A iter 101", "iter 101 (A)")
	must("from machine A iter 102", "iter 102 (A)")
	must("HEAD: machine B fully overwrote", "HEAD overwrite")

	candidates, err := Recover(dir, 7)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	// The HEAD commit's blob equals HEAD's blob → not a candidate.
	// The three earlier commits all differ from HEAD → candidates.
	if got, want := len(candidates), 3; got != want {
		t.Fatalf("len(candidates) = %d, want %d (got: %+v)", got, want, candidates)
	}
	// Sorted by CommittedAt descending: the most recent (iter 102 (A))
	// should appear first among candidates.
	if !contains(candidates[0].Subject, "iter 102") {
		t.Errorf("candidates[0].Subject = %q, want contains 'iter 102'", candidates[0].Subject)
	}
	for _, c := range candidates {
		if c.SHA == "" {
			t.Errorf("candidate has empty SHA: %+v", c)
		}
		if c.Author == "" {
			t.Errorf("candidate has empty Author: %+v", c)
		}
		if c.CommittedAt.IsZero() {
			t.Errorf("candidate has zero CommittedAt: %+v", c)
		}
		found := false
		for _, f := range c.Files {
			if f == manual {
				found = true
			}
		}
		if !found {
			t.Errorf("candidate missing path %q: %+v", manual, c)
		}
	}
}

func TestRecover_EmptyRepo(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	// No commits beyond the initial. The initial commit touches
	// README.md which is a Manual-class file; its blob equals HEAD's
	// blob, so no candidates.
	candidates, err := Recover(dir, 7)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d: %+v", len(candidates), candidates)
	}
}

func TestRecover_DaysZero(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	manual := "Projects/p/iterations.md"
	writeAtPath(t, dir, manual, "some content")
	gitx.GitRun(t, dir, "add", "-A")
	gitx.GitRun(t, dir, "commit", "-m", "iter 100")

	candidates, err := Recover(dir, 0)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates with --days 0, got %d", len(candidates))
	}
}

// writeAtPath writes content to dir/relPath, creating parent
// directories as needed.
func writeAtPath(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}
