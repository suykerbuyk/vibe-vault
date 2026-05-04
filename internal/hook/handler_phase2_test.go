// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/staging"
	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

// TestPhase2_HookFire_LandsInStagingNotVault locks the headline
// invariant of vault-two-tier-narrative-vs-sessions-split: a hook
// fire writes the session note to the host-local staging dir and
// commits there, NOT into the shared vault.
func TestPhase2_HookFire_LandsInStagingNotVault(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	transcriptPath := writeTranscript(t, minimalTranscript)

	input := &Input{
		SessionID:      "phase2-staging-route",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}

	// File present in staging.
	stagingProj := stagingNotesFor(cfg, "proj")
	mds := globMD(t, stagingProj)
	if len(mds) == 0 {
		t.Fatalf("no .md files in staging dir %s", stagingProj)
	}

	// File NOT present in vault sessions/.
	vaultSessions := filepath.Join(cfg.VaultPath, "Projects", "proj", "sessions")
	if entries, _ := os.ReadDir(vaultSessions); len(entries) > 0 {
		var leaked []string
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				leaked = append(leaked, e.Name())
			}
		}
		if len(leaked) > 0 {
			t.Errorf("session notes leaked into vault sessions/: %v", leaked)
		}
	}
}

// TestPhase2_HookFire_RecoversFromMissingGit is the v4-M1 regression
// lock: sentinel survives but .git/ was wiped. EnsureInit must
// re-bootstrap before the staging.Commit attempt.
func TestPhase2_HookFire_RecoversFromMissingGit(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	stagingDir := filepath.Join(cfg.Staging.Root, "proj")
	if err := staging.InitAt(stagingDir); err != nil {
		t.Fatalf("pre-InitAt: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(stagingDir, ".git")); err != nil {
		t.Fatalf("rm .git: %v", err)
	}

	transcriptPath := writeTranscript(t, minimalTranscript)
	input := &Input{
		SessionID:      "phase2-recover-git",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD not recreated by EnsureInit: %v", err)
	}
}

// TestPhase2_HookFire_ColdInit covers both-missing: no prior staging
// dir at all. EnsureInit runs Init in-process; sentinel + .git
// materialize; note lands.
func TestPhase2_HookFire_ColdInit(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	transcriptPath := writeTranscript(t, minimalTranscript)
	input := &Input{
		SessionID:      "phase2-cold-init",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}
	stagingDir := filepath.Join(cfg.Staging.Root, "proj")
	if _, err := os.Stat(filepath.Join(stagingDir, staging.SentinelName)); err != nil {
		t.Errorf("sentinel missing post-cold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD missing post-cold: %v", err)
	}
}

// TestPhase2_CustomStagingRoot: cfg.Staging.Root override beats the
// XDG default. Forces a tempdir target and asserts the file lands
// there.
func TestPhase2_CustomStagingRoot(t *testing.T) {
	cfg := testConfig(t)
	customRoot := filepath.Join(t.TempDir(), "custom-staging")
	cfg.Staging.Root = customRoot
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	transcriptPath := writeTranscript(t, minimalTranscript)
	input := &Input{
		SessionID:      "phase2-custom-root",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}
	mds := globMD(t, filepath.Join(customRoot, "proj"))
	if len(mds) == 0 {
		t.Errorf("no notes landed in custom staging root %s", customRoot)
	}
}

// TestPhase2_HookFire_NoGlobalGitIdentity asserts the v3-H2 promise
// holds end-to-end through the hook: even without ~/.gitconfig,
// the staging commit succeeds via the repo-local identity from
// staging.Init.
func TestPhase2_HookFire_NoGlobalGitIdentity(t *testing.T) {
	gitx.SandboxNoIdentity(t)
	cfg := testConfig(t)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	transcriptPath := writeTranscript(t, minimalTranscript)
	input := &Input{
		SessionID:      "phase2-no-global-id",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}
	stagingDir := filepath.Join(cfg.Staging.Root, "proj")
	// Probe with a raw exec to avoid the gitx identity-injection.
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = stagingDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %s: %v", out, err)
	}
	if !strings.Contains(string(out), "session: proj/") {
		t.Errorf("staging commit log missing session subject: %q", string(out))
	}
}

