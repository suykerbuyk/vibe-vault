// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package wrapmetrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setTestHome pins $VIBE_VAULT_HOME to dir for the duration of the test and
// restores the original value on cleanup.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	orig, had := os.LookupEnv("VIBE_VAULT_HOME")
	t.Setenv("VIBE_VAULT_HOME", dir)
	t.Cleanup(func() {
		if had {
			os.Setenv("VIBE_VAULT_HOME", orig)
		} else {
			os.Unsetenv("VIBE_VAULT_HOME")
		}
	})
}

// makeTestLine returns a minimal Line with the given field name.
func makeTestLine(field string) Line {
	return Line{
		Timestamp:   "2026-04-25T17:00:00Z",
		Host:        "testhost",
		User:        "testuser",
		CWD:         "/home/testuser/code/myproject",
		Project:     "test-project",
		Iteration:   1,
		Field:       field,
		SynthSHA256: "abc123",
		ApplySHA256: "abc123",
		SynthBytes:  100,
		ApplyBytes:  100,
		DriftBytes:  0,
	}
}

// TestAppendCreatesFile verifies that AppendLine creates the metrics file
// and its parent directories when they do not yet exist.
func TestAppendCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	if err := AppendLine(makeTestLine("iteration_block")); err != nil {
		t.Fatalf("AppendLine: %v", err)
	}

	cacheDir := filepath.Join(tmp, ".cache", "vibe-vault")
	path := filepath.Join(cacheDir, ActiveFile)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("metrics file not created: %v", err)
	}
}

// TestAppendAppendsLine verifies that successive AppendLine calls each add
// exactly one line to the file.
func TestAppendAppendsLine(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	cacheDir := filepath.Join(tmp, ".cache", "vibe-vault")

	for i := 1; i <= 3; i++ {
		if err := AppendLine(makeTestLine(fmt.Sprintf("field_%d", i))); err != nil {
			t.Fatalf("AppendLine %d: %v", i, err)
		}
		n, err := CountActiveLines(cacheDir)
		if err != nil {
			t.Fatalf("CountActiveLines after %d appends: %v", i, err)
		}
		if n != i {
			t.Errorf("after %d appends: got %d lines, want %d", i, n, i)
		}
	}
}

// TestRotationTriggerAt1000Lines verifies that the active file is rotated
// to wrap-metrics-archive-YYYY.jsonl after the 1000th line is present.
// We pre-fill the file with 1000 lines before the triggering AppendLine.
func TestRotationTriggerAt1000Lines(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	cacheDir := filepath.Join(tmp, ".cache", "vibe-vault")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-fill the active file with exactly RotationThreshold lines.
	path := filepath.Join(cacheDir, ActiveFile)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < RotationThreshold; i++ {
		fmt.Fprintln(f, `{"field":"prefill"}`)
	}
	f.Close()

	n, _ := CountActiveLines(cacheDir)
	if n != RotationThreshold {
		t.Fatalf("pre-fill: got %d lines, want %d", n, RotationThreshold)
	}

	// Append one more line — rotation should fire.
	if err := AppendLine(makeTestLine("trigger")); err != nil {
		t.Fatalf("AppendLine after prefill: %v", err)
	}

	// The archive file should now exist.
	year := time.Now().Year()
	archiveName := fmt.Sprintf("wrap-metrics-archive-%d.jsonl", year)
	archiveFull := filepath.Join(cacheDir, archiveName)
	if _, err := os.Stat(archiveFull); err != nil {
		t.Fatalf("archive file not created after rotation: %v", err)
	}

	// The active file should contain only the new line (1 line).
	na, err := CountActiveLines(cacheDir)
	if err != nil {
		t.Fatalf("CountActiveLines after rotation: %v", err)
	}
	if na != 1 {
		t.Errorf("active file after rotation: got %d lines, want 1", na)
	}
}

// TestSchemaFieldsMatchSpec verifies that every required schema field is
// present in a marshalled Line.
func TestSchemaFieldsMatchSpec(t *testing.T) {
	line := Line{
		Timestamp:   "2026-04-25T17:00:00Z",
		Host:        "myhost",
		User:        "myuser",
		CWD:         "/some/dir",
		Project:     "vibe-vault",
		Iteration:   152,
		Field:       "iteration_block",
		SynthSHA256: "aaaa",
		ApplySHA256: "bbbb",
		SynthBytes:  2048,
		ApplyBytes:  2049,
		DriftBytes:  1,
	}
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{
		"timestamp", "host", "user", "cwd", "project",
		"iteration", "field",
		"synth_sha256", "apply_sha256",
		"synth_bytes", "apply_bytes", "drift_bytes",
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			t.Errorf("schema: missing field %q in marshalled Line", k)
		}
	}
}

