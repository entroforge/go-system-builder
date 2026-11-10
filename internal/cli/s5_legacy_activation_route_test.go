package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Simulate an already registered, pre-convergence S5 responsibility. The
// envelope is genuinely published; only its historical registration is seeded.
// New production registration must never need this compatibility fixture.
func historicalS5Activation(t *testing.T, f stageAnswerFixture, p workpacket.Packet) string {
	t.Helper()
	ref, err := assignment.PreStageActivationEnvelope(f.root, "workgroup-stage-answer", f.taskID, p.Binding.Owner, "plan_checkpoint", assignment.ActivationSourceEntry{
		AgentID: p.Binding.Owner, AgentDefinitionRef: ".claude/agents/document-verifier.md", SkillRefs: []string{"document-verification"}, OutputPaths: []string{p.AnswerPath},
	})
	if err != nil || ref == "" {
		t.Fatalf("historical envelope: %q %v", ref, err)
	}
	snap := stageSnapshot(t, f.root)
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == p.Binding.Owner {
			row["activation_ref"] = ref
		}
	}
	stageJSON(t, f.root, ".claude/loop-state.json", snap.State)
	return ref
}

func historicalS5Plan(t *testing.T, f stageAnswerFixture, p workpacket.Packet) string {
	t.Helper()
	asset, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(asset, &messages); err != nil {
		t.Fatal(err)
	}
	for _, plan := range messages {
		if plan["message_type"] != "plan_report" {
			continue
		}
		plan["runtime_id"], plan["agent_id"], plan["assignment_id"] = p.Binding.RuntimeID, p.Binding.Owner, p.Binding.AssignmentID
		plan["agent_definition_ref"] = ".claude/agents/document-verifier.md"
		plan["team_id"], plan["task_id"], plan["assignment_revision"] = "workgroup-stage-answer", f.taskID, 1
		plan["planned_paths"] = []string{p.AnswerPath}
		plan["objective"] = "Independently verify the frozen document responsibility."
		ref := ".claude/evidence/historical-plan-" + p.Binding.Owner + ".json"
		stageJSON(t, f.root, ref, plan)
		return ref
	}
	t.Fatal("missing plan example")
	return ""
}

func TestS5HistoricalActivationRetainsPlanSendMessageAndSavedAnswerRoute(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	ref := historicalS5Activation(t, f, p)
	if _, err := os.Stat(filepath.Join(f.root, ref)); err != nil {
		t.Fatal(err)
	}
	planRef := historicalS5Plan(t, f, p)
	body, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "agent_id": p.Binding.Owner, "teammate_name": p.Binding.Owner,
		"session_id": "historical-s5", "cwd": f.root, "tool_name": "SendMessage",
		"tool_input": map[string]any{"message_type": "plan_report", "agent_id": p.Binding.Owner, "plan_ref": planRef},
	})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("Post: %d %s %s", code, &out, &diagnostic)
	}
	snap := stageSnapshot(t, f.root)
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == p.Binding.Owner && (row["state"] != "working" || row["activation_revision"] == nil || row["readback_ref"] == nil) {
			t.Fatalf("legacy PLAN auto-chain was swallowed: %+v stdout=%s stderr=%s", row, &out, &diagnostic)
		}
	}
	if !strings.Contains(diagnostic.String(), "auto-chain:") {
		t.Fatalf("missing original auto-chain: %s %s", &out, &diagnostic)
	}
	receipt := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass"))
	if !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("legacy saved answer was not received: %+v", receipt)
	}
	if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, p.ID); err != nil || !accepted {
		t.Fatalf("missing legacy receipt: %t %v", accepted, err)
	}
}

func TestS5FreshNativeWithoutActivationReceivesDirectReturn(t *testing.T) {
	f := newStageAnswerFixture(t)
	for _, raw := range stageSnapshot(t, f.root).State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["activation_ref"] != nil || row["activation_revision"] != nil || row["readback_ref"] != nil {
			t.Fatalf("fresh native unexpectedly needs legacy activation: %+v", row)
		}
	}
	p := f.packets[0]
	if code, out, diagnostic := nativeReviewEvent(t, f.root, "SubagentStart", p.Binding.Owner, ""); code != 0 || !strings.Contains(out, "original questionnaire") {
		t.Fatalf("native Start: %d %s %s", code, out, diagnostic)
	}
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	if code, out, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", p.Binding.Owner, filepath.Join(f.root, p.ViewPath)); code != 0 || !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("native Stop: %d %s %s", code, out, diagnostic)
	}
	if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, p.ID); err != nil || !accepted {
		t.Fatalf("missing native receipt: %t %v", accepted, err)
	}
}
