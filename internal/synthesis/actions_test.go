package synthesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

func TestAppendLearnings_NewEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("# Knowledge\n\n## Decisions\n\n- Existing decision\n\n## Patterns\n\n## Learnings\n"), 0o644)

	added, skipped, err := appendLearnings(path, "test", []Learning{
		{Section: "Decisions", Entry: "New decision about synthesis"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || skipped != 0 {
		t.Errorf("added=%d skipped=%d, want 1/0", added, skipped)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "- New decision about synthesis") {
		t.Error("new entry not found in file")
	}
}

func TestAppendLearnings_DuplicateSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("# Knowledge\n\n## Decisions\n\n- Use synthesis agent for knowledge\n\n## Patterns\n\n## Learnings\n"), 0o644)

	added, skipped, err := appendLearnings(path, "test", []Learning{
		{Section: "Decisions", Entry: "synthesis agent for knowledge propagation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || skipped != 1 {
		t.Errorf("added=%d skipped=%d, want 0/1", added, skipped)
	}
}

func TestAppendLearnings_MissingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("# Knowledge\n\n## Decisions\n\n## Learnings\n"), 0o644)

	added, skipped, err := appendLearnings(path, "test", []Learning{
		{Section: "Patterns", Entry: "test pattern"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Patterns section header exists but no ## Patterns in the file...
	// Actually it doesn't exist in this test. Should be skipped.
	if added != 0 || skipped != 1 {
		t.Errorf("added=%d skipped=%d, want 0/1", added, skipped)
	}
}

func TestAppendLearnings_MissingFile_SeedsTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	// File doesn't exist

	added, _, err := appendLearnings(path, "myproject", []Learning{
		{Section: "Decisions", Entry: "First decision"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("added=%d, want 1", added)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "# Knowledge — myproject") {
		t.Error("template not seeded")
	}
	if !strings.Contains(content, "- First decision") {
		t.Error("learning not appended")
	}
}

func TestAppendLearnings_EmptySection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("# Knowledge\n\n## Decisions\n\n## Patterns\n"), 0o644)

	added, _, err := appendLearnings(path, "test", []Learning{
		{Section: "Decisions", Entry: "New entry in empty section"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("added=%d, want 1", added)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "- New entry in empty section") {
		t.Error("entry not found")
	}
}

func TestFlagStaleEntries_IndexMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("## Decisions\n\n- First\n- Second important decision\n- Third\n"), 0o644)

	flagged, skipped, err := flagStaleEntries(
		[]StaleEntry{{File: "knowledge.md", Section: "Decisions", Index: 1, Entry: "Second important decision", Reason: "outdated"}},
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if flagged != 1 || skipped != 0 {
		t.Errorf("flagged=%d skipped=%d, want 1/0", flagged, skipped)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "*(stale: outdated)*") {
		t.Error("stale marker not found")
	}
}

func TestFlagStaleEntries_FuzzyFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("## Decisions\n\n- First\n- Important decision about synthesis agent\n- Third\n"), 0o644)

	// Wrong index (5) but matching text
	flagged, _, err := flagStaleEntries(
		[]StaleEntry{{File: "knowledge.md", Section: "Decisions", Index: 5, Entry: "synthesis agent decision", Reason: "changed"}},
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if flagged != 1 {
		t.Errorf("flagged=%d, want 1 (fuzzy fallback)", flagged)
	}
}

func TestFlagStaleEntries_NoMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("## Decisions\n\n- Unrelated entry\n"), 0o644)

	flagged, skipped, err := flagStaleEntries(
		[]StaleEntry{{File: "knowledge.md", Section: "Decisions", Index: 0, Entry: "something completely different topic", Reason: "gone"}},
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if flagged != 0 || skipped != 1 {
		t.Errorf("flagged=%d skipped=%d, want 0/1", flagged, skipped)
	}
}

func TestFlagStaleEntries_AlreadyFlagged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")
	os.WriteFile(path, []byte("## Decisions\n\n- Already flagged entry *(stale: old)*\n"), 0o644)

	flagged, skipped, err := flagStaleEntries(
		[]StaleEntry{{File: "knowledge.md", Section: "Decisions", Index: 0, Entry: "Already flagged entry", Reason: "again"}},
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	if flagged != 0 || skipped != 1 {
		t.Errorf("flagged=%d skipped=%d, want 0/1", flagged, skipped)
	}
}

func TestUpdateResume_BothSections(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "resume.md"), []byte("# Resume\n\n## Current State\n\nold state\n\n## Open Threads\n\nold threads\n"), 0o644)

	updated, err := updateResume(dir, &ResumeUpdate{
		CurrentState: "New state after synthesis",
		OpenThreads:  "New threads",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "resume.md"))
	content := string(data)
	if !strings.Contains(content, "New state after synthesis") {
		t.Error("current state not updated")
	}
	if !strings.Contains(content, "New threads") {
		t.Error("open threads not updated")
	}
}

