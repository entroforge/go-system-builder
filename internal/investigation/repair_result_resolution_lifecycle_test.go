package investigation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestS9ResolutionUsesOriginalAgentLifecycleCASAndExactReplay(t *testing.T) {
	fixture, owner, packet, blockerCapability, blockerReceipt, blocked, lockedCtx, unlock := prepareAcceptedBlockerForResolution(t, "blocked")
	defer unlock()
	path, draft, err := repair.RepairResultResolutionDraft(fixture.root, blocked.State, packet.ID, owner.agentID)
	if err != nil {
		t.Fatalf("build Main resolution draft: %v", err)
	}
	expectedPath, err := repair.RepairResultResolutionAuthoringPath(packet)
	if err != nil || path != expectedPath {
		t.Fatalf("resolution path=%q, expected exact packet-derived path %q (err=%v)", path, expectedPath, err)
	}
	var authored map[string]any
	if err := json.Unmarshal(draft, &authored); err != nil {
		t.Fatal(err)
	}
	resolution := authored["s9_resolution"].(map[string]any)
	if resolution["rationale"] != "" || authored["message_type"] != "blocker_resolution" || authored["s9_blocker"] != nil {
		t.Fatalf("draft is not an incomplete Main resolution envelope: %#v", authored)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(fixture.root, filepath.FromSlash(path))), 0o700); err != nil {
		t.Fatal(err)
	}
	authoringFile := filepath.Join(fixture.root, filepath.FromSlash(path))
	if err := os.WriteFile(authoringFile, draft, 0o600); err != nil {
		t.Fatal(err)
	}
	mainVerifier := func(root string, _ map[string]any) error {
		if root != fixture.root {
			return fmt.Errorf("Main verifier received root %q, want %q", root, fixture.root)
		}
		return nil
	}
	prepare := repair.PrepareIssuedRepairResultResolutionRequest{
		ExpectedRevision: blocked.Revision, PacketID: packet.ID, OwnerAgentID: owner.agentID,
		AuthoringPath: path, AuthorizeMain: mainVerifier,
	}
	beforePrepareState := mustRead(t, fixture.statePath)
	beforePrepareJournal := mustRead(t, fixture.journalPath)
	if _, _, err := repair.PrepareIssuedRepairResultResolution(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, prepare); err == nil {
		t.Fatal("schema-invalid empty-rationale Main draft became a resolution capability")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforePrepareState, beforePrepareJournal, "incomplete S9 Main resolution draft")

	authored["body"] = "I reviewed the blocker and authorize a bounded retry within the accepted contract."
	resolution["rationale"] = "The blocker is limited to a missing fixture; the accepted contract and execution tuple remain unchanged."
	resolution["evidence_refs"] = []any{"test://s9-resolution/approved-boundary"}
	authoringBytes, err := json.MarshalIndent(authored, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	authoringBytes = append(authoringBytes, '\n')
	if err := os.WriteFile(authoringFile, authoringBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	resolutionCapability, previous, err := repair.PrepareIssuedRepairResultResolution(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, prepare)
	if err != nil || resolutionCapability == nil || previous != nil {
		t.Fatalf("prepare current S9 Main resolution: capability=%t receipt=%+v err=%v", resolutionCapability != nil, previous, err)
	}
	if resolutionCapability.MessageRef() == "" || len(resolutionCapability.EvidenceIDs()) != 2 || resolutionCapability.EvidenceIDs()[0] != resolutionCapability.MessageRef() || resolutionCapability.EvidenceIDs()[1] != blockerReceipt.ArtifactRef {
		t.Fatalf("resolution capability does not bind message then exact predecessor blocker: message=%q evidence=%v", resolutionCapability.MessageRef(), resolutionCapability.EvidenceIDs())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforePrepareState, beforePrepareJournal, "S9 resolution Prepare before original lifecycle CAS")

	resolutionMessagePath := filepath.Join(fixture.root, filepath.FromSlash(resolutionCapability.MessageRef()))
	request := assignment.AgentEventRequest{
		ExpectedRevision: blocked.Revision, AgentID: owner.agentID, Event: "blocker_resolved",
		MessagePath: resolutionMessagePath, OccurredAt: time.Now().UTC(),
	}
	// Resolution capabilities are event-specific and mutually exclusive with
	// the Worker blocker capability.
	wrongEvent := request
	wrongEvent.Event = "work_blocked"
	wrongEvent.IssuedRepairResultResolution = resolutionCapability
	if _, err := advanceS9AgentWithContext(lockedCtx, fixture, wrongEvent); err == nil {
		t.Fatal("S9 resolution capability authorized work_blocked")
	}
	bothCapabilities := request
	bothCapabilities.IssuedRepairResultBlocker = blockerCapability
	bothCapabilities.IssuedRepairResultResolution = resolutionCapability
	if _, err := advanceS9AgentWithContext(lockedCtx, fixture, bothCapabilities); err == nil {
		t.Fatal("Agent lifecycle accepted blocker and resolution capabilities together")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforePrepareState, beforePrepareJournal, "wrong-event and conflicting S9 capabilities")

	// The prepared record captures Main's professional decision once; the
	// Writer associates that captured message with the still-blocked attempt.
	request.IssuedRepairResultResolution = resolutionCapability
	accepted, err := advanceS9AgentWithContext(lockedCtx, fixture, request)
	if err != nil {
		t.Fatalf("original Agent lifecycle CAS rejected S9 resolution: %v", err)
	}
	if accepted.Revision != blocked.Revision+1 {
		t.Fatalf("resolution revision=%d, want one original lifecycle CAS after %d", accepted.Revision, blocked.Revision)
	}
	resolvedAgent := agentLifecycleRow(accepted.State, owner.agentID)
	projection, projectionErr := workstate.AgentProjection(resolvedAgent, fmt.Sprint(accepted.State["schema_version"]))
	if projectionErr != nil || projection.Status != workstate.Active || projection.Activity != "working" ||
		(workstate.Unified(accepted.State["schema_version"]) && resolvedAgent["last_lifecycle_event"] != "blocker_resolved") ||
		resolvedAgent["work_blocked_ref"] != blockerCapability.MessageRef() || resolvedAgent["blocker_resolved_ref"] != resolutionCapability.MessageRef() {
		t.Fatalf("original CAS did not preserve blocker history and record exact resolution ref: %#v", resolvedAgent)
	}
	receipt, found, err := runtime.VerifiedRepairResultResolutionReceipt(fixture.root, accepted.State, packet.ID, owner.agentID)
	if err != nil || !found {
		t.Fatalf("indexed record reader did not return Main recovery decision: found=%t err=%v", found, err)
	}
	if receipt.BlockerRef != blockerReceipt.ArtifactRef || receipt.BlockerSHA256 != blockerReceipt.ArtifactSHA256 ||
		receipt.MessageRef != resolutionCapability.MessageRef() || receipt.MessageSHA256 == "" {
		t.Fatalf("Main recovery record lost its blocker/message association: %+v", receipt)
	}
	if current, err := runtime.CurrentRepairResultPackets(fixture.root, accepted.State, owner.agentID); err != nil || len(current) != 0 {
		t.Fatalf("resolved old packet remained current before a separately issued Resume packet: current=%v err=%v", current, err)
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, accepted.State, packet.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("resolved historical blocker retained Stop authority: mayEnd=%t err=%v", mayEnd, err)
	}
	if !hasS9ResolutionEvidenceRow(accepted.State, packet, blockerReceipt, receipt) {
		t.Fatalf("original lifecycle CAS omitted the exact Main resolution evidence row: %#v", accepted.State["evidence"])
	}

	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	request.ExpectedRevision = blocked.Revision
	replayedCapability, replayedReceipt, err := repair.PrepareIssuedRepairResultResolution(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, prepare)
	if err != nil || replayedCapability != nil || replayedReceipt == nil || replayedReceipt.AcceptedEventID != receipt.AcceptedEventID || replayedReceipt.MessageRef != receipt.MessageRef {
		t.Fatalf("exact old-revision Prepare replay did not return original receipt: capability=%t receipt=%+v err=%v", replayedCapability != nil, replayedReceipt, err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact S9 resolution Prepare replay")
	replayed, err := advanceS9AgentWithContext(lockedCtx, fixture, request)
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("exact old-capability lifecycle replay did not return the accepted revision: revision=%d err=%v", replayed.Revision, err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact S9 resolution lifecycle replay")

	changedAfterAcceptance := map[string]any{}
	if err := json.Unmarshal(authoringBytes, &changedAfterAcceptance); err != nil {
		t.Fatal(err)
	}
	changedAfterAcceptance["body"] = "Changed after acceptance."
	changedAcceptedBytes, err := json.MarshalIndent(changedAfterAcceptance, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	changedAcceptedBytes = append(changedAcceptedBytes, '\n')
	if err := os.WriteFile(authoringFile, changedAcceptedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.PrepareIssuedRepairResultResolution(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, prepare); err == nil {
		t.Fatal("accepted S9 resolution replay accepted changed Main authoring bytes")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "changed accepted S9 resolution replay")
}

func TestS9ResolutionKeepsContractConflictAtS8HumanGate(t *testing.T) {
	fixture, owner, packet, _, _, blocked, lockedCtx, unlock := prepareAcceptedBlockerForResolution(t, "contract_conflict")
	defer unlock()
	reader, err := submission.NewReadOnly(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := reader.RepairResultReceipt(owner.agentID, packet.ID)
	if err != nil || !strings.Contains(receipt.NextAction, "S8 新 Contract 审批人闸") || strings.Contains(receipt.NextAction, "main-resolution.json") {
		t.Fatalf("contract_conflict receipt lost S8 human gate or proposed retry: %+v err=%v", receipt, err)
	}
	_, _, err = repair.RepairResultResolutionDraft(fixture.root, blocked.State, packet.ID, owner.agentID)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "s8") {
		t.Fatalf("contract_conflict received an ordinary retry draft instead of the S8 human gate: %v", err)
	}
	authoringPath, err := repair.RepairResultResolutionAuthoringPath(packet)
	if err != nil {
		t.Fatal(err)
	}
	request := repair.PrepareIssuedRepairResultResolutionRequest{
		ExpectedRevision: blocked.Revision, PacketID: packet.ID, OwnerAgentID: owner.agentID,
		AuthoringPath: authoringPath,
		AuthorizeMain: func(root string, _ map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("wrong Main root %q", root)
			}
			return nil
		},
	}
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if _, _, err := repair.PrepareIssuedRepairResultResolution(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, request); err == nil || !strings.Contains(strings.ToLower(err.Error()), "s8") {
		t.Fatalf("contract_conflict Prepare did not name the original S8 human gate: %v", err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeState, beforeJournal, "contract_conflict S9 resolution refusal")
}

func prepareAcceptedBlockerForResolution(t *testing.T, verdict string) (*intakeFixture, s9HookOwner, workpacket.RepairResultPacket, *repair.IssuedRepairResultBlockerCapability, runtime.RepairResultBlockerReceipt, runtime.Snapshot, context.Context, func()) {
	t.Helper()
	fixture, owner, packet, execution, workerRoot, _, issued := prepareWorkingS9RepairResultPacket(t)
	answer := encodeS9NonpassRepairResultAnswer(t, packet, verdict)
	answerPath := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(answerPath, append(append([]byte(nil), answer...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	lockedCtx, unlock, err := integration.LockWorkspace(context.Background(), fixture.root)
	if err != nil {
		t.Fatalf("acquire S9 Main workspace lock: %v", err)
	}
	blockerCapability, _, err := repair.PrepareIssuedRepairResultBlocker(lockedCtx, fixture.root, fixture.statePath, fixture.journalPath, repair.AcceptIssuedRepairResultBlockerRequest{
		ExpectedRevision: issued.Revision, PacketID: packet.ID, AnswerPath: packet.AnswerPath,
		OwnerAgentID: owner.agentID, SourceRoot: workerRoot,
		AuthorizeSource: func(root string, state map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("blocker callback received Main root %q, want %q", root, fixture.root)
			}
			registry, err := workspace.Decode(state)
			if err != nil {
				return err
			}
			if registry == nil {
				return fmt.Errorf("Runtime has no Workspace registration")
			}
			registered, ok := registry.Execution(owner.manifestAssignmentID, packet.Binding.Sources.RuntimeID, packet.Binding.Sources.BaselineGeneration)
			if !ok || registered.Status != "ready" || registered.AgentID != owner.agentID || registered.Path != workerRoot || registered.PlatformSessionID != execution.PlatformSessionID {
				return fmt.Errorf("registered Worker execution differs from blocker source")
			}
			pointerRoot, marked, err := workspace.PointerRoot(workerRoot)
			if err != nil || !marked || pointerRoot != workerRoot {
				return fmt.Errorf("Worker source lacks its exact pointer: root=%q marked=%t err=%v", pointerRoot, marked, err)
			}
			return nil
		},
	})
	if err != nil || blockerCapability == nil {
		unlock()
		t.Fatalf("prepare original S9 blocker: capability=%t err=%v", blockerCapability != nil, err)
	}
	blocked, err := advanceS9AgentWithContext(lockedCtx, fixture, workBlockedWithCapability(assignment.AgentEventRequest{
		ExpectedRevision: issued.Revision, AgentID: owner.agentID, Event: "work_blocked",
		MessagePath: filepath.Join(fixture.root, filepath.FromSlash(blockerCapability.MessageRef())), OccurredAt: time.Now().UTC(),
	}, blockerCapability))
	if err != nil {
		unlock()
		t.Fatalf("accept original S9 blocker: %v", err)
	}
	blockerReceipt, found, err := runtime.VerifiedRepairResultBlockerReceipt(fixture.root, blocked.State, packet.ID, owner.agentID)
	if err != nil || !found {
		unlock()
		t.Fatalf("verify original accepted S9 blocker: found=%t err=%v", found, err)
	}
	return fixture, owner, packet, blockerCapability, blockerReceipt, blocked, lockedCtx, unlock
}

func hasS9ResolutionEvidenceRow(state map[string]any, packet workpacket.RepairResultPacket, blocker runtime.RepairResultBlockerReceipt, resolution runtime.RepairResultResolutionReceipt) bool {
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != resolution.MessageRef {
			continue
		}
		refs, _ := row["scope_refs"].([]any)
		producers, _ := row["produced_by"].([]any)
		return row["kind"] == "repair_result_resolution" && row["path"] == resolution.MessageRef &&
			row["sha256"] == resolution.MessageSHA256 && row["status"] == "valid" && row["responsibility_id"] == nil &&
			fmt.Sprint(row["baseline_generation"]) == fmt.Sprint(packet.Binding.Sources.BaselineGeneration) &&
			fmt.Sprint(row["review_round"]) == fmt.Sprint(packet.Binding.Sources.ReviewRound) &&
			len(producers) == 1 && producers[0] == "main" && len(refs) == 3 &&
			containsStringInAny(refs, packet.ID) && containsStringInAny(refs, packet.Binding.Sources.RepairAssignmentID) && containsStringInAny(refs, blocker.ArtifactRef)
	}
	return false
}

func containsStringInAny(values []any, wanted string) bool {
	for _, raw := range values {
		if value, ok := raw.(string); ok && value == wanted {
			return true
		}
	}
	return false
}
