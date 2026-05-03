// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package staging owns the host-local staging dir for session capture in
// the two-tier vault (β2) layout. The staging dir lives outside the shared
// Obsidian vault — by default at $XDG_STATE_HOME/vibe-vault/<project>/
// (or ~/.local/state/vibe-vault/<project>/) — and is the per-host write
// target for the SessionEnd / Stop / PreCompact hook. A wrap-time mirror
// (Phase 3) projects staging contents into <vault>/Projects/<p>/sessions/
// /<host>/ for cross-host browse.
//
// Phase 1 (this file set) lands the bootstrap surface: directory layout,
// repo-local git identity, hostname sanitization, and the `vv staging`
// CLI subcommand group. Sibling packages call vaultsync.GitCommand for
// every git operation — there is no parallel fork-exec layer here.
package staging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/atomicfile"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
)

// SentinelName is the marker file dropped by Init to signal "this staging
// project has been bootstrapped." The hook auto-init fast path stats this
// file (and .git/HEAD) to skip re-running Init on every fire.
const SentinelName = ".init-done"

// gitTimeout bounds every staging git invocation. Generous enough for
// disk-bound init / config writes on slow filesystems, short enough that
// a hung git never wedges the hook.
const gitTimeout = 10 * time.Second

// hostnameAllowed is the per-rune allowlist for SanitizeHostname.
// Anything outside [a-zA-Z0-9._-] is rewritten to '_'. The allowlist is
// intentionally narrower than POSIX hostname rules: it has to round-trip
// safely as a path component on every platform vibe-vault targets.
func hostnameAllowed(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	}
	return false
}

