package cli

import (
	"encoding/json"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Reuse only the existing observation of this exact Runtime revision. A
// changed Runtime or a missing observation still goes through the Controller.
func s6ObservedControlForHook(root string, request policy.Input) (controller.ControlResult, bool) {
	if request.Event != "PreToolUse" {
		return controller.ControlResult{}, false
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return controller.ControlResult{}, false
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	milestone, _ := snapshot.State["milestone"].(map[string]any)
	if lifecycle["state"] != "building" || milestone["stage"] != "S6" || milestone["lifecycle_state"] != "building" ||
		stringValue(lifecycle["phase"]) != stringValue(milestone["lifecycle_phase"]) ||
		milestone["source_revision"] == nil || integerValue(milestone["source_revision"]) != snapshot.Revision {
		return controller.ControlResult{}, false
	}
	var gate controller.QualityGateResult
	gateBytes, err := json.Marshal(milestone["quality_gate"])
	if err != nil || json.Unmarshal(gateBytes, &gate) != nil || gate.Status == "" || gate.NextCursor != "building" ||
		(gate.GateID != "GATE-BUILDER-BATCH-READY" && gate.GateID != "GATE-EXECUTION-SPEC-CHANGE-REQUIRED") {
		return controller.ControlResult{}, false
	}
	// A previous observation can be displayed; it never commits a transition.
	gate.TransitionCommitted = false
	guidance := BuildGuidanceForState(root, snapshot.State, request.Event, request)
	return controller.ControlResult{
		Snapshot: snapshot, QualityGate: gate,
		Decision: policy.Decision{Decision: "allow", Guidance: &guidance},
	}, true
}
