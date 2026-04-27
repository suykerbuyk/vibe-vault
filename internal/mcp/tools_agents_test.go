// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
)

func TestVVGetAgentDefinition_HappyPath(t *testing.T) {
	tool := NewGetAgentDefinitionTool()
	params, _ := json.Marshal(map[string]string{"name": "wrap-executor"})

	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got agentregistry.AgentDefinition
	if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", jsonErr, out)
	}
	if got.Name != "wrap-executor" {
		t.Errorf("name: got %q want %q", got.Name, "wrap-executor")
	}
	if got.RecommendedModelClass != "sonnet" {
		t.Errorf("recommended_model_class: got %q want %q", got.RecommendedModelClass, "sonnet")
	}
	if len(got.RequiredTools) != 2 {
		t.Errorf("required_tools should have 2 entries, got %d", len(got.RequiredTools))
	}
	if len(got.EscalationTriggers) != 6 {
		t.Errorf("escalation_triggers should have 6 entries, got %d", len(got.EscalationTriggers))
	}
	if got.SystemPrompt == "" {
		t.Error("system_prompt must be populated")
	}
	if got.Sha256 == "" || len(got.Sha256) != 64 {
		t.Errorf("sha256 not populated correctly: %q", got.Sha256)
	}
}

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

func TestVVGetAgentDefinition_RoundTrip(t *testing.T) {
	tool := NewGetAgentDefinitionTool()
	params, _ := json.Marshal(map[string]string{"name": "wrap-executor"})

	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Deserialize -> re-serialize -> deserialize again. Final value must
	// equal the first deserialization.
	var first agentregistry.AgentDefinition
	if jsonErr := json.Unmarshal([]byte(out), &first); jsonErr != nil {
		t.Fatalf("first unmarshal: %v", jsonErr)
	}
	reEncoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var second agentregistry.AgentDefinition
	if jsonErr := json.Unmarshal(reEncoded, &second); jsonErr != nil {
		t.Fatalf("second unmarshal: %v", jsonErr)
	}
	if first.Name != second.Name ||
		first.Version != second.Version ||
		first.Description != second.Description ||
		first.SystemPrompt != second.SystemPrompt ||
		first.OutputFormat != second.OutputFormat ||
		first.RecommendedModelClass != second.RecommendedModelClass ||
		first.Sha256 != second.Sha256 {
		t.Errorf("scalar fields drifted across round-trip:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	if !equalSlices(first.RequiredTools, second.RequiredTools) {
		t.Errorf("required_tools drifted: %v vs %v", first.RequiredTools, second.RequiredTools)
	}
	if !equalSlices(first.ForbiddenTools, second.ForbiddenTools) {
		t.Errorf("forbidden_tools drifted: %v vs %v", first.ForbiddenTools, second.ForbiddenTools)
	}
	if !equalSlices(first.EscalationTriggers, second.EscalationTriggers) {
		t.Errorf("escalation_triggers drifted: %v vs %v", first.EscalationTriggers, second.EscalationTriggers)
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

func equalSlices(a, b []string) bool {
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
