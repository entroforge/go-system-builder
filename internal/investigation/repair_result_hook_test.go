package investigation_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type s9AcceptedRepairResultFixture struct {
	fixture                *intakeFixture
	owner                  s9HookOwner
	packet                 workpacket.RepairResultPacket
	execution              workspace.Execution
	workerRoot             string
	productBytes           []byte
	answerPath             string
	answerBytes            []byte
	issueSnapshot          runtime.Snapshot
	acceptedSnapshot       runtime.Snapshot
	candidateBeforeState   []byte
	candidateBeforeJournal []byte
	acceptedState          []byte
	acceptedJournal        []byte
	candidateReceipt       runtime.RepairResultCandidateReceipt
}

func prepareAcceptedS9RepairResultCandidate(t *testing.T) s9AcceptedRepairResultFixture {
	return prepareAcceptedS9RepairResultCandidateWithResume(t, false)
}

func prepareAcceptedS9RepairResultCandidateWithoutManualCompletion(t *testing.T) s9AcceptedRepairResultFixture {
	return prepareAcceptedS9RepairResultCandidateWithOptions(t, false, false, false)
}

func prepareAcceptedS9RepairResultCandidateWithResume(t *testing.T, withResume bool) s9AcceptedRepairResultFixture {
	return prepareAcceptedS9RepairResultCandidateWithOptions(t, withResume, true, false)
}

