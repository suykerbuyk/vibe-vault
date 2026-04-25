// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/identity"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// DetectProject builds a session.Info from a Zed thread's metadata.
// Does NOT shell out to git — builds Info directly from thread data.
//
// Fallback chain:
//  1. ProjectSnapshot.WorktreePath basename (future-proofed; currently always empty in Zed)
//  2. Common root of absolute paths from file mentions + tool-use inputs
//  3. "zed" (final fallback — at least conveys the source)
func DetectProject(thread *Thread, cfg config.Config) session.Info {
	info := session.Info{
		SessionID: "zed:" + thread.ID,
		Model:     modelString(thread.Model),
	}

	// Extract worktree path and project name from snapshot
	if thread.ProjectSnapshot != nil && len(thread.ProjectSnapshot.WorktreeSnapshots) > 0 {
		wt := thread.ProjectSnapshot.WorktreeSnapshots[0]
		info.CWD = wt.WorktreePath

		// Project name from worktree path basename
		if wt.WorktreePath != "" {
			info.Project = filepath.Base(wt.WorktreePath)
		}

		// Branch from snapshot
		if wt.GitBranch != "" {
			info.Branch = wt.GitBranch
		}
	}

	// Fallback branch from DB field (currently always NULL, but future-proofed)
	if info.Branch == "" && thread.WorktreeBranch != "" {
		info.Branch = thread.WorktreeBranch
	}

	// Fallback: infer project from file mentions and tool-use paths
	if info.Project == "" {
		paths := collectAbsolutePaths(thread)
		if project, dir := commonProjectRoot(paths); project != "" {
			info.Project = project
			if info.CWD == "" {
				info.CWD = dir
			}
		}
	}

	// Try identity file on resolved CWD
	if info.CWD != "" {
		if id, _ := identity.Load(info.CWD); id != nil {
			if info.Project == "" {
				info.Project = id.Project.Name
			}
			// Domain from identity set below (after detectDomain)
		}
	}

	// Final fallback
	if info.Project == "" {
		info.Project = "zed"
	}

	// Domain detection via config paths
	info.Domain = detectDomain(info.CWD, cfg)

	// Identity domain overrides config-based domain detection
	if info.CWD != "" {
		if id, _ := identity.Load(info.CWD); id != nil && id.Project.Domain != "" {
			info.Domain = id.Project.Domain
		}
	}

	return info
}

// collectAbsolutePaths gathers absolute filesystem paths from a thread's
// file mentions (user messages) and tool-use inputs (agent messages).
func collectAbsolutePaths(thread *Thread) []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		p = filepath.Clean(p)
		if !filepath.IsAbs(p) || isSystemPath(p) {
			return
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	for _, msg := range thread.Messages {
		switch msg.Role {
		case "user":
			// File mentions have structured absolute paths
			for _, c := range msg.Content {
				if c.Type == "mention" && c.MentionURI != nil && c.MentionURI.Type == "file" {
					add(c.MentionURI.AbsPath)
				}
			}
		case "assistant":
			// Tool-use inputs: extract file_path/path from Read, Edit, Write, Grep, Glob
			for _, c := range msg.Content {
				if c.Type != "tool_use" {
					continue
				}
				canonical := NormalizeTool(c.ToolName)
				switch canonical {
				case "Read", "Edit", "Write", "Grep", "Glob":
					if p := inputStr(c.Input, "file_path"); p != "" {
						add(p)
					}
					if p := inputStr(c.Input, "path"); p != "" {
						add(p)
					}
				}
			}
		}
	}

	return paths
}

// isSystemPath returns true for paths that should be excluded from project inference.
func isSystemPath(p string) bool {
	for _, prefix := range []string{"/tmp", "/etc", "/dev", "/proc", "/sys", "/var"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// commonProjectRoot finds the deepest common ancestor directory of the given
// absolute paths, returning (basename, fullpath). Returns ("", "") if:
//   - fewer than 2 paths (single-path heuristic is unreliable)
//   - the common root is too shallow (less than 2 segments below $HOME)
//   - paths are not absolute
func commonProjectRoot(paths []string) (project, dir string) {
	if len(paths) < 2 {
		return "", ""
	}

	// Split first path into segments
	parts := strings.Split(filepath.Clean(paths[0]), string(filepath.Separator))

	// Find longest common prefix across all paths
	for _, p := range paths[1:] {
		pp := strings.Split(filepath.Clean(p), string(filepath.Separator))
		n := len(parts)
		if len(pp) < n {
			n = len(pp)
		}
		match := 0
		for i := 0; i < n; i++ {
			if parts[i] != pp[i] {
				break
			}
			match = i + 1
		}
		parts = parts[:match]
	}

	if len(parts) == 0 {
		return "", ""
	}

	root := string(filepath.Separator) + filepath.Join(parts[1:]...) // rejoin with leading /

	// Depth gate: must be at least 2 segments below $HOME.
	// For /home/user/code/project, that's segments: home, user, code, project
	// We need at least homeIdx+3 segments (home dir + 2 more).
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = filepath.Join(string(filepath.Separator), "home", os.Getenv("USER"))
	}
	homeParts := strings.Split(filepath.Clean(homeDir), string(filepath.Separator))
	minDepth := len(homeParts) + 2 // e.g., /home/user = 3 parts, need 5 = /home/user/code/project

	if len(parts) < minDepth {
		return "", ""
	}

	return filepath.Base(root), root
}

// detectDomain maps a CWD to a domain using config path prefixes.
func detectDomain(cwd string, cfg config.Config) string {
	if cwd == "" {
		return "personal"
	}
	cwd = filepath.Clean(cwd)

	domainMap := map[string]string{
		filepath.Clean(cfg.Domains.Work):       "work",
		filepath.Clean(cfg.Domains.Personal):   "personal",
		filepath.Clean(cfg.Domains.Opensource): "opensource",
	}

	for prefix, domain := range domainMap {
		if prefix != "." && (cwd == prefix || len(cwd) > len(prefix) && cwd[:len(prefix)+1] == prefix+"/") {
			return domain
		}
	}

	return "personal"
}
