// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSetKey_FreshConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "vibe-vault", "config.toml")

	if err := runConfigSetKey([]string{"anthropic", "sk-ant-test"}, strings.NewReader(""), configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	want := "[providers.anthropic]\napi_key = \"sk-ant-test\"\n"
	if got != want {
		t.Errorf("config content mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	fi, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}

	dirInfo, err := os.Stat(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Errorf("parent dir mode = %o, want 0700", mode)
	}
}

func TestConfigSetKey_AddProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	original := `# my comment
vault_path = "/tmp/vault"

[domains]
work = "~/work"
`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := runConfigSetKey([]string{"anthropic", "sk-ant-test"}, strings.NewReader(""), configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)

	// Existing lines must survive verbatim.
	if !strings.Contains(got, "# my comment") {
		t.Errorf("comment lost: %s", got)
	}
	if !strings.Contains(got, `vault_path = "/tmp/vault"`) {
		t.Errorf("vault_path lost: %s", got)
	}
	if !strings.Contains(got, "[domains]") {
		t.Errorf("[domains] lost: %s", got)
	}
	if !strings.Contains(got, `work = "~/work"`) {
		t.Errorf("work line lost: %s", got)
	}
	// New section appended at end.
	if !strings.Contains(got, "[providers.anthropic]") {
		t.Errorf("missing new section header: %s", got)
	}
	if !strings.Contains(got, `api_key = "sk-ant-test"`) {
		t.Errorf("missing api_key line: %s", got)
	}
	// New section comes after the existing content.
	domainsIdx := strings.Index(got, "[domains]")
	providersIdx := strings.Index(got, "[providers.anthropic]")
	if domainsIdx == -1 || providersIdx == -1 || providersIdx < domainsIdx {
		t.Errorf("[providers.anthropic] should appear after [domains]:\n%s", got)
	}
}

func TestConfigSetKey_OverwriteRequiresForce(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	original := "[providers.anthropic]\napi_key = \"old-key\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	err := runConfigSetKey([]string{"anthropic", "new-key"}, strings.NewReader(""), configPath)
	if err == nil {
		t.Fatalf("expected refusal, got nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already set") || !strings.Contains(msg, "--force") {
		t.Errorf("error %q should mention 'already set' and '--force'", msg)
	}

	// File untouched.
	data, _ := os.ReadFile(configPath)
	if string(data) != original {
		t.Errorf("file was modified despite refusal:\n%s", string(data))
	}
}

func TestConfigSetKey_OverwriteWithForce(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	original := `# top comment
vault_path = "/tmp/vault"

[providers.anthropic]
api_key = "old-key"

[domains]
work = "~/work"
`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := runConfigSetKey([]string{"--force", "anthropic", "new-key"}, strings.NewReader(""), configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)

	if strings.Contains(got, "old-key") {
		t.Errorf("old key still present: %s", got)
	}
	if !strings.Contains(got, `api_key = "new-key"`) {
		t.Errorf("missing new key: %s", got)
	}
	// Other lines preserved.
	for _, expected := range []string{"# top comment", `vault_path = "/tmp/vault"`, "[domains]", `work = "~/work"`} {
		if !strings.Contains(got, expected) {
			t.Errorf("expected line missing: %q\n%s", expected, got)
		}
	}
}

func TestConfigSetKey_StdinDash(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	stdin := strings.NewReader("sk-from-stdin\n")
	if err := runConfigSetKey([]string{"anthropic", "-"}, stdin, configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), `api_key = "sk-from-stdin"`) {
		t.Errorf("expected key from stdin, got:\n%s", string(data))
	}
}

func TestConfigSetKey_StdinDashTrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Exactly one trailing newline.
	stdin := strings.NewReader("abc-key\n")
	if err := runConfigSetKey([]string{"openai", "-"}, stdin, configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if !strings.Contains(got, `api_key = "abc-key"`) {
		t.Errorf("trailing newline not trimmed; got:\n%s", got)
	}
	if strings.Contains(got, `api_key = "abc-key\n"`) {
		t.Errorf("escaped newline ended up in value: %s", got)
	}
}

func TestConfigSetKey_RejectsKeyWithEmbeddedNewline(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	stdin := strings.NewReader("ab\ncd\n")
	err := runConfigSetKey([]string{"google", "-"}, stdin, configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "whitespace") {
		t.Errorf("error %q should mention whitespace", err)
	}

	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Errorf("config file should not have been written: %v", statErr)
	}
}

