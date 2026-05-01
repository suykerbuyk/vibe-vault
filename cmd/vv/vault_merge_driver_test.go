// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

// writeStampFile encodes a Stamp{Surface: v} (with empty auxiliary fields) to
// the given path. Helper for the merge-driver tests.
func writeStampFile(t *testing.T, path string, v int) {
	t.Helper()
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(surface.Stamp{Surface: v}); err != nil {
		t.Fatalf("encode stamp: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readMergedSurface parses the integer from a stamp file written by the
// merge driver. Asserts the file is well-formed TOML.
func readMergedSurface(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var s surface.Stamp
	if err := toml.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse %s: %v\ncontents:\n%s", path, err, data)
	}
	return s.Surface
}

func mergeDriverPaths(t *testing.T, ancestorV, oursV, theirsV int) (anc, ours, theirs string) {
	t.Helper()
	dir := t.TempDir()
	anc = filepath.Join(dir, "ancestor.surface")
	ours = filepath.Join(dir, "ours.surface")
	theirs = filepath.Join(dir, "theirs.surface")
	if ancestorV >= 0 {
		writeStampFile(t, anc, ancestorV)
	}
	writeStampFile(t, ours, oursV)
	writeStampFile(t, theirs, theirsV)
	return anc, ours, theirs
}

func TestMergeDriver_OursHigher(t *testing.T) {
	anc, ours, theirs := mergeDriverPaths(t, 10, 12, 11)
	if got := mergeDriver(anc, ours, theirs); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := readMergedSurface(t, ours); got != 12 {
		t.Fatalf("merged surface = %d, want 12", got)
	}
}

func TestMergeDriver_TheirsHigher(t *testing.T) {
	anc, ours, theirs := mergeDriverPaths(t, 10, 11, 13)
	if got := mergeDriver(anc, ours, theirs); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := readMergedSurface(t, ours); got != 13 {
		t.Fatalf("merged surface = %d, want 13", got)
	}
}

func TestMergeDriver_Equal(t *testing.T) {
	anc, ours, theirs := mergeDriverPaths(t, 10, 11, 11)
	if got := mergeDriver(anc, ours, theirs); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := readMergedSurface(t, ours); got != 11 {
		t.Fatalf("merged surface = %d, want 11", got)
	}
}

func TestMergeDriver_MissingAncestor(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface") // intentionally not created
	ours := filepath.Join(dir, "ours.surface")
	theirs := filepath.Join(dir, "theirs.surface")
	writeStampFile(t, ours, 5)
	writeStampFile(t, theirs, 7)

	if got := mergeDriver(anc, ours, theirs); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := readMergedSurface(t, ours); got != 7 {
		t.Fatalf("merged surface = %d, want 7", got)
	}
}

func TestMergeDriver_MalformedOurs(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface")
	ours := filepath.Join(dir, "ours.surface")
	theirs := filepath.Join(dir, "theirs.surface")
	writeStampFile(t, anc, 10)
	if err := os.WriteFile(ours, []byte("this is not toml = ::: garbage"), 0o644); err != nil {
		t.Fatalf("write ours: %v", err)
	}
	writeStampFile(t, theirs, 11)

	if got := mergeDriver(anc, ours, theirs); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}

	out, err := os.ReadFile(ours)
	if err != nil {
		t.Fatalf("read ours after merge: %v", err)
	}
	s := string(out)
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
		if !strings.Contains(s, marker) {
			t.Fatalf("missing conflict marker %q in ours:\n%s", marker, s)
		}
	}
}

func TestMergeDriver_AllZero(t *testing.T) {
	anc, ours, theirs := mergeDriverPaths(t, 0, 0, 0)
	if got := mergeDriver(anc, ours, theirs); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := readMergedSurface(t, ours); got != 0 {
		t.Fatalf("merged surface = %d, want 0", got)
	}
}

func TestMergeDriver_MalformedTheirs(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface")
	ours := filepath.Join(dir, "ours.surface")
	theirs := filepath.Join(dir, "theirs.surface")
	writeStampFile(t, anc, 10)
	writeStampFile(t, ours, 11)
	if err := os.WriteFile(theirs, []byte("\x00\x01 not toml at all"), 0o644); err != nil {
		t.Fatalf("write theirs: %v", err)
	}

	if got := mergeDriver(anc, ours, theirs); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
}

