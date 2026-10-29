package investigation_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This fixture exercises the original S8 approval and S9 writers plus the
// public S9 packet/Hook adapters. The authored red check is deterministic test
// input, not an assertion that an AI model performed or professionally judged
// the check.
func TestS9RepairPlanReportOriginalHookCoverageReplayAndWriteBarrier(t *testing.T) {
	fixture, plan, owners := prepareS9RepairPlanReportHookFixture(t)
	if len(plan.Assignments) != 2 || len(owners) != 2 {
		t.Fatalf("S9 fixture assignments/owners = %d/%d, want two siblings", len(plan.Assignments), len(owners))
	}

	var issueOut, issueErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &issueOut, &issueErr); code != 0 {
		entities, _ := currentS9State(t, fixture)["entities"].(map[string]any)
		agents, _ := json.Marshal(entities["agents"])
		tasks, _ := json.Marshal(entities["tasks"])
		t.Fatalf("public S9 stage-answer issue: code=%d stderr=%s stdout=%s agents=%s tasks=%s", code, issueErr.String(), issueOut.String(), agents, tasks)
	}
	var packets []workpacket.RepairPlanReportPacket
	if err := json.Unmarshal(issueOut.Bytes(), &packets); err != nil {
		t.Fatalf("decode original issued S9 packets: %v; stdout=%s", err, issueOut.String())
	}
	if len(packets) != 2 {
		t.Fatalf("issued S9 packets = %d, want one for each registered Builder: %+v", len(packets), packets)
	}
	byAssignment := map[string]workpacket.RepairPlanReportPacket{}
	for _, packet := range packets {
		if packet.Binding.BaselineGeneration != 2 || packet.Binding.OwnerAgentID == "" || packet.AnswerPath == "" {
			t.Fatalf("packet lost its generation-two original dispatch binding: %+v", packet.Binding)
		}
		byAssignment[packet.Binding.RepairAssignmentID] = packet
	}

	// File-save Hook events do not receive Worker answers. Exercise the explicit
	// original consumer with its bound owner, including rejection of a wrong owner.
	first := byAssignment[plan.Assignments[0].AssignmentID]
	if first.ID == "" {
		t.Fatalf("no S9 packet for first assignment %s", plan.Assignments[0].AssignmentID)
	}
	firstAnswer := writeS9RepairPlanReportAnswer(t, fixture.root, first, "inspect the approved boundary and establish the failing pre-fix check")
	beforeWrongOwner, journalWrongOwner := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if _, _, err := submitS9RecoveryTestPacket(fixture, -1, first, firstAnswer, "wrong-builder"); err == nil {
		t.Fatal("wrong owner consumed the original S9 packet")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeWrongOwner, journalWrongOwner, "wrong original S9 owner")
	firstReceipt := submitS9OriginalReceipt(t, fixture, first, firstAnswer)
	if !firstReceipt.Accepted || firstReceipt.PacketID != first.ID {
		t.Fatalf("original S9 consumer rejected the bound owner: %+v", firstReceipt)
	}

	// The original PTR-BUG-10 consumer may record the first sibling, but the
	// original PTR-BUG-11 write barrier rejects execution until the other
	// dispatched Assignment has its own exact receipt.
	if !firstReceipt.Accepted || firstReceipt.PacketID != first.ID || firstReceipt.WorkComplete {
		t.Fatalf("original S9 original consumer did not accept a pre-execution receipt: %+v", firstReceipt)
	}
	if durable, accepted, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, currentS9State(t, fixture), first.ID, first.Binding.OwnerAgentID); err != nil || !accepted || durable.WorkComplete || durable.ReportID != first.ReportID {
		t.Fatalf("S9 receipt is not original, durable, and pre-execution: accepted=%v receipt=%+v err=%v", accepted, durable, err)
	}
	current := currentS9Snapshot(t, fixture)
	if _, err := repair.BeginRepairExecution(fixture.root, fixture.statePath, fixture.journalPath, repair.BeginRepairExecutionRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: current.Revision, Actor: "main-session"},
	}); err != nil {
		t.Fatalf("Builder with its own PlanReport must not wait for sibling planning: %v", err)
	}

	second := byAssignment[plan.Assignments[1].AssignmentID]
	if second.ID == "" {
		t.Fatalf("no S9 packet for second assignment %s", plan.Assignments[1].AssignmentID)
	}
	secondAnswer := writeS9RepairPlanReportAnswer(t, fixture.root, second, "independently inspect the sibling boundary and establish its failing pre-fix check")
	secondReceipt := submitS9OriginalReceipt(t, fixture, second, secondAnswer)
	if !secondReceipt.Accepted || secondReceipt.PacketID != second.ID || secondReceipt.WorkComplete {
		t.Fatalf("original S9 original consumer did not accept the sibling pre-execution receipt: %+v", secondReceipt)
	}

	// A late sibling PlanReport preserves the existing fixing phase.
	assertS9ProductWriteObserved(t, fixture, owners[0])
	current = currentS9Snapshot(t, fixture)
	started, err := repair.BeginRepairExecution(fixture.root, fixture.statePath, fixture.journalPath, repair.BeginRepairExecutionRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: current.Revision, Actor: "main-session"},
	})
	if err != nil {
		t.Fatalf("original execution begin after full exact sibling coverage: %v", err)
	}
	if repairPointer := started.State["review"].(map[string]any)["repair"].(map[string]any); repairPointer["status"] != "repairing" {
		t.Fatalf("original execution begin status = %v, want repairing", repairPointer["status"])
	}

	// An exact duplicate original-consumer submission remains a receipt-only replay even after
	// the repair phase advances. It must not append another Runtime revision.
	stateBeforeReplay := mustRead(t, fixture.statePath)
	journalBeforeReplay := mustRead(t, fixture.journalPath)
	replay := submitS9OriginalReceipt(t, fixture, first, firstAnswer)
	if !replay.Accepted || replay.PacketID != first.ID || replay.WorkComplete {
		t.Fatalf("same-byte S9 original consumer replay after execution begin: %+v", replay)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBeforeReplay, journalBeforeReplay, "exact accepted S9 replay")

	// An arbitrary terminal path cannot select an original PlanReport consumer.
	// Keep the exact issued path for the positive Hook path below.
	var consumeOut, consumeErr bytes.Buffer
	unissuedPath := filepath.Join(fixture.root, ".claude", "submissions", "s9-unregistered", "S9-plan", "answer.json")
	stateBeforeUnissued, journalBeforeUnissued := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", fixture.root, "--agent", first.Binding.OwnerAgentID, "--path", unissuedPath}, nil, &consumeOut, &consumeErr)
	if code == 0 || !strings.Contains(consumeErr.String(), "S9 PlanReport answer path has no current registered issuance") {
		t.Fatalf("terminal S9 unissued-path consume code=%d stdout=%s stderr=%s, want exact-source rejection", code, consumeOut.String(), consumeErr.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBeforeUnissued, journalBeforeUnissued, "unissued terminal PlanReport source")

	// A changed answer at the same issued path conflicts with the accepted
	// receipt and cannot rewrite the original report or receipt.
	changedAnswer := writeS9RepairPlanReportAnswer(t, fixture.root, first, "changed answer after original acceptance")
	stateBeforeConflict := mustRead(t, fixture.statePath)
	journalBeforeConflict := mustRead(t, fixture.journalPath)
	conflict := submitS9OriginalReceipt(t, fixture, first, changedAnswer)
	if conflict.Accepted {
		t.Fatalf("changed S9 answer replaced an accepted report: %+v", conflict)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBeforeConflict, journalBeforeConflict, "changed accepted S9 answer")
}

