package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestRuntimeSemanticCoreRequiresMatchingUnifiedS7AssignmentEvent(t *testing.T) {
	root := t.TempDir()
	definitionPath := filepath.Join(root, "docs", "control", "loop-definition.json")
	if err := os.MkdirAll(filepath.Dir(definitionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	definition := []byte(`{"states":{"inactive":{"phase_machine":null}},"phase_machines":{},"entity_lifecycles":{"agent":{"states":[]},"task":{"states":[]},"bug":{"states":[]}}}`)
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"schema_version": workstate.UnifiedVersion,
		"lifecycle":      map[string]any{"state": "inactive", "phase": nil},
		"entities":       map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}},
		"review": map[string]any{
			"assignments": map[string]any{
				"assignment-qa-1": map[string]any{
					"status":               "pending",
					"last_lifecycle_event": "review_plan_registered",
				},
			},
		},
	}
	if err := validateRuntimeSemanticCore(root, state); err != nil {
		t.Fatalf("matching S7 Assignment event rejected: %v", err)
	}

	review := state["review"].(map[string]any)
	originalAssignments := review["assignments"]
	delete(review, "assignments")
	if err := validateRuntimeSemanticCore(root, state); err != nil {
		t.Fatalf("unopened S7 round rejected: %v", err)
	}
	review["assignments"] = originalAssignments
	row := state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	row["status"] = "cancelled"
	if err := validateRuntimeSemanticCore(root, state); err == nil {
		t.Fatal("cancelled S7 row without original cancellation event accepted")
	}
	row["status"] = "active"
	if err := validateRuntimeSemanticCore(root, state); err == nil {
		t.Fatal("mismatched S7 Assignment status/event was accepted")
	}
}
