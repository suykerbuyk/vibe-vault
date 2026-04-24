package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

func TestDetectProject(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/work/my-api", "my-api"},
		{"/home/user/personal/vibe-vault", "vibe-vault"},
		{"/home/user/obsidian/ObsMeetings", "ObsMeetings"},
		{"", "_unknown"},
		{"/", "_unknown"},
	}

	for _, tt := range tests {
		got := DetectProject(tt.cwd)
		if got != tt.want {
			t.Errorf("DetectProject(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestDetectDomain(t *testing.T) {
	cfg := config.Config{
		Domains: config.DomainsConfig{
			Work:       "/home/user/work",
			Personal:   "/home/user/personal",
			Opensource: "/home/user/opensource",
		},
	}

	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/work/my-api", "work"},
		{"/home/user/personal/vibe-vault", "personal"},
		{"/home/user/opensource/linux", "opensource"},
		{"/home/user/random/project", "personal"}, // default
		{"", "personal"},
	}

	for _, tt := range tests {
		got := detectDomain(tt.cwd, cfg)
		if got != tt.want {
			t.Errorf("detectDomain(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestTitleFromFirstMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"Implement the login page", "Implement the login page"},
		{"", "Session"},
		{"hi", "Session"},
		{"This is a very long message that exceeds the maximum length allowed for titles and should be truncated to fit within the display", "This is a very long message that exceeds the maximum length allowed for title..."},
		{"First line\nSecond line", "First line"},
	}

	for _, tt := range tests {
		got := TitleFromFirstMessage(tt.msg)
		if got != tt.want {
			t.Errorf("TitleFromFirstMessage(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

// mustGit runs a git command in the given directory, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		// SSH SCP-style
		{"git@github.com:user/repo.git", "repo"},
		{"git@github.com:user/repo", "repo"},
		{"git@gitlab.com:org/sub/repo.git", "repo"},

		// SSH with scheme
		{"ssh://git@github.com/user/repo.git", "repo"},
		{"ssh://git@github.com/user/repo", "repo"},

		// HTTPS
		{"https://github.com/user/repo.git", "repo"},
		{"https://github.com/user/repo", "repo"},
		{"https://gitlab.com/org/sub/repo.git", "repo"},

		// File protocol
		{"file:///path/to/repo.git", "repo"},

		// Bare path
		{"/path/to/repo.git", "repo"},
		{"/path/to/repo", "repo"},

		// Edge cases
		{"", ""},
		{"   ", ""},
		{"git@github.com:", ""},
		{"   git@github.com:user/repo.git   ", "repo"}, // whitespace trimmed
	}

	for _, tt := range tests {
		got := repoNameFromURL(tt.url)
		if got != tt.want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestDetectProject_GitRemote(t *testing.T) {
	dir := t.TempDir()
	// Name the subdirectory differently from the repo name to prove remote wins
	projectDir := filepath.Join(dir, "generic-dirname")
	if err := exec.Command("mkdir", "-p", projectDir).Run(); err != nil {
		t.Fatal(err)
	}

	mustGit(t, projectDir, "init")
	mustGit(t, projectDir, "remote", "add", "origin", "git@github.com:user/awesome-api.git")

	got := DetectProject(projectDir)
	if got != "awesome-api" {
		t.Errorf("detectProject with SSH remote = %q, want %q", got, "awesome-api")
	}
}

func TestDetectProject_GitHTTPS(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "some-dir")
	if err := exec.Command("mkdir", "-p", projectDir).Run(); err != nil {
		t.Fatal(err)
	}

	mustGit(t, projectDir, "init")
	mustGit(t, projectDir, "remote", "add", "origin", "https://github.com/user/cool-project.git")

	got := DetectProject(projectDir)
	if got != "cool-project" {
		t.Errorf("detectProject with HTTPS remote = %q, want %q", got, "cool-project")
	}
}

func TestDetectProject_GitNoRemote(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "local-only")
	if err := exec.Command("mkdir", "-p", projectDir).Run(); err != nil {
		t.Fatal(err)
	}

	mustGit(t, projectDir, "init")

	got := DetectProject(projectDir)
	if got != "local-only" {
		t.Errorf("detectProject with no remote = %q, want %q", got, "local-only")
	}
}

func TestDetectProject_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "plain-dir")
	if err := exec.Command("mkdir", "-p", projectDir).Run(); err != nil {
		t.Fatal(err)
	}

	got := DetectProject(projectDir)
	if got != "plain-dir" {
		t.Errorf("detectProject in non-git dir = %q, want %q", got, "plain-dir")
	}
}

func TestDetectProject_IdentityOverridesDir(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "generic-dirname")
	os.MkdirAll(projectDir, 0o755)
	os.WriteFile(filepath.Join(projectDir, ".vibe-vault.toml"), []byte(`
[project]
name = "my-real-project"
`), 0o644)

	got := DetectProject(projectDir)
	if got != "my-real-project" {
		t.Errorf("DetectProject with identity = %q, want %q", got, "my-real-project")
	}
}

func TestDetect_IdentityDomainOverride(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproj")
	os.MkdirAll(projectDir, 0o755)
	os.WriteFile(filepath.Join(projectDir, ".vibe-vault.toml"), []byte(`
[project]
name = "myproj"
domain = "developer-tools"
`), 0o644)

	cfg := config.Config{
		Domains: config.DomainsConfig{
			Work:       "/home/user/work",
			Personal:   "/home/user/personal",
			Opensource: "/home/user/opensource",
		},
	}

	info := Detect(projectDir, "main", "claude", "s1", cfg)
	if info.Project != "myproj" {
		t.Errorf("Project = %q, want myproj", info.Project)
	}
	if info.Domain != "developer-tools" {
		t.Errorf("Domain = %q, want developer-tools", info.Domain)
	}
}

func TestDetect_MissingIdentityFallsThrough(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "fallback-dir")
	os.MkdirAll(projectDir, 0o755)
	// No .vibe-vault.toml

	got := DetectProject(projectDir)
	if got != "fallback-dir" {
		t.Errorf("DetectProject = %q, want fallback-dir", got)
	}
}

// TestDetectProject_OriginProjectContract pins the behaviors Phase 6.1's
// write-time provenance stamper relies on for origin_project emission.
// Breaking any of these cases would corrupt cross-project forensic
// signal in session notes and iteration trailers.
func TestDetectProject_OriginProjectContract(t *testing.T) {
	// Case 1: non-existent path — basename fallback still produces a
	// usable project name. The integration test sentinel
	// /vibe-vault-test-cwd rides this branch.
	if got := DetectProject("/vibe-vault-test-cwd"); got != "vibe-vault-test-cwd" {
		t.Errorf("DetectProject(%q) = %q, want %q",
			"/vibe-vault-test-cwd", got, "vibe-vault-test-cwd")
	}

	// Case 2: empty input returns the "_unknown" sentinel. The caller
	// (Phase 6.1) is expected to guard against this — either by
	// checking the cwd stamp for empty first, or by normalizing
	// "_unknown" to "" before setting origin_project. This test pins
	// the sentinel value so the caller's guard stays correct.
	if got := DetectProject(""); got != "_unknown" {
		t.Errorf("DetectProject(%q) = %q, want %q", "", got, "_unknown")
	}

	// Case 3: a real non-project path (e.g., /tmp) resolves to the
	// directory basename, not empty. Phase 6.1 will emit this as
	// origin_project: tmp. That's acceptable per D2 — the stamp
	// records what cwd resolved to, not a curated project list.
	if got := DetectProject("/tmp"); got != "tmp" {
		t.Errorf("DetectProject(%q) = %q, want %q", "/tmp", got, "tmp")
	}

	// Case 4: stability under double-invocation. The write-time
	// stamper may be called once per session note and many times per
	// iteration block across a long-running MCP server. Verify
	// idempotence for a path where git subprocess is NOT invoked
	// (non-git temp dir).
	tmp := t.TempDir()
	first := DetectProject(tmp)
	second := DetectProject(tmp)
	if first != second {
		t.Errorf("DetectProject not idempotent: first=%q second=%q",
			first, second)
	}
}
