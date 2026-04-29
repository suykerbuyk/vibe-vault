// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
)

// Direction-C Phase 4 retired the only embedded agent (wrap-executor)
// when the wrap-bundle pipeline retired. The generator code is kept as
// v2-portability scaffolding but has nothing to emit until future agents
// are registered. The tests below construct synthetic AgentDefinitions
// in-place so the generator's idempotency / banner contracts remain
// regression-locked without depending on any specific embedded file.

func sampleGeneratorDefs() []*agentregistry.AgentDefinition {
	return []*agentregistry.AgentDefinition{
		{
			Name:                  "example-agent",
			Version:               "1.0",
			Description:           "example for generator regression",
			SystemPrompt:          "You are an example agent body.\n",
			RequiredTools:         []string{"tool_a", "tool_b"},
			ForbiddenTools:        []string{"Bash"},
			EscalationTriggers:    []string{"trigger_one", "trigger_two"},
			OutputFormat:          "terminal: example()",
			RecommendedModelClass: "sonnet",
		},
	}
}

func TestGenerator_Idempotent(t *testing.T) {
	dir := t.TempDir()
	defs := sampleGeneratorDefs()

	// First pass.
	count1, err := generateAgentsTo(dir, defs)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if count1 != len(defs) {
		t.Errorf("first pass: wrote %d files, expected %d", count1, len(defs))
	}

	// Snapshot the contents of every file for byte-equality comparison.
	first := readAllFiles(t, dir)

	// Second pass — no changes between calls.
	count2, err := generateAgentsTo(dir, defs)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if count2 != count1 {
		t.Errorf("second pass wrote %d files, first wrote %d", count2, count1)
	}
	second := readAllFiles(t, dir)

	if len(first) != len(second) {
		t.Fatalf("file count differs across runs: %d vs %d", len(first), len(second))
	}
	for path, want := range first {
		got, ok := second[path]
		if !ok {
			t.Errorf("file %s present in first run but missing in second", path)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("file %s differs between runs:\nfirst:\n%s\nsecond:\n%s", path, want, got)
		}
	}
}

func TestGenerator_ContainsBanner(t *testing.T) {
	dir := t.TempDir()
	defs := sampleGeneratorDefs()

	if _, err := generateAgentsTo(dir, defs); err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, def := range defs {
		path := filepath.Join(dir, def.Name+".md")
		data, err := os.ReadFile(path) //nolint:gosec // Test-controlled path
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		if !strings.HasPrefix(text, "<!-- AUTO-GENERATED") {
			t.Errorf("%s: missing AUTO-GENERATED banner; first line: %q",
				path, firstLine(text))
		}
		if !strings.Contains(text, "v2-portability artifact") {
			t.Errorf("%s: banner does not mention v2-portability", path)
		}
		if !strings.Contains(text, "name: "+def.Name) {
			t.Errorf("%s: frontmatter missing name key", path)
		}
	}
}

// readAllFiles returns a map of relative path -> file bytes for all regular
// files under dir.
func readAllFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // Test-controlled walk
		if readErr != nil {
			return readErr
		}
		out[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}