func prepareAcceptedS9RepairResultCandidateWithOptions(t *testing.T, withResume, withManualCompletion, professionalAnswerOnly bool) s9AcceptedRepairResultFixture {
	t.Helper()
	fixture, plan, owners := prepareS9RepairPlanReportHookFixtureWithActivity(t, installS9WorkerBootstrapInputs, withManualCompletion)
	if len(plan.Assignments) != 2 || len(owners) != 2 {
		t.Fatalf("fixture assignments/owners=%d/%d, want the original two-assignment S9 plan", len(plan.Assignments), len(owners))
	}
	const redCommand = "test -f internal/api/payload.go"
	if err := runS9FixtureCommand(fixture.root, "sh", "-c", redCommand); err == nil {
		t.Fatal("the original pre-fix red command unexpectedly passed")
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "internal/api/payload.go")); !os.IsNotExist(err) {
		t.Fatalf("pre-fix file must be absent, stat error=%v", err)
	}

	var issueReportsOut, issueReportsErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &issueReportsOut, &issueReportsErr); code != 0 {
		t.Fatalf("issue original S9 PlanReports: code=%d stderr=%s stdout=%s", code, issueReportsErr.String(), issueReportsOut.String())
	}
	var reportPackets []workpacket.RepairPlanReportPacket
	if err := json.Unmarshal(issueReportsOut.Bytes(), &reportPackets); err != nil || len(reportPackets) != 2 {
		t.Fatalf("decode original sibling PlanReports: packets=%d err=%v stdout=%s", len(reportPackets), err, issueReportsOut.String())
	}
	for _, packet := range reportPackets {
		if professionalAnswerOnly {
			viewBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(packet.ViewPath)))
			if err != nil {
				t.Fatal(err)
			}
			var view map[string]json.RawMessage
			var questionnaire workpacket.RepairPlanReportDraft
			if json.Unmarshal(viewBytes, &view) != nil || json.Unmarshal(viewBytes, &questionnaire) != nil ||
				len(questionnaire.Questions) == 0 || questionnaire.Instruction == "" || len(questionnaire.Read) == 0 ||
				view["inputs"] != nil || view["draft"] != nil {
				t.Fatalf("Plan view must expose the professional questionnaire without packet mirrors: %s", viewBytes)
			}
			planRead := false
			for _, material := range questionnaire.Read {
				if !filepath.IsAbs(material.Path) {
					t.Fatalf("Plan view material path must be absolute: %q", material.Path)
				}
				info, err := os.Stat(material.Path)
				if err != nil {
					t.Fatalf("Plan view material must exist at its supplied path: %v", err)
				}
				if !info.IsDir() {
					if _, err := os.ReadFile(material.Path); err != nil {
						t.Fatalf("Plan view file material must be readable: %v", err)
					}
				}
				if material.Path != filepath.Join(fixture.root, filepath.FromSlash(packet.Binding.PlanRef)) {
					continue
				}
				data, err := os.ReadFile(material.Path)
				var suppliedPlan repair.RepairPlan
				if err != nil || json.Unmarshal(data, &suppliedPlan) != nil || suppliedPlan.PlanID != plan.PlanID || len(suppliedPlan.Assignments) != len(plan.Assignments) {
					t.Fatalf("Plan read pointer must retain the actual Plan and assignment IDs: %v", err)
				}
				planRead = true
			}
			if !planRead {
				t.Fatal("professional Plan view omitted its actual Plan read pointer")
			}
		}
		answerPath := writeS9RepairPlanReportForResult(t, fixture.root, packet, redCommand)
		receipt := runS9PostToolUse(t, fixture, packet.Binding.OwnerAgentID, fixture.root, answerPath)
		if !receipt.Accepted || receipt.PacketID != packet.ID || receipt.WorkComplete {
			t.Fatalf("original S9 pre-execution report was not accepted exactly: %+v", receipt)
		}
	}
	started := currentS9Snapshot(t, fixture)
	if life, _ := started.State["lifecycle"].(map[string]any); life == nil || life["phase"] != "fixing" {
		t.Fatalf("personal PlanReport save did not reach normal work startup: %v", started.State["lifecycle"])
	}

	owner := owners[0]
	if code := runS9Command(t, fixture.root, "runtime", "workspace", "bind", "--root", fixture.root); code != 0 {
		t.Fatal("original Main workspace bind failed")
	}
	prepareOutput := runS9CommandOutput(t, fixture.root, "runtime", "workspace", "prepare", "--root", fixture.root,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--check-location", "main")
	var preparation struct {
		Execution workspace.Execution `json:"execution"`
	}
	if err := json.Unmarshal(prepareOutput, &preparation); err != nil || preparation.Execution.Status != "ready" || preparation.Execution.CheckLocation != "main" || len(preparation.Execution.Checks) != 0 {
		t.Fatalf("S9 Workspace prepare must leave Main checks for actual integration: execution=%+v err=%v output=%s", preparation.Execution, err, prepareOutput)
	}
	if professionalAnswerOnly {
		var output map[string]json.RawMessage
		if err := json.Unmarshal(prepareOutput, &output); err != nil {
			t.Fatal(err)
		}
		if _, exists := output["commit_command"]; exists {
			t.Fatalf("S9 prepare still asks for a commit form: %s", prepareOutput)
		}
		var nextAction string
		if err := json.Unmarshal(output["next_action"], &nextAction); err != nil {
			t.Fatal(err)
		}
		for _, guidance := range []string{"ordinary git add and git commit", "no-change tests require no new commit", "save the supplied RepairResult answer", "No extra commit form or generic completion report", "Main owns real integration and necessary checks"} {
			if !strings.Contains(nextAction, guidance) {
				t.Fatalf("S9 prepare lost %q guidance: %s", guidance, prepareOutput)
			}
		}
		t.Log("public S9 prepare omits commit_command and supplies ordinary Git, no-change, Result-save and Main integration guidance")
	}
	prepared := preparation.Execution
	workerRoot := prepared.Path
	if workerRoot == "" || workerRoot == fixture.root {
		t.Fatalf("S9 execution must use a separate registered Worker checkout: %q", workerRoot)
	}
	installS9FakeClaude(t)
	_ = runS9CommandOutput(t, fixture.root, "runtime", "workspace", "launch", "--root", fixture.root,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--timeout", "10s")
	launched := currentS9Snapshot(t, fixture)
	registry, err := workspace.Decode(launched.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original launched Workspace registry: %v", err)
	}
	execution, found := registry.Execution(owner.manifestAssignmentID, "loop-REQ-INTAKE", 2)
	if !found || execution.Status != "ready" || execution.PlatformSessionID == "" || execution.Path != workerRoot {
		t.Fatalf("original Worker execution lacks its registered session: found=%t execution=%+v", found, execution)
	}
	submissionPaths, err := registry.WorkerSubmissionPaths(execution)
	if err != nil {
		t.Fatalf("read original Worker execution's frozen submission paths: %v", err)
	}
	expectedSuccessorID := ""
	if withResume {
		var initialOut, initialErr bytes.Buffer
		if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &initialOut, &initialErr); code != 0 {
			t.Fatalf("issue original working S9 packet before Worker changes: code=%d stderr=%s stdout=%s", code, initialErr.String(), initialOut.String())
		}
		var initialPackets []workpacket.RepairResultPacket
		if err := json.Unmarshal(initialOut.Bytes(), &initialPackets); err != nil || len(initialPackets) != 1 {
			t.Fatalf("decode original working S9 packet: packets=%d err=%v stdout=%s", len(initialPackets), err, initialOut.String())
		}
		successor := resolveS9RepairResultBlockerAndIssueSuccessor(t, fixture, owner, execution, workerRoot, initialPackets[0])
		if successor.Binding.Resume == nil {
			t.Fatal("accepted Main resolution did not bind the new issued packet to its verified predecessor")
		}
		expectedSuccessorID = successor.ID
	}

	productPath := filepath.Join(workerRoot, "internal/api/payload.go")
	if err := os.MkdirAll(filepath.Dir(productPath), 0o755); err != nil {
		t.Fatal(err)
	}
	productBytes := []byte("package api\n\n// Payload marks the accepted S9 repair boundary.\nfunc Payload() string { return \"ready\" }\n")
	if err := os.WriteFile(productPath, productBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if professionalAnswerOnly {
		for _, args := range [][]string{{"add", "--", "internal/api/payload.go"}, {"commit", "-m", "Implement the assigned payload boundary"}} {
			if _, err := workspace.Git(context.Background(), workerRoot, args...); err != nil {
				t.Fatalf("ordinary Worker Git %v failed: %v", args, err)
			}
		}
		t.Log("actual Worker source change committed with ordinary git add and git commit")
	} else {
		commitS9WorkerPayload(t, workerRoot, owner, submissionPaths)
	}
	if _, err := workspace.Git(context.Background(), workerRoot, "diff", "--check", "HEAD^", "HEAD"); err != nil {
		t.Fatalf("original Worker committed patch failed the real Git whitespace check: %v", err)
	}
	if withManualCompletion {
		if err := writeS9RepairCompletionReport(t, workerRoot, owner, execution.RuntimeID, submissionPaths, productBytes); err != nil {
			t.Fatal(err)
		}
		if code := runS9Command(t, fixture.root, "runtime", "workspace", "report", "--root", fixture.root,
			"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID); code != 0 {
			t.Fatal("original Worker completion_report consumer failed")
		}
	}

	var resultPackets []workpacket.RepairResultPacket
	if withResume {
		var issueResultOut, issueResultErr bytes.Buffer
		if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &issueResultOut, &issueResultErr); code != 0 {
			issueState := currentS9State(t, fixture)
			issueRegistry, registryErr := workspace.Decode(issueState)
			t.Fatalf("issue original typed S9 RepairResult: code=%d stderr=%s stdout=%s workspace executions=%+v registry error=%v PlanReport bindings=%+v", code, issueResultErr.String(), issueResultOut.String(), issueRegistry, registryErr, []workpacket.RepairPlanReportPacket{reportPackets[0], reportPackets[1]})
		}
		if err := json.Unmarshal(issueResultOut.Bytes(), &resultPackets); err != nil || len(resultPackets) != 1 {
			t.Fatalf("decode current ready S9 RepairResult packet: packets=%d err=%v stdout=%s", len(resultPackets), err, issueResultOut.String())
		}
	} else {
		packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), fixture.root), currentS9State(t, fixture))
		if err != nil {
			t.Fatal(err)
		}
		for _, packet := range packets {
			if packet.Binding.Sources.OwnerAgentID == owner.agentID && packet.Binding.Sources.SessionID == plan.SessionID && packet.Binding.Sources.PlanID == plan.PlanID {
				resultPackets = append(resultPackets, packet)
			}
		}
		if len(resultPackets) != 1 {
			t.Fatalf("normal prepare must publish one personal Result: %d", len(resultPackets))
		}
		for _, path := range []string{resultPackets[0].ViewPath, resultPackets[0].AnswerPath} {
			if _, err := os.Stat(filepath.Join(workerRoot, filepath.FromSlash(path))); err != nil {
				t.Fatalf("normal prepare omitted personal Result file %s: %v", path, err)
			}
		}
	}
	packet := resultPackets[0]
	if professionalAnswerOnly {
		viewBytes, err := os.ReadFile(filepath.Join(workerRoot, filepath.FromSlash(packet.ViewPath)))
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]json.RawMessage
		var questionnaire workpacket.RepairResultDraft
		if json.Unmarshal(viewBytes, &view) != nil || json.Unmarshal(viewBytes, &questionnaire) != nil ||
			len(questionnaire.Questions) == 0 || questionnaire.Instruction == "" || len(questionnaire.Read) == 0 ||
			view["inputs"] != nil || view["draft"] != nil {
			t.Fatalf("Result view must expose the professional questionnaire without packet mirrors: %s", viewBytes)
		}
		for _, material := range questionnaire.Read {
			if !filepath.IsAbs(material.Path) {
				t.Fatalf("Result view material path must be absolute: %q", material.Path)
			}
			info, err := os.Stat(material.Path)
			if err != nil {
				t.Fatalf("Result view material must exist at its supplied path: %v", err)
			}
			if !info.IsDir() {
				if _, err := os.ReadFile(material.Path); err != nil {
					t.Fatalf("Result view file material must be readable: %v", err)
				}
			}
		}
		answerDraft, err := os.ReadFile(filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath)))
		if err != nil || !bytes.Equal(answerDraft, viewBytes) {
			t.Fatalf("initial Result view and answer must supply identical professional materials: %v", err)
		}
	}
	if packet.Binding.Sources.OwnerAgentID != owner.agentID || packet.Binding.Sources.RepairAssignmentID != plan.Assignments[0].AssignmentID || packet.Binding.Sources.ManifestAssignmentID != owner.manifestAssignmentID {
		t.Fatalf("issued S9 result packet does not bind the selected original assignment: %+v", packet.Binding.Sources)
	}
	if withResume {
		if packet.ID != expectedSuccessorID || packet.Binding.Resume == nil {
			t.Fatalf("normal issuer changed or lost the current Resume successor after Worker completion: got=%s want=%s resume=%+v", packet.ID, expectedSuccessorID, packet.Binding.Resume)
		}
	}
	// The listener gives the exact existing Worker questionnaire before any
	// answer is authored; no terminal issue/consume is an Agent step here.
	listenerInput, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": "Read", "agent_id": owner.agentID,
		"cwd": workerRoot, "session_id": execution.PlatformSessionID,
		"tool_input": map[string]any{"file_path": filepath.Join(workerRoot, filepath.FromSlash(packet.ViewPath))},
	})
	var listenerOut, listenerErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", fixture.root}, bytes.NewReader(listenerInput), &listenerOut, &listenerErr); code != 0 {
		t.Fatalf("S9 questionnaire listener: %d %s", code, &listenerErr)
	}
	var listenerEnvelope struct {
		Output struct {
			Context    string `json:"additionalContext"`
			Permission string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(listenerOut.Bytes(), &listenerEnvelope); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listenerEnvelope.Output.Context, filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))) || listenerEnvelope.Output.Permission == "deny" {
		t.Fatalf("S9 listener lost its exact Worker questionnaire path: %s", &listenerOut)
	}
	beforeReceiptState, beforeReceiptJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	unacceptedBytes := runS9CommandOutput(t, fixture.root, "runtime", "stage-answer", "receipt", "--root", fixture.root, "--agent", owner.agentID, "--packet", packet.ID)
	assertSharedSubmissionReceiptWire(t, unacceptedBytes)
	var unaccepted submission.Receipt
	if err := json.Unmarshal(unacceptedBytes, &unaccepted); err != nil || unaccepted.Accepted || unaccepted.WorkComplete || unaccepted.ResponsibilityStatus != "not_accepted" || unaccepted.PacketID != packet.ID {
		t.Fatalf("unaccepted original candidate query: receipt=%+v err=%v output=%s", unaccepted, err, unacceptedBytes)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeReceiptState, beforeReceiptJournal, "read-only unaccepted candidate receipt")
	answerPath, answerBytes := writeS9RepairResultAnswer(t, workerRoot, packet, productBytes)
	if professionalAnswerOnly {
		var authored struct {
			Answer json.RawMessage `json:"answer"`
		}
		if err := json.Unmarshal(answerBytes, &authored); err != nil {
			t.Fatal(err)
		}
		answerBytes, err = json.Marshal(map[string]json.RawMessage{"answer": authored.Answer})
		if err != nil {
			t.Fatal(err)
		}
		answerBytes = append(answerBytes, '\n')
		if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(answerPath)), answerBytes, 0600); err != nil {
			t.Fatal(err)
		}
		t.Log("normal public Result save uses only {answer: professional answer}; UI envelope omitted without changing professional content")
	}

	if !withManualCompletion {
		// The professional path starts without a generic completion envelope.
		// Saving this Result must preserve the Agent/TASK lifecycle.
		completionPath := filepath.Join(workerRoot, filepath.FromSlash(submissionPaths.Completion))
		if _, err := os.Stat(completionPath); !os.IsNotExist(err) {
			t.Fatalf("new S9 path already has a separately authored completion report: %s (stat error %v)", completionPath, err)
		}
		entities, _ := currentS9State(t, fixture)["entities"].(map[string]any)
		agents, _ := entities["agents"].([]any)
		ownerReported := false
		for _, raw := range agents {
			row, _ := raw.(map[string]any)
			completionRef, _ := row["completion_reported_ref"].(string)
			if row["id"] == owner.agentID && completionRef != "" {
				ownerReported = true
			}
		}
		if ownerReported {
			t.Fatal("new S9 path reached RepairResult save with a separately reported generic completion")
		}
	}

	// Public candidate capture needs the current execution and actual Git
	// source, without a private issued capability. Capturing is not acceptance.
	beforeNoCapabilityState, beforeNoCapabilityJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	publicCandidate, _, candidateErr := repair.CreateWorkspaceCandidate(context.Background(), fixture.root, currentS9State(t, fixture), execution, repair.RepairResultRequest{
		ResultID: "repair-result-public-capture", AssignmentID: packet.Binding.Sources.RepairAssignmentID,
		ProducerAgentID: owner.agentID, UnitResults: []repair.RepairUnitResult{{UnitID: "unit-api-contract", Status: "pass"}},
	})
	if candidateErr != nil || publicCandidate.ExecutionGeneration != execution.Generation ||
		publicCandidate.AssignmentID != execution.AssignmentID || publicCandidate.SourceHead == "" ||
		len(publicCandidate.Proposal.ChangedArtifacts) != 1 || publicCandidate.Proposal.ChangedArtifacts[0].Path != "internal/api/payload.go" {
		t.Fatalf("public candidate did not capture the current attempt and actual Git change: candidate=%+v err=%v", publicCandidate, candidateErr)
	}
	staleExecution := execution
	staleExecution.Generation++
	if _, _, err := repair.CreateWorkspaceCandidate(context.Background(), fixture.root, currentS9State(t, fixture), staleExecution, publicCandidate.Proposal); err == nil {
		t.Fatal("public candidate accepted a different execution attempt")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeNoCapabilityState, beforeNoCapabilityJournal, "public candidate capture and wrong-attempt refusal")

	// The registered execution root still identifies the actual work location;
	// the public Hook derives source/owner facts from the current packet.
	issuedResult := currentS9Snapshot(t, fixture)
	if !professionalAnswerOnly {
		beforeLegacyState, beforeLegacyJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
		_, _, _, legacyErr := repair.SubmitRepairResultToRuntime(fixture.root, fixture.statePath, fixture.journalPath, repair.SubmitResultRuntimeRequest{
			RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: issuedResult.Revision, Actor: owner.agentID},
			Result: repair.RepairResultRequest{
				ResultID: packet.ResultID, AssignmentID: packet.Binding.Sources.RepairAssignmentID, ProducerAgentID: owner.agentID,
			},
		})
		if legacyErr == nil || !strings.Contains(legacyErr.Error(), "already has an issued typed RepairResult packet") {
			t.Fatalf("legacy untyped Result Writer bypassed the issued S9 packet guard: %v", legacyErr)
		}
		assertS9RuntimeBytesUnchanged(t, fixture, beforeLegacyState, beforeLegacyJournal, "legacy untyped issued-target refusal")
	}
	beforeRejectedState, beforeRejectedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	baseRequest := repair.AcceptIssuedRepairResultCandidateRequest{
		ExpectedRevision: issuedResult.Revision, PacketID: packet.ID, AnswerPath: packet.AnswerPath,
		OwnerAgentID: owner.agentID, SourceRoot: workerRoot, OccurredAt: time.Now().UTC(),
		AuthorizeSource: func(string, map[string]any) error { return nil },
	}
	wrongRoot := baseRequest
	wrongRoot.SourceRoot = t.TempDir()
	if _, _, _, err := repair.AcceptIssuedRepairResultCandidate(context.Background(), fixture.root, fixture.statePath, fixture.journalPath, wrongRoot); err == nil {
		t.Fatal("direct original candidate receiver accepted a foreign SourceRoot")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeRejectedState, beforeRejectedJournal, "rejected direct candidate foreign work location")

	answerAbs := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	beforeInvalidState, beforeInvalidJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if err := os.WriteFile(answerAbs, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidAnswerReceipt := runS9RepairResultHookPayload(t, fixture.root, s9RepairResultHookPayload("unrelated-hook-agent", filepath.Join(fixture.root, "unrelated-cwd"), "unrelated-session", answerAbs))
	if invalidAnswerReceipt.Accepted {
		t.Fatalf("original S9 professional Result consumer accepted a malformed answer: %+v", invalidAnswerReceipt)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeInvalidState, beforeInvalidJournal, "invalid S9 professional answer")
	if err := os.WriteFile(answerAbs, answerBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	terminalOut, terminalErr := new(bytes.Buffer), new(bytes.Buffer)
	unissuedWorkerPath := filepath.Join(fixture.root, ".claude", "submissions", "unregistered-worker", "S9-result", "answer.json")
	stateBeforeUnissued, journalBeforeUnissued := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", fixture.root, "--agent", owner.agentID, "--path", unissuedWorkerPath}, nil, terminalOut, terminalErr); code == 0 || !strings.Contains(terminalErr.String(), "S9 RepairResult answer path has no current registered Worker issuance") {
		t.Fatalf("terminal unregistered S9 Worker path was not rejected by exact-source lookup: code=%d stdout=%s stderr=%s", code, terminalOut.String(), terminalErr.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBeforeUnissued, journalBeforeUnissued, "unregistered terminal S9 Worker source")

	hookStarted := time.Now()
	candidateBeforeState, candidateBeforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	accepted := runS9RepairResultHookPayload(t, fixture.root, s9RepairResultHookPayload("unrelated-hook-agent", filepath.Join(fixture.root, "unrelated-cwd"), "unrelated-session", answerAbs))
	hookElapsed := time.Since(hookStarted)
	t.Logf("original S9 candidate Hook response elapsed=%s", hookElapsed)
	if withResume && hookElapsed > 10*time.Second {
		t.Fatalf("resumed candidate exceeded the original platform Hook timeout: %s", hookElapsed)
	}
	if !accepted.Accepted || accepted.PacketID != packet.ID || !accepted.WorkComplete || !accepted.ConsumerPending {
		t.Fatalf("accepted S9 Worker candidate did not project owner-complete/Main-pending: %+v", accepted)
	}
	if !professionalAnswerOnly {
		if _, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, currentS9State(t, fixture), packet.ID, owner.agentID); err != nil || !found {
			t.Fatalf("original Runtime lacks exact candidate receipt: found=%t err=%v", found, err)
		}
		if _, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, currentS9State(t, fixture), packet.ID, owner.agentID); err != nil || found {
			t.Fatalf("candidate acceptance prematurely created final Main Result receipt: found=%t err=%v", found, err)
		}
	}

	acceptedSnapshot := currentS9Snapshot(t, fixture)
	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	deniedSource := baseRequest
	deniedSource.ExpectedRevision = acceptedSnapshot.Revision
	callbackCalled := false
	deniedSource.AuthorizeSource = func(string, map[string]any) error {
		callbackCalled = true
		return errors.New("fixture source/session denial")
	}
	if _, _, _, err := repair.AcceptIssuedRepairResultCandidate(context.Background(), fixture.root, fixture.statePath, fixture.journalPath, deniedSource); err != nil || callbackCalled {
		t.Fatalf("accepted same-answer replay must ignore the old source authorization callback: called=%t err=%v", callbackCalled, err)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "same-answer replay ignores old source authorization")
	var candidateReceipt runtime.RepairResultCandidateReceipt
	if professionalAnswerOnly {
		publicBytes := runS9CommandOutput(t, fixture.root, "runtime", "stage-answer", "receipt", "--root", fixture.root, "--agent", owner.agentID, "--packet", packet.ID)
		var receipt submission.Receipt
		if err := json.Unmarshal(publicBytes, &receipt); err != nil || !receipt.Accepted || !receipt.WorkComplete || len(receipt.DomainRefs) != 1 {
			t.Fatalf("public candidate facts unavailable: receipt=%+v err=%v", receipt, err)
		}
		ref := receipt.DomainRefs[0]
		candidateBytes := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(ref)))
		candidate, err := repair.ReadWorkspaceCandidate(fixture.root, repair.ArtifactRef{Path: ref})
		if err != nil || candidate.Provenance == nil {
			t.Fatalf("read current candidate: %v", err)
		}
		candidateReceipt = runtime.RepairResultCandidateReceipt{CandidateRef: ref, CandidateSHA256: workpacket.Digest(candidateBytes), Provenance: *candidate.Provenance}
	} else {
		var candidateFound bool
		var err error
		candidateReceipt, candidateFound, err = runtime.VerifiedRepairResultCandidateReceipt(fixture.root, acceptedSnapshot.State, packet.ID, owner.agentID)
		if err != nil || !candidateFound {
			t.Fatalf("read original accepted S9 candidate receipt for shared fixture: found=%t err=%v", candidateFound, err)
		}
	}
	if !withManualCompletion {
		provenance := candidateReceipt.Provenance
		if provenance.CompletionReportRef != "" || provenance.CompletionReportSHA256 != "" || provenance.CompletionReportEventID != "" {
			t.Fatalf("professional candidate generated a generic completion binding: %+v", provenance)
		}
		if _, err := runtime.CurrentRepairResultCompletionReport(fixture.root, acceptedSnapshot.State, packet); err == nil {
			t.Fatal("professional Result save generated a generic completion report")
		}
		beforeEntities, _ := json.Marshal(issuedResult.State["entities"])
		afterEntities, _ := json.Marshal(acceptedSnapshot.State["entities"])
		if !bytes.Equal(beforeEntities, afterEntities) {
			t.Fatal("professional Result save changed the Agent/TASK lifecycle")
		}
	}
	return s9AcceptedRepairResultFixture{
		fixture: fixture, owner: owner, packet: packet, execution: execution,
		workerRoot: workerRoot, productBytes: append([]byte(nil), productBytes...),
		answerPath: answerPath, answerBytes: append([]byte(nil), answerBytes...),
		issueSnapshot: issuedResult, acceptedSnapshot: acceptedSnapshot,
		candidateBeforeState: append([]byte(nil), candidateBeforeState...), candidateBeforeJournal: append([]byte(nil), candidateBeforeJournal...),
		acceptedState: append([]byte(nil), acceptedState...), acceptedJournal: append([]byte(nil), acceptedJournal...),
		candidateReceipt: candidateReceipt,
	}
}

