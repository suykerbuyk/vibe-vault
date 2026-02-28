package session

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/johns/vibe-vault/internal/config"
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
