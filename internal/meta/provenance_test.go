// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package meta

import (
	"errors"
	"os"
	osuser "os/user"
	"testing"
)

// TestStampHappyPath asserts that in a normal environment Stamp returns
// a populated struct. We don't pin Host to a specific value — hostnameFunc's
// output is machine-dependent — only that the resolver produced something
// non-empty. USER is pinned to an arbitrary sentinel to decouple from CI's
// actual user.
func TestStampHappyPath(t *testing.T) {
	t.Setenv("USER", "testuser")
	t.Setenv("VIBE_VAULT_HOSTNAME", "")

	got := Stamp()

	if got.Host == "" {
		t.Errorf("Host empty in happy path; hostnameFunc err? got %+v", got)
	}
	if got.User != "testuser" {
		t.Errorf("User = %q, want %q", got.User, "testuser")
	}
	if got.CWD == "" {
		t.Errorf("CWD empty; os.Getwd should succeed in a normal test env")
	}
}

// TestStampHostnameOverride asserts VIBE_VAULT_HOSTNAME short-circuits the
// hostnameFunc call entirely. We replace hostnameFunc with a panic-func to
// prove the override path never reaches it.
func TestStampHostnameOverride(t *testing.T) {
	t.Setenv("VIBE_VAULT_HOSTNAME", "sentinel-host")
	t.Setenv("USER", "irrelevant")

	original := hostnameFunc
	t.Cleanup(func() { hostnameFunc = original })
	hostnameFunc = func() (string, error) {
		t.Fatalf("hostnameFunc should not be called when VIBE_VAULT_HOSTNAME is set")
		return "", nil
	}

	got := Stamp()
	if got.Host != "sentinel-host" {
		t.Errorf("Host = %q, want %q", got.Host, "sentinel-host")
	}
}

// TestStampUserFallback walks the $USER → $LOGNAME → user.Current() chain.
// The last sub-case depends on osuser.Current succeeding in CI; we tolerate
// either a populated username or an empty string (container environments
// sometimes lack a passwd entry for the test uid).
func TestStampUserFallback(t *testing.T) {
	// Pin hostname resolution so this test only exercises the user path.
	t.Setenv("VIBE_VAULT_HOSTNAME", "host-pin")

	cases := []struct {
		name    string
		user    string
		logname string
		want    string // empty sentinel "" means "whatever user.Current returns"
	}{
		{name: "user_wins_over_logname", user: "alice", logname: "bob", want: "alice"},
		{name: "logname_when_user_empty", user: "", logname: "bob", want: "bob"},
		{name: "both_unset_falls_back", user: "", logname: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USER", tc.user)
			t.Setenv("LOGNAME", tc.logname)

			got := Stamp()

			if tc.want != "" {
				if got.User != tc.want {
					t.Errorf("User = %q, want %q", got.User, tc.want)
				}
				return
			}
			// Fallback sub-case: compare to osuser.Current(). If Current errs,
			// Stamp should return ""; if it succeeds, Stamp should match.
			u, err := osuser.Current()
			if err != nil || u == nil {
				if got.User != "" {
					t.Errorf("User = %q, want \"\" (user.Current err: %v)", got.User, err)
				}
				return
			}
			if got.User != u.Username {
				t.Errorf("User = %q, want %q (from user.Current)", got.User, u.Username)
			}
		})
	}
}

// TestStampHostnameFailure replaces hostnameFunc with a failing func and
// clears VIBE_VAULT_HOSTNAME so the env override doesn't short-circuit the
// error path. Stamp must degrade to Host="" without panicking.
func TestStampHostnameFailure(t *testing.T) {
	t.Setenv("VIBE_VAULT_HOSTNAME", "")
	t.Setenv("USER", "someone")

	original := hostnameFunc
	t.Cleanup(func() { hostnameFunc = original })
	hostnameFunc = func() (string, error) {
		return "", errors.New("boom")
	}

	got := Stamp()
	if got.Host != "" {
		t.Errorf("Host = %q, want \"\" on hostnameFunc failure", got.Host)
	}
	if got.User != "someone" {
		t.Errorf("User = %q, want \"someone\" (unrelated to host failure)", got.User)
	}
}

