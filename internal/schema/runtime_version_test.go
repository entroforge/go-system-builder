package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRuntimeSchemaSelectsClosedVersion(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, version                 string
		optimization, unknown, accept bool
	}{
		{"legacy", "1.1.0", false, false, true},
		{"new empty", "1.2.0", false, false, true},
		{"new enabled", "1.2.0", true, false, true},
		{"unified empty", "1.3.0", false, false, true},
		{"unified enabled", "1.3.0", true, false, true},
		{"legacy cannot enable", "1.1.0", true, false, false},
		{"new remains closed", "1.2.0", true, true, false},
		{"unified remains closed", "1.3.0", true, true, false},
		{"unknown", "9.0.0", false, false, false},
		{"missing", "", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var state map[string]any
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			state["schema_version"] = tc.version
			if tc.version == "" {
				delete(state, "schema_version")
			}
			if tc.optimization {
				state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
				state["entities"].(map[string]any)["work_packets"] = []any{}
				state["entities"].(map[string]any)["submissions"] = []any{}
			}
			if tc.unknown {
				state["entities"].(map[string]any)["unregistered_index"] = []any{}
			}
			instance, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			err = schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", instance)
			if (err == nil) != tc.accept {
				t.Fatalf("accept=%v, validation error: %v", tc.accept, err)
			}
		})
	}
}

func TestUnifiedRuntimeRequiresFiveStateActivityFields(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.3.0"
	entities := state["entities"].(map[string]any)
	entities["agents"] = []any{map[string]any{
		"id": "agent-1", "role": "backend-builder", "state": "active", "last_lifecycle_event": "work_started",
		"task_ids": []any{"TASK-001"}, "team_id": "team-1", "definition_ref": "agents/backend-builder.md",
		"prompt_ref": "docs/teams/team-1/manifest.json#assignment-1", "readback_ref": nil, "activation_ref": nil,
		"activation_revision": nil, "updated_at": "2026-10-15T00:00:00Z",
	}}
	entities["tasks"] = []any{map[string]any{
		"id": "TASK-001", "state": "pending", "last_lifecycle_event": "task_registered", "path": "docs/dev/tasks/TASK-001.md",
		"sha256": strings.Repeat("a", 64), "owner_agent_ids": []any{"agent-1"},
	}}
	instance, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateBytes("loop-state.schema.json", instance); err != nil {
		t.Fatalf("valid unified Runtime rejected: %v", err)
	}

	delete(entities["agents"].([]any)[0].(map[string]any), "last_lifecycle_event")
	instance, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("loop-state.schema.json", instance); err == nil {
		t.Fatal("unified Agent without its original lifecycle event was accepted")
	}

	entities["agents"].([]any)[0].(map[string]any)["last_lifecycle_event"] = "work_started"
	delete(entities["tasks"].([]any)[0].(map[string]any), "last_lifecycle_event")
	instance, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("loop-state.schema.json", instance); err == nil {
		t.Fatal("unified TASK without its original lifecycle event was accepted")
	}

	entities["tasks"].([]any)[0].(map[string]any)["last_lifecycle_event"] = "task_registered"
	entities["agents"].([]any)[0].(map[string]any)["state"] = "working"
	instance, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("loop-state.schema.json", instance); err == nil {
		t.Fatal("legacy Agent activity was accepted in unified Runtime")
	}
}

func TestLegacySchemaCannotAcceptNewRuntime(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	instance, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state-v1.1.schema.json", instance); err == nil {
		t.Fatal("a v1.1 writer must reject a v1.2 instance")
	}
}
