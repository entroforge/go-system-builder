package investigation_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

type s8OwnerFixture struct {
	hypothesisID string
	assignmentID string
	ownerID      string
	teamID       string
	taskID       string
	dispatchMode string
}

func TestS8IssueUsesActualGenerationTwoInvestigatorRegistration(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	if len(owners) != 1 {
		t.Fatalf("registered owners = %d, want one", len(owners))
	}
	registered, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(fixture.root, ".claude", "workgroups", "manifest-s8-g2-01.json")
	originalManifest := mustRead(t, manifestPath)
	var changedManifest map[string]any
	if err := json.Unmarshal(originalManifest, &changedManifest); err != nil {
		t.Fatal(err)
	}
	changedManifest["quantity_rationale"] = "changed after the original ENTITY-REGISTER source was frozen"
	writeInvestigationJSON(t, manifestPath, changedManifest)
	stateBeforeManifestDrift := mustRead(t, fixture.statePath)
	journalBeforeManifestDrift := mustRead(t, fixture.journalPath)
	if _, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, registered.Revision, owners[0].ownerID); err == nil || !strings.Contains(err.Error(), "original registration digest") {
		t.Fatalf("post-registration manifest edit was accepted as original dispatch source: %v", err)
	}
	if !bytes.Equal(stateBeforeManifestDrift, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeManifestDrift, mustRead(t, fixture.journalPath)) {
		t.Fatal("manifest source drift rejection changed Runtime state or journal")
	}
	writeInvestigationBytes(t, fixture.root, ".claude/workgroups/manifest-s8-g2-01.json", originalManifest)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, registered.Revision, owners[0].ownerID)
	if err != nil {
		t.Fatalf("issue from original generation-2 registration: %v", err)
	}
	if len(packets) != 1 || packets[0].Binding.BaselineGeneration != 2 || packets[0].Binding.OwnerAgentID != owners[0].ownerID {
		t.Fatalf("S8 issue did not preserve actual registered generation-2 tuple: %+v", packets)
	}
	manifestBytes := mustRead(t, filepath.Join(fixture.root, ".claude", "workgroups", "manifest-s8-g2-01.json"))
	if packets[0].Binding.ManifestSHA256 != workpacket.Digest(manifestBytes) {
		t.Fatalf("S8 issue manifest digest = %s, want source digest %s", packets[0].Binding.ManifestSHA256, workpacket.Digest(manifestBytes))
	}
	current, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := hookctx.FromSnapshot(fixture.root, owners[0].ownerID, current)
	if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil ||
		!loaded.PolicyContext.Agent.HypothesisResultAssignment || loaded.PolicyContext.Agent.PlanReportedRef != "" {
		t.Fatalf("S8 Hook context depended on generic PLAN instead of the registered Case Assignment: loaded=%+v err=%v", loaded, err)
	}
	if len(loaded.PolicyContext.HypothesisResultAnswerPaths) != 1 || loaded.PolicyContext.HypothesisResultAnswerPaths[0] != packets[0].AnswerPath {
		t.Fatalf("S8 Hook context omitted exact original answer path %s: %+v", packets[0].AnswerPath, loaded.PolicyContext.HypothesisResultAnswerPaths)
	}
	answerPath := writeS8ProfessionalAnswer(t, fixture.root, packets[0], "The registered Case question is answerable from its frozen Finding evidence.")
	// Main ordinary saves do not receive readonly Worker answers; native Stop
	// hands the exact questionnaire to the original Case consumer.
	before := currentS8Snapshot(t, fixture)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": fixture.root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(fixture.root, answerPath)}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || currentS8Snapshot(t, fixture).Revision != before.Revision {
		t.Fatalf("Main save received readonly answer: %d %s", code, diagnostic.String())
	}
	body, _ = json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "native-investigator-g2", "agent_type": "investigator", "session_id": "native-parent", "cwd": fixture.root, "last_assistant_message": filepath.Join(fixture.root, packets[0].ViewPath)})
	out.Reset()
	diagnostic.Reset()
	if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("native Stop failed: %d %s", code, diagnostic.String())
	}
	if _, accepted, err := investigation.IssuedHypothesisResultReceipt(fixture.root, currentS8Snapshot(t, fixture).State, packets[0].ID, owners[0].ownerID); err != nil || !accepted {
		t.Fatalf("native Stop did not reach original Case consumer: %t %v", accepted, err)
	}
}

