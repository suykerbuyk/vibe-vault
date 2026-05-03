// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package surface tracks the MCP tool-surface version stamped into a vault on
// every successful vault write. The stamp ("<dir>/.surface") is a TOML record
// that lets a future verifier detect a vault written by a newer client.
//
// Phase 1a: introduce the package and wire write-primitive stamping. CLI
// gates land in Phase 1b — CheckCompatible is a stub here.
package surface

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// MCPSurfaceVersion is the current MCP tool-surface schema version. It bumps
// when the verifier ships in Phase 3.
const MCPSurfaceVersion int = 13

// Stamp models the on-disk .surface TOML file recording the latest writer.
type Stamp struct {
	Surface     int    `toml:"surface"`
	LastWriter  string `toml:"last_writer"`
	LastWriteAt string `toml:"last_write_at"`
}

// stampFilename is the per-directory record relative to a stamp directory.
const stampFilename = ".surface"

// ReadStamp reads the .surface file from stampDir.
//
// A missing file returns Stamp{Surface: 0}, nil — mirroring
// internal/context.ReadVersion's zero-value-on-missing pattern. Malformed TOML
// returns a wrapped error.
func ReadStamp(stampDir string) (Stamp, error) {
	path := filepath.Join(stampDir, stampFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Stamp{Surface: 0}, nil
		}
		return Stamp{}, fmt.Errorf("read .surface: %w", err)
	}
	var s Stamp
	if err := toml.Unmarshal(data, &s); err != nil {
		return Stamp{}, fmt.Errorf("parse .surface: %w", err)
	}
	return s, nil
}

// WriteStamp atomically writes <stampDir>/.surface with the supplied version
// and writerFingerprint, setting last_write_at to time.Now().UTC() in RFC3339.
//
// Monotonic: if an existing stamp's surface is strictly greater than version,
// WriteStamp returns nil without writing. Equal versions refresh the
// timestamp; lower or missing versions get overwritten.
//
// Implementation note: the .surface file is written via a direct
// temp-file + rename rather than atomicfile.Write to avoid an import cycle
// (atomicfile imports surface for the stamp side-effect on vault writes).
func WriteStamp(stampDir string, version int, writerFingerprint string) error {
	existing, err := ReadStamp(stampDir)
	if err != nil {
		return err
	}
	if existing.Surface > version {
		return nil
	}

	if err := os.MkdirAll(stampDir, 0o755); err != nil {
		return fmt.Errorf("create stamp dir: %w", err)
	}

	s := Stamp{
		Surface:     version,
		LastWriter:  writerFingerprint,
		LastWriteAt: time.Now().UTC().Format(time.RFC3339),
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		return fmt.Errorf("encode .surface: %w", err)
	}

	path := filepath.Join(stampDir, stampFilename)
	if err := writeStampFileAtomic(path, buf.Bytes()); err != nil {
		return fmt.Errorf("write .surface: %w", err)
	}
	return nil
}

// writeStampFileAtomic mirrors atomicfile.Write's temp-file + rename dance
// for the .surface record. Kept private and minimal so surface does not need
// to import atomicfile (which would create an import cycle: atomicfile must
// import surface to stamp on vault writes).
func writeStampFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".vv-surface-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	removeTemp = false
	return nil
}

// unrecognizedTopWarn keys a sync.Once per unrecognized top-level directory
// so the stderr warning fires at most once per process per name.
var (
	unrecognizedTopWarnMu sync.Mutex
	unrecognizedTopWarn   = map[string]*sync.Once{}
)

// ResolveStampDir maps a vault write target (writePath) under vaultPath to
// the directory whose .surface file should be touched.
//
// Returns ("", nil) when:
//   - writePath is outside vaultPath (host-local write)
//   - writePath is under vaultPath but the top-level dir is not Projects /
//     Knowledge / Templates (in which case a stderr warning fires once per
//     process per top-level name)
//
// Recognized layouts:
//   - <vault>/Projects/<p>/...   → <vault>/Projects/<p>/agentctx
//   - <vault>/Knowledge/...      → <vault>/Knowledge
//   - <vault>/Templates/...      → <vault>/Templates
func ResolveStampDir(vaultPath, writePath string) (string, error) {
	if vaultPath == "" {
		return "", nil
	}
	absVault, err := filepath.Abs(vaultPath)
	if err != nil {
		return "", fmt.Errorf("abs vault path: %w", err)
	}
	absWrite, err := filepath.Abs(writePath)
	if err != nil {
		return "", fmt.Errorf("abs write path: %w", err)
	}
	rel, err := filepath.Rel(absVault, absWrite)
	if err != nil {
		// Different volumes / unrelated paths — host-local write.
		return "", nil
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		// Outside the vault — host-local write, no stamp, no warning.
		return "", nil
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", nil
	}
	top := parts[0]
	switch top {
	case "Projects":
		if len(parts) < 2 || parts[1] == "" {
			return "", nil
		}
		return filepath.Join(absVault, "Projects", parts[1], "agentctx"), nil
	case "Knowledge":
		return filepath.Join(absVault, "Knowledge"), nil
	case "Templates":
		return filepath.Join(absVault, "Templates"), nil
	default:
		// Vault-relative but unrecognized — warn once per top-level name.
		warnUnrecognizedTopOnce(top, writePath)
		return "", nil
	}
}

