package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcceptCommands_AcceptAll(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	os.MkdirAll(cmdsDir, 0o755)

	// Set up original and pending files
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old content"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new content"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "restart.md"), []byte("old restart"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "restart.md.pending"), []byte("new restart"), 0o644)

	actions, err := AcceptCommands(dir, "", false)
	if err != nil {
		t.Fatalf("AcceptCommands: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	for _, a := range actions {
		if a.Action != "UPDATE" {
			t.Errorf("action = %q, want UPDATE", a.Action)
		}
	}

	// Originals should have new content
	data, _ := os.ReadFile(filepath.Join(cmdsDir, "wrap.md"))
	if string(data) != "new content" {
		t.Errorf("wrap.md = %q, want 'new content'", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(cmdsDir, "restart.md"))
	if string(data) != "new restart" {
		t.Errorf("restart.md = %q, want 'new restart'", string(data))
	}

	// .pending files should be removed
	if _, err := os.Stat(filepath.Join(cmdsDir, "wrap.md.pending")); !os.IsNotExist(err) {
		t.Error("wrap.md.pending should be removed")
	}
	if _, err := os.Stat(filepath.Join(cmdsDir, "restart.md.pending")); !os.IsNotExist(err) {
		t.Error("restart.md.pending should be removed")
	}
}

func TestAcceptCommands_SingleFile(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	os.MkdirAll(cmdsDir, 0o755)

	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "restart.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "restart.md.pending"), []byte("new"), 0o644)

	actions, err := AcceptCommands(dir, "wrap.md", false)
	if err != nil {
		t.Fatalf("AcceptCommands: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Path != "commands/wrap.md" {
		t.Errorf("path = %q, want commands/wrap.md", actions[0].Path)
	}

	// wrap.md should be updated
	data, _ := os.ReadFile(filepath.Join(cmdsDir, "wrap.md"))
	if string(data) != "new" {
		t.Errorf("wrap.md = %q, want 'new'", string(data))
	}

	// restart.md.pending should still exist (not processed)
	if _, err := os.Stat(filepath.Join(cmdsDir, "restart.md.pending")); os.IsNotExist(err) {
		t.Error("restart.md.pending should still exist")
	}
}

func TestAcceptCommands_KeepMine(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	os.MkdirAll(cmdsDir, 0o755)

	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("my custom"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new template"), 0o644)

	actions, err := AcceptCommands(dir, "wrap.md", true)
	if err != nil {
		t.Fatalf("AcceptCommands: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "SKIP" {
		t.Errorf("action = %q, want SKIP", actions[0].Action)
	}

	// Original should be preserved
	data, _ := os.ReadFile(filepath.Join(cmdsDir, "wrap.md"))
	if string(data) != "my custom" {
		t.Errorf("wrap.md = %q, want 'my custom'", string(data))
	}

	// .pinned marker should exist
	if _, err := os.Stat(filepath.Join(cmdsDir, "wrap.md.pinned")); os.IsNotExist(err) {
		t.Error("wrap.md.pinned should exist")
	}

	// .pending should be removed
	if _, err := os.Stat(filepath.Join(cmdsDir, "wrap.md.pending")); !os.IsNotExist(err) {
		t.Error("wrap.md.pending should be removed")
	}
}

func TestAcceptPending_MultiSubdir(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(cmdsDir, 0o755)
	os.MkdirAll(skillsDir, 0o755)

	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old cmd"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new cmd"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill"), []byte("old skill"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill.pending"), []byte("new skill"), 0o644)

	actions, err := AcceptPending(dir, "", false)
	if err != nil {
		t.Fatalf("AcceptPending: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	paths := map[string]bool{}
	for _, a := range actions {
		paths[a.Path] = true
		if a.Action != "UPDATE" {
			t.Errorf("action = %q, want UPDATE", a.Action)
		}
	}
	if !paths["commands/wrap.md"] {
		t.Error("missing commands/wrap.md action")
	}
	if !paths["skills/test.skill"] {
		t.Error("missing skills/test.skill action")
	}

	// Content should be updated
	data, _ := os.ReadFile(filepath.Join(cmdsDir, "wrap.md"))
	if string(data) != "new cmd" {
		t.Errorf("wrap.md = %q", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(skillsDir, "test.skill"))
	if string(data) != "new skill" {
		t.Errorf("test.skill = %q", string(data))
	}
}

func TestAcceptPending_QualifiedFile(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(cmdsDir, 0o755)
	os.MkdirAll(skillsDir, 0o755)

	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "test.skill.pending"), []byte("new"), 0o644)

	// Accept only the skill using qualified path
	actions, err := AcceptPending(dir, "skills/test.skill", false)
	if err != nil {
		t.Fatalf("AcceptPending: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Path != "skills/test.skill" {
		t.Errorf("path = %q, want skills/test.skill", actions[0].Path)
	}

	// Command pending should still exist (not processed)
	if _, err := os.Stat(filepath.Join(cmdsDir, "wrap.md.pending")); os.IsNotExist(err) {
		t.Error("wrap.md.pending should still exist")
	}
}

func TestAcceptPending_BareFileFallsBackToCommands(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, "commands")
	os.MkdirAll(cmdsDir, 0o755)

	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("new"), 0o644)

	// Bare filename without subdir prefix → falls back to commands/
	actions, err := AcceptPending(dir, "wrap.md", false)
	if err != nil {
		t.Fatalf("AcceptPending: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Path != "commands/wrap.md" {
		t.Errorf("path = %q, want commands/wrap.md", actions[0].Path)
	}
}