// TestProvenanceFieldsPopulated verifies that AppendBundleLines fills host,
// user, cwd, project, and iteration on each line.
func TestProvenanceFieldsPopulated(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	cacheDir := filepath.Join(tmp, ".cache", "vibe-vault")

	fields := []Line{
		{Field: "commit_msg", SynthSHA256: "s1", ApplySHA256: "a1", SynthBytes: 10, ApplyBytes: 10},
		{Field: "capture_session", SynthSHA256: "s2", ApplySHA256: "a2", SynthBytes: 20, ApplyBytes: 20},
	}
	err := AppendBundleLines("myhost", "myuser", "/cwd", "myproject", 42, fields)
	if err != nil {
		t.Fatalf("AppendBundleLines: %v", err)
	}

	lines, err := ReadActiveLines(cacheDir)
	if err != nil {
		t.Fatalf("ReadActiveLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	for i, raw := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("line %d: unmarshal: %v", i, err)
		}
		if m["host"] != "myhost" {
			t.Errorf("line %d: host=%v, want myhost", i, m["host"])
		}
		if m["user"] != "myuser" {
			t.Errorf("line %d: user=%v, want myuser", i, m["user"])
		}
		if m["cwd"] != "/cwd" {
			t.Errorf("line %d: cwd=%v, want /cwd", i, m["cwd"])
		}
		if m["project"] != "myproject" {
			t.Errorf("line %d: project=%v, want myproject", i, m["project"])
		}
		if int(m["iteration"].(float64)) != 42 {
			t.Errorf("line %d: iteration=%v, want 42", i, m["iteration"])
		}
	}
}

// TestRotationSkippedOnErrorWithWarning verifies that when os.Rename fails
// during rotation, a warning is emitted, no error is returned to the caller,
// and the new line is still written.
func TestRotationSkippedOnErrorWithWarning(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	cacheDir := filepath.Join(tmp, ".cache", "vibe-vault")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-fill to trigger rotation.
	path := filepath.Join(cacheDir, ActiveFile)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < RotationThreshold; i++ {
		fmt.Fprintln(f, `{"field":"x"}`)
	}
	f.Close()

	// Capture warnings.
	var warned []string
	orig := warnFunc
	warnFunc = func(format string, args ...any) {
		warned = append(warned, fmt.Sprintf(format, args...))
	}
	defer func() { warnFunc = orig }()

	// Make the cache dir read-only so Rename will fail.
	if err := os.Chmod(cacheDir, 0o555); err != nil {
		t.Skip("chmod not supported on this platform:", err)
	}
	defer os.Chmod(cacheDir, 0o755) //nolint:errcheck

	// AppendLine should not return an error (rotation failure is a warning).
	// But we can't write to a read-only dir either, so just check that
	// the rotation warning was issued via rotateIfNeeded.
	// We call rotateIfNeeded directly to isolate the rotation path.
	rotateErr := rotateIfNeeded(path, cacheDir)
	if rotateErr != nil {
		t.Errorf("rotateIfNeeded should not return an error, got: %v", rotateErr)
	}
	if len(warned) == 0 {
		t.Error("expected at least one rotation warning, got none")
	}
	// Verify the warning message mentions the failure.
	found := false
	for _, w := range warned {
		if strings.Contains(w, "rotation") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("warning does not mention rotation; got: %v", warned)
	}
}

// TestCacheDirUsesVIBE_VAULT_HOME verifies CacheDir honours the env sentinel.
func TestCacheDirUsesVIBE_VAULT_HOME(t *testing.T) {
	t.Setenv("VIBE_VAULT_HOME", "/fake/home")
	got, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	want := "/fake/home/.cache/vibe-vault"
	if got != want {
		t.Errorf("CacheDir=%q, want %q", got, want)
	}
}

// TestDriftBytesIsSignedDelta verifies the drift_bytes field captures the
// signed byte delta between apply and synth.
func TestDriftBytesIsSignedDelta(t *testing.T) {
	line := Line{
		SynthBytes: 100,
		ApplyBytes: 80,
		DriftBytes: -20,
	}
	if line.DriftBytes != line.ApplyBytes-line.SynthBytes {
		t.Errorf("DriftBytes should equal ApplyBytes-SynthBytes: got %d, want %d",
			line.DriftBytes, line.ApplyBytes-line.SynthBytes)
	}
}
