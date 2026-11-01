package review

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRegisteredResultPacketSourceRootBindsCurrentExecution(t *testing.T) {
	mainRoot := t.TempDir()
	state := packetModeVerificationState()
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "agent-qa-1" {
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	initResultExecutionGitRoot(t, mainRoot)
	boundReq := state["bound_req"].(map[string]any)
	boundReq["workspace"] = map[string]any{
		"project_root": mainRoot, "dev_branch": "main", "release_upstream": "main",
	}
	statePath, journalPath := writeState(t, mainRoot, state)
	fixtureEvidenceRoot = mainRoot
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	planPath := writePlanFile(t, mainRoot)
	resultExecutionGit(t, mainRoot, "add", "internal/example/service.go", "plan.json")
	resultExecutionGit(t, mainRoot, "commit", "-m", "Pin S7 review plan source fixture")
	snapshot, err := RegisterPlan(mainRoot, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("register real Git S7 plan fixture: %v", err)
	}
	snapshot = markDispatched(t, mainRoot, statePath, journalPath, snapshot, "assignment-qa-1", "agent-qa-1")
	registry, err := workspace.New(context.Background(), mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	e, err := registry.Plan(context.Background(), snapshot.State, "assignment-qa-1", "agent-qa-1", []string{"e2e-workspace"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	registry.Executions[e.AssignmentID] = e
	state = snapshot.State
	state["workspace"] = workspace.Encode(registry)
	writer := loopruntime.NewWriter(statePath, journalPath, mainRoot, semantic.RuntimeCandidateValidator{})
	snapshot, err = writer.UpdateCurrent(loopruntime.Mutation{
		EventID: "evt-test-s7-worker-registration", TransitionID: "WORKSPACE", Event: "workspace_updated",
		Actor: "main", RuntimeID: "loop-REQ-TEST", BaselineGeneration: 1,
		IdempotencyKey: "test:s7-worker-registration", RetainLastTransition: true, OccurredAt: time.Now().UTC(),
		Apply: func(current map[string]any) error {
			current["workspace"] = workspace.Encode(registry)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("persist original S7 Worker execution: %v", err)
	}
	state = snapshot.State
	workerRoot := e.Path
	packet := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{
		RuntimeID: "loop-REQ-TEST", BaselineGeneration: 1,
		AssignmentID: "assignment-qa-1", OwnerAgentID: "agent-qa-1",
	}}
	mainOnly, independent, err := registeredResultPacketSourceRoot(mainRoot, packetModeVerificationState(), packet)
	if err != nil || independent || mainOnly != mainRoot {
		t.Fatalf("native/Main source without a declared execution = %q, independent=%t, err=%v", mainOnly, independent, err)
	}

	got, independent, err := registeredResultPacketSourceRoot(mainRoot, state, packet)
	if err != nil || !independent || got != workerRoot {
		t.Fatalf("registered execution root = %q, independent=%t, err=%v", got, independent, err)
	}
	plan, ptr, err := LoadPlan(mainRoot, state)
	if err != nil {
		t.Fatal(err)
	}
	assignment := findPlanAssignment(plan, packet.Binding.AssignmentID)
	packet, err = buildCurrentResultPacket(mainRoot, state, plan, ptr, assignment, packet.Binding.OwnerAgentID)
	if err != nil {
		t.Fatal(err)
	}
	result := &Result{
		ResultID: packet.ResultID, AssignmentID: packet.Binding.AssignmentID,
		AssignmentRevision: packet.Binding.ReviewPlanRevision, ReviewPlanID: packet.Binding.ReviewPlanID,
		BaselineGeneration: packet.Binding.BaselineGeneration,
		ProducerAgentID:    packet.Binding.OwnerAgentID, Verdict: "pass",
	}
	receipt, err := newResultReceipt(mainRoot, state, plan, ptr, assignment, result, ".claude/evidence/result.json", &packet, workerRoot)
	if err != nil || receipt.ExecutionRoot != workerRoot {
		t.Fatalf("durable receipt did not bind actual Worker root: root=%q err=%v", receipt.ExecutionRoot, err)
	}

	seedResultMethodSources(t, mainRoot, snapshot.State, plan)
	writeMethodState(t, statePath, snapshot.State)
	issuedPackets, err := IssueResultPackets(mainRoot, statePath, journalPath, snapshot.Revision, packet.Binding.OwnerAgentID)
	if err != nil || len(issuedPackets) != 1 {
		t.Fatalf("issue original S7 answer for registered Worker: packets=%d err=%v", len(issuedPackets), err)
	}
	packet = issuedPackets[0]
	resultPath := writeResultFile(t, workerRoot, plan, packet.Binding.AssignmentID, packet.ResultID, packet.Binding.OwnerAgentID, "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	answer := s7ProfessionalTestAnswer(t, original)
	packet.Draft.Answer, _ = json.Marshal(answer)
	draft, err := json.Marshal(packet.Draft)
	if err != nil {
		t.Fatal(err)
	}
	workerAnswer := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(workerAnswer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workerAnswer, append(draft, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	issuedSnapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	request := IssuedSubmitRequest{ExpectedRevision: issuedSnapshot.Revision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID}
	stateBeforeRejects, journalBeforeRejects := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	wrongOwner := request
	wrongOwner.OwnerAgentID = "agent-dv-1"
	if _, err := SubmitIssuedResult(mainRoot, statePath, journalPath, wrongOwner); err == nil {
		t.Fatal("S7 packet accepted a different original Assignment owner")
	}
	wrongSource := request
	wrongSource.SourceRoot = mainRoot
	if _, err := SubmitIssuedResult(mainRoot, statePath, journalPath, wrongSource); err == nil {
		t.Fatal("S7 packet accepted Main as a substitute for its registered Worker source")
	}
	if !bytes.Equal(stateBeforeRejects, mustReadTestFile(t, statePath)) || !bytes.Equal(journalBeforeRejects, mustReadTestFile(t, journalPath)) {
		t.Fatal("wrong S7 owner/source tuple changed the original Runtime state or journal")
	}
	// Source registration does not prove that a new original answer exists.
	unavailableWorker := workerRoot + ".temporarily-unavailable"
	if err := os.Rename(workerRoot, unavailableWorker); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitIssuedResult(mainRoot, statePath, journalPath, request); err == nil {
		t.Fatal("new acceptance used a missing Worker checkout")
	}
	if !bytes.Equal(stateBeforeRejects, mustReadTestFile(t, statePath)) || !bytes.Equal(journalBeforeRejects, mustReadTestFile(t, journalPath)) {
		t.Fatal("missing original answer changed Runtime or journal")
	}
	if err := os.Rename(unavailableWorker, workerRoot); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(mainRoot, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("original S7 consumer required Hook source authorization for a registered Worker answer: %v", err)
	}
	acceptedReceipt, found, err := IssuedResultReceipt(mainRoot, accepted.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found || acceptedReceipt.ExecutionRoot != workerRoot {
		t.Fatalf("original S7 receipt lost registered Worker source: found=%v root=%q want=%q err=%v", found, acceptedReceipt.ExecutionRoot, workerRoot, err)
	}
	state = accepted.State
	acceptedState, acceptedJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	replayed, err := SubmitIssuedResult(mainRoot, statePath, journalPath, request)
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("exact S7 answer replay failed: revision=%d want=%d err=%v", replayed.Revision, accepted.Revision, err)
	}
	if !bytes.Equal(acceptedState, mustReadTestFile(t, statePath)) || !bytes.Equal(acceptedJournal, mustReadTestFile(t, journalPath)) {
		t.Fatal("exact S7 answer replay wrote another Runtime result or journal event")
	}

	// Receipt replay proves the immutable registered path tuple without
	// requiring that the mutable Worker checkout still exists.
	if err := os.RemoveAll(workerRoot); err != nil {
		t.Fatal(err)
	}
	got, independent, err = registeredResultPacketSourceRoot(mainRoot, state, packet)
	if err != nil || !independent || got != workerRoot {
		t.Fatalf("replay depended on live Worker checkout: root=%q independent=%t err=%v", got, independent, err)
	}
	if replayed, err := SubmitIssuedResult(mainRoot, statePath, journalPath, request); err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("accepted replay depended on missing Worker checkout: %v", err)
	}
	if !bytes.Equal(acceptedState, mustReadTestFile(t, statePath)) || !bytes.Equal(acceptedJournal, mustReadTestFile(t, journalPath)) {
		t.Fatal("missing-checkout replay changed Runtime or journal")
	}

	// A declared but mismatched current execution cannot silently fall back to
	// Main, and registering Main itself does not resurrect the shared entry.
	registry.Executions[e.AssignmentID] = func() workspace.Execution {
		bad := e
		bad.RuntimeID = "loop-other"
		return bad
	}()
	state["workspace"] = workspace.Encode(registry)
	if got, _, err := registeredResultPacketSourceRoot(mainRoot, state, packet); err == nil || got != "" {
		t.Fatalf("mismatched declared execution fell back to %q: %v", got, err)
	}
	registry.Executions[e.AssignmentID] = func() workspace.Execution {
		main := e
		main.Path = mainRoot
		return main
	}()
	state["workspace"] = workspace.Encode(registry)
	if got, _, err := registeredResultPacketSourceRoot(mainRoot, state, packet); err == nil || got != "" {
		t.Fatalf("declared Main execution remained a valid Worker source %q: %v", got, err)
	}
}

func initResultExecutionGitRoot(t *testing.T, root string) {
	t.Helper()
	resultExecutionGit(t, root, "init", "--initial-branch=main")
	resultExecutionGit(t, root, "config", "user.email", "s7-source-test@example.invalid")
	resultExecutionGit(t, root, "config", "user.name", "S7 Source Test")
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("S7 Worker source fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultExecutionGit(t, root, "add", "docs/control/loop-definition.json", "README.md")
	resultExecutionGit(t, root, "commit", "-m", "Initialize S7 source fixture")
}

func resultExecutionGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestPacketReceiptEventIdentityBindsOriginalPacket(t *testing.T) {
	ptr := &PlanPointer{PlanID: "plan-1", Path: "plan.json", Revision: 2}
	result := &Result{
		ResultID: "result-1", AssignmentID: "assignment-1", AssignmentRevision: 2,
		ReviewPlanID: "plan-1", BaselineGeneration: 4,
		ProducerAgentID: "agent-1",
	}
	firstEvent, firstKey := receiptEventIdentity("runtime-1", 4, ptr, result, "packet-1")
	secondEvent, secondKey := receiptEventIdentity("runtime-1", 4, ptr, result, "packet-2")
	if firstEvent == secondEvent || firstKey == secondKey {
		t.Fatal("packet receipt identity did not bind the original packet")
	}
}
