// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
)

// seedSkeleton writes a skeleton to the cache and returns the handle.
func seedSkeleton(t *testing.T, facts SkeletonFacts) SkeletonHandle {
	t.Helper()
	sk := BuildSkeleton(facts)
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		t.Fatalf("marshal skeleton: %v", err)
	}
	path, sha, err := wrapbundlecache.Write(facts.Iter, data)
	if err != nil {
		t.Fatalf("Write skeleton: %v", err)
	}
	return SkeletonHandle{
		Iter:           facts.Iter,
		SkeletonPath:   path,
		SkeletonSHA256: sha,
	}
}

func TestVVSynthesizeWrapBundle_HappyPath(t *testing.T) {
	withSkeletonCacheDir(t)

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:         12,
		Project:      "myproject",
		FilesChanged: []string{"a.go", "b.go"},
		ResumeThreadBlocks: []SkeletonThreadOpen{
			{Slug: "open-thread"},
		},
	})

	tool := NewSynthesizeWrapTool(config.Config{})
	args := map[string]any{
		"skeleton_handle":     handle,
		"iteration_narrative": "Did stuff.",
		"iteration_title":     "Phase 3a",
		"commit_subject":      "feat(mcp): test",
		"thread_bodies": map[string]string{
			"open-thread": "Body of the open thread.",
		},
		"capture_summary": "Wrap summary.",
	}
	params, _ := json.Marshal(args)
	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var bundle WrapBundle
	if err := json.Unmarshal([]byte(out), &bundle); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}

	if bundle.Iteration != 12 {
		t.Errorf("Iteration=%d, want 12", bundle.Iteration)
	}
	if !strings.Contains(bundle.IterationBlock.Content, "Phase 3a") {
		t.Errorf("iteration block missing title: %s", bundle.IterationBlock.Content)
	}
	if !strings.Contains(bundle.CommitMsg.Content, "feat(mcp): test") {
		t.Errorf("commit subject missing: %s", bundle.CommitMsg.Content)
	}
	if len(bundle.ResumeThreadBlocks) != 1 {
		t.Fatalf("threads len=%d, want 1", len(bundle.ResumeThreadBlocks))
	}
	if bundle.ResumeThreadBlocks[0].Body != "Body of the open thread." {
		t.Errorf("thread body=%q", bundle.ResumeThreadBlocks[0].Body)
	}
	if bundle.CaptureSession.Content.Summary != "Wrap summary." {
		t.Errorf("capture summary=%q", bundle.CaptureSession.Content.Summary)
	}
}

func TestVVSynthesizeWrapBundle_DetectsTamperedSkeleton(t *testing.T) {
	withSkeletonCacheDir(t)

	handle := seedSkeleton(t, SkeletonFacts{Iter: 4, Project: "p"})
	// Mutate the file on disk.
	if err := os.WriteFile(handle.SkeletonPath, []byte(`{"iter":99,"project":"hacked"}`), 0o600); err != nil {
		t.Fatalf("mutate skeleton: %v", err)
	}

	tool := NewSynthesizeWrapTool(config.Config{})
	args := map[string]any{"skeleton_handle": handle}
	params, _ := json.Marshal(args)
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatalf("expected sha-mismatch error")
	}
	if !strings.Contains(err.Error(), "modified") && !strings.Contains(err.Error(), "sha") {
		t.Errorf("error=%q, want sha-mismatch wording", err.Error())
	}
}

func TestVVSynthesizeWrapBundle_RejectsMissingHandle(t *testing.T) {
	withSkeletonCacheDir(t)
	tool := NewSynthesizeWrapTool(config.Config{})

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"empty handle", map[string]any{"skeleton_handle": map[string]any{}}, "iter must be > 0"},
		{"missing path", map[string]any{"skeleton_handle": map[string]any{"iter": 1}}, "skeleton_path is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(tc.args)
			_, err := tool.Handler(params)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error=%q, want contains %q", err.Error(), tc.want)
			}
		})
	}
}

func TestVVSynthesizeWrapBundle_BundleNotCached(t *testing.T) {
	dir := withSkeletonCacheDir(t)

	handle := seedSkeleton(t, SkeletonFacts{Iter: 7, Project: "p"})

	// Snapshot cache contents after the prepare call.
	beforeEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	tool := NewSynthesizeWrapTool(config.Config{})
	args := map[string]any{
		"skeleton_handle":     handle,
		"iteration_narrative": "x",
		"iteration_title":     "t",
		"commit_subject":      "chore: t",
	}
	params, _ := json.Marshal(args)
	if _, herr := tool.Handler(params); herr != nil {
		t.Fatalf("Handler: %v", herr)
	}

	afterEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(afterEntries) != len(beforeEntries) {
		t.Errorf("cache dir grew after synthesize: before=%d after=%d (bundle should NOT be cached)",
			len(beforeEntries), len(afterEntries))
	}
	// And the only file in dir is the skeleton from seedSkeleton.
	want := filepath.Base(handle.SkeletonPath)
	for _, e := range afterEntries {
		if e.Name() != want {
			t.Errorf("unexpected file in cache: %q", e.Name())
		}
	}
}

func TestVVSynthesizeWrapBundle_PreservesThreadReplaceBodies(t *testing.T) {
	withSkeletonCacheDir(t)

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    9,
		Project: "p",
		ResumeThreadsReplace: []SkeletonThreadReplace{
			{Slug: "alpha"},
			{Slug: "beta"},
		},
	})

	tool := NewSynthesizeWrapTool(config.Config{})
	args := map[string]any{
		"skeleton_handle":     handle,
		"iteration_narrative": "n",
		"iteration_title":     "t",
		"commit_subject":      "chore: t",
		"thread_bodies": map[string]string{
			"alpha": "alpha-body",
			"beta":  "beta-body",
		},
	}
	params, _ := json.Marshal(args)
	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var bundle WrapBundle
	if err := json.Unmarshal([]byte(out), &bundle); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(bundle.ResumeThreadsReplace) != 2 {
		t.Fatalf("replace len=%d", len(bundle.ResumeThreadsReplace))
	}
	bodies := map[string]string{}
	for _, r := range bundle.ResumeThreadsReplace {
		bodies[r.Slug] = r.Body
	}
	if bodies["alpha"] != "alpha-body" || bodies["beta"] != "beta-body" {
		t.Errorf("replace bodies=%v", bodies)
	}
}

// TestFingerprintString_Deterministic verifies the same input always produces
// the same fingerprint.
func TestFingerprintString_Deterministic(t *testing.T) {
	for i := 0; i < 5; i++ {
		got := fingerprintString("hello world")
		if fingerprintString("hello world") != got {
			t.Error("fingerprintString is not deterministic")
		}
	}
	if fingerprintString("a") == fingerprintString("b") {
		t.Error("different inputs produced same fingerprint")
	}
}

// TestFirstNWords returns the first n words of a string.
func TestFirstNWords(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"one two three four five", 3, "one two three"},
		{"short", 10, "short"},
		{"", 5, ""},
		{"a b c", 3, "a b c"},
	}
	for _, tc := range cases {
		got := firstNWords(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("firstNWords(%q, %d)=%q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}
