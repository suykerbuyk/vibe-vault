package check

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/johns/vibe-vault/internal/config"
)

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

func TestCheckSessions_Pass(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "Sessions")
	os.Mkdir(sessDir, 0o755)
	os.WriteFile(filepath.Join(sessDir, "note1.md"), []byte("# Note"), 0o644)
	os.WriteFile(filepath.Join(sessDir, "note2.md"), []byte("# Note"), 0o644)

	r := CheckSessions(sessDir)
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if r.Detail != "Sessions/ (2 notes)" {
		t.Errorf("unexpected detail: %s", r.Detail)
	}
}

func TestCheckSessions_Warn(t *testing.T) {
	r := CheckSessions("/nonexistent/sessions")
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
	if r.Status != Pass {
		t.Errorf("expected Pass, got %s: %s", r.Status, r.Detail)
	}
	if r.Detail != "disabled" {
		t.Errorf("unexpected detail: %s", r.Detail)
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
	if r.Status != Warn {
		t.Errorf("expected Warn, got %s: %s", r.Status, r.Detail)
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

	sessDir := filepath.Join(vault, "Sessions")
	os.Mkdir(sessDir, 0o755)
	os.WriteFile(filepath.Join(sessDir, "note.md"), []byte("# Note"), 0o644)

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