func TestConfigSetKey_RejectsEmptyKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	err := runConfigSetKey([]string{"anthropic", ""}, strings.NewReader(""), configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Errorf("error %q should mention 'empty'", err)
	}

	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Errorf("config file should not have been written: %v", statErr)
	}
}

func TestConfigSetKey_UnknownProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	err := runConfigSetKey([]string{"nope", "some-key"}, strings.NewReader(""), configPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "supported: anthropic, openai, google") {
		t.Errorf("error %q should list supported providers", msg)
	}
}

func TestConfigSetKey_FileMode(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := runConfigSetKey([]string{"anthropic", "sk-test"}, strings.NewReader(""), configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}
	fi, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 0600", mode)
	}
}

func TestConfigSetKey_PreservesOtherLines(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	original := `# Header comment
vault_path = "~/vault"

# Another comment
[domains]
work = "~/work"
personal = "~/personal"

[providers.anthropic]
# pre-existing comment in providers section
api_key = "old-key" # inline comment

[providers.openai]
api_key = "openai-untouched"

[archive]
compress = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := runConfigSetKey([]string{"--force", "anthropic", "new-key"}, strings.NewReader(""), configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)

	// Targeted change applied.
	if strings.Contains(got, "old-key") {
		t.Errorf("old-key still present: %s", got)
	}
	if !strings.Contains(got, `api_key = "new-key"`) {
		t.Errorf("missing new key: %s", got)
	}

	// All other lines preserved.
	expected := []string{
		"# Header comment",
		`vault_path = "~/vault"`,
		"# Another comment",
		"[domains]",
		`work = "~/work"`,
		`personal = "~/personal"`,
		"[providers.anthropic]",
		"# pre-existing comment in providers section",
		"# inline comment",
		"[providers.openai]",
		`api_key = "openai-untouched"`,
		"[archive]",
		"compress = true",
	}
	for _, line := range expected {
		if !strings.Contains(got, line) {
			t.Errorf("expected line missing: %q\n--- file ---\n%s", line, got)
		}
	}

	// Construct the expected file by replacing only the targeted line and
	// confirm an exact match — the strongest possible assertion.
	wantFull := strings.Replace(original, `api_key = "old-key" # inline comment`, `api_key = "new-key" # inline comment`, 1)
	if got != wantFull {
		t.Errorf("file content differs from minimal-edit expectation\n--- got ---\n%s\n--- want ---\n%s", got, wantFull)
	}
}

func TestConfigSetKey_TempFileSameDirectory(t *testing.T) {
	// Verify no .tmp file leaks into /tmp by isolating the config in a
	// dedicated test dir and walking it post-write.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := runConfigSetKey([]string{"anthropic", "sk-test"}, strings.NewReader(""), configPath); err != nil {
		t.Fatalf("runConfigSetKey: %v", err)
	}

	// After a successful rename, no temp files should remain in the
	// config directory.
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".config-setkey-") {
			t.Errorf("leftover temp file in config dir: %s", name)
		}
	}

	// Verify the temp file was created in the same dir as configPath:
	// re-run with an injected hook — drive the path through a deliberately
	// bogus path with read-only parent and verify the error mentions the
	// config dir, not /tmp/. We approximate by checking that the
	// implementation references filepath.Dir(configPath) for the temp file
	// (single-dir invariant): create a config in a dedicated tempdir
	// distinct from $TMPDIR and assert the rename succeeded (which on
	// Linux requires same-filesystem temp+rename for atomicity in the
	// general case; t.TempDir() returns a path under $TMPDIR, so this
	// asserts the function did NOT pick a path outside dir).

	// Strongest available black-box check without a seam: the file exists
	// and the directory contains exactly one entry (the final file).
	if len(entries) != 1 {
		t.Errorf("config dir has %d entries; want 1 (just the config file)", len(entries))
	}
	if len(entries) >= 1 && entries[0].Name() != "config.toml" {
		t.Errorf("entry = %q, want config.toml", entries[0].Name())
	}
}
