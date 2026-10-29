package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS7GraphListenerLeavesDomainHandoffsToController(t *testing.T) {
	root := t.TempDir()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "running"}
	// No plan/evidence file or control definition exists: ordinary observation
	// must read these committed facts without invoking their original validators.
	plan := map[string]any{"plan_id": "review-1", "path": "absent-plan.json", "status": "running", "review_round": 1}
	state["review"] = map[string]any{"round": 1, "plan": plan, "assignments": map[string]any{
		"review-task": map[string]any{"status": "dispatched", "agent_id": "reviewer"},
	}}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "clean", "observation_sealed", "paused"} {
		t.Run(status, func(t *testing.T) {
			plan["status"] = status
			before, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, before, 0o644); err != nil {
				t.Fatal(err)
			}
			result, observed := graphControlForHook(root, policy.Input{Event: "PreToolUse", ToolName: "Read", AgentID: "reviewer"})
			if observed != (status == "running") {
				t.Fatalf("status %s: ordinary observation=%v; domain handoffs must fall through to the original Controller", status, observed)
			}
			if observed && (result.QualityGate.Status != "" || result.Decision.Guidance == nil || result.Decision.Guidance.Blocked ||
				!strings.Contains(result.Decision.Guidance.Action, "review-task")) {
				t.Fatalf("ordinary Read must show pending assignment without a gate or completion verdict: %+v", result)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("observation changed Runtime: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/loop-events.jsonl")); !os.IsNotExist(err) {
				t.Fatalf("observation wrote journal: %v", err)
			}
		})
	}
}