// TestS9RepairResultUsesOriginalWorkerCandidateAndMainIntegrationChain runs
// the original approval, S9 dispatch/PLAN/PlanReport, Workspace execution,
// Hook reception, Worker commit/check, professional candidate acceptance,
// and Main integration writers. The
// authored professional answer is mechanical fixture input; this test does
// not claim model judgment or a human-reviewed engineering conclusion.
func TestS9RepairResultCandidateDoesNotRequireGenericCompletion(t *testing.T) {
	accepted := prepareAcceptedS9RepairResultCandidateWithoutManualCompletion(t)
	if accepted.acceptedSnapshot.Revision != accepted.issueSnapshot.Revision+1 {
		t.Fatalf("professional Result required additional lifecycle commits: before=%d after=%d", accepted.issueSnapshot.Revision, accepted.acceptedSnapshot.Revision)
	}
	beforeState, beforeJournal := mustRead(t, accepted.fixture.statePath), mustRead(t, accepted.fixture.journalPath)
	replayed := runS9RepairResultPostToolUse(t, accepted.fixture.root, accepted.owner.agentID, accepted.workerRoot, accepted.execution.PlatformSessionID, accepted.answerPath)
	if !replayed.Accepted || !replayed.WorkComplete {
		t.Fatalf("professional candidate same-answer replay failed: %+v", replayed)
	}
	assertS9RuntimeBytesUnchanged(t, accepted.fixture, beforeState, beforeJournal, "professional candidate same-answer replay without generic completion")
}

