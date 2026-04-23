package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/plugin"
)

// pluginGenerate is a test helper that calls plugin.Generate.
func pluginGenerate(version string) (string, error) {
	return plugin.Generate(version)
}

func TestCheckVaultPath_Pass(t *testing.T) {
	dir := t.TempDir()
	r := CheckVaultPath(dir)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckVaultPath_Fail(t *testing.T) {
	r := CheckVaultPath("/nonexistent/vault/path")
	if r.Status != Fail {
		t.Errorf("expected Fail, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckObsidian_Pass(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".obsidian"), 0o755)
	r := CheckObsidian(dir)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckObsidian_Warn(t *testing.T) {
	dir := t.TempDir()
	r := CheckObsidian(dir)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckProjects_Pass(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "Projects")
	os.Mkdir(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "note1.md"), []byte("# Note"), 0o644)
	os.WriteFile(filepath.Join(projDir, "note2.md"), []byte("# Note"), 0o644)

	r := CheckProjects(projDir)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if r.Detail != "Projects/ (2 notes)" {
		t.Errorf("unexpected detail: %s", r.Detail)
	}
}

func TestCheckProjects_Warn(t *testing.T) {
	r := CheckProjects("/nonexistent/projects")
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckStateDir_Pass(t *testing.T) {
	dir := t.TempDir()
	r := CheckStateDir(dir)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckStateDir_Warn(t *testing.T) {
	r := CheckStateDir("/nonexistent/state")
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckIndex_Pass(t *testing.T) {
	dir := t.TempDir()
	idx := map[string]interface{}{
		"sess-1": map[string]string{"title": "One"},
		"sess-2": map[string]string{"title": "Two"},
		"sess-3": map[string]string{"title": "Three"},
	}
	data, _ := json.Marshal(idx)
	os.WriteFile(filepath.Join(dir, "session-index.json"), data, 0o644)

	r := CheckIndex(dir)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if r.Detail != "session-index.json (3 entries)" {
		t.Errorf("unexpected detail: %s", r.Detail)
	}
}

func TestCheckIndex_Warn(t *testing.T) {
	dir := t.TempDir()
	r := CheckIndex(dir)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckIndex_Fail(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "session-index.json"), []byte("{bad json"), 0o644)

	r := CheckIndex(dir)
	if r.Status != Fail {
		t.Errorf("expected Fail, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckDomains_AllExist(t *testing.T) {
	work := t.TempDir()
	personal := t.TempDir()

	domains := config.DomainsConfig{Work: work, Personal: personal}
	results := CheckDomains(domains)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != Pass {
			t.Errorf("%s: expected Pass, got %s: %s", r.Name, r.Status, r.Detail)
		}
	}
}

func TestCheckDomains_SomeMissing(t *testing.T) {
	work := t.TempDir()
	domains := config.DomainsConfig{Work: work, Personal: "/nonexistent/personal"}
	results := CheckDomains(domains)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Status != Pass {
		t.Errorf("work: expected Pass, got %s", results[0].Status)
	}
	if results[1].Status != Warn {
		t.Errorf("personal: expected Warn, got %s", results[1].Status)
	}
}

func TestCheckDomains_EmptySkipped(t *testing.T) {
	domains := config.DomainsConfig{Work: "", Personal: "", Opensource: ""}
	results := CheckDomains(domains)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty domains, got %d", len(results))
	}
}

func TestCheckEnrichment_Disabled(t *testing.T) {
	ecfg := config.EnrichmentConfig{Enabled: false}
	r := CheckEnrichment(ecfg)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "disabled") {
		t.Errorf("expected 'disabled' in detail, got: %s", r.Detail)
	}
}

func TestCheckEnrichment_EnabledWithKey(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-123")
	ecfg := config.EnrichmentConfig{Enabled: true, APIKeyEnv: "TEST_API_KEY"}
	r := CheckEnrichment(ecfg)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckEnrichment_EnabledNoKey(t *testing.T) {
	t.Setenv("TEST_API_KEY_MISSING", "")
	ecfg := config.EnrichmentConfig{Enabled: true, APIKeyEnv: "TEST_API_KEY_MISSING"}
	r := CheckEnrichment(ecfg)
	if r.Status != Fail {
		t.Errorf("expected Fail, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckEnrichment_InferredKeyEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-123")
	ecfg := config.EnrichmentConfig{Enabled: true, Provider: "anthropic"}
	r := CheckEnrichment(ecfg)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "anthropic") {
		t.Errorf("expected 'anthropic' in detail, got: %s", r.Detail)
	}
}

func TestCheckSynthesis_Disabled(t *testing.T) {
	scfg := config.SynthesisConfig{Enabled: false}
	ecfg := config.EnrichmentConfig{Enabled: true}
	r := CheckSynthesis(scfg, ecfg)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "disabled") {
		t.Errorf("expected 'disabled' in detail, got: %s", r.Detail)
	}
}

func TestCheckSynthesis_EnabledNoLLM(t *testing.T) {
	scfg := config.SynthesisConfig{Enabled: true}
	ecfg := config.EnrichmentConfig{Enabled: false}
	r := CheckSynthesis(scfg, ecfg)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "no LLM provider") {
		t.Errorf("expected 'no LLM provider' in detail, got: %s", r.Detail)
	}
}

func TestCheckSynthesis_EnabledLLMNoKey(t *testing.T) {
	t.Setenv("TEST_SYNTH_KEY", "")
	scfg := config.SynthesisConfig{Enabled: true}
	ecfg := config.EnrichmentConfig{Enabled: true, APIKeyEnv: "TEST_SYNTH_KEY"}
	r := CheckSynthesis(scfg, ecfg)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckSynthesis_Pass(t *testing.T) {
	t.Setenv("TEST_SYNTH_KEY", "sk-test-123")
	scfg := config.SynthesisConfig{Enabled: true}
	ecfg := config.EnrichmentConfig{Enabled: true, Provider: "anthropic", Model: "claude-sonnet", APIKeyEnv: "TEST_SYNTH_KEY"}
	r := CheckSynthesis(scfg, ecfg)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "anthropic/claude-sonnet") {
		t.Errorf("expected 'anthropic/claude-sonnet' in detail, got: %s", r.Detail)
	}
}

func TestCheckHookFile_Pass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"vv hook"}]}]}}`
	os.WriteFile(path, []byte(content), 0o644)

	r := checkHookFile(path)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckHookFile_Warn(t *testing.T) {
	r := checkHookFile("/nonexistent/settings.json")
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckHookFile_Fail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"hooks":{}}`), 0o644)

	r := checkHookFile(path)
	if r.Status != Fail {
		t.Errorf("expected Fail, got %s: %s", r.Status, r.Detail)
	}
}