func TestS9RepairPlanReportSavedFromReadingRunsOriginalPlanCheckpoint(t *testing.T) {
	fixture, plan, owners := prepareS9RepairPlanReportReadingFixture(t)
	if len(plan.Assignments) != 2 || len(owners) != 2 {
		t.Fatalf("S9 reading fixture assignments/owners = %d/%d, want two siblings", len(plan.Assignments), len(owners))
	}
	for _, owner := range owners {
		if s9AgentWorking(currentS9State(t, fixture), owner.agentID) {
			t.Fatalf("fixture unexpectedly began %s before its professional PlanReport was saved", owner.agentID)
		}
	}

	var issueOut, issueErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &issueOut, &issueErr); code != 0 {
		t.Fatalf("issue original S9 reading-stage packets: code=%d stderr=%s stdout=%s", code, issueErr.String(), issueOut.String())
	}
	var packets []workpacket.RepairPlanReportPacket
	if err := json.Unmarshal(issueOut.Bytes(), &packets); err != nil || len(packets) != len(owners) {
		t.Fatalf("decode original S9 reading-stage packets: count=%d err=%v stdout=%s", len(packets), err, issueOut.String())
	}
	packet := packets[0]
	answerPath := writeS9RepairPlanReportAnswer(t, fixture.root, packet, "inspect the approved boundary and record the pre-fix check")
	before := currentS9Snapshot(t, fixture)
	receipt := runS9PostToolUse(t, fixture, "unrelated-hook-agent", filepath.Join(fixture.root, "unrelated-cwd"), answerPath)
	if !receipt.Accepted || receipt.WorkComplete || receipt.PacketID != packet.ID {
		t.Fatalf("original S9 consumer did not accept the reading-stage professional answer: %+v", receipt)
	}
	after := currentS9Snapshot(t, fixture)
	if after.Revision != before.Revision+1 {
		t.Fatalf("professional PlanReport should record one domain fact: revision %d->%d", before.Revision, after.Revision)
	}
	beforeRow, _ := json.Marshal(s9AgentRow(before.State, packet.Binding.OwnerAgentID))
	afterRow, _ := json.Marshal(s9AgentRow(after.State, packet.Binding.OwnerAgentID))
	if !bytes.Equal(beforeRow, afterRow) {
		t.Fatalf("professional PlanReport changed a generic Agent lifecycle: before=%s after=%s", beforeRow, afterRow)
	}
	journal := string(mustRead(t, fixture.journalPath))
	for _, generic := range []string{"msg-s9-plan-", packet.Binding.OwnerAgentID + "-activation_sent", packet.Binding.OwnerAgentID + "-work_started"} {
		if strings.Contains(journal, generic) {
			t.Fatalf("professional PlanReport generated generic PLAN/AgentBegin proof: %s", generic)
		}
	}
	if _, found, err := repair.IssuedRepairPlanReportReceipt(fixture.root, after.State, packet.ID, packet.Binding.OwnerAgentID); err != nil || !found {
		t.Fatalf("original S9 professional receipt missing after direct recording: found=%t err=%v", found, err)
	}
	stateBeforeReplay, journalBeforeReplay := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replay := runS9PostToolUse(t, fixture, "different-hook-agent", filepath.Join(fixture.root, "different-cwd"), answerPath)
	if !replay.Accepted || replay.PacketID != packet.ID {
		t.Fatalf("exact saved S9 answer did not replay independently of Hook identity metadata: %+v", replay)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBeforeReplay, journalBeforeReplay, "exact S9 reading-stage questionnaire replay")
}