// This actual host chain uses the original Runtime registration, PLAN/activation
// transition, packet issuer, PostToolUse CLI, and Case revision writer. The
// answer bytes below are authored fixture input, not a claim about professional
// judgment quality or model performance.
func TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, true)
	fixture.hookBinary = factory.BuildHarness(t)
	if len(owners) != 4 {
		t.Fatalf("registered owners = %d, want two accepted siblings, one approval-gated owner, and one route-pending owner", len(owners))
	}

	// Two continuous-execution Investigators complete the original PLAN,
	// activation, and AgentBegin chain. The third uses the existing human
	// approval mode and must not be promoted by a PLAN_REPORT.
	for _, owner := range owners[:2] {
		runS8AgentBegin(t, fixture, owner)
	}
	approval := owners[2]
	routePending := owners[3]
	runS8AgentBegin(t, fixture, routePending)
	stateBeforeApproval := mustRead(t, fixture.statePath)
	journalBeforeApproval := mustRead(t, fixture.journalPath)
	planPath := writeS8PlanReport(t, fixture, approval)
	if _, err := assignment.AdvanceAgent(fixture.root, fixture.statePath, fixture.journalPath, assignment.AgentEventRequest{
		ExpectedRevision: currentS8Revision(t, fixture), AgentID: approval.ownerID, Event: "readback_submitted", MessagePath: planPath,
	}); err == nil || !strings.Contains(err.Error(), "readback_response") {
		t.Fatalf("plan_approval_required accepted a PLAN_REPORT through the original Agent writer: %v", err)
	}
	if !bytes.Equal(stateBeforeApproval, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeApproval, mustRead(t, fixture.journalPath)) {
		t.Fatal("rejected PLAN_REPORT changed Runtime state or journal")
	}

	registered, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, registered.Revision, "")
	if err != nil {
		t.Fatalf("issue exact open questions after original PLAN transition: %v", err)
	}
	if len(packets) != 4 {
		t.Fatalf("issued packets = %d, want one for each original registered Investigator", len(packets))
	}
	byOwner := map[string]workpacket.HypothesisResultPacket{}
	for _, packet := range packets {
		byOwner[packet.Binding.OwnerAgentID] = packet
	}
	for _, owner := range owners {
		if _, found := byOwner[owner.ownerID]; !found {
			t.Fatalf("no original issued packet for owner %s", owner.ownerID)
		}
	}
	var statusOut, statusErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "investigation", "status", "--root", fixture.root, "--case-id", packets[0].Binding.CaseID}, nil, &statusOut, &statusErr); code != 0 {
		t.Fatalf("original S8 status board after packet issue: code=%d stderr=%s stdout=%s", code, statusErr.String(), statusOut.String())
	}
	var status struct {
		Board struct {
			NextAction string           `json:"next_action"`
			Packets    []map[string]any `json:"hypothesis_result_packets"`
		} `json:"board"`
	}
	if err := json.Unmarshal(statusOut.Bytes(), &status); err != nil {
		t.Fatalf("decode original S8 status board: %v", err)
	}
	if len(status.Board.Packets) != 4 || !strings.Contains(status.Board.NextAction, "answer_path") || strings.Contains(status.Board.NextAction, "runtime investigation hypothesis result") {
		t.Fatalf("original S8 status did not project issued packet work: packets=%d next=%q", len(status.Board.Packets), status.Board.NextAction)
	}
	for _, row := range status.Board.Packets {
		if s8TestString(row["packet_id"]) == "" || s8TestString(row["view_path"]) == "" || s8TestString(row["answer_path"]) == "" || s8TestString(row["receipt_command"]) == "" {
			t.Fatalf("S8 status packet row lacks exact issuance/answer/receipt paths: %#v", row)
		}
	}
	if _, blocked := hook.StopIdleDecision(fixture.root, policy.Input{Event: "TeammateIdle", AgentID: routePending.ownerID}); !blocked {
		t.Fatal("unaccepted registered S8 Investigator was allowed to idle before its exact Case receipt")
	}

	approvalPacket := byOwner[approval.ownerID]
	approvalAnswer := writeS8ProfessionalAnswer(t, fixture.root, approvalPacket, "The approval-gated Investigator cannot submit before human approval.")
	stateBeforeRejectedHook := mustRead(t, fixture.statePath)
	journalBeforeRejectedHook := mustRead(t, fixture.journalPath)
	approvalReceipt := runS8PostToolUse(t, fixture, approval.ownerID, approvalAnswer)
	if approvalReceipt.Accepted {
		t.Fatal("S8 answer from the unapproved plan_approval_required Investigator was accepted")
	}
	if !bytes.Equal(stateBeforeRejectedHook, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeRejectedHook, mustRead(t, fixture.journalPath)) {
		t.Fatal("unapproved S8 hook attempt changed Runtime state or journal")
	}

	firstOwner, siblingOwner := owners[0], owners[1]
	firstPacket, siblingPacket := byOwner[firstOwner.ownerID], byOwner[siblingOwner.ownerID]
	firstAnswer := writeS8ProfessionalAnswer(t, fixture.root, firstPacket, "The first registered question has a source-backed observation.")

	firstReceipt := runS8PostToolUse(t, fixture, firstOwner.ownerID, firstAnswer)
	if !firstReceipt.Accepted || firstReceipt.PacketID != firstPacket.ID {
		t.Fatalf("original S8 PostToolUse did not accept the first answer: %+v", firstReceipt)
	}
	firstDurable, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, currentS8State(t, fixture), firstPacket.ID, firstOwner.ownerID)
	if err != nil || !found || firstDurable.ExecutionRoot != fixture.root || firstDurable.PacketID != firstPacket.ID {
		t.Fatalf("original Case receipt was not independently verified: found=%v receipt=%+v err=%v", found, firstDurable, err)
	}
	stopDecision, blocked := hook.StopIdleDecision(fixture.root, policy.Input{Event: "TeammateIdle", AgentID: firstOwner.ownerID})
	if blocked {
		t.Fatalf("exact accepted S8 Investigator was still blocked at TeammateIdle: %+v", stopDecision)
	}

	// An aggregate recovery scan must skip only the exact packet with its
	// verified original Case receipt, while restoring a missing open sibling
	// draft from the existing issuance without another Runtime write.
	if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(siblingPacket.AnswerPath))); err != nil {
		t.Fatalf("remove pending sibling draft for recovery fixture: %v", err)
	}
	stateBeforeDraftRecovery := mustRead(t, fixture.statePath)
	journalBeforeDraftRecovery := mustRead(t, fixture.journalPath)
	reissued, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, currentS8Revision(t, fixture), "")
	if err != nil {
		t.Fatalf("recover open sibling packet after one accepted result: %v", err)
	}
	if len(reissued) != 3 {
		t.Fatalf("aggregate packet recovery returned %d packets, want 3 open owners and omit only the accepted owner", len(reissued))
	}
	var siblingRestored bool
	for _, packet := range reissued {
		if packet.Binding.OwnerAgentID == siblingOwner.ownerID && packet.ID == siblingPacket.ID {
			siblingRestored = true
		}
		if packet.Binding.OwnerAgentID == firstOwner.ownerID {
			t.Fatalf("aggregate packet recovery returned already accepted owner %s", firstOwner.ownerID)
		}
	}
	if !siblingRestored {
		t.Fatal("aggregate packet recovery did not preserve and republish the exact open sibling packet")
	}
	if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(siblingPacket.AnswerPath))); err != nil {
		t.Fatalf("open sibling answer draft was not restored from the frozen packet: %v", err)
	}
	if !bytes.Equal(stateBeforeDraftRecovery, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeDraftRecovery, mustRead(t, fixture.journalPath)) {
		t.Fatal("packet/draft recovery wrote another Runtime revision or journal event")
	}
	siblingAnswer := writeS8ProfessionalAnswer(t, fixture.root, siblingPacket, "The sibling question has an independent source-backed observation.")
	currentSiblingPackets, currentSiblingErr := runtime.CurrentHypothesisResultPackets(fixture.root, currentS8State(t, fixture), siblingOwner.ownerID)
	if currentSiblingErr != nil || len(currentSiblingPackets) != 1 || currentSiblingPackets[0].ID != siblingPacket.ID {
		t.Fatalf("unanswered sibling packet lost current Case continuity after accepted sibling: packets=%+v err=%v", currentSiblingPackets, currentSiblingErr)
	}

	// This second packet was frozen before the first Case write. Its manifest
	// points to the preceding immutable Case revision; the original consumer
	// must accept it after confirming the sibling's own question/source stayed
	// unchanged and then write through the same Case CAS.
	siblingReceipt := runS8PostToolUse(t, fixture, siblingOwner.ownerID, siblingAnswer)
	if !siblingReceipt.Accepted || siblingReceipt.PacketID != siblingPacket.ID {
		t.Fatalf("sibling answer did not survive the preceding Case revision: %+v", siblingReceipt)
	}
	casePointer := investigationPointerFromState(t, currentS8State(t, fixture))
	caseDocument := readCaseDocument(t, fixture.root, casePointer["path"].(string))
	if got := len(caseDocument["hypothesis_results"].([]any)); got != 2 {
		t.Fatalf("original Case has %d accepted sibling results, want 2", got)
	}

	// Exact replay after a further sibling Case revision is a zero-write
	// receipt recovery. A changed answer under the same packet identity is a
	// conflict and also leaves the Runtime pair unchanged.
	stateBeforeReplay := mustRead(t, fixture.statePath)
	journalBeforeReplay := mustRead(t, fixture.journalPath)
	replay := runS8PostToolUse(t, fixture, firstOwner.ownerID, firstAnswer)
	if !replay.Accepted {
		t.Fatalf("exact S8 replay after sibling revision was rejected: %+v", replay)
	}
	if !bytes.Equal(stateBeforeReplay, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeReplay, mustRead(t, fixture.journalPath)) {
		t.Fatal("exact S8 replay wrote another Runtime revision or journal event")
	}
	var changed map[string]any
	originalFirstAnswer := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(firstAnswer)))
	if err := json.Unmarshal(originalFirstAnswer, &changed); err != nil {
		t.Fatal(err)
	}
	answer := changed["answer"].(map[string]any)
	answer["observed"] = "Changed professional answer for the already accepted packet."
	writeInvestigationJSON(t, filepath.Join(fixture.root, filepath.FromSlash(firstAnswer)), changed)
	stateBeforeConflict := mustRead(t, fixture.statePath)
	journalBeforeConflict := mustRead(t, fixture.journalPath)
	conflict := runS8PostToolUse(t, fixture, firstOwner.ownerID, firstAnswer)
	if conflict.Accepted {
		t.Fatal("changed answer reused the original S8 packet identity")
	}
	if !bytes.Equal(stateBeforeConflict, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeConflict, mustRead(t, fixture.journalPath)) {
		t.Fatal("conflicting S8 answer changed Runtime state or journal")
	}
	writeInvestigationBytes(t, fixture.root, firstAnswer, originalFirstAnswer)

	// Consume investigate_more while the durable Case is left at its original
	// unconsumed route tuple. A new answer must compare the current Runtime
	// route pointer with those Case bytes and reject this stale authority.
	latest, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	casePointer = investigationPointerFromState(t, latest.State)
	routed, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: latest.Revision, ExpectedCaseRevision: s8TestInt(casePointer["revision"]), ExpectedCaseSHA256: casePointer["sha256"].(string),
		CaseID: casePointer["case_id"].(string), Route: "investigate_more", RouteReason: "the original registered question remains open",
	})
	if err != nil {
		t.Fatalf("record original investigate_more Case route: %v", err)
	}
	consumed, err := investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: routed.Revision, CaseID: casePointer["case_id"].(string), Actor: "orchestrator",
	})
	if err != nil {
		t.Fatalf("consume original investigate_more route: %v", err)
	}
	consumedPointer := investigationPointerFromState(t, consumed.State)
	if consumedPointer["route"] != "investigate_more" || consumedPointer["route_consumed_at"] == nil {
		t.Fatalf("Runtime route pointer did not record consumption: %#v", consumedPointer)
	}
	caseAfterConsume := readCaseDocument(t, fixture.root, consumedPointer["path"].(string))
	if caseAfterConsume["route_consumed_at"] != nil {
		t.Fatalf("fixture no longer reproduces split Runtime-pointer/Case route sources: %#v", caseAfterConsume["route_consumed_at"])
	}
	stateBeforeConsumedRouteSave := mustRead(t, fixture.statePath)
	journalBeforeConsumedRouteSave := mustRead(t, fixture.journalPath)
	lateAnswer := writeS8ProfessionalAnswer(t, fixture.root, byOwner[routePending.ownerID], "This answer must not be accepted after Runtime consumed the Case route.")
	_, directErr := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.IssuedHypothesisResultRequest{
		ExpectedRevision: consumed.Revision, PacketID: byOwner[routePending.ownerID].ID, AnswerPath: lateAnswer,
		OwnerAgentID: routePending.ownerID, SourceRoot: fixture.root,
		AuthorizeSource: func(root string, _ map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("unexpected source root %q", root)
			}
			return nil
		},
	})
	if directErr == nil || !strings.Contains(strings.ToLower(directErr.Error()), "route") {
		t.Fatalf("direct original S8 writer accepted pointer-only consumed route: %v", directErr)
	}
	if !bytes.Equal(stateBeforeConsumedRouteSave, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeConsumedRouteSave, mustRead(t, fixture.journalPath)) {
		t.Fatal("direct original S8 writer after consumed route changed Runtime state or journal")
	}
	lateReceipt := runS8PostToolUse(t, fixture, routePending.ownerID, lateAnswer)
	if lateReceipt.Accepted {
		t.Fatal("fresh S8 answer was accepted after Runtime consumed investigate_more while Case bytes remained unconsumed")
	}
	if !bytes.Equal(stateBeforeConsumedRouteSave, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeConsumedRouteSave, mustRead(t, fixture.journalPath)) {
		t.Fatal("fresh S8 answer after consumed route changed Runtime state or journal")
	}
	if _, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, consumed.State, firstPacket.ID, firstOwner.ownerID); err != nil || !found {
		t.Fatalf("route consumption invalidated read-only proof for an already accepted S8 answer: found=%v err=%v", found, err)
	}
	// A later Case edit can clear the pointer's consumed tuple and restore the
	// old visible route. That must not revive an unanswered packet whose
	// immutable Case history crossed the consumed route transaction.
	registeredAgain, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision: consumed.Revision, ExpectedCaseRevision: s8TestInt(consumedPointer["revision"]), ExpectedCaseSHA256: consumedPointer["sha256"].(string),
		CaseID: consumedPointer["case_id"].(string), HypothesisID: "hypothesis-after-route-consume", AssignmentID: "assignment-after-route-consume",
		Statement: "a fresh question follows the consumed investigation route", Invariant: "the consumed route does not restore old work",
		Discriminator: "compare the old issuance to the immutable Case lineage", ExpectedOutcomes: map[string]any{"support": "the old packet stays stale", "refute": "the old packet becomes current"},
		SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"evidence://s8-after-route-consume"},
	})
	if err != nil {
		t.Fatalf("register a fresh question after route consumption: %v", err)
	}
	registeredPointer := investigationPointerFromState(t, registeredAgain.State)
	if registeredPointer["route_consumed_at"] != nil || registeredPointer["route_consumer"] != nil {
		t.Fatalf("new Case revision did not clear the pointer-only consumed tuple as expected: %#v", registeredPointer)
	}
	restoredRoute, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision: registeredAgain.Revision, ExpectedCaseRevision: s8TestInt(registeredPointer["revision"]), ExpectedCaseSHA256: registeredPointer["sha256"].(string),
		CaseID: registeredPointer["case_id"].(string), Operation: "fixture_restore_prior_route_tuple",
		Mutate: func(document map[string]any) error {
			document["route"] = nil
			document["route_reason"] = nil
			return nil
		},
	})
	if err != nil {
		t.Fatalf("restore the prior visible route after registering a fresh question: %v", err)
	}
	restoredPointer := investigationPointerFromState(t, restoredRoute.State)
	if restoredPointer["route"] != nil || restoredPointer["route_consumed_at"] != nil {
		t.Fatalf("fixture did not restore the prior visible route tuple: %#v", restoredPointer)
	}
	stateBeforeContinuityReject := mustRead(t, fixture.statePath)
	journalBeforeContinuityReject := mustRead(t, fixture.journalPath)
	_, continuityErr := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.IssuedHypothesisResultRequest{
		ExpectedRevision: restoredRoute.Revision, PacketID: byOwner[routePending.ownerID].ID, AnswerPath: lateAnswer,
		OwnerAgentID: routePending.ownerID, SourceRoot: fixture.root,
		AuthorizeSource: func(root string, _ map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("unexpected source root %q", root)
			}
			return nil
		},
	})
	if continuityErr == nil || !strings.Contains(strings.ToLower(continuityErr.Error()), "continuity") {
		t.Fatalf("direct original writer revived an old packet after route consumption and tuple restoration: %v", continuityErr)
	}
	if !bytes.Equal(stateBeforeContinuityReject, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeContinuityReject, mustRead(t, fixture.journalPath)) {
		t.Fatal("stale packet after route-consumption/route-restoration changed Runtime state or journal")
	}
	if _, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, restoredRoute.Revision, ""); err == nil {
		// The fresh question above has no registered dispatch; the old packet
		// must still be rejected when the issuer scans all current owners.
		t.Fatal("S8 issuer accepted an old packet after the original Runtime route was consumed")
	}
}

