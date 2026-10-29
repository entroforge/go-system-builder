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
	mainHead := initResultExecutionGitRoot(t, mainRoot)
	boundReq := state["bound_req"].(map[string]any)
	boundReq["workspace"] = map[string]any{
		"project_root": mainRoot, "dev_branch": "main", "release_upstream": "main", "bound_commit": mainHead,
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
	mainOnly, independent, err := registeredResultPacketSourceRoot(mainRoot, packetModeVerificationState(), packet, false)
	if err != nil || independent || mainOnly != mainRoot {
		t.Fatalf("native/Main source without a declared execution = %q, independent=%t, err=%v", mainOnly, independent, err)
	}

	got, independent, err := registeredResultPacketSourceRoot(mainRoot, state, packet, false)
	if err != nil || !independent || got != workerRoot {
		t.Fatalf("registered execution root = %q, independent=%t, err=%v", got, independent, err)
	}
	got, independent, err = registeredResultPacketSourceRoot(mainRoot, state, packet, true)
	if err != nil || !independent || got != workerRoot {
		t.Fatalf("live Git Worker execution root = %q, independent=%t, err=%v", got, independent, err)
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
		ReviewRound: packet.Binding.ReviewRound, BaselineGeneration: packet.Binding.BaselineGeneration,
		ProducerAgentID: packet.Binding.OwnerAgentID, SubjectDigest: packet.Binding.SubjectDigest, Verdict: "pass",
	}
	receipt, err := newResultReceipt(mainRoot, state, plan, ptr, assignment, result, strings.Repeat("a", 64), strings.Repeat("b", 64), ".claude/evidence/result.json", strings.Repeat("c", 64), &packet, workerRoot)
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
	got, independent, err = registeredResultPacketSourceRoot(mainRoot, state, packet, false)
	if err != nil || !independent || got != workerRoot {
		t.Fatalf("replay depended on live Worker checkout: root=%q independent=%t err=%v", got, independent, err)
	}
	if _, _, err := registeredResultPacketSourceRoot(mainRoot, state, packet, true); err == nil {
		t.Fatal("new acceptance used a missing Worker checkout")
	}

	// A declared but mismatched current execution cannot silently fall back to
	// Main, and registering Main itself does not resurrect the shared entry.
	registry.Executions[e.AssignmentID] = func() workspace.Execution {
		bad := e
		bad.RuntimeID = "loop-other"
		return bad
	}()
	state["workspace"] = workspace.Encode(registry)
	if got, _, err := registeredResultPacketSourceRoot(mainRoot, state, packet, false); err == nil || got != "" {
		t.Fatalf("mismatched declared execution fell back to %q: %v", got, err)
	}
	registry.Executions[e.AssignmentID] = func() workspace.Execution {
		main := e
		main.Path = mainRoot
		return main
	}()
	state["workspace"] = workspace.Encode(registry)
	if got, _, err := registeredResultPacketSourceRoot(mainRoot, state, packet, false); err == nil || got != "" {
		t.Fatalf("declared Main execution remained a valid Worker source %q: %v", got, err)
	}
}