func TestS9IssuedReportStaleStagingIsSafelyRebuiltAfterSiblingCommit(t *testing.T) {
	fixture, _, owners := prepareS9RepairPlanReportHookFixture(t)
	if len(owners) != 2 {
		t.Fatalf("S9 fixture owners=%d, want two siblings", len(owners))
	}

	first := issueS9RecoveryTestPacket(t, fixture, owners[0].agentID)
	second := issueS9RecoveryTestPacket(t, fixture, owners[1].agentID)
	firstAnswer := writeS9RepairPlanReportAnswer(t, fixture.root, first, "Reproduce the first Builder's approved pre-fix boundary.")
	secondAnswer := writeS9RepairPlanReportAnswer(t, fixture.root, second, "Reproduce the sibling Builder's approved pre-fix boundary.")
	start := currentS9Snapshot(t, fixture)
	startState, startJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)

	stagePath := ".claude/review/repair/plan-reports/" + first.ReportID + ".json"
	stageAbs := filepath.Join(fixture.root, filepath.FromSlash(stagePath))
	// Materialize a real report through its original Writer, then retain only
	// the immutable artifact to model an interrupted pre-CAS staging write.
	if _, _, err := submitS9RecoveryTestPacket(fixture, start.Revision, first, firstAnswer, first.Binding.OwnerAgentID); err != nil {
		t.Fatalf("construct original staging artifact: %v", err)
	}
	if err := os.WriteFile(fixture.statePath, startState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.journalPath, startJournal, 0600); err != nil {
		t.Fatal(err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, startState, startJournal, "fixture interrupted pre-CAS staging")
	stagedBytes, readErr := os.ReadFile(stageAbs)
	if readErr != nil {
		t.Fatalf("exact unaccepted PlanReport staging artifact was not retained for retry: %v", readErr)
	}
	stagedSHA := workpacket.Digest(stagedBytes)
	staged, err := repair.ValidatePlanReport(fixture.root, repair.ArtifactRef{Path: stagePath, SHA256: stagedSHA})
	if err != nil || staged.Receipt == nil {
		t.Fatalf("validate retained pre-CAS staged PlanReport: report=%+v err=%v", staged, err)
	}

	// The sibling commit advances the Runtime and journal coordinates that the
	// failed first artifact predicted. Its original report must remain intact.
	secondAccepted, _, err := submitS9RecoveryTestPacket(fixture, start.Revision, second, secondAnswer, second.Binding.OwnerAgentID)
	if err != nil {
		t.Fatalf("accept sibling report while first unaccepted artifact is staged: %v", err)
	}
	secondReceipt, found, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, secondAccepted.State, second.ID, second.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("verify sibling original receipt after its commit: found=%v err=%v", found, err)
	}
	assertS9SingleOriginalReportCommit(t, fixture, secondAccepted.State, second, secondReceipt)

	beforeRetry := currentS9Snapshot(t, fixture)
	if beforeRetry.Revision != start.Revision+1 {
		t.Fatalf("sibling commit revision=%d, want %d", beforeRetry.Revision, start.Revision+1)
	}
	firstAccepted, _, err := submitS9RecoveryTestPacket(fixture, beforeRetry.Revision, first, firstAnswer, first.Binding.OwnerAgentID)
	if err != nil {
		t.Fatalf("retry first report after exact stale staging cleanup: %v", err)
	}
	firstReceipt, found, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, firstAccepted.State, first.ID, first.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("verify retried first original receipt: found=%v err=%v", found, err)
	}
	if firstReceipt.AcceptedAfterRuntimeRevision != staged.Receipt.AcceptedAfterRuntimeRevision+1 || firstReceipt.AcceptedSequence != staged.Receipt.AcceptedSequence+1 {
		t.Fatalf("retried receipt coordinates=%d/%d, stale staging predicted %d/%d; want exactly one sibling commit later", firstReceipt.AcceptedAfterRuntimeRevision, firstReceipt.AcceptedSequence, staged.Receipt.AcceptedAfterRuntimeRevision, staged.Receipt.AcceptedSequence)
	}
	finalReportBytes, err := os.ReadFile(stageAbs)
	if err != nil {
		t.Fatalf("read rebuilt first report artifact: %v", err)
	}
	if workpacket.Digest(finalReportBytes) == stagedSHA {
		t.Fatal("successful retry reused the stale pre-sibling report artifact instead of rebuilding its predicted CAS receipt")
	}

	state := firstAccepted.State
	review, _ := state["review"].(map[string]any)
	repairState, _ := review["repair"].(map[string]any)
	refs, _ := repairState["plan_report_refs"].([]any)
	indexed := map[string]string{}
	for _, raw := range refs {
		row, _ := raw.(map[string]any)
		path, _ := row["path"].(string)
		sha, _ := row["sha256"].(string)
		if path != "" {
			indexed[path] = sha
		}
	}
	firstPath := firstReceipt.ReportRef
	secondPath := secondReceipt.ReportRef
	secondReportBytes := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(secondPath)))
	if len(indexed) != 2 || indexed[firstPath] != workpacket.Digest(finalReportBytes) || indexed[secondPath] != workpacket.Digest(secondReportBytes) {
		t.Fatalf("retry did not preserve both exact sibling report refs: refs=%v firstSHA=%s secondSHA=%s", indexed, workpacket.Digest(finalReportBytes), workpacket.Digest(secondReportBytes))
	}
	if repairState["plan_report_ref"] != firstPath {
		t.Fatalf("singular current report pointer=%v, want retried first report %s", repairState["plan_report_ref"], firstPath)
	}
	if verifiedSecond, ok, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, state, second.ID, second.Binding.OwnerAgentID); err != nil || !ok || verifiedSecond.AcceptedEventID != secondReceipt.AcceptedEventID {
		t.Fatalf("sibling receipt/event was lost when stale first artifact was rebuilt: accepted=%v receipt=%+v err=%v", ok, verifiedSecond, err)
	}
	assertS9ReportEvents(t, fixture, firstReceipt, secondReceipt)

	// Exact replay, even with the old expected revision, is receipt-only and
	// cannot add another PTR-BUG-10 event or mutate either report artifact.
	committedState, committedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	committedFirst, committedSecond := mustRead(t, stageAbs), secondReportBytes
	if _, _, err := repair.SubmitIssuedRepairPlanReport(fixture.root, fixture.statePath, fixture.journalPath, repair.SubmitIssuedRepairPlanReportRequest{
		ExpectedRevision: start.Revision,
		PacketID:         first.ID,
		AnswerPath:       firstAnswer,
		OwnerAgentID:     first.Binding.OwnerAgentID,
		SourceRoot:       fixture.root,
		AuthorizeSource: func(root string, state map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("unexpected source authority root %q", root)
			}
			if err := runtime.VerifyRepairPlanReportPacketIssue(root, state, first); err != nil {
				return err
			}
			return runtime.VerifyRepairPlanReportCurrent(root, state, first)
		},
	}); err != nil {
		t.Fatalf("exact accepted S9 retry with stale expected revision: %v", err)
	}
	if !bytes.Equal(committedState, mustRead(t, fixture.statePath)) || !bytes.Equal(committedJournal, mustRead(t, fixture.journalPath)) ||
		!bytes.Equal(committedFirst, mustRead(t, stageAbs)) || !bytes.Equal(committedSecond, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(secondPath)))) {
		t.Fatal("exact accepted replay mutated Runtime, journal, or either sibling report artifact")
	}
}

