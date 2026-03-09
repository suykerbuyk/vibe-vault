// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import "testing"

func TestFilterBySource(t *testing.T) {
	entries := map[string]SessionEntry{
		"cc1": {SessionID: "cc1", Source: ""},
		"cc2": {SessionID: "cc2", Source: ""},
		"z1":  {SessionID: "z1", Source: "zed"},
		"z2":  {SessionID: "z2", Source: "zed"},
		"z3":  {SessionID: "z3", Source: "zed"},
	}

	tests := []struct {
		name   string
		source string
		want   int
	}{
		{"empty returns all", "", 5},
		{"all returns all", "all", 5},
		{"zed filters to zed", "zed", 3},
		{"claude-code filters to claude-code", "claude-code", 2},
		{"unknown source returns empty", "cursor", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterBySource(entries, tt.source)
			if len(got) != tt.want {
				t.Errorf("FilterBySource(%q) returned %d entries, want %d", tt.source, len(got), tt.want)
			}
		})
	}
}

func TestFilterBySource_Nil(t *testing.T) {
	got := FilterBySource(nil, "zed")
	if len(got) != 0 {
		t.Errorf("FilterBySource(nil) returned %d entries, want 0", len(got))
	}
}

func TestFilterBySource_PreservesEntries(t *testing.T) {
	entries := map[string]SessionEntry{
		"z1": {SessionID: "z1", Source: "zed", Project: "myproj"},
	}
	got := FilterBySource(entries, "zed")
	if e, ok := got["z1"]; !ok || e.Project != "myproj" {
		t.Error("FilterBySource should preserve entry data")
	}
}
