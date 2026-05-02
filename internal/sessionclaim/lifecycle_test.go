// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/pidlive"
)

// withLiveTriple sets pidlive.Starttime + Validate to mock a live
// (ppid, starttime) triple. Tests that mint claims under a synthetic
// triple should call this to ensure the just-minted claim is treated
// as live by AcquireOrRefresh's revalidate branch.
func withLiveTriple(t *testing.T) {
	t.Helper()
	origStart := pidlive.Starttime
	origValidate := pidlive.Validate
	pidlive.Starttime = func(int) (int64, error) { return fixedStarttime, nil }
	pidlive.Validate = func(_ int, st int64) bool { return st == fixedStarttime }
	t.Cleanup(func() {
		pidlive.Starttime = origStart
		pidlive.Validate = origValidate
	})
}

// withCacheRoot installs a per-test XDG_CACHE_HOME so claim files do
// not leak across tests.
func withCacheRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	return root
}

func TestAcquireOrRefresh_FromEmpty(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	c, err := AcquireOrRefresh("/proj/A")
	if err != nil {
		t.Fatalf("AcquireOrRefresh: %v", err)
	}
	if c == nil {
		t.Fatal("AcquireOrRefresh returned nil claim")
	}
	if !strings.HasPrefix(c.DerivedSessionID, "derived:") {
		t.Errorf("DerivedSessionID prefix = %q, want derived:", c.DerivedSessionID)
	}
	if got := len(c.DerivedSessionID); got != len("derived:")+32 {
		t.Errorf("DerivedSessionID len = %d, want %d", got, len("derived:")+32)
	}
	if c.Status != StatusActive {
		t.Errorf("Status = %q, want %q", c.Status, StatusActive)
	}
	if c.HarnessSessionID != "" {
		t.Errorf("HarnessSessionID = %q, want empty (no hook fired)", c.HarnessSessionID)
	}

	// File exists at expected path with mode 0o600.
	dir, _ := cacheDir()
	token := tokenFor("/proj/A", os.Getppid(), fixedStarttime)
	path := filepath.Join(dir, token+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat claim file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("claim mode = %o, want 0600", mode)
	}
}

func TestAcquireOrRefresh_RefreshExisting(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	c1, err := AcquireOrRefresh("/proj/B")
	if err != nil {
		t.Fatalf("first AcquireOrRefresh: %v", err)
	}
	// Force a measurable LastSeenAt advance.
	time.Sleep(2 * time.Millisecond)

	c2, err := AcquireOrRefresh("/proj/B")
	if err != nil {
		t.Fatalf("second AcquireOrRefresh: %v", err)
	}
	if c1.DerivedSessionID != c2.DerivedSessionID {
		t.Errorf("DerivedSessionID changed: %q -> %q", c1.DerivedSessionID, c2.DerivedSessionID)
	}
	if !c2.LastSeenAt.After(c2.ClaimedAt) {
		t.Errorf("LastSeenAt (%v) not after ClaimedAt (%v) after refresh", c2.LastSeenAt, c2.ClaimedAt)
	}
}

func TestAcquireOrRefresh_StalePID(t *testing.T) {
	withCacheRoot(t)

	// Step 1: install a "live" triple long enough to mint, then
	// rebuild a stale-validate before the second call.
	origStart := pidlive.Starttime
	origValidate := pidlive.Validate
	pidlive.Starttime = func(int) (int64, error) { return fixedStarttime, nil }
	pidlive.Validate = func(_ int, st int64) bool { return st == fixedStarttime }
	t.Cleanup(func() {
		pidlive.Starttime = origStart
		pidlive.Validate = origValidate
	})

	c1, err := AcquireOrRefresh("/proj/stale")
	if err != nil {
		t.Fatalf("first AcquireOrRefresh: %v", err)
	}

	// Now flip Validate to false so the existing claim looks stale.
	pidlive.Validate = func(int, int64) bool { return false }

	c2, err := AcquireOrRefresh("/proj/stale")
	if err != nil {
		t.Fatalf("second AcquireOrRefresh: %v", err)
	}
	if c1.DerivedSessionID == c2.DerivedSessionID {
		t.Errorf("DerivedSessionID stayed %q despite stale validate; expected fresh mint",
			c1.DerivedSessionID)
	}
}

func TestAcquireOrRefresh_HookWinsReconciliation(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	if err := UpdateHarnessSessionID("/proj/H", "harness-id-A"); err != nil {
		t.Fatalf("UpdateHarnessSessionID: %v", err)
	}
	c, err := AcquireOrRefresh("/proj/H")
	if err != nil {
		t.Fatalf("AcquireOrRefresh: %v", err)
	}
	if c.HarnessSessionID != "harness-id-A" {
		t.Errorf("HarnessSessionID = %q, want %q", c.HarnessSessionID, "harness-id-A")
	}
	if c.DerivedSessionID == "" {
		t.Error("DerivedSessionID empty after hook-wins reconciliation; expected the original mint to survive")
	}
}