func assertS9ReportEvents(t *testing.T, fixture *intakeFixture, receipts ...runtime.RepairPlanReportReceipt) {
	t.Helper()
	want := make(map[string]runtime.RepairPlanReportReceipt, len(receipts))
	for _, receipt := range receipts {
		want[receipt.AcceptedEventID] = receipt
	}
	counts := make(map[string]int, len(want))
	ptrBug10Count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(mustRead(t, fixture.journalPath)), []byte("\n")) {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode S9 report retry journal event: %v", err)
		}
		if event["transition_id"] != "PTR-BUG-10" {
			continue
		}
		ptrBug10Count++
		id, _ := event["event_id"].(string)
		if receipt, ok := want[id]; ok && event["event"] == "transition_committed" && event["idempotency_key"] == receipt.AcceptedIdempotencyKey {
			counts[id]++
		}
	}
	if ptrBug10Count != len(receipts) {
		t.Fatalf("original S9 PlanReport journal has %d PTR-BUG-10 events, want %d sibling commits", ptrBug10Count, len(receipts))
	}
	for eventID, count := range counts {
		if count != 1 {
			t.Fatalf("original S9 accepted event %s occurs %d times, want exactly one", eventID, count)
		}
	}
	if len(counts) != len(receipts) {
		t.Fatalf("original S9 journal retains %d/%d accepted sibling events", len(counts), len(receipts))
	}
}

