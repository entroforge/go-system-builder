package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility(t *testing.T) {
	root := t.TempDir()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildReviewResult(workpacket.ReviewResultBinding{
		RuntimeID: "loop-S7-life", BaselineGeneration: 1, ReviewRound: 1,
		ReviewPlanID: "review-plan-life", ReviewPlanRef: "absent-plan.json", ReviewPlanRevision: 1,
		ReviewPlanSHA256: strings.Repeat("a", 64), AssignmentID: "assignment-qa-life",
		AssignmentSHA256: strings.Repeat("b", 64), OwnerAgentID: "reviewer-life",
		ClaimIDs: []string{"claim-life"}, SubjectDigest: strings.Repeat("c", 64),
	}, workpacket.ReviewResultInputs{
		Claims:         json.RawMessage(`[{"claim_id":"claim-life","lens":"qa","target":"service","assertion":"errors propagate","oracle":"error response","method":"inspection","applicability":"required","source_refs":["REQ-life"]}]`),
		FrozenSubjects: json.RawMessage(`[{"path":"service.go","sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(packet)
	var packetRow map[string]any
	if err := json.Unmarshal(encoded, &packetRow); err != nil {
		t.Fatal(err)
	}
	state["schema_version"], state["runtime_id"] = "1.2.0", packet.Binding.RuntimeID
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["baseline"].(map[string]any)["generation"] = 1
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "running"}
	plan := map[string]any{"plan_id": packet.Binding.ReviewPlanID, "path": packet.Binding.ReviewPlanRef,
		"sha256": packet.Binding.ReviewPlanSHA256, "revision": 1, "review_round": 1, "status": "running"}
	state["review"] = map[string]any{"round": 1, "plan": plan, "assignments": map[string]any{
		packet.Binding.AssignmentID: map[string]any{"status": "dispatched", "agent_id": packet.Binding.OwnerAgentID,
			"lens": "qa", "claim_ids": []any{"claim-life"}, "result_ref": nil},
	}}
	state["entities"].(map[string]any)["work_packets"] = []any{packetRow}
	state["entities"].(map[string]any)["agents"] = []any{}
	// No Plan, product baseline, issuer journal or Service authority exists.
	// Existing questionnaire paths must still be delivered without revalidation.
	for _, ref := range []string{packet.ViewPath, packet.AnswerPath} {
		path := filepath.Join(root, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserved professional work"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	for _, event := range []string{"SessionStart", "SubagentStart", "PreCompact", "PreToolUse", "Stop", "SubagentStop", "TeammateIdle"} {
		t.Run(event, func(t *testing.T) {
			for _, status := range []string{"running", "clean", "observation_sealed", "paused"} {
				plan["status"] = status
				before, _ := json.Marshal(state)
				if err := os.WriteFile(statePath, before, 0600); err != nil {
					t.Fatal(err)
				}
				request := policy.Input{Event: event, CWD: root}
				result, observed := graphControlForHook(root, request)
				if observed != (status == "running") {
					t.Fatalf("%s handoff selection=%v", status, observed)
				}
				if observed {
					if result.QualityGate.Status != "" || result.Decision.Guidance == nil || result.Decision.Guidance.Event != event || result.Decision.Guidance.Blocked {
						t.Fatalf("graph reminder must not decide completion: %+v", result)
					}
					context := currentQuestionnaireLifecycleContext(root, request)
					if event == "SubagentStart" && context != "" {
						t.Fatal("unidentified SubagentStart must not guess Main's responsibility")
					}
					if event != "SubagentStart" && (!strings.Contains(context, filepath.Join(root, filepath.FromSlash(packet.AnswerPath))) || !strings.Contains(context, filepath.Join(root, filepath.FromSlash(packet.ViewPath)))) {
						t.Fatalf("entry/stop reminder lost exact issued paths: %s", context)
					}
					if event == "SubagentStop" || event == "TeammateIdle" {
						decision, controlled := hook.StopIdleDecision(root, policy.Input{Event: event, AgentID: packet.Binding.OwnerAgentID})
						if !controlled || !isDenyingHookDecision(decision.Decision) {
							t.Fatal("graph projection bypassed original unfinished responsibility check")
						}
					}
				}
				after, err := os.ReadFile(statePath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("reminder wrote Runtime: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, ".claude/loop-events.jsonl")); !os.IsNotExist(err) {
					t.Fatalf("reminder wrote journal: %v", err)
				}
			}
		})
	}
	// Missing Plan files permit reminders, never completion: even a reported
	// Agent and consumed label cannot replace the original consumer receipt.
	plan["status"] = "running"
	row := state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
	row["status"], row["result_ref"] = "consumed", "unreceipted-result.json"
	facts := hook.CurrentResponsibility(root, runtime.Snapshot{State: state}, &policy.AgentContext{
		ID: packet.Binding.OwnerAgentID, AssignmentID: packet.Binding.AssignmentID,
		State: "reported", ReviewAssignment: true, CompletionReportedRef: "generic-completion.json",
	}, &hookctx.AssignmentContext{AssignmentID: packet.Binding.AssignmentID, OwnerAgentID: packet.Binding.OwnerAgentID}, false)
	if facts.DeliveryRecorded || facts.ExecutionMayEnd || facts.Status == "done" {
		t.Fatalf("generic report or unreceipted row became done: %+v", facts)
	}
}
