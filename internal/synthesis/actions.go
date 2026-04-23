package synthesis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	vvcontext "github.com/suykerbuyk/vibe-vault/internal/context"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
)

// Apply executes the synthesis result: appends learnings, flags stale entries,
// updates resume sections, and updates task status.
func Apply(result *Result, project string, cfg config.Config) (*ActionReport, error) {
	report := &ActionReport{}
	projectDir := filepath.Join(cfg.VaultPath, "Projects", project)

	// Append learnings to knowledge.md
	knowledgePath := filepath.Join(projectDir, "knowledge.md")
	added, skipped, err := appendLearnings(knowledgePath, project, result.Learnings)
	if err != nil {
		return nil, fmt.Errorf("append learnings: %w", err)
	}
	report.LearningsAdded = added
	report.LearningsSkipped = skipped

	// Flag stale entries
	flagged, fSkipped, err := flagStaleEntries(result.StaleEntries, projectDir)
	if err != nil {
		return nil, fmt.Errorf("flag stale entries: %w", err)
	}
	report.StalesFlagged = flagged
	report.StalesSkipped = fSkipped

	// Update resume
	if result.ResumeUpdate != nil {
		agentctxDir := filepath.Join(projectDir, "agentctx")
		var updated bool
		updated, err = updateResume(agentctxDir, result.ResumeUpdate)
		if err != nil {
			return nil, fmt.Errorf("update resume: %w", err)
		}
		report.ResumeUpdated = updated
	}

	// Apply task updates
	tasksDir := filepath.Join(projectDir, "agentctx", "tasks")
	taskCount, err := applyTaskUpdates(tasksDir, result.TaskUpdates)
	if err != nil {
		return nil, fmt.Errorf("apply task updates: %w", err)
	}
	report.TasksUpdated = taskCount

	return report, nil
}

func appendLearnings(knowledgePath, project string, learnings []Learning) (added, skipped int, err error) {
	if len(learnings) == 0 {
		return 0, 0, nil
	}

	content, err := os.ReadFile(knowledgePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, 0, err
		}
		// Seed knowledge.md from template
		content = []byte(fmt.Sprintf("# Knowledge — %s\n\n## Decisions\n\n## Patterns\n\n## Learnings\n", project))
	}
	doc := string(content)

	for _, l := range learnings {
		section := "## " + l.Section
		sectionIdx := strings.Index(doc, section)
		if sectionIdx < 0 {
			skipped++
			continue
		}

		// Check for duplicates in this section
		sectionBody := extractSectionBullets(doc, l.Section)
		newWords := mdutil.SignificantWords(l.Entry)
		isDup := false
		for _, bullet := range sectionBody {
			bulletWords := mdutil.SignificantWords(bullet)
			if mdutil.Overlap(newWords, bulletWords) >= 2 {
				isDup = true
				break
			}
		}
		if isDup {
			skipped++
			continue
		}

		// Find insertion point: after the last bullet in this section
		doc = insertBulletInSection(doc, l.Section, l.Entry)
		added++
	}

	if added > 0 {
		if err := mdutil.AtomicWriteFile(knowledgePath, []byte(doc), 0o644); err != nil {
			return added, skipped, err
		}
	}
	return added, skipped, nil
}

// extractSectionBullets returns all "- " lines in the given section.
func extractSectionBullets(doc, section string) []string {
	lines := strings.Split(doc, "\n")
	target := "## " + section
	inSection := false
	var bullets []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == target {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if inSection && strings.HasPrefix(trimmed, "- ") {
			bullets = append(bullets, strings.TrimPrefix(trimmed, "- "))
		}
	}
	return bullets
}

// insertBulletInSection appends a bullet after the last bullet in the section.
func insertBulletInSection(doc, section, entry string) string {
	lines := strings.Split(doc, "\n")
	target := "## " + section
	sectionStart := -1
	lastBulletIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == target {
			sectionStart = i
			continue
		}
		if sectionStart >= 0 && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if sectionStart >= 0 && strings.HasPrefix(trimmed, "- ") {
			lastBulletIdx = i
		}
	}

	insertAt := lastBulletIdx + 1
	if lastBulletIdx < 0 && sectionStart >= 0 {
		// Empty section: insert after heading + blank line
		insertAt = sectionStart + 1
		// Skip any blank lines after heading
		for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) == "" {
			insertAt++
		}
	}

	newLine := "- " + entry
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertAt]...)
	result = append(result, newLine)
	result = append(result, lines[insertAt:]...)

	return strings.Join(result, "\n")
}

func flagStaleEntries(entries []StaleEntry, projectDir string) (flagged, skipped int, err error) {
	if len(entries) == 0 {
		return 0, 0, nil
	}

	// Group by file to minimize reads/writes
	byFile := make(map[string][]StaleEntry)
	for _, e := range entries {
		byFile[e.File] = append(byFile[e.File], e)
	}

	for file, fileEntries := range byFile {
		var path string
		switch file {
		case "knowledge.md":
			path = filepath.Join(projectDir, "knowledge.md")
		case "resume.md":
			path = filepath.Join(projectDir, "agentctx", "resume.md")
		default:
			skipped += len(fileEntries)
			continue
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			skipped += len(fileEntries)
			continue
		}
		doc := string(data)
		modified := false

		for _, entry := range fileEntries {
			newDoc, matched := flagEntry(doc, entry)
			if matched {
				doc = newDoc
				modified = true
				flagged++
			} else {
				skipped++
			}
		}

		if modified {
			if err := mdutil.AtomicWriteFile(path, []byte(doc), 0o644); err != nil {
				return flagged, skipped, err
			}
		}
	}
	return flagged, skipped, nil
}

