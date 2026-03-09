// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"path/filepath"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/session"
)

// DetectProject builds a session.Info from a Zed thread's metadata.
// Does NOT shell out to git — builds Info directly from thread data.
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

	// Fallback project name
	if info.Project == "" {
		info.Project = "_unknown"
	}

	// Domain detection via config paths
	info.Domain = detectDomain(info.CWD, cfg)

	return info
}

// detectDomain maps a CWD to a domain using config path prefixes.
func detectDomain(cwd string, cfg config.Config) string {
	if cwd == "" {
		return "personal"
	}
	cwd = filepath.Clean(cwd)

	domainMap := map[string]string{
		filepath.Clean(cfg.Domains.Work):      "work",
		filepath.Clean(cfg.Domains.Personal):  "personal",
		filepath.Clean(cfg.Domains.Opensource): "opensource",
	}

	for prefix, domain := range domainMap {
		if prefix != "." && (cwd == prefix || len(cwd) > len(prefix) && cwd[:len(prefix)+1] == prefix+"/") {
			return domain
		}
	}

	return "personal"
}
