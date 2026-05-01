// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// GoldenEntry captures the contract for one tool: its name and the
// top-level "required" array from its InputSchema. Detecting a change
// to required-input set triggers a surface-version bump requirement.
type GoldenEntry struct {
	Name           string   `json:"name"`
	RequiredInputs []string `json:"required_inputs"`
}

// GoldenManifest is the on-disk shape of internal/mcp/tool_surface.golden.json.
type GoldenManifest struct {
	SurfaceVersion int           `json:"surface_version"`
	Tools          []GoldenEntry `json:"tools"`
}

// LiveManifest builds the manifest from the currently-registered tools.
// surfaceVersion is passed in (caller supplies surface.MCPSurfaceVersion).
func LiveManifest(srv *Server, surfaceVersion int) GoldenManifest {
	defs := srv.ToolDefs()
	tools := make([]GoldenEntry, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, GoldenEntry{
			Name:           d.Name,
			RequiredInputs: extractRequired(d.InputSchema),
		})
	}
	return GoldenManifest{SurfaceVersion: surfaceVersion, Tools: tools}
}

// extractRequired parses the top-level "required" array from a tool's
// InputSchema. Returns empty slice if absent or unparseable.
func extractRequired(rawSchema json.RawMessage) []string {
	if len(rawSchema) == 0 {
		return nil
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return nil
	}
	sort.Strings(schema.Required)
	return schema.Required
}

// LoadGolden reads the golden manifest from path. Returns zero-value on
// missing file (no error — bootstrap case).
func LoadGolden(path string) (GoldenManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return GoldenManifest{}, nil
		}
		return GoldenManifest{}, fmt.Errorf("read golden: %w", err)
	}
	var m GoldenManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return GoldenManifest{}, fmt.Errorf("parse golden: %w", err)
	}
	return m, nil
}

// DiffResult enumerates the differences between live and golden.
type DiffResult struct {
	Added             []string               // names in live not in golden
	Removed           []string               // names in golden not in live
	RequiredInputDiff map[string][2][]string // name → [oldRequired, newRequired]
}

// Empty reports whether the diff has no added, removed, or required-changed entries.
func (d DiffResult) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.RequiredInputDiff) == 0
}

// Diff compares live to golden and produces a DiffResult.
func Diff(live, golden GoldenManifest) DiffResult {
	out := DiffResult{RequiredInputDiff: map[string][2][]string{}}
	goldByName := map[string]GoldenEntry{}
	for _, t := range golden.Tools {
		goldByName[t.Name] = t
	}
	liveByName := map[string]GoldenEntry{}
	for _, t := range live.Tools {
		liveByName[t.Name] = t
		if g, ok := goldByName[t.Name]; ok {
			if !equalStringSlice(g.RequiredInputs, t.RequiredInputs) {
				out.RequiredInputDiff[t.Name] = [2][]string{g.RequiredInputs, t.RequiredInputs}
			}
		} else {
			out.Added = append(out.Added, t.Name)
		}
	}
	for _, t := range golden.Tools {
		if _, ok := liveByName[t.Name]; !ok {
			out.Removed = append(out.Removed, t.Name)
		}
	}
	sort.Strings(out.Added)
	sort.Strings(out.Removed)
	return out
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// WriteGolden serializes the manifest to path as pretty-printed JSON.
func WriteGolden(path string, m GoldenManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal golden: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write golden: %w", err)
	}
	return nil
}

// FormatDriftError builds the human-readable error message for a non-empty
// diff that lacks a sufficient surface-version bump.
func FormatDriftError(diff DiffResult, binarySurface, goldenSurface int) string {
	var b strings.Builder
	b.WriteString("vv internal verify-tool-surface: surface drift requires version bump\n")
	for _, n := range diff.Added {
		fmt.Fprintf(&b, "    added: %s\n", n)
	}
	for _, n := range diff.Removed {
		fmt.Fprintf(&b, "    removed: %s\n", n)
	}
	// Required-changed entries: emit in stable name order so the message is
	// deterministic across runs (map iteration order is not).
	changedNames := make([]string, 0, len(diff.RequiredInputDiff))
	for name := range diff.RequiredInputDiff {
		changedNames = append(changedNames, name)
	}
	sort.Strings(changedNames)
	for _, name := range changedNames {
		pair := diff.RequiredInputDiff[name]
		fmt.Fprintf(&b, "    required-changed: %s\n        old: %v\n        new: %v\n",
			name, pair[0], pair[1])
	}
	fmt.Fprintf(&b, "    binary surface: %d\n    golden surface: %d\n", binarySurface, goldenSurface)
	b.WriteString("    action: bump internal/surface/version.go's MCPSurfaceVersion to ")
	fmt.Fprintf(&b, "%d, rebuild (make build), then re-run with --update-golden\n", goldenSurface+1)
	return b.String()
}
