package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestReviewResultPacketLookupIssueAndSubtypeIsolation(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, packet, nil)
	if got, found, err := FindReviewResultPacket(state, packet.ID); err != nil || !found || got.ID != packet.ID {
		t.Fatalf("FindReviewResultPacket() = (%v, %v, %v)", got.ID, found, err)
	}
	if _, _, err := FindWorkPacket(state, packet.ID); err == nil || !strings.Contains(err.Error(), "typed lookup") {
		t.Fatalf("S5 lookup accepted S7 packet: %v", err)
	}
	if err := VerifyReviewResultPacketIssue(root, state, packet); err != nil {
		t.Fatalf("valid original S7 issuance rejected: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReviewResultPacketIssue(root, state, packet); err != nil {
		t.Fatalf("public issue verifier rejected exact Writer event: %v", err)
	}
	filtered, err := validateReviewResultCheckpointIndices(root, state, journal)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(filtered["entities"].(map[string]any)["work_packets"].([]any)); got != 0 {
		t.Fatalf("S7 filtering for legacy S5 validator retained %d rows", got)
	}
	if got := len(state["entities"].(map[string]any)["work_packets"].([]any)); got != 1 {
		t.Fatalf("S7 validation mutated the original Runtime state: rows=%d", got)
	}
	if err := ValidateCheckpointIndices(root, state, journal); err != nil {
		t.Fatalf("full Runtime checkpoint index path rejected canonical S7 packet: %v", err)
	}
}

func TestCurrentReviewResultPacketsProjectsUnifiedAssignmentStatus(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, packet, nil)
	state["schema_version"] = "1.3.0"
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	row := assignments[packet.Binding.AssignmentID].(map[string]any)
	row["status"] = "active"
	row["last_lifecycle_event"] = "workgroup_registered"
	current, err := CurrentReviewResultPackets(state, packet.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 || current[0].ID != packet.ID {
		t.Fatalf("canonical active Assignment packet lookup = %v, %v", current, err)
	}

	row["status"] = "done"
	row["last_lifecycle_event"] = "review_result_submitted"
	row["result_ref"] = ".claude/evidence/result.json"
	consumed, err := CurrentReviewResultPackets(state, packet.Binding.OwnerAgentID)
	if err != nil || len(consumed) != 1 || consumed[0].ID != packet.ID {
		t.Fatalf("canonical consumed Assignment packet lookup = %v, %v", consumed, err)
	}

	row["last_lifecycle_event"] = "review_plan_registered"
	current, err = CurrentReviewResultPackets(state, packet.Binding.OwnerAgentID)
	if err != nil || len(current) != 0 {
		t.Fatalf("mismatched canonical status/event granted current packet: %v, %v", current, err)
	}
}

func TestReviewResultPacketIssueDoesNotReproveHistoricalEvent(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, packet, nil)
	var event map[string]any
	if err := json.Unmarshal(journal[:len(journal)-1], &event); err != nil {
		t.Fatal(err)
	}
	event["idempotency_key"] = "packet:other"
	corrupt, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), append(corrupt, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReviewResultPacketIssue(root, state, packet); err != nil {
		t.Fatalf("historical event metadata invalidated issued packet: %v", err)
	}
}

func TestCurrentReviewResultPacketsIncludesConsumedTupleForReplay(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, packet, nil)
	state["review"] = map[string]any{
		"round": 1,
		"plan": map[string]any{
			"plan_id": packet.Binding.ReviewPlanID, "path": packet.Binding.ReviewPlanRef,
			"revision": packet.Binding.ReviewPlanRevision,
			"status":   "running",
		},
		"assignments": map[string]any{packet.Binding.AssignmentID: map[string]any{
			"status": "consumed", "agent_id": packet.Binding.OwnerAgentID,
			"claim_ids": []any{packet.Binding.ClaimIDs[0], packet.Binding.ClaimIDs[1]}, "result_ref": "result.json",
		}},
	}
	state["lifecycle"] = map[string]any{"state": "complete", "phase": nil}
	state["review"].(map[string]any)["plan"].(map[string]any)["status"] = "completed"
	packets, err := CurrentReviewResultPackets(state, packet.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 || packets[0].ID != packet.ID {
		t.Fatalf("consumed exact-current packet should remain available for receipt replay: packets=%d err=%v", len(packets), err)
	}
	legacy, err := ReviewResultPackets(map[string]any{"schema_version": "1.1.0"})
	if err != nil || len(legacy) != 0 {
		t.Fatalf("legacy state with no packet registry should remain compatible: len=%d err=%v", len(legacy), err)
	}
}

func TestReviewResultCheckpointDoesNotInventPacketFromHistoricalIssue(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, packet, nil)
	state["entities"].(map[string]any)["work_packets"] = []any{}
	if _, err := validateReviewResultCheckpointIndices(root, state, journal); err != nil {
		t.Fatalf("unrelated historical issuance blocked checkpoint: %v", err)
	}
	if _, found, err := FindReviewResultPacket(state, packet.ID); err != nil || found {
		t.Fatalf("historical issue invented a packet: found=%v err=%v", found, err)
	}
}