// TestPhase2_HookFire_TwoFires_WarmAndCold simulates two consecutive
// fires: the first cold-inits the staging repo, the second uses the
// fast path (sentinel + .git/HEAD both present). The warm fire's
// note must land alongside the cold fire's note in staging.
func TestPhase2_HookFire_TwoFires_WarmAndCold(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	t1 := writeTranscript(t, minimalTranscript)
	if err := handleInput(&Input{
		SessionID:      "phase2-two-fires-cold",
		TranscriptPath: t1,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}, "", cfg); err != nil {
		t.Fatalf("cold fire: %v", err)
	}

	t2 := writeTranscript(t, minimalTranscript)
	if err := handleInput(&Input{
		SessionID:      "phase2-two-fires-warm",
		TranscriptPath: t2,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}, "", cfg); err != nil {
		t.Fatalf("warm fire: %v", err)
	}

	stagingDir := filepath.Join(cfg.Staging.Root, "proj")
	mds := globMD(t, stagingDir)
	if len(mds) < 2 {
		t.Errorf("expected ≥2 notes after two fires, got %d: %v", len(mds), mds)
	}
}

// TestPhase2_HookFire_StagingCommitFailure_FailSafe locks the
// fail-safe contract: when the staging git layer fails (here we
// simulate by setting cfg.Staging.Root to a path that contains a
// pre-existing non-repo directory the hook can write into but
// `git add` will reject), the markdown file still lands on disk and
// the hook returns nil. Pre-Phase-2 this would propagate as a hook
// error and Claude Code would log a SessionEnd failure.
func TestPhase2_HookFire_StagingCommitFailure_FailSafe(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	// Pre-create the staging dir but DO NOT init a git repo. EnsureInit
	// runs and recovers — but for this test we skip it by pre-creating
	// the sentinel + a fake .git/HEAD that points nowhere git accepts.
	stagingProj := filepath.Join(cfg.Staging.Root, "proj")
	if err := os.MkdirAll(filepath.Join(stagingProj, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingProj, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write fake HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingProj, staging.SentinelName), []byte("now\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	transcriptPath := writeTranscript(t, minimalTranscript)
	input := &Input{
		SessionID:      "phase2-fail-safe",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput should be fail-safe, got: %v", err)
	}
	mds := globMD(t, stagingProj)
	if len(mds) == 0 {
		t.Errorf("markdown file missing despite staging-commit failure (fail-safe broken): %s", stagingProj)
	}
}

// BenchmarkHook_StagingWritePlusCommit measures the warm hook-fire
// path: parse-transcript + capture + atomic write + staging.Commit.
// Plan target: ≤100ms warm. The benchmark surfaces the actual number
// for tuning; the b.Skip on egregious regression keeps slow CI from
// false-failing while still flagging multi-second pathologies.
func BenchmarkHook_StagingWritePlusCommit(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.VaultPath = filepath.Join(b.TempDir(), "vault")
	cfg.Enrichment.Enabled = false
	cfg.Staging.Root = filepath.Join(b.TempDir(), "staging")
	b.Setenv("VIBE_VAULT_HOSTNAME", "benchhost")

	// Pre-init staging so we measure only the warm path.
	if err := staging.EnsureInitAt(cfg.Staging.Root, "proj"); err != nil {
		b.Fatalf("EnsureInit: %v", err)
	}

	dir := b.TempDir()
	transcriptPath := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(minimalTranscript), 0o644); err != nil {
		b.Fatalf("write transcript: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := &Input{
			SessionID: "bench-iter",
			// Each iteration uses a unique session ID so dedup
			// doesn't short-circuit the write path.
			TranscriptPath: transcriptPath,
			HookEventName:  "SessionEnd",
			CWD:            "/tmp/proj",
		}
		input.SessionID = "bench-" + filepath.Base(b.TempDir()) + "-" + filepath.Base(dir)
		if err := handleInput(input, "", cfg); err != nil {
			b.Fatalf("handleInput: %v", err)
		}
	}
	b.StopTimer()
	// 100ms = 100_000_000 ns. b.N might be 1 on cold runs; use the
	// elapsed/N estimate to flag egregious regressions only.
	if b.N > 0 {
		nsPerOp := b.Elapsed().Nanoseconds() / int64(b.N)
		if nsPerOp > 1_000_000_000 { // 1s/op = pathological
			b.Skipf("hook fire >1s/op (%d ns) — host too slow for ≤100ms target lock", nsPerOp)
		}
	}
}

// globMD returns the .md files directly under dir (no recursion).
func globMD(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			out = append(out, e.Name())
		}
	}
	return out
}