type s9HookOwner struct {
	assignmentID         string
	manifestAssignmentID string
	agentID              string
	taskID               string
	teamID               string
}

func prepareS9RepairPlanReportHookFixture(t *testing.T) (*intakeFixture, repair.RepairPlan, []s9HookOwner) {
	return prepareS9RepairPlanReportHookFixtureWithSetup(t, nil)
}

func prepareS9RepairPlanReportReadingFixture(t *testing.T) (*intakeFixture, repair.RepairPlan, []s9HookOwner) {
	return prepareS9RepairPlanReportHookFixtureWithActivity(t, nil, false)
}

// prepareS9RepairPlanReportHookFixtureWithSetup lets a later original-Writer
// integration test install repository-owned bootstrap inputs before the
// immutable RepairSession opens. The existing wrapper deliberately keeps its
// original fixture semantics.
func prepareS9RepairPlanReportHookFixtureWithSetup(t *testing.T, setup func(*testing.T, string)) (*intakeFixture, repair.RepairPlan, []s9HookOwner) {
	return prepareS9RepairPlanReportHookFixtureWithActivity(t, setup, true)
}

func prepareS9RepairPlanReportHookFixtureWithActivity(t *testing.T, setup func(*testing.T, string), beginBuilders bool) (*intakeFixture, repair.RepairPlan, []s9HookOwner) {
	t.Helper()
	fixture := newIntakeFixture(t, []string{"finding-1", "finding-2"})
	state := currentS9State(t, fixture)
	state["schema_version"] = "1.2.0"
	state["baseline"].(map[string]any)["generation"] = 2
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["bound_req"].(map[string]any)["workspace"] = bindS8FixtureGitRoot(t, fixture.root)
	writeInvestigationJSON(t, fixture.statePath, state)
	writeObservationBatch(t, fixture, []string{"finding-1", "finding-2"}, 2)
	setContractLifecycle(t, fixture)

	if _, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision: 0, GroupingRationale: "the two sealed Findings enter one bounded generation-two repair Case",
	}); err != nil {
		t.Fatalf("original generation-two Ingest: %v", err)
	}
	prepareCaseForContractApproval(t, fixture)
	contractPath := writeContractDraft(t, fixture.root, []string{"finding-1", "finding-2"})
	contractBytes := mustRead(t, contractPath)
	var contract map[string]any
	if err := json.Unmarshal(contractBytes, &contract); err != nil {
		t.Fatal(err)
	}
	contract["repair_units"] = []any{
		map[string]any{"id": "unit-api-contract", "description": "repair the first assertion slice", "assertion_ids": []string{"symptom-1", "root-1"}, "scope": []string{"internal/api"}},
		map[string]any{"id": "unit-api-regression", "description": "repair the sibling detection assertion", "assertion_ids": []string{"symptom-2", "gap-1"}, "scope": []string{"internal/api"}},
	}
	writeInvestigationJSON(t, contractPath, contract)
	approvalHash, approvalEvidenceID, expectedRevision := registerContractApprovalEvidence(t, fixture, contractPath)
	approved, err := investigation.ApproveContract(fixture.root, fixture.statePath, fixture.journalPath, investigation.ContractRequest{
		ExpectedRevision: expectedRevision, CaseID: "investigation-case-observation-batch-r1", ContractPath: contractPath,
		ApprovedBy: "main-session", ApprovalHash: approvalHash, ApprovalEvidenceID: approvalEvidenceID,
	})
	if err != nil {
		t.Fatalf("original S8 ApproveContract writer: %v", err)
	}
	if approved.State["review"].(map[string]any)["investigation"].(map[string]any)["status"] != "contract_approved" {
		t.Fatal("original ApproveContract did not retain contract_approved authority")
	}

	// Make the original dispatch's repository-owned definition inputs present
	// before opening the Session baseline. The test still uses the public
	// original repair dispatch and Register for both Assignment rows.
	copyS9RepositoryFile(t, filepath.Join("..", "..", "agents", "backend-builder.md"), filepath.Join(fixture.root, "agents", "backend-builder.md"))
	copyS9RepositoryFile(t, filepath.Join("..", "..", "docs", "control", "agent-protocol.md"), filepath.Join(fixture.root, "docs", "control", "agent-protocol.md"))
	if setup != nil {
		setup(t, fixture.root)
	}
	agentDefinitionRef := "agents/backend-builder.md"
	if setup != nil {
		agentDefinitionRef = ".claude/agents/backend-builder.md"
	}

	opened, _, _, err := repair.OpenRepairSession(fixture.root, fixture.statePath, fixture.journalPath, repair.OpenSessionRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: approved.Revision, Actor: "main-session"},
		SessionID:      "repair-session-s9-hook-chain", CreatedBy: "main-session",
	})
	if err != nil {
		t.Fatalf("original S9 session open after approved Contract: %v", err)
	}
	compiled, plan, _, err := repair.CompileRepairPlan(fixture.root, fixture.statePath, fixture.journalPath, repair.CompilePlanRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: opened.Revision, Actor: "main-session"},
		PlanID:         "repair-plan-s9-hook-chain", CreatedBy: "main-session",
	})
	if err != nil {
		t.Fatalf("original S9 RepairPlan compile: %v", err)
	}
	if compiled.State["lifecycle"].(map[string]any)["phase"] != "planning" || len(plan.Assignments) != 2 {
		t.Fatalf("original S9 plan/cursor = assignments=%d lifecycle=%v", len(plan.Assignments), compiled.State["lifecycle"])
	}

	owners := make([]s9HookOwner, 0, len(plan.Assignments))
	for index, assignment := range plan.Assignments {
		owner := s9HookOwner{
			assignmentID: assignment.AssignmentID,
			agentID:      fmt.Sprintf("agent-builder-s9-hook-%02d", index+1),
		}
		var out, errout bytes.Buffer
		if code := cli.Run([]string{"runtime", "repair", "dispatch", "--root", fixture.root,
			"--assignment-id", assignment.AssignmentID, "--agent-id", owner.agentID, "--role-family", "backend-builder"}, nil, &out, &errout); code != 0 {
			t.Fatalf("original S9 RepairAssignment dispatch %s: code=%d stderr=%s stdout=%s", assignment.AssignmentID, code, errout.String(), out.String())
		}
		var dispatch struct {
			TaskID       string `json:"task_id"`
			TeamID       string `json:"workgroup_id"`
			ManifestPath string `json:"manifest_path"`
		}
		if err := json.Unmarshal(out.Bytes(), &dispatch); err != nil {
			t.Fatalf("decode original S9 dispatch response: %v", err)
		}
		manifestBytes := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(dispatch.ManifestPath)))
		var manifest map[string]any
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			t.Fatalf("decode original generated S9 manifest: %v", err)
		}
		manifestAssignments, ok := manifest["assignments"].([]any)
		if !ok || len(manifestAssignments) != 1 {
			t.Fatalf("original S9 dispatch manifest assignments = %#v, want one", manifest["assignments"])
		}
		manifestAssignment, _ := manifestAssignments[0].(map[string]any)
		manifestAssignmentID, _ := manifestAssignment["assignment_id"].(string)
		owner.taskID, owner.teamID, owner.manifestAssignmentID = dispatch.TaskID, dispatch.TeamID, manifestAssignmentID
		if owner.manifestAssignmentID == "" {
			t.Fatal("original S9 manifest has no registered workgroup Assignment ID")
		}
		if beginBuilders {
			runS9AgentBeginWithDefinition(t, fixture, owner, agentDefinitionRef)
		}
		owners = append(owners, owner)
	}
	return fixture, plan, owners
}

