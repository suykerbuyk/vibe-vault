// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/flowdoc"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// fakeFlowdocProvider returns canned ChatCompletion responses; injected
// in place of newProviderForFlowdoc to keep the gen-path tests offline.
type fakeFlowdocProvider struct {
	content string
	err     error
	calls   int
	lastReq llm.Request
}

func (f *fakeFlowdocProvider) ChatCompletion(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.content}, nil
}

func (f *fakeFlowdocProvider) Name() string { return "fake-flowdoc" }

func TestRunFlowdocUsage(t *testing.T) {
	stderr, restore := captureStderr(t)

	code := runFlowdoc(nil)
	restore()
	if code != 2 {
		t.Fatalf("runFlowdoc(nil) exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage: vv flowdoc") {
		t.Errorf("expected usage on stderr, got: %q", stderr.String())
	}
}

func TestRunFlowdocUnknownVerb(t *testing.T) {
	stderr, restore := captureStderr(t)

	code := runFlowdoc([]string{"unknown"})
	restore()
	if code != 2 {
		t.Fatalf("runFlowdoc([unknown]) exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown flowdoc verb") {
		t.Errorf("expected unknown-verb error on stderr, got: %q", stderr.String())
	}
}

func TestRunFlowdocHelp(t *testing.T) {
	stderr, restore := captureStderr(t)

	code := runFlowdoc([]string{"--help"})
	restore()
	if code != 0 {
		t.Fatalf("runFlowdoc([--help]) exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "subcommands:") {
		t.Errorf("expected help text on stderr, got: %q", stderr.String())
	}
}

func TestParseFlowdocGenArgs_AllFlags(t *testing.T) {
	opts, err := parseFlowdocGenArgs([]string{"--project", "foo", "--model", "bar", "--open"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if opts.project != "foo" {
		t.Errorf("project = %q, want foo", opts.project)
	}
	if opts.model != "bar" {
		t.Errorf("model = %q, want bar", opts.model)
	}
	if !opts.open {
		t.Errorf("open = false, want true")
	}
}

func TestParseFlowdocGenArgs_EqualsForm(t *testing.T) {
	opts, err := parseFlowdocGenArgs([]string{"--project=alpha", "--model=beta"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if opts.project != "alpha" {
		t.Errorf("project = %q, want alpha", opts.project)
	}
	if opts.model != "beta" {
		t.Errorf("model = %q, want beta", opts.model)
	}
	if opts.open {
		t.Errorf("open = true, want false")
	}
}

func TestParseFlowdocGenArgs_UnknownFlag(t *testing.T) {
	_, err := parseFlowdocGenArgs([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("error = %q, want 'unknown flag'", err.Error())
	}
}

func TestParseFlowdocGenArgs_MissingValue(t *testing.T) {
	_, err := parseFlowdocGenArgs([]string{"--project"})
	if err == nil {
		t.Fatal("expected error for --project with no value")
	}
}

func TestParseFlowdocGenArgs_Empty(t *testing.T) {
	opts, err := parseFlowdocGenArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.project != "" || opts.model != "" || opts.open {
		t.Errorf("expected zero opts, got %+v", opts)
	}
}

func TestParseFlowdocGenArgs_Help(t *testing.T) {
	_, err := parseFlowdocGenArgs([]string{"--help"})
	if err == nil || err.Error() != "help requested" {
		t.Fatalf("expected 'help requested', got %v", err)
	}
}

func TestStripJSONFence(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"{}", "{}"},
		{"```json\n{}\n```", "{}"},
		{"```\n{}\n```", "{}"},
		{"  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
		{`{"a":1}`, `{"a":1}`},
	}
	for _, tc := range cases {
		got := stripJSONFence(tc.in)
		if got != tc.want {
			t.Errorf("stripJSONFence(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// minimalFlowDocJSON is a syntactically valid FlowDoc that passes
// flowdoc.Validate — used by happy-path tests to avoid spinning up the
// real LLM.
const minimalFlowDocJSON = `{
  "$schema_version": "1",
  "project": "test-project",
  "generated_at": "2026-05-13T16:00:00Z",
  "generator": "vv flowdoc gen",
  "nodes": [
    {"id": "cmd/vv", "label": "cmd/vv", "path": "cmd/vv/main.go", "language": "go", "layout_group": "entry", "kind": "binary"},
    {"id": "internal/mcp", "label": "mcp", "path": "internal/mcp/", "language": "go", "layout_group": "mcp", "kind": "subsystem"}
  ],
  "flows": [
    {
      "slug": "vv-mcp",
      "label": "vv mcp",
      "kind": "cli-verb",
      "description": "Starts the MCP server.",
      "entry_point": "vv mcp",
      "nodes": ["cmd/vv", "internal/mcp"],
      "steps": [{"from": "cmd/vv", "to": "internal/mcp", "op": "dispatch"}]
    }
  ]
}`

func TestRunFlowdocGen_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)

	// Make tmp a "project root" with a .git marker so DetectProjectRoot
	// stops here and writes doc/ into tmp.
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	fake := &fakeFlowdocProvider{content: minimalFlowDocJSON}
	restoreProvider := swapFlowdocProvider(t, func(cfg config.Config) (llm.Provider, error) {
		return fake, nil
	})
	defer restoreProvider()

	stdout, restoreOut := captureStdout(t)
	_, restoreErr := captureStderr(t)

	code := runFlowdocGen([]string{"--project", "test-project", "--model", "fake-model"})
	restoreOut()
	restoreErr()
	if code != 0 {
		t.Fatalf("runFlowdocGen exit code = %d, want 0", code)
	}

	jsonPath := filepath.Join(tmp, "doc", "flows.json")
	htmlPath := filepath.Join(tmp, "doc", "FLOWS.html")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("expected %s to exist: %v", jsonPath, err)
	}
	if _, err := os.Stat(htmlPath); err != nil {
		t.Fatalf("expected %s to exist: %v", htmlPath, err)
	}

	// Verify the written JSON round-trips through Validate (which the
	// gen path runs before writing — so this primarily proves the write
	// step didn't corrupt the document).
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc flowdoc.FlowDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("written flows.json does not parse: %v", err)
	}
	if err := flowdoc.Validate(&doc); err != nil {
		t.Fatalf("written flows.json fails Validate: %v", err)
	}

	// Sanity-check the model + system prompt were threaded through.
	if fake.lastReq.Model != "fake-model" {
		t.Errorf("provider received model %q, want fake-model", fake.lastReq.Model)
	}
	if fake.lastReq.System != flowdoc.Brief {
		t.Errorf("provider did not receive flowdoc.Brief as system prompt")
	}
	if fake.lastReq.MaxTokens != 16384 {
		t.Errorf("MaxTokens = %d, want 16384", fake.lastReq.MaxTokens)
	}
	if !fake.lastReq.JSONMode {
		t.Errorf("JSONMode = false, want true")
	}

	// Confirm stdout reports both written paths.
	out := stdout.String()
	if !strings.Contains(out, "flows.json") || !strings.Contains(out, "FLOWS.html") {
		t.Errorf("stdout missing written paths: %q", out)
	}
}

func TestRunFlowdocGen_FencedJSON(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	fenced := "```json\n" + minimalFlowDocJSON + "\n```"
	fake := &fakeFlowdocProvider{content: fenced}
	restore := swapFlowdocProvider(t, func(cfg config.Config) (llm.Provider, error) {
		return fake, nil
	})
	defer restore()

	_, restoreOut := captureStdout(t)
	_, restoreErr := captureStderr(t)

	code := runFlowdocGen([]string{"--project", "test-project"})
	restoreOut()
	restoreErr()
	if code != 0 {
		t.Fatalf("runFlowdocGen exit code = %d, want 0 (fence stripping should let valid JSON through)", code)
	}
}

func TestRunFlowdocGen_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	fake := &fakeFlowdocProvider{content: "not valid json at all"}
	swapFlowdocProvider(t, func(cfg config.Config) (llm.Provider, error) {
		return fake, nil
	})

	stderr, restoreErr := captureStderr(t)
	_, restoreOut := captureStdout(t)

	code := runFlowdocGen([]string{"--project", "test-project"})
	restoreOut()
	restoreErr()
	if code == 0 {
		t.Fatal("expected non-zero exit on invalid JSON response")
	}
	if !strings.Contains(stderr.String(), "parse") {
		t.Errorf("stderr missing parse error: %q", stderr.String())
	}
}

func TestRunFlowdocGen_ValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Parseable but fails Validate: empty nodes[].
	bad := `{"$schema_version":"1","project":"x","nodes":[],"flows":[]}`
	fake := &fakeFlowdocProvider{content: bad}
	swapFlowdocProvider(t, func(cfg config.Config) (llm.Provider, error) {
		return fake, nil
	})

	stderr, restoreErr := captureStderr(t)
	_, restoreOut := captureStdout(t)

	code := runFlowdocGen([]string{"--project", "x"})
	restoreOut()
	restoreErr()
	if code == 0 {
		t.Fatal("expected non-zero exit on Validate failure")
	}
	if !strings.Contains(stderr.String(), "validate") {
		t.Errorf("stderr missing validate error: %q", stderr.String())
	}
}

func TestRunFlowdocGen_UnknownFlag(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)

	stderr, restore := captureStderr(t)

	code := runFlowdocGen([]string{"--bogus"})
	restore()
	if code != 2 {
		t.Fatalf("runFlowdocGen exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Errorf("stderr missing unknown-flag error: %q", stderr.String())
	}
}

// --- vv flowdoc verify ---

func TestParseFlowdocVerifyArgs(t *testing.T) {
	if p, err := parseFlowdocVerifyArgs([]string{"--project", "foo"}); err != nil || p != "foo" {
		t.Errorf("--project foo => (%q, %v), want (foo, nil)", p, err)
	}
	if p, err := parseFlowdocVerifyArgs([]string{"--project=bar"}); err != nil || p != "bar" {
		t.Errorf("--project=bar => (%q, %v), want (bar, nil)", p, err)
	}
	if p, err := parseFlowdocVerifyArgs(nil); err != nil || p != "" {
		t.Errorf("nil => (%q, %v), want (\"\", nil)", p, err)
	}
	if _, err := parseFlowdocVerifyArgs([]string{"--bogus"}); err == nil {
		t.Error("expected error for unknown flag")
	}
	if _, err := parseFlowdocVerifyArgs([]string{"--project"}); err == nil {
		t.Error("expected error for --project with no value")
	}
	if _, err := parseFlowdocVerifyArgs([]string{"--help"}); err == nil || err.Error() != "help requested" {
		t.Errorf("--help => %v, want 'help requested'", err)
	}
}

func TestRunFlowdocVerify_Help(t *testing.T) {
	stderr, restore := captureStderr(t)
	code := runFlowdocVerify([]string{"--help"})
	restore()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "usage: vv flowdoc verify") {
		t.Errorf("expected usage on stderr, got %q", stderr.String())
	}
}

func TestRunFlowdocVerify_UnknownFlag(t *testing.T) {
	stderr, restore := captureStderr(t)
	code := runFlowdocVerify([]string{"--bogus"})
	restore()
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Errorf("expected unknown-flag error on stderr, got %q", stderr.String())
	}
}

func TestRunFlowdocVerify_NoFlowsJSON(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	stderr, restoreErr := captureStderr(t)
	_, restoreOut := captureStdout(t)
	code := runFlowdocVerify(nil)
	restoreOut()
	restoreErr()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no doc/flows.json found") {
		t.Errorf("expected 'no doc/flows.json found' on stderr, got %q", stderr.String())
	}
}

func TestRunFlowdocVerify_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "doc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "doc", "flows.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr, restoreErr := captureStderr(t)
	_, restoreOut := captureStdout(t)
	code := runFlowdocVerify(nil)
	restoreOut()
	restoreErr()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "parse") {
		t.Errorf("expected parse error on stderr, got %q", stderr.String())
	}
}