// SanitizeHostname returns a path-safe form of s. Allowed runes pass
// through; everything else is replaced by '_'. The empty string, ".",
// and ".." all reject to "_unknown" — neither would round-trip safely
// as a directory component, and both would let a hostname-derived path
// escape its parent.
//
// Special case: any rune adjacent to a non-allowed rune (i.e. a rune
// that would already become '_') is itself rewritten to '_' if it is
// '.'. This blocks `../escape`-style inputs from reducing to
// `.._escape`, which would still be path-traversal-suspect when joined
// into a longer path. The classic safe-host inputs `host.local` and
// `1.2.3.4` are unaffected because their dots have allowed neighbors.
//
// Two operators whose hostnames sanitize to the same string (e.g.
// "host/1" and "host_1") would collide on the per-host vault subtree;
// documented as a known constraint.
func SanitizeHostname(s string) string {
	if s == "" || s == "." || s == ".." {
		return "_unknown"
	}
	runes := []rune(s)
	bad := make([]bool, len(runes))
	for i, r := range runes {
		if !hostnameAllowed(r) {
			bad[i] = true
		}
	}
	// Iterative pass: rewrite '.' to '_' when adjacent to a bad rune,
	// propagating until quiescence. The adjacency check catches `../escape`
	// (dots next to '/'), `..host` (leading dot-dot), and `host/.` (trailing
	// dot after '/'); the propagation step catches a run of dots adjacent
	// to a single bad rune (e.g. `..` next to `/`, where the first pass
	// only flags the dot directly touching the slash). Allowed-neighbor
	// dots (`host.local`, `1.2.3.4`) are unaffected — every dot in those
	// inputs is between two allowed runes and stays clean across all passes.
	for {
		changed := false
		for i, r := range runes {
			if r != '.' || bad[i] {
				continue
			}
			left := i > 0 && bad[i-1]
			right := i+1 < len(runes) && bad[i+1]
			if left || right {
				bad[i] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	var b strings.Builder
	b.Grow(len(runes))
	for i, r := range runes {
		if bad[i] {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := b.String()
	// Defense-in-depth: a degenerate sanitization (all-rejected unicode,
	// or a literal "." / "..") still resolves to "_unknown" so logs
	// read clearly and downstream path joins never produce an empty
	// or traversal-prone segment.
	if out == "" || out == "." || out == ".." {
		return "_unknown"
	}
	return out
}

// Root returns the staging root directory. Honors $XDG_STATE_HOME when
// set; otherwise falls back to <home>/.local/state/vibe-vault. Cheap to
// call repeatedly (one or two env reads, no I/O).
//
// The returned path is suitable to os.MkdirAll — it does not have to
// exist yet. Init creates the per-project subdirectory; callers that
// need a project path should prefer Path().
func Root() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "vibe-vault"), nil
	}
	home, err := meta.HomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("staging: home dir is empty and XDG_STATE_HOME unset")
	}
	return filepath.Join(home, ".local", "state", "vibe-vault"), nil
}

// Path returns the staging directory for a single project:
// <Root()>/<project>/. No I/O beyond the one Root() resolution; the
// directory is not guaranteed to exist (Init creates it).
//
// project must be non-empty. Path component rules are the caller's
// responsibility — vibe-vault project names are themselves filesystem
// safe by construction (see internal/identity).
func Path(project string) (string, error) {
	if project == "" {
		return "", errors.New("staging: project name is empty")
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, project), nil
}

// Init bootstraps the staging dir for a project. Idempotent: if the
// staging dir is already a git repo with the sentinel present, returns
// nil without touching disk.
//
// On a fresh dir: creates <root>/<project>/, runs `git init -b main`,
// writes repo-local user.email = vibe-vault@<sanitized-host> and
// user.name = vibe-vault into .git/config, then drops the sentinel.
// Repo-local identity means the hook never depends on global git
// config — fixes the v3-H2 review concern and lets the hook run
// cleanly on hosts where the operator never set `git config --global
// user.email`.
//
// Re-running on a partially-initialized dir (sentinel missing but
// .git/HEAD present, or vice-versa) re-runs the missing steps; both
// must be present for the fast-path skip.
func Init(project string) error {
	dir, err := Path(project)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create staging dir %s: %w", dir, err)
	}

	gitHEAD := filepath.Join(dir, ".git", "HEAD")
	sentinelPath := filepath.Join(dir, SentinelName)

	gitInited := fileExists(gitHEAD)
	sentinelPresent := fileExists(sentinelPath)

	if gitInited && sentinelPresent {
		return nil // fully initialized — fast-path no-op
	}

	if !gitInited {
		if _, err := vaultsync.GitCommand(dir, gitTimeout, "init", "-b", "main"); err != nil {
			return fmt.Errorf("git init in %s: %w", dir, err)
		}
	}

	host := SanitizeHostname(currentHostname())
	email := "vibe-vault@" + host
	if _, err := vaultsync.GitCommand(dir, gitTimeout, "config", "user.email", email); err != nil {
		return fmt.Errorf("git config user.email: %w", err)
	}
	if _, err := vaultsync.GitCommand(dir, gitTimeout, "config", "user.name", "vibe-vault"); err != nil {
		return fmt.Errorf("git config user.name: %w", err)
	}

	if !sentinelPresent {
		// atomicfile.Write is the canonical small-file write primitive.
		// vaultPath="" suppresses the .surface stamp side-channel — the
		// staging dir is not a vault subtree.
		if err := atomicfile.Write("", sentinelPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n")); err != nil {
			return fmt.Errorf("write sentinel: %w", err)
		}
	}
	return nil
}

// hostnameFunc is the test seam for SanitizeHostname / Init host
// resolution. Production calls os.Hostname; tests may override to
// exercise reject / collision paths deterministically without
// changing $VIBE_VAULT_HOSTNAME.
var hostnameFunc = os.Hostname

// currentHostname mirrors meta.hostname's env-override precedence so
// operators can pin a deterministic host label via VIBE_VAULT_HOSTNAME
// (used during host renames and in tests). The env-var check comes
// first; os.Hostname()'s uname(2) syscall does not honor $HOSTNAME.
func currentHostname() string {
	if v := os.Getenv("VIBE_VAULT_HOSTNAME"); v != "" {
		return v
	}
	h, err := hostnameFunc()
	if err != nil || h == "" {
		return "_unknown"
	}
	return h
}

// fileExists reports whether path resolves to a regular file or
// directory. Errors (including ENOENT) collapse to false; the caller
// re-runs the relevant init step on any negative result.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