func TestMergeDriver_MalformedAncestor(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface")
	ours := filepath.Join(dir, "ours.surface")
	theirs := filepath.Join(dir, "theirs.surface")
	if err := os.WriteFile(anc, []byte(":::not-toml:::"), 0o644); err != nil {
		t.Fatalf("write ancestor: %v", err)
	}
	writeStampFile(t, ours, 11)
	writeStampFile(t, theirs, 12)

	if got := mergeDriver(anc, ours, theirs); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
}

func TestMergeDriver_MissingOurs(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface")
	ours := filepath.Join(dir, "ours.surface") // not created
	theirs := filepath.Join(dir, "theirs.surface")
	writeStampFile(t, anc, 10)
	writeStampFile(t, theirs, 11)

	if got := mergeDriver(anc, ours, theirs); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
}

func TestRunVaultMergeDriverExit_TooFewArgs(t *testing.T) {
	if got := runVaultMergeDriverExit(nil); got != 1 {
		t.Errorf("runVaultMergeDriverExit(nil) = %d, want 1", got)
	}
	if got := runVaultMergeDriverExit([]string{"a", "b"}); got != 1 {
		t.Errorf("runVaultMergeDriverExit(2 args) = %d, want 1", got)
	}
}

func TestRunVaultMergeDriverExit_HappyPath(t *testing.T) {
	anc, ours, theirs := mergeDriverPaths(t, 5, 7, 8)
	if got := runVaultMergeDriverExit([]string{anc, ours, theirs}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := readMergedSurface(t, ours); got != 8 {
		t.Fatalf("merged surface = %d, want 8", got)
	}
}

func TestMergeDriver_OursPathIsDirectory_WriteFails(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface")
	ours := filepath.Join(dir, "ours.surface")
	theirs := filepath.Join(dir, "theirs.surface")
	writeStampFile(t, anc, 5)
	// Make ours a directory so os.WriteFile fails with EISDIR.
	if err := os.Mkdir(ours, 0o755); err != nil {
		t.Fatalf("mkdir ours: %v", err)
	}
	writeStampFile(t, theirs, 6)

	// readMergeInput on a directory will succeed (os.ReadFile on a dir
	// returns an EISDIR-style error on Linux); the merge driver routes
	// that into the read-error branch and writes a text conflict — which
	// itself fails because ours is a dir. Expect exit code 1 either way.
	if got := mergeDriver(anc, ours, theirs); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestReadMergeInput_PermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "no-read.surface")
	if err := os.WriteFile(path, []byte("surface = 1\n"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readMergeInput(path); err == nil {
		t.Errorf("readMergeInput on 0000-mode file: err = nil, want non-nil")
	}
}

func TestMergeDriver_ResolutionClearsAuxiliaryFields(t *testing.T) {
	dir := t.TempDir()
	anc := filepath.Join(dir, "ancestor.surface")
	ours := filepath.Join(dir, "ours.surface")
	theirs := filepath.Join(dir, "theirs.surface")

	writeStampFile(t, anc, 10)
	// Manually craft an "ours" with populated auxiliary fields so we can
	// assert the resolution clears them per the spec.
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(surface.Stamp{
		Surface:     12,
		LastWriter:  "abcd1234",
		LastWriteAt: "2026-04-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(ours, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write ours: %v", err)
	}
	writeStampFile(t, theirs, 11)

	if got := mergeDriver(anc, ours, theirs); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}

	data, err := os.ReadFile(ours)
	if err != nil {
		t.Fatalf("read ours: %v", err)
	}
	var got surface.Stamp
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse ours: %v", err)
	}
	if got.Surface != 12 {
		t.Errorf("Surface = %d, want 12", got.Surface)
	}
	if got.LastWriter != "" {
		t.Errorf("LastWriter = %q, want empty (re-stamped on next write)", got.LastWriter)
	}
	if got.LastWriteAt != "" {
		t.Errorf("LastWriteAt = %q, want empty (re-stamped on next write)", got.LastWriteAt)
	}
}
