// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package wrapmetrics writes host-local drift metrics for vv_apply_wrap_bundle
// operations.
//
// Each call to AppendLine writes one JSONL record to
// ~/.cache/vibe-vault/wrap-metrics.jsonl. When the active file exceeds 1000
// lines it is rotated to wrap-metrics-archive-YYYY.jsonl (year from the
// current wall clock). Rotation is atomic (rename + create); on rename
// failure a warning is logged and the active file continues to grow.
//
// The file is host-local and intentionally NOT vault-side: it avoids the
// multi-machine append-race that would otherwise require a merge=union
// gitattributes setup. Drift trends are per-machine by design.
package wrapmetrics

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ActiveFile is the filename of the active metrics file within the cache dir.
	ActiveFile = "wrap-metrics.jsonl"
	// RotationThreshold is the line count at which the file is rotated.
	RotationThreshold = 1000
)

// Line is one JSONL record written per bundle field per wrap.
type Line struct {
	Timestamp   string `json:"timestamp"`
	Host        string `json:"host"`
	User        string `json:"user"`
	CWD         string `json:"cwd"`
	Project     string `json:"project"`
	Iteration   int    `json:"iteration"`
	Field       string `json:"field"`
	SynthSHA256 string `json:"synth_sha256"`
	ApplySHA256 string `json:"apply_sha256"`
	SynthBytes  int    `json:"synth_bytes"`
	ApplyBytes  int    `json:"apply_bytes"`
	DriftBytes  int    `json:"drift_bytes"`
}

// homeDirFunc is a test seam; production code uses os.UserHomeDir.
var homeDirFunc = os.UserHomeDir

// CacheDir resolves ~/.cache/vibe-vault.
// It honours $VIBE_VAULT_HOME for deterministic testing (same sentinel as
// meta.HomeDir). Returns an error only when the home directory cannot be
// determined at all.
func CacheDir() (string, error) {
	if v := os.Getenv("VIBE_VAULT_HOME"); v != "" {
		return filepath.Join(v, ".cache", "vibe-vault"), nil
	}
	home, err := homeDirFunc()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "vibe-vault"), nil
}

// activePath returns the absolute path to the active metrics file.
func activePath(cacheDir string) string {
	return filepath.Join(cacheDir, ActiveFile)
}

// archivePath returns the path to the year-tagged archive file.
func archivePath(cacheDir string, t time.Time) string {
	return filepath.Join(cacheDir, fmt.Sprintf("wrap-metrics-archive-%d.jsonl", t.Year()))
}

// warnFunc is a test seam for the rotation warning log output.
var warnFunc = func(format string, args ...any) {
	log.Printf("wrapmetrics: "+format, args...)
}

// AppendLine marshals r to JSON and appends it to the active metrics file,
// rotating if the file exceeds RotationThreshold lines.
//
// Parent directories are created as needed. Errors from the rotation step
// are logged as warnings and do not abort the append.
func AppendLine(r Line) error {
	cacheDir, cacheDirErr := CacheDir()
	if cacheDirErr != nil {
		return cacheDirErr
	}
	if mkdirErr := os.MkdirAll(cacheDir, 0o755); mkdirErr != nil {
		return fmt.Errorf("create cache dir %q: %w", cacheDir, mkdirErr)
	}

	path := activePath(cacheDir)

	// Rotate before writing if the file already has >= RotationThreshold lines.
	if rotErr := rotateIfNeeded(path, cacheDir); rotErr != nil {
		// Non-fatal: warn and continue.
		warnFunc("rotation check failed: %v", rotErr)
	}

	// Marshal the line.
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal metrics line: %w", err)
	}
	data = append(data, '\n')

	// Open for append, creating if necessary.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open metrics file %q: %w", path, err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write metrics line: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close metrics file: %w", closeErr)
	}
	return nil
}

// rotateIfNeeded rotates path → archive-YYYY.jsonl when the line count
// equals or exceeds RotationThreshold.
//
// Rotation is atomic: rename the active file to the archive name, then let
// the next AppendLine call create a fresh active file. On rename failure a
// warning is returned (non-fatal to the caller).
func rotateIfNeeded(path, cacheDir string) error {
	n, err := countLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no file yet — nothing to rotate
		}
		return fmt.Errorf("count lines in %q: %w", path, err)
	}
	if n < RotationThreshold {
		return nil
	}

	dst := archivePath(cacheDir, time.Now())
	if err := os.Rename(path, dst); err != nil {
		warnFunc("rotation rename %q → %q failed: %v (active file will keep growing)", path, dst, err)
		return nil // treat as warning, not hard error
	}
	return nil
}

// countLines returns the number of newline-terminated lines in path.
// Returns 0 and os.ErrNotExist if the file does not exist.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	count := 0
	for {
		n, err := f.Read(buf)
		for _, b := range buf[:n] {
			if b == '\n' {
				count++
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("read metrics file: %w", err)
		}
	}
	return count, nil
}

// AppendBundleLines writes one Line per field in fields. host, user, cwd,
// project, and iteration are shared across all lines; each entry in fields
// provides the field-specific values.
//
// Returns on the first error; fields written before the error are not
// rolled back (the metrics file is append-only).
func AppendBundleLines(host, user, cwd, project string, iteration int, fields []Line) error {
	for i := range fields {
		fields[i].Host = host
		fields[i].User = user
		fields[i].CWD = cwd
		fields[i].Project = project
		fields[i].Iteration = iteration
		if fields[i].Timestamp == "" {
			fields[i].Timestamp = time.Now().UTC().Format(time.RFC3339)
		}
		if err := AppendLine(fields[i]); err != nil {
			return fmt.Errorf("field %q (index %d): %w", fields[i].Field, i, err)
		}
	}
	return nil
}

// CountActiveLines returns the current line count in the active metrics file.
// Returns 0 if the file does not exist.
func CountActiveLines(cacheDir string) (int, error) {
	n, err := countLines(activePath(cacheDir))
	if os.IsNotExist(err) {
		return 0, nil
	}
	return n, err
}

// ReadActiveLines returns all raw JSONL lines from the active metrics file.
// Returns nil and no error when the file does not exist.
func ReadActiveLines(cacheDir string) ([]string, error) {
	data, err := os.ReadFile(activePath(cacheDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read active metrics: %w", err)
	}
	raw := strings.TrimRight(string(data), "\n")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}
