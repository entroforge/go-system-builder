package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// Exercise the original Plan consumer and workgroup registration using real
// document references, without constructing SHA or a synthetic round.
func TestS7OriginalPlanDraftAndRegisterWithoutDigest(t *testing.T) {
	exerciseS7OriginalPlanDraftAndRegister(t, false)
}

func TestS7NaturalRevisionContinuesChangedResponsibility(t *testing.T) {
	exerciseS7OriginalPlanDraftAndRegister(t, true)
}

func exerciseS7OriginalPlanDraftAndRegister(t *testing.T, revise bool) {
	t.Helper()
	root := assignmentTestRoot(t)
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	state := activeState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["baseline"].(map[string]any)["generation"] = 1
	state["review"] = map[string]any{"round_entry": map[string]any{"plan_id": "review-plan-original", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}}
	bound := state["bound_req"].(map[string]any)
	bound["id"], bound["path"], bound["version"] = "REQ-002", "docs/control/agent-protocol.md", "v1.0.0"
	delete(bound, "sha256")
	taskPath := filepath.Join(root, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state["documents"] = []any{map[string]any{"id": "TASK-012", "kind": "task", "path": "TASK-012.md", "version": "v1.0.0", "generation": 1, "status": "locked"}}
	statePath, journal := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
	writeJSON(t, statePath, state)
	plan := review.Plan{SchemaVersion: "1.1.0", ReviewPlanID: "review-plan-original", BaselineGeneration: 1,
		FrozenSubjects:         []review.FrozenSubject{{Path: "TASK-012.md", Kind: "task"}},
		Claims:                 []review.Claim{{ClaimID: "claim-dv", Lens: "delivery", Target: "TASK-012", Assertion: "scope covered", Oracle: "consistent", Method: "review", Applicability: "required", SourceRefs: []string{"TASK-012"}}},
		Assignments:            []review.PlanAssignment{{AssignmentID: "assignment-dv", Lens: "delivery", ClaimIDs: []string{"claim-dv"}, NonOverlapBoundary: "owns this traceability", ExecutionWave: "static"}},
		DispatchCapacityPolicy: "coverage_complete", CreatedBy: "Main", CreatedAt: "2026-10-31T00:00:00Z"}
	planPath := filepath.Join(dir, "review-plan-answer.json")
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := json.Unmarshal(planData, &original); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, planPath, original)
	registered, err := review.RegisterPlan(root, statePath, journal, review.PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("original plan reception: %v", err)
	}
	draft, _, err := review.DraftManifest(root, registered.State, "assignment-dv")
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	// Preserve the selected real Skill sources in this isolated project so the
	// issuer can bind the actual registered methods, just as an installation does.
	for _, skill := range draft.Assignments[0].SkillRefs {
		data, err := os.ReadFile(filepath.Join("..", "..", "skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "skills", skill, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"sha256"`) || strings.Contains(string(encoded), `"review_round"`) {
		t.Fatalf("draft introduced old machine qualification: %s", encoded)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	writeJSON(t, manifestPath, draft)
	dispatched, err := assignment.Register(root, statePath, journal, assignment.Request{ExpectedRevision: registered.Revision, ManifestPath: manifestPath, TaskID: "TASK-012", TaskPath: taskPath})
	if err != nil {
		t.Fatalf("original workgroup registration: %v", err)
	}
	row := dispatched.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv"].(map[string]any)
	if row["status"] != "dispatched" || row["agent_id"] != draft.Assignments[0].AgentID {
		t.Fatalf("original owner not dispatched: %v", row)
	}
	packets, err := review.IssueResultPackets(root, statePath, journal, dispatched.Revision, draft.Assignments[0].AgentID)
	if err != nil {
		t.Fatalf("original reviewer issuance: %v", err)
	}
	if len(packets) != 1 || packets[0].Binding.AssignmentID != "assignment-dv" || packets[0].Binding.ReviewPlanID != "review-plan-original" {
		t.Fatalf("wrong reviewer packet: %+v", packets)
	}
	if revise {
		claims := original["claims"].([]any)
		claims[0].(map[string]any)["assertion"] = "revised scope covered"
		writeJSON(t, planPath, original)
		revised, outcome, err := review.ReceivePlanAnswer(root, statePath, journal, review.PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
		if err != nil || outcome != review.PlanReceptionRevised {
			t.Fatalf("natural revision: outcome=%s err=%v", outcome, err)
		}
		unchanged, err := review.ManifestAssignmentUnchangedSince(root, revised.State, "assignment-dv", 1)
		if err != nil || unchanged {
			t.Fatalf("same Claim ID with changed assertion was treated as unchanged: unchanged=%t err=%v", unchanged, err)
		}
		next, _, err := review.DraftManifest(root, revised.State, "assignment-dv")
		if err != nil {
			t.Fatalf("revised draft: %v", err)
		}
		replayedDraft, _, err := review.DraftManifest(root, revised.State, "assignment-dv")
		if err != nil || replayedDraft.ManifestID != next.ManifestID || replayedDraft.WorkgroupID != next.WorkgroupID || replayedDraft.Assignments[0].AgentID != next.Assignments[0].AgentID {
			t.Fatalf("same amendment did not reuse dispatch identity: %#v err=%v", replayedDraft, err)
		}
		nextPath := filepath.Join(dir, "revised-manifest.json")
		writeJSON(t, nextPath, next)
		reissued, err := assignment.Register(root, statePath, journal, assignment.Request{ExpectedRevision: revised.Revision, ManifestPath: nextPath, TaskID: "TASK-012", TaskPath: taskPath})
		if err != nil {
			t.Fatalf("revised workgroup registration: %v", err)
		}
		row := reissued.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv"].(map[string]any)
		if row["status"] != "dispatched" {
			t.Fatalf("changed responsibility not dispatched: %v", row)
		}
		if next.Assignments[0].AgentID == draft.Assignments[0].AgentID || next.WorkgroupID == draft.WorkgroupID {
			t.Fatal("changed responsibility reused its original owner or workgroup")
		}
		newPackets, err := review.IssueResultPackets(root, statePath, journal, reissued.Revision, next.Assignments[0].AgentID)
		if err != nil || len(newPackets) != 1 {
			t.Fatalf("issue revised original responsibility: packets=%d err=%v", len(newPackets), err)
		}
		if newPackets[0].ID == packets[0].ID || newPackets[0].AnswerPath == packets[0].AnswerPath {
			t.Fatal("revised issuance replaced its in-flight original packet")
		}
		receive := func(packetIndex int) {
			t.Helper()
			packet := packets[0]
			if packetIndex == 1 {
				packet = newPackets[0]
			}
			packet.Draft.Answer = json.RawMessage(`{"verdict":"pass"}`)
			data, err := json.Marshal(packet.Draft)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, packet.AnswerPath), data, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = review.SubmitIssuedResult(root, statePath, journal, review.IssuedSubmitRequest{
				ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID,
			})
			if err != nil {
				t.Fatalf("receive original packet %d: %v", packetIndex, err)
			}
		}
		receive(0)
		afterOld, err := loopruntime.NewStore(statePath, journal).Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		oldReceipt, accepted, err := review.IssuedResultReceipt(root, afterOld.State, packets[0].ID, packets[0].Binding.OwnerAgentID)
		if err != nil || !accepted || oldReceipt.ResultRef == "" {
			t.Fatalf("original late answer was not retained: accepted=%t receipt=%+v err=%v", accepted, oldReceipt, err)
		}
		if review.RequiredResponsibilitiesComplete(afterOld.State) {
			t.Fatal("original late answer completed the revised responsibility")
		}
		latest, err := review.IssuedResultAppliesToCurrent(root, reissued.State, packets[0], nil)
		if err != nil || latest {
			t.Fatalf("superseded original result must not claim amended responsibility: applies=%t err=%v", latest, err)
		}
		receive(1)
		after, err := loopruntime.NewStore(statePath, journal).Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		ptr := review.PlanPointerFromState(after.State)
		if !review.RequiredResponsibilitiesComplete(after.State) || ptr == nil || ptr.Status != "clean" {
			t.Fatalf("revised original result did not complete its responsibility: err=%v", err)
		}
		claims[0].(map[string]any)["assertion"] = "a second real amendment"
		writeJSON(t, planPath, original)
		second, outcome, err := review.ReceivePlanAnswer(root, statePath, journal, review.PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
		if err != nil || outcome != review.PlanReceptionRevised {
			t.Fatalf("second natural revision: outcome=%s err=%v", outcome, err)
		}
		thirdDraft, _, err := review.DraftManifest(root, second.State, "assignment-dv")
		if err != nil {
			t.Fatalf("second revised draft: %v", err)
		}
		if thirdDraft.WorkgroupID == next.WorkgroupID || thirdDraft.Assignments[0].AgentID == next.Assignments[0].AgentID {
			t.Fatal("consecutive amendments reused a prior owner or workgroup")
		}
		thirdPath := filepath.Join(dir, "second-revised-manifest.json")
		writeJSON(t, thirdPath, thirdDraft)
		third, err := assignment.Register(root, statePath, journal, assignment.Request{ExpectedRevision: second.Revision, ManifestPath: thirdPath, TaskID: "TASK-012", TaskPath: taskPath})
		if err != nil {
			t.Fatalf("second revised registration: %v", err)
		}
		thirdPackets, err := review.IssueResultPackets(root, statePath, journal, third.Revision, thirdDraft.Assignments[0].AgentID)
		if err != nil || len(thirdPackets) != 1 {
			t.Fatalf("second revised packet: packets=%d err=%v", len(thirdPackets), err)
		}
	}
}
