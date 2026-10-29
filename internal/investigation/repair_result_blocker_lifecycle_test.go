package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/cli"
	loopHook "github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// TestS9RepairResultBlockerUsesOriginalAgentLifecycleCAS exercises a typed
// non-pass result through the original Agent lifecycle Writer. The authored
// nine-field answers below are mechanical fixture data, and fakeClaude only
// exercises the registered Workspace launch path; this is not a model or live
// platform Hook acceptance claim.
func TestS9RepairResultBlockerUsesOriginalAgentLifecycleCAS(t *testing.T) {
	fixture, owner, packet, execution, workerRoot, answerBytes, issued := prepareWorkingS9RepairResultPacket(t)
	if !s9AgentWorking(issued.State, owner.agentID) {
		t.Fatal("the original Agent must still be working when its S9 Result packet is issued")
	}
	issuedAgent := agentLifecycleRow(issued.State, owner.agentID)
	if issuedAgent == nil {
		t.Fatal("issued S9 packet has no registered owner Agent")
	}
	if issuedAgent["completion_reported_ref"] != nil && issuedAgent["completion_reported_ref"] != "" {
		t.Fatal("blocker fixture must not report completion before S9 Result acceptance")
	}
	if _, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, issued.State, packet.ID, owner.agentID); err != nil || found {
		t.Fatalf("working blocker fixture unexpectedly has an accepted candidate: found=%t err=%v", found, err)
	}
	if _, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, issued.State, packet.ID, owner.agentID); err != nil || found {
		t.Fatalf("working blocker fixture unexpectedly has a final Result: found=%t err=%v", found, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "internal/api/payload.go")); !os.IsNotExist(err) {
		t.Fatalf("blocker receipt must not require a Main product change, stat error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workerRoot, "internal/api/payload.go")); !os.IsNotExist(err) {
		t.Fatalf("blocker receipt must not require a Worker product change, stat error=%v", err)
	}
	if len(execution.ToolCheckReceipts) != 0 || execution.CheckLocation != "main" || execution.Status != "ready" {
		t.Fatalf("fixture must have a ready registered Worker with Main-owned checks not yet run: %+v", execution)
	}
	workerHead, err := workspace.Git(context.Background(), workerRoot, "rev-parse", "HEAD")
	if err != nil || workerHead != execution.BaseCommit {
		t.Fatalf("blocker receipt fixture unexpectedly committed Worker product changes: head=%s base=%s err=%v", workerHead, execution.BaseCommit, err)
	}

	// All three non-pass professional verdicts remain complete, closed answer
	// values. Only the first is written and accepted below; none implies a
	// machine PASS or a candidate/Main Result.
	for _, verdict := range []string{"blocked", "contract_conflict", "stale"} {
		data := encodeS9NonpassRepairResultAnswer(t, packet, verdict)
		if _, problems := workpacket.ValidateRepairResultAnswer(packet, data); len(problems) != 0 {
			t.Fatalf("complete %s answer was rejected by the neutral packet validator: %+v", verdict, problems)
		}
	}
	blockedAnswer := encodeS9NonpassRepairResultAnswer(t, packet, "blocked")
	platformOwner, err := workspace.SessionOwner(context.Background(), fixture.root, workerRoot, execution.PlatformSessionID)
	if err != nil || platformOwner != owner.agentID {
		t.Fatalf("preflight registered Worker platform session owner=%q err=%v, want %q", platformOwner, err, owner.agentID)
	}
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath)), append(blockedAnswer, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	answerBytes = append(append([]byte(nil), blockedAnswer...), '\n')
	// A harmless untracked diagnostic file demonstrates that a nonpass blocker
	// does not depend on CleanInputs or a clean product tree.
	notePath := filepath.Join(workerRoot, "s9-blocker-diagnostic.txt")
	if err := os.WriteFile(notePath, []byte("mechanical blocker fixture; not a product artifact\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workerStatus, err := workspace.Git(context.Background(), workerRoot, "status", "--porcelain", "--untracked-files=all")
	if err != nil || !strings.Contains(workerStatus, "s9-blocker-diagnostic.txt") {
		t.Fatalf("fixture failed to establish a harmless dirty/untracked Worker state: status=%q err=%v", workerStatus, err)
	}

	beforePrepareState := mustRead(t, fixture.statePath)
	beforePrepareJournal := mustRead(t, fixture.journalPath)
	request := repair.AcceptIssuedRepairResultBlockerRequest{
		ExpectedRevision: issued.Revision,
		PacketID:         packet.ID,
		AnswerPath:       packet.AnswerPath,
		OwnerAgentID:     owner.agentID,
		SourceRoot:       workerRoot,
	}
	lockedCtx, unlock, err := integration.LockWorkspace(context.Background(), fixture.root)
	if err != nil {
		t.Fatalf("acquire outer workspace lock for Prepare→Agent lifecycle CAS: %v", err)
	}
	defer unlock()
	capability, priorReceipt, err := repair.PrepareIssuedRepairResultBlocker(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil || capability == nil || priorReceipt != nil {
		t.Fatalf("prepare current working S9 nonpass answer: capability=%v receipt=%+v err=%v", capability != nil, priorReceipt, err)
	}
	if capability.MessageRef() == "" || len(capability.EvidenceIDs()) != 2 {
		t.Fatalf("prepared lifecycle capability did not bind its exact immutable message and artifact: message=%q evidence=%v", capability.MessageRef(), capability.EvidenceIDs())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforePrepareState, beforePrepareJournal, "S9 blocker prepare before original lifecycle CAS")

	messagePath := filepath.Join(fixture.root, filepath.FromSlash(capability.MessageRef()))
	workBlocked := assignment.AgentEventRequest{
		ExpectedRevision: issued.Revision,
		AgentID:          owner.agentID,
		Event:            "work_blocked",
		MessagePath:      messagePath,
		OccurredAt:       time.Now().UTC(),
	}
	// A packet-targeted generic lifecycle call cannot turn an S9 answer into a
	// blocker without the opaque original-writer capability.
	if _, err := advanceS9AgentWithContext(lockedCtx, fixture, workBlocked); err == nil {
		t.Fatal("generic work_blocked accepted an issued S9 packet without its capability")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforePrepareState, beforePrepareJournal, "generic S9 work_blocked refusal without capability")

	// The opaque capability also rejects an answer changed after preparation;
	// restoring the exact authored bytes leaves that same capability usable.
	changed := bytes.Replace(answerBytes, []byte(`"verdict":"blocked"`), []byte(`"verdict":"stale"`), 1)
	if bytes.Equal(changed, answerBytes) {
		t.Fatal("changed S9 blocker answer fixture did not alter its verdict")
	}
	answerFile := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(answerFile, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := advanceS9AgentWithContext(lockedCtx, fixture, workBlockedWithCapability(workBlocked, capability)); err == nil {
		t.Fatal("original lifecycle capability accepted an answer changed after blocker preparation")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforePrepareState, beforePrepareJournal, "changed S9 blocker answer refusal")
	if err := os.WriteFile(answerFile, answerBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	accepted, err := advanceS9AgentWithContext(lockedCtx, fixture, workBlockedWithCapability(workBlocked, capability))
	if err != nil {
		t.Fatalf("original Agent lifecycle Writer rejected the issued S9 blocker capability: %v", err)
	}
	if accepted.Revision != issued.Revision+1 {
		t.Fatalf("original blocker lifecycle revision=%d, want exactly one CAS after %d", accepted.Revision, issued.Revision)
	}
	blockedAgent := agentLifecycleRow(accepted.State, owner.agentID)
	if blockedAgent == nil || blockedAgent["state"] != "blocked" || blockedAgent["work_blocked_ref"] != capability.MessageRef() {
		t.Fatalf("original lifecycle CAS did not atomically block the owner and set its message pointer: %#v", blockedAgent)
	}
	if blockedAgent["blocker_resolved_ref"] != nil && blockedAgent["blocker_resolved_ref"] != "" {
		t.Fatalf("new blocker unexpectedly has a resolution pointer: %#v", blockedAgent["blocker_resolved_ref"])
	}
	if blockedAgent["completion_reported_ref"] != nil && blockedAgent["completion_reported_ref"] != "" {
		t.Fatalf("blocked owner unexpectedly reported completion: %#v", blockedAgent["completion_reported_ref"])
	}
	if _, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, accepted.State, packet.ID, owner.agentID); err != nil || found {
		t.Fatalf("typed blocker transition created a candidate receipt: found=%t err=%v", found, err)
	}
	if _, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, accepted.State, packet.ID, owner.agentID); err != nil || found {
		t.Fatalf("typed blocker transition created a final Main Result: found=%t err=%v", found, err)
	}
	receipt, found, err := runtime.VerifiedRepairResultBlockerReceipt(fixture.root, accepted.State, packet.ID, owner.agentID)
	if err != nil || !found {
		t.Fatalf("original journal/evidence reader did not verify the blocker receipt: found=%t err=%v", found, err)
	}
	if receipt.Contract != "s9-repair-result-blocker-receipt-v1" || receipt.Artifact.PacketID != packet.ID || receipt.Artifact.AnswerPath != packet.AnswerPath || receipt.Artifact.ExecutionRoot != workerRoot || receipt.Artifact.PlatformSessionID != execution.PlatformSessionID || receipt.Artifact.AnswerSHA256 == "" {
		t.Fatalf("verified blocker receipt lost its exact packet/answer/Worker source binding: %+v", receipt)
	}
	if receipt.AcceptedAfterRuntimeRevision != accepted.Revision || receipt.AcceptedIdempotencyKey != fmt.Sprintf("runtime:agent:%s:work_blocked:%d", owner.agentID, issued.Revision) || receipt.AcceptedEventID != fmt.Sprintf("evt-agent-%s-work_blocked-r%d", owner.agentID, accepted.Revision) || receipt.AcceptedSequence <= 0 {
		t.Fatalf("verified blocker receipt has wrong original lifecycle CAS coordinates: %+v", receipt)
	}
	if receipt.Artifact.MessageRef != capability.MessageRef() || receipt.Artifact.MessageSHA256 == "" || receipt.ArtifactRef == "" || receipt.ArtifactSHA256 == "" {
		t.Fatalf("verified blocker receipt lacks the exact message/artifact digest chain: %+v", receipt)
	}
	mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, accepted.State, packet.ID, owner.agentID)
	if err != nil || !mayEnd {
		t.Fatalf("current exact S9 blocker did not authorize only execution ending: mayEnd=%t err=%v", mayEnd, err)
	}
	blockerAgent := &policy.AgentContext{
		ID: owner.agentID, State: "blocked", Activity: "blocked", LifecycleEvent: "work_blocked",
		WorkBlockedRef: receipt.Artifact.MessageRef, RepairResultAssignment: true,
		AssignmentID:       packet.Binding.Sources.ManifestAssignmentID,
		RepairAssignmentID: packet.Binding.Sources.RepairAssignmentID,
	}
	blockerAssignment := &hookctx.AssignmentContext{
		AssignmentID: packet.Binding.Sources.ManifestAssignmentID, OwnerAgentID: owner.agentID,
	}
	facts := loopHook.CurrentResponsibility(fixture.root, accepted, blockerAgent, blockerAssignment, false)
	if facts.Status != "blocked" || !facts.ExecutionMayEnd || facts.DeliveryRecorded || facts.ConsumerOwner != "main" || !facts.ConsumerPending {
		t.Fatalf("Hook responsibility projection lost the exact current S9 blocker facts: %+v", facts)
	}
	changedAnswer := bytes.Replace(answerBytes, []byte(`"verdict":"blocked"`), []byte(`"verdict":"stale"`), 1)
	if bytes.Equal(changedAnswer, answerBytes) {
		t.Fatal("accepted blocker answer fixture did not expose its verdict field")
	}
	if err := os.WriteFile(answerFile, changedAnswer, 0o600); err != nil {
		t.Fatal(err)
	}
	answerMayEnd, answerErr := repair.RepairResultBlockerExecutionMayEnd(fixture.root, accepted.State, packet.ID, owner.agentID)
	restoreAnswerErr := os.WriteFile(answerFile, answerBytes, 0o600)
	if restoreAnswerErr != nil {
		t.Fatal(restoreAnswerErr)
	}
	if answerErr != nil || answerMayEnd {
		t.Fatalf("changed current Worker answer retained historical blocker Stop authority: mayEnd=%t err=%v", answerMayEnd, answerErr)
	}
	cloneAcceptedState := func() map[string]any {
		t.Helper()
		data, err := json.Marshal(accepted.State)
		if err != nil {
			t.Fatal(err)
		}
		var cloned map[string]any
		if err := json.Unmarshal(data, &cloned); err != nil {
			t.Fatal(err)
		}
		return cloned
	}
	stalePointer := cloneAcceptedState()
	agentLifecycleRow(stalePointer, owner.agentID)["work_blocked_ref"] = "other-message.json"
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, stalePointer, packet.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("wrong current work_blocked_ref retained Stop authority: mayEnd=%t err=%v", mayEnd, err)
	}
	invalidated := cloneAcceptedState()
	rows, _ := invalidated["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == receipt.ArtifactRef {
			row["status"] = "invalid"
			row["invalidated_by"] = "evt-later-invalidation"
		}
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, invalidated, packet.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("invalidated historical blocker retained Stop authority: mayEnd=%t err=%v", mayEnd, err)
	}
	resolved := cloneAcceptedState()
	agentLifecycleRow(resolved, owner.agentID)["blocker_resolved_ref"] = "resolution.json"
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, resolved, packet.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("resolved historical blocker retained Stop authority: mayEnd=%t err=%v", mayEnd, err)
	}
	if rows, ok := accepted.State["evidence"].([]any); !ok || !hasS9BlockerEvidenceRow(rows, receipt) {
		t.Fatalf("original lifecycle CAS did not append the receipt's one typed evidence row: %#v", accepted.State["evidence"])
	}
	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	assertS9BlockerCommittedJournalIdentity(t, acceptedJournal, receipt)

	// Exact historical Prepare replay is receipt-first and zero-write even with
	// the old expected revision and blocked Agent state.
	replayedCapability, replayedReceipt, err := repair.PrepareIssuedRepairResultBlocker(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil || replayedCapability != nil || replayedReceipt == nil || replayedReceipt.AcceptedEventID != receipt.AcceptedEventID || replayedReceipt.ArtifactRef != receipt.ArtifactRef || replayedReceipt.ArtifactSHA256 != receipt.ArtifactSHA256 {
		t.Fatalf("exact old-revision blocker Prepare replay did not return its original receipt: capability=%v receipt=%+v err=%v", replayedCapability != nil, replayedReceipt, err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact old-revision S9 blocker Prepare replay")

	// The old opaque capability replays through the original lifecycle API and
	// returns the accepted snapshot without another Runtime or journal write.
	replayed, err := advanceS9AgentWithContext(lockedCtx, fixture, workBlockedWithCapability(workBlocked, capability))
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("exact old-capability lifecycle replay did not return the accepted state: revision=%d err=%v", replayed.Revision, err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact old-capability S9 blocker lifecycle replay")

	// A generic resolution message cannot use the legacy event to bypass the
	// issued packet's distinct typed resolution path.
	resolutionPath := writeS9GenericBlockerResolution(t, fixture, owner, packet, receipt)
	noCapResolution := assignment.AgentEventRequest{
		ExpectedRevision: accepted.Revision,
		AgentID:          owner.agentID,
		Event:            "blocker_resolved",
		MessagePath:      resolutionPath,
		OccurredAt:       time.Now().UTC(),
	}
	if _, err := advanceS9AgentWithContext(lockedCtx, fixture, noCapResolution); err == nil {
		t.Fatal("generic blocker_resolved accepted an issued S9 packet without its resolution capability")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "generic S9 blocker_resolved refusal without capability")
}

func prepareWorkingS9RepairResultPacket(t *testing.T) (*intakeFixture, s9HookOwner, workpacket.RepairResultPacket, workspace.Execution, string, []byte, runtime.Snapshot) {
	t.Helper()
	return prepareS9RepairResultPacketWithPlanActivity(t, true)
}

func prepareS9RepairResultPacketWithPlanActivity(t *testing.T, beginBuilder bool) (*intakeFixture, s9HookOwner, workpacket.RepairResultPacket, workspace.Execution, string, []byte, runtime.Snapshot) {
	t.Helper()
	fixture, plan, owners := prepareS9RepairPlanReportHookFixtureWithActivity(t, installS9WorkerBootstrapInputs, beginBuilder)
	if len(plan.Assignments) != 2 || len(owners) != 2 {
		t.Fatalf("fixture assignments/owners=%d/%d, want two original S9 sibling Assignments", len(plan.Assignments), len(owners))
	}
	owner := owners[0]
	redCommand := "test -f internal/api/payload.go"
	if err := runS9FixtureCommand(fixture.root, "sh", "-c", redCommand); err == nil {
		t.Fatal("original pre-fix S9 red command unexpectedly passed")
	}
	var issueReportOut, issueReportErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &issueReportOut, &issueReportErr); code != 0 {
		t.Fatalf("issue original S9 PlanReports: code=%d stderr=%s stdout=%s", code, issueReportErr.String(), issueReportOut.String())
	}
	var reportPackets []workpacket.RepairPlanReportPacket
	if err := json.Unmarshal(issueReportOut.Bytes(), &reportPackets); err != nil || len(reportPackets) != 2 {
		t.Fatalf("decode two original S9 PlanReport packets: count=%d err=%v stdout=%s", len(reportPackets), err, issueReportOut.String())
	}
	for _, reportPacket := range reportPackets {
		answerPath := writeS9RepairPlanReportForResult(t, fixture.root, reportPacket, redCommand)
		receipt := runS9PostToolUse(t, fixture, reportPacket.Binding.OwnerAgentID, fixture.root, answerPath)
		if !receipt.Accepted || receipt.PacketID != reportPacket.ID || receipt.WorkComplete {
			t.Fatalf("original S9 PlanReport coverage was not accepted before Begin: %+v", receipt)
		}
	}
	started := currentS9Snapshot(t, fixture)
	if life, _ := started.State["lifecycle"].(map[string]any); life == nil || life["phase"] != "fixing" {
		t.Fatalf("personal PlanReport save did not reach normal work startup: %v", started.State["lifecycle"])
	}
	if code := runS9Command(t, fixture.root, "runtime", "workspace", "bind", "--root", fixture.root); code != 0 {
		t.Fatal("original Main workspace bind failed")
	}
	prepareOut := runS9CommandOutput(t, fixture.root, "runtime", "workspace", "prepare", "--root", fixture.root,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--check-location", "main")
	var preparation struct {
		Execution workspace.Execution `json:"execution"`
	}
	if err := json.Unmarshal(prepareOut, &preparation); err != nil || preparation.Execution.Status != "ready" || preparation.Execution.CheckLocation != "main" || len(preparation.Execution.Checks) != 0 {
		t.Fatalf("normal S9 prepare must retain Main checks as pending work: output=%s err=%v", prepareOut, err)
	}
	prepared := preparation.Execution
	workerRoot := prepared.Path
	if workerRoot == "" || workerRoot == fixture.root {
		t.Fatalf("original S9 execution must use its separate registered Worker checkout: %q", workerRoot)
	}
	installS9FakeClaude(t)
	_ = runS9CommandOutput(t, fixture.root, "runtime", "workspace", "launch", "--root", fixture.root,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--timeout", "10s")
	launched := currentS9Snapshot(t, fixture)
	registry, err := workspace.Decode(launched.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original S9 Workspace registry after launch: %v", err)
	}
	execution, found := registry.Execution(owner.manifestAssignmentID, "loop-REQ-INTAKE", 2)
	if !found || execution.Status != "ready" || execution.Path != workerRoot || execution.PlatformSessionID == "" {
		t.Fatalf("original S9 Worker execution is not ready with a registered session: found=%t execution=%+v", found, execution)
	}
	issued := currentS9Snapshot(t, fixture)
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), fixture.root), issued.State)
	if err != nil {
		t.Fatal(err)
	}
	var packet workpacket.RepairResultPacket
	for _, candidate := range packets {
		if candidate.Binding.Sources.OwnerAgentID == owner.agentID && candidate.Binding.Sources.ManifestAssignmentID == owner.manifestAssignmentID {
			if packet.ID != "" {
				t.Fatal("normal prepare published multiple personal Result packets")
			}
			packet = candidate
		}
	}
	if packet.ID == "" {
		t.Fatal("normal prepare did not supply the personal Result packet")
	}
	for _, path := range []string{packet.ViewPath, packet.AnswerPath} {
		if _, err := os.Stat(filepath.Join(workerRoot, filepath.FromSlash(path))); err != nil {
			t.Fatalf("normal prepare omitted personal Result file %s: %v", path, err)
		}
	}
	issuedAgent := agentLifecycleRow(issued.State, owner.agentID)
	if issuedAgent == nil {
		t.Fatal("issued S9 packet has no registered owner Agent")
	}
	if issuedAgent["completion_reported_ref"] != nil && issuedAgent["completion_reported_ref"] != "" {
		t.Fatal("S9 Result packet fixture must not call completion_reported")
	}
	answerBytes := encodeS9NonpassRepairResultAnswer(t, packet, "blocked")
	answerPath := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(answerPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(answerPath, append(answerBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture, owner, packet, execution, workerRoot, append(append([]byte(nil), answerBytes...), '\n'), issued
}

func encodeS9NonpassRepairResultAnswer(t *testing.T, packet workpacket.RepairResultPacket, verdict string) []byte {
	t.Helper()
	var assigned struct {
		AssertionIDs []string `json:"assertion_ids"`
		UnitIDs      []string `json:"unit_ids"`
	}
	if err := json.Unmarshal(packet.Inputs.Sources.RepairAssignment, &assigned); err != nil || len(assigned.AssertionIDs) == 0 || len(assigned.UnitIDs) == 0 {
		t.Fatalf("decode exact S9 answer slots: %+v err=%v", assigned, err)
	}
	answer := workpacket.RepairResultProfessionalAnswer{
		Verdict: verdict, BeforeFixResults: []workpacket.RepairResultBeforeFixResult{},
		UnitResults: []workpacket.RepairResultUnitResult{}, DetectionGapArtifacts: []string{},
		EvidenceRefs: []string{packet.Binding.PlanReportRef}, ScopeDeviations: []string{}, ResidualRisks: []string{},
		MigrationRef: "", RollbackRef: "",
	}
	for _, id := range assigned.AssertionIDs {
		answer.BeforeFixResults = append(answer.BeforeFixResults, workpacket.RepairResultBeforeFixResult{
			AssertionID: id, ObservedRed: "Mechanical fixture observation: the approved red check was configured before the repair began.",
			EvidenceRefs: []string{"test://s9-blocker/pre-fix/" + id},
		})
	}
	for _, id := range assigned.UnitIDs {
		answer.UnitResults = append(answer.UnitResults, workpacket.RepairResultUnitResult{
			UnitID: id, Status: "blocked", EvidenceRefs: []string{"test://s9-blocker/unit/" + id},
			ImplementationSummary: "Mechanical fixture records that no product implementation was made before the blocker.",
			RestoredInvariantRefs: []string{},
		})
	}
	data, err := json.Marshal(workpacket.RepairResultDraft{
		Title: packet.Draft.Title, Instruction: packet.Draft.Instruction, Read: packet.Draft.Read,
		Questions: packet.Draft.Questions, Answer: mustJSONRaw(t, answer),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func advanceS9AgentWithContext(ctx context.Context, fixture *intakeFixture, request assignment.AgentEventRequest) (runtime.Snapshot, error) {
	return assignment.AdvanceAgentWithContext(ctx, fixture.root, fixture.statePath, fixture.journalPath, request)
}

func workBlockedWithCapability(request assignment.AgentEventRequest, capability *repair.IssuedRepairResultBlockerCapability) assignment.AgentEventRequest {
	request.IssuedRepairResultBlocker = capability
	return request
}

func agentLifecycleRow(state map[string]any, owner string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == owner {
			return row
		}
	}
	return nil
}

func hasS9BlockerEvidenceRow(rows []any, receipt runtime.RepairResultBlockerReceipt) bool {
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != receipt.ArtifactRef {
			continue
		}
		refs, _ := row["scope_refs"].([]any)
		producers, _ := row["produced_by"].([]any)
		sources := receipt.Artifact.Binding.Sources
		return row["kind"] == "repair_result_blocker" && row["path"] == receipt.ArtifactRef &&
			row["sha256"] == receipt.ArtifactSHA256 && row["status"] == "valid" && row["invalidated_by"] == nil &&
			blockerEvidenceInt(row["baseline_generation"], sources.BaselineGeneration) && blockerEvidenceInt(row["review_round"], sources.ReviewRound) &&
			row["responsibility_id"] == "BUILD-WORK-PACKAGE" && len(producers) == 1 && producers[0] == sources.OwnerAgentID &&
			len(refs) == 2 && containsS9String(refs, receipt.Artifact.PacketID) && containsS9String(refs, sources.RepairAssignmentID)
	}
	return false
}

func assertS9BlockerCommittedJournalIdentity(t *testing.T, journal []byte, receipt runtime.RepairResultBlockerReceipt) {
	t.Helper()
	var events []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(journal), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode original S9 blocker journal row: %v", err)
		}
		events = append(events, event)
	}
	countID, countKey := 0, 0
	var issueEvent, acceptedEvent map[string]any
	for _, event := range events {
		if event["event_id"] == receipt.Artifact.IssuanceEventID {
			issueEvent = event
		}
		if event["event_id"] == receipt.AcceptedEventID {
			countID++
			acceptedEvent = event
		}
		if event["idempotency_key"] == receipt.AcceptedIdempotencyKey && event["outcome"] == "committed" {
			countKey++
		}
	}
	if countID != 1 || countKey != 1 {
		t.Fatalf("original S9 blocker event/key counts = %d/%d, want exactly one committed event", countID, countKey)
	}
	if issueEvent == nil || acceptedEvent == nil || acceptedEvent["transition_id"] != "AGENT-LIFECYCLE" ||
		acceptedEvent["event"] != "transition_committed" || acceptedEvent["outcome"] != "committed" ||
		acceptedEvent["idempotency_key"] != receipt.AcceptedIdempotencyKey ||
		!blockerEvidenceInt(acceptedEvent["baseline_generation"], receipt.Artifact.Binding.Sources.BaselineGeneration) {
		t.Fatalf("original S9 blocker journal lost exact issuance/lifecycle generation proof: issue=%#v accepted=%#v", issueEvent, acceptedEvent)
	}
	issueSequence, issueSeqOK := journalInteger(issueEvent["sequence"])
	acceptedSequence, acceptedSeqOK := journalInteger(acceptedEvent["sequence"])
	acceptedAfter, acceptedAfterOK := journalInteger(acceptedEvent["after_revision"])
	evidenceIDs, evidenceIDsOK := acceptedEvent["evidence_ids"].([]any)
	if !issueSeqOK || !acceptedSeqOK || !acceptedAfterOK || !evidenceIDsOK ||
		acceptedSequence <= issueSequence || acceptedSequence != receipt.AcceptedSequence ||
		acceptedAfter != receipt.AcceptedAfterRuntimeRevision || !containsS9String(evidenceIDs, receipt.Artifact.MessageRef) ||
		!containsS9String(evidenceIDs, receipt.ArtifactRef) || len(evidenceIDs) != 2 {
		t.Fatalf("original S9 blocker journal coordinates/evidence differ from receipt: issue=%#v accepted=%#v receipt=%+v", issueEvent, acceptedEvent, receipt)
	}
}

func containsS9String(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func blockerEvidenceInt(value any, want int) bool {
	switch number := value.(type) {
	case float64:
		return number == float64(want)
	case int:
		return number == want
	case json.Number:
		parsed, err := number.Int64()
		return err == nil && parsed == int64(want)
	default:
		return false
	}
}

func journalInteger(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		integer := int(number)
		return integer, number == float64(integer)
	case int:
		return number, true
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func writeS9GenericBlockerResolution(t *testing.T, fixture *intakeFixture, owner s9HookOwner, packet workpacket.RepairResultPacket, receipt runtime.RepairResultBlockerReceipt) string {
	t.Helper()
	packetSHA, err := workpacket.RepairResultPacketDigest(packet)
	if err != nil {
		t.Fatalf("digest original issued S9 packet for generic resolution negative: %v", err)
	}
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "blocker_resolution",
		"message_id":     "msg-generic-s9-resolution-" + owner.agentID,
		"correlation_id": "corr-generic-s9-resolution-" + owner.assignmentID,
		"runtime_id":     "loop-REQ-INTAKE", "agent_id": owner.agentID,
		"agent_definition_ref": ".claude/agents/backend-builder.md", "task_id": owner.taskID,
		"bug_id": nil, "team_id": owner.teamID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"body":        "Mechanical fixture attempts the generic resolution path without a typed capability.",
		"s9_resolution": map[string]any{
			"decision": "retry", "rationale": "Mechanical no-capability negative; this message grants no authority.",
			"evidence_refs": []string{}, "packet_id": packet.ID, "packet_sha256": packetSHA,
			"blocker_ref": receipt.ArtifactRef, "blocker_sha256": receipt.ArtifactSHA256,
			"execution_assignment_id": receipt.Artifact.ExecutionAssignmentID,
			"execution_generation":    receipt.Artifact.ExecutionGeneration,
			"execution_sha256":        packet.Binding.ExecutionSHA256,
		},
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.root, ".claude/submissions", "generic-s9-blocker-resolution.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
