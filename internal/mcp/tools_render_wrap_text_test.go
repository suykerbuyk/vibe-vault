// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderWrapText_DeprecationShimErrors(t *testing.T) {
	tool := NewRenderWrapTextTool()
	_, err := tool.Handler(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from deprecation shim, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"retired", "DESIGN #104", "vv context sync"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q; got: %s", want, msg)
		}
	}
}
