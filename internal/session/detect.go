package session

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/config"
)

// Info holds detected session metadata.
type Info struct {
	Project   string // e.g., "proteus-rs", "ObsMeetings"
	Domain    string // "work", "personal", "opensource"
	Branch    string
	Model     string
	SessionID string
	CWD       string
}

// Detect determines project name, domain, and branch from the working directory and transcript metadata.
func Detect(cwd, gitBranch, model, sessionID string, cfg config.Config) Info {
	info := Info{
		Branch:    gitBranch,
		Model:     model,
		SessionID: sessionID,
		CWD:       cwd,
	}

	info.Project = detectProject(cwd)
	info.Domain = detectDomain(cwd, cfg)

	return info
}

// detectProject extracts the project name from the working directory.
// Prefers the git remote origin name (stable across worktrees and renames),
// falling back to the directory basename.
func detectProject(cwd string) string {
	if cwd == "" {
		return "_unknown"
	}
	cwd = filepath.Clean(cwd)

	// Try git remote first (stable across worktrees and renames)
	if name := gitRemoteProject(cwd); name != "" {
		return name
	}

	// Fall back to directory basename
	name := filepath.Base(cwd)
	if name == "" || name == "." || name == "/" {
		return "_unknown"
	}
	return name
}

// gitRemoteProject runs `git remote get-url origin` and extracts the repo name.
// Returns "" on any failure (not a git repo, no remote, timeout, parse error).
func gitRemoteProject(cwd string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return repoNameFromURL(strings.TrimSpace(string(out)))
}

// repoNameFromURL extracts the repository name from a git remote URL.
// Handles SSH (SCP-style), HTTPS, file://, and bare path formats.
// Returns "" on any parse failure.
func repoNameFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	var path string

	// SCP-style: git@host:path (no :// but has :)
	if !strings.Contains(rawURL, "://") && strings.Contains(rawURL, ":") {
		idx := strings.Index(rawURL, ":")
		path = rawURL[idx+1:]
	} else {
		// URL with scheme (https://, ssh://, file://, etc.) or bare path
		u, err := url.Parse(rawURL)
		if err != nil {
			return ""
		}
		path = u.Path
	}

	if path == "" {
		return ""
	}

	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".git")
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

// detectDomain maps the working directory to a domain based on config paths.
func detectDomain(cwd string, cfg config.Config) string {
	if cwd == "" {
		return "personal"
	}

	cwd = filepath.Clean(cwd)

	domainMap := map[string]string{
		filepath.Clean(cfg.Domains.Work):       "work",
		filepath.Clean(cfg.Domains.Personal):   "personal",
		filepath.Clean(cfg.Domains.Opensource):  "opensource",
	}

	for prefix, domain := range domainMap {
		if strings.HasPrefix(cwd, prefix+"/") || cwd == prefix {
			return domain
		}
	}

	return "personal"
}

// TitleFromFirstMessage generates a session title from the first user message.
// Falls back to a generic title if the message is too short or looks trivial.
func TitleFromFirstMessage(firstMsg string) string {
	if firstMsg == "" {
		return "Session"
	}

	// Trim and take first line
	firstMsg = strings.TrimSpace(firstMsg)
	if idx := strings.IndexByte(firstMsg, '\n'); idx > 0 {
		firstMsg = firstMsg[:idx]
	}

	// Skip trivial messages
	lower := strings.ToLower(firstMsg)
	trivials := []string{"hi", "hello", "hey", "ok", "okay", "yes", "no", "thanks", "thank you", "y", "n"}
	for _, t := range trivials {
		if lower == t {
			return "Session"
		}
	}

	// Truncate if too long
	if len(firstMsg) > 80 {
		firstMsg = firstMsg[:77] + "..."
	}

	return firstMsg
}

// SummaryFromContent generates a one-line summary.
// Phase 1: just uses the first user message.
// Phase 2: will use LLM enrichment.
func SummaryFromContent(firstMsg string) string {
	title := TitleFromFirstMessage(firstMsg)
	if title == "Session" {
		return "Claude Code session"
	}
	return title
}