func TestS9RepairResultUsesOriginalWorkerCandidateAndMainIntegrationChain(t *testing.T) {
	acceptedFixture := prepareAcceptedS9RepairResultCandidateWithOptions(t, false, false, true)
	fixture := acceptedFixture.fixture
	owner := acceptedFixture.owner
	packet := acceptedFixture.packet
	execution := acceptedFixture.execution
	workerRoot := acceptedFixture.workerRoot
	productBytes := acceptedFixture.productBytes
	answerPath := acceptedFixture.answerPath
	answerBytes := acceptedFixture.answerBytes
	issuedResult := acceptedFixture.issueSnapshot
	acceptedState, acceptedJournal := acceptedFixture.acceptedState, acceptedFixture.acceptedJournal
	// Typed delivery facts must be visible without a generic completion report.
	pendingBytes := runS9CommandOutput(t, fixture.root, "runtime", "workspace", "pending", "--root", fixture.root)
	var pending []integration.PendingIntegration
	if err := json.Unmarshal(pendingBytes, &pending); err != nil {
		t.Fatal(err)
	}
	pendingFound := false
	for _, row := range pending {
		if row.AssignmentID == owner.manifestAssignmentID {
			pendingFound = strings.Join(row.Command, " ") == "loop-harness runtime workspace integrate --root "+fixture.root+" --assignment "+owner.manifestAssignmentID+" --agent "+owner.agentID && strings.Contains(row.Failure, "--check-command")
		}
	}
	if !pendingFound {
		t.Fatalf("accepted typed candidate disappeared from normal Main pending projection: %s", pendingBytes)
	}
	readMainContext := func() string {
		t.Helper()
		input, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Read", "agent_id": "Main", "cwd": fixture.root, "tool_input": map[string]any{"file_path": filepath.Join(fixture.root, "internal/api/payload.go")}})
		if err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", fixture.root}, bytes.NewReader(input), &out, &stderr); code != 0 {
			t.Fatalf("Main current-work projection: %d %s", code, &stderr)
		}
		var envelope struct {
			Output struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Output.Context
	}
	candidateContext := readMainContext()
	candidateHint := "Assignment " + packet.Binding.Sources.RepairAssignmentID + ": candidate received; Main runs "
	start := strings.Index(candidateContext, candidateHint)
	if start < 0 {
		t.Fatalf("Main current feedback omitted accepted typed candidate: %s", candidateContext)
	}
	currentHint := candidateContext[start:]
	if end := strings.Index(currentHint, ". "); end >= 0 {
		currentHint = currentHint[:end]
	}
	if !strings.Contains(currentHint, "runtime workspace integrate") || !strings.Contains(currentHint, "--check-command") || strings.Contains(currentHint, packet.AnswerPath) {
		t.Fatalf("candidate feedback did not prioritize integration over the old questionnaire: %s", currentHint)
	}
	// Acceptance is not completion: the same current candidate may honestly
	// record unfinished work. This only projects fixture facts, without a save.
	candidatePath := filepath.Join(fixture.root, filepath.FromSlash(acceptedFixture.candidateReceipt.CandidateRef))
	candidateBytes := mustRead(t, candidatePath)
	var pendingCandidate repair.WorkspaceCandidate
	if err := json.Unmarshal(candidateBytes, &pendingCandidate); err != nil {
		t.Fatal(err)
	}
	pendingCandidate.Proposal.Verdict = "blocked"
	pendingBytes, err := json.Marshal(pendingCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, pendingBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if done, err := repair.RepairResultWorkComplete(fixture.root, acceptedFixture.acceptedSnapshot.State, packet.ID, owner.agentID); err != nil || done {
		t.Fatalf("received blocked candidate was treated as completed: done=%t err=%v", done, err)
	}
	pendingCandidate.Proposal.Verdict = "completed"
	pendingCandidate.Proposal.UnitResults = nil
	pendingBytes, err = json.Marshal(pendingCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, pendingBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if done, err := repair.RepairResultWorkComplete(fixture.root, acceptedFixture.acceptedSnapshot.State, packet.ID, owner.agentID); err != nil || done {
		t.Fatalf("received candidate with missing actual unit work was treated as completed: done=%t err=%v", done, err)
	}
	if err := os.WriteFile(candidatePath, candidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	// Changed bytes under the accepted same packet conflict; restoring the exact
	// answer allows a zero-write replay of the original durable candidate.
	changedAnswer := bytes.Replace(answerBytes, []byte("Mechanical fixture observation"), []byte("Changed fixture observation"), 1)
	if bytes.Equal(changedAnswer, answerBytes) {
		t.Fatal("changed-answer fixture did not change bytes")
	}
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(answerPath)), changedAnswer, 0o600); err != nil {
		t.Fatal(err)
	}
	changedReceipt := runS9RepairResultPostToolUse(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, answerPath)
	if changedReceipt.Accepted || !strings.Contains(strings.ToLower(changedReceipt.Error), "different content") {
		t.Fatalf("changed accepted S9 answer did not produce the original conflict receipt: %+v", changedReceipt)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "changed S9 answer replay")
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(answerPath)), answerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	exactReplay := runS9RepairResultPostToolUse(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, answerPath)
	if !exactReplay.Accepted || exactReplay.PacketID != packet.ID || !exactReplay.WorkComplete {
		t.Fatalf("original accepted S9 answer did not replay from its durable receipt: %+v", exactReplay)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, acceptedState, acceptedJournal, "exact accepted S9 RepairResult replay")

	if code := runS9HookEvent(t, fixture.root, "SubagentStop", owner.agentID, workerRoot, execution.PlatformSessionID); code != 0 {
		t.Fatal("original S9 Worker Stop remained blocked after its exact accepted professional Result and auto-projected completion")
	}

	testCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(testCWD, fixture.root)
	if err != nil || filepath.IsAbs(relativeRoot) {
		t.Fatalf("normal relative Main root: %q %v", relativeRoot, err)
	}
	var integratedOut, integratedErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "workspace", "integrate", "--root", relativeRoot, "--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--check-command", "test -f internal/api/payload.go"}, nil, &integratedOut, &integratedErr); code != 0 {
		t.Fatalf("original Main merge/check/Result/ACK/cleanup chain failed: code=%d stderr=%s stdout=%s", code, integratedErr.String(), integratedOut.String())
	}
	final := currentS9Snapshot(t, fixture)
	completedContext := readMainContext()
	if !strings.Contains(completedContext, "Assignment "+packet.Binding.Sources.RepairAssignmentID+" is complete; Main integration/checks and cleanup are recorded.") || strings.Contains(completedContext, "Original execution "+owner.manifestAssignmentID+" owner=") {
		t.Fatalf("completed execution still asks Main to recover or repeat old author work: %s", completedContext)
	}

	publicReceiptBytes := runS9CommandOutput(t, fixture.root, "runtime", "stage-answer", "receipt", "--root", fixture.root, "--agent", owner.agentID, "--packet", packet.ID)
	assertSharedSubmissionReceiptWire(t, publicReceiptBytes)
	var publicReceipt submission.Receipt
	if err := json.Unmarshal(publicReceiptBytes, &publicReceipt); err != nil || publicReceipt.PacketID != packet.ID || !publicReceipt.Accepted || !publicReceipt.WorkComplete {
		t.Fatalf("public original S9 receipt did not project accepted current owner completion: receipt=%+v err=%v output=%s", publicReceipt, err, publicReceiptBytes)
	}
	mainPayload := filepath.Join(fixture.root, "internal/api/payload.go")
	if actual := mustRead(t, mainPayload); !bytes.Equal(actual, productBytes) {
		t.Fatalf("original non-squash integration did not deliver the Worker product bytes: %q", actual)
	}
	finalRegistry, err := workspace.Decode(final.State)
	if err != nil || finalRegistry == nil {
		t.Fatalf("decode final S9 Workspace registry: %v", err)
	}
	if finalExecution, exists := finalRegistry.Executions[owner.manifestAssignmentID]; !exists || finalExecution.Status != "complete" {
		t.Fatalf("original Main ACK/cleanup did not complete the Worker execution: %+v", finalExecution)
	}
	if _, err := workspace.Git(context.Background(), fixture.root, "status", "--porcelain", "--untracked-files=all"); err != nil {
		t.Fatalf("inspect final Main repository after integration: %v", err)
	}
	checkpoint, checkpointFound, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPathForS9(t, fixture.root, execution))
	if err != nil || !checkpointFound || checkpoint.State != integration.StateComplete {
		t.Fatalf("read final original Main checkpoint: found=%t checkpoint=%+v err=%v", checkpointFound, checkpoint, err)
	}
	if _, err := os.Stat(workerRoot); !os.IsNotExist(err) {
		t.Fatalf("original Main cleanup should have removed the Worker checkout before receipt replay; stat error=%v", err)
	}

	if len(publicReceipt.DomainRefs) != 2 {
		t.Fatalf("Final receipt lacks candidate/Result refs: %+v", publicReceipt)
	}
	var finalResult repair.RepairResult
	if err := json.Unmarshal(mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(publicReceipt.DomainRefs[1]))), &finalResult); err != nil || finalResult.Receipt == nil {
		t.Fatalf("read recorded Final facts: %v", err)
	}
	acceptedFinal := *finalResult.Receipt
	resultBytesBeforeReplay := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(acceptedFinal.ResultRef)))
	beforeReplayState, beforeReplayJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replaySnapshot, replayResult, replayRef, err := repair.SubmitIssuedRepairResult(context.Background(), fixture.root, fixture.statePath, fixture.journalPath, repair.SubmitIssuedRepairResultRequest{
		ExpectedRevision: issuedResult.Revision, PacketID: packet.ID, OwnerAgentID: owner.agentID,
		MainProof: acceptedFinal.MainProof, OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("replay accepted S9 Main Result with its original stale expected revision after Worker cleanup: %v", err)
	}
	if replaySnapshot.Revision != final.Revision || replayResult.ResultID != acceptedFinal.ResultID || replayRef.Path != acceptedFinal.ResultRef || workpacket.Digest(resultBytesBeforeReplay) != replayRef.SHA256 || !bytes.Equal(resultBytesBeforeReplay, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(replayRef.Path)))) {
		t.Fatalf("stale-revision exact Main Result replay changed immutable result identity/ref/bytes: snapshot=%d final=%d result=%s receipt=%s ref=%+v", replaySnapshot.Revision, final.Revision, replayResult.ResultID, acceptedFinal.ResultID, replayRef)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeReplayState, beforeReplayJournal, "stale-revision exact S9 Main Result replay")

	forgedProof := acceptedFinal.MainProof
	var forgedFacts map[string]any
	if err := json.Unmarshal(forgedProof.VerifiedFacts, &forgedFacts); err != nil || forgedFacts == nil {
		t.Fatalf("decode accepted Main facts for conflict negative: %v", err)
	}
	originalTestedHead, _ := forgedFacts["tested_head"].(string)
	forgedTestedHead := strings.Repeat("0", 40)
	if forgedTestedHead == originalTestedHead {
		forgedTestedHead = strings.Repeat("1", 40)
	}
	forgedFacts["tested_head"] = forgedTestedHead
	forgedProof.VerifiedFacts, err = json.Marshal(forgedFacts)
	if err != nil {
		t.Fatal(err)
	}
	beforeForgedState, beforeForgedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	_, _, _, forgedErr := repair.SubmitIssuedRepairResult(context.Background(), fixture.root, fixture.statePath, fixture.journalPath, repair.SubmitIssuedRepairResultRequest{
		ExpectedRevision: issuedResult.Revision, PacketID: packet.ID, OwnerAgentID: owner.agentID,
		MainProof: forgedProof, OccurredAt: time.Now().UTC(),
	})
	if !errors.Is(forgedErr, runtime.ErrSubmissionConflict) {
		t.Fatalf("stale-revision replay accepted forged nonempty Main verified facts: %v", forgedErr)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, beforeForgedState, beforeForgedJournal, "forged Main facts replay refusal")

	loadedOwner, err := hookctx.FromSnapshot(fixture.root, owner.agentID, final)
	if err != nil || loadedOwner == nil || loadedOwner.PolicyContext.Agent == nil {
		t.Fatalf("project original owner from completed Runtime snapshot: loaded=%+v err=%v", loadedOwner, err)
	}
	if !loadedOwner.PolicyContext.Agent.RepairResultAssignment || loadedOwner.PolicyContext.Agent.RepairAssignmentID != packet.Binding.Sources.RepairAssignmentID {
		t.Fatalf("completed owner snapshot lost its current typed S9 responsibility binding: %+v", loadedOwner.PolicyContext.Agent)
	}
	if !hook.RepairResultWorkComplete(fixture.root, final.State, loadedOwner.PolicyContext.Agent) {
		t.Fatal("original hook responsibility projector did not accept the exact final S9 Result after Main ACK/cleanup")
	}
	// Later-phase snapshots project the retained Builder facts only; this is
	// not a synthetic Impact/Targeted consumer acceptance or stage PASS.
	for _, phase := range []string{"impact_reconciliation", "targeted_reverification"} {
		encoded, err := json.Marshal(final.State)
		if err != nil {
			t.Fatal(err)
		}
		var later map[string]any
		if err := json.Unmarshal(encoded, &later); err != nil {
			t.Fatal(err)
		}
		later["lifecycle"].(map[string]any)["phase"] = phase
		later["review"].(map[string]any)["repair"].(map[string]any)["status"] = phase
		loaded, err := hookctx.FromSnapshot(fixture.root, owner.agentID, runtime.Snapshot{State: later, Revision: final.Revision})
		if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil || !loaded.PolicyContext.Agent.RepairResultAssignment ||
			!hook.RepairResultWorkComplete(fixture.root, later, loaded.PolicyContext.Agent) || len(loaded.PolicyContext.RepairResultAnswerPaths) != 0 {
			t.Fatalf("later %s lost completed Builder facts or exposed a fresh answer: loaded=%+v err=%v", phase, loaded, err)
		}
	}

}

