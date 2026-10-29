package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// This uses the public save-Hook boundary with mechanical decision facts;
// registered path and original blocker facts own acceptance, not Hook identity.
func TestS9MainResolutionPublicHookRetiresOldAttempt(t *testing.T) {
	fixture, owner, packet, execution, workerRoot, _, _ := prepareWorkingS9RepairResultPacket(t)
	blocked := encodeS9NonpassRepairResultAnswer(t, packet, "blocked")
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath)), blocked, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, _ := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, packet.AnswerPath)
	if !accepted.Accepted || accepted.WorkComplete {
		t.Fatalf("original blocker not accepted: %+v", accepted)
	}
	path, err := repair.RepairResultResolutionAuthoringPath(packet)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(fixture.root, filepath.FromSlash(path))
	draftBytes := mustRead(t, abs)
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	resolution := draft["s9_resolution"].(map[string]any)
	if resolution["rationale"] != "" {
		t.Fatalf("Main must author the recovery rationale, got %v", resolution["rationale"])
	}
	resolution["rationale"] = "External dependency restored; preserve the old report and continue the repair."
	// Main fills only the recovery judgment; body is supplied by the system.
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	payload := s9RepairResultHookPayload("", fixture.root, "main-resolution-platform-session", abs)
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	failedSave := cloneS9HookPayload(t, payload)
	failedSave["tool_response"] = map[string]any{"is_error": true}
	failedBody, err := json.Marshal(failedSave)
	if err != nil {
		t.Fatal(err)
	}
	var failedOut, failedErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "PostToolUse"}, bytes.NewReader(failedBody), &failedOut, &failedErr); code != 0 {
		t.Fatalf("failed save Hook exited %d: %s", code, failedErr.String())
	}
	if bytes.Contains(failedOut.Bytes(), []byte("S9 SUBMISSION RECEIPT:")) {
		t.Fatalf("failed file save was routed to the S9 resolution consumer: %s", failedOut.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeState, beforeJournal, "failed Main resolution save")
	// A terminal consume cannot manufacture a resolution by attaching Main or
	// Worker identity to an unissued path. The exact registered path remains
	// available through the original successful-save Hook below.
	unissuedPath := filepath.Join(fixture.root, ".claude/submissions/unregistered/S9-result/main-resolution.json")
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", fixture.root, "--path", unissuedPath}, nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), "no exact original issuance") {
		t.Fatalf("unissued Main resolution source was not rejected by exact registered-path lookup: code=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeState, beforeJournal, "unissued resolution consume")
	mainContext, err := hookctx.FromSnapshot(fixture.root, "", currentS9Snapshot(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	mainPre := policy.Input{Event: "PreToolUse", ToolName: "Edit", CWD: fixture.root,
		SessionID: "main-resolution-platform-session", ToolInput: map[string]any{"file_path": abs}, Runtime: mainContext.PolicyContext}
	if decision, denied := policy.EvaluateAgentScoped(mainPre); denied {
		t.Fatalf("exact Main resolution authoring denied: %+v", decision)
	}
	resolved, raw := runS9RepairResultHookPayloadWithRaw(t, fixture.root, payload)
	if !resolved.Accepted || resolved.WorkComplete || resolved.ResponsibilityStatus != "blocker_resolution_recorded" ||
		resolved.ConsumerOwner != "main" || !resolved.ConsumerPending {
		t.Fatalf("Main resolution was not separately received: %s", raw)
	}
	if !strings.Contains(resolved.NextAction, "runtime workspace prepare") || strings.Contains(resolved.NextAction, "监听器会准备") || strings.Contains(resolved.NextAction, "stage-answer issue") {
		t.Fatalf("Main recovery feedback must name normal prepare, without passive preparation or manual issuance: %q", resolved.NextAction)
	}
	if body, _ := draft["body"].(string); body != "" {
		t.Fatal("rationale-only fixture unexpectedly authored a duplicate body")
	}
	current := currentS9Snapshot(t, fixture)
	if _, found, err := runtime.VerifiedRepairResultResolutionReceipt(fixture.root, current.State, packet.ID, owner.agentID); err != nil || !found {
		t.Fatalf("original resolution CAS receipt missing: found=%t err=%v", found, err)
	}
	if current, err := runtime.CurrentRepairResultPackets(fixture.root, current.State, owner.agentID); err != nil || len(current) != 0 {
		t.Fatalf("resolved old packet remained current: packets=%v err=%v", current, err)
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, current.State, packet.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("historical blocker revived Stop after resolve: mayEnd=%t err=%v", mayEnd, err)
	}
	workerContext, err := hookctx.FromSnapshot(fixture.root, owner.agentID, current)
	if err != nil || workerContext == nil || workerContext.PolicyContext.Agent == nil {
		t.Fatalf("resolved owner context unavailable: %v", err)
	}
	agent := workerContext.PolicyContext.Agent
	if !agent.RepairResultResumePending || len(workerContext.PolicyContext.RepairResultAnswerPaths) != 0 {
		t.Fatalf("resolved responsibility lost its pending reminder or exposed a retired questionnaire: agent=%+v answers=%v", agent, workerContext.PolicyContext.RepairResultAnswerPaths)
	}
	var active *hookctx.AssignmentContext
	for i := range workerContext.Assignments {
		if workerContext.Assignments[i].OwnerAgentID == owner.agentID && workerContext.Assignments[i].AssignmentID == agent.AssignmentID {
			active = &workerContext.Assignments[i]
		}
	}
	if facts := hook.CurrentResponsibility(fixture.root, current, agent, active, false); facts.ExecutionMayEnd || facts.DeliveryRecorded || facts.NextAction != "await_consumer" {
		t.Fatalf("resolved Worker was told to reuse/finish the old attempt: %+v", facts)
	}
	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replayPayload := cloneS9HookPayload(t, payload)
	delete(replayPayload, "agent_id")
	delete(replayPayload, "teammate_name")
	delete(replayPayload, "cwd")
	delete(replayPayload, "session_id")
	_, replay := runS9RepairResultHookPayloadWithRaw(t, fixture.root, replayPayload)
	if !bytes.Equal(raw, replay) {
		t.Fatalf("exact Main resolution replay changed receipt:\n%s\n%s", raw, replay)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact Main resolution replay")
	resolution["rationale"] = "Different recovery decision must not replace the original message."
	changed, _ := json.Marshal(draft)
	if err := os.WriteFile(abs, changed, 0600); err != nil {
		t.Fatal(err)
	}
	r, _ := runS9RepairResultHookPayloadWithRaw(t, fixture.root, payload)
	if r.Accepted {
		t.Fatal("changed Main decision replay was accepted")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "changed Main resolution replay")
}
