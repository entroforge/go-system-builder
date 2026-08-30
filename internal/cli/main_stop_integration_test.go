package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/controller"
)

func TestMainStopHookBlocksUnconsumedReviewResult(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	seedSatisfiedPlanningDesign(t, root, state)
	state["review"] = map[string]any{
		"round":       1,
		"clean_round": nil,
		"assignments": map[string]any{
			"assignment-qa-1": map[string]any{
				"lens":       "qa",
				"claim_ids":  []any{"claim-qa-1"},
				"status":     "result_submitted",
				"agent_id":   "agent-qa-1",
				"result_ref": "docs/review/result.json",
			},
		},
	}
	writeACState(t, root, state)
	control, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "Stop", SessionID: "session-main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !control.QualityGate.TransitionCommitted {
		t.Fatalf("fixture must expose an automatic migration before Stop preflight: status=%q gate=%q missing=%v conflicts=%v error=%q", control.QualityGate.Status, control.QualityGate.GateID, control.QualityGate.Missing, control.QualityGate.Conflicts, control.Error)
	}
	// The probe above intentionally proves that this fixture would mutate if
	// the Controller ran first. Restore the pre-hook snapshot before exercising
	// the actual CLI path.
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "Stop", "--root", root}, strings.NewReader(`{"hook_event_name":"Stop","session_id":"session-main"}`), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Main Stop must exit 2 for an unconsumed Result, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	after := readStateRevision(t, root)
	if after != 1 {
		t.Fatalf("a blocked Main Stop preflight must not run a mutating Controller cycle: revision=%d", after)
	}
	for _, expected := range []string{"main_stop_unconsumed_result", "consume", "assignment-qa-1"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("Stop feedback missing %q: %s", expected, stderr.String())
		}
	}
}

func seedSatisfiedPlanningDesign(t *testing.T, root string, state map[string]any) {
	t.Helper()
	req := []byte("# REQ-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	architecture := []byte("# ARCHITECTURE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	for path, data := range map[string][]byte{
		"docs/requirements/REQ-001.md":                 req,
		"docs/design/architecture/ARCHITECTURE-001.md": architecture,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(root, path), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-design-stop",
		"kind":                    "planning_design",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "architect-1",
		"producer_responsibility": "Architect",
		"subject_refs": []map[string]any{
			{"path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(req)},
			{"path": "docs/design/architecture/ARCHITECTURE-001.md", "version": "v1.0.0", "sha256": sha256Hex(architecture)},
		},
		"conclusion": "pass",
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal design evidence: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evidence", "design.json"), envelopeData, 0o644); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{map[string]any{
		"id": "ev-design-stop", "kind": "planning_design", "path": "evidence/design.json",
		"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": 1,
		"review_round": nil, "produced_by": []any{"architect-1"},
		"responsibility_id": "Architect", "scope_refs": []any{}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil,
	}}
}

func readStateRevision(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	value, ok := state["revision"].(float64)
	if !ok {
		t.Fatalf("state revision has unexpected shape: %#v", state["revision"])
	}
	return int(value)
}

func TestMainStopHookAllowsActiveBackgroundReviewWork(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	state["review"] = map[string]any{
		"round": 1,
		"assignments": map[string]any{
			"assignment-qa-1": map[string]any{
				"status":   "dispatched",
				"agent_id": "agent-qa-1",
			},
		},
	}
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "Stop", "--root", root}, strings.NewReader(`{"hook_event_name":"Stop","session_id":"session-main"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("active background work must not block Main Stop, code=%d stderr=%s", code, stderr.String())
	}
}