func checkpointTestReviewPacket(t testing.TB, root string) workpacket.ReviewResultPacket {
	t.Helper()
	claimBytes := json.RawMessage(`[
		{"claim_id":"claim-login","lens":"qa","target":"login","assertion":"login works","oracle":"visible home","method":"submit credentials","required_evidence":[],"applicability":"required","na_rationale":"","source_refs":["REQ-041"],"focus_key":"","depends_on":[],"resource_locks":[]},
		{"claim_id":"claim-reset","lens":"qa","target":"reset","assertion":"reset works","oracle":"email sent","method":"request reset","applicability":"required","source_refs":["REQ-041"]}
	]`)
	subjectBytes := json.RawMessage(`[{"path":"docs/requirements/REQ-041.md","kind":""}]`)
	var typedClaims []reviewmeta.Claim
	if err := json.Unmarshal(claimBytes, &typedClaims); err != nil {
		t.Fatal(err)
	}
	packetClaimBytes, err := json.Marshal(typedClaims)
	if err != nil {
		t.Fatal(err)
	}
	var typedSubjects []reviewmeta.FrozenSubject
	if err := json.Unmarshal(subjectBytes, &typedSubjects); err != nil {
		t.Fatal(err)
	}
	packetSubjectBytes, err := json.Marshal(typedSubjects)
	if err != nil {
		t.Fatal(err)
	}
	assignment := reviewmeta.PlanAssignment{
		AssignmentID: "assignment-qa-1", Lens: "qa", ClaimIDs: []string{"claim-login", "claim-reset"},
		NonOverlapBoundary: "Review the assigned login and reset claims", ExecutionWave: "static",
	}
	assignmentBytes, err := json.Marshal(assignment)
	if err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": "review-plan-041-r1", "baseline_generation": 3,
		"frozen_subjects": json.RawMessage(subjectBytes), "claims": json.RawMessage(claimBytes),
		"assignments":        json.RawMessage("[" + string(assignmentBytes) + "]"),
		"e2e_coverage_state": "cold_start", "dispatch_capacity_policy": "coverage_complete",
		"created_by": "fixture-planner", "created_at": "2026-10-14T00:00:00Z",
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planRef := ".claude/review/plan.json"
	if err := os.MkdirAll(filepath.Join(root, ".claude", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(planRef)), planBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", planBytes); err != nil {
		t.Fatalf("fixture pinned ReviewPlan does not satisfy schema: %v", err)
	}
	p, err := workpacket.BuildReviewResult(workpacket.ReviewResultBinding{
		RuntimeID: "loop-REQ-041", BaselineGeneration: 3,
		ReviewPlanID: "review-plan-041-r1", ReviewPlanRef: planRef,
		ReviewPlanRevision: 2,
		AssignmentID:       assignment.AssignmentID, OwnerAgentID: "reviewer-qa-1",
		ClaimIDs: []string{"claim-login", "claim-reset"},
	}, workpacket.ReviewResultInputs{
		Claims: packetClaimBytes, FrozenSubjects: packetSubjectBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func checkpointTestReviewState(t testing.TB, packet workpacket.ReviewResultPacket, otherEvents []map[string]any) (map[string]any, []byte) {
	t.Helper()
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var packetRow map[string]any
	if err := json.Unmarshal(packetJSON, &packetRow); err != nil {
		t.Fatal(err)
	}
	cursor := map[string]any{"state": "verification", "phase": "review"}
	event := buildJournalEvent(Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.RuntimeID,
		BaselineGeneration: packet.Binding.BaselineGeneration, From: cursor, To: cursor,
	}, packet.Binding.RuntimeID, 1, 2, 1, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	events := append([]map[string]any{event}, otherEvents...)
	var journal []byte
	lastID := ""
	for i, row := range events {
		if i > 0 {
			row["sequence"] = i + 1
		}
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		journal = append(journal, data...)
		journal = append(journal, '\n')
		lastID = stringValue(row["event_id"])
	}
	lastRevision := 2
	if len(events) > 0 {
		if revision, err := integerField(events[len(events)-1], "after_revision"); err == nil {
			lastRevision = revision
		}
	}
	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": packet.Binding.RuntimeID, "revision": lastRevision,
		"baseline":      map[string]any{"generation": packet.Binding.BaselineGeneration},
		"lifecycle":     map[string]any{"state": "verification", "phase": "review"},
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"journal":       map[string]any{"last_sequence": len(events), "last_event_id": lastID},
		"entities":      map[string]any{"work_packets": []any{packetRow}, "submissions": []any{}},
		"review": map[string]any{
			"round": 1,
			"plan": map[string]any{
				"plan_id": packet.Binding.ReviewPlanID, "path": packet.Binding.ReviewPlanRef,
				"revision": packet.Binding.ReviewPlanRevision,
				"status":   "running",
			},
			"assignments": map[string]any{packet.Binding.AssignmentID: map[string]any{
				"status": "dispatched", "agent_id": packet.Binding.OwnerAgentID,
				"claim_ids": []any{packet.Binding.ClaimIDs[0], packet.Binding.ClaimIDs[1]},
			}},
		},
	}
	return state, journal
}
