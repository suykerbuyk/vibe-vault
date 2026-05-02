// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestWrite_HappyPath verifies a basic write succeeds, the content matches,
// and the resulting file mode is 0o644.
func TestWrite_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	want := []byte("hello, atomicfile\n")

	if err := Write("", path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("mode = %o, want 0644", mode)
	}
}

// TestWrite_RenameCollision verifies that Write atomically replaces an
// existing file at the target path.
func TestWrite_RenameCollision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	want := []byte("replaced")
	if err := Write("", path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}

	// Mode is normalized to 0o644 by Write.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("mode = %o, want 0644", mode)
	}
}

// TestWrite_ParentDirMissing verifies that Write fails when the parent
// path component cannot be created (e.g., a regular file exists where a
// directory must be created). os.WriteFile would also fail here.
func TestWrite_ParentDirMissing(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file at "dir/blocker"; then try to write to
	// "dir/blocker/sub/file.txt" — MkdirAll cannot create a directory
	// where a file exists, so Write must surface an error.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	target := filepath.Join(blocker, "sub", "file.txt")

	if err := Write("", target, []byte("nope")); err == nil {
		t.Fatalf("Write succeeded; expected error when parent path is unwritable")
	}
}

// TestWrite_EmptyData verifies an empty write produces a zero-byte file.
func TestWrite_EmptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	if err := Write("", path, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 bytes, got %d", len(got))
	}
}

// TestWrite_LargeData verifies a 1 MiB write succeeds and the content is
// preserved byte-for-byte.
func TestWrite_LargeData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.bin")

	const size = 1 << 20 // 1 MiB
	want := make([]byte, size)
	for i := range want {
		want[i] = byte(i % 251)
	}

	if err := Write("", path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch at size %d", size)
	}
}

// TestWrite_VaultPathIsNoOp verifies that passing a non-empty vaultPath
// produces the same observable behavior as passing "". Phase 0a is a pure
// relocation; the vaultPath parameter is reserved for Phase 1a's
// stamp-on-success behavior and must currently be ignored.
func TestWrite_VaultPathIsNoOp(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	data := []byte("identical content")

	if err := Write("", pathA, data); err != nil {
		t.Fatalf("Write A: %v", err)
	}
	if err := Write("/some/fake/vault", pathB, data); err != nil {
		t.Fatalf("Write B: %v", err)
	}

	gotA, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatalf("ReadFile A: %v", err)
	}
	gotB, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatalf("ReadFile B: %v", err)
	}
	if !bytes.Equal(gotA, gotB) {
		t.Fatalf("vaultPath argument changed observable behavior: A=%q B=%q", gotA, gotB)
	}

	// Also confirm the fake vault path was not created as a side effect.
	if _, err := os.Stat("/some/fake/vault"); !os.IsNotExist(err) {
		t.Fatalf("vaultPath argument leaked: /some/fake/vault stat err = %v", err)
	}
}

// TestWrite_RenameFails verifies the rename-into-place error path. When
// the destination path is an existing non-empty directory, os.Rename of a
// regular file over it must fail, which exercises the rename error branch
// and the temp-file cleanup defer.
func TestWrite_RenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Place a file inside target so it is a non-empty directory; rename
	// of a regular file over a non-empty directory always fails on Linux.
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	if err := Write("", target, []byte("payload")); err == nil {
		t.Fatalf("Write succeeded; expected rename failure when target is a non-empty directory")
	}

	// The temp file must have been cleaned up by the defer.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) >= len(".vv-tmp-") && name[:len(".vv-tmp-")] == ".vv-tmp-" {
			t.Fatalf("temp file %q leaked after failed rename", name)
		}
	}
}

// TestWrite_StampsOnVaultPath verifies that a successful write under a
// recognized vault layout (Projects/<p>/agentctx/) refreshes the .surface
// file with the current MCPSurfaceVersion.
func TestWrite_StampsOnVaultPath(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	if err := os.MkdirAll(agentctxDir, 0o755); err != nil {
		t.Fatalf("seed agentctx dir: %v", err)
	}
	target := filepath.Join(agentctxDir, "foo.md")

	if err := Write(vault, target, []byte("# hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	stampPath := filepath.Join(agentctxDir, ".surface")
	got, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatalf("read .surface: %v", err)
	}
	body := string(got)
	if !bytes.Contains(got, []byte("surface = 12")) {
		t.Errorf(".surface missing surface=12; got %q", body)
	}
	if !bytes.Contains(got, []byte("last_writer")) {
		t.Errorf(".surface missing last_writer; got %q", body)
	}
	if !bytes.Contains(got, []byte("last_write_at")) {
		t.Errorf(".surface missing last_write_at; got %q", body)
	}
}

// TestWrite_NoStampOnEmptyVaultPath verifies that vaultPath="" writes
// successfully and leaves no .surface file behind. (Host-local writes must
// not stamp.)
func TestWrite_NoStampOnEmptyVaultPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "foo.md")
	if err := Write("", target, []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".surface")); !os.IsNotExist(err) {
		t.Fatalf(".surface should not exist for vaultPath=\"\"; stat err = %v", err)
	}
}

// TestWrite_StampFailureDoesNotFailWrite verifies that a stamp-side error
// (here, a stamp directory pre-occupied by a regular file with the same name
// as .surface) does NOT fail the primary write.
func TestWrite_StampFailureDoesNotFailWrite(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	if err := os.MkdirAll(agentctxDir, 0o755); err != nil {
		t.Fatalf("seed agentctx dir: %v", err)
	}
	// Create a directory at the .surface path so rename-of-file-over-dir
	// fails inside surface.WriteStamp. The primary write must still succeed.
	stampPath := filepath.Join(agentctxDir, ".surface")
	if err := os.MkdirAll(stampPath, 0o755); err != nil {
		t.Fatalf("seed stamp blocker dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stampPath, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed stamp blocker child: %v", err)
	}

	target := filepath.Join(agentctxDir, "foo.md")
	if err := Write(vault, target, []byte("primary")); err != nil {
		t.Fatalf("Write must not fail when stamping fails: %v", err)
	}
	// Primary write succeeded; verify the file is on disk.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, []byte("primary")) {
		t.Fatalf("target content mismatch: %q", got)
	}
}

// TestDirOf covers the path-component helper across a few shapes.
func TestDirOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "."},
		{"file.txt", "."},
		{"/a/b/c", "/a/b"},
		{"/a", ""},
		{"a/b", "a"},
		{`C:\foo\bar`, `C:\foo`},
	}
	for _, c := range cases {
		if got := dirOf(c.in); got != c.want {
			t.Errorf("dirOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
