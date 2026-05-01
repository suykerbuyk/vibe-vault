// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package surface

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadStamp(t *testing.T) {
	t.Run("missing file returns zero stamp", func(t *testing.T) {
		dir := t.TempDir()
		got, err := ReadStamp(dir)
		if err != nil {
			t.Fatalf("ReadStamp: %v", err)
		}
		if got.Surface != 0 || got.LastWriter != "" || got.LastWriteAt != "" {
			t.Fatalf("expected zero stamp, got %+v", got)
		}
	})

	t.Run("happy path reads correct values", func(t *testing.T) {
		dir := t.TempDir()
		body := "" +
			"surface = 11\n" +
			"last_writer = \"a3c1d8f9\"\n" +
			"last_write_at = \"2026-05-01T12:14:00Z\"\n"
		if err := os.WriteFile(filepath.Join(dir, ".surface"), []byte(body), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		got, err := ReadStamp(dir)
		if err != nil {
			t.Fatalf("ReadStamp: %v", err)
		}
		if got.Surface != 11 {
			t.Errorf("Surface = %d, want 11", got.Surface)
		}
		if got.LastWriter != "a3c1d8f9" {
			t.Errorf("LastWriter = %q, want a3c1d8f9", got.LastWriter)
		}
		if got.LastWriteAt != "2026-05-01T12:14:00Z" {
			t.Errorf("LastWriteAt = %q, want 2026-05-01T12:14:00Z", got.LastWriteAt)
		}
	})

	t.Run("malformed TOML returns error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".surface"), []byte("not = = valid toml ["), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		_, err := ReadStamp(dir)
		if err == nil {
			t.Fatalf("expected parse error, got nil")
		}
	})
}

func TestWriteStamp(t *testing.T) {
	t.Run("first write creates file", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteStamp(dir, 11, "a3c1d8f9"); err != nil {
			t.Fatalf("WriteStamp: %v", err)
		}
		got, err := ReadStamp(dir)
		if err != nil {
			t.Fatalf("ReadStamp: %v", err)
		}
		if got.Surface != 11 || got.LastWriter != "a3c1d8f9" {
			t.Fatalf("unexpected stamp: %+v", got)
		}
		if got.LastWriteAt == "" {
			t.Fatalf("LastWriteAt empty")
		}
		if _, err := time.Parse(time.RFC3339, got.LastWriteAt); err != nil {
			t.Fatalf("LastWriteAt not RFC3339 (%q): %v", got.LastWriteAt, err)
		}
	})

	t.Run("higher version overwrites", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteStamp(dir, 11, "writer1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := WriteStamp(dir, 12, "writer2"); err != nil {
			t.Fatalf("WriteStamp: %v", err)
		}
		got, _ := ReadStamp(dir)
		if got.Surface != 12 || got.LastWriter != "writer2" {
			t.Fatalf("expected v12/writer2, got %+v", got)
		}
	})

	t.Run("lower version is no-op", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteStamp(dir, 12, "writer-high"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := WriteStamp(dir, 11, "writer-low"); err != nil {
			t.Fatalf("WriteStamp: %v", err)
		}
		got, _ := ReadStamp(dir)
		if got.Surface != 12 || got.LastWriter != "writer-high" {
			t.Fatalf("lower-version write mutated stamp: %+v", got)
		}
	})

	t.Run("equal version refreshes timestamp", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteStamp(dir, 11, "writer1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		first, _ := ReadStamp(dir)
		// Sleep slightly so RFC3339 (second precision) ticks.
		time.Sleep(1100 * time.Millisecond)
		if err := WriteStamp(dir, 11, "writer2"); err != nil {
			t.Fatalf("WriteStamp: %v", err)
		}
		second, _ := ReadStamp(dir)
		if second.Surface != 11 {
			t.Errorf("Surface = %d, want 11", second.Surface)
		}
		if second.LastWriter != "writer2" {
			t.Errorf("LastWriter = %q, want writer2", second.LastWriter)
		}
		if first.LastWriteAt == second.LastWriteAt {
			t.Errorf("LastWriteAt did not refresh: %q == %q", first.LastWriteAt, second.LastWriteAt)
		}
	})
}

func TestWriteStamp_CreatesMissingStampDir(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "deep", "stamp", "dir")
	if err := WriteStamp(missing, 11, "fp"); err != nil {
		t.Fatalf("WriteStamp: %v", err)
	}
	got, err := ReadStamp(missing)
	if err != nil {
		t.Fatalf("ReadStamp: %v", err)
	}
	if got.Surface != 11 {
		t.Fatalf("Surface = %d, want 11", got.Surface)
	}
}

func TestWriteStamp_FailsWhenStampDirIsAFile(t *testing.T) {
	parent := t.TempDir()
	// Make 'stamp' a regular file so MkdirAll(stamp) fails.
	blocker := filepath.Join(parent, "stamp")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteStamp(blocker, 11, "fp"); err == nil {
		t.Fatalf("expected error when stamp dir is occupied by a regular file")
	}
}

