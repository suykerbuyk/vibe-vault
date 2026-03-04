// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"log"
	"os"
	"path/filepath"
)

// GenerateResult holds metrics from a GenerateContext call.
type GenerateResult struct {
	ProjectsUpdated  int
	KnowledgeWritten bool
}

// GenerateContext writes per-project history.md and cross-project _knowledge.md
// files. It uses the already-loaded index (no rebuild) so it's fast enough for
// post-hook use. Callers provide knowledge summaries (may be nil).
// alertThreshold is the friction alert threshold (0 = disabled).
func GenerateContext(idx *Index, vaultPath string, summaries []KnowledgeSummary, alertThreshold int) (*GenerateResult, error) {
	result := &GenerateResult{}

	// Generate per-project context documents
	projectsDir := filepath.Join(vaultPath, "Projects")
	for _, project := range idx.Projects() {
		doc := idx.ProjectContext(project, summaries, alertThreshold)
		dir := filepath.Join(projectsDir, project)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("warning: create dir for %s: %v", project, err)
			continue
		}
		path := filepath.Join(dir, "history.md")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			log.Printf("warning: write context for %s: %v", project, err)
			continue
		}
		result.ProjectsUpdated++
	}

	// Generate cross-project knowledge document
	if len(summaries) > 0 {
		crossDoc := CrossProjectKnowledge(summaries)
		if crossDoc != "" {
			crossPath := filepath.Join(vaultPath, "Knowledge", "_knowledge.md")
			if err := os.MkdirAll(filepath.Dir(crossPath), 0o755); err != nil {
				log.Printf("warning: create Knowledge dir: %v", err)
			} else if err := os.WriteFile(crossPath, []byte(crossDoc), 0o644); err != nil {
				log.Printf("warning: write cross-project knowledge: %v", err)
			} else {
				result.KnowledgeWritten = true
			}
		}
	}

	return result, nil
}
