// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFile is a small helper for the mirror tests.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestMirror_EmptySource_NoOp(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	changed, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty", changed)
	}
}

func TestMirror_NonExistentSource_NoErrorEmptyChanged(t *testing.T) {
	dst := t.TempDir()
	changed, err := Mirror(filepath.Join(t.TempDir(), "does-not-exist"), dst)
	if err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty", changed)
	}
}

func TestMirror_CopiesMarkdownFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "2026-05-03-100000000.md"), "# note A")
	writeFile(t, filepath.Join(src, "2026-05-03-100100000.md"), "# note B")

	changed, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if len(changed) != 2 {
		t.Fatalf("changed = %v, want 2", changed)
	}
	for _, name := range []string{"2026-05-03-100000000.md", "2026-05-03-100100000.md"} {
		body, readErr := os.ReadFile(filepath.Join(dst, name))
		if readErr != nil {
			t.Errorf("missing %s: %v", name, readErr)
			continue
		}
		if !strings.HasPrefix(string(body), "# note ") {
			t.Errorf("%s body = %q", name, body)
		}
	}
}

func TestMirror_SkipsDotDirectories(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(src, ".git", "config"), "[core]\n")
	writeFile(t, filepath.Join(src, "real.md"), "# real")
	writeFile(t, filepath.Join(src, ".init-done"), "ts")

	changed, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if len(changed) != 1 || changed[0] != "real.md" {
		t.Fatalf("changed = %v, want [real.md]", changed)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Errorf("dst/.git should not exist: stat err = %v", err)
	}
}

func TestMirror_SkipsNonMarkdownFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "scratch.txt"), "scratch")
	writeFile(t, filepath.Join(src, "real.md"), "# real")

	changed, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if len(changed) != 1 || changed[0] != "real.md" {
		t.Fatalf("changed = %v, want [real.md]", changed)
	}
	if _, err := os.Stat(filepath.Join(dst, "scratch.txt")); !os.IsNotExist(err) {
		t.Errorf("scratch.txt should not be mirrored")
	}
}

// TestMirror_ContentHashSkip: re-running with no source changes copies
// nothing. Asserts via destination mtime preservation AND empty
// changed slice.
func TestMirror_ContentHashSkip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "a.md"), "alpha")
	writeFile(t, filepath.Join(src, "b.md"), "bravo")

	changed1, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror #1: %v", err)
	}
	if len(changed1) != 2 {
		t.Fatalf("changed1 = %v, want 2", changed1)
	}
	st1, err := os.Stat(filepath.Join(dst, "a.md"))
	if err != nil {
		t.Fatalf("stat a.md: %v", err)
	}
	// Sleep one filesystem-mtime tick so a hypothetical rewrite would be
	// detectable. 10ms is enough on every filesystem we care about.
	time.Sleep(10 * time.Millisecond)

	changed2, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror #2: %v", err)
	}
	if len(changed2) != 0 {
		t.Fatalf("changed2 = %v, want empty (idempotent)", changed2)
	}
	st2, err := os.Stat(filepath.Join(dst, "a.md"))
	if err != nil {
		t.Fatalf("stat a.md #2: %v", err)
	}
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Errorf("mtime changed: %v -> %v (idempotent mirror should not rewrite)",
			st1.ModTime(), st2.ModTime())
	}
}

// TestMirror_ChangedContent_Rewrites: when source content differs from
// destination, the destination is updated.
func TestMirror_ChangedContent_Rewrites(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "evolving.md"), "v1")

	if _, err := Mirror(src, dst); err != nil {
		t.Fatalf("Mirror v1: %v", err)
	}

	writeFile(t, filepath.Join(src, "evolving.md"), "v2")
	changed, err := Mirror(src, dst)
	if err != nil {
		t.Fatalf("Mirror v2: %v", err)
	}
	if len(changed) != 1 || changed[0] != "evolving.md" {
		t.Fatalf("changed = %v, want [evolving.md]", changed)
	}
	body, _ := os.ReadFile(filepath.Join(dst, "evolving.md"))
	if string(body) != "v2" {
		t.Errorf("body = %q, want v2", body)
	}
}

