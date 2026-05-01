// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// newTestServerForVerify builds a Server with a few synthetic tools whose
// InputSchemas exercise the required-extraction code paths.
func newTestServerForVerify(t *testing.T) *Server {
	t.Helper()
	srv := NewServer(ServerInfo{Name: "verify-test", Version: "0.0.0"}, log.New(io.Discard, "", 0))
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "tool_alpha",
			Description: "alpha",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`),
		},
	})
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "tool_beta",
			Description: "beta",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"},"y":{"type":"string"}},"required":["y","x"]}`),
		},
	})
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "tool_gamma",
			Description: "gamma",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"opt":{"type":"string"}}}`),
		},
	})
	return srv
}

func TestLiveManifest_PicksUpAllTools(t *testing.T) {
	srv := newTestServerForVerify(t)
	m := LiveManifest(srv, 42)

	if m.SurfaceVersion != 42 {
		t.Errorf("SurfaceVersion = %d, want 42", m.SurfaceVersion)
	}
	if len(m.Tools) != 3 {
		t.Fatalf("len(Tools) = %d, want 3", len(m.Tools))
	}

	// Stable alphabetical order.
	wantNames := []string{"tool_alpha", "tool_beta", "tool_gamma"}
	for i, want := range wantNames {
		if m.Tools[i].Name != want {
			t.Errorf("Tools[%d].Name = %q, want %q", i, m.Tools[i].Name, want)
		}
	}

	// tool_alpha required = ["a"]
	if !reflect.DeepEqual(m.Tools[0].RequiredInputs, []string{"a"}) {
		t.Errorf("tool_alpha required = %v, want [a]", m.Tools[0].RequiredInputs)
	}
	// tool_beta required = ["x","y"] after sort
	if !reflect.DeepEqual(m.Tools[1].RequiredInputs, []string{"x", "y"}) {
		t.Errorf("tool_beta required = %v, want [x y]", m.Tools[1].RequiredInputs)
	}
	// tool_gamma has no required key — empty/nil slice
	if len(m.Tools[2].RequiredInputs) != 0 {
		t.Errorf("tool_gamma required = %v, want empty", m.Tools[2].RequiredInputs)
	}
}

func TestExtractRequired_HappyPath(t *testing.T) {
	got := extractRequired(json.RawMessage(`{"required":["b","a","c"]}`))
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractRequired_NoSchema(t *testing.T) {
	got := extractRequired(nil)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	got = extractRequired(json.RawMessage(``))
	if got != nil {
		t.Errorf("empty raw: got %v, want nil", got)
	}
}

func TestExtractRequired_NoRequiredKey(t *testing.T) {
	got := extractRequired(json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`))
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestExtractRequired_MalformedJSON(t *testing.T) {
	got := extractRequired(json.RawMessage(`{not-json`))
	if got != nil {
		t.Errorf("malformed json: got %v, want nil", got)
	}
}

func TestLoadGolden_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.json")
	m, err := LoadGolden(path)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if m.SurfaceVersion != 0 || len(m.Tools) != 0 {
		t.Errorf("got %+v, want zero", m)
	}
}

func TestLoadGolden_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not-json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGolden(path); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadGolden_ReadError(t *testing.T) {
	// Pass a directory as the path — os.ReadFile returns a non-not-exist error.
	dir := t.TempDir()
	if _, err := LoadGolden(dir); err == nil {
		t.Fatal("expected read error, got nil")
	}
}

func TestDiff_Empty(t *testing.T) {
	m := GoldenManifest{
		SurfaceVersion: 1,
		Tools: []GoldenEntry{
			{Name: "a", RequiredInputs: []string{"x"}},
			{Name: "b", RequiredInputs: nil},
		},
	}
	d := Diff(m, m)
	if !d.Empty() {
		t.Errorf("expected empty diff, got %+v", d)
	}
}

func TestDiff_Added(t *testing.T) {
	live := GoldenManifest{Tools: []GoldenEntry{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}}
	golden := GoldenManifest{Tools: []GoldenEntry{{Name: "a"}}}
	d := Diff(live, golden)
	if !reflect.DeepEqual(d.Added, []string{"b", "c"}) {
		t.Errorf("Added = %v, want [b c]", d.Added)
	}
	if len(d.Removed) != 0 || len(d.RequiredInputDiff) != 0 {
		t.Errorf("unexpected other fields: %+v", d)
	}
}

func TestDiff_Removed(t *testing.T) {
	live := GoldenManifest{Tools: []GoldenEntry{{Name: "a"}}}
	golden := GoldenManifest{Tools: []GoldenEntry{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}}
	d := Diff(live, golden)
	if !reflect.DeepEqual(d.Removed, []string{"b", "c"}) {
		t.Errorf("Removed = %v, want [b c]", d.Removed)
	}
	if len(d.Added) != 0 || len(d.RequiredInputDiff) != 0 {
		t.Errorf("unexpected other fields: %+v", d)
	}
}