func runS9AgentBegin(t *testing.T, fixture *intakeFixture, owner s9HookOwner) {
	runS9AgentBeginWithDefinition(t, fixture, owner, "agents/backend-builder.md")
}

func runS9AgentBeginWithDefinition(t *testing.T, fixture *intakeFixture, owner s9HookOwner, agentDefinitionRef string) {
	t.Helper()
	planRel := filepath.ToSlash(filepath.Join(".claude", "plans", owner.agentID+"-plan.json"))
	planPath := filepath.Join(fixture.root, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeInvestigationJSON(t, planPath, map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-" + owner.agentID + "-plan",
		"correlation_id": "corr-" + owner.assignmentID, "runtime_id": "loop-REQ-INTAKE", "agent_id": owner.agentID,
		"agent_definition_ref": agentDefinitionRef, "task_id": owner.taskID, "bug_id": nil,
		"team_id": owner.teamID, "occurred_at": "2026-10-14T12:30:00Z", "assignment_id": owner.manifestAssignmentID, "assignment_revision": 1,
		"objective":        "Prepare the exact pre-write plan for the approved RepairAssignment.",
		"planned_paths":    []string{"internal/api"},
		"steps":            []any{map[string]any{"description": "read the approved contract and reproduce the pre-fix failure", "target": "internal/api"}},
		"assertion_checks": []any{map[string]any{"assertion": "the assigned assertion has a reproducible pre-fix failure", "oracle": "the named red check has source-anchored evidence"}},
		"dependencies":     []string{}, "risks_blockers": []string{},
	})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "agent-begin", "--root", fixture.root, "--agent-id", owner.agentID, "--plan", planPath}, nil, &out, &errout); code != 0 {
		t.Fatalf("original generic PLAN/AgentBegin for %s: code=%d stderr=%s stdout=%s", owner.agentID, code, errout.String(), out.String())
	}
	snapshot := currentS9Snapshot(t, fixture)
	if !s9AgentWorking(snapshot.State, owner.agentID) {
		t.Fatalf("original AgentBegin did not place %s in working state", owner.agentID)
	}
}