func prepareS8GenerationTwoFixture(t *testing.T, siblings bool, sourceSetup ...func(*intakeFixture)) (*intakeFixture, []s8OwnerFixture) {
	t.Helper()
	fixture := newIntakeFixture(t, []string{"finding-1"})
	stateBytes := mustRead(t, fixture.statePath)
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	boundREQ, _ := state["bound_req"].(map[string]any)
	if boundREQ == nil {
		boundREQ = map[string]any{}
		state["bound_req"] = boundREQ
	}
	boundREQ["workspace"] = bindS8FixtureGitRoot(t, fixture.root)
	state["schema_version"] = "1.2.0"
	state["baseline"].(map[string]any)["generation"] = 2
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	writeInvestigationJSON(t, fixture.statePath, state)
	for _, setup := range sourceSetup {
		setup(fixture)
	}
	writeObservationBatch(t, fixture, []string{"finding-1"}, 2)
	setContractLifecycle(t, fixture)
	registered, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision: 0, GroupingRationale: "one sealed Finding enters one bounded Case",
	})
	if err != nil {
		t.Fatalf("Ingest v1.2 generation 2 Case: %v", err)
	}
	definitionBytes := mustRead(t, filepath.Join("..", "..", "agents", "investigator.md"))
	writeInvestigationBytes(t, fixture.root, ".claude/agents/investigator.md", definitionBytes)

	count := 1
	if siblings {
		count = 4
	}
	owners := make([]s8OwnerFixture, 0, count)
	for index := 1; index <= count; index++ {
		pointer := investigationPointerFromState(t, registered.State)
		caseID := pointer["case_id"].(string)
		hypothesisID := fmt.Sprintf("hypothesis-s8-generation-two-%02d", index)
		assignmentID := fmt.Sprintf("assignment-s8-generation-two-%02d", index)
		registered, err = investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
			ExpectedRevision: registered.Revision, ExpectedCaseRevision: s8TestInt(pointer["revision"]), ExpectedCaseSHA256: pointer["sha256"].(string),
			CaseID: caseID, HypothesisID: hypothesisID, AssignmentID: assignmentID,
			Statement: fmt.Sprintf("question %d checks whether the boundary drops a required value", index),
			Invariant: "the value survives this boundary", Discriminator: fmt.Sprintf("trace question %d through the decoder", index),
			ExpectedOutcomes: map[string]any{"support": "the value is dropped", "refute": "the value survives"},
			SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{fmt.Sprintf("evidence://s8-generation-two-%02d", index)},
		})
		if err != nil {
			t.Fatalf("register open Hypothesis %d: %v", index, err)
		}
		pointer = investigationPointerFromState(t, registered.State)
		owner := s8OwnerFixture{
			hypothesisID: hypothesisID, assignmentID: assignmentID,
			ownerID: fmt.Sprintf("agent-investigator-s8-g2-%02d", index), teamID: fmt.Sprintf("workgroup-s8-generation-two-%02d", index),
			taskID: fmt.Sprintf("TASK-842-%02d", index), dispatchMode: "plan_checkpoint",
		}
		if siblings && index == 3 {
			owner.dispatchMode = "plan_approval_required"
		}
		registered = registerS8Owner(t, fixture, registered, owner, pointer["path"].(string), pointer["sha256"].(string))
		owners = append(owners, owner)
	}
	registrationEvents := 0
	for _, event := range s8CommittedEvents(t, fixture) {
		if event["transition_id"] == "ENTITY-REGISTER" && event["event"] == "transition_committed" && s8TestInt(event["baseline_generation"]) != 2 {
			t.Fatalf("original ENTITY-REGISTER event did not bind generation 2: %#v", event)
		}
		if event["transition_id"] == "ENTITY-REGISTER" && event["event"] == "transition_committed" {
			registrationEvents++
		}
	}
	if registrationEvents != count {
		t.Fatalf("committed original ENTITY-REGISTER events = %d, want %d", registrationEvents, count)
	}
	return fixture, owners
}