func TestReport_HasFailures_True(t *testing.T) {
	r := Report{Results: []Result{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Fail},
	}}
	if !r.HasFailures() {
		t.Error("expected HasFailures() == true")
	}
}

func TestReport_HasFailures_False(t *testing.T) {
	r := Report{Results: []Result{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Warn},
	}}
	if r.HasFailures() {
		t.Error("expected HasFailures() == false")
	}
}

func TestRun_Integration(t *testing.T) {
	vault := t.TempDir()

	// Create vault structure.
	os.Mkdir(filepath.Join(vault, ".obsidian"), 0o755)

	projDir := filepath.Join(vault, "Projects")
	os.Mkdir(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "note.md"), []byte("# Note"), 0o644)

	stateDir := filepath.Join(vault, ".vibe-vault")
	os.Mkdir(stateDir, 0o755)

	idx := map[string]interface{}{"sess-1": map[string]string{"title": "Test"}}
	data, _ := json.Marshal(idx)
	os.WriteFile(filepath.Join(stateDir, "session-index.json"), data, 0o644)

	cfg := config.Config{
		VaultPath: vault,
		Domains:   config.DomainsConfig{}, // empty = skipped
		Enrichment: config.EnrichmentConfig{
			Enabled: false,
		},
	}

	report := Run(cfg)

	// Check no failures (hook will warn/fail but vault structure is valid).
	for _, res := range report.Results {
		if res.Name == "hook" {
			continue // hook depends on real home dir
		}
		if res.Status == Fail {
			t.Errorf("unexpected failure: %s — %s", res.Name, res.Detail)
		}
	}

	// Verify format output is non-empty.
	output := report.Format()
	if output == "" {
		t.Error("Format() returned empty string")
	}
}