func TestDiff_RequiredChanged(t *testing.T) {
	live := GoldenManifest{Tools: []GoldenEntry{
		{Name: "a", RequiredInputs: []string{"x", "y"}},
	}}
	golden := GoldenManifest{Tools: []GoldenEntry{
		{Name: "a", RequiredInputs: []string{"x"}},
	}}
	d := Diff(live, golden)
	if len(d.RequiredInputDiff) != 1 {
		t.Fatalf("RequiredInputDiff len = %d, want 1", len(d.RequiredInputDiff))
	}
	pair := d.RequiredInputDiff["a"]
	if !reflect.DeepEqual(pair[0], []string{"x"}) || !reflect.DeepEqual(pair[1], []string{"x", "y"}) {
		t.Errorf("pair = %v, want [[x] [x y]]", pair)
	}
}

func TestDiff_OrderInsensitive(t *testing.T) {
	// extractRequired normalises by sorting, so the canonical form is
	// already sorted before Diff sees it. Verify same-set-different-input-order
	// produces no diff after extraction.
	srv1 := NewServer(ServerInfo{Name: "t", Version: "0"}, log.New(io.Discard, "", 0))
	srv1.RegisterTool(Tool{Definition: ToolDef{
		Name:        "t1",
		InputSchema: json.RawMessage(`{"required":["a","b"]}`),
	}})

	srv2 := NewServer(ServerInfo{Name: "t", Version: "0"}, log.New(io.Discard, "", 0))
	srv2.RegisterTool(Tool{Definition: ToolDef{
		Name:        "t1",
		InputSchema: json.RawMessage(`{"required":["b","a"]}`),
	}})

	m1 := LiveManifest(srv1, 1)
	m2 := LiveManifest(srv2, 1)
	d := Diff(m1, m2)
	if !d.Empty() {
		t.Errorf("expected empty diff for reordered required arrays, got %+v", d)
	}
}

func TestWriteGolden_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golden.json")
	want := GoldenManifest{
		SurfaceVersion: 7,
		Tools: []GoldenEntry{
			{Name: "alpha", RequiredInputs: []string{"a", "b"}},
			{Name: "beta", RequiredInputs: nil},
		},
	}
	if err := WriteGolden(path, want); err != nil {
		t.Fatalf("WriteGolden: %v", err)
	}

	got, err := LoadGolden(path)
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}
	if got.SurfaceVersion != want.SurfaceVersion {
		t.Errorf("SurfaceVersion = %d, want %d", got.SurfaceVersion, want.SurfaceVersion)
	}
	if len(got.Tools) != len(want.Tools) {
		t.Fatalf("len(Tools) = %d, want %d", len(got.Tools), len(want.Tools))
	}
	for i := range want.Tools {
		if got.Tools[i].Name != want.Tools[i].Name {
			t.Errorf("Tools[%d].Name = %q, want %q", i, got.Tools[i].Name, want.Tools[i].Name)
		}
		// nil and []string{} both serialize sensibly; compare by length for the
		// nil-required case.
		if len(got.Tools[i].RequiredInputs) != len(want.Tools[i].RequiredInputs) {
			t.Errorf("Tools[%d].RequiredInputs len = %d, want %d", i,
				len(got.Tools[i].RequiredInputs), len(want.Tools[i].RequiredInputs))
		}
	}
}

func TestWriteGolden_TrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golden.json")
	if err := WriteGolden(path, GoldenManifest{SurfaceVersion: 1}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("expected trailing newline")
	}
}

func TestFormatDriftError_ContainsAll(t *testing.T) {
	d := DiffResult{
		Added:   []string{"new_tool"},
		Removed: []string{"gone_tool"},
		RequiredInputDiff: map[string][2][]string{
			"changed_tool": {[]string{"a"}, []string{"a", "b"}},
		},
	}
	msg := FormatDriftError(d, 11, 11)

	for _, want := range []string{
		"surface drift requires version bump",
		"added: new_tool",
		"removed: gone_tool",
		"required-changed: changed_tool",
		"binary surface: 11",
		"golden surface: 11",
		"bump internal/surface/version.go",
		"--update-golden",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\nfull message:\n%s", want, msg)
		}
	}
}

func TestFormatDriftError_BumpHint(t *testing.T) {
	// Bump hint text should suggest goldenSurface+1.
	msg := FormatDriftError(DiffResult{Added: []string{"x"}}, 5, 7)
	if !strings.Contains(msg, "MCPSurfaceVersion to 8") {
		t.Errorf("expected bump suggestion to 8, got:\n%s", msg)
	}
}

func TestEqualStringSlice(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, []string{}, true},
		{nil, []string{}, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a"}, []string{"b"}, false},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
	}
	for i, tc := range cases {
		got := equalStringSlice(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("case %d: equalStringSlice(%v, %v) = %v, want %v", i, tc.a, tc.b, got, tc.want)
		}
	}
}