func installS9WorkerBootstrapInputs(t *testing.T, root string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, "agents")); err != nil {
		t.Fatalf("remove legacy root agent assets from installed-layout fixture: %v", err)
	}
	copyS9RepositoryTree(t, filepath.Join("..", "..", "docs", "rules"), filepath.Join(root, "docs", "rules"))
	if err := os.MkdirAll(filepath.Join(root, ".claude/agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyS9RepositoryFile(t, filepath.Join("..", "..", "agents", "backend-builder.md"), filepath.Join(root, ".claude", "agents", "backend-builder.md"))
	copyS9RepositoryTree(t, filepath.Join("..", "..", "skills"), filepath.Join(root, ".claude", "skills"))
	ignore := []byte(".worktrees/\n.claude/*\n!.claude/agents/\n!.claude/agents/**\n!.claude/skills/\n!.claude/skills/**\n")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), ignore, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", "--", ".gitignore", "repair-contract-draft.json", "docs/control", "docs/rules", ".claude/agents", ".claude/skills").CombinedOutput(); err != nil {
		t.Fatalf("stage original S9 bootstrap/input sources: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", root, "commit", "-m", "fixture approved S9 source closure").CombinedOutput(); err != nil {
		t.Fatalf("commit original S9 bootstrap/input sources before RepairSession: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func writeS9RepairPlanReportForResult(t *testing.T, root string, packet workpacket.RepairPlanReportPacket, command string) string {
	t.Helper()
	answer := workpacket.RepairPlanReportProfessionalAnswer{
		Plan:          "The test fixture reproduces the approved missing-payload boundary before writing within the assigned API scope.",
		RedChecks:     []workpacket.RepairPlanReportCheck{{Name: "pre-fix payload presence", Command: command, Result: "fail", EvidenceRefs: []string{"test://s9-pre-fix/" + packet.Binding.RepairAssignmentID}}},
		ProposedPaths: []string{"internal/api/payload.go"},
	}
	data, err := json.Marshal(workpacket.RepairPlanReportDraft{Title: packet.Draft.Title, Instruction: packet.Draft.Instruction, Read: packet.Draft.Read, Questions: packet.Draft.Questions, Answer: mustJSONRaw(t, answer)})
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := workpacket.ValidateRepairPlanReportAnswer(packet, data); len(problems) != 0 {
		t.Fatalf("mechanical S9 PlanReport fixture answer invalid: %+v", problems)
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

func writeS9RepairResultAnswer(t *testing.T, workerRoot string, packet workpacket.RepairResultPacket, product []byte) (string, []byte) {
	t.Helper()
	var assignment struct {
		AssertionIDs []string `json:"assertion_ids"`
		UnitIDs      []string `json:"unit_ids"`
	}
	if err := json.Unmarshal(packet.Inputs.Sources.RepairAssignment, &assignment); err != nil {
		t.Fatalf("decode frozen S9 RepairAssignment answer slots: %v", err)
	}
	if len(assignment.AssertionIDs) == 0 || len(assignment.UnitIDs) == 0 {
		t.Fatalf("frozen S9 answer slots are empty: %+v", assignment)
	}
	hash := sha256.Sum256(product)
	productEvidence := "path:internal/api/payload.go#sha256=" + hex.EncodeToString(hash[:])
	answer := workpacket.RepairResultProfessionalAnswer{
		Verdict: "completed", BeforeFixResults: []workpacket.RepairResultBeforeFixResult{}, UnitResults: []workpacket.RepairResultUnitResult{},
		DetectionGapArtifacts: []string{}, EvidenceRefs: []string{productEvidence}, ScopeDeviations: []string{}, ResidualRisks: []string{},
		MigrationRef: "", RollbackRef: "",
	}
	for _, assertion := range assignment.AssertionIDs {
		answer.BeforeFixResults = append(answer.BeforeFixResults, workpacket.RepairResultBeforeFixResult{
			AssertionID: assertion, ObservedRed: "Mechanical fixture observation: the pre-fix presence command failed because the assigned payload path was absent.",
			EvidenceRefs: []string{"test://s9-pre-fix/" + packet.Binding.Sources.RepairAssignmentID},
		})
	}
	for _, unit := range assignment.UnitIDs {
		answer.UnitResults = append(answer.UnitResults, workpacket.RepairResultUnitResult{
			UnitID: unit, Status: "pass", EvidenceRefs: []string{productEvidence},
			ImplementationSummary: "Mechanical fixture added the missing payload boundary in the assigned source path.",
			RestoredInvariantRefs: []string{"root-1"},
		})
	}
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	outer := struct {
		Title       string                         `json:"title"`
		Instruction string                         `json:"instruction"`
		Read        []workpacket.ReadItem          `json:"read"`
		Questions   map[string]workpacket.Question `json:"questions"`
		Answer      json.RawMessage                `json:"answer"`
	}{packet.Draft.Title, packet.Draft.Instruction, packet.Draft.Read, packet.Draft.Questions, answerBytes}
	data, err := json.Marshal(outer)
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := workpacket.ValidateRepairResultAnswer(packet, data); len(problems) != 0 {
		t.Fatalf("mechanical S9 RepairResult fixture answer invalid: %+v", problems)
	}
	path := filepath.Join(workerRoot, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return packet.AnswerPath, data
}

func writeS9RepairCompletionReport(t *testing.T, workerRoot string, owner s9HookOwner, runtimeID string, paths workspace.WorkerSubmissionPaths, product []byte) error {
	t.Helper()
	hash := sha256.Sum256(product)
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "message_id": "msg-s9-result-complete-" + owner.agentID,
		"correlation_id": "corr-s9-result-complete-" + owner.assignmentID, "runtime_id": runtimeID, "agent_id": owner.agentID,
		"agent_definition_ref": ".claude/agents/backend-builder.md", "task_id": owner.taskID, "bug_id": nil, "team_id": owner.teamID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "completed",
		"summary":       "Added the assigned payload boundary; Main still owns the frozen integration check.",
		"changed_paths": []string{"internal/api/payload.go"}, "reviewed_paths": []string{}, "checks": []any{},
		"evidence_refs": []string{"path:internal/api/payload.go#sha256=" + hex.EncodeToString(hash[:])}, "finding_refs": []string{},
		"remaining_risks": []string{}, "scope_deviations": []string{}, "requested_event": "completion_reported",
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	path := filepath.Join(workerRoot, filepath.FromSlash(paths.Completion))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func s9RepairResultHookPayload(owner, cwd, session, answerPath string) map[string]any {
	return map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "Write",
		"tool_input": map[string]any{"file_path": answerPath}, "agent_id": owner, "cwd": cwd, "session_id": session,
	}
}

func runS9RepairResultPostToolUse(t *testing.T, root, owner, workerRoot, session, answerRel string) submission.Receipt {
	t.Helper()
	return runS9RepairResultHookPayload(t, root, s9RepairResultHookPayload(owner, workerRoot, session, filepath.Join(workerRoot, filepath.FromSlash(answerRel))))
}

func runS9RepairResultHookPayload(t *testing.T, root string, payload map[string]any) submission.Receipt {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("S9 RepairResult PostToolUse platform Hook exited %d: stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	var envelope struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode S9 RepairResult Hook envelope: %v: %s", err, out.String())
	}
	const prefix = "S9 SUBMISSION RECEIPT: "
	if !strings.HasPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix) {
		t.Fatalf("S9 RepairResult Hook did not return the original domain receipt: %s", out.String())
	}
	var receipt submission.Receipt
	receiptBytes := []byte(strings.TrimPrefix(envelope.HookSpecificOutput.AdditionalContext, prefix))
	assertSharedSubmissionReceiptWire(t, receiptBytes)
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode S9 RepairResult Hook receipt: %v", err)
	}
	return receipt
}

// Validate the entire public wire before decoding into a test projection:
// reduced test structs must not hide unknown fields or missing constraints.
func assertSharedSubmissionReceiptWire(t *testing.T, receiptBytes []byte) {
	t.Helper()
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", receiptBytes); err != nil {
		t.Fatalf("public submission receipt differs from the shared schema: %v\n%s", err, receiptBytes)
	}
}

func runS9HookEvent(t *testing.T, root, event, owner, cwd, session string) int {
	t.Helper()
	body, err := json.Marshal(map[string]any{"hook_event_name": event, "agent_id": owner, "cwd": cwd, "session_id": session, "stop_hook_active": false})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	return cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &errout)
}