func initResultExecutionGitRoot(t *testing.T, root string) string {
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
	return resultExecutionGit(t, root, "rev-parse", "HEAD")
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

func TestPacketReceiptEventIdentityBindsExecutionRoot(t *testing.T) {
	ptr := &PlanPointer{PlanID: "plan-1", Path: "plan.json", SHA256: strings.Repeat("a", 64), Revision: 2}
	result := &Result{
		ResultID: "result-1", AssignmentID: "assignment-1", AssignmentRevision: 2,
		ReviewPlanID: "plan-1", ReviewRound: 3, BaselineGeneration: 4,
		ProducerAgentID: "agent-1", SubjectDigest: strings.Repeat("b", 64),
	}
	firstEvent, firstKey := receiptEventIdentity("runtime-1", 4, ptr, result, strings.Repeat("c", 64), "packet-1", strings.Repeat("d", 64), "/tmp/worker-one")
	secondEvent, secondKey := receiptEventIdentity("runtime-1", 4, ptr, result, strings.Repeat("c", 64), "packet-1", strings.Repeat("d", 64), "/tmp/worker-two")
	if firstEvent == secondEvent || firstKey == secondKey {
		t.Fatal("packet receipt identity did not bind the execution root")
	}
}

func TestWorkerExecutionArtifactDigestRejectsMainSubstitution(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	workspaceRel := "e2e-workspace"
	workspaceName := "fixture.json"
	mainWorkspace := filepath.Join(mainRoot, workspaceRel)
	workerWorkspace := filepath.Join(workerRoot, workspaceRel)
	if err := os.MkdirAll(mainWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workerWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	mainBytes, workerBytes := []byte("Main fixture bytes\n"), []byte("Worker fixture bytes\n")
	if err := os.WriteFile(filepath.Join(mainWorkspace, workspaceName), mainBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerWorkspace, workspaceName), workerBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	state := packetModeVerificationState()
	boundReq, _ := state["bound_req"].(map[string]any)
	boundReq["workspace"] = map[string]any{
		"project_root": mainRoot, "dev_branch": "main", "release_upstream": "main",
		"bound_commit": strings.Repeat("a", 40),
	}
	runtimeID, _ := state["runtime_id"].(string)
	registry := &workspace.ExecutionRegistry{
		Version: 1, MainRoot: mainRoot, CommonDir: filepath.Join(mainRoot, ".git"),
		Branch: "main", BoundHead: strings.Repeat("a", 40),
		Executions: map[string]workspace.Execution{
			"assignment-qa-1": {
				AssignmentID: "assignment-qa-1", RuntimeID: runtimeID, BaselineGeneration: 1,
				Generation: 1, AgentID: "agent-qa-1", Path: workerRoot,
				Branch: "worker-assignment-qa-1", TargetBranch: "main", Status: "ready",
				WritePaths: []string{}, Checks: []string{}, Inputs: map[string]string{},
			},
		},
	}
	state["workspace"] = workspace.Encode(registry)
	packet := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{
		RuntimeID: runtimeID, BaselineGeneration: 1,
		AssignmentID: "assignment-qa-1", OwnerAgentID: "agent-qa-1",
	}}
	authorityRoot, independent, err := registeredResultPacketSourceRoot(mainRoot, state, packet, false)
	if err != nil || !independent || authorityRoot != workerRoot {
		t.Fatalf("exact registered source root = %q, independent=%t, err=%v", authorityRoot, independent, err)
	}

	ptr := &PlanPointer{VerificationArtifactWorkspace: workspaceRel}
	plan := &Plan{}
	workerDigest, err := WorkspaceDigest(workerRoot, workspaceRel)
	if err != nil {
		t.Fatal(err)
	}
	mainDigest, err := WorkspaceDigest(mainRoot, workspaceRel)
	if err != nil {
		t.Fatal(err)
	}
	if workerDigest == mainDigest {
		t.Fatal("fixture roots unexpectedly have the same workspace digest")
	}
	result := &Result{VerificationArtifactDigest: &workerDigest}
	if err := verifyResultArtifactDigestAt(authorityRoot, plan, ptr, result, "e2e"); err != nil {
		t.Fatalf("Worker-bound artifact digest rejected at registered root: %v", err)
	}
	if err := verifyResultArtifactDigestAt(mainRoot, plan, ptr, result, "e2e"); err == nil {
		t.Fatal("Main workspace digest was accepted as a substitute for the registered Worker digest")
	}

	changedWorkerBytes := []byte("Worker fixture changed after submission\n")
	if err := os.WriteFile(filepath.Join(workerWorkspace, workspaceName), changedWorkerBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyResultArtifactDigestAt(authorityRoot, plan, ptr, result, "e2e"); err == nil {
		t.Fatal("old Worker digest survived a changed Worker source file")
	}
	changedWorkerSHA := sha256Of(changedWorkerBytes)
	resultRef := ".claude/evidence/result.json"
	if err := os.MkdirAll(filepath.Join(mainRoot, ".claude/evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	resultBytes, err := json.Marshal(map[string]any{
		"checks": []any{map[string]any{
			"evidence_refs": []string{"path:" + workspaceRel + "/" + workspaceName + "#sha256=" + changedWorkerSHA},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainRoot, filepath.FromSlash(resultRef)), resultBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"result_ref": resultRef}
	if err := verifyResultWorkspaceAnchors(mainRoot, authorityRoot, workspaceRel, "assignment-qa-1", row); err != nil {
		t.Fatalf("Result authority/Worker file anchor verification failed: %v", err)
	}
	if err := verifyResultWorkspaceAnchors(mainRoot, mainRoot, workspaceRel, "assignment-qa-1", row); err == nil {
		t.Fatal("Result path anchor was read from Main instead of the registered Worker")
	}
}

func TestVerifySealedArtifactDigestsRequiresCurrentPlanProjection(t *testing.T) {
	state := map[string]any{"review": map[string]any{
		"plan": map[string]any{
			"plan_id": "review-plan-1", "path": ".claude/review/plans/review-plan-1.json",
			"sha256": strings.Repeat("a", 64), "revision": 1, "review_round": 1,
			"status": "current", "e2e_coverage_state": "cold_start",
			"verification_artifact_workspace": "e2e-workspace",
		},
		"assignments": map[string]any{},
	}}
	current := PlanPointerFromState(state)
	if current == nil {
		t.Fatal("fixture did not project current plan")
	}
	stale := *current
	stale.VerificationArtifactWorkspace = ""
	if err := VerifySealedArtifactDigests(t.TempDir(), state, &stale, map[string]any{}); err == nil || !strings.Contains(err.Error(), "non-current Plan projection") {
		t.Fatalf("blank caller projection suppressed current workspace checks: %v", err)
	}
}

func TestTypedPathEvidenceUsesWorkerSourceWithoutReplacingAuthorityRoot(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	const rel = "observation.txt"
	if err := os.WriteFile(filepath.Join(mainRoot, rel), []byte("Main bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerRoot, rel), []byte("Worker bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	result := &Result{ClaimResults: []ClaimResult{{ClaimID: "claim-source", EvidenceRefs: []string{"path:" + rel}}}}
	if err := bindIssuedPathEvidence(mainRoot, workerRoot, nil, result, false); err != nil {
		t.Fatal(err)
	}
	if err := validateResultEvidenceReferences(mainRoot, nil, result, workerRoot); err != nil {
		t.Fatalf("actual Worker source rejected: %v", err)
	}
	if err := validateResultEvidenceReferences(mainRoot, nil, result); err == nil {
		t.Fatal("Main same-name replacement accepted as Worker evidence")
	}
	result.ClaimResults[0].EvidenceRefs[0] = "path:" + rel + "#sha256=" + sha256Of([]byte("Main bytes"))
	if err := validateResultEvidenceReferences(mainRoot, nil, result, workerRoot); err == nil {
		t.Fatal("Main digest accepted for actual Worker bytes")
	}
}
