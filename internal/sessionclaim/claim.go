// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package sessionclaim implements Mechanism 2 of the
// session-slot-multihost-disambiguation plan: a host-local cache file
// that mints and persists a stable derived_session_id keyed off
// (project_root, ppid, ppid_starttime).
//
// Phase 0a shipped the read path; Phase 3 (this file in current form)
// adds the full lifecycle: AcquireOrRefresh, UpdateHarnessSessionID,
// ReleaseSession, the per-token lockfile (H4), gopsutil-backed
// starttime via internal/pidlive, harness detection, and the
// stale-claim sweep.
//
// Lock-ordering invariant (M10 in the v8 plan): every write entry
// point acquires <cacheDir>/<token>.lock entirely WITHIN its own
// function body and releases on return. The lock never escapes into a
// caller that already holds the index lock at
// <stateDir>/session-index.json.lock — call sites preserve the order
// claim-then-capture, never capture-then-claim.
package sessionclaim

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
	"github.com/suykerbuyk/vibe-vault/internal/pidlive"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// ErrNoClaim is returned by Read when no claim file exists for the
// current (projectRoot, ppid, ppid_starttime) triple. Callers treat
// this as "first call this session — needs a fresh claim minted by
// AcquireOrRefresh".
var ErrNoClaim = errors.New("sessionclaim: no claim file")

// Claim is the on-disk schema for a session claim file. Persisted at
// <UserCacheDir>/vibe-vault/session-claims/<token>.json where
// <token> = hex.EncodeToString(sha256(project_root || \x00 || ppid ||
// \x00 || starttime)[:16]) — 32 hex chars.
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

// Status values for Claim.Status.
const (
	StatusActive   = "active"
	StatusReleased = "released"
)

// Test seams (L9 testability pattern). Production code calls these
// vars rather than os.Getwd / os.Hostname / time.Now / os.Getppid /
// rand.Read so tests can deterministically inject values.
var (
	getwd     = os.Getwd
	hostname  = os.Hostname
	now       = time.Now
	getppid   = os.Getppid
	randRead  = rand.Read
)

// Read loads the current claim for projectRoot using the current
// process's parent PID and start-time to derive the token. Returns
// (nil, ErrNoClaim) when no claim file exists for the derived token.
// Returns (nil, err) on any other I/O or JSON parse error.
//
// Phase 3 wires the real pidlive.Starttime probe (replacing Phase 0a's
// hardcoded zero). Starttime errors propagate as I/O failures.
func Read(projectRoot string) (*Claim, error) {
	ppid := getppid()
	starttime, err := pidlive.Starttime(ppid)
	if err != nil {
		return nil, fmt.Errorf("sessionclaim: starttime(%d): %w", ppid, err)
	}
	return readByToken(projectRoot, ppid, starttime)
}

