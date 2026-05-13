// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// pkgRefRe matches Go-convention package paths of the form
// `internal/<pkg>` or `cmd/<pkg>` where <pkg> is a single
// lowercase identifier. Top-level only — nested sub-packages
// like `internal/foo/bar` are validated by their top-level
// component (`internal/foo`).
var pkgRefRe = regexp.MustCompile(`\b(internal|cmd)/([a-z][a-z0-9_]+)\b`)

const (
	historyTailStartMarker = "<!-- vv:project-history-tail:start -->"
	historyTailEndMarker   = "<!-- vv:project-history-tail:end -->"
)

// CheckStalePackageRefs validates that Go-convention package-path
// references (`internal/<pkg>`, `cmd/<pkg>`) in the project's
// resume.md point to packages that exist in the source tree at
// repoPath.
//
// Scoped to resume.md (excluding the project-history-tail marker
// block, which is append-only historical narrative). Task files
// are intentionally not scanned — they are forward-looking plans
// (mentioning future packages as deliverables) and motivation
// prose (citing retired packages as past context), and loading
// them on `/restart` is not part of bootstrap.
//
// Returns nil for non-Go projects (no internal/ or cmd/ at
// repoPath) and for projects without an agentctx directory.
func CheckStalePackageRefs(vaultPath, project, repoPath string) *Result {
	if vaultPath == "" || project == "" || project == "_unknown" || repoPath == "" {
		return nil
	}

	hasInternal := dirExists(filepath.Join(repoPath, "internal"))
	hasCmd := dirExists(filepath.Join(repoPath, "cmd"))
	if !hasInternal && !hasCmd {
		return nil
	}

	agentctxDir := filepath.Join(vaultPath, "Projects", project, "agentctx")
	if _, err := os.Stat(agentctxDir); os.IsNotExist(err) {
		return nil
	}

	content, ok := readResumeActive(filepath.Join(agentctxDir, "resume.md"))
	if !ok {
		return nil
	}
	stale, refsChecked := scanForStale("resume.md", content, repoPath)

	if len(stale) == 0 {
		return &Result{
			Name:   "stale-package-refs",
			Status: Pass,
			Detail: fmt.Sprintf("%s: %d package refs validated", project, refsChecked),
		}
	}

	return &Result{
		Name:   "stale-package-refs",
		Status: Warn,
		Detail: fmt.Sprintf("%s: %d stale ref(s) — %s", project, len(stale), summarizeStale(stale, 3)),
	}
}

// readResumeActive reads resume.md and returns its content with
// the project-history-tail marker block stripped out. The history
// block is append-only historical narrative; package names there
// reflect the architecture at the time of writing and should not
// be validated against the current source tree.
//
// Returns ("", false) when the file cannot be read.
func readResumeActive(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := string(data)
	start := strings.Index(s, historyTailStartMarker)
	if start == -1 {
		return s, true
	}
	end := strings.Index(s, historyTailEndMarker)
	if end == -1 || end < start {
		return s, true
	}
	return s[:start] + s[end+len(historyTailEndMarker):], true
}

// scanForStale finds all unique package-path references in content
// and returns those whose target directory doesn't exist under
// repoPath, prefixed with the source label. Also returns the total
// number of unique references checked (for the pass-case detail).
//
// Matches preceded by `/` are skipped — these are sub-paths inside
// longer module paths (e.g., `github.com/golangci/golangci-lint/cmd/golangci-lint`
// matches `cmd/golangci`, which is a third-party module path, not
// this project's binary).
func scanForStale(label, content, repoPath string) ([]string, int) {
	seen := map[string]bool{}
	var stale []string
	checked := 0
	for _, idx := range pkgRefRe.FindAllStringSubmatchIndex(content, -1) {
		matchStart := idx[0]
		if matchStart > 0 && content[matchStart-1] == '/' {
			continue
		}
		prefix := content[idx[2]:idx[3]]
		pkg := content[idx[4]:idx[5]]
		pkgPath := prefix + "/" + pkg
		if seen[pkgPath] {
			continue
		}
		seen[pkgPath] = true
		checked++
		if !dirExists(filepath.Join(repoPath, pkgPath)) {
			stale = append(stale, label+":"+pkgPath)
		}
	}
	return stale, checked
}

// summarizeStale joins the first n stale entries with ", ", appending
// "(+K more)" when more exist. Deterministic ordering is the caller's
// responsibility.
func summarizeStale(stale []string, n int) string {
	if len(stale) <= n {
		return strings.Join(stale, ", ")
	}
	return strings.Join(stale[:n], ", ") + fmt.Sprintf(" (+%d more)", len(stale)-n)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