func TestUpdateResume_OneSection(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "resume.md"), []byte("# Resume\n\n## Current State\n\nold\n\n## Open Threads\n\nkeep this\n"), 0o644)

	updated, err := updateResume(dir, &ResumeUpdate{
		CurrentState: "Updated",
		OpenThreads:  "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "resume.md"))
	content := string(data)
	if !strings.Contains(content, "Updated") {
		t.Error("current state not updated")
	}
	if !strings.Contains(content, "keep this") {
		t.Error("open threads should be preserved")
	}
}

func TestUpdateResume_MissingFile(t *testing.T) {
	updated, err := updateResume("/nonexistent", &ResumeUpdate{CurrentState: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("expected updated=false for missing file")
	}
}

func TestSynthesis_RoutesFeaturesToFeaturesMd(t *testing.T) {
	dir := t.TempDir()
	resumePath := filepath.Join(dir, "resume.md")
	featuresPath := filepath.Join(dir, "features.md")
	versionPath := filepath.Join(dir, ".version")

	os.WriteFile(resumePath, []byte("# Resume\n\n## Current State\n\n- **Tests:** 1200.\n\n## Open Threads\n\nold threads\n"), 0o644)
	os.WriteFile(featuresPath, []byte("# Features\n\n## Template cascade\n\n- pre-existing entry\n"), 0o644)
	// Stamp v10.
	os.WriteFile(versionPath, []byte("schema_version = 10\n"), 0o644)

	updated, err := updateResume(dir, &ResumeUpdate{
		OpenThreads: "keep threads updated",
		Features:    "new capability: synthesis Features routing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected resume updated=true (OpenThreads was set)")
	}

	// Current State should be untouched — Features routed elsewhere.
	resumeContent, _ := os.ReadFile(resumePath)
	if !strings.Contains(string(resumeContent), "- **Tests:** 1200.") {
		t.Error("Current State should be untouched by features routing")
	}
	if strings.Contains(string(resumeContent), "synthesis Features routing") {
		t.Error("Features prose should not appear in resume.md")
	}

	// features.md should have the new bullet appended under the first section.
	featuresContent, _ := os.ReadFile(featuresPath)
	fc := string(featuresContent)
	if !strings.Contains(fc, "- new capability: synthesis Features routing") {
		t.Errorf("features.md missing appended entry:\n%s", fc)
	}
	if !strings.Contains(fc, "## Template cascade") || !strings.Contains(fc, "- pre-existing entry") {
		t.Error("features.md existing content should be preserved")
	}
}

func TestSynthesis_IgnoresFeaturesOnPreV10(t *testing.T) {
	dir := t.TempDir()
	resumePath := filepath.Join(dir, "resume.md")
	featuresPath := filepath.Join(dir, "features.md")
	versionPath := filepath.Join(dir, ".version")

	os.WriteFile(resumePath, []byte("# Resume\n\n## Current State\n\nstate\n\n## Open Threads\n\nthreads\n"), 0o644)
	// Stamp v9 — pre-contract.
	os.WriteFile(versionPath, []byte("schema_version = 9\n"), 0o644)

	_, err := updateResume(dir, &ResumeUpdate{
		Features: "should be silently ignored",
	})
	if err != nil {
		t.Fatal(err)
	}

	// features.md must NOT have been created.
	if _, statErr := os.Stat(featuresPath); !os.IsNotExist(statErr) {
		t.Errorf("features.md should not exist on pre-v10 project (stat err: %v)", statErr)
	}
}

func TestAppendFeaturesEntry_SeedsUngroupedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "features.md")

	if err := appendFeaturesEntry(path, "first feature"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "## Ungrouped") {
		t.Errorf("expected seeded Ungrouped section:\n%s", content)
	}
	if !strings.Contains(content, "- first feature") {
		t.Errorf("expected entry as bullet:\n%s", content)
	}
}

func TestAppendFeaturesEntry_AppendsToFirstSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "features.md")
	os.WriteFile(path, []byte("# Features\n\n## A\n\n- one\n- two\n\n## B\n\n- alpha\n"), 0o644)

	if err := appendFeaturesEntry(path, "three"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// "three" should follow section A's bullets, before section B.
	aIdx := strings.Index(content, "## A")
	bIdx := strings.Index(content, "## B")
	threeIdx := strings.Index(content, "- three")
	if aIdx < 0 || bIdx < 0 || threeIdx < 0 {
		t.Fatalf("missing markers (aIdx=%d bIdx=%d threeIdx=%d):\n%s", aIdx, bIdx, threeIdx, content)
	}
	if aIdx >= threeIdx || threeIdx >= bIdx {
		t.Errorf("entry not inserted in section A:\n%s", content)
	}
}