func installS9FakeClaude(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })
}

func commitS9WorkerPayload(t *testing.T, workerRoot string, owner s9HookOwner, paths workspace.WorkerSubmissionPaths) {
	t.Helper()
	head, err := workspace.Git(context.Background(), workerRoot, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("read original Worker commit parent: %v", err)
	}
	requestBytes, err := json.Marshal(workspace.CommitRequest{
		ExpectedHead: head,
		Message:      "Implement the assigned payload boundary",
		Paths:        []string{"internal/api/payload.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(workerRoot, filepath.FromSlash(paths.Commit))
	if err := os.MkdirAll(filepath.Dir(requestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, append(requestBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runS9Command(t, workerRoot, "runtime", "workspace", "commit", "--root", workerRoot,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID); code != 0 {
		t.Fatalf("original Worker explicit-path commit failed: code=%d", code)
	}
}

func copyS9RepositoryTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		output := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(output, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("repository fixture source %s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o644)
	}); err != nil {
		t.Fatalf("copy original S9 repository source tree %s: %v", source, err)
	}
}

func runS9FixtureCommand(root, name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Dir = root
	return command.Run()
}

func runS9Command(t *testing.T, root string, args ...string) int {
	t.Helper()
	return runS9CommandCode(t, root, args...)
}

func runS9CommandCode(t *testing.T, root string, args ...string) int {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, nil, &stdout, &stderr)
	if code != 0 {
		t.Logf("command failed: %v; stderr=%s stdout=%s", args, stderr.String(), stdout.String())
	}
	return code
}

func runS9CommandOutput(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := cli.Run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("command %v failed: stderr=%s stdout=%s", args, stderr.String(), stdout.String())
	}
	return stdout.Bytes()
}

func workspaceExecutionCheckpointPathForS9(t *testing.T, root string, execution workspace.Execution) string {
	t.Helper()
	return integration.DefaultCheckpointStore().Path(root, execution.RuntimeID, execution.BaselineGeneration, execution.AssignmentID)
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
