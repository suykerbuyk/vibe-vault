// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// Direction-C Phase 4 retired the only embedded agent (wrap-executor)
// when the wrap-bundle pipeline retired. The vv_get_agent_definition
// tool itself is preserved as v2-portability scaffolding; tests now
// exercise the empty-registry behavior. Re-introduce happy-path /
// round-trip tests when future agents land.

func TestVVGetAgentDefinition_NotFound(t *testing.T) {
	tool := NewGetAgentDefinitionTool()
	params, _ := json.Marshal(map[string]string{"name": "does-not-exist"})

	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for unknown agent name")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should mention the missing name: %v", err)
	}
}

func TestVVGetAgentDefinition_MissingName(t *testing.T) {
	tool := NewGetAgentDefinitionTool()
	params, _ := json.Marshal(map[string]string{})

	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for missing name argument")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}
