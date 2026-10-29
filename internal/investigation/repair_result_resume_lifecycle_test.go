package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

func TestS9ResumedSecondBlockerClearsOldResolutionIndexInAgentCAS(t *testing.T) {
	fixture, owner, predecessor, execution, workerRoot, _, _ := prepareWorkingS9RepairResultPacket(t)
	successor := resolveS9RepairResultBlockerAndIssueSuccessor(t, fixture, owner, execution, workerRoot, predecessor)
	if successor.Binding.Resume == nil || successor.Binding.Resume.PredecessorPacketID != predecessor.ID {
		t.Fatalf("successor lacks its exact predecessor Resume tuple: %+v", successor.Binding.Resume)
	}

	beforeSecondBlock := currentS9Snapshot(t, fixture)
	agent := agentLifecycleRow(beforeSecondBlock.State, owner.agentID)
	if agent == nil || agent["blocker_resolved_ref"] != successor.Binding.Resume.ResolutionMessageRef {
		t.Fatalf("Main resolution pointer does not identify the Resume predecessor before the next blocker: %#v", agent)
	}
	answer := encodeS9NonpassRepairResultAnswer(t, successor, "blocked")
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(successor.AnswerPath)), answer, 0o600); err != nil {
		t.Fatal(err)
	}
	// Measure the actual platform process, including its independent watchdog.
	// The build and fixture setup are outside the Hook's wall-clock interval.
	binary := factory.BuildHarness(t)
	payload, err := json.Marshal(s9RepairResultHookPayload(owner.agentID, workerRoot, execution.PlatformSessionID, filepath.Join(workerRoot, filepath.FromSlash(successor.AnswerPath))))
	if err != nil {
		t.Fatal(err)
	}
	hookStarted := time.Now()
	code, stdoutText, stderrText := factory.RunHook(t, binary, fixture.root, "PostToolUse", payload)
	hookElapsed := time.Since(hookStarted)
	t.Logf("resumed blocker public Hook elapsed=%s", hookElapsed)
	if hookElapsed > 10*time.Second {
		t.Fatalf("resumed blocker exceeded the original platform Hook timeout: %s", hookElapsed)
	}
	if code != 0 {
		t.Fatalf("resumed blocker Hook exit=%d stdout=%s stderr=%s", code, stdoutText, stderrText)
	}
	accepted, _ := parseS9RepairResultHookReceipt(t, []byte(stdoutText))
	if !accepted.Accepted || accepted.WorkComplete || accepted.PacketID != successor.ID {
		t.Fatalf("resumed successor's second blocker was not accepted through its original receiver: %+v", accepted)
	}

	blocked := currentS9Snapshot(t, fixture)
	blockedAgent := agentLifecycleRow(blocked.State, owner.agentID)
	if blockedAgent == nil || blockedAgent["state"] != "blocked" || blockedAgent["blocker_resolved_ref"] != nil && blockedAgent["blocker_resolved_ref"] != "" {
		t.Fatalf("second blocker did not atomically retire the stale resolution index: %#v", blockedAgent)
	}
	if blockedAgent["work_blocked_ref"] == nil || blockedAgent["work_blocked_ref"] == "" {
		t.Fatalf("second blocker did not retain its new Agent lifecycle pointer: %#v", blockedAgent)
	}
	if _, found, err := runtime.VerifiedRepairResultBlockerReceipt(fixture.root, blocked.State, successor.ID, owner.agentID); err != nil || !found {
		t.Fatalf("second blocker lacks its current typed receipt: found=%t err=%v", found, err)
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, blocked.State, successor.ID, owner.agentID); err != nil || !mayEnd {
		t.Fatalf("exact resumed blocker did not retain execution-ending authority: mayEnd=%t err=%v", mayEnd, err)
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, blocked.State, predecessor.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("resolved predecessor regained execution-ending authority: mayEnd=%t err=%v", mayEnd, err)
	}
	current, err := runtime.CurrentRepairResultPackets(fixture.root, blocked.State, owner.agentID)
	if err != nil || len(current) != 1 || current[0].ID != successor.ID {
		t.Fatalf("second blocker changed the unique current packet: packets=%v err=%v", current, err)
	}
}

