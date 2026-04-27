// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package wrapbundlecache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// withTempCacheDir overrides CacheDir() to a t.TempDir() for isolation.
func withTempCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := cacheDirOverride
	cacheDirOverride = dir
	t.Cleanup(func() { cacheDirOverride = prev })
	return dir
}

func TestCache_WriteAndRead_RoundTrip(t *testing.T) {
	dir := withTempCacheDir(t)

	payload := []byte(`{"iter":42,"project":"demo"}`)
	path, sum, err := Write(42, payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantPath := filepath.Join(dir, "iter-42-skeleton.json")
	if path != wantPath {
		t.Errorf("path=%q, want %q", path, wantPath)
	}
	expected := sha256.Sum256(payload)
	if sum != hex.EncodeToString(expected[:]) {
		t.Errorf("sha256 mismatch: got %s want %s", sum, hex.EncodeToString(expected[:]))
	}

	// Read back.
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("read content=%q, want %q", got, payload)
	}

	// File mode should be 0600.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("perm=%o, want 0600", mode)
	}
}

func TestCache_AtomicWrite_NoPartialFile(t *testing.T) {
	dir := withTempCacheDir(t)
	if _, _, err := Write(7, []byte(`{"x":1}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// After Write, no *.tmp files should remain in the cache dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("found leftover temp file %q after Write", e.Name())
		}
	}

	// And exactly one final skeleton file should exist.
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-skeleton.json") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("skeleton file count=%d, want 1", count)
	}
}

func TestCache_RotateKeepN_DeletesOldest(t *testing.T) {
	withTempCacheDir(t)

	for _, iter := range []int{1, 5, 7, 10, 12} {
		if _, _, err := Write(iter, []byte("{}")); err != nil {
			t.Fatalf("Write iter=%d: %v", iter, err)
		}
	}
	deleted, err := RotateKeepN(3)
	if err != nil {
		t.Fatalf("RotateKeepN: %v", err)
	}
	// Expect iters 5 and 1 deleted (sorted descending: 12,10,7 keep; 5,1 delete).
	sort.Strings(deleted)
	gotIters := []string{}
	for _, p := range deleted {
		gotIters = append(gotIters, filepath.Base(p))
	}
	wantIters := []string{"iter-1-skeleton.json", "iter-5-skeleton.json"}
	sort.Strings(wantIters)
	if len(gotIters) != len(wantIters) {
		t.Fatalf("deleted=%v, want %v", gotIters, wantIters)
	}
	for i := range gotIters {
		if gotIters[i] != wantIters[i] {
			t.Errorf("deleted[%d]=%q, want %q", i, gotIters[i], wantIters[i])
		}
	}

	// Verify {12,10,7} are still present.
	for _, iter := range []int{7, 10, 12} {
		path, _ := SkeletonPath(iter)
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("iter %d should still exist: %v", iter, statErr)
		}
	}
	// Verify {1,5} are gone.
	for _, iter := range []int{1, 5} {
		path, _ := SkeletonPath(iter)
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("iter %d should have been deleted", iter)
		}
	}
}

func TestCache_RotateKeepN_FewerThanN(t *testing.T) {
	withTempCacheDir(t)

	for _, iter := range []int{3, 4} {
		if _, _, err := Write(iter, []byte("{}")); err != nil {
			t.Fatalf("Write iter=%d: %v", iter, err)
		}
	}
	deleted, err := RotateKeepN(3)
	if err != nil {
		t.Fatalf("RotateKeepN: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted=%v, want []", deleted)
	}
	for _, iter := range []int{3, 4} {
		path, _ := SkeletonPath(iter)
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("iter %d unexpectedly missing: %v", iter, statErr)
		}
	}
}

func TestCache_Read_RejectsTraversal(t *testing.T) {
	withTempCacheDir(t)

	// Path outside the cache dir.
	other := t.TempDir()
	outside := filepath.Join(other, "evil.json")
	if err := os.WriteFile(outside, []byte("evil"), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if _, err := Read(outside); err == nil {
		t.Errorf("Read of outside path should error")
	}

	// Path with .. traversal.
	if _, err := Read("../etc/passwd"); err == nil {
		t.Errorf("Read with traversal should error")
	}
}

func TestCache_RotateKeepN_RejectsZero(t *testing.T) {
	withTempCacheDir(t)
	if _, err := RotateKeepN(0); err == nil {
		t.Errorf("RotateKeepN(0) should error")
	}
}