// readByToken is the pure read by an explicit triple — used internally
// by AcquireOrRefresh / UpdateHarnessSessionID / ReleaseSession after
// they have computed the triple under the per-token lock.
func readByToken(projectRoot string, ppid int, starttime int64) (*Claim, error) {
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

// AcquireOrRefresh reads the existing claim (or mints a fresh one) for
// projectRoot, gated by a per-token lockfile. See plan Mechanism 2 for
// the full lifecycle.
//
// Returns (nil, err) on any I/O failure (cacheDir unavailable,
// read-only filesystem, lock acquisition timeout, JSON parse error).
// Callers treat (nil, err) as "fall back to legacy behavior" — DO NOT
// PANIC, this is the H6 contract documented in the v8 plan.
//
// Lock invariant (M10): the per-token lock is acquired and released
// entirely within this function, BEFORE the caller proceeds to
// session.Capture. No nested holding of both locks.
func AcquireOrRefresh(projectRoot string) (*Claim, error) {
	ppid := getppid()
	starttime, err := pidlive.Starttime(ppid)
	if err != nil {
		return nil, fmt.Errorf("sessionclaim: starttime(%d): %w", ppid, err)
	}

	dir, err := cacheDir()
	if err != nil {
		return nil, fmt.Errorf("sessionclaim: cache dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("sessionclaim: mkdir %s: %w", dir, err)
	}

	token := tokenFor(projectRoot, ppid, starttime)
	fl, err := lockfile.Acquire(filepath.Join(dir, token+".lock"))
	if err != nil {
		return nil, fmt.Errorf("sessionclaim: lock: %w", err)
	}
	defer func() { _ = fl.Release() }()

	existing, err := readByToken(projectRoot, ppid, starttime)
	switch {
	case errors.Is(err, ErrNoClaim):
		// Fresh mint.
		c, mintErr := mintClaim(projectRoot, ppid, starttime)
		if mintErr != nil {
			return nil, mintErr
		}
		if err := writeClaim(dir, token, c); err != nil {
			return nil, err
		}
		sweepStaleClaims(projectRoot, token)
		return c, nil
	case err != nil:
		return nil, err
	}

	// Claim is present — validate its (ppid, starttime) triple.
	if !pidlive.Validate(existing.PPID, existing.PPIDStarttime) {
		log.Printf("sessionclaim: previous session crashed without releasing — minting fresh claim for project_root=%s",
			projectRoot)
		c, mintErr := mintClaim(projectRoot, ppid, starttime)
		if mintErr != nil {
			return nil, mintErr
		}
		if err := writeClaim(dir, token, c); err != nil {
			return nil, err
		}
		sweepStaleClaims(projectRoot, token)
		return c, nil
	}

	// Live triple — refresh.
	maybeWarnProjectRootDrift(existing)
	existing.LastSeenAt = now().UTC()
	if err := writeClaim(dir, token, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// UpdateHarnessSessionID sets the harness-supplied session id on the
// stored claim. Self-bootstraps: if no claim file exists, mints a
// fresh one (H5) — handles hook-only short sessions where no MCP tool
// ever fired. Idempotent: a redundant call with the same harnessID is
// a no-op (does not rewrite the file).
//
// Lock invariant (M10): per-token lock acquired internally, released
// on return.
func UpdateHarnessSessionID(projectRoot string, harnessID string) error {
	ppid := getppid()
	starttime, err := pidlive.Starttime(ppid)
	if err != nil {
		return fmt.Errorf("sessionclaim: starttime(%d): %w", ppid, err)
	}

	dir, err := cacheDir()
	if err != nil {
		return fmt.Errorf("sessionclaim: cache dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("sessionclaim: mkdir %s: %w", dir, err)
	}

	token := tokenFor(projectRoot, ppid, starttime)
	fl, err := lockfile.Acquire(filepath.Join(dir, token+".lock"))
	if err != nil {
		return fmt.Errorf("sessionclaim: lock: %w", err)
	}
	defer func() { _ = fl.Release() }()

	existing, err := readByToken(projectRoot, ppid, starttime)
	switch {
	case errors.Is(err, ErrNoClaim):
		// H5: self-bootstrap — mint a fresh claim, then stamp harnessID.
		c, mintErr := mintClaim(projectRoot, ppid, starttime)
		if mintErr != nil {
			return mintErr
		}
		c.HarnessSessionID = harnessID
		if err := writeClaim(dir, token, c); err != nil {
			return err
		}
		sweepStaleClaims(projectRoot, token)
		return nil
	case err != nil:
		return err
	}

	if existing.HarnessSessionID == harnessID {
		// Already matches — idempotent no-op (no rewrite).
		return nil
	}
	existing.HarnessSessionID = harnessID
	existing.LastSeenAt = now().UTC()
	return writeClaim(dir, token, existing)
}

// ReleaseSession clears the session-identity fields and sets status to
// StatusReleased. Idempotent: returns nil (no-op success) if no claim
// file exists (H7). The file is NOT deleted — it persists so that if
// a stuck process re-checks its (ppid, starttime) triple it finds a
// "released" claim and treats it as stale.
//
// Lock invariant (M10): per-token lock acquired internally, released
// on return.
func ReleaseSession(projectRoot string) error {
	ppid := getppid()
	starttime, err := pidlive.Starttime(ppid)
	if err != nil {
		return fmt.Errorf("sessionclaim: starttime(%d): %w", ppid, err)
	}

	dir, err := cacheDir()
	if err != nil {
		return fmt.Errorf("sessionclaim: cache dir: %w", err)
	}
	// ReleaseSession must NOT create the cache dir on a fresh host —
	// H7 specifies "no warning, no file created" on missing claim.

	token := tokenFor(projectRoot, ppid, starttime)
	lockPath := filepath.Join(dir, token+".lock")

	// If the cache dir doesn't exist, there's nothing to release —
	// short-circuit before we try to take the lock (which would create
	// the directory via lockfile.Acquire).
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return nil
	}

	fl, err := lockfile.Acquire(lockPath)
	if err != nil {
		return fmt.Errorf("sessionclaim: lock: %w", err)
	}
	defer func() { _ = fl.Release() }()

	existing, err := readByToken(projectRoot, ppid, starttime)
	switch {
	case errors.Is(err, ErrNoClaim):
		// H7: no-op success.
		return nil
	case err != nil:
		return err
	}

	existing.DerivedSessionID = ""
	existing.HarnessSessionID = ""
	existing.Status = StatusReleased
	existing.LastSeenAt = now().UTC()
	return writeClaim(dir, token, existing)
}

// EffectiveSessionID returns the harness-supplied id if present, else
// the derived id. Returns "" if c == nil.
func EffectiveSessionID(c *Claim) string {
	if c == nil {
		return ""
	}
	if c.HarnessSessionID != "" {
		return c.HarnessSessionID
	}
	return c.DerivedSessionID
}

// EffectiveSource returns the value to set in CaptureOpts.Source based
// on the recorded harness:
//
//	"claude-code" → ""        (Claude Code default — no source field)
//	"zed-mcp"     → "zed"     (Zed sessions stamped explicitly)
//	otherwise     → "unknown" (defensive — includes nil claim)
func EffectiveSource(c *Claim) string {
	if c == nil {
		return HarnessUnknown
	}
	switch c.Harness {
	case HarnessClaudeCode:
		return ""
	case HarnessZedMCP:
		return "zed"
	default:
		return HarnessUnknown
	}
}

// mintClaim builds a fresh Claim for the current process. Captures
// (ppid, ppid_starttime, projectRoot, cwd, host, harness) and mints a
// random derived_session_id via crypto/rand.
func mintClaim(projectRoot string, ppid int, starttime int64) (*Claim, error) {
	var raw [16]byte
	if _, err := randRead(raw[:]); err != nil {
		return nil, fmt.Errorf("sessionclaim: rand: %w", err)
	}
	derived := "derived:" + hex.EncodeToString(raw[:])

	cwd, _ := getwd() // best-effort — empty string is acceptable.
	host, _ := hostname()

	t := now().UTC()
	return &Claim{
		DerivedSessionID: derived,
		PPID:             ppid,
		PPIDStarttime:    starttime,
		ProjectRoot:      projectRoot,
		CWD:              cwd,
		Harness:          DetectHarness(ppid),
		Host:             host,
		ClaimedAt:        t,
		LastSeenAt:       t,
		Status:           StatusActive,
	}, nil
}

// writeClaim serializes c and atomic-writes it to <dir>/<token>.json.
func writeClaim(dir, token string, c *Claim) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("sessionclaim: marshal: %w", err)
	}
	path := filepath.Join(dir, token+".json")
	return atomicWrite(path, data)
}

// maybeWarnProjectRootDrift logs a single-line warning when the
// claim's recorded cwd resolves to a different project root than the
// current cwd, and BOTH project roots are non-empty. Same-project
// subdirectory cd's do not trigger the warning.
func maybeWarnProjectRootDrift(c *Claim) {
	curCwd, err := getwd()
	if err != nil || curCwd == "" {
		return
	}
	curRoot := session.DetectProjectRoot(curCwd)
	prevRoot := session.DetectProjectRoot(c.CWD)
	if curRoot == "" || prevRoot == "" {
		return
	}
	if curRoot != prevRoot {
		log.Printf("sessionclaim: cwd-drift warning: claim project_root=%s, current project_root=%s",
			prevRoot, curRoot)
	}
}

// sweepStaleClaims walks <cacheDir>/*.json and removes any whose
// project_root matches projectRoot AND whose (PPID, PPIDStarttime)
// triple no longer Validates. Skips selfToken so the just-written
// claim is preserved. Best-effort: errors are logged at warning level
// and ignored.
func sweepStaleClaims(projectRoot, selfToken string) {
	dir, err := cacheDir()
	if err != nil {
		log.Printf("sessionclaim: warning — cache dir: %v", err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing cache dir is benign — nothing to sweep.
		if os.IsNotExist(err) {
			return
		}
		log.Printf("sessionclaim: warning — readdir %s: %v", dir, err)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if filepath.Ext(name) != ".json" {
			continue
		}
		token := name[:len(name)-len(".json")]
		if token == selfToken {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("sessionclaim: warning — read %s: %v", path, err)
			continue
		}
		var c Claim
		if err := json.Unmarshal(data, &c); err != nil {
			log.Printf("sessionclaim: warning — parse %s: %v", path, err)
			continue
		}
		if c.ProjectRoot != projectRoot {
			continue
		}
		if pidlive.Validate(c.PPID, c.PPIDStarttime) {
			continue
		}
		if err := os.Remove(path); err != nil {
			log.Printf("sessionclaim: warning — remove %s: %v", path, err)
		}
	}
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
// creation is the caller's responsibility (every write entry point
// MkdirAll(0o700) lazily on first write).
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "vibe-vault", "session-claims"), nil
}
