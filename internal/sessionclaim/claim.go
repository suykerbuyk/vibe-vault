// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package sessionclaim implements Mechanism 2 of the
// session-slot-multihost-disambiguation plan: a host-local cache file
// that mints and persists a stable derived_session_id keyed off
// (project_root, ppid, ppid_starttime).
//
// Phase 0a (this file) ships only the read path: schema, token
// derivation, cache-dir resolution, and Read. Phase 3 wires
// AcquireOrRefresh, UpdateHarnessSessionID, ReleaseSession, the
// per-token lockfile (H4), gopsutil-backed starttime, harness
// detection, and the stale-claim sweep.
package sessionclaim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ErrNoClaim is returned by Read when no claim file exists for the
// current (projectRoot, ppid, ppid_starttime) triple. Callers treat
// this as "first call this session — needs a fresh claim minted by
// AcquireOrRefresh" (Phase 3).
var ErrNoClaim = errors.New("sessionclaim: no claim file")

// Claim is the on-disk schema for a session claim file. Persisted at
// <UserCacheDir>/vibe-vault/session-claims/<token>.json where
// <token> = hex.EncodeToString(sha256(project_root || \x00 || ppid ||
// \x00 || starttime)[:16]) — 32 hex chars.
//
// Phase 0a ships only the read path; Phase 3 wires AcquireOrRefresh /
// UpdateHarnessSessionID / ReleaseSession with per-token locking and
// the full lifecycle described in Mechanism 2 of the
// session-slot-multihost-disambiguation plan.
type Claim struct {
	DerivedSessionID string    `json:"derived_session_id"`
	HarnessSessionID string    `json:"harness_session_id"`
	PPID             int       `json:"ppid"`
	PPIDStarttime    int64     `json:"ppid_starttime"`
	ProjectRoot      string    `json:"project_root"`
	CWD              string    `json:"cwd"`
	Harness          string    `json:"harness"`
	Host             string    `json:"host"`
	ClaimedAt        time.Time `json:"claimed_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	Status           string    `json:"status"`
}

// Read loads the current claim for projectRoot using the current
// process's parent PID (os.Getppid) to derive the token. Returns
// (nil, ErrNoClaim) when no claim file exists for the derived token.
// Returns (nil, err) on any other I/O or JSON parse error.
//
// Phase-0a-only simplification: starttime is hardcoded to 0. Phase 3
// replaces this with a gopsutil-backed probe (Linux: /proc/<ppid>/stat
// field 22; macOS: gopsutil.Process.CreateTime()). The token is still
// stable per-process within a session because the (ppid, 0) pair is
// stable for the duration of any one process's lifetime; correctness
// for multi-instance disambiguation arrives with Phase 3's real
// starttime.
//
// TODO(phase-3): replace hardcoded starttime=0 with a real probe.
func Read(projectRoot string) (*Claim, error) {
	ppid := os.Getppid()
	const starttime int64 = 0 // TODO(phase-3): real starttime probe
	token := tokenFor(projectRoot, ppid, starttime)

	dir, err := cacheDir()
	if err != nil {
		return nil, fmt.Errorf("sessionclaim: cache dir: %w", err)
	}
	path := filepath.Join(dir, token+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoClaim
		}
		return nil, fmt.Errorf("sessionclaim: read %s: %w", path, err)
	}
	var c Claim
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("sessionclaim: parse %s: %w", path, err)
	}
	return &c, nil
}

// tokenFor computes the 32-hex-char token from
// (projectRoot, ppid, starttime). Format:
//
//	hex.EncodeToString(
//	    sha256(projectRoot || "\x00" ||
//	           strconv.Itoa(ppid) || "\x00" ||
//	           strconv.FormatInt(starttime, 10))[:16])
//
// The truncation to 16 bytes (32 hex chars) is intentional and
// load-bearing: the spec in Mechanism 2 calls out the prefix length.
// Tests assert exact length.
func tokenFor(projectRoot string, ppid int, starttime int64) string {
	h := sha256.New()
	h.Write([]byte(projectRoot))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(ppid)))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(starttime, 10)))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// cacheDir returns <UserCacheDir>/vibe-vault/session-claims via
// os.UserCacheDir + filepath.Join. Does NOT MkdirAll — directory
// creation is Phase 3's responsibility (it owns the write path).
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "vibe-vault", "session-claims"), nil
}