func writeS9RepairPlanReportAnswer(t *testing.T, root string, packet workpacket.RepairPlanReportPacket, planText string) string {
	t.Helper()
	answer := workpacket.RepairPlanReportProfessionalAnswer{
		Plan: planText,
		RedChecks: []workpacket.RepairPlanReportCheck{{
			Name: "pre-fix contract check", Command: "go test ./internal/api -run TestPayloadContract",
			Result: "fail", EvidenceRefs: []string{"test://s9-pre-fix/" + packet.Binding.RepairAssignmentID},
		}},
		ProposedPaths: []string{"internal/api/payload.go"},
	}
	data, err := json.Marshal(struct {
		Title       string                                        `json:"title"`
		Instruction string                                        `json:"instruction"`
		Read        []workpacket.ReadItem                         `json:"read"`
		Questions   map[string]workpacket.Question                `json:"questions"`
		Answer      workpacket.RepairPlanReportProfessionalAnswer `json:"answer"`
	}{Title: packet.Draft.Title, Instruction: packet.Draft.Instruction, Read: packet.Draft.Read, Questions: packet.Draft.Questions, Answer: answer})
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := workpacket.ValidateRepairPlanReportAnswer(packet, data); len(problems) != 0 {
		t.Fatalf("fixture S9 answer is invalid: %+v", problems)
	}
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return packet.AnswerPath
}

type s9PostToolReceipt struct {
	PacketID             string `json:"packet_id"`
	Accepted             bool   `json:"accepted"`
	ResponsibilityStatus string `json:"responsibility_status"`
	WorkComplete         bool   `json:"work_complete"`
	Error                string `json:"error"`
}

