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
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// This exercises the public S9 PostToolUse receiver and Stop projection for a
// complete typed non-pass answer. The authored answer and mismatched Hook
// metadata are mechanical fixture facts; this is not a model or live-host acceptance.
func TestS9RepairResultBlockerPublicHookReceiptAndStop(t *testing.T) {
	fixture, owner, packet, execution, workerRoot, answerBytes, issued := prepareS9RepairResultPacketWithPlanActivity(t, false)
	projection, err := workstate.AgentProjection(agentLifecycleRow(issued.State, owner.agentID), fmt.Sprint(issued.State["schema_version"]))
	if err != nil || projection.Status != workstate.Active || projection.Activity == "working" {
		t.Fatalf("normal S9 blocker author must be active without generic Begin: projection=%+v err=%v", projection, err)
	}
	t.Log("normal Plan save and workspace prepare supplied Result; active author has no generic working prerequisite")

	// All original non-pass verdicts remain complete professional answers. The
	// receiver below accepts only the blocked answer without rewriting its verdict.
	for _, verdict := range []string{"blocked", "contract_conflict", "stale"} {
		answer := encodeS9NonpassRepairResultAnswer(t, packet, verdict)
		if _, problems := workpacket.ValidateRepairResultAnswer(packet, answer); len(problems) != 0 {
			t.Fatalf("complete %s S9 answer rejected by the closed packet schema: %+v", verdict, problems)
		}
		var draft workpacket.RepairResultDraft
		if err := json.Unmarshal(answer, &draft); err != nil {
			t.Fatalf("decode %s S9 answer draft: %v", verdict, err)
		}
		var professional workpacket.RepairResultProfessionalAnswer
		if err := json.Unmarshal(draft.Answer, &professional); err != nil || professional.Verdict != verdict {
			t.Fatalf("S9 receiver input changed %s professional verdict to %q: err=%v", verdict, professional.Verdict, err)
		}
	}

	beforeAccepted := currentS9Snapshot(t, fixture)
	metadataMismatch := s9RepairResultHookPayload("unrelated-hook-agent", fixture.root, "unrelated-session", filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath)))
	accepted, acceptedJSON := runS9RepairResultHookPayloadWithRaw(t, fixture.root, metadataMismatch)
	var acceptedWire map[string]any
	if err := json.Unmarshal(acceptedJSON, &acceptedWire); err != nil {
		t.Fatalf("decode complete public S9 blocker receipt: %v", err)
	}
	wantKey := fmt.Sprintf("runtime:agent:%s:work_blocked:%d", owner.agentID, issued.Revision)
	if acceptedWire["packet_id"] != packet.ID || acceptedWire["accepted"] != true || acceptedWire["responsibility_status"] != "blocker_recorded" ||
		acceptedWire["work_complete"] != false || acceptedWire["status"] != "blocked" ||
		acceptedWire["consumer_owner"] != "main" || acceptedWire["consumer_pending"] != true ||
		accepted.SubmissionKey != wantKey || !accepted.Accepted {
		t.Fatalf("exact S9 blocker path was rejected by Hook metadata or lost domain facts: %s", acceptedJSON)
	}

	acceptedSnapshot := currentS9Snapshot(t, fixture)
	if acceptedSnapshot.Revision <= beforeAccepted.Revision {
		t.Fatalf("exact S9 blocker save did not commit its original lifecycle event: %d -> %d", beforeAccepted.Revision, acceptedSnapshot.Revision)
	}
	blocker, found, err := runtime.VerifiedRepairResultBlockerReceipt(fixture.root, acceptedSnapshot.State, packet.ID, owner.agentID)
	if err != nil || !found || blocker.AcceptedIdempotencyKey != wantKey || blocker.Artifact.PacketID != packet.ID {
		t.Fatalf("public S9 Hook did not commit the original blocker lifecycle receipt: found=%t receipt=%+v err=%v", found, blocker, err)
	}
	if _, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, acceptedSnapshot.State, packet.ID, owner.agentID); err != nil || found {
		t.Fatalf("non-pass S9 blocker created a candidate receipt: found=%t err=%v", found, err)
	}
	if _, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, acceptedSnapshot.State, packet.ID, owner.agentID); err != nil || found {
		t.Fatalf("non-pass S9 blocker created a final Result receipt: found=%t err=%v", found, err)
	}

	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	resolutionRef, err := workpacket.RepairResultResolutionAuthoringPath(packet)
	if err != nil {
		t.Fatal(err)
	}
	resolutionPath := filepath.Join(fixture.root, filepath.FromSlash(resolutionRef))
	if !strings.Contains(accepted.NextAction, resolutionPath) {
		t.Fatalf("accepted blocker omitted exact Main resolution: %+v", accepted)
	}
	// Re-entering through the public read-only receipt retains the same
	// path without republishing a missing draft or accepting anything again.
	if err := os.Remove(resolutionPath); err != nil {
		t.Fatal(err)
	}
	reader, err := submission.NewReadOnly(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	currentReceipt, err := reader.RepairResultReceipt(owner.agentID, packet.ID)
	if err != nil || !strings.Contains(currentReceipt.NextAction, resolutionPath) || len(currentReceipt.WaitingOn) != 1 || currentReceipt.WaitingOn[0].Ref != resolutionPath {
		t.Fatalf("receipt re-entry lost Main resolution: %+v err=%v", currentReceipt, err)
	}
	if _, err := os.Stat(resolutionPath); !os.IsNotExist(err) {
		t.Fatalf("receipt projection published a draft: %v", err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "blocker receipt pure path projection")
	_, replayedJSON := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, packet.AnswerPath)
	if !bytes.Equal(acceptedJSON, replayedJSON) {
		t.Fatalf("exact public S9 blocker replay changed its original receipt:\nfirst: %s\nreplay: %s", acceptedJSON, replayedJSON)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact S9 blocker Hook replay")

	// A schema-valid changed verdict cannot replay the accepted packet, and its
	// changed bytes also invalidate the Stop proof until the original bytes return.
	changedAnswer := encodeS9NonpassRepairResultAnswer(t, packet, "stale")
	if _, problems := workpacket.ValidateRepairResultAnswer(packet, changedAnswer); len(problems) != 0 {
		t.Fatalf("changed-answer negative must itself be schema-valid: %+v", problems)
	}
	answerPath := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(answerPath, append(changedAnswer, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, changedJSON := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, packet.AnswerPath)
	var changedWire map[string]any
	if err := json.Unmarshal(changedJSON, &changedWire); err != nil {
		t.Fatalf("decode changed-answer raw Hook receipt: %v", err)
	}
	if changedWire["accepted"] != false || changedWire["responsibility_status"] != "not_accepted" {
		t.Fatalf("changed S9 blocker answer replay was not refused: %s", changedJSON)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "changed S9 blocker answer replay")
	historical, err := reader.RepairResultReceipt(owner.agentID, packet.ID)
	if err != nil || !historical.Accepted || historical.WorkComplete || strings.Contains(historical.NextAction, "本次执行可结束") {
		t.Fatalf("historical blocker receipt claimed current completion: %+v err=%v", historical, err)
	}
	if decision, handled := hook.StopIdleDecision(fixture.root, policy.Input{
		Event: "SubagentStop", AgentID: owner.agentID, CWD: workerRoot, SessionID: execution.PlatformSessionID,
	}); !handled || decision.Decision != "deny" {
		t.Fatalf("changed source still authorized S9 Stop: handled=%t decision=%+v", handled, decision)
	}

	if err := os.WriteFile(answerPath, answerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runS9HookEvent(t, fixture.root, "SubagentStop", owner.agentID, workerRoot, execution.PlatformSessionID); code != 0 {
		t.Fatalf("exact current blocked S9 receipt did not allow the owner's Stop: code=%d", code)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "S9 Stop projection")
}

func runS9RepairResultPostToolUseWithRaw(t *testing.T, root, owner, workerRoot, session, answerRel string) (submission.Receipt, []byte) {
	t.Helper()
	payload := s9RepairResultHookPayload(owner, workerRoot, session, filepath.Join(workerRoot, filepath.FromSlash(answerRel)))
	return runS9RepairResultHookPayloadWithRaw(t, root, payload)
}

func runS9RepairResultHookPayloadWithRaw(t *testing.T, root string, payload map[string]any) (submission.Receipt, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("S9 RepairResult PostToolUse Hook exited %d: stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	return parseS9RepairResultHookReceipt(t, out.Bytes())
}

func parseS9RepairResultHookReceipt(t *testing.T, out []byte) (submission.Receipt, []byte) {
	t.Helper()
	var envelope struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("decode outer S9 Hook envelope: %v: %s", err, out)
	}
	const prefix = "S9 SUBMISSION RECEIPT: "
	if !strings.HasPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix) {
		t.Fatalf("S9 Hook did not return a domain receipt: %s", out)
	}
	rawReceipt := []byte(strings.TrimPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix))
	assertSharedSubmissionReceiptWire(t, rawReceipt)
	var receipt submission.Receipt
	if err := json.Unmarshal(rawReceipt, &receipt); err != nil {
		t.Fatalf("decode S9 Hook receipt projection: %v", err)
	}
	return receipt, rawReceipt
}

func cloneS9HookPayload(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