func bindS8FixtureGitRoot(t *testing.T, root string) map[string]any {
	t.Helper()
	run := func(args ...string) string {
		command := exec.Command("git", args...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %v: %s", args, err, strings.TrimSpace(string(output)))
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "--initial-branch=main", root)
	run("-C", root, "config", "user.email", "s8-fixture@example.invalid")
	run("-C", root, "config", "user.name", "S8 fixture")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("temporary S8 workspace authority\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("-C", root, "add", "README.md")
	run("-C", root, "commit", "-qm", "fixture baseline")
	run("-C", root, "switch", "-qc", "s8-fixture")
	commit := run("-C", root, "rev-parse", "HEAD")
	return map[string]any{"project_root": root, "dev_branch": "s8-fixture", "release_upstream": "main", "bound_commit": commit}
}

func registerS8Owner(t *testing.T, fixture *intakeFixture, current runtime.Snapshot, owner s8OwnerFixture, caseRef, caseSHA string) runtime.Snapshot {
	t.Helper()
	caseID := investigationPointerFromState(t, current.State)["case_id"].(string)
	caseDoc := readCaseDocument(t, fixture.root, caseRef)
	var question map[string]any
	for _, raw := range caseDoc["hypotheses"].([]any) {
		row := raw.(map[string]any)
		if row["hypothesis_id"] == owner.hypothesisID {
			question = row
		}
	}
	if question == nil {
		t.Fatalf("Case %s has no registered question %s", caseID, owner.hypothesisID)
	}
	taskRef := filepath.ToSlash(filepath.Join(".claude", "tasks", owner.taskID+".json"))
	manifestRef := filepath.ToSlash(filepath.Join(".claude", "workgroups", "manifest-s8-g2-"+strings.TrimPrefix(owner.taskID, "TASK-842-")+".json"))
	task := map[string]any{"task_id": owner.taskID, "case_id": caseID, "hypothesis_id": owner.hypothesisID, "assignment_id": owner.assignmentID, "objective": question["discriminator"]}
	taskBytes, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	taskBytes = append(taskBytes, '\n')
	writeInvestigationBytes(t, fixture.root, taskRef, taskBytes)
	readPaths := []string{caseRef}
	if fixture.batchRel != "" {
		readPaths = append(readPaths, fixture.batchRel)
	}
	for _, raw := range caseDoc["source_finding_ids"].([]any) {
		readPaths = append(readPaths, ".claude/evidence/findings/"+raw.(string)+".json")
	}
	if fixture.batchRel == "" {
		for _, raw := range question["evidence_refs"].([]any) {
			ref, _ := raw.(string)
			for _, evidence := range current.State["evidence"].([]any) {
				row := evidence.(map[string]any)
				if ref == row["id"] || ref == "runtime:"+row["id"].(string) {
					ref, _ = row["path"].(string)
					break
				}
			}
			if info, err := os.Stat(filepath.Join(fixture.root, ref)); err == nil && info.Mode().IsRegular() {
				readPaths = append(readPaths, ref)
			}
		}
	}

	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": "team-manifest-" + owner.teamID, "version": "v1.0.0",
		"runtime_id": "loop-REQ-INTAKE", "req_id": "REQ-002", "baseline_generation": 2, "review_round": 1,
		"platform_team_id": "platform-" + owner.teamID, "workgroup_id": owner.teamID, "workgroup_kind": "investigator", "status": "planned",
		"documents": []any{
			map[string]any{"id": "investigation-case", "path": caseRef, "version": fmt.Sprintf("r%d", s8TestInt(caseDoc["revision"])), "sha256": caseSHA},
			map[string]any{"id": owner.taskID, "path": taskRef, "version": "v1.0.0", "sha256": workpacket.Digest(taskBytes)},
		},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "S8-CAUSAL-INVESTIGATION", "disposition": "assigned", "trigger": "one exact open Hypothesis requires independent investigation",
			"assignment_ids": []string{owner.assignmentID}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments": []any{map[string]any{
			"assignment_id": owner.assignmentID, "responsibility_id": "S8-CAUSAL-INVESTIGATION", "role_family": "investigator",
			"scope": []string{"case:" + caseID, "hypothesis:" + owner.hypothesisID}, "agent_id": owner.ownerID,
			"agent_definition_ref": ".claude/agents/investigator.md", "skill_refs": agentcontext.CoreSkills("investigator"),
			"read_paths":  readPaths,
			"write_paths": []string{}, "output_paths": []string{".claude/review/investigation/"},
			"depends_on": []string{}, "reuse_decision": "create", "grouping_rationale": "one owner for one registered discriminator",
			"dispatch_mode": owner.dispatchMode, "status": "planned",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "one Investigator owns the exact Case question",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-10-14T00:00:00Z"},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes = append(manifestBytes, '\n')
	writeInvestigationBytes(t, fixture.root, manifestRef, manifestBytes)
	snapshot, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{
		ExpectedRevision: current.Revision, ManifestPath: filepath.Join(fixture.root, filepath.FromSlash(manifestRef)), TaskID: owner.taskID,
		TaskPath: filepath.Join(fixture.root, filepath.FromSlash(taskRef)), OccurredAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC).Add(time.Duration(current.Revision) * time.Second),
	})
	if err != nil {
		t.Fatalf("actual ENTITY-REGISTER %s: %v", owner.assignmentID, err)
	}
	return snapshot
}

