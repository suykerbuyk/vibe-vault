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
