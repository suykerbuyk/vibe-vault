// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/pidlive"
)

// fixedStarttime is the canonical value tests inject via
// pidlive.Starttime so token derivation in Read matches the value tests
// use to plant fixture files. Using a non-zero value keeps tests honest
// against the Phase 3 wiring that replaced Phase 0a's hardcoded zero.
const fixedStarttime int64 = 1714683121123

// withFixedStarttime overrides pidlive.Starttime to return
// fixedStarttime for the duration of t.
func withFixedStarttime(t *testing.T) {
	t.Helper()
	orig := pidlive.Starttime
	pidlive.Starttime = func(int) (int64, error) { return fixedStarttime, nil }
	t.Cleanup(func() { pidlive.Starttime = orig })
}

func TestRead_NoFile(t *testing.T) {
	withFixedStarttime(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	claim, err := Read("/some/project/root")
	if claim != nil {
		t.Errorf("Read returned non-nil claim on missing file: %+v", claim)
	}
	if !errors.Is(err, ErrNoClaim) {
		t.Errorf("Read err = %v, want ErrNoClaim", err)
	}
}

func TestRead_RoundTrip(t *testing.T) {
	withFixedStarttime(t)
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	// Mirror Read's token derivation so we know where to plant the
	// fixture file. Phase 3 uses pidlive.Starttime (overridden above to
	// fixedStarttime) instead of Phase 0a's hardcoded zero.
	projectRoot := "/home/johns/code/vibe-vault"
	ppid := os.Getppid()
	starttime := fixedStarttime
	token := tokenFor(projectRoot, ppid, starttime)

	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir: %v", err)
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	claimedAt := time.Date(2026, 5, 2, 14, 32, 1, 123e6, time.UTC)
	lastSeen := time.Date(2026, 5, 2, 14, 55, 18, 901e6, time.UTC)
	want := Claim{
		DerivedSessionID: "derived:9f12e7a8b6c5d4f3e2a1b0c9d8e7f6a5",
		HarnessSessionID: "1275303e-abc",
		PPID:             ppid,
		PPIDStarttime:    starttime,
		ProjectRoot:      projectRoot,
		CWD:              projectRoot,
		Harness:          "claude-code",
		Host:             "s76",
		ClaimedAt:        claimedAt,
		LastSeenAt:       lastSeen,
		Status:           "active",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, token+".json"), data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Read(projectRoot)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned nil claim")
	}
	if got.DerivedSessionID != want.DerivedSessionID {
		t.Errorf("DerivedSessionID = %q, want %q", got.DerivedSessionID, want.DerivedSessionID)
	}
	if got.HarnessSessionID != want.HarnessSessionID {
		t.Errorf("HarnessSessionID = %q, want %q", got.HarnessSessionID, want.HarnessSessionID)
	}
	if got.PPID != want.PPID {
		t.Errorf("PPID = %d, want %d", got.PPID, want.PPID)
	}
	if got.PPIDStarttime != want.PPIDStarttime {
		t.Errorf("PPIDStarttime = %d, want %d", got.PPIDStarttime, want.PPIDStarttime)
	}
	if got.ProjectRoot != want.ProjectRoot {
		t.Errorf("ProjectRoot = %q, want %q", got.ProjectRoot, want.ProjectRoot)
	}
	if got.CWD != want.CWD {
		t.Errorf("CWD = %q, want %q", got.CWD, want.CWD)
	}
	if got.Harness != want.Harness {
		t.Errorf("Harness = %q, want %q", got.Harness, want.Harness)
	}
	if got.Host != want.Host {
		t.Errorf("Host = %q, want %q", got.Host, want.Host)
	}
	if !got.ClaimedAt.Equal(want.ClaimedAt) {
		t.Errorf("ClaimedAt = %v, want %v", got.ClaimedAt, want.ClaimedAt)
	}
	if !got.LastSeenAt.Equal(want.LastSeenAt) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, want.LastSeenAt)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
}

func TestTokenFor_Stable(t *testing.T) {
	a := tokenFor("/a", 100, 200)
	b := tokenFor("/a", 100, 200)
	if a != b {
		t.Errorf("tokenFor not stable: %q vs %q", a, b)
	}
	if len(a) != 32 {
		t.Errorf("tokenFor len = %d, want 32", len(a))
	}
}

func TestTokenFor_DifferentInputs(t *testing.T) {
	base := tokenFor("/a", 100, 200)
	cases := []struct {
		name        string
		projectRoot string
		ppid        int
		starttime   int64
	}{
		{"different projectRoot", "/b", 100, 200},
		{"different ppid", "/a", 101, 200},
		{"different starttime", "/a", 100, 201},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tokenFor(c.projectRoot, c.ppid, c.starttime)
			if got == base {
				t.Errorf("tokenFor(%q, %d, %d) = %q matched base — distinct inputs collided",
					c.projectRoot, c.ppid, c.starttime, got)
			}
			if len(got) != 32 {
				t.Errorf("tokenFor len = %d, want 32", len(got))
			}
		})
	}
}

func TestCacheDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir: %v", err)
	}
	want := filepath.Join("vibe-vault", "session-claims")
	if !strings.HasSuffix(dir, want) {
		t.Errorf("cacheDir() = %q, want suffix %q", dir, want)
	}
}

func TestRead_BadJSON(t *testing.T) {
	withFixedStarttime(t)
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	projectRoot := "/garbage/case"
	ppid := os.Getppid()
	starttime := fixedStarttime
	token := tokenFor(projectRoot, ppid, starttime)

	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir: %v", err)
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, token+".json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	claim, err := Read(projectRoot)
	if claim != nil {
		t.Errorf("Read returned non-nil claim on garbage JSON: %+v", claim)
	}
	if err == nil {
		t.Fatal("Read returned nil error on garbage JSON")
	}
	if errors.Is(err, ErrNoClaim) {
		t.Errorf("Read err = ErrNoClaim on garbage JSON; want a parse error")
	}
}
