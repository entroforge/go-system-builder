package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

// This is a complete original S5 -> TR-003 -> S6 Main -> TR-006 -> S7
// RegisterPlan -> reviewer Register -> packet issue/submit chain.
// The S5 evidence and the answer strings below are mechanism fixtures: they
// prove the original writers, guards, receipts, and CleanRound consumer run;
// they do not claim that a real model performed the professional review.
func TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound(t *testing.T) {
	runS7IssuedResultPacketChain(t, false)
}

func TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound(t *testing.T) {
	runS7IssuedResultPacketChain(t, true)
}

// Product drift invalidates the pinned Plan through the original consumer's
// stale transaction. Keep that mutation isolated from the successful round.
func TestS7IssuedResultUnissuedSiblingMarksOriginalPlanStale(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	registeredPlan := registerCanonicalPlanForPacketTest(t, fixture)
	plan, _, err := review.LoadPlan(fixture.root, registeredPlan.State)
	if err != nil {
		t.Fatal(err)
	}
	owners := dispatchS7ReviewerAssignments(t, fixture, plan)
	packets, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
	if err != nil || len(packets) == 0 {
		t.Fatalf("issue original packet: count=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	owner := owners[packet.Binding.AssignmentID]
	answerPath := filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
	writeJSONFixture(t, answerPath, s7MechanismAnswer(t, fixture, packet))
	before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	beforeJournal, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	// An unissued sibling is product drift, not another control output.
	if err := os.WriteFile(filepath.Join(filepath.Dir(answerPath), "extra.json"), []byte(`{"unissued":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{
		ExpectedRevision: before.Revision, PacketID: packet.ID, AnswerPath: packet.AnswerPath,
		OwnerAgentID: owner, SourceRoot: fixture.root, OccurredAt: time.Now().UTC(),
	}); err == nil || !strings.Contains(err.Error(), "marked stale") {
		t.Fatalf("unissued sibling did not reach original stale rejection: %v", err)
	}
	after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	ptr := review.PlanPointerFromState(after.State)
	if after.Revision != before.Revision+1 || ptr == nil || ptr.Status != "stale" {
		t.Fatalf("original stale transaction missing: revision %d->%d plan=%+v", before.Revision, after.Revision, ptr)
	}
	afterJournal, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(afterJournal, beforeJournal) {
		t.Fatal("stale transaction rewrote journal history")
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(afterJournal[len(beforeJournal):]), &event); err != nil {
		t.Fatalf("expected exactly one stale event: %v", err)
	}
	if event["transition_id"] != "REVIEW-PLAN-STALE" || event["event"] != "transition_committed" || event["outcome"] != "committed" {
		t.Fatalf("wrong original rejection transaction: %v", event)
	}
	lastTransition, _ := after.State["last_transition"].(map[string]any)
	if lastTransition["event"] != "review_plan_stale" {
		t.Fatalf("original stale domain event missing: %v", lastTransition)
	}
	reviewState := after.State["review"].(map[string]any)
	for id, raw := range reviewState["assignments"].(map[string]any) {
		row := raw.(map[string]any)
		if row["result_ref"] != nil || row["result_receipt"] != nil {
			t.Fatalf("drift accepted a Result/receipt for %s: %v", id, row)
		}
	}
	for id, raw := range reviewState["claims"].(map[string]any) {
		if raw.(map[string]any)["disposition"] == "pass" {
			t.Fatalf("drift manufactured Claim PASS: %s", id)
		}
	}
	resultPath := filepath.Join(fixture.root, ".claude/evidence", packet.Binding.RuntimeID, fmt.Sprintf("g%d", packet.Binding.BaselineGeneration), "reviews", owner, packet.ResultID+".json")
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatalf("drift published an original Result artifact: %s err=%v", resultPath, err)
	}
}

func runS7IssuedResultPacketChain(t *testing.T, coldHooks bool) {
	t.Helper()
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	if coldHooks {
		fixture.hookBinary = factory.BuildHarness(t)
	}
	registeredPlan := registerCanonicalPlanForPacketTest(t, fixture)
	plan, _, err := review.LoadPlan(fixture.root, registeredPlan.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Assignments) == 0 {
		t.Fatal("mechanism fixture produced no S7 reviewer Assignments")
	}

	// The original wave gate permits behavior dispatch only after required
	// static Claims have actual dispositions from the original Result consumer.
	owners := dispatchS7ReviewerAssignments(t, fixture, plan, "static")
	issued, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
	if err != nil {
		t.Fatalf("original static S7 packet issuer: %v", err)
	}
	staticCount := len(owners)
	if staticCount == 0 || len(issued) != staticCount {
		t.Fatalf("issued %d packets for %d static Plan Assignments", len(issued), staticCount)
	}

	var firstPacket workpacket.ReviewResultPacket
	var firstOwner string
	var firstAcceptedAnswer workpacket.ReviewResultDraft
	for i := 0; i < len(issued); i++ {
		packet := issued[i]
		owner := owners[packet.Binding.AssignmentID]
		if owner == "" || packet.Binding.OwnerAgentID != owner {
			t.Fatalf("issued packet %s owner %q has no matching original workgroup dispatch", packet.ID, packet.Binding.OwnerAgentID)
		}
		if i == 0 {
			firstPacket, firstOwner = packet, owner
		}
		before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
		if i == 0 {
			assertS7IssuedNegativeCases(t, fixture, packet, owner, before.Revision)
		}

		answer := s7MechanismAnswer(t, fixture, packet)
		if i == 0 {
			firstAcceptedAnswer = answer
		}
		writeJSONFixture(t, filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath)), answer)
		concurrentRevision := 0
		if i == 0 {
			concurrentRevision = assertS7ConcurrentReceptionKeepsOriginalAnswer(t, fixture, packet, owner).Revision
		}
		var accepted loopruntime.Snapshot
		if coldHooks {
			accepted = runS7StopHookMode(t, fixture, packet, owner, true)
		} else {
			// Instrument the original consumer and Writer under race. Platform
			// time limits belong to the separate production-process chain.
			accepted, err = review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{
				Context: t.Context(), ExpectedRevision: -1, PacketID: packet.ID,
				AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: fixture.root, OccurredAt: time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("original S7 consumer rejected issued answer: %v", err)
			}
		}
		if concurrentRevision != 0 {
			if accepted.Revision != concurrentRevision {
				t.Fatal("normal delivery replay duplicated the concurrently accepted Result")
			}
		} else if accepted.Revision <= before.Revision {
			t.Fatalf("original SubmitIssuedResult did not commit a Runtime event for %s", packet.Binding.AssignmentID)
		}
		receipt, found, err := review.IssuedResultReceipt(fixture.root, accepted.State, packet.ID, owner)
		if err != nil || !found || receipt.PacketID != packet.ID || receipt.ProducerAgentID != owner || receipt.SubmissionKey != packet.ResultID || receipt.ResultRef == "" {
			t.Fatalf("original issued Result receipt for %s = %+v found=%t err=%v", packet.Binding.AssignmentID, receipt, found, err)
		}
		if i == 0 {
			assertS7PacketReceiptSchemaPairing(t, fixture.root, accepted.State, packet.Binding.AssignmentID)
			assertS7AcceptedPacketSourceNegatives(t, fixture, packet, owner, accepted.Revision)
		}

		// The real hook context reads the same accepted Runtime snapshot, then
		// the original stop/idle consumer proves this exact Result was consumed.
		loaded, err := hookctx.FromSnapshot(fixture.root, owner, accepted)
		if err != nil || loaded.PolicyContext.Agent == nil {
			t.Fatalf("load original reviewer context after reception: loaded=%+v err=%v", loaded, err)
		}
		if !hook.ReviewResultConsumed(fixture.root, accepted.State, loaded.PolicyContext.Agent) {
			t.Fatalf("original ReviewResultConsumed rejected accepted Result for %s", packet.Binding.AssignmentID)
		}
		if i+1 == staticCount {
			if !review.StaticClaimsSettled(accepted.State, plan) {
				t.Fatal("original static receptions did not settle the behavior prerequisite")
			}
			behaviorOwners := dispatchS7ReviewerAssignments(t, fixture, plan, "behavior")
			for assignment, owner := range behaviorOwners {
				owners[assignment] = owner
			}
			behaviorPackets, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
			if err != nil || len(behaviorPackets) != len(behaviorOwners) {
				t.Fatalf("original behavior issuance after static acceptance: packets=%d owners=%d err=%v", len(behaviorPackets), len(behaviorOwners), err)
			}
			issued = append(issued, behaviorPackets...)
		}
	}
	if len(issued) != len(plan.Assignments) {
		t.Fatalf("issued %d packets across both waves for %d Plan Assignments", len(issued), len(plan.Assignments))
	}

	final := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	pointer := review.PlanPointerFromState(final.State)
	if pointer == nil || pointer.Status != "clean" || final.State["review"].(map[string]any)["clean_round"] != float64(1) {
		t.Fatalf("original final ReviewResult did not produce CleanRound: pointer=%+v review=%#v", pointer, final.State["review"])
	}
	cleanEvidence := 0
	for _, raw := range final.State["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row != nil && row["kind"] == "clean_round" {
			cleanEvidence++
		}
	}
	if cleanEvidence != 1 {
		t.Fatalf("original CleanRound evidence count = %d, want one", cleanEvidence)
	}

	// Once accepted, the same packet key cannot acknowledge a changed answer.
	// It must conflict without changing Runtime state; then restore the exact
	// accepted bytes so the next check exercises safe replay.
	answerPath := filepath.Join(fixture.root, filepath.FromSlash(firstPacket.AnswerPath))
	changed := firstAcceptedAnswer
	var changedAnswer map[string]any
	if err := json.Unmarshal(changed.Answer, &changedAnswer); err != nil {
		t.Fatal(err)
	}
	claimRows := changedAnswer["claim_results"].([]any)
	claim := claimRows[0].(map[string]any)
	claim["observed"] = "changed after the packet was accepted"
	changedBytes, err := json.Marshal(changedAnswer)
	if err != nil {
		t.Fatal(err)
	}
	changed.Answer = changedBytes
	writeJSONFixture(t, answerPath, changed)
	stateBeforeConflict, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBeforeConflict, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	// A new explicit save still validates the current professional bytes,
	// including after the public flow has moved to S10. A late Stop itself
	// does not reopen an already closed Reviewer responsibility.
	if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{Context: t.Context(), ExpectedRevision: -1, PacketID: firstPacket.ID, AnswerPath: firstPacket.AnswerPath, OwnerAgentID: firstOwner, SourceRoot: fixture.root}); err == nil || !strings.Contains(err.Error(), "conflicts with the originally accepted professional answer") {
		t.Fatalf("new explicit save of a same-key changed answer did not conflict: %v", err)
	}
	code, conflictStdout, conflictStderr := invokeS7StopHookMode(t, fixture, firstPacket, firstOwner, coldHooks)
	priorLifecycle, _ := final.State["lifecycle"].(map[string]any)
	if code != 0 || priorLifecycle["state"] == "verification" && !strings.Contains(conflictStderr, "conflicts with the originally accepted professional answer") {
		t.Fatalf("original Stop lost active-reception/closed-responsibility boundary: stdout=%s stderr=%s", conflictStdout, conflictStderr)
	}
	stateAfterConflict, _ := os.ReadFile(fixture.statePath)
	journalAfterConflict, _ := os.ReadFile(fixture.journalPath)
	if string(stateBeforeConflict) != string(stateAfterConflict) || string(journalBeforeConflict) != string(journalAfterConflict) {
		t.Fatal("same-key changed S7 answer conflict changed Runtime state or journal")
	}
	writeJSONFixture(t, answerPath, firstAcceptedAnswer)

	// The direct-consumer variant leaves TR-009 to its first public Stop.
	// The cold variant has already run that Stop while receiving the last
	// answer. Both must reach acceptance before exact public replay is tested.
	originalReceipt, found, err := review.IssuedResultReceipt(fixture.root, final.State, firstPacket.ID, firstOwner)
	if err != nil || !found {
		t.Fatalf("original accepted receipt unavailable before outlet: %v", err)
	}
	promoted := runS7StopHookMode(t, fixture, firstPacket, firstOwner, coldHooks)
	lifecycle, _ := promoted.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "acceptance" {
		t.Fatalf("first public Stop did not complete original TR-009: %#v", lifecycle)
	}
	promotedReceipt, found, err := review.IssuedResultReceipt(fixture.root, promoted.State, firstPacket.ID, firstOwner)
	if err != nil || !found || !reflect.DeepEqual(originalReceipt, promotedReceipt) {
		t.Fatalf("outlet rewrote original reception: %+v -> %+v (%v)", originalReceipt, promotedReceipt, err)
	}
	events, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanEvidenceID := ""
	for _, raw := range final.State["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row["kind"] == "clean_round" && row["status"] == "valid" {
			cleanEvidenceID, _ = row["id"].(string)
		}
	}
	if cleanEvidenceID == "" {
		t.Fatal("original CleanRound has no valid evidence ID")
	}
	catalogue, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	gateSpec, exists := registry.Lookup("GATE-VERIFY-CLEAN-ROUND-PASSED")
	if !exists {
		t.Fatal("original CleanRound gate definition is missing")
	}
	producers := map[string]bool{}
	for _, requirement := range gateSpec.EvidenceRequirements {
		for _, role := range requirement.Responsibilities {
			producers[role] = true
		}
	}
	exits := 0
	for _, line := range bytes.Split(events, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event["transition_id"] == "TR-009" {
			exits++
			fingerprint, _ := event["gate_fingerprint"].(string)
			if event["event"] != "transition_committed" || event["outcome"] != "committed" || event["gate_id"] != "GATE-VERIFY-CLEAN-ROUND-PASSED" || fingerprint == "" || fingerprint == "sha256:manual" || !producers[fmt.Sprint(event["producer_responsibility"])] {
				t.Fatalf("public exit lost original Gate/Writer evidence: %+v", event)
			}
			hasCleanEvidence := false
			for _, id := range event["evidence_ids"].([]any) {
				hasCleanEvidence = hasCleanEvidence || id == cleanEvidenceID
			}
			if !hasCleanEvidence {
				t.Fatalf("TR-009 did not consume its original CleanRound evidence %s: %+v", cleanEvidenceID, event)
			}
			guards, _ := event["guard_results"].([]any)
			if len(guards) == 0 {
				t.Fatal("public exit recorded no original guard results")
			}
			for _, raw := range guards {
				guard, _ := raw.(map[string]any)
				if guard["result"] != "pass" {
					t.Fatalf("TR-009 committed without passing original guard: %+v", guard)
				}
			}
		}
	}
	if exits != 1 {
		t.Fatalf("original public handoff committed %d TR-009 events", exits)
	}
	stateBefore, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore := append([]byte(nil), events...)
	replayed := runS7StopHookMode(t, fixture, firstPacket, firstOwner, coldHooks)
	if replayed.Revision != promoted.Revision {
		t.Fatalf("completed public handoff replay changed revision: %d -> %d", promoted.Revision, replayed.Revision)
	}
	stateAfter, _ := os.ReadFile(fixture.statePath)
	journalAfter, _ := os.ReadFile(fixture.journalPath)
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("completed public handoff replay wrote Runtime or journal")
	}

}

// Inject a real peer commit and an editor's later answer after the service
// captured this attempt. The bounded original CAS continuation must accept the
// captured answer, preserve the peer, and leave a new explicit save distinct.
func assertS7ConcurrentReceptionKeepsOriginalAnswer(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string) loopruntime.Snapshot {
	t.Helper()
	service, err := submission.NewReadOnly(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	answerPath := filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
	original, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatal(err)
	}
	var changed workpacket.ReviewResultDraft
	if err := json.Unmarshal(original, &changed); err != nil {
		t.Fatal(err)
	}
	var professional map[string]any
	if err := json.Unmarshal(changed.Answer, &professional); err != nil {
		t.Fatal(err)
	}
	firstClaim := professional["claim_results"].([]any)[0].(map[string]any)
	originalObservation := firstClaim["observed"]
	firstClaim["observed"] = "editor saved a different professional observation after the original input was frozen"
	changed.Answer, err = json.Marshal(professional)
	if err != nil {
		t.Fatal(err)
	}
	changedBytes, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	beforeJournal, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	authorizationCalls := 0
	receipt, err := service.SaveReviewResultFrom(t.Context(), owner, answerPath, fixture.root, func(root string, state map[string]any) error {
		authorizationCalls++
		writer := loopruntime.NewWriter(service.StatePath, service.JournalPath, root, service.Validator).WithContext(t.Context())
		_, err := writer.UpdateCurrent(loopruntime.Mutation{
			EventID: "evt-s7-unrelated-before-reception", TransitionID: "S7-RECEPTION-COORDINATION-PROBE", Event: "test_unrelated_commit", Actor: "orchestrator",
			IdempotencyKey: "s7-unrelated-before-reception", RuntimeID: canonicalRuntimeID, BaselineGeneration: 1,
			Apply: func(current map[string]any) error {
				current["milestone"].(map[string]any)["objective"] = "Main coordinates another unchanged responsibility"
				return nil
			},
		})
		if err != nil {
			return err
		}
		return os.WriteFile(answerPath, changedBytes, 0o600)
	})
	if err != nil || !receipt.Accepted || len(receipt.FieldErrors) != 0 || authorizationCalls != 1 {
		t.Fatalf("bounded original reception: receipt=%+v authorizations=%d err=%v", receipt, authorizationCalls, err)
	}
	after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if after.Revision != before.Revision+2 || after.State["milestone"].(map[string]any)["objective"] != "Main coordinates another unchanged responsibility" {
		t.Fatalf("original answer/peer facts not each committed once: %d -> %d", before.Revision, after.Revision)
	}
	originalReceipt, found, err := review.IssuedResultReceipt(fixture.root, after.State, packet.ID, owner)
	if err != nil || !found || originalReceipt.SubmissionKey != packet.ResultID {
		t.Fatalf("original accepted receipt missing: found=%t err=%v", found, err)
	}
	envelopeBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(originalReceipt.ResultRef)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	submitted := envelope["submitted_answer"].(map[string]any)
	if submitted["claim_results"].([]any)[0].(map[string]any)["observed"] != originalObservation {
		t.Fatal("bounded reception consumed the editor's new bytes instead of its fixed original answer")
	}
	onDisk, err := os.ReadFile(answerPath)
	if err != nil || !bytes.Equal(onDisk, changedBytes) {
		t.Fatalf("receiver overwrote the editor's subsequent answer: %v", err)
	}
	afterJournal, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(afterJournal, beforeJournal) {
		t.Fatal("concurrent reception rewrote original journal history")
	}
	events := bytes.Split(bytes.TrimSpace(afterJournal[len(beforeJournal):]), []byte("\n"))
	if len(events) != 2 {
		t.Fatalf("bounded original reception appended %d events, want one peer and one Result", len(events))
	}
	peerEvents, resultEvents := 0, 0
	for _, data := range events {
		var event map[string]any
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		switch event["transition_id"] {
		case "S7-RECEPTION-COORDINATION-PROBE":
			peerEvents++
		case "REVIEW-RESULT":
			resultEvents++
		}
	}
	if peerEvents != 1 || resultEvents != 1 {
		t.Fatalf("peer/result event counts=%d/%d", peerEvents, resultEvents)
	}
	stateAfter := mustReadFixtureBytes(t, fixture.statePath)
	conflict, err := service.SaveReviewResultFrom(t.Context(), owner, answerPath, fixture.root, nil)
	if err == nil || conflict.Accepted || len(conflict.FieldErrors) != 0 || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("new explicit answer was hidden by the original receipt: receipt=%+v err=%v", conflict, err)
	}
	if !bytes.Equal(stateAfter, mustReadFixtureBytes(t, fixture.statePath)) || !bytes.Equal(afterJournal, mustReadFixtureBytes(t, fixture.journalPath)) {
		t.Fatal("conflicting new input changed original Runtime facts")
	}
	if err := os.WriteFile(answerPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	replay, err := service.SaveReviewResultFrom(t.Context(), owner, answerPath, fixture.root, nil)
	if err != nil || !replay.Accepted || replay.SubmissionKey != receipt.SubmissionKey {
		t.Fatalf("same original answer did not replay: receipt=%+v err=%v", replay, err)
	}
	if !bytes.Equal(stateAfter, mustReadFixtureBytes(t, fixture.statePath)) || !bytes.Equal(afterJournal, mustReadFixtureBytes(t, fixture.journalPath)) {
		t.Fatal("same-answer replay duplicated accepted facts")
	}
	return after
}

func mustReadFixtureBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertS7PacketReceiptSchemaPairing(t *testing.T, root string, state map[string]any, assignmentID string) {
	t.Helper()
	validator := schema.NewValidator(root)
	validate := func(value map[string]any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return validator.ValidateBytes("loop-state-v1.2.schema.json", data)
	}
	clone := func() map[string]any {
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	findReceipt := func(value map[string]any) map[string]any {
		reviewState := value["review"].(map[string]any)
		assignments := reviewState["assignments"].(map[string]any)
		return assignments[assignmentID].(map[string]any)["result_receipt"].(map[string]any)
	}
	if err := validate(clone()); err != nil {
		t.Fatalf("valid issued packet receipt failed v1.2 schema: %v", err)
	}
	executionRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	executionRoot, err = filepath.Abs(executionRoot)
	if err != nil || findReceipt(clone())["execution_root"] != executionRoot {
		t.Fatalf("S7 receipt execution_root = %v, want canonical actual source %q (err %v)", findReceipt(clone())["execution_root"], executionRoot, err)
	}
	partial := clone()
	delete(findReceipt(partial), "packet_sha256")
	if err := validate(partial); err == nil {
		t.Fatal("v1.2 schema accepted a packet_id without packet_sha256 and execution_root")
	}
	missingExecutionRoot := clone()
	delete(findReceipt(missingExecutionRoot), "execution_root")
	if err := validate(missingExecutionRoot); err == nil {
		t.Fatal("v1.2 schema accepted an issued packet receipt without execution_root")
	}
	missingPacketID := clone()
	delete(findReceipt(missingPacketID), "packet_id")
	if err := validate(missingPacketID); err == nil {
		t.Fatal("v1.2 schema accepted execution_root without its packet identity")
	}
	legacy := clone()
	legacyReceipt := findReceipt(legacy)
	delete(legacyReceipt, "packet_id")
	delete(legacyReceipt, "packet_sha256")
	delete(legacyReceipt, "execution_root")
	if err := validate(legacy); err != nil {
		t.Fatalf("legacy receipt without packet fields failed v1.2 schema: %v", err)
	}
}

func assertS7AcceptedPacketSourceNegatives(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, revision int) {
	t.Helper()
	stateBefore, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRequest := review.IssuedSubmitRequest{
		ExpectedRevision: revision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner,
	}
	wrongRoot := t.TempDir()
	wrongRootRequest := baseRequest
	wrongRootRequest.SourceRoot = wrongRoot
	if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, wrongRootRequest); err == nil {
		t.Fatal("accepted S7 packet replay authorized a different physical source root")
	}
	registeredSourceRequest := baseRequest
	registeredSourceRequest.SourceRoot = fixture.root
	if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, registeredSourceRequest); err != nil {
		t.Fatalf("exact S7 packet replay required a Hook-side source authorization callback: %v", err)
	}
	stateAfter, _ := os.ReadFile(fixture.statePath)
	journalAfter, _ := os.ReadFile(fixture.journalPath)
	if string(stateBefore) != string(stateAfter) || string(journalBefore) != string(journalAfter) {
		t.Fatal("rejected accepted-packet source replay changed Runtime state or journal")
	}
}

func enablePacketModeAndApplyTR003(t *testing.T, root, statePath, journalPath string) {
	t.Helper()
	// Reviewer source assets are part of the physical Main fixture from its
	// original TR-003 baseline onward. This keeps later registration from
	// looking like product drift while still using the normal installed asset
	// authority for the dispatch records.
	for _, role := range []string{"delivery-verifier", "document-verifier", "e2e-tester", "qa", "test-builder"} {
		copyFixtureFile(t, filepath.Join(sourceRepositoryRoot(t), "agents", role+".md"), filepath.Join(root, ".claude", "agents", role+".md"))
	}
	gitFixture(t, root, "add", ".claude/agents")
	gitFixture(t, root, "commit", "-m", "Install S7 reviewer role sources before TR-003")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	configuration, _ := state["configuration"].(map[string]any)
	if configuration == nil {
		configuration = map[string]any{}
		state["configuration"] = configuration
	}
	configuration["optimization"] = map[string]any{"packet": true}
	writeJSONFixture(t, statePath, state)
	seedCanonicalS5AndApplyTR003(t, root, statePath, journalPath)
}

func registerCanonicalPlanForPacketTest(t *testing.T, fixture *canonicalMainFixture) loopruntime.Snapshot {
	t.Helper()
	ctx := context.Background()
	inspection := fixture.inspect(t, "")
	if !inspection.Ready {
		t.Fatalf("original Main Inspect before S7 = blockers %v", inspection.Blockers)
	}
	if integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("original Main Integrate before S7 = state %q err %v", integrated.Checkpoint.State, err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	catalogue, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fileview.New(fixture.root, "refs/heads/main", catalogue.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(ctx, qualitygate.Input{
		Root: fixture.root, Snapshot: current, Files: files, GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("original TR-006 Gate: status=%q missing=%v conflicts=%v err=%v", gate.Status, gate.Missing, gate.Conflicts, err)
	}
	started, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
		Files: files, TransitionID: "TR-006", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)},
		GateID:   gate.GateID, GateFingerprint: gate.Fingerprint, ProducerResponsibility: "delivery_orchestrator",
	})
	if err != nil {
		t.Fatalf("original TR-006 Writer: %v", err)
	}
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State, 1)
	if draft == nil || draft.ProductBaseline == nil {
		t.Fatalf("source-backed ReviewPlan draft missing product baseline: plan=%+v notes=%v", draft, notes)
	}
	// This fixture declares the actual command-visible product marker scenario;
	// it supplies no browser inventory and does not claim real model review.
	authorCanonicalMechanismBehavior(draft, "e2e-workspace/s7-packet-fixture")
	for i := range draft.Claims {
		claim := &draft.Claims[i]
		if !strings.Contains(claim.Oracle, "TODO(planner)") {
			continue
		}
		if claim.Lens == "delivery" {
			claim.Oracle = fmt.Sprintf("Trace the locked %s acceptance obligations through the accepted implementation and verified Main change at %s.", canonicalTaskID, canonicalSourceFile)
		} else {
			claim.Oracle = fmt.Sprintf("Inspect the frozen Main change at %s against this Claim and record the concrete supporting or refuting behavior.", canonicalSourceFile)
		}
	}
	planPath := filepath.Join(fixture.root, ".claude", "evidence", "review-plan-s7-packet-fixture.json")
	writeJSONFixture(t, planPath, draft)
	registered, err := review.RegisterPlan(fixture.root, fixture.statePath, fixture.journalPath, review.PlanRequest{
		ExpectedRevision: started.Revision, PlanPath: planPath, OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("original S7 RegisterPlan: %v", err)
	}
	return registered
}

func dispatchS7ReviewerAssignments(t *testing.T, fixture *canonicalMainFixture, plan *review.Plan, waves ...string) map[string]string {
	t.Helper()
	wave := "static"
	if len(waves) != 0 {
		wave = waves[0]
	}
	owners := make(map[string]string, len(plan.Assignments))
	for _, planned := range plan.Assignments {
		if planned.ExecutionWave != wave {
			continue
		}
		manifest, _, err := review.DraftManifest(fixture.root, readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).State, planned.AssignmentID)
		if err != nil {
			t.Fatalf("original reviewer manifest draft for %s: %v", planned.AssignmentID, err)
		}
		owner := "agent-s7-packet-" + strings.TrimPrefix(planned.AssignmentID, "assignment-")
		owners[planned.AssignmentID] = owner
		manifest.Assignments[0].AgentID = owner
		manifestPath := filepath.Join(fixture.root, ".claude", "workgroups", "manifest-s7-packet-"+strings.TrimPrefix(planned.AssignmentID, "assignment-")+".json")
		writeJSONFixture(t, manifestPath, manifest)

		definition := manifest.Assignments[0].AgentDefinitionRef
		definitionBase := filepath.Base(definition)
		manifest.Assignments[0].AgentDefinitionRef = filepath.ToSlash(filepath.Join(".claude", "agents", definitionBase))

		before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
		registered, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{
			ExpectedRevision: before.Revision, ManifestPath: manifestPath,
			TaskID: canonicalTaskID, TaskPath: filepath.Join(fixture.root, filepath.FromSlash(canonicalTask)),
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("original reviewer workgroup Register for %s: %v", planned.AssignmentID, err)
		}
		row := registered.State["review"].(map[string]any)["assignments"].(map[string]any)[planned.AssignmentID].(map[string]any)
		if row["status"] != "dispatched" || row["agent_id"] != owner {
			t.Fatalf("original workgroup did not dispatch exact Plan Assignment %s: %#v", planned.AssignmentID, row)
		}
	}
	return owners
}

func assertS7IssuedNegativeCases(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, expectedRevision int) {
	t.Helper()
	valid := s7MechanismAnswer(t, fixture, packet)
	answerPath := filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
	writeJSONFixture(t, answerPath, valid)
	otherRoot := t.TempDir()
	for _, tc := range []struct {
		name    string
		request review.IssuedSubmitRequest
	}{
		{name: "wrong owner", request: review.IssuedSubmitRequest{ExpectedRevision: expectedRevision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: "agent-s7-packet-wrong", SourceRoot: fixture.root}},
		{name: "wrong source checkout", request: review.IssuedSubmitRequest{ExpectedRevision: expectedRevision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: otherRoot}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, tc.request); err == nil {
				t.Fatal("invalid identity/source reached the original Result Writer")
			}
			if got := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).Revision; got != expectedRevision {
				t.Fatalf("rejected %s changed Runtime revision to %d", tc.name, got)
			}
		})
	}

	// The exact questionnaire is immutable. Editing its question text cannot
	// be converted into an accepted professional answer.
	edited := packet.Draft
	edited.Questions = make(map[string]workpacket.Question, len(packet.Draft.Questions))
	for key, value := range packet.Draft.Questions {
		edited.Questions[key] = value
	}
	editedQuestion := edited.Questions["verdict"]
	editedQuestion.Question = "Edited question that was never issued"
	edited.Questions["verdict"] = editedQuestion
	edited.Answer = valid.Answer
	writeJSONFixture(t, answerPath, edited)
	if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{
		ExpectedRevision: expectedRevision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: fixture.root,
	}); err == nil {
		t.Fatal("edited S7 questionnaire reached the original Result Writer")
	}

	// Omit one owned Claim while keeping the rest of the answer valid.
	var raw map[string]any
	if err := json.Unmarshal(valid.Answer, &raw); err != nil {
		t.Fatal(err)
	}
	claimRows := raw["claim_results"].([]any)
	if len(claimRows) < 1 {
		t.Fatalf("mechanism packet has no Claims: %s", valid.Answer)
	}
	raw["claim_results"] = claimRows[:len(claimRows)-1]
	missing, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	missingDraft := packet.Draft
	missingDraft.Answer = missing
	writeJSONFixture(t, answerPath, missingDraft)
	if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{
		ExpectedRevision: expectedRevision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: fixture.root,
	}); err == nil {
		t.Fatal("answer missing an assigned Claim reached the original Result Writer")
	}
	if got := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).Revision; got != expectedRevision {
		t.Fatalf("invalid questionnaire/Claim answers changed Runtime revision to %d", got)
	}

	// The old untyped direct submit API still routes to the original consumer,
	// which rejects a direct Result while this Assignment has an issued packet.
	var professional map[string]any
	if err := json.Unmarshal(valid.Answer, &professional); err != nil {
		t.Fatal(err)
	}
	direct := map[string]any{
		"schema_version": "1.0.0", "result_id": packet.ResultID, "assignment_id": packet.Binding.AssignmentID,
		"assignment_revision": packet.Binding.ReviewPlanRevision, "review_plan_id": packet.Binding.ReviewPlanID,
		"review_round": packet.Binding.ReviewRound, "baseline_generation": packet.Binding.BaselineGeneration,
		"producer_agent_id": owner, "subject_digest": packet.Binding.SubjectDigest,
		"verification_artifact_digest": nil, "claim_results": professional["claim_results"],
		"blocked_claims": professional["blocked_claims"], "checks": professional["checks"],
		"findings": professional["findings"], "deviations": professional["deviations"],
		"site_lost": professional["site_lost"], "verdict": professional["verdict"],
	}
	directPath := filepath.Join(fixture.root, ".claude", "evidence", "direct-s7-bypass.json")
	writeJSONFixture(t, directPath, direct)
	if _, err := review.SubmitResult(fixture.root, fixture.statePath, fixture.journalPath, review.SubmitRequest{
		ExpectedRevision: expectedRevision, AssignmentID: packet.Binding.AssignmentID, ResultPath: directPath,
	}); err == nil {
		t.Fatal("direct SubmitResult bypassed its active frozen S7 packet")
	}
	if got := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).Revision; got != expectedRevision {
		t.Fatalf("direct-submit bypass attempt changed Runtime revision to %d", got)
	}
	writeJSONFixture(t, answerPath, valid)
}

func s7MechanismAnswer(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket) workpacket.ReviewResultDraft {
	t.Helper()
	var answer map[string]any
	if err := json.Unmarshal(packet.Draft.Answer, &answer); err != nil {
		t.Fatal(err)
	}
	claimRows, _ := answer["claim_results"].([]any)
	if len(claimRows) != len(packet.Binding.ClaimIDs) {
		t.Fatalf("draft has %d claim answers for %d frozen Claims", len(claimRows), len(packet.Binding.ClaimIDs))
	}
	evidenceRel := filepath.ToSlash(filepath.Join(".claude", "evidence", "s7-mechanism", packet.Binding.AssignmentID+".txt"))
	evidenceBytes := []byte("mechanism fixture only; this source exercises the original S7 receipt and CleanRound machinery, not a real professional review\n")
	writeFixtureFile(t, fixture.root, evidenceRel, evidenceBytes)
	evidenceRef := fmt.Sprintf("path:%s#sha256=%s", evidenceRel, digestHex(evidenceBytes))
	for _, raw := range claimRows {
		row, _ := raw.(map[string]any)
		row["conclusion"] = "pass"
		row["observed"] = "mechanism fixture observed the registered Claim/receipt path; not a product-review assertion"
		row["evidence_refs"] = []string{evidenceRef}
	}
	answer["verdict"] = "pass"
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	valid := packet.Draft
	valid.Answer = answerBytes
	return valid
}

func runS7StopHook(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string) loopruntime.Snapshot {
	return runS7StopHookMode(t, fixture, packet, owner, false)
}

func runS7StopHookMode(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, cold bool) loopruntime.Snapshot {
	t.Helper()
	code, stdoutText, stderrText := invokeS7StopHookMode(t, fixture, packet, owner, cold)
	if code != 0 {
		t.Fatalf("original S7 SubagentStop hook exit=%d stdout=%s stderr=%s", code, stdoutText, stderrText)
	}
	snapshot, snapshotErr := loopruntime.NewStore(fixture.statePath, fixture.journalPath).WithContext(t.Context()).Snapshot()
	if snapshotErr != nil {
		detail := map[string]any{"cold_process": cold, "packet_id": packet.ID, "owner": owner, "snapshot_error": snapshotErr.Error(), "exit_code": code, "stdout": stdoutText, "stderr": stderrText}
		for name, path := range map[string]string{"pending": fixture.statePath + ".commit-pending.json", "events": fixture.journalPath, "state": fixture.statePath} {
			file, err := os.Open(path)
			if err != nil {
				detail[name+"_error"] = err.Error()
				continue
			}
			info, statErr := file.Stat()
			data, readErr := io.ReadAll(io.LimitReader(file, 4<<20))
			tail := data
			if statErr == nil {
				detail[name+"_size"] = info.Size()
				detail[name+"_artifact_prefix_truncated"] = info.Size() > int64(len(data))
				size := min(info.Size(), int64(8192))
				tail = make([]byte, int(size))
				if _, err := file.ReadAt(tail, info.Size()-size); err != nil && err != io.EOF {
					detail[name+"_tail_error"] = err.Error()
				}
			} else {
				detail[name+"_stat_error"] = statErr.Error()
			}
			_ = file.Close()
			if readErr != nil {
				detail[name+"_error"] = readErr.Error()
			}
			if directory := os.Getenv("GSB_S7_FAILURE_DIR"); directory != "" {
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Logf("failure evidence directory: %v", err)
				} else {
					_ = os.WriteFile(filepath.Join(directory, name+".json"), data, 0600)
				}
			}
			if len(tail) > 8192 {
				tail = tail[len(tail)-8192:]
			}
			detail[name+"_tail"] = string(tail)
		}
		encoded, _ := json.MarshalIndent(detail, "", "  ")
		if directory := os.Getenv("GSB_S7_FAILURE_DIR"); directory != "" {
			_ = os.WriteFile(filepath.Join(directory, "hook-failure.json"), encoded, 0600)
		}
		t.Fatalf("original public S7 handoff left an unreadable durable pair; do not hide it by automatic test recovery: %s", encoded)
	}
	if _, found, err := review.IssuedResultReceipt(fixture.root, snapshot.State, packet.ID, owner); err != nil || !found {
		t.Fatalf("original hook did not persist an exact S7 receipt for %s: found=%t err=%v stdout=%s stderr=%s", packet.ID, found, err, stdoutText, stderrText)
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] == "verification" && !strings.Contains(stderrText, "original consumer") {
		t.Fatalf("active S7 Stop did not report original reception: stdout=%s stderr=%s", stdoutText, stderrText)
	}
	// After TR-009, a late Reviewer Stop only projects the new global stage.
	// The exact durable receipt above, not a repeated reception phrase, proves
	// the old work was accepted. Callers separately verify replay has no writes.
	return snapshot
}

func invokeS7StopHook(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string) (int, string, string) {
	return invokeS7StopHookMode(t, fixture, packet, owner, false)
}

func invokeS7StopHookMode(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, cold bool) (int, string, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"hook_event_name":        "SubagentStop",
		"last_assistant_message": filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath)),
		// The exact registered answer path supplies the professional owner and
		// source root. Hook actor, cwd, and session metadata are not required to
		// prove those already registered facts.
		"agent_id": owner + "-unrelated-hook-payload", "cwd": filepath.Join(fixture.root, "not-main"), "session_id": "unrelated-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cold {
		return factory.RunHook(t, fixture.hookBinary, fixture.root, "SubagentStop", payload)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "SubagentStop"}, bytes.NewReader(payload), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// parseS7HookReceipt retains the legacy wire-schema compatibility fixture.
// Native SubagentStop reports reception through the original durable receipt.
func parseS7HookReceipt(t *testing.T, stdout string) (bool, string) {
	t.Helper()
	var envelope struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode PostToolUse hook envelope: %v (%s)", err, stdout)
	}
	const prefix = "S7 SUBMISSION RECEIPT: "
	context := envelope.HookSpecificOutput.AdditionalContext
	if !strings.Contains(context, prefix) {
		t.Fatalf("PostToolUse hook has no S7 receipt context: %s", stdout)
	}
	start := strings.Index(context, prefix) + len(prefix)
	receiptBytes := []byte(strings.TrimSpace(context[start:]))
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", receiptBytes); err != nil {
		t.Fatalf("public S7 receipt differs from the shared schema: %v\n%s", err, receiptBytes)
	}
	var receipt struct {
		Accepted bool   `json:"accepted"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode S7 hook receipt: %v (%s)", err, context[start:])
	}
	return receipt.Accepted, receipt.Error
}

func TestParseS7HookReceiptDecodesBothJSONLayers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted bool
		errText  string
	}{
		{name: "accepted", accepted: true},
		{name: "conflict", accepted: false, errText: "submission key conflicts with the originally accepted professional answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := submission.Receipt{
				PacketID: "packet-" + strings.Repeat("a", 64), Accepted: tc.accepted, Error: tc.errText,
				ResponsibilityStatus: "not_accepted", CurrentStage: "S7", NextAction: "Review the original receipt.",
				DomainRefs: []string{}, WaitingOn: []submission.Waiting{}, FieldErrors: []workpacket.FieldError{},
			}
			if tc.accepted {
				value.ResponsibilityStatus, value.WorkComplete = "accepted", true
				value.SubmissionKey = "review-result-" + strings.Repeat("a", 64)
				value.DomainRefs = []string{"review-result.json"}
			}
			receiptBytes, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			outerBytes, err := json.Marshal(map[string]any{
				"hookSpecificOutput": map[string]any{
					"additionalContext": "S7 SUBMISSION RECEIPT: " + string(receiptBytes),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			accepted, gotError := parseS7HookReceipt(t, string(outerBytes))
			if accepted != tc.accepted || gotError != tc.errText {
				t.Fatalf("parsed S7 receipt accepted=%t error=%q, want accepted=%t error=%q", accepted, gotError, tc.accepted, tc.errText)
			}
		})
	}
}

func authorCanonicalMechanismBehavior(plan *review.Plan, workspace string) {
	plan.E2ECoverageState = "cold_start"
	plan.VerificationArtifactWorkspace = &workspace
	for i := range plan.Assignments {
		if plan.Assignments[i].Lens == "e2e" {
			plan.Assignments[i].BehaviorSurface = "non_browser"
		}
	}
	for i := range plan.Claims {
		claim := &plan.Claims[i]
		if claim.Lens != "e2e" {
			continue
		}
		claim.Target = canonicalSourceFile + " through the non-browser marker-check command"
		claim.Assertion = "The accepted Main product exposes the exact main-check-marker line to a read-only command without changing the file"
		claim.Oracle = "grep -qx 'main-check-marker' " + canonicalSourceFile + " exits 0 for the accepted product marker; a missing or altered marker exits nonzero, and the product bytes remain unchanged"
		claim.Method = "Run the declared grep command against the frozen Main product checkout; record its actual exit status and compare the file bytes before and after. No browser execution or model review is inferred by this mechanism fixture."
	}
}
