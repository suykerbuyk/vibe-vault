// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"log"
	"os"
	"path/filepath"
)

// disableEnv is the operator opt-out hatch for staging routing. When
// set to a truthy value ("1", "true", "yes"), ResolveRoot returns ""
// and every entry point reverts to the legacy flat-vault layout.
//
// Used by integration tests that pre-date Phase 2 and assert against
// vault-relative paths; also a documented operator escape hatch for
// debugging the back-compat surface in unconfigured environments.
const disableEnv = "VIBE_VAULT_DISABLE_STAGING"

// ResolveRoot returns the staging root that callers should pass into
// session.CaptureOpts.StagingRoot. cfgRoot wins when non-empty
// (operator override via the [staging] root TOML key); otherwise
// Root() resolves the XDG default. A resolution error returns "" with
// a WARN log — callers preserve back-compat flat-vault behavior on
// error rather than blocking the write.
//
// VIBE_VAULT_DISABLE_STAGING="1" forces "" (legacy flat-vault layout)
// regardless of cfgRoot. Documented escape hatch for migration and
// for integration tests that pre-date Phase 2.
//
// Centralized here so all five session-write entry points (hook,
// `vv process`, `vv backfill`, `vv reprocess`, Zed batch, MCP
// `vv_capture_session`) share one resolution rule.
func ResolveRoot(cfgRoot string) string {
	if isDisabled() {
		return ""
	}
	if cfgRoot != "" {
		return cfgRoot
	}
	root, err := Root()
	if err != nil {
		log.Printf("warning: resolve staging root: %v (falling back to flat-vault layout)", err)
		return ""
	}
	return root
}

// isDisabled reports whether the operator opt-out env var is set to
// a truthy value. Tolerates the common forms ("1", "true", "yes",
// case-insensitive) so test harnesses and shell exports both work.
func isDisabled() bool {
	switch v := os.Getenv(disableEnv); v {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes":
		return true
	}
	return false
}

// EnsureInit guarantees the staging dir for project is bootstrapped
// before a hook fires. Two-stat fast path (~20µs warm): if both the
// .init-done sentinel AND .git/HEAD are present, return nil immediately.
//
// If EITHER is missing, fall back to Init() which is idempotent and
// re-runs only the missing steps. The .git/HEAD co-check bounds the
// v4-M1 TOCTOU window where the sentinel survives a manual
// `rm -rf .git/` — without it, the hook would attempt a `git add` in a
// non-repo and fail loudly per fire.
//
// In-process (no fork-exec on the warm path), so it is safe to call
// from every session-write entry point at handler bootstrap.
//
// Uses Root() (XDG default). Callers with a cfg-supplied override
// should use EnsureInitAt to keep the init target in sync with the
// write target.
func EnsureInit(project string) error {
	dir, err := Path(project)
	if err != nil {
		return err
	}
	return ensureInitAtDir(dir, project)
}

// EnsureInitAt is the cfg-aware variant of EnsureInit: it uses the
// supplied root rather than re-resolving via Root(). All five Phase 2
// session-write entry points pass cfg.Staging.Root through to
// session.CaptureOpts.StagingRoot; they must use this variant so the
// init target matches the write target when cfg overrides the
// XDG default.
//
// Empty root falls back to EnsureInit (XDG default) for back-compat.
func EnsureInitAt(root, project string) error {
	if root == "" {
		return EnsureInit(project)
	}
	if project == "" {
		return ErrEmptyProject
	}
	dir := filepath.Join(root, project)
	return ensureInitAtDir(dir, project)
}

// ensureInitAtDir is the shared two-stat fast path + lazy Init
// fallback. Init() always uses Root()-derived paths, so when root
// differs from XDG we run InitAt (a thin wrapper that pins the
// staging dir explicitly) instead.
func ensureInitAtDir(dir, project string) error {
	if fileExists(filepath.Join(dir, SentinelName)) &&
		fileExists(filepath.Join(dir, ".git", "HEAD")) {
		return nil
	}
	return InitAt(dir)
}