func TestWriteStamp_RenameFailsBubblesError(t *testing.T) {
	dir := t.TempDir()
	// Pre-create .surface as a non-empty directory; rename will fail.
	stampPath := filepath.Join(dir, ".surface")
	if err := os.MkdirAll(stampPath, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stampPath, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if err := WriteStamp(dir, 11, "fp"); err == nil {
		t.Fatalf("expected error when rename target is a non-empty directory")
	}
}

func TestResolveStampDir(t *testing.T) {
	vault := t.TempDir()

	cases := []struct {
		name      string
		writePath string
		want      string
	}{
		{
			name:      "Projects agentctx file",
			writePath: filepath.Join(vault, "Projects", "myproj", "agentctx", "resume.md"),
			want:      filepath.Join(vault, "Projects", "myproj", "agentctx"),
		},
		{
			name:      "Projects sessions file",
			writePath: filepath.Join(vault, "Projects", "myproj", "sessions", "2026-05-01-foo.md"),
			want:      filepath.Join(vault, "Projects", "myproj", "agentctx"),
		},
		{
			name:      "Knowledge file",
			writePath: filepath.Join(vault, "Knowledge", "go-tips.md"),
			want:      filepath.Join(vault, "Knowledge"),
		},
		{
			name:      "Templates file",
			writePath: filepath.Join(vault, "Templates", "agentctx", "resume.md"),
			want:      filepath.Join(vault, "Templates"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveStampDir(vault, c.writePath)
			if err != nil {
				t.Fatalf("ResolveStampDir: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}

	t.Run("path outside vault returns empty no warning", func(t *testing.T) {
		other := t.TempDir()
		writePath := filepath.Join(other, "foo.md")
		got, err := ResolveStampDir(vault, writePath)
		if err != nil {
			t.Fatalf("ResolveStampDir: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("vault-relative unknown top warns once", func(t *testing.T) {
		resetUnrecognizedTopWarnForTest()
		// Capture stderr.
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		defer func() { os.Stderr = oldStderr }()

		writePath1 := filepath.Join(vault, "Junk", "a.md")
		writePath2 := filepath.Join(vault, "Junk", "b.md")
		got1, _ := ResolveStampDir(vault, writePath1)
		got2, _ := ResolveStampDir(vault, writePath2)

		_ = w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		os.Stderr = oldStderr

		if got1 != "" || got2 != "" {
			t.Fatalf("expected empty stamp dir, got %q / %q", got1, got2)
		}
		out := buf.String()
		count := strings.Count(out, "unrecognized path")
		if count != 1 {
			t.Fatalf("expected exactly 1 warning, got %d in: %q", count, out)
		}
	})

	t.Run("warn keyed by top-level dir name", func(t *testing.T) {
		resetUnrecognizedTopWarnForTest()
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		defer func() { os.Stderr = oldStderr }()

		_, _ = ResolveStampDir(vault, filepath.Join(vault, "Junk1", "a.md"))
		_, _ = ResolveStampDir(vault, filepath.Join(vault, "Junk2", "b.md"))
		_, _ = ResolveStampDir(vault, filepath.Join(vault, "Junk1", "c.md"))

		_ = w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		os.Stderr = oldStderr

		out := buf.String()
		// Junk1 + Junk2 → two warnings; the second Junk1 hit is suppressed.
		if got := strings.Count(out, "unrecognized path"); got != 2 {
			t.Fatalf("expected 2 warnings (one per distinct top), got %d in: %q", got, out)
		}
	})

	t.Run("empty vault path returns empty", func(t *testing.T) {
		got, err := ResolveStampDir("", "/some/path")
		if err != nil {
			t.Fatalf("ResolveStampDir: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	// Concurrency check on the once-warning machinery.
	t.Run("once warning is concurrency safe", func(t *testing.T) {
		resetUnrecognizedTopWarnForTest()
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		defer func() { os.Stderr = oldStderr }()

		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = ResolveStampDir(vault, filepath.Join(vault, "Concurrent", "x.md"))
			}()
		}
		wg.Wait()

		_ = w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		os.Stderr = oldStderr

		if got := strings.Count(buf.String(), "unrecognized path"); got != 1 {
			t.Fatalf("expected 1 warning under concurrency, got %d", got)
		}
	})
}

func TestWriterFingerprint(t *testing.T) {
	t.Run("returns 8 hex chars", func(t *testing.T) {
		got := WriterFingerprint("/some/vault")
		if len(got) != 8 {
			t.Fatalf("len = %d, want 8 (got %q)", len(got), got)
		}
		for _, r := range got {
			isDigit := '0' <= r && r <= '9'
			isHexLower := 'a' <= r && r <= 'f'
			if !isDigit && !isHexLower {
				t.Fatalf("non-hex char %q in %q", r, got)
			}
		}
	})

	t.Run("deterministic for same input", func(t *testing.T) {
		a := WriterFingerprint("/vault/a")
		b := WriterFingerprint("/vault/a")
		if a != b {
			t.Fatalf("non-deterministic: %q != %q", a, b)
		}
	})

	t.Run("different vault paths differ", func(t *testing.T) {
		a := WriterFingerprint("/vault/a")
		b := WriterFingerprint("/vault/b")
		if a == b {
			t.Fatalf("expected distinct fingerprints, both %q", a)
		}
	})
}

func TestCheckCompatible_Stub(t *testing.T) {
	if err := CheckCompatible("/any/vault"); err != nil {
		t.Fatalf("Phase 1a stub should return nil, got %v", err)
	}
	if err := CheckCompatible(""); err != nil {
		t.Fatalf("empty path stub should return nil, got %v", err)
	}
}
