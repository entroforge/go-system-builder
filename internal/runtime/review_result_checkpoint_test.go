package runtime

import (
	"encoding/json"
	"errors"
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
	if err := verifyReviewResultPacketIssueJournal(root, state, packet, journal); err != nil {
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

func TestReviewResultPacketIssueRejectsTamperedOriginalEvent(t *testing.T) {
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
	if err := verifyReviewResultPacketIssueJournal(root, state, packet, append(corrupt, '\n')); err == nil {
		t.Fatal("tampered original packet issuance was accepted")
	}
}

func TestReviewResultPacketCheckpointReopensPinnedPlanAndExactSources(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	if err := verifyReviewResultPacketSources(root, packet); err != nil {
		t.Fatalf("valid pinned Plan source rejected: %v", err)
	}

	var claims []map[string]any
	if err := json.Unmarshal(packet.Inputs.Claims, &claims); err != nil {
		t.Fatal(err)
	}
	claims[0]["assertion"] = "a different assertion"
	changedClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	forgedInputs, err := workpacket.BuildReviewResult(packet.Binding, workpacket.ReviewResultInputs{
		Claims: changedClaims, FrozenSubjects: packet.Inputs.FrozenSubjects,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReviewResultPacketSources(root, forgedInputs); err == nil {
		t.Fatal("packet-authored Claim prose detached from original Plan was accepted")
	}

	wrongAssignment := packet.Binding
	wrongAssignment.AssignmentSHA256 = strings.Repeat("e", 64)
	forgedAssignment, err := workpacket.BuildReviewResult(wrongAssignment, packet.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReviewResultPacketSources(root, forgedAssignment); err == nil {
		t.Fatal("packet with an Assignment digest not produced by the original seven-field JSON DTO was accepted")
	}

	planPath := filepath.Join(root, filepath.FromSlash(packet.Binding.ReviewPlanRef))
	original, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var invalidPlan map[string]any
	if err := json.Unmarshal(original, &invalidPlan); err != nil {
		t.Fatal(err)
	}
	invalidPlan["review_round"] = 0
	invalidBytes, err := json.Marshal(invalidPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, invalidBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	invalidBinding := packet.Binding
	invalidBinding.ReviewPlanSHA256 = sha256Bytes(invalidBytes)
	invalidPacket, err := workpacket.BuildReviewResult(invalidBinding, packet.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReviewResultPacketSources(root, invalidPacket); err == nil {
		t.Fatal("schema-invalid Plan with a matching forged packet SHA was accepted")
	}
	if err := os.WriteFile(planPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(original, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReviewResultPacketSources(root, packet); err == nil {
		t.Fatal("pinned Plan byte drift was accepted")
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
			"sha256": packet.Binding.ReviewPlanSHA256, "revision": packet.Binding.ReviewPlanRevision,
			"review_round": packet.Binding.ReviewRound, "status": "running",
			"verification_artifact_digest": strings.Repeat("d", 64),
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

func TestReviewResultCheckpointRejectsReceiptWithoutPacket(t *testing.T) {
	state := map[string]any{
		"schema_version": "1.2.0",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"entities":       map[string]any{"work_packets": []any{}},
		"review": map[string]any{"assignments": map[string]any{
			"assignment-qa-1": map[string]any{"result_receipt": map[string]any{"packet_id": "packet-missing"}},
		}},
	}
	if err := rejectOrphanReviewResultReceipts(state); err == nil {
		t.Fatal("packet-bound receipt without the canonical registry row was accepted")
	}
}

func TestReviewResultCheckpointRejectsPacketRemovedAfterOriginalIssue(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, packet, nil)
	state["entities"].(map[string]any)["work_packets"] = []any{}
	if _, err := validateReviewResultCheckpointIndices(root, state, journal); err == nil || !strings.Contains(err.Error(), "issuance event") {
		t.Fatalf("original S7 issuance with its packet removed was accepted: %v", err)
	}
}

func TestReviewResultCheckpointVerifiesPacketBoundDurableAcceptance(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	packetSHA, err := workpacket.ReviewResultPacketDigest(packet)
	if err != nil {
		t.Fatal(err)
	}
	claimResults := make([]any, 0, len(packet.Binding.ClaimIDs))
	for _, claimID := range packet.Binding.ClaimIDs {
		claimResults = append(claimResults, map[string]any{
			"claim_id": claimID, "conclusion": "pass", "method": "fixture method",
			"observed": "fixture observed the declared oracle", "evidence_refs": []string{},
		})
	}
	answer := map[string]any{
		"schema_version": "1.0.0", "result_id": packet.ResultID, "assignment_id": packet.Binding.AssignmentID,
		"assignment_revision": packet.Binding.ReviewPlanRevision, "review_plan_id": packet.Binding.ReviewPlanID,
		"review_round": packet.Binding.ReviewRound, "baseline_generation": packet.Binding.BaselineGeneration,
		"producer_agent_id": packet.Binding.OwnerAgentID, "subject_digest": packet.Binding.SubjectDigest,
		"verification_artifact_digest": nil, "claim_results": claimResults, "blocked_claims": []any{},
		"checks": []any{}, "findings": []any{}, "deviations": []any{}, "site_lost": []any{}, "verdict": "pass",
	}
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	answerDigest, err := reviewPacketProfessionalDigest(json.RawMessage(answerBytes))
	if err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": packet.ResultID, "kind": "review_result",
		"runtime_id": packet.Binding.RuntimeID, "baseline_generation": packet.Binding.BaselineGeneration,
		"review_round": packet.Binding.ReviewRound, "producer_agent_id": packet.Binding.OwnerAgentID,
		"review_plan_id": packet.Binding.ReviewPlanID, "assignment_id": packet.Binding.AssignmentID,
		"assignment_revision": packet.Binding.ReviewPlanRevision, "subject_digest": packet.Binding.SubjectDigest,
		"verification_artifact_digest": nil, "packet_id": packet.ID, "packet_sha256": packetSHA,
		"execution_root":   root,
		"submitted_answer": answer, "effective_answer": answer,
		"answer_sha256": answerDigest, "effective_answer_sha256": answerDigest,
	}
	resultBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	resultRef := ".claude/evidence/s7/result.json"
	if err := os.MkdirAll(filepath.Join(root, ".claude", "evidence", "s7"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(resultRef)), resultBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	resultSHA := sha256Bytes(resultBytes)
	receipt := checkpointReviewResultReceipt{
		Contract: "s7-review-result-receipt-v1", SubmissionKey: packet.ResultID,
		RuntimeID: packet.Binding.RuntimeID, BaselineGeneration: packet.Binding.BaselineGeneration,
		ReviewPlanID: packet.Binding.ReviewPlanID, ReviewPlanRef: packet.Binding.ReviewPlanRef,
		ReviewPlanSHA256: packet.Binding.ReviewPlanSHA256, ReviewPlanRevision: packet.Binding.ReviewPlanRevision,
		ReviewRound: packet.Binding.ReviewRound, AssignmentID: packet.Binding.AssignmentID,
		AssignmentRevision: packet.Binding.ReviewPlanRevision, AssignmentSHA256: packet.Binding.AssignmentSHA256,
		ProducerAgentID: packet.Binding.OwnerAgentID, SubjectDigest: packet.Binding.SubjectDigest,
		AnswerSHA256: answerDigest, EffectiveAnswerSHA256: answerDigest,
		ResultRef: resultRef, ResultSHA256: resultSHA, EventID: "evt-review-result-test",
		IdempotencyKey: "runtime:review-result:test", AcceptedAfterRevision: 3, AcceptedSequence: 2,
		PacketID: packet.ID, PacketSHA256: packetSHA,
		ExecutionRoot: root,
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var receiptRow map[string]any
	if err := json.Unmarshal(receiptBytes, &receiptRow); err != nil {
		t.Fatal(err)
	}
	issueCursor := map[string]any{"state": "verification", "phase": "review"}
	resultEvent := buildJournalEvent(Mutation{
		EventID: receipt.EventID, TransitionID: "REVIEW-RESULT", Event: "review_result_submitted",
		Actor: "orchestrator", IdempotencyKey: receipt.IdempotencyKey, RuntimeID: receipt.RuntimeID,
		BaselineGeneration: receipt.BaselineGeneration, From: issueCursor, To: issueCursor,
		EvidenceIDs: []string{packet.ResultID},
	}, packet.Binding.RuntimeID, 2, 3, 2, time.Date(2026, 10, 14, 0, 1, 0, 0, time.UTC))
	state, journal := checkpointTestReviewState(t, packet, []map[string]any{resultEvent})
	state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID] = map[string]any{
		"status": "consumed", "agent_id": packet.Binding.OwnerAgentID,
		"claim_ids":  []any{packet.Binding.ClaimIDs[0], packet.Binding.ClaimIDs[1]},
		"result_ref": resultRef, "result_receipt": receiptRow,
	}
	state["evidence"] = []any{map[string]any{
		"id": packet.ResultID, "kind": "review_result", "path": resultRef, "sha256": resultSHA,
		"baseline_generation": packet.Binding.BaselineGeneration, "review_round": packet.Binding.ReviewRound,
		"produced_by": []any{packet.Binding.OwnerAgentID}, "status": "valid",
	}}
	filtered, err := validateReviewResultCheckpointIndices(root, state, journal)
	if err != nil {
		t.Fatalf("valid packet-bound Result acceptance rejected: %v", err)
	}
	if len(filtered["entities"].(map[string]any)["work_packets"].([]any)) != 0 {
		t.Fatal("validated S7 packet was not removed from temporary S5 projection")
	}
	if err := rewriteCheckpointResultWithIncompleteClaims(root, resultRef, resultBytes, state, packet); err != nil {
		t.Fatal(err)
	}
	if _, err := validateReviewResultCheckpointIndices(root, state, journal); err == nil || !strings.Contains(err.Error(), "missing claim-reset") {
		t.Fatalf("consume event and recomputed receipt accepted an incomplete professional answer: %v", err)
	}
	restoredReceipt := state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)["result_receipt"].(map[string]any)
	restoredReceipt["result_sha256"] = resultSHA
	restoredReceipt["answer_sha256"] = answerDigest
	restoredReceipt["effective_answer_sha256"] = answerDigest
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = resultSHA
	corrupt := append([]byte(nil), resultBytes...)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(resultRef)), resultBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt = append(corrupt, []byte(" ")...)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(resultRef)), corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateReviewResultCheckpointIndices(root, state, journal); err == nil {
		t.Fatal("Result artifact byte drift was accepted")
	}
}

func TestS7ExecutionRootUsesExactRuntimeWorkspaceRegistration(t *testing.T) {
	mainRoot := t.TempDir()
	workerRoot := filepath.Join(t.TempDir(), "worker")
	packet := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{
		RuntimeID: "loop-runtime", BaselineGeneration: 3,
		AssignmentID: "assignment-e2e", OwnerAgentID: "agent-e2e",
	}}
	if err := validateS7ExecutionRoot(mainRoot, map[string]any{}, packet, mainRoot); err != nil {
		t.Fatalf("unregistered Main/native execution root rejected: %v", err)
	}
	if err := validateS7ExecutionRoot(mainRoot, map[string]any{}, packet, workerRoot); err == nil {
		t.Fatal("unregistered non-Main execution root was accepted")
	}
	registered := map[string]any{
		"main_root": mainRoot,
		"executions": map[string]any{
			packet.Binding.AssignmentID: map[string]any{
				"assignment_id":       packet.Binding.AssignmentID,
				"runtime_id":          packet.Binding.RuntimeID,
				"baseline_generation": packet.Binding.BaselineGeneration,
				"agent_id":            packet.Binding.OwnerAgentID,
				"worktree_path":       workerRoot,
				"status":              "ready",
			},
		},
	}
	if err := validateS7ExecutionRoot(mainRoot, map[string]any{"workspace": registered}, packet, workerRoot); err != nil {
		t.Fatalf("exact current Worker execution root rejected: %v", err)
	}
	if err := validateS7ExecutionRoot(mainRoot, map[string]any{"workspace": registered}, packet, mainRoot); err == nil {
		t.Fatal("declared Worker execution allowed the retired Main source")
	}
	registered["executions"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)["agent_id"] = "other-owner"
	if err := validateS7ExecutionRoot(mainRoot, map[string]any{"workspace": registered}, packet, workerRoot); err == nil {
		t.Fatal("Worker source with a different registered owner was accepted")
	}
}

func rewriteCheckpointResultWithIncompleteClaims(root, resultRef string, original []byte, state map[string]any, packet workpacket.ReviewResultPacket) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(original, &envelope); err != nil {
		return err
	}
	var answer map[string]any
	if err := json.Unmarshal(envelope["submitted_answer"], &answer); err != nil {
		return err
	}
	claims, _ := answer["claim_results"].([]any)
	if len(claims) < 2 {
		return errors.New("fixture requires at least two Claim answers")
	}
	answer["claim_results"] = claims[:1]
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	answerSHA, err := reviewPacketProfessionalDigest(answerBytes)
	if err != nil {
		return err
	}
	envelope["submitted_answer"] = answerBytes
	envelope["effective_answer"] = answerBytes
	envelope["answer_sha256"], _ = json.Marshal(answerSHA)
	envelope["effective_answer_sha256"], _ = json.Marshal(answerSHA)
	resultBytes, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(resultRef)), resultBytes, 0o600); err != nil {
		return err
	}
	resultSHA := sha256Bytes(resultBytes)
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	row := assignments[packet.Binding.AssignmentID].(map[string]any)
	receipt := row["result_receipt"].(map[string]any)
	receipt["result_sha256"] = resultSHA
	receipt["answer_sha256"] = answerSHA
	receipt["effective_answer_sha256"] = answerSHA
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = resultSHA
	return nil
}

func checkpointTestReviewPacket(t testing.TB, root string) workpacket.ReviewResultPacket {
	t.Helper()
	claimBytes := json.RawMessage(`[
		{"claim_id":"claim-login","lens":"qa","target":"login","assertion":"login works","oracle":"visible home","method":"submit credentials","required_evidence":[],"applicability":"required","na_rationale":"","source_refs":["REQ-041"],"focus_key":"","depends_on":[],"resource_locks":[]},
		{"claim_id":"claim-reset","lens":"qa","target":"reset","assertion":"reset works","oracle":"email sent","method":"request reset","applicability":"required","source_refs":["REQ-041"]}
	]`)
	subjectBytes := json.RawMessage(`[{"path":"docs/requirements/REQ-041.md","sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","kind":""}]`)
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
		"schema_version": "1.0.0", "review_plan_id": "review-plan-041-r1", "review_round": 1, "baseline_generation": 3,
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
		ReviewPlanSHA256: sha256Bytes(planBytes), ReviewPlanRevision: 2, ReviewRound: 1,
		AssignmentID: assignment.AssignmentID, AssignmentSHA256: sha256Bytes(assignmentBytes), OwnerAgentID: "reviewer-qa-1",
		ClaimIDs: []string{"claim-login", "claim-reset"}, SubjectDigest: strings.Repeat("c", 64),
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
				"sha256": packet.Binding.ReviewPlanSHA256, "revision": packet.Binding.ReviewPlanRevision,
				"review_round": packet.Binding.ReviewRound, "status": "running",
			},
			"assignments": map[string]any{packet.Binding.AssignmentID: map[string]any{
				"status": "dispatched", "agent_id": packet.Binding.OwnerAgentID,
				"claim_ids": []any{packet.Binding.ClaimIDs[0], packet.Binding.ClaimIDs[1]},
			}},
		},
	}
	return state, journal
}