// warnUnrecognizedTopOnce emits the unrecognized-vault-path warning for the
// given top-level directory at most once per process.
func warnUnrecognizedTopOnce(top, writePath string) {
	unrecognizedTopWarnMu.Lock()
	once, ok := unrecognizedTopWarn[top]
	if !ok {
		once = &sync.Once{}
		unrecognizedTopWarn[top] = once
	}
	unrecognizedTopWarnMu.Unlock()
	once.Do(func() {
		fmt.Fprintf(os.Stderr, "vv: warning — vault write at unrecognized path %q (no .surface stamp)\n", writePath)
	})
}

// resetUnrecognizedTopWarnForTest clears the once-cache. Test-only.
func resetUnrecognizedTopWarnForTest() {
	unrecognizedTopWarnMu.Lock()
	defer unrecognizedTopWarnMu.Unlock()
	unrecognizedTopWarn = map[string]*sync.Once{}
}

// WriterFingerprint returns an 8-hex-char prefix of sha256(hostname+vaultPath).
//
// Privacy: the raw hostname is never written to the vault. A future
// configuration knob ([stamp].verbose_writer = true) may opt-in to the raw
// hostname; that requires config plumbing and lands in a separate change.
// For Phase 1a we always return the hash prefix.
func WriterFingerprint(vaultPath string) string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	sum := sha256.Sum256([]byte(host + vaultPath))
	return hex.EncodeToString(sum[:])[:8]
}

// IncompatibleError is returned by CheckCompatible when a vault stamp's
// surface version exceeds MCPSurfaceVersion. The fields support both
// callers that want to format their own messages and the standard
// Error() text used by the entry-point gates.
type IncompatibleError struct {
	BinarySurface int
	VaultSurface  int
	StampDir      string // worst (highest) stamp's directory
	LastWriter    string // optional, may be empty
}

// Error renders the spec's standard message. The "last writer" field falls
// back to "unknown" when the worst stamp does not record one.
func (e *IncompatibleError) Error() string {
	writer := e.LastWriter
	if writer == "" {
		writer = "unknown"
	}
	return fmt.Sprintf(
		"vv: this binary supports MCP surface v%d; vault target '%s' is at v%d\n"+
			"    last writer: %s (best-effort, not enforced)\n"+
			"    action:    cd ~/code/vibe-vault && git pull && make install\n"+
			"    if you cannot upgrade right now (deploy host, network outage):\n"+
			"       VV_SURFACE_GATE=warn <original-command>   (proceed at risk)",
		e.BinarySurface, e.StampDir, e.VaultSurface, writer,
	)
}

// CheckCompatible scans known stamp targets under vaultPath and returns a
// non-nil error if any stamp's surface > MCPSurfaceVersion (i.e., the vault
// was written by a newer binary than this one).
//
// Vault-unreachable degradation: if vaultPath is empty or os.Stat fails,
// returns nil (best-effort; gates proceed). Callers may log the unreachable
// case to stderr but should still proceed.
//
// On mismatch, returns *IncompatibleError populated with the worst (highest)
// stamp's directory, surface version, and last_writer (when available).
func CheckCompatible(vaultPath string) error {
	if vaultPath == "" {
		return nil
	}
	if _, err := os.Stat(vaultPath); err != nil {
		return nil
	}

	patterns := []string{
		filepath.Join(vaultPath, "Projects", "*", "agentctx", stampFilename),
		filepath.Join(vaultPath, "Knowledge", stampFilename),
		filepath.Join(vaultPath, "Templates", stampFilename),
	}

	maxSurface := 0
	worstDir := ""
	worstWriter := ""

	for _, pat := range patterns {
		matches, err := filepath.Glob(pat)
		if err != nil {
			continue
		}
		for _, m := range matches {
			stampDir := filepath.Dir(m)
			s, err := ReadStamp(stampDir)
			if err != nil {
				// Malformed stamps are not gating events; skip silently.
				continue
			}
			if s.Surface > maxSurface {
				maxSurface = s.Surface
				worstDir = stampDir
				worstWriter = s.LastWriter
			}
		}
	}

	if maxSurface > MCPSurfaceVersion {
		return &IncompatibleError{
			BinarySurface: MCPSurfaceVersion,
			VaultSurface:  maxSurface,
			StampDir:      worstDir,
			LastWriter:    worstWriter,
		}
	}
	return nil
}
