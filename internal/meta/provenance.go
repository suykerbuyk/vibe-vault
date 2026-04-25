// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package meta stamps runtime provenance (host, user, cwd) onto vault writes
// so future forensics can answer "which machine wrote this, from which
// directory?" without bisecting git history. The package is stdlib-only and
// every resolver degrades gracefully to the empty string on failure; callers
// are expected to render absent fields (e.g. omit the YAML key, drop the
// trailer fragment) rather than emit a literal empty value.
package meta

import (
	"os"
	osuser "os/user"
)

// Provenance identifies where and by whom a vault write originated.
// Empty fields indicate the corresponding resolver failed; callers should
// render absent fields rather than literal empty values.
type Provenance struct {
	Host string // hostname(), empty on error
	User string // $USER → $LOGNAME → user.Current().Username, empty on all-failure
	CWD  string // os.Getwd(), empty on error
}

// hostnameFunc is the test seam for exercising os.Hostname() failure paths
// deterministically. Tests may replace this; production uses os.Hostname.
var hostnameFunc = os.Hostname

// cwdFunc is the test seam for exercising os.Getwd() failure paths
// deterministically. Tests may replace this; production uses os.Getwd.
var cwdFunc = os.Getwd

// homeDirFunc is the test seam for exercising os.UserHomeDir() failure paths
// deterministically. Tests may replace this; production uses os.UserHomeDir.
var homeDirFunc = os.UserHomeDir

// HomeDir returns the user's home directory, honoring the $VIBE_VAULT_HOME
// sentinel for deterministic testing. Mirrors os.UserHomeDir() for drop-in
// replacement at production call sites.
//
// Exposed as a top-level helper rather than a Provenance field because most
// callers want just the home dir and would otherwise pay the syscall cost
// of resolving Host+User+CWD via Stamp() unnecessarily.
func HomeDir() (string, error) {
	if v := os.Getenv("VIBE_VAULT_HOME"); v != "" {
		return v, nil
	}
	return homeDirFunc()
}

// Stamp resolves host, user, and cwd at the moment of the call. Safe on
// all failure paths — never panics, never returns an error.
func Stamp() Provenance {
	return Provenance{
		Host: hostname(),
		User: user(),
		CWD:  cwd(),
	}
}

// hostname resolves the host label, preferring the VIBE_VAULT_HOSTNAME env
// override so integration tests and operators can pin a deterministic value
// without monkey-patching. The env-var check comes first because os.Hostname
// calls uname(2) directly — it does NOT read $HOSTNAME — so there is no
// stdlib-level way to influence the syscall result from userspace.
func hostname() string {
	if v := os.Getenv("VIBE_VAULT_HOSTNAME"); v != "" {
		return v
	}
	h, err := hostnameFunc()
	if err != nil {
		return ""
	}
	return h
}

// user resolves the acting user, trying $USER then $LOGNAME then the
// os/user lookup. All three can fail on stripped-down containers and in
// certain CI environments; empty string signals "unknown" to callers.
func user() string {
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	if v := os.Getenv("LOGNAME"); v != "" {
		return v
	}
	u, err := osuser.Current()
	if err != nil || u == nil {
		return ""
	}
	return u.Username
}

// cwd resolves the current working directory, preferring the
// VIBE_VAULT_CWD env override so integration tests and operators
// can pin a deterministic value without monkey-patching. Mirrors
// the hostname() precedent. Returns empty string if the cwd has
// been deleted underneath us or the lookup otherwise fails.
func cwd() string {
	if v := os.Getenv("VIBE_VAULT_CWD"); v != "" {
		return v
	}
	d, err := cwdFunc()
	if err != nil {
		return ""
	}
	return d
}
