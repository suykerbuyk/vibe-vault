// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import "testing"

func TestSourceName(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"empty defaults to claude-code", "", "claude-code"},
		{"zed source", "zed", "zed"},
		{"custom source", "custom-tool", "custom-tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := SessionEntry{Source: tt.source}
			if got := e.SourceName(); got != tt.want {
				t.Errorf("SourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}
