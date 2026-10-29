package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9ReadOrderKeepsMainAndWorkerResponsibilitiesSeparate(t *testing.T) {
	for _, result := range []bool{false, true} {
		for _, accepted := range []bool{false, true} {
			f := newS9ListenerFixture(t, result)
			artifact := ""
			if accepted {
				if result {
					artifact = ".claude/candidate.json"
					ref := f.artifact(t, artifact, map[string]any{"provenance": map[string]any{"packet_id": f.packet.id, "owner_agent_id": f.packet.binding.OwnerAgentID, "execution_root": f.worker}})
					execution := mapFieldCLI(mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions"), f.packet.binding.ManifestAssignmentID)
					execution["delivery_ref"], execution["delivery_sha256"] = ref["path"], ref["sha256"]
				} else {
					artifact = ".claude/accepted-plan.json"
					ref := f.artifact(t, artifact, map[string]any{"receipt": map[string]any{"packet_id": f.packet.id, "owner_agent_id": f.packet.binding.OwnerAgentID}})
					repairPointerCLI(f.state)["plan_report_refs"] = []any{ref}
				}
			}
			if result && accepted {
				if err := os.RemoveAll(f.worker); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := json.Marshal(f.state)
			for _, owner := range []string{"Main", f.packet.binding.OwnerAgentID} {
				got := strings.Join(s9QuestionnaireReadOrder(f.root, f.state, policy.Input{Event: "PreToolUse", AgentID: owner, CWD: f.root}), "\n")
				private := strings.Contains(got, f.packet.answer) || strings.Contains(got, f.packet.view)
				if private != (owner != "Main" && !accepted) {
					t.Fatalf("result=%t accepted=%t owner=%s private answer misassigned: %s", result, accepted, owner, got)
				}
				if accepted && !strings.Contains(got, filepath.Join(f.root, artifact)) {
					t.Fatalf("accepted original artifact missing: %s", got)
				}
			}
			after, _ := json.Marshal(f.state)
			if string(before) != string(after) {
				t.Fatal("read order changed Runtime")
			}
		}
	}
}

func TestProgressIgnoresOtherTypedPacketsButKeepsS5Diagnostics(t *testing.T) {
	root := initFormatRoot(t)
	state, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	state["bound_req"] = map[string]any{"id": "REQ-progress"}
	state["lifecycle"].(map[string]any)["state"] = "verification"
	state["lifecycle"].(map[string]any)["phase"] = "running"
	state["milestone"].(map[string]any)["lifecycle_state"] = "verification"
	state["milestone"].(map[string]any)["read"] = []any{}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	request := policy.Input{CWD: root, SessionID: "typed-progress"}
	before, _, err := projectWorkProgress(root, request, runtime.Snapshot{State: state})
	if err != nil || !before.Known {
		t.Fatalf("original Main progress unavailable: %+v %v", before, err)
	}
	rows := []any{}
	for _, template := range []string{"s2-design-v1", "s3-contract-v1", "s4-task-planning-v1", "s7-review-result-v1", "s8-hypothesis-result-v1", "s9-plan-report-v1", "s9-repair-result-v1"} {
		rows = append(rows, map[string]any{"packet_id": "packet-" + template, "template_id": template})
	}
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = rows
	stateBytes, _ := json.Marshal(state)
	after, _, err := projectWorkProgress(root, request, runtime.Snapshot{State: state})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("unrelated typed history changed original progress: %+v %v", after, err)
	}
	unchanged, _ := json.Marshal(state)
	if string(stateBytes) != string(unchanged) {
		t.Fatal("sampler mutated typed packets")
	}
	// Corrupt supported S5 packets still get their original diagnostic.
	entities["work_packets"] = append(rows, map[string]any{"packet_id": "bad-s5", "template_id": "s5-document-review-v1", "binding": 1})
	if _, _, err := projectWorkProgress(root, request, runtime.Snapshot{State: state}); err == nil || !strings.Contains(err.Error(), "issued responsibility unavailable") {
		t.Fatalf("supported S5 corruption disappeared: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/hook-notices/work-progress")); !os.IsNotExist(err) {
		t.Fatalf("projection created a diagnostic cache: %v", err)
	}
}

func TestS9MainReadOrderKeepsOriginalBlockerRecovery(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	ref := f.artifact(t, ".claude/blocker.json", map[string]any{"packet_id": f.packet.id, "binding": f.row["binding"], "execution_root": f.worker, "submitted_answer": map[string]any{"verdict": "blocked"}})
	ref["kind"], ref["baseline_generation"], ref["scope_refs"] = "repair_result_blocker", 1, []any{f.packet.id, f.packet.binding.RepairAssignmentID}
	f.state["evidence"] = []any{ref}
	resolution, err := workpacket.RepairResultResolutionAuthoringPath(workpacket.RepairResultPacket{AnswerPath: f.packet.answer, Binding: *f.packet.result})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s9QuestionnaireReadOrder(f.root, f.state, policy.Input{Event: "PreToolUse", AgentID: "Main"}), "\n")
	if !strings.Contains(got, filepath.Join(f.root, resolution)) || !strings.Contains(got, filepath.Join(f.root, ".claude/blocker.json")) || strings.Contains(got, f.packet.view) || strings.Contains(got, filepath.Join(f.worker, f.packet.answer)) {
		t.Fatalf("Main lost blocker recovery or acquired Worker's private answer: %s", got)
	}
}

