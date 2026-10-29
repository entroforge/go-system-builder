package investigation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Real original dispatch, professional Plan, Begin and Worker preparation are
// used with no AgentBegin. Engineering judgments remain mechanical fixture data.
func TestS9NonpassAndResolutionDoNotRequireGenericPlan(t *testing.T) {
	fixture, owner, packet, execution, workerRoot, _, issued := prepareS9RepairResultPacketWithPlanActivity(t, false)
	agent := agentLifecycleRow(issued.State, owner.agentID)
	projection, err := workstate.AgentProjection(agent, issued.State["schema_version"].(string))
	if err != nil || projection.Status != workstate.Active || projection.Activity != "reading" {
		t.Fatalf("fixture did not retain dispatched active reading responsibility: %+v %v", projection, err)
	}
	for _, field := range []string{"readback_ref", "work_started_ref", "completion_reported_ref"} {
		if value, _ := agent[field].(string); value != "" {
			t.Fatalf("fixture unexpectedly supplied generic prerequisite %s=%s", field, value)
		}
	}
	accepted := runS9RepairResultPostToolUse(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, packet.AnswerPath)
	if !accepted.Accepted || accepted.WorkComplete || accepted.Error != "" {
		t.Fatalf("professional nonpass was not accepted independently: %+v", accepted)
	}
	blocked := currentS9Snapshot(t, fixture)
	if blocked.Revision != issued.Revision+1 || workstate.AgentActivity(agentLifecycleRow(blocked.State, owner.agentID)) != "blocked" {
		t.Fatalf("professional blocker did not record one original blocked CAS: before=%d after=%d", issued.Revision, blocked.Revision)
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, blocked.State, packet.ID, owner.agentID); err != nil || !mayEnd {
		t.Fatalf("current blocker still required generic PLAN proof: mayEnd=%t err=%v", mayEnd, err)
	}
	path, err := repair.RepairResultResolutionAuthoringPath(packet)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(fixture.root, filepath.FromSlash(path))
	if !strings.Contains(accepted.NextAction, abs) {
		t.Fatalf("accepted blocker omitted exact Main recovery questionnaire: %+v", accepted)
	}
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replayed := runS9RepairResultPostToolUse(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, packet.AnswerPath)
	if !replayed.Accepted {
		t.Fatalf("same professional blocker did not replay: %+v", replayed)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeState, beforeJournal, "professional blocker same-answer replay without generic PLAN")
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, abs), &draft); err != nil {
		t.Fatal(err)
	}
	draft["s9_resolution"].(map[string]any)["rationale"] = "The missing fixture is available again; resume the same professional responsibility within its unchanged Contract."
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0600); err != nil {
		t.Fatal(err)
	}
	resolved, _ := runS9RepairResultHookPayloadWithRaw(t, fixture.root, s9RepairResultHookPayload("", fixture.root, "main-resolution-session", abs))
	if !resolved.Accepted || resolved.WorkComplete || resolved.ResponsibilityStatus != "blocker_resolution_recorded" {
		t.Fatalf("professional Main recovery still required generic PLAN: %+v", resolved)
	}
	current := currentS9Snapshot(t, fixture)
	if _, found, err := runtime.VerifiedRepairResultResolutionReceipt(fixture.root, current.State, packet.ID, owner.agentID); err != nil || !found {
		t.Fatalf("professional recovery lacks original resolution fact: found=%t err=%v", found, err)
	}
}