func TestApplyTaskUpdates_Complete(t *testing.T) {
	dir := t.TempDir()
	taskPath := filepath.Join(dir, "my-task.md")
	os.WriteFile(taskPath, []byte("# My Task\n\nStatus: In Progress\n\nDetails here\n"), 0o644)

	count, err := applyTaskUpdates(dir, []TaskUpdate{
		{Name: "my-task", Action: "complete", Status: "Done", Reason: "finished"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count=%d, want 1", count)
	}

	// Original should be gone
	if _, statErr := os.Stat(taskPath); !os.IsNotExist(statErr) {
		t.Error("task file should be moved to done/")
	}

	// Should be in done/
	donePath := filepath.Join(dir, "done", "my-task.md")
	data, err := os.ReadFile(donePath)
	if err != nil {
		t.Fatal("task not found in done/")
	}
	if !strings.Contains(string(data), "Status: Done") {
		t.Error("status not updated to Done")
	}
}

func TestApplyTaskUpdates_UpdateStatus(t *testing.T) {
	dir := t.TempDir()
	taskPath := filepath.Join(dir, "my-task.md")
	os.WriteFile(taskPath, []byte("# My Task\n\nStatus: In Progress\n\nDetails\n"), 0o644)

	count, err := applyTaskUpdates(dir, []TaskUpdate{
		{Name: "my-task", Action: "update_status", Status: "Phase 3 complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count=%d, want 1", count)
	}

	data, _ := os.ReadFile(taskPath)
	if !strings.Contains(string(data), "Status: Phase 3 complete") {
		t.Error("status not updated")
	}
}

func TestApplyTaskUpdates_MissingTask(t *testing.T) {
	dir := t.TempDir()
	count, err := applyTaskUpdates(dir, []TaskUpdate{
		{Name: "nonexistent", Action: "complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count=%d, want 0 for missing task", count)
	}
}

func TestApply_FullWorkflow(t *testing.T) {
	dir := t.TempDir()

	// Set up knowledge.md
	os.WriteFile(filepath.Join(dir, "knowledge.md"), []byte(
		"# Knowledge — test\n\n## Decisions\n\n- Old decision about testing\n\n## Patterns\n\n## Learnings\n",
	), 0o644)

	// Set up resume.md
	agentctx := filepath.Join(dir, "agentctx")
	os.MkdirAll(agentctx, 0o755)
	os.WriteFile(filepath.Join(agentctx, "resume.md"), []byte(
		"# Resume\n\n## Current State\n\nOld state\n\n## Open Threads\n\nOld threads\n",
	), 0o644)

	// Set up task
	tasksDir := filepath.Join(agentctx, "tasks")
	os.MkdirAll(tasksDir, 0o755)
	os.WriteFile(filepath.Join(tasksDir, "my-task.md"), []byte(
		"# My Task\n\nStatus: In Progress\n",
	), 0o644)

	result := &Result{
		Learnings: []Learning{
			{Section: "Decisions", Entry: "New synthesis decision"},
		},
		StaleEntries: []StaleEntry{
			{File: "knowledge.md", Section: "Decisions", Index: 0, Entry: "Old decision about testing", Reason: "superseded"},
		},
		ResumeUpdate: &ResumeUpdate{
			CurrentState: "Synthesis agent complete",
			OpenThreads:  "Integration tests pending",
		},
		TaskUpdates: []TaskUpdate{
			{Name: "my-task", Action: "complete", Reason: "done"},
		},
	}

	cfg := config.Config{VaultPath: filepath.Dir(dir)}
	// We need project dir to be dir, so adjust:
	// Apply uses filepath.Join(cfg.VaultPath, "Projects", project)
	// So set VaultPath such that VaultPath/Projects/test == dir
	projectsDir := filepath.Join(dir, "Projects", "test")
	os.MkdirAll(projectsDir, 0o755)

	// Move files into the proper structure
	projectKnowledge := filepath.Join(projectsDir, "knowledge.md")
	projectAgentctx := filepath.Join(projectsDir, "agentctx")
	os.MkdirAll(filepath.Join(projectAgentctx, "tasks"), 0o755)

	os.WriteFile(projectKnowledge, []byte(
		"# Knowledge — test\n\n## Decisions\n\n- Old decision about testing\n\n## Patterns\n\n## Learnings\n",
	), 0o644)
	os.WriteFile(filepath.Join(projectAgentctx, "resume.md"), []byte(
		"# Resume\n\n## Current State\n\nOld state\n\n## Open Threads\n\nOld threads\n",
	), 0o644)
	os.WriteFile(filepath.Join(projectAgentctx, "tasks", "my-task.md"), []byte(
		"# My Task\n\nStatus: In Progress\n",
	), 0o644)

	cfg.VaultPath = dir
	report, err := Apply(result, "test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if report.LearningsAdded != 1 {
		t.Errorf("learnings added=%d, want 1", report.LearningsAdded)
	}
	if report.StalesFlagged != 1 {
		t.Errorf("stales flagged=%d, want 1", report.StalesFlagged)
	}
	if !report.ResumeUpdated {
		t.Error("expected resume updated")
	}
	if report.TasksUpdated != 1 {
		t.Errorf("tasks updated=%d, want 1", report.TasksUpdated)
	}
}
