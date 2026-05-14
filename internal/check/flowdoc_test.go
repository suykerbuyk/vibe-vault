// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeFlowdocRepo builds a temporary directory with a .git marker so
// session.DetectProjectRoot resolves it as the project root. Returns
// the repo path; the caller writes doc/flows.json (or not) as needed.
func makeFlowdocRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return repo
}

// repoRoot walks up from this test file to the module root (the
// directory containing go.mod). The golden fixture's refs are
// expressed relative to that root, so VerifyRefs only resolves them
// cleanly when pointed there.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above test file")
		}
		dir = parent
	}
}

func TestCheckFlowdoc_NilOnEmptyCwd(t *testing.T) {
	if r := CheckFlowdoc(""); r != nil {
		t.Errorf("empty cwd: expected nil, got %+v", r)
	}
}

func TestCheckFlowdoc_WarnOnMissingFlowsJSON(t *testing.T) {
	repo := makeFlowdocRepo(t) // no doc/flows.json written
	r := CheckFlowdoc(repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s; detail: %s", r.Status, r.Detail)
	}
	if r.Detail != "no flows.json" {
		t.Errorf("detail: want %q, got %q", "no flows.json", r.Detail)
	}
	if r.Name != "flowdoc" {
		t.Errorf("name: want %q, got %q", "flowdoc", r.Name)
	}
}

// TestCheckFlowdoc_PassOnValidGolden copies the real golden fixture
// into a tempdir's doc/ but — crucially — points the check at the
// ACTUAL repo root, not the tempdir. The golden's node paths and step
// refs (internal/mcp/, cmd/vv/main.go, etc.) only resolve against a
// tree with real source. We achieve that by writing flows.json into
// the repo root's own doc/ dir under a temp name is not possible
// (CheckFlowdoc hard-codes doc/flows.json), so instead we write the
// fixture into <repoRoot>/doc/flows.json only if absent, and skip if
// the repo already has one (it doesn't ship one — it's gen output).
// This keeps the test deterministic and honest: the golden's refs are
// verified against the live tree they were authored from.
func TestCheckFlowdoc_PassOnValidGolden(t *testing.T) {
	root := repoRoot(t)
	docDir := filepath.Join(root, "doc")
	jsonPath := filepath.Join(docDir, "flows.json")

	if _, err := os.Stat(jsonPath); err == nil {
		t.Skip("repo already has doc/flows.json; skipping to avoid clobbering operator state")
	}

	golden, err := os.ReadFile(filepath.Join(root, "internal", "flowdoc", "testdata", "golden", "vibe-vault-flows.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir doc: %v", err)
	}
	if err := os.WriteFile(jsonPath, golden, 0o644); err != nil {
		t.Fatalf("write flows.json: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(jsonPath)
		// Remove doc/ only if we created it and it's now empty.
		_ = os.Remove(docDir)
	})

	r := CheckFlowdoc(root)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Pass {
		t.Errorf("status: want Pass, got %s; detail: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "flows") || !strings.Contains(r.Detail, "nodes") {
		t.Errorf("detail should mention flow + node counts, got: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "no drift") {
		t.Errorf("detail should report no drift, got: %s", r.Detail)
	}
}

// TestCheckFlowdoc_WarnOnDrift writes the golden fixture into a bare
// tempdir project that has NO source tree. Every node path and step
// ref then fails to resolve, so VerifyRefs returns error-level issues
// and the row is Warn-with-drift. This is the deterministic
// counterpart to the Pass test: a tempdir without source is honestly
// the drift path.
func TestCheckFlowdoc_WarnOnDrift(t *testing.T) {
	repo := makeFlowdocRepo(t)
	root := repoRoot(t)
	golden, err := os.ReadFile(filepath.Join(root, "internal", "flowdoc", "testdata", "golden", "vibe-vault-flows.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	docDir := filepath.Join(repo, "doc")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "flows.json"), golden, 0o644); err != nil {
		t.Fatalf("write flows.json: %v", err)
	}
	r := CheckFlowdoc(repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s; detail: %s", r.Status, r.Detail)
	}
	if r.Status == Fail {
		t.Fatal("flowdoc check must NEVER emit Fail")
	}
	if !strings.Contains(r.Detail, "drift") || !strings.Contains(r.Detail, "vv flowdoc verify") {
		t.Errorf("detail should mention drift count + remediation, got: %s", r.Detail)
	}
}

func TestCheckFlowdoc_WarnOnMalformedJSON(t *testing.T) {
	repo := makeFlowdocRepo(t)
	docDir := filepath.Join(repo, "doc")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "flows.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write flows.json: %v", err)
	}
	r := CheckFlowdoc(repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s; detail: %s", r.Status, r.Detail)
	}
	if r.Status == Fail {
		t.Fatal("flowdoc check must NEVER emit Fail")
	}
	if r.Detail != "invalid flows.json; run vv flowdoc verify" {
		t.Errorf("detail: want %q, got %q", "invalid flows.json; run vv flowdoc verify", r.Detail)
	}
}

func TestCheckFlowdoc_WarnOnValidateFailure(t *testing.T) {
	repo := makeFlowdocRepo(t)
	docDir := filepath.Join(repo, "doc")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir doc: %v", err)
	}
	// Well-formed JSON but fails flowdoc.Validate: empty nodes[] and a
	// bad $schema_version.
	bad := `{"$schema_version":"99","project":"x","nodes":[],"flows":[]}`
	if err := os.WriteFile(filepath.Join(docDir, "flows.json"), []byte(bad), 0o644); err != nil {
		t.Fatalf("write flows.json: %v", err)
	}
	r := CheckFlowdoc(repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s; detail: %s", r.Status, r.Detail)
	}
	if r.Status == Fail {
		t.Fatal("flowdoc check must NEVER emit Fail")
	}
	if r.Detail != "invalid flows.json; run vv flowdoc verify" {
		t.Errorf("detail: want %q, got %q", "invalid flows.json; run vv flowdoc verify", r.Detail)
	}
}

// TestCheckFlowdoc_NeverFails is the explicit guard: across every
// reachable input shape, the flowdoc row's status is Pass or Warn —
// never Fail. A Fail would change `vv check`'s exit code and block
// /restart and /wrap, which the warn-only contract forbids.
func TestCheckFlowdoc_NeverFails(t *testing.T) {
	root := repoRoot(t)
	golden, err := os.ReadFile(filepath.Join(root, "internal", "flowdoc", "testdata", "golden", "vibe-vault-flows.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}

	cases := []struct {
		name    string
		content string // "" means no doc/flows.json at all
	}{
		{"missing", ""},
		{"malformed", "{nope"},
		{"empty object", "{}"},
		{"validate-fail", `{"$schema_version":"99","project":"x","nodes":[],"flows":[]}`},
		{"valid-but-drifted", string(golden)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := makeFlowdocRepo(t)
			if tc.content != "" {
				docDir := filepath.Join(repo, "doc")
				if err := os.MkdirAll(docDir, 0o755); err != nil {
					t.Fatalf("mkdir doc: %v", err)
				}
				if err := os.WriteFile(filepath.Join(docDir, "flows.json"), []byte(tc.content), 0o644); err != nil {
					t.Fatalf("write flows.json: %v", err)
				}
			}
			r := CheckFlowdoc(repo)
			if r == nil {
				t.Fatal("expected Result, got nil")
			}
			if r.Status == Fail {
				t.Errorf("%s: flowdoc row emitted Fail — must only ever be Pass or Warn", tc.name)
			}
		})
	}
}