// TestStampCWDOverride asserts VIBE_VAULT_CWD short-circuits the cwdFunc
// call entirely, mirroring the VIBE_VAULT_HOSTNAME pattern. We install a
// cwdFunc that would fail the test if it were reached.
func TestStampCWDOverride(t *testing.T) {
	t.Setenv("VIBE_VAULT_CWD", "/sentinel")
	t.Setenv("VIBE_VAULT_HOSTNAME", "host-pin")
	t.Setenv("USER", "user-pin")

	original := cwdFunc
	t.Cleanup(func() { cwdFunc = original })
	cwdFunc = func() (string, error) {
		t.Fatalf("cwdFunc should not be called when VIBE_VAULT_CWD is set")
		return "", nil
	}

	got := Stamp()
	if got.CWD != "/sentinel" {
		t.Errorf("CWD = %q, want %q", got.CWD, "/sentinel")
	}
}

// TestStampCWDFallback asserts that when VIBE_VAULT_CWD is empty the resolver
// falls through to cwdFunc and returns its value verbatim. Covers the
// happy path of the testability seam added in Phase 6.0.
func TestStampCWDFallback(t *testing.T) {
	t.Setenv("VIBE_VAULT_CWD", "")
	t.Setenv("VIBE_VAULT_HOSTNAME", "host-pin")
	t.Setenv("USER", "user-pin")

	original := cwdFunc
	t.Cleanup(func() { cwdFunc = original })
	cwdFunc = func() (string, error) {
		return "/fake-cwd", nil
	}

	got := Stamp()
	if got.CWD != "/fake-cwd" {
		t.Errorf("CWD = %q, want %q", got.CWD, "/fake-cwd")
	}
}

// TestStampCWDFailure replaces cwdFunc with a failing func and clears
// VIBE_VAULT_CWD so the env override doesn't short-circuit the error path.
// Stamp must degrade to CWD="" without panicking — symmetric to the
// hostname-failure contract.
func TestStampCWDFailure(t *testing.T) {
	t.Setenv("VIBE_VAULT_CWD", "")
	t.Setenv("VIBE_VAULT_HOSTNAME", "host-pin")
	t.Setenv("USER", "user-pin")

	original := cwdFunc
	t.Cleanup(func() { cwdFunc = original })
	cwdFunc = func() (string, error) {
		return "", errors.New("boom")
	}

	got := Stamp()
	if got.CWD != "" {
		t.Errorf("CWD = %q, want \"\" on cwdFunc failure", got.CWD)
	}
}

// TestStampCWDDeleted exercises the cwd()-failure path by chdir-ing into a
// tempdir and removing it before calling Stamp. On Linux os.Getwd returns an
// error (or a path suffixed with " (deleted)") once the inode is gone — both
// are acceptable as long as Stamp doesn't panic and returns some CWD value.
// Skipped if the platform keeps returning the original path (macOS under
// some filesystems).
func TestStampCWDDeleted(t *testing.T) {
	// Save and restore process-wide cwd so we don't leak into sibling tests.
	orig, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot determine starting cwd: %v", err)
	}
	t.Cleanup(func() {
		if chErr := os.Chdir(orig); chErr != nil {
			t.Logf("failed to restore cwd to %q: %v", orig, chErr)
		}
	})

	dir := t.TempDir()
	// t.TempDir auto-cleans, but we need to remove it *before* calling Stamp
	// while we're still chdir'd inside.
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to tempdir: %v", err)
	}
	if err := os.Remove(dir); err != nil {
		// On some platforms the dir may be non-empty or locked; skip rather
		// than fail, since the test's premise (deleted cwd) is moot.
		t.Skipf("cannot remove tempdir underneath cwd: %v", err)
	}

	// Whatever happens, Stamp must not panic. We don't pin CWD's value:
	// Linux kernels return "" (via Getwd err) or "<path> (deleted)"; either
	// is fine. The contract is "no panic, no error bubbling".
	got := Stamp()
	_ = got.CWD // intentionally unasserted; platform-dependent
}
