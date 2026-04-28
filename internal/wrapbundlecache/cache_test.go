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
	"sync"
	"testing"
)

// withTempCacheDir overrides CacheDir() to a t.TempDir() for isolation.
// It also resets legacyMigrationOnce so each test gets a fresh migration
// window (some tests seed legacy files before the first CacheDir() call).
func withTempCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := cacheDirOverride
	cacheDirOverride = dir
	legacyMigrationOnce = sync.Once{}
	t.Cleanup(func() {
		cacheDirOverride = prev
		legacyMigrationOnce = sync.Once{}
	})
	return dir
}

func TestCache_WriteAndRead_RoundTrip(t *testing.T) {
	dir := withTempCacheDir(t)

	payload := []byte(`{"iter":42,"project":"demo"}`)
	path, sum, err := Write("alpha", 42, payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantPath := filepath.Join(dir, "alpha", "iter-42-skeleton.json")
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
	if _, _, err := Write("alpha", 7, []byte(`{"x":1}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// After Write, no *.tmp files should remain in the project subdir.
	projDir := filepath.Join(dir, "alpha")
	entries, err := os.ReadDir(projDir)
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
		if _, _, err := Write("alpha", iter, []byte("{}")); err != nil {
			t.Fatalf("Write iter=%d: %v", iter, err)
		}
	}
	deleted, err := RotateKeepN("alpha", 3)
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
		path, _ := SkeletonPath("alpha", iter)
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("iter %d should still exist: %v", iter, statErr)
		}
	}
	// Verify {1,5} are gone.
	for _, iter := range []int{1, 5} {
		path, _ := SkeletonPath("alpha", iter)
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("iter %d should have been deleted", iter)
		}
	}
}

func TestCache_RotateKeepN_FewerThanN(t *testing.T) {
	withTempCacheDir(t)

	for _, iter := range []int{3, 4} {
		if _, _, err := Write("alpha", iter, []byte("{}")); err != nil {
			t.Fatalf("Write iter=%d: %v", iter, err)
		}
	}
	deleted, err := RotateKeepN("alpha", 3)
	if err != nil {
		t.Fatalf("RotateKeepN: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted=%v, want []", deleted)
	}
	for _, iter := range []int{3, 4} {
		path, _ := SkeletonPath("alpha", iter)
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

	// And a project-shaped path that climbs out via .. should still reject.
	if _, err := Read(filepath.Join(other, "..", "..", "etc", "passwd")); err == nil {
		t.Errorf("Read with relative escape should error")
	}
}

func TestCache_RotateKeepN_RejectsZero(t *testing.T) {
	withTempCacheDir(t)
	if _, err := RotateKeepN("alpha", 0); err == nil {
		t.Errorf("RotateKeepN(0) should error")
	}
}

// TestCache_RotateKeepN_PerProjectIsolation verifies the cross-project
// eviction bug the per-project layout fixes: alpha's iter-10 skeleton must
// survive a rotation pass even when beta has higher-numbered iters resident.
func TestCache_RotateKeepN_PerProjectIsolation(t *testing.T) {
	withTempCacheDir(t)

	if _, _, err := Write("alpha", 10, []byte("{}")); err != nil {
		t.Fatalf("Write alpha/10: %v", err)
	}
	for _, iter := range []int{100, 101, 102} {
		if _, _, err := Write("beta", iter, []byte("{}")); err != nil {
			t.Fatalf("Write beta/%d: %v", iter, err)
		}
	}

	if _, err := RotateKeepN("alpha", 2); err != nil {
		t.Fatalf("RotateKeepN(alpha): %v", err)
	}
	if _, err := RotateKeepN("beta", 2); err != nil {
		t.Fatalf("RotateKeepN(beta): %v", err)
	}

	// alpha/10 must still be there — no cross-project eviction.
	alphaPath, _ := SkeletonPath("alpha", 10)
	if _, err := os.Stat(alphaPath); err != nil {
		t.Errorf("alpha iter-10 evicted by beta's higher iters: %v", err)
	}
	// beta/100 must be the one beta lost (lowest of three, keeping 2).
	betaGone, _ := SkeletonPath("beta", 100)
	if _, err := os.Stat(betaGone); err == nil {
		t.Errorf("beta iter-100 should have been rotated out")
	}
	for _, iter := range []int{101, 102} {
		p, _ := SkeletonPath("beta", iter)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("beta iter-%d should still exist: %v", iter, err)
		}
	}
}

// TestCache_Write_CreatesProjectSubdir verifies a write to a project with
// no pre-existing subdirectory creates it at 0o700.
func TestCache_Write_CreatesProjectSubdir(t *testing.T) {
	dir := withTempCacheDir(t)

	if _, _, err := Write("newproj", 1, []byte("{}")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	subdir := filepath.Join(dir, "newproj")
	st, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("stat subdir: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("expected directory at %s", subdir)
	}
	if mode := st.Mode().Perm(); mode != 0o700 {
		t.Errorf("subdir perm=%o, want 0700", mode)
	}
}

// TestCache_LegacyMigration_RelocatesRootFiles seeds a root-level skeleton
// (pre-migration shape) and verifies the first CacheDir() call relocates
// it into <base>/_legacy/.
func TestCache_LegacyMigration_RelocatesRootFiles(t *testing.T) {
	dir := withTempCacheDir(t)

	// Seed a legacy root-level skeleton BEFORE the first CacheDir call.
	legacyName := "iter-5-skeleton.json"
	legacySrc := filepath.Join(dir, legacyName)
	if err := os.WriteFile(legacySrc, []byte(`{"legacy":true}`), 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	// First CacheDir call triggers the migration.
	if _, err := CacheDir("x"); err != nil {
		t.Fatalf("CacheDir: %v", err)
	}

	// Source should be gone, dest should exist.
	if _, err := os.Stat(legacySrc); err == nil {
		t.Errorf("legacy file %s should have been relocated", legacySrc)
	}
	legacyDst := filepath.Join(dir, "_legacy", legacyName)
	got, err := os.ReadFile(legacyDst)
	if err != nil {
		t.Fatalf("read relocated legacy: %v", err)
	}
	if string(got) != `{"legacy":true}` {
		t.Errorf("legacy content lost: %q", got)
	}

	// _legacy/ itself should be 0700.
	legacyDir := filepath.Join(dir, "_legacy")
	st, err := os.Stat(legacyDir)
	if err != nil {
		t.Fatalf("stat _legacy: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o700 {
		t.Errorf("_legacy perm=%o, want 0700", mode)
	}
}

// TestCache_LegacyMigration_Idempotent verifies a second migration pass is
// a no-op: a file landing in _legacy/ from the first pass stays put and
// content is unchanged.
func TestCache_LegacyMigration_Idempotent(t *testing.T) {
	dir := withTempCacheDir(t)

	legacyName := "iter-7-skeleton.json"
	legacySrc := filepath.Join(dir, legacyName)
	if err := os.WriteFile(legacySrc, []byte(`{"orig":1}`), 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	if _, err := CacheDir("x"); err != nil {
		t.Fatalf("CacheDir #1: %v", err)
	}

	legacyDst := filepath.Join(dir, "_legacy", legacyName)
	beforeStat, err := os.Stat(legacyDst)
	if err != nil {
		t.Fatalf("stat after first migration: %v", err)
	}
	beforeMtime := beforeStat.ModTime()

	// Force the Once to allow re-entry — simulates another process /
	// another freshly-loaded module instance hitting the same base. The
	// idempotency guarantee is that a second call with no root-level
	// legacy files left is a no-op.
	legacyMigrationOnce = sync.Once{}
	if _, err2 := CacheDir("x"); err2 != nil {
		t.Fatalf("CacheDir #2: %v", err2)
	}

	afterStat, err := os.Stat(legacyDst)
	if err != nil {
		t.Fatalf("stat after second migration: %v", err)
	}
	if !afterStat.ModTime().Equal(beforeMtime) {
		t.Errorf("legacy file mtime changed across idempotent migration: before=%v after=%v",
			beforeMtime, afterStat.ModTime())
	}
	got, err := os.ReadFile(legacyDst)
	if err != nil {
		t.Fatalf("read legacy: %v", err)
	}
	if string(got) != `{"orig":1}` {
		t.Errorf("legacy content mutated: %q", got)
	}
	// And no fresh root-level file appeared.
	if _, err := os.Stat(filepath.Join(dir, legacyName)); err == nil {
		t.Errorf("root-level legacy file reappeared after second migration")
	}
}

// TestCache_RotateKeepN_IgnoresLegacyDir seeds a file in _legacy/ plus 5
// fresh skeletons under a project; rotation must touch only the project
// directory, leaving _legacy/ contents intact.
func TestCache_RotateKeepN_IgnoresLegacyDir(t *testing.T) {
	dir := withTempCacheDir(t)

	// Seed _legacy/ directly (skip the migration trigger; we want a
	// resident file regardless of the migration path).
	legacyDir := filepath.Join(dir, "_legacy")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir _legacy: %v", err)
	}
	legacyFile := filepath.Join(legacyDir, "iter-999-skeleton.json")
	if err := os.WriteFile(legacyFile, []byte(`{"legacy":true}`), 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	for _, iter := range []int{1, 2, 3, 4, 5} {
		if _, _, err := Write("alpha", iter, []byte("{}")); err != nil {
			t.Fatalf("Write alpha/%d: %v", iter, err)
		}
	}

	deleted, err := RotateKeepN("alpha", 2)
	if err != nil {
		t.Fatalf("RotateKeepN: %v", err)
	}
	if len(deleted) != 3 {
		t.Errorf("deleted=%d, want 3", len(deleted))
	}

	// _legacy/iter-999-skeleton.json must be untouched.
	if _, statErr := os.Stat(legacyFile); statErr != nil {
		t.Errorf("legacy file should still exist after rotation: %v", statErr)
	}
	got, err := os.ReadFile(legacyFile)
	if err != nil {
		t.Fatalf("read legacy: %v", err)
	}
	if string(got) != `{"legacy":true}` {
		t.Errorf("legacy content mutated: %q", got)
	}
}
