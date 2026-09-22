package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivatedCheckpointRecoveryRejectsTamperingAndWrongAssignment(t *testing.T) {
	root, snapshot, planPath := planCheckpointFixture(t)
	planBytes, _ := os.ReadFile(planPath)
	var plan map[string]any
	json.Unmarshal(planBytes, &plan)
	examples, _ := schema.ReadAsset("agent-message.examples.json")
	var messages []map[string]any
	json.Unmarshal(examples, &messages)
	var activation map[string]any
	for _, m := range messages {
		if m["message_type"] == "activation" {
			activation = m
			break
		}
	}
	for _, k := range []string{"runtime_id", "agent_id", "task_id", "team_id"} {
		activation[k] = plan[k]
	}
	activation["approved_readback_sha256"] = fmt.Sprintf("%x", sha256.Sum256(planBytes))
	activation["approved_readback_message_id"] = plan["message_id"]
	activationPath := writePlanCheckpointJSON(t, root, "activation.json", activation)
	rows := snapshot.State["entities"].(map[string]any)["agents"].([]any)
	row := rows[0].(map[string]any)
	row["dispatch_mode"] = "plan_checkpoint"
	row["state"] = "working"
	row["readback_ref"] = planPath
	row["activation_ref"] = activationPath
	ref, err := plancheckpoint.ActivatedRef(root, snapshot, "agent-s9")
	if err != nil || ref != planPath {
		t.Fatalf("valid missing marker must recover: %q %v", ref, err)
	}
	snapshot.State["revision"] = 0
	os.MkdirAll(filepath.Join(root, ".claude"), 0700)
	stateBytes, _ := json.Marshal(snapshot.State)
	os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), stateBytes, 0600)
	loaded, loadErr := hookctx.Load(root, "agent-s9")
	if loadErr != nil || loaded.Agent == nil || loaded.Agent.PlanReportedRef != planPath || loaded.PlanReportedRef != planPath {
		t.Fatalf("Idle and write gates did not receive the same restored checkpoint: %#v %v", loaded, loadErr)
	}
	// Same valid JSON and same identity, different bytes: activation no longer binds it.
	os.WriteFile(planPath, append(planBytes, '\n'), 0600)
	if ref, err := plancheckpoint.ActivatedRef(root, snapshot, "agent-s9"); err == nil || ref != "" {
		t.Fatal("tampered plan recovered")
	}
	loaded, loadErr = hookctx.Load(root, "agent-s9")
	if loadErr != nil || loaded.Agent == nil || loaded.Agent.PlanReportedRef != "" || loaded.PlanReportedRef != "" {
		t.Fatalf("tampered plan cleared a gate: %#v %v", loaded, loadErr)
	}
	os.WriteFile(planPath, planBytes, 0600)
	row["task_ids"] = []any{"TASK-OTHER"}
	if ref, err := plancheckpoint.ActivatedRef(root, snapshot, "agent-s9"); err == nil || ref != "" {
		t.Fatal("reassigned agent accepted old plan")
	}
}

func TestPostToolUseDoesNotClaimFailedPlanPersistence(t *testing.T) {
	root, snapshot, planPath := planCheckpointFixture(t)
	row := snapshot.State["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	row["state"] = "reading"
	row["dispatch_mode"] = "plan_checkpoint"
	snapshot.State["revision"] = 0
	// The partial Runtime fixture can be read, but cannot pass Writer validation.
	os.MkdirAll(filepath.Join(root, ".claude"), 0700)
	bytes, _ := json.Marshal(snapshot.State)
	os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), bytes, 0600)
	var out, errout strings.Builder
	code := runPostToolUseHook(root, policy.Input{Event: "PostToolUse", ToolName: "SendMessage", AgentID: "agent-s9", ToolInput: map[string]any{"message_type": "plan_report", "plan_ref": planPath}}, &out, &errout)
	if code != 0 || !strings.Contains(out.String(), "not persisted") {
		t.Fatalf("persistence failure hidden: code=%d out=%s err=%s", code, out.String(), errout.String())
	}
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if string(after) != string(bytes) {
		t.Fatal("failed observation continued activation")
	}
}