func runS8AgentBegin(t *testing.T, fixture *intakeFixture, owner s8OwnerFixture) {
	t.Helper()
	planPath := writeS8PlanReport(t, fixture, owner)
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "agent-begin", "--root", fixture.root, "--agent-id", owner.ownerID, "--plan", planPath}, nil, &out, &errout); code != 0 {
		t.Fatalf("original PLAN/activation/AgentBegin for %s: code=%d stderr=%s stdout=%s", owner.ownerID, code, errout.String(), out.String())
	}
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range snapshot.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == owner.ownerID && row["state"] != "working" {
			t.Fatalf("AgentBegin left %s in %v, want working", owner.ownerID, row["state"])
		}
	}
}

func writeS8PlanReport(t *testing.T, fixture *intakeFixture, owner s8OwnerFixture) string {
	t.Helper()
	planPath := filepath.Join(fixture.root, ".claude", "plans", owner.ownerID+"-plan.json")
	plan := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-" + owner.ownerID + "-plan",
		"correlation_id": "corr-" + owner.assignmentID, "runtime_id": "loop-REQ-INTAKE", "agent_id": owner.ownerID,
		"agent_definition_ref": ".claude/agents/investigator.md", "task_id": owner.taskID, "bug_id": nil,
		"team_id": owner.teamID, "occurred_at": "2026-10-14T12:30:00Z", "assignment_id": owner.assignmentID, "assignment_revision": 1,
		"objective":        "Trace the registered hypothesis against its frozen source evidence.",
		"planned_paths":    []string{".claude/review/investigation/"},
		"steps":            []any{map[string]any{"description": "Read the sealed observation and frozen Finding encounter", "target": ".claude/evidence/"}},
		"assertion_checks": []any{map[string]any{"assertion": "The observation addresses the registered discriminator.", "oracle": "Compare the answer to the exact source bytes."}},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeInvestigationBytes(t, fixture.root, filepath.ToSlash(filepath.Join(".claude", "plans", filepath.Base(planPath))), append(data, '\n'))
	return planPath
}