// flagEntry tries to flag a stale entry in the document. Returns the modified
// document and whether a match was found.
func flagEntry(doc string, entry StaleEntry) (string, bool) {
	lines := strings.Split(doc, "\n")
	target := "## " + entry.Section
	sectionStart := -1
	bulletIdx := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == target {
			sectionStart = i
			bulletIdx = 0
			continue
		}
		if sectionStart >= 0 && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if sectionStart >= 0 && strings.HasPrefix(trimmed, "- ") {
			// Already flagged?
			if strings.Contains(line, "*(stale:") {
				bulletIdx++
				continue
			}

			// Strategy 1: index-based match with text verification
			if bulletIdx == entry.Index {
				entryWords := mdutil.SignificantWords(entry.Entry)
				bulletWords := mdutil.SignificantWords(trimmed)
				if mdutil.Overlap(entryWords, bulletWords) >= 2 {
					lines[i] = line + fmt.Sprintf(" *(stale: %s)*", entry.Reason)
					return strings.Join(lines, "\n"), true
				}
			}

			// Strategy 2: fuzzy text match (fallback)
			if bulletIdx != entry.Index {
				entryWords := mdutil.SignificantWords(entry.Entry)
				bulletWords := mdutil.SignificantWords(trimmed)
				if mdutil.Overlap(entryWords, bulletWords) >= 2 {
					lines[i] = line + fmt.Sprintf(" *(stale: %s)*", entry.Reason)
					return strings.Join(lines, "\n"), true
				}
			}

			bulletIdx++
		}
	}
	return doc, false
}

func updateResume(agentctxDir string, update *ResumeUpdate) (bool, error) {
	resumePath := filepath.Join(agentctxDir, "resume.md")
	data, err := os.ReadFile(resumePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // no-op
		}
		return false, err
	}
	doc := string(data)
	modified := false

	if update.CurrentState != "" {
		newDoc, err := mdutil.ReplaceSectionBody(doc, "Current State", update.CurrentState)
		if err == nil {
			doc = newDoc
			modified = true
		}
	}

	if update.OpenThreads != "" {
		newDoc, err := mdutil.ReplaceSectionBody(doc, "Open Threads", update.OpenThreads)
		if err == nil {
			doc = newDoc
			modified = true
		}
	}

	if modified {
		if err := mdutil.AtomicWriteFile(resumePath, []byte(doc), 0o644); err != nil {
			return true, err
		}
	}

	// Route shipped-capability narrative to features.md on v10+ projects.
	// Pre-v10 projects don't have the features.md contract yet — ignore silently.
	if update.Features != "" {
		vf, verr := vvcontext.ReadVersion(agentctxDir)
		if verr == nil && vf.SchemaVersion >= 10 {
			featuresPath := filepath.Join(agentctxDir, "features.md")
			if err := appendFeaturesEntry(featuresPath, update.Features); err != nil {
				return modified, err
			}
		}
	}

	return modified, nil
}

// appendFeaturesEntry appends entry as a bullet to the first `## ` section of
// features.md. If features.md doesn't exist or contains no `## ` section, a
// new `## Ungrouped` section is seeded first.
func appendFeaturesEntry(featuresPath, entry string) error {
	content, err := os.ReadFile(featuresPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	doc := string(content)

	section := firstMarkdownSection(doc)
	if section == "" {
		section = "Ungrouped"
		switch {
		case doc == "":
			doc = "# Features\n\n## Ungrouped\n"
		case strings.HasSuffix(doc, "\n"):
			doc += "\n## Ungrouped\n"
		default:
			doc += "\n\n## Ungrouped\n"
		}
	}
	doc = insertBulletInSection(doc, section, entry)
	return mdutil.AtomicWriteFile(featuresPath, []byte(doc), 0o644)
}

// firstMarkdownSection returns the name of the first `## ` heading, or "" if
// none is present. The returned name has `## ` and surrounding whitespace
// stripped.
func firstMarkdownSection(doc string) string {
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
		}
	}
	return ""
}

func applyTaskUpdates(tasksDir string, updates []TaskUpdate) (int, error) {
	count := 0
	for _, u := range updates {
		taskPath := filepath.Join(tasksDir, u.Name+".md")
		data, err := os.ReadFile(taskPath)
		if err != nil {
			continue // missing task → skip
		}
		content := string(data)

		switch u.Action {
		case "complete":
			content = statusRegexp.ReplaceAllString(content, "Status: Done")
			if err := mdutil.AtomicWriteFile(taskPath, []byte(content), 0o644); err != nil {
				return count, err
			}
			// Move to done/
			doneDir := filepath.Join(tasksDir, "done")
			if err := os.MkdirAll(doneDir, 0o755); err != nil {
				return count, err
			}
			donePath := filepath.Join(doneDir, u.Name+".md")
			if err := os.Rename(taskPath, donePath); err != nil {
				return count, err
			}
			count++

		case "update_status":
			if statusRegexp.MatchString(content) {
				content = statusRegexp.ReplaceAllString(content, "Status: "+u.Status)
			} else {
				// Insert status after first heading
				lines := strings.SplitN(content, "\n", 2)
				if len(lines) == 2 {
					content = lines[0] + "\n\nStatus: " + u.Status + "\n" + lines[1]
				}
			}
			if err := mdutil.AtomicWriteFile(taskPath, []byte(content), 0o644); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}