func TestNativeS9MainReadOrderUsesDispatchBrief(t *testing.T) {
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	repairLeadCommitFixture(t, root)
	repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	state := repairDispatchSnapshot(t, root).State
	packets := nativeS9PlanPackets(root, state)
	if len(packets) != 1 {
		t.Fatalf("expected one original native responsibility, got %d", len(packets))
	}
	brief, _, _ := nativeS9Paths(packets[0])
	before := repairDispatchRuntimeBytes(t, root)
	got := strings.Join(s9QuestionnaireReadOrder(root, state, policy.Input{Event: "PreToolUse", AgentID: "Main"}), "\n")
	if !strings.Contains(got, filepath.Join(root, brief)) || strings.Contains(got, packets[0].AnswerPath) || strings.Contains(got, packets[0].ViewPath) {
		t.Fatalf("Main did not receive original dispatch brief instead of Worker question: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9UnknownNativeEventDoesNotReadMainQuestions(t *testing.T) {
	f := newS9ListenerFixture(t, false)
	for _, event := range []string{"SubagentStart", "SubagentStop", "TeammateIdle"} {
		if got := s9QuestionnaireReadOrder(f.root, f.state, policy.Input{Event: event}); len(got) != 0 {
			t.Fatalf("unidentified %s inherited Main reading: %v", event, got)
		}
	}
}

func TestS9MainReadsFinalAndImpactAfterIntegration(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	final := f.artifact(t, ".claude/final.json", map[string]any{"result": "pass"})
	repairPointerCLI(f.state)["result_refs"] = []any{final}
	repairPointerCLI(f.state)["impact_ref"] = ".claude/impact.json"
	for _, phase := range []string{"targeted_reverification", "ready_for_full_review"} {
		mapFieldCLI(f.state, "lifecycle")["phase"] = phase
		repairPointerCLI(f.state)["status"] = phase
		got := strings.Join(s9QuestionnaireReadOrder(f.root, f.state, policy.Input{Event: "PreToolUse", AgentID: "Main"}), "\n")
		if !strings.Contains(got, filepath.Join(f.root, ".claude/final.json")) || !strings.Contains(got, filepath.Join(f.root, ".claude/impact.json")) || strings.Contains(got, f.packet.answer) || strings.Contains(got, f.packet.view) {
			t.Fatalf("%s lost Main's accepted sources or revived a Worker question: %s", phase, got)
		}
	}
}
