package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTemplate_Fallback(t *testing.T) {
	vault := t.TempDir()
	fallback := func() string { return "default content" }

	got := resolveTemplate(vault, "workflow.md", TemplateVars{Project: "test"}, fallback)
	if got != "default content" {
		t.Errorf("expected fallback content, got %q", got)
	}
}

func TestResolveTemplate_VaultOverride(t *testing.T) {
	vault := t.TempDir()
	tmplDir := filepath.Join(vault, "Templates", "agentctx")
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, "workflow.md"), []byte("custom for {{PROJECT}}"), 0o644)

	fallback := func() string { return "default content" }

	got := resolveTemplate(vault, "workflow.md", TemplateVars{Project: "myapp"}, fallback)
	if got != "custom for myapp" {
		t.Errorf("expected vault override with substitution, got %q", got)
	}
}

func TestResolveTemplate_VarSubstitution(t *testing.T) {
	vault := t.TempDir()
	tmplDir := filepath.Join(vault, "Templates", "agentctx")
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, "test.md"), []byte("project={{PROJECT}} date={{DATE}}"), 0o644)

	vars := TemplateVars{Project: "myapp", Date: "2026-03-06"}
	got := resolveTemplate(vault, "test.md", vars, func() string { return "" })
	if got != "project=myapp date=2026-03-06" {
		t.Errorf("got %q", got)
	}
}

func TestApplyVars(t *testing.T) {
	content := "# {{PROJECT}} — Created {{DATE}}"
	vars := TemplateVars{Project: "demo", Date: "2026-01-01"}
	got := applyVars(content, vars)
	want := "# demo — Created 2026-01-01"
	if got != want {
		t.Errorf("applyVars = %q, want %q", got, want)
	}
}

func TestEnsureVaultTemplates_Creates(t *testing.T) {
	vault := t.TempDir()
	actions := EnsureVaultTemplates(vault)

	if len(actions) == 0 {
		t.Error("expected actions from EnsureVaultTemplates, got none")
	}

	// README.md should exist
	readme := filepath.Join(vault, "Templates", "agentctx", "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		t.Error("Templates/agentctx/README.md not created")
	}

	// commands/restart.md should exist
	restart := filepath.Join(vault, "Templates", "agentctx", "commands", "restart.md")
	if _, err := os.Stat(restart); os.IsNotExist(err) {
		t.Error("Templates/agentctx/commands/restart.md not created")
	}
}

func TestEnsureVaultTemplates_SkipsExisting(t *testing.T) {
	vault := t.TempDir()
	tmplDir := filepath.Join(vault, "Templates", "agentctx")
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, "README.md"), []byte("custom"), 0o644)

	EnsureVaultTemplates(vault)

	// README.md should keep custom content
	data, _ := os.ReadFile(filepath.Join(tmplDir, "README.md"))
	if string(data) != "custom" {
		t.Errorf("README.md was overwritten: %q", string(data))
	}
}

func TestDefaultVars(t *testing.T) {
	vars := DefaultVars("myproject")
	if vars.Project != "myproject" {
		t.Errorf("Project = %q, want %q", vars.Project, "myproject")
	}
	if vars.Date == "" {
		t.Error("Date is empty")
	}
	// Date should be YYYY-MM-DD format
	if !strings.Contains(vars.Date, "-") || len(vars.Date) != 10 {
		t.Errorf("Date format unexpected: %q", vars.Date)
	}
}