func runS9PostToolUse(t *testing.T, fixture *intakeFixture, owner, cwd, answerPath string) s9PostToolReceipt {
	t.Helper()
	path := filepath.Join(fixture.root, filepath.FromSlash(answerPath))
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "Write",
		"tool_input": map[string]any{"file_path": path}, "agent_id": owner, "cwd": cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("original S9 PostToolUse Hook code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	var envelope struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode original S9 Hook envelope: %v; stdout=%s stderr=%s", err, out.String(), errout.String())
	}
	const prefix = "S9 SUBMISSION RECEIPT: "
	if !strings.HasPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix) {
		t.Fatalf("original S9 Hook did not return a domain receipt: %s", out.String())
	}
	var receipt s9PostToolReceipt
	receiptBytes := []byte(strings.TrimPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix))
	assertSharedSubmissionReceiptWire(t, receiptBytes)
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode original S9 receipt: %v", err)
	}
	return receipt
}

func assertS9ProductWriteObserved(t *testing.T, fixture *intakeFixture, owner s9HookOwner) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": "Edit", "tool_input": map[string]any{"file_path": "internal/api/payload.go"},
		"agent_id": owner.agentID, "cwd": fixture.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("S9 pre-begin PreToolUse Hook code=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	var envelope struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
			PermissionReason   string `json:"permissionDecisionReason"`
			AdditionalContext  string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode pre-begin product-write decision: %v; stdout=%s", err, out.String())
	}
	if envelope.HookSpecificOutput.PermissionDecision == "deny" {
		t.Fatalf("S9 listener denied an observed tool: %s", out.String())
	}
	packets, err := runtime.RepairPlanReportPacketsContext(runtime.WithPacketBodyRoot(t.Context(), fixture.root), currentS9State(t, fixture))
	if err != nil {
		t.Fatalf("read registered S9 questionnaire path: %v", err)
	}
	answerPath := ""
	for _, packet := range packets {
		if packet.Binding.OwnerAgentID == owner.agentID && packet.Binding.RepairAssignmentID == owner.assignmentID {
			answerPath = filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
		}
	}
	context := envelope.HookSpecificOutput.AdditionalContext
	if answerPath == "" || !strings.Contains(context, "questionnaire "+answerPath) || !strings.Contains(context, "The PlanReport is recorded. Continue the same native Builder") || strings.Contains(context, "runtime stage-answer issue") {
		t.Fatalf("S9 reminder omitted the exact questionnaire path or original next duty: %s", context)
	}
}

func assertS9RuntimeBytesUnchanged(t *testing.T, fixture *intakeFixture, state, journal []byte, label string) {
	t.Helper()
	if !bytes.Equal(state, mustRead(t, fixture.statePath)) || !bytes.Equal(journal, mustRead(t, fixture.journalPath)) {
		t.Fatalf("%s changed Runtime state or original journal", label)
	}
}

func copyS9RepositoryFile(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read repository fixture source %s: %v", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func currentS9Snapshot(t *testing.T, fixture *intakeFixture) runtime.Snapshot {
	t.Helper()
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func currentS9State(t *testing.T, fixture *intakeFixture) map[string]any {
	t.Helper()
	return currentS9Snapshot(t, fixture).State
}

func s9AgentWorking(state map[string]any, agentID string) bool {
	row := s9AgentRow(state, agentID)
	return row != nil && row["state"] == "working"
}

func s9AgentRow(state map[string]any, agentID string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == agentID {
			return row
		}
	}
	return nil
}

// Use the same original receiver as terminal/native reception; synthetic fixtures
// supply their source-root authority explicitly without restoring save reception.
func submitS9OriginalReceipt(t *testing.T, fixture *intakeFixture, packet workpacket.RepairPlanReportPacket, answer string) s9PostToolReceipt {
	t.Helper()
	_, _, err := submitS9RecoveryTestPacket(fixture, -1, packet, answer, packet.Binding.OwnerAgentID)
	if err != nil {
		return s9PostToolReceipt{PacketID: packet.ID, Error: err.Error()}
	}
	receipt, accepted, err := repair.IssuedRepairPlanReportReceipt(fixture.root, currentS9State(t, fixture), packet.ID, packet.Binding.OwnerAgentID)
	if err != nil {
		t.Fatal(err)
	}
	return s9PostToolReceipt{PacketID: packet.ID, Accepted: accepted, WorkComplete: receipt.WorkComplete}
}
