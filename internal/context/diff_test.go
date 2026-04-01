package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiffProjectContent_CommandsAndSkills(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(cmdsDir, 0o755)
	os.MkdirAll(skillsDir, 0o755)

	// Pending command
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old cmd"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new cmd"), 0o644)

	// Pending skill
	os.WriteFile(filepath.Join(skillsDir, "test.skill.pending"), []byte("new skill"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill"), []byte("old skill"), 0o644)

	diffs := DiffProjectContent(dir)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}

	names := map[string]bool{}
	for _, d := range diffs {
		names[d.Name] = true
	}
	if !names["commands/wrap.md"] {
		t.Error("missing commands/wrap.md diff")
	}
	if !names["skills/test.skill"] {
		t.Error("missing skills/test.skill diff")
	}
}

func TestDiffProjectContent_EmptySubdirs(t *testing.T) {
	dir := t.TempDir()
	// No commands/ or skills/ directories
	diffs := DiffProjectContent(dir)
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs, got %d", len(diffs))
	}
}

func TestDiffProjectCommands_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(cmdsDir, 0o755)
	os.MkdirAll(skillsDir, 0o755)

	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill.pending"), []byte("new"), 0o644)

	// Deprecated function should only return commands
	diffs := DiffProjectCommands(dir)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff (commands only), got %d", len(diffs))
	}
	if diffs[0].Name != "commands/wrap.md" {
		t.Errorf("name = %q, want commands/wrap.md", diffs[0].Name)
	}
}
