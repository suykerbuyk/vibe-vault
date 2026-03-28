package synthesis

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/noteparse"
)

const (
	maxDiffBytes     = 8 * 1024
	maxKnowledgeBytes = 12 * 1024
	maxResumeBytes    = 12 * 1024
	gitTimeout        = 5 * time.Second
)

var statusRegexp = regexp.MustCompile(`(?m)^(?:##\s+)?Status:\s*(.+)`)

// GatherInput collects all context the synthesis agent needs.
func GatherInput(notePath string, cwd string, cfg config.Config, idx *index.Index) (*Input, error) {
	note, err := noteparse.ParseFile(notePath)
	if err != nil {
		return nil, fmt.Errorf("parse session note: %w", err)
	}

	diff := gatherGitDiff(note.Commits, cwd)
	knowledgeMD := readFileCapped(filepath.Join(cfg.VaultPath, "Projects", note.Project, "knowledge.md"), maxKnowledgeBytes)
	resumeMD := readFileCapped(filepath.Join(cfg.VaultPath, "Projects", note.Project, "agentctx", "resume.md"), maxResumeBytes)
	history := gatherHistory(idx, note.Project)
	tasks := gatherTasks(filepath.Join(cfg.VaultPath, "Projects", note.Project, "agentctx", "tasks"))

	return &Input{
		SessionNote:   note,
		GitDiff:       diff,
		KnowledgeMD:   knowledgeMD,
		ResumeMD:      resumeMD,
		RecentHistory: history,
		TaskSummaries: tasks,
	}, nil
}

// gatherGitDiff builds a diff from the session's commits, or falls back to
// uncommitted changes.
func gatherGitDiff(commits []string, cwd string) string {
	var diff string
	if len(commits) > 0 {
		first := commits[0]
		last := commits[len(commits)-1]
		diff = gitCmd(cwd, "diff", first+"~1.."+last)
		if diff == "" {
			// first~1 may fail if it's the initial commit
			diff = gitCmd(cwd, "diff", first+"^.."+last)
		}
	} else {
		staged := gitCmd(cwd, "diff", "--cached")
		unstaged := gitCmd(cwd, "diff")
		diff = staged + unstaged
	}
	return truncateAtNewline(diff, maxDiffBytes)
}

func gitCmd(cwd string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func truncateAtNewline(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Find last newline before maxBytes
	idx := strings.LastIndex(s[:maxBytes], "\n")
	if idx < 0 {
		return s[:maxBytes]
	}
	return s[:idx+1]
}

func readFileCapped(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(data)
	if len(s) > maxBytes {
		return truncateAtNewline(s, maxBytes)
	}
	return s
}

// gatherHistory returns the last 5 sessions for the project from the index.
func gatherHistory(idx *index.Index, project string) []HistoryEntry {
	if idx == nil {
		return nil
	}

	type dated struct {
		date  string
		entry index.SessionEntry
	}
	var entries []dated
	for _, e := range idx.Entries {
		if e.Project == project {
			entries = append(entries, dated{date: e.Date, entry: e})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].date < entries[j].date
	})

	// Last 5
	start := len(entries) - 5
	if start < 0 {
		start = 0
	}
	entries = entries[start:]

	var result []HistoryEntry
	for _, e := range entries {
		result = append(result, HistoryEntry{
			Date:    e.entry.Date,
			Title:   e.entry.Title,
			Summary: e.entry.Summary,
			Tag:     e.entry.Tag,
		})
	}
	return result
}

// gatherTasks reads active task files from the tasks directory.
func gatherTasks(tasksDir string) []TaskSummary {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil
	}

	var tasks []TaskSummary
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tasksDir, entry.Name()))
		if err != nil {
			continue
		}
		title := extractTitle(string(data))
		status := extractStatus(string(data))
		tasks = append(tasks, TaskSummary{
			Name:   strings.TrimSuffix(entry.Name(), ".md"),
			Title:  title,
			Status: status,
		})
	}
	return tasks
}

func extractTitle(content string) string {
	for _, line := range strings.SplitN(content, "\n", 10) {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
		}
	}
	return ""
}

func extractStatus(content string) string {
	m := statusRegexp.FindStringSubmatch(content)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return "unknown"
}