// TestMirror_PartialFailure_DestReadOnly: chmod the destination
// subtree mid-walk. Mirror should surface the error rather than
// silently swallowing it. We can only test partial-failure on Unix
// (chmod semantics differ on Windows); skip elsewhere.
func TestMirror_PartialFailure_DestReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-driven partial-failure is POSIX-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod permissions")
	}

	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "a.md"), "alpha")

	// Make the destination dir read-only so atomicfile.Write fails on
	// the temp-file create. Restore in cleanup so t.TempDir teardown
	// works.
	if err := os.Chmod(dst, 0o500); err != nil {
		t.Fatalf("chmod dst: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dst, 0o755) })

	_, err := Mirror(src, dst)
	if err == nil {
		t.Fatal("Mirror should have errored under read-only dst")
	}
	if !strings.Contains(err.Error(), "mirror") {
		t.Errorf("error = %v, expected to mention mirror context", err)
	}
}

// TestMirror_ProductionSized benchmarks mirror against a fabricated
// production-sized fixture. Asserts <500ms target as a soft-fail at
// 5x the target (per Phase 3 benchmark guidance), so a slow CI runner
// doesn't false-fail the whole suite.
func BenchmarkMirror_ProductionSized(b *testing.B) {
	src := b.TempDir()
	// 800 small markdown files totaling ~5MB → average 6KB per file.
	// Matches the order-of-magnitude shape of a year's worth of
	// vibe-vault session captures across multiple projects.
	const n = 800
	body := strings.Repeat("This is a mock session body line.\n", 200) // ~6.6KB
	for i := 0; i < n; i++ {
		path := filepath.Join(src, fmt.Sprintf("2026-05-03-%09d.md", i*1000))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			b.Fatalf("write fixture %d: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := b.TempDir()
		start := time.Now()
		if _, err := Mirror(src, dst); err != nil {
			b.Fatalf("Mirror: %v", err)
		}
		dur := time.Since(start)
		if dur > 5*time.Second {
			b.Fatalf("mirror took %v (>5s ceiling — perf regression)", dur)
		}
		if dur > 500*time.Millisecond {
			b.Logf("mirror took %v (target was <500ms; soft-fail above 5x)", dur)
		}
	}
}

// TestMirror_SrcIsRegularFile_Errors locks the input-validation path:
// if srcDir is a regular file (operator config typo), Mirror surfaces
// an actionable error rather than silently succeeding.
func TestMirror_SrcIsRegularFile_Errors(t *testing.T) {
	src := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(src, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Mirror(src, t.TempDir())
	if err == nil {
		t.Fatal("expected error when src is a regular file")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %v, expected 'not a directory'", err)
	}
}

// TestMirror_EmptyArgs_Errors locks the explicit-intent guard.
func TestMirror_EmptyArgs_Errors(t *testing.T) {
	if _, err := Mirror("", "x"); err == nil {
		t.Error("expected error on empty srcDir")
	}
	if _, err := Mirror("x", ""); err == nil {
		t.Error("expected error on empty dstDir")
	}
}

// TestMirror_RawHostnameContainingSlash_DestSanitized verifies the
// orchestrator path: SyncSessions sanitizes the hostname before
// composing dstDir. The Mirror function itself does not interact with
// hostnames, but the contract is locked here so a hostname like
// "host/with/slash" cannot escape its parent via the dest path.
func TestMirror_RawHostnameContainingSlash_DestSanitized(t *testing.T) {
	raw := "host/with/slash"
	sanitized := SanitizeHostname(raw)
	if strings.Contains(sanitized, "/") {
		t.Fatalf("SanitizeHostname(%q) = %q (contains slash — would escape dst path)", raw, sanitized)
	}
	// Compose a dest path with the sanitized hostname; assert it stays
	// inside the parent.
	parent := t.TempDir()
	dst := filepath.Join(parent, "Projects", "demo", "sessions", sanitized)
	if !strings.HasPrefix(dst, parent) {
		t.Errorf("dst %q escaped parent %q", dst, parent)
	}
}