func TestUpdateHarnessSessionID_SelfBootstrap(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	if err := UpdateHarnessSessionID("/proj/Boot", "id-X"); err != nil {
		t.Fatalf("UpdateHarnessSessionID: %v", err)
	}
	c, err := Read("/proj/Boot")
	if err != nil {
		t.Fatalf("Read after self-bootstrap: %v", err)
	}
	if c.HarnessSessionID != "id-X" {
		t.Errorf("HarnessSessionID = %q, want id-X", c.HarnessSessionID)
	}
	if c.DerivedSessionID == "" {
		t.Error("DerivedSessionID empty after self-bootstrap")
	}
	if c.Status != StatusActive {
		t.Errorf("Status = %q, want active", c.Status)
	}
	if c.PPID != os.Getppid() {
		t.Errorf("PPID = %d, want %d", c.PPID, os.Getppid())
	}
}

func TestUpdateHarnessSessionID_Idempotent(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	if err := UpdateHarnessSessionID("/proj/Idem", "id-Y"); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	dir, _ := cacheDir()
	token := tokenFor("/proj/Idem", os.Getppid(), fixedStarttime)
	path := filepath.Join(dir, token+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if err := UpdateHarnessSessionID("/proj/Idem", "id-Y"); err != nil {
		t.Fatalf("second Update: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("idempotent Update rewrote file:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestReleaseSession_Idempotent_Missing(t *testing.T) {
	withLiveTriple(t)
	cacheRoot := withCacheRoot(t)

	// Capture log output to confirm no warning fires.
	var buf bytes.Buffer
	origLog := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origLog) })

	if err := ReleaseSession("/proj/Empty"); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}
	if buf.Len() > 0 {
		t.Errorf("ReleaseSession on missing file logged: %q (want silent)", buf.String())
	}

	// Cache dir must NOT exist after a release-on-empty.
	dir := filepath.Join(cacheRoot, "vibe-vault", "session-claims")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cache dir exists after release-on-empty: stat err = %v", err)
	}
}

func TestReleaseSession_ClearsFields(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	if _, err := AcquireOrRefresh("/proj/Rel"); err != nil {
		t.Fatalf("AcquireOrRefresh: %v", err)
	}
	if err := ReleaseSession("/proj/Rel"); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}

	dir, _ := cacheDir()
	token := tokenFor("/proj/Rel", os.Getppid(), fixedStarttime)
	path := filepath.Join(dir, token+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var c Claim
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if c.DerivedSessionID != "" {
		t.Errorf("DerivedSessionID = %q, want empty after release", c.DerivedSessionID)
	}
	if c.HarnessSessionID != "" {
		t.Errorf("HarnessSessionID = %q, want empty after release", c.HarnessSessionID)
	}
	if c.Status != StatusReleased {
		t.Errorf("Status = %q, want %q", c.Status, StatusReleased)
	}
}