func TestRunFlowdocVerify_ValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "doc"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Parseable but fails Validate: empty nodes[].
	bad := `{"$schema_version":"1","project":"x","nodes":[],"flows":[]}`
	if err := os.WriteFile(filepath.Join(tmp, "doc", "flows.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr, restoreErr := captureStderr(t)
	_, restoreOut := captureStdout(t)
	code := runFlowdocVerify(nil)
	restoreOut()
	restoreErr()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "nodes must be non-empty") {
		t.Errorf("expected validate error on stderr, got %q", stderr.String())
	}
}

// goldenFlowsJSONPath walks up from the test cwd to the repo root and
// returns the path to the flowdoc golden fixture.
func goldenFlowsJSONPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "internal", "flowdoc", "testdata", "golden", "vibe-vault-flows.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate golden fixture walking up from test cwd")
		}
		dir = parent
	}
}

func TestRunFlowdocVerify_CleanGolden(t *testing.T) {
	// The golden fixture's relative paths are anchored at the repo root,
	// so the temp "project root" must BE the repo root for refs to
	// resolve. Copy the golden into <repoRoot>/doc/flows.json is unsafe
	// (pollutes the tree); instead chdir into the repo root and ensure
	// doc/flows.json exists there for the duration of the test, restoring
	// any pre-existing file afterward.
	golden := goldenFlowsJSONPath(t)
	// Walk up from the golden file to the module root (the dir with go.mod);
	// that is the repo root the golden's relative paths are anchored at.
	repoRoot := filepath.Dir(golden)
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			t.Fatal("could not find go.mod walking up from golden fixture")
		}
		repoRoot = parent
	}

	docDir := filepath.Join(repoRoot, "doc")
	flowsPath := filepath.Join(docDir, "flows.json")

	// Preserve any existing doc/flows.json and doc/ dir state.
	var savedContent []byte
	hadFile := false
	if b, err := os.ReadFile(flowsPath); err == nil {
		savedContent = b
		hadFile = true
	}
	hadDocDir := true
	if _, err := os.Stat(docDir); err != nil {
		hadDocDir = false
	}
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flowsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadFile {
			_ = os.WriteFile(flowsPath, savedContent, 0o644)
		} else {
			_ = os.Remove(flowsPath)
			if !hadDocDir {
				_ = os.Remove(docDir)
			}
		}
	})

	withChdir(t, repoRoot)

	stdout, restoreOut := captureStdout(t)
	stderr, restoreErr := captureStderr(t)
	code := runFlowdocVerify(nil)
	restoreOut()
	restoreErr()

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (clean golden); stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	// The golden has weak-match warnings, so output is the warnings
	// report rather than the "no drift" line. Either way, no errors.
	out := stdout.String()
	if strings.Contains(out, "errors (") {
		t.Errorf("clean golden produced error lines: %q", out)
	}
}

