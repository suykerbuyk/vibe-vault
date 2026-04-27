// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
)

// withSkeletonCacheDir routes wrapbundlecache to a t.TempDir() for the
// duration of the test, and (only if the caller hasn't already pinned it)
// sets VIBE_VAULT_HOME to a sibling tempdir so any wrapmetrics.CacheDir()
// reads/writes triggered downstream by handler tests (notably
// vv_wrap_dispatch's DispatchLine emission) land in test scratch storage
// rather than polluting ~/.cache/vibe-vault/wrap-dispatch.jsonl.
func withSkeletonCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wrapbundlecache.SetCacheDirForTesting(dir)
	t.Cleanup(func() { wrapbundlecache.SetCacheDirForTesting("") })
	if os.Getenv("VIBE_VAULT_HOME") == "" {
		t.Setenv("VIBE_VAULT_HOME", t.TempDir())
	}
	return dir
}

func TestVVPrepareWrapSkeleton_HappyPath(t *testing.T) {
	dir := withSkeletonCacheDir(t)
	tool := NewPrepareWrapSkeletonTool()

	args := map[string]any{
		"iter":             5,
		"project":          "vibe-vault",
		"files_changed":    []string{"a.go", "b.go"},
		"test_count_delta": 7,
		"decisions":        []string{"chose A"},
		"threads_to_open": []map[string]any{
			{"slug": "new-thread", "anchor_after": "carried-forward"},
		},
		"carried_to_add": []map[string]any{
			{"slug": "carry-1", "title": "First carry"},
		},
	}
	params, _ := json.Marshal(args)
	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var resp struct {
		Iter           int    `json:"iter"`
		SkeletonPath   string `json:"skeleton_path"`
		SkeletonSHA256 string `json:"skeleton_sha256"`
	}
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
		t.Fatalf("unmarshal response: %v\n%s", jerr, out)
	}
	if resp.Iter != 5 {
		t.Errorf("iter=%d, want 5", resp.Iter)
	}
	wantPath := filepath.Join(dir, "iter-5-skeleton.json")
	if resp.SkeletonPath != wantPath {
		t.Errorf("path=%q, want %q", resp.SkeletonPath, wantPath)
	}

	// Verify the cache file exists and its sha matches.
	data, err := os.ReadFile(resp.SkeletonPath)
	if err != nil {
		t.Fatalf("read skeleton file: %v", err)
	}
	sum := sha256.Sum256(data)
	wantSHA := hex.EncodeToString(sum[:])
	if resp.SkeletonSHA256 != wantSHA {
		t.Errorf("sha mismatch: got %s, want %s", resp.SkeletonSHA256, wantSHA)
	}

	// And the skeleton JSON has the orchestrator facts inside.
	var sk WrapSkeleton
	if err := json.Unmarshal(data, &sk); err != nil {
		t.Fatalf("unmarshal skeleton: %v", err)
	}
	if sk.Iter != 5 || sk.Project != "vibe-vault" {
		t.Errorf("skeleton iter/project=%d/%q", sk.Iter, sk.Project)
	}
	if len(sk.ResumeThreadBlocks) != 1 || sk.ResumeThreadBlocks[0].Slug != "new-thread" {
		t.Errorf("threads_to_open lost: %v", sk.ResumeThreadBlocks)
	}
	if len(sk.CarriedChangesAdd) != 1 || sk.CarriedChangesAdd[0].Title != "First carry" {
		t.Errorf("carried_to_add lost: %v", sk.CarriedChangesAdd)
	}
}

func TestVVPrepareWrapSkeleton_RejectsMissingRequired(t *testing.T) {
	withSkeletonCacheDir(t)
	tool := NewPrepareWrapSkeletonTool()

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing iter", map[string]any{"project": "p"}, "iter is required"},
		{"iter zero", map[string]any{"iter": 0, "project": "p"}, "iter is required"},
		{"missing project", map[string]any{"iter": 1}, "project is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(tc.args)
			_, err := tool.Handler(params)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error=%q, want contains %q", err.Error(), tc.want)
			}
		})
	}
}

func TestVVPrepareWrapSkeleton_RotatesAfterWrite(t *testing.T) {
	dir := withSkeletonCacheDir(t)
	tool := NewPrepareWrapSkeletonTool()

	// Pre-populate three skeletons.
	for _, iter := range []int{1, 2, 3} {
		args := map[string]any{"iter": iter, "project": "p"}
		params, _ := json.Marshal(args)
		if _, err := tool.Handler(params); err != nil {
			t.Fatalf("seed iter=%d: %v", iter, err)
		}
	}
	// Write a 4th — RotateKeepN(3) should remove iter 1.
	args := map[string]any{"iter": 4, "project": "p"}
	params, _ := json.Marshal(args)
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("write iter=4: %v", err)
	}

	for _, iter := range []int{2, 3, 4} {
		path := filepath.Join(dir, "iter-"+itoa(iter)+"-skeleton.json")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("iter %d should still exist: %v", iter, err)
		}
	}
	gone := filepath.Join(dir, "iter-1-skeleton.json")
	if _, err := os.Stat(gone); err == nil {
		t.Errorf("iter 1 should have been rotated out")
	}
}

func TestVVPrepareWrapSkeleton_AcceptsThreadReplace(t *testing.T) {
	dir := withSkeletonCacheDir(t)
	tool := NewPrepareWrapSkeletonTool()

	args := map[string]any{
		"iter":    9,
		"project": "p",
		"threads_to_replace": []map[string]any{
			{"slug": "old-thread"},
			{"slug": "another-thread"},
		},
	}
	params, _ := json.Marshal(args)
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "iter-9-skeleton.json"))
	if err != nil {
		t.Fatalf("read skeleton: %v", err)
	}
	var sk WrapSkeleton
	if err := json.Unmarshal(data, &sk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(sk.ResumeThreadsReplace) != 2 {
		t.Fatalf("replace len=%d, want 2", len(sk.ResumeThreadsReplace))
	}
	if sk.ResumeThreadsReplace[0].Slug != "old-thread" || sk.ResumeThreadsReplace[1].Slug != "another-thread" {
		t.Errorf("replace slugs=%v", sk.ResumeThreadsReplace)
	}
}

// itoa is a tiny helper to avoid pulling strconv into the test file.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