func TestAcquireOrRefresh_IOError(t *testing.T) {
	withLiveTriple(t)

	// Point UserCacheDir at a path that os.MkdirAll can't create
	// (a regular file standing where the directory needs to be).
	tmp := t.TempDir()
	cacheBase := filepath.Join(tmp, "cache")
	if err := os.WriteFile(cacheBase, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("plant blocking file: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", cacheBase)

	c, err := AcquireOrRefresh("/proj/io")
	if err == nil {
		t.Fatalf("AcquireOrRefresh: expected error, got nil claim=%+v", c)
	}
	if c != nil {
		t.Errorf("AcquireOrRefresh: claim = %+v, want nil on error", c)
	}
}

func TestEffectiveSource_Matrix(t *testing.T) {
	cases := []struct {
		name string
		in   *Claim
		want string
	}{
		{"nil", nil, HarnessUnknown},
		{"claude-code", &Claim{Harness: HarnessClaudeCode}, ""},
		{"zed-mcp", &Claim{Harness: HarnessZedMCP}, "zed"},
		{"unknown", &Claim{Harness: HarnessUnknown}, HarnessUnknown},
		{"empty harness", &Claim{}, HarnessUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EffectiveSource(c.in)
			if got != c.want {
				t.Errorf("EffectiveSource = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEffectiveSessionID_Matrix(t *testing.T) {
	cases := []struct {
		name string
		in   *Claim
		want string
	}{
		{"nil", nil, ""},
		{"derived only", &Claim{DerivedSessionID: "derived:abc"}, "derived:abc"},
		{"harness wins", &Claim{DerivedSessionID: "derived:abc", HarnessSessionID: "h-1"}, "h-1"},
		{"both empty", &Claim{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EffectiveSessionID(c.in)
			if got != c.want {
				t.Errorf("EffectiveSessionID = %q, want %q", got, c.want)
			}
		})
	}
}

func TestProjectRootDriftWarning(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	// Stage 1: build two real-on-disk git repos so DetectProjectRoot
	// returns absolute paths for both.
	parent := t.TempDir()
	repoA := filepath.Join(parent, "A")
	repoB := filepath.Join(parent, "B")
	for _, r := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(r, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir %s/.git: %v", r, err)
		}
	}
	subA := filepath.Join(repoA, "sub")
	if err := os.MkdirAll(subA, 0o755); err != nil {
		t.Fatalf("mkdir subA: %v", err)
	}

	// First mint: cwd = repoA root.
	origGetwd := getwd
	getwd = func() (string, error) { return repoA, nil }
	t.Cleanup(func() { getwd = origGetwd })

	if _, err := AcquireOrRefresh(repoA); err != nil {
		t.Fatalf("first AcquireOrRefresh: %v", err)
	}

	// Second call: cwd = repoA/sub (same project) — must NOT warn.
	var buf bytes.Buffer
	origLog := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origLog) })

	getwd = func() (string, error) { return subA, nil }
	if _, err := AcquireOrRefresh(repoA); err != nil {
		t.Fatalf("subdir AcquireOrRefresh: %v", err)
	}
	if strings.Contains(buf.String(), "cwd-drift") {
		t.Errorf("subdir cd produced cwd-drift warning: %q", buf.String())
	}

	// Third call: cwd = repoB (different project) — must warn once.
	buf.Reset()
	getwd = func() (string, error) { return repoB, nil }
	if _, err := AcquireOrRefresh(repoA); err != nil {
		t.Fatalf("cross-project AcquireOrRefresh: %v", err)
	}
	if !strings.Contains(buf.String(), "cwd-drift") {
		t.Errorf("cross-project cd did not warn; buf = %q", buf.String())
	}
}

func TestSweepStaleClaims(t *testing.T) {
	withCacheRoot(t)

	origStart := pidlive.Starttime
	origValidate := pidlive.Validate
	pidlive.Starttime = func(int) (int64, error) { return fixedStarttime, nil }
	pidlive.Validate = func(_ int, st int64) bool { return st == fixedStarttime }
	t.Cleanup(func() {
		pidlive.Starttime = origStart
		pidlive.Validate = origValidate
	})

	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// File A: project matches, triple alive (won't sweep).
	tokenA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	plantClaim(t, dir, tokenA, &Claim{
		ProjectRoot: "/proj/X", PPID: 1, PPIDStarttime: fixedStarttime,
		Status: StatusActive,
	})
	// File B: project matches, triple dead (will sweep).
	tokenB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plantClaim(t, dir, tokenB, &Claim{
		ProjectRoot: "/proj/X", PPID: 2, PPIDStarttime: 999999, // distinct stale starttime
		Status: StatusActive,
	})
	// File C: project differs, triple dead (won't sweep — different project).
	tokenC := "cccccccccccccccccccccccccccccccc"
	plantClaim(t, dir, tokenC, &Claim{
		ProjectRoot: "/proj/Y", PPID: 3, PPIDStarttime: 88888,
		Status: StatusActive,
	})

	sweepStaleClaims("/proj/X", "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ" /* selfToken */)

	for _, want := range []string{tokenA, tokenC} {
		if _, err := os.Stat(filepath.Join(dir, want+".json")); err != nil {
			t.Errorf("expected file %s.json to remain: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, tokenB+".json")); !os.IsNotExist(err) {
		t.Errorf("expected file %s.json to be removed; stat err = %v", tokenB, err)
	}
}

func plantClaim(t *testing.T, dir, token string, c *Claim) {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, token+".json"), data, 0o600); err != nil {
		t.Fatalf("write %s.json: %v", token, err)
	}
}

func TestTokenStability_AcrossPidliveOverride(t *testing.T) {
	a := tokenFor("/p", 100, fixedStarttime)
	b := tokenFor("/p", 100, fixedStarttime)
	if a != b {
		t.Errorf("token unstable: %q vs %q", a, b)
	}
}

func TestTwoInstance_NonThrash(t *testing.T) {
	withCacheRoot(t)

	// Two distinct synthetic triples — model two Claude Code instances
	// on the same project on the same host.
	const stA int64 = 111111
	const stB int64 = 222222

	// Plant claim files directly via the lifecycle, switching Starttime
	// per call. Validate returns true iff starttime matches one of the
	// known-live values.
	origStart := pidlive.Starttime
	origValidate := pidlive.Validate
	t.Cleanup(func() {
		pidlive.Starttime = origStart
		pidlive.Validate = origValidate
	})
	pidlive.Validate = func(_ int, st int64) bool {
		return st == stA || st == stB
	}

	pinA := func() { pidlive.Starttime = func(int) (int64, error) { return stA, nil } }
	pinB := func() { pidlive.Starttime = func(int) (int64, error) { return stB, nil } }

	pinA()
	cA1, err := AcquireOrRefresh("/proj/Z")
	if err != nil {
		t.Fatalf("instance A first: %v", err)
	}
	pinB()
	cB1, err := AcquireOrRefresh("/proj/Z")
	if err != nil {
		t.Fatalf("instance B first: %v", err)
	}
	if cA1.DerivedSessionID == cB1.DerivedSessionID {
		t.Errorf("instances A and B got same DerivedSessionID: %q", cA1.DerivedSessionID)
	}

	// 100 alternating refreshes; each instance must see stable id.
	for i := 0; i < 50; i++ {
		pinA()
		cA, err := AcquireOrRefresh("/proj/Z")
		if err != nil {
			t.Fatalf("A iter %d: %v", i, err)
		}
		if cA.DerivedSessionID != cA1.DerivedSessionID {
			t.Errorf("A iter %d: DerivedSessionID drifted: %q -> %q", i, cA1.DerivedSessionID, cA.DerivedSessionID)
		}

		pinB()
		cB, err := AcquireOrRefresh("/proj/Z")
		if err != nil {
			t.Fatalf("B iter %d: %v", i, err)
		}
		if cB.DerivedSessionID != cB1.DerivedSessionID {
			t.Errorf("B iter %d: DerivedSessionID drifted: %q -> %q", i, cB1.DerivedSessionID, cB.DerivedSessionID)
		}
	}

	// Two distinct files on disk.
	dir, _ := cacheDir()
	tokA := tokenFor("/proj/Z", os.Getppid(), stA)
	tokB := tokenFor("/proj/Z", os.Getppid(), stB)
	if tokA == tokB {
		t.Fatal("token derivation collided for distinct starttimes")
	}
	for _, tok := range []string{tokA, tokB} {
		if _, err := os.Stat(filepath.Join(dir, tok+".json")); err != nil {
			t.Errorf("expected claim file for %s: %v", tok, err)
		}
	}
}

func TestConcurrentAcquire_SameToken(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	const N = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []string
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := AcquireOrRefresh("/proj/Race")
			if err != nil {
				t.Errorf("AcquireOrRefresh: %v", err)
				return
			}
			mu.Lock()
			results = append(results, c.DerivedSessionID)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(results) != N {
		t.Fatalf("got %d results, want %d", len(results), N)
	}
	first := results[0]
	for i, r := range results {
		if r != first {
			t.Errorf("goroutine %d got DerivedSessionID %q, want %q", i, r, first)
		}
	}

	// Persisted file is internally consistent.
	dir, _ := cacheDir()
	token := tokenFor("/proj/Race", os.Getppid(), fixedStarttime)
	data, err := os.ReadFile(filepath.Join(dir, token+".json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var c Claim
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if c.DerivedSessionID != first {
		t.Errorf("persisted DerivedSessionID %q != observed %q", c.DerivedSessionID, first)
	}
}

func TestLockReleased_BeforeReturn(t *testing.T) {
	withLiveTriple(t)
	withCacheRoot(t)

	if _, err := AcquireOrRefresh("/proj/Lock"); err != nil {
		t.Fatalf("AcquireOrRefresh: %v", err)
	}

	// After AcquireOrRefresh returns, we should be able to take the
	// same token lock from this goroutine WITHOUT contention.
	dir, _ := cacheDir()
	token := tokenFor("/proj/Lock", os.Getppid(), fixedStarttime)
	lockPath := filepath.Join(dir, token+".lock")

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Use AcquireOrRefresh again — it takes the same lock and
		// must succeed promptly because the prior call released.
		if _, err := AcquireOrRefresh("/proj/Lock"); err != nil {
			t.Errorf("re-acquire: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("re-acquire blocked > 2s; lock not released by prior call (%s)", lockPath)
	}
}

func TestParentName_RuntimeOSGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows pidlive.ParentName is documented to return ("", nil);
		// DetectHarness folds this to HarnessUnknown.
		got := DetectHarness(os.Getppid())
		if got != HarnessUnknown {
			t.Errorf("DetectHarness on Windows = %q, want %q", got, HarnessUnknown)
		}
	}
}