func TestCheckAgentctxSchema_Current(t *testing.T) {
	vault := t.TempDir()
	project := "myproject"
	agentctxDir := filepath.Join(vault, "Projects", project, "agentctx")
	os.MkdirAll(agentctxDir, 0o755)
	os.WriteFile(filepath.Join(agentctxDir, ".version"), []byte("schema_version = 2\n"), 0o644)

	r := CheckAgentctxSchema(vault, project, 2)
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckAgentctxSchema_Outdated(t *testing.T) {
	vault := t.TempDir()
	project := "myproject"
	agentctxDir := filepath.Join(vault, "Projects", project, "agentctx")
	os.MkdirAll(agentctxDir, 0o755)
	// No .version file = schema v0

	r := CheckAgentctxSchema(vault, project, 2)
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "vv context sync") {
		t.Errorf("expected detail to suggest vv context sync, got: %s", r.Detail)
	}
}

func TestCheckAgentctxSchema_NoAgentctx(t *testing.T) {
	vault := t.TempDir()
	r := CheckAgentctxSchema(vault, "nonexistent", 2)
	if r != nil {
		t.Errorf("expected nil result for missing agentctx, got %+v", r)
	}
}

func TestStatus_String(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{Pass, "pass"},
		{Warn, "warn"},
		{Fail, "FAIL"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

// --- checkMCPFile plugin-aware tests ---

func writeSettings(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckMCPFile_PluginOnly_NoFiles(t *testing.T) {
	// Plugin configured in settings but no files on disk → Warn.
	// Isolate HOME so IsInstalled()/AnyCacheInstalled() don't find real files.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	dir := t.TempDir()
	path := writeSettings(t, dir, `{
		"extraKnownMarketplaces": {"vibe-vault-local": {"source": {"source": "directory"}}},
		"enabledPlugins": {"vibe-vault@vibe-vault-local": true}
	}`)

	r := checkMCPFile(path)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "files missing") {
		t.Errorf("expected 'files missing' in detail, got: %s", r.Detail)
	}
}

func TestCheckMCPFile_PluginWithFiles(t *testing.T) {
	// Plugin configured AND files exist on disk → Pass.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	// Create plugin files and cache.
	if _, err := pluginGenerate("1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.InstallToCache("1.0.0"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := writeSettings(t, dir, `{
		"extraKnownMarketplaces": {"vibe-vault-local": {"source": {"source": "directory"}}},
		"enabledPlugins": {"vibe-vault@vibe-vault-local": true}
	}`)

	r := checkMCPFile(path)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "plugin") {
		t.Errorf("expected 'plugin' in detail, got: %s", r.Detail)
	}
}

func TestCheckMCPFile_McpServersOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeSettings(t, dir, `{
		"mcpServers": {"vibe-vault": {"command": "vv", "args": ["mcp"]}}
	}`)

	r := checkMCPFile(path)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "--claude-plugin") {
		t.Errorf("expected '--claude-plugin' suggestion in detail, got: %s", r.Detail)
	}
}

func TestCheckMCPFile_Both(t *testing.T) {
	// Plugin configured + mcpServers, with files on disk → Pass with legacy note.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	// Create plugin files and cache.
	if _, err := plugin.Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.InstallToCache("1.0.0"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := writeSettings(t, dir, `{
		"mcpServers": {"vibe-vault": {"command": "vv"}},
		"extraKnownMarketplaces": {"vibe-vault-local": {"source": {"source": "directory"}}},
		"enabledPlugins": {"vibe-vault@vibe-vault-local": true}
	}`)

	r := checkMCPFile(path)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "legacy") {
		t.Errorf("expected 'legacy' note in detail, got: %s", r.Detail)
	}
}

func TestCheckMCPFile_Neither(t *testing.T) {
	dir := t.TempDir()
	path := writeSettings(t, dir, `{"hooks": {}}`)

	r := checkMCPFile(path)
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "vv mcp install") {
		t.Errorf("expected install suggestion, got: %s", r.Detail)
	}
	// Should NOT suggest --claude-plugin for fresh users.
	if strings.Contains(r.Detail, "--claude-plugin") {
		t.Errorf("should not suggest --claude-plugin for fresh install, got: %s", r.Detail)
	}
}

func TestCheckMemoryLink_Nil_NoProject(t *testing.T) {
	if r := CheckMemoryLink("", "", "/tmp"); r != nil {
		t.Errorf("expected nil, got %v", r)
	}
	if r := CheckMemoryLink("/v", "_unknown", "/tmp"); r != nil {
		t.Errorf("expected nil on _unknown, got %v", r)
	}
}

func TestCheckMemoryLink_Nil_NoAgentctx(t *testing.T) {
	dir := t.TempDir()
	if r := CheckMemoryLink(dir, "no-such-proj", "/tmp"); r != nil {
		t.Errorf("expected nil when agentctx missing, got %v", r)
	}
}

func TestCheckMemoryLink_Warn_NotLinked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vault := t.TempDir()
	project := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vault, "Projects", "demo", "agentctx"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := CheckMemoryLink(vault, "demo", project)
	if r == nil || r.Status != Warn {
		t.Fatalf("expected Warn, got %+v", r)
	}
	if !strings.Contains(r.Detail, "not linked") {
		t.Errorf("expected 'not linked' detail, got: %s", r.Detail)
	}
}

func TestCheckMemoryLink_Warn_RealDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vault := t.TempDir()
	parentDir := t.TempDir()
	project := filepath.Join(parentDir, "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vault, "Projects", "demo", "agentctx"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolved, _ := filepath.EvalSymlinks(project)
	slug := strings.ReplaceAll(filepath.Clean(resolved), "/", "-")
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects", slug, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := CheckMemoryLink(vault, "demo", project)
	if r == nil || r.Status != Warn {
		t.Fatalf("expected Warn, got %+v", r)
	}
	if !strings.Contains(r.Detail, "real directory") {
		t.Errorf("expected 'real directory' detail, got: %s", r.Detail)
	}
}

func TestCheckMemoryLink_Pass(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vault := t.TempDir()
	parentDir := t.TempDir()
	project := filepath.Join(parentDir, "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	agentctx := filepath.Join(vault, "Projects", "demo", "agentctx")
	if err := os.MkdirAll(agentctx, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(agentctx, "memory")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	resolved, _ := filepath.EvalSymlinks(project)
	slug := strings.ReplaceAll(filepath.Clean(resolved), "/", "-")
	parent := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(parent, "memory")); err != nil {
		t.Fatal(err)
	}

	r := CheckMemoryLink(vault, "demo", project)
	if r == nil || r.Status != Pass {
		t.Fatalf("expected Pass, got %+v", r)
	}
}

func TestCheckMemoryLink_Warn_WrongTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vault := t.TempDir()
	parentDir := t.TempDir()
	project := filepath.Join(parentDir, "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vault, "Projects", "demo", "agentctx"), 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(parentDir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}

	resolved, _ := filepath.EvalSymlinks(project)
	slug := strings.ReplaceAll(filepath.Clean(resolved), "/", "-")
	parent := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(parent, "memory")); err != nil {
		t.Fatal(err)
	}

	r := CheckMemoryLink(vault, "demo", project)
	if r == nil || r.Status != Warn {
		t.Fatalf("expected Warn, got %+v", r)
	}
	if !strings.Contains(r.Detail, "points to") {
		t.Errorf("expected 'points to' detail, got: %s", r.Detail)
	}
}

// --- CheckCurrentStateInvariants ---

// seedV10Project creates a vaultPath/Projects/<project>/agentctx/ with a
// v10 .version file and the given resume.md body. Returns the vault path.
func seedV10Project(t *testing.T, project, resumeBody string, schemaVersion int) string {
	t.Helper()
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", project, "agentctx")
	if err := os.MkdirAll(agentctxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	versionTOML := []byte("schema_version = ")
	versionTOML = append(versionTOML, []byte(fmt.Sprintf("%d\n", schemaVersion))...)
	if err := os.WriteFile(filepath.Join(agentctxDir, ".version"), versionTOML, 0o644); err != nil {
		t.Fatal(err)
	}
	if resumeBody != "" {
		if err := os.WriteFile(filepath.Join(agentctxDir, "resume.md"), []byte(resumeBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return vault
}

func TestCheckCurrentStateInvariants_NilOnMissingAgentctx(t *testing.T) {
	vault := t.TempDir()
	if r := CheckCurrentStateInvariants(vault, "nonexistent"); r != nil {
		t.Errorf("expected nil for missing agentctx, got %+v", r)
	}
}

func TestCheckCurrentStateInvariants_NilOnPreV10(t *testing.T) {
	vault := seedV10Project(t, "legacy", "## Current State\n\narbitrary content\n", 9)
	if r := CheckCurrentStateInvariants(vault, "legacy"); r != nil {
		t.Errorf("expected nil for schema v9, got %+v", r)
	}
}

func TestCheckCurrentStateInvariants_NilOnMissingResume(t *testing.T) {
	vault := seedV10Project(t, "empty", "", 10)
	if r := CheckCurrentStateInvariants(vault, "empty"); r != nil {
		t.Errorf("expected nil for missing resume.md, got %+v", r)
	}
}

func TestCheckCurrentStateInvariants_WarnOnMissingHeading(t *testing.T) {
	vault := seedV10Project(t, "noheading", "# Some project\n\n## Other\n\n- **Tests:** 5\n", 10)
	r := CheckCurrentStateInvariants(vault, "noheading")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "Current State") {
		t.Errorf("expected detail to mention Current State, got: %s", r.Detail)
	}
}

func TestCheckCurrentStateInvariants_PassOnCleanBody(t *testing.T) {
	body := "# demo\n\n## Current State\n\n" +
		"- **Iterations:** 42\n" +
		"- **Tests:** 100 unit + 10 integration; coverage 85%.\n" +
		"- **Lint:** clean.\n" +
		"\n## Open Threads\n"
	vault := seedV10Project(t, "demo", body, 10)
	r := CheckCurrentStateInvariants(vault, "demo")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "clean") {
		t.Errorf("expected detail to mention 'clean', got: %s", r.Detail)
	}
}

func TestCheckCurrentStateInvariants_WarnOnOffendingBullet(t *testing.T) {
	body := "## Current State\n\n" +
		"- **Iterations:** 42\n" +
		"- **Phase:** narrative-style phase description that should be rejected\n" +
		"\n## Open Threads\n"
	vault := seedV10Project(t, "dirty", body, 10)
	r := CheckCurrentStateInvariants(vault, "dirty")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "Phase") {
		t.Errorf("expected detail to mention offending 'Phase' bullet, got: %s", r.Detail)
	}
}

func TestExtractCurrentStateBody(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		want  string
		found bool
	}{
		{
			name:  "missing heading",
			doc:   "# Title\n\nno heading\n",
			want:  "",
			found: false,
		},
		{
			name:  "heading to next section",
			doc:   "## Current State\n\n- **Tests:** 5\n\n## Other\n\nelsewhere\n",
			want:  "\n- **Tests:** 5\n",
			found: true,
		},
		{
			name:  "heading at end of doc",
			doc:   "## Current State\n\n- **Tests:** 5\n",
			want:  "\n- **Tests:** 5\n",
			found: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := extractCurrentStateBody(tt.doc)
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if got != tt.want {
				t.Errorf("body = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short passthrough", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncate with ellipsis", "hello world", 5, "hello…"},
		{"trims whitespace", "   hi   ", 10, "hi"},
		{"rune-safe (multibyte)", "aé中bñ", 3, "aé中…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateLine(tt.in, tt.n); got != tt.want {
				t.Errorf("truncateLine(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}