func TestS9ResumedSuccessorCompletesThroughOriginalCandidateWithResumeProvenance(t *testing.T) {
	accepted := prepareAcceptedS9RepairResultCandidateWithResume(t, true)
	if accepted.packet.Binding.Resume == nil {
		t.Fatal("completed candidate fixture has no Resume predecessor tuple")
	}
	if !reflect.DeepEqual(accepted.candidateReceipt.Provenance.Resume, accepted.packet.Binding.Resume) {
		t.Fatalf("accepted candidate provenance lost its packet Resume tuple: candidate=%+v packet=%+v", accepted.candidateReceipt.Provenance.Resume, accepted.packet.Binding.Resume)
	}
	wantResumeSHA, err := workpacket.RepairResultResumeSHA256(accepted.packet.Binding.Resume)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.candidateReceipt.Provenance.ResumeSHA256 != wantResumeSHA {
		t.Fatalf("candidate Resume digest=%q, want canonical %q", accepted.candidateReceipt.Provenance.ResumeSHA256, wantResumeSHA)
	}
	if accepted.candidateReceipt.Provenance.AnswerSHA256 == "" || accepted.candidateReceipt.Provenance.SourceHead == "" {
		t.Fatal("resumed candidate lacks its professional answer and actual product commit")
	}
	if err := runtime.VerifyRepairResultPacketCurrent(accepted.fixture.root, accepted.acceptedSnapshot.State, accepted.packet); err != nil {
		t.Fatalf("completed resumed packet is not the current accepted attempt: %v", err)
	}
	predecessor, found, err := runtime.FindRepairResultPacketContext(runtime.WithPacketBodyRoot(t.Context(), accepted.fixture.root), accepted.acceptedSnapshot.State, accepted.packet.Binding.Resume.PredecessorPacketID)
	if err != nil || !found {
		t.Fatalf("accepted Resume predecessor is unavailable for history check: found=%t err=%v", found, err)
	}
	if err := runtime.VerifyRepairResultPacketCurrent(accepted.fixture.root, accepted.acceptedSnapshot.State, predecessor); err == nil {
		t.Fatal("resolved predecessor answer became current after successor completion")
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(accepted.fixture.root, accepted.acceptedSnapshot.State, predecessor.ID, accepted.owner.agentID); err != nil || mayEnd {
		t.Fatalf("old blocker Stop authority revived after successor completion: mayEnd=%t err=%v", mayEnd, err)
	}

	var integratedOut, integratedErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "workspace", "integrate", "--root", accepted.fixture.root, "--assignment", accepted.owner.manifestAssignmentID, "--agent", accepted.owner.agentID, "--check-command", "test -f internal/api/payload.go"}, nil, &integratedOut, &integratedErr); code != 0 {
		t.Fatalf("original Main integration rejected the resumed candidate: code=%d stdout=%s stderr=%s", code, integratedOut.String(), integratedErr.String())
	}
	final := currentS9Snapshot(t, accepted.fixture)
	if _, found, err := runtime.VerifiedRepairResultReceipt(accepted.fixture.root, final.State, accepted.packet.ID, accepted.owner.agentID); err != nil || !found {
		t.Fatalf("final Main receipt is not bound to the resumed successor: found=%t err=%v", found, err)
	}
	if _, found, err := runtime.VerifiedRepairResultReceipt(accepted.fixture.root, final.State, predecessor.ID, accepted.owner.agentID); err != nil || found {
		t.Fatalf("resolved predecessor received a final Result through successor integration: found=%t err=%v", found, err)
	}
}

func resolveS9RepairResultBlockerAndIssueSuccessor(
	t *testing.T,
	fixture *intakeFixture,
	owner s9HookOwner,
	execution workspace.Execution,
	workerRoot string,
	predecessor workpacket.RepairResultPacket,
) workpacket.RepairResultPacket {
	t.Helper()
	blockedAnswer := encodeS9NonpassRepairResultAnswer(t, predecessor, "blocked")
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(predecessor.AnswerPath)), blockedAnswer, 0o600); err != nil {
		t.Fatal(err)
	}
	blocked, _ := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, predecessor.AnswerPath)
	if !blocked.Accepted || blocked.WorkComplete || blocked.PacketID != predecessor.ID {
		t.Fatalf("original packet blocker was not accepted before Resume: %+v", blocked)
	}
	resolutionPath, err := repair.RepairResultResolutionAuthoringPath(predecessor)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(fixture.root, filepath.FromSlash(resolutionPath))
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, abs), &draft); err != nil {
		t.Fatal(err)
	}
	resolution, ok := draft["s9_resolution"].(map[string]any)
	if !ok {
		t.Fatalf("Main resolution draft has no typed s9_resolution object: %#v", draft)
	}
	resolution["rationale"] = "The dependency is available again; retry under the original frozen repair authority."
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, _ := runS9RepairResultHookPayloadWithRaw(t, fixture.root, s9RepairResultHookPayload("", fixture.root, "main-resume-lifecycle-session", abs))
	if !resolved.Accepted || resolved.WorkComplete || resolved.ResponsibilityStatus != "blocker_resolution_recorded" {
		t.Fatalf("original Main resolution was not accepted before Resume: %+v", resolved)
	}
	resolvedSnapshot := currentS9Snapshot(t, fixture)
	if _, err := runtime.CurrentRepairResultCompletionReport(fixture.root, resolvedSnapshot.State, predecessor); err == nil {
		t.Fatal("resolved predecessor reused completion evidence before the resumed successor produced a new completion report")
	}

	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &out, &errout); code != 0 {
		t.Fatalf("original issuer did not issue the verified Resume successor: code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
	var packets []workpacket.RepairResultPacket
	if err := json.Unmarshal(out.Bytes(), &packets); err != nil || len(packets) != 1 {
		t.Fatalf("decode unique resumed S9 successor: packets=%d err=%v stdout=%s", len(packets), err, out.String())
	}
	successor := packets[0]
	current, err := runtime.CurrentRepairResultPackets(fixture.root, currentS9Snapshot(t, fixture).State, owner.agentID)
	if err != nil || len(current) != 1 || current[0].ID != successor.ID {
		t.Fatalf("issuer did not install a unique current successor: packets=%v err=%v", current, err)
	}
	if _, err := runtime.CurrentRepairResultCompletionReport(fixture.root, currentS9Snapshot(t, fixture).State, successor); err == nil {
		t.Fatal("resumed successor inherited a completion report before its own original completion event")
	}
	return successor
}
