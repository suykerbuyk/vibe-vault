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
	ProjectsUpdated int
}

// GenerateContext writes per-project history.md files. It uses the
// already-loaded index (no rebuild) so it's fast enough for post-hook use.
// alertThreshold is the friction alert threshold (0 = disabled).
func GenerateContext(idx *Index, vaultPath string, alertThreshold int) (*GenerateResult, error) {
	result := &GenerateResult{}

	// Generate per-project context documents
	projectsDir := filepath.Join(vaultPath, "Projects")
	for _, project := range idx.Projects() {
		doc := idx.ProjectContext(project, alertThreshold)
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

		// Seed per-project knowledge.md if it doesn't exist
		knowledgePath := filepath.Join(dir, "knowledge.md")
		if _, err := os.Stat(knowledgePath); os.IsNotExist(err) {
			content := "# Knowledge — " + project + "\n\n## Decisions\n\n## Patterns\n\n## Learnings\n"
			if err := os.WriteFile(knowledgePath, []byte(content), 0o644); err != nil {
				log.Printf("warning: write knowledge.md for %s: %v", project, err)
			}
		}
	}

	return result, nil
}