func TestRunFlowdocVerify_DriftDetected(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "doc"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid structurally, but a node points at a path that does not
	// exist under the (empty) temp project root.
	drift := `{
  "$schema_version": "1",
  "project": "drift-test",
  "nodes": [
    {"id": "ghost", "label": "ghost", "path": "internal/not/here.go", "language": "go", "layout_group": "g", "kind": "subsystem"}
  ],
  "flows": []
}`
	if err := os.WriteFile(filepath.Join(tmp, "doc", "flows.json"), []byte(drift), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, restoreOut := captureStdout(t)
	_, restoreErr := captureStderr(t)
	code := runFlowdocVerify(nil)
	restoreOut()
	restoreErr()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (drift detected)", code)
	}
	if !strings.Contains(stdout.String(), "missing-file") {
		t.Errorf("expected missing-file in report, got %q", stdout.String())
	}
}

func TestRunFlowdocVerify_CleanNoDrift(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "doc"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A node whose path actually exists in the temp tree, no flows/refs:
	// VerifyRefs returns zero issues, so verify prints the summary line.
	if err := os.MkdirAll(filepath.Join(tmp, "internal", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	clean := `{
  "$schema_version": "1",
  "project": "clean-test",
  "nodes": [
    {"id": "real", "label": "real", "path": "internal/real/", "language": "go", "layout_group": "g", "kind": "subsystem"}
  ],
  "flows": []
}`
	if err := os.WriteFile(filepath.Join(tmp, "doc", "flows.json"), []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, restoreOut := captureStdout(t)
	_, restoreErr := captureStderr(t)
	code := runFlowdocVerify(nil)
	restoreOut()
	restoreErr()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "no drift") {
		t.Errorf("expected 'no drift' summary, got %q", stdout.String())
	}
}

func TestDefaultFlowdocModel(t *testing.T) {
	if defaultFlowdocModel != "grok-4-fast" {
		t.Errorf("defaultFlowdocModel = %q, want grok-4-fast", defaultFlowdocModel)
	}
}

func TestOpenInBrowser_DoesNotPanic(t *testing.T) {
	// Sanity: with an empty path on a non-resolvable command, Start
	// must not panic (we explicitly ignore the error).
	prev := execCommandForOpen
	execCommandForOpen = func(name string, args ...string) *exec.Cmd {
		// Point at a binary guaranteed to fail to start to exercise the
		// error-swallow branch.
		return exec.Command("/this/does/not/exist")
	}
	defer func() { execCommandForOpen = prev }()
	openInBrowser("/tmp/anything")
}

// --- test helpers ---

// swapFlowdocProvider replaces newProviderForFlowdoc for the duration of
// a test, returning a restore func. Cleanup is also wired via t.Cleanup
// for safety against early returns.
func swapFlowdocProvider(t *testing.T, fn func(config.Config) (llm.Provider, error)) func() {
	t.Helper()
	prev := newProviderForFlowdoc
	newProviderForFlowdoc = fn
	restore := func() { newProviderForFlowdoc = prev }
	t.Cleanup(restore)
	return restore
}

// withChdir cd's into dir for the duration of the test, restoring the
// original cwd in cleanup. Fatal on either chdir failure.
func withChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// captureStdout / captureStderr redirect the named stream into a
// bytes.Buffer for the duration of the test. The returned restore func
// must be called (a deferred t.Cleanup also fires defensively).
func captureStdout(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdout
	os.Stdout = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	var once sync.Once
	restore := func() {
		once.Do(func() {
			_ = w.Close()
			<-done
			os.Stdout = prev
		})
	}
	t.Cleanup(restore)
	return buf, restore
}

func captureStderr(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	var once sync.Once
	restore := func() {
		once.Do(func() {
			_ = w.Close()
			<-done
			os.Stderr = prev
		})
	}
	t.Cleanup(restore)
	return buf, restore
}