func writeS8ProfessionalAnswer(t *testing.T, root string, packet workpacket.HypothesisResultPacket, observed string) string {
	t.Helper()
	result := "supported"
	answer := workpacket.HypothesisResultAnswer{
		Method:       "Read the sealed ObservationBatch and compare its registered Finding encounter with the question.",
		EvidenceRefs: []string{packet.Inputs.Hypothesis.EvidenceRefs[0]}, SourceBoundaryRefs: []string{"finding:finding-1"},
		Observed: observed, CounterfactualOrDiscriminator: "A source-aligned boundary would preserve the registered value.", Result: &result,
		ExplainsFindingIDs: []string{"finding-1"}, DoesNotExplain: []string{}, NewHypotheses: []workpacket.HypothesisNewQuestion{},
	}
	answerBytes, err := json.MarshalIndent(struct {
		Title       string                            `json:"title"`
		Instruction string                            `json:"instruction"`
		Read        []workpacket.ReadItem             `json:"read"`
		Questions   map[string]workpacket.Question    `json:"questions"`
		Answer      workpacket.HypothesisResultAnswer `json:"answer"`
	}{
		Title: packet.Draft.Title, Instruction: packet.Draft.Instruction,
		Read: packet.Draft.Read, Questions: packet.Draft.Questions, Answer: answer,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeInvestigationBytes(t, root, packet.AnswerPath, append(answerBytes, '\n'))
	return packet.AnswerPath
}

func runS8PostToolUse(t *testing.T, fixture *intakeFixture, owner, answerPath string) submissionReceipt {
	return runS8PostToolUseWithHookMetadata(t, fixture, owner, fixture.root, "", answerPath)
}

func runS8PostToolUseWithHookMetadata(t *testing.T, fixture *intakeFixture, owner, cwd, session, answerPath string) submissionReceipt {
	t.Helper()
	// Frozen teammate compatibility; actual native actors use Start/Stop in
	// native_hypothesis_hook_test.go, without an engineering actor alias.
	abs := filepath.Join(fixture.root, filepath.FromSlash(answerPath))
	payload := map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": abs},
		"cwd": cwd, "session_id": session,
	}
	if owner != "" {
		payload["teammate_name"] = owner
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	var code int
	if fixture.hookBinary != "" {
		var stdoutText, stderrText string
		code, stdoutText, stderrText = factory.RunHook(t, fixture.hookBinary, fixture.root, "PostToolUse", body)
		out.WriteString(stdoutText)
		errout.WriteString(stderrText)
	} else {
		code = cli.Run([]string{"hook", "--root", fixture.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout)
	}
	if code != 0 {
		t.Fatalf("original PostToolUse hook code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	var envelope struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode original PostToolUse receipt envelope: %v; stdout=%s stderr=%s", err, out.String(), errout.String())
	}
	const prefix = "S8 SUBMISSION RECEIPT: "
	if !strings.HasPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix) {
		t.Fatalf("PostToolUse did not return the original S8 receipt: %s", out.String())
	}
	var receipt submissionReceipt
	receiptBytes := []byte(strings.TrimPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix))
	assertSharedSubmissionReceiptWire(t, receiptBytes)
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode S8 domain receipt: %v", err)
	}
	return receipt
}

type submissionReceipt struct {
	PacketID             string `json:"packet_id"`
	Accepted             bool   `json:"accepted"`
	ResponsibilityStatus string `json:"responsibility_status"`
	Error                string `json:"error"`
}

func currentS8State(t *testing.T, fixture *intakeFixture) map[string]any {
	t.Helper()
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.State
}

func currentS8Revision(t *testing.T, fixture *intakeFixture) int {
	t.Helper()
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Revision
}

func s8CommittedEvents(t *testing.T, fixture *intakeFixture) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(mustRead(t, fixture.journalPath))), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func writeInvestigationJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeInvestigationBytes(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func s8TestInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int32:
		return int(number)
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func s8TestString(value any) string {
	text, _ := value.(string)
	return text
}
