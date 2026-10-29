package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// On a capable host this follows the S6 workspace owned-check path through
// the original CompleteTask Writer using an actual failing runner receipt.
// On an incapable host it verifies prepare denial and an unchanged Runtime
// pair, then records that the runner/Result path was not exercised.
func TestS6WorkspaceOwnedCheckCompletionReplayAliasAndNewMachineFacts(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const (
		manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
		owner       = "agent-s6-native-context"
		assignment  = "assignment-s6-native-context"
	)
	manifestPath := filepath.Join(root, filepath.FromSlash(manifestRef))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	row := manifest["assignments"].([]any)[0].(map[string]any)
	row["integration_check_mode"] = "post_merge"
	row["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
	workerProfile := map[string]any{
		"id": "profile-s6-workspace-owned-check", "version": "v1.0.0", "check_index": 0,
		"command": "go test ./src/pkg", "location": "worker", "timeout_seconds": 30,
		"environment_policy": "declared_only", "environment": []any{},
		"dependencies": []any{map[string]any{
			"path": "src/pkg/input.go", "sha256": workpacket.Digest([]byte("package pkg\n")),
		}},
		"fixtures": []any{map[string]any{
			"path": "testdata/input.txt", "sha256": workpacket.Digest([]byte("registered profile input\n")),
		}},
		"input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
	}
	row["closing_contract"] = map[string]any{
		"contract": assignmentrecord.ClosingContractVersion,
		"worker_checks": []any{map[string]any{
			"index": 0, "command": "go test ./src/pkg", "profile": workerProfile,
		}},
	}
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)

	registered := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(registered.State, owner)
	if err != nil || !found || record.AssignmentID != assignment || record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) != 1 || record.IntegrationCheckMode != "post_merge" {
		t.Fatalf("S6 registration did not freeze the Main/Worker check split: found=%t record=%+v err=%v", found, record, err)
	}
	beforePrepareState, beforePrepareJournal := s6CanonicalRuntimePair(t, root)
	var prepareOut, prepareErr bytes.Buffer
	prepareCode := cli.Run([]string{"runtime", "workspace", "prepare", "--root", root, "--assignment", assignment, "--agent", owner}, nil, &prepareOut, &prepareErr)
	if prepareCode != 0 {
		if !strings.Contains(prepareErr.String(), "Record-frozen WorkerCheck capability unavailable") {
			t.Fatalf("workspace prepare failed for a reason other than the expected fail-closed host capability check: code=%d stderr=%s stdout=%s", prepareCode, prepareErr.String(), prepareOut.String())
		}
		afterState, afterJournal := s6CanonicalRuntimePair(t, root)
		if !bytes.Equal(beforePrepareState, afterState) || !bytes.Equal(beforePrepareJournal, afterJournal) {
			t.Fatal("workspace prepare capability rejection changed the original Runtime state/journal")
		}
		t.Logf("workspace prepare correctly refused to publish an execution without WorkerCheck isolation: %s", strings.TrimSpace(prepareErr.String()))
		return
	}
	prepareOutput := prepareOut.String()
	var prepared struct {
		OwnedCheckCommands []string `json:"owned_check_commands"`
		Execution          workspace.Execution
	}
	if err := json.Unmarshal([]byte(prepareOutput), &prepared); err != nil || prepared.Execution.Status != "ready" || len(prepared.OwnedCheckCommands) != 1 {
		t.Fatalf("original workspace prepare did not project the frozen owned check: prepared=%+v err=%v", prepared, err)
	}
	registry, err := workspace.Decode(stageSnapshot(t, root).State)
	if err != nil || registry == nil {
		t.Fatalf("decode prepared Workspace: %v", err)
	}
	e, exists := registry.Execution(assignment, record.RuntimeID, record.BaselineGeneration)
	if !exists || e.AgentID != owner || e.Status != "ready" || e.AssignmentRecord == nil || *e.AssignmentRecord != record.Reference() || e.AssignmentRecordSHA256 == "" {
		t.Fatalf("prepared original Workspace does not bind the canonical Record: %+v", e)
	}
	contextView, err := registry.ReadDispatchContext(e)
	if err != nil || contextView.ResultDraft == nil || contextView.ResultPath != workspace.ResultDraftPath {
		t.Fatalf("read frozen workspace context/Result draft: result=%q err=%v", contextView.ResultPath, err)
	}
	assertion := "The author accurately reports the implementation and registered package-check outcome."
	oracle := "Review the delivered source evidence and the original captured WorkerCheck result."
	plan := contextView.PlanDraft
	plan["objective"] = "Implement the reviewed package TASK and report its actual frozen check result."
	plan["planned_paths"] = []any{"src/pkg/owned_check_test.go"}
	plan["assertion_checks"] = []any{map[string]any{
		"assertion": assertion, "oracle": oracle, "done_when_indexes": []any{0},
	}}
	plan["dependencies"] = []any{}
	plan["risks_blockers"] = []any{}
	stageJSON(t, e.Path, workspace.PlanDraftPath, plan)
	stageCLI(t, "runtime", "workspace", "begin", "--root", e.Path, "--assignment", assignment, "--agent", owner)

	// A real failing package test makes the actual registered command return a
	// failure even on hosts where Bubblewrap is available. The receipt itself is
	// still produced, sealed and captured only by the original runner path.
	failureSource := func(detail string) []byte {
		return []byte("package pkg\nimport \"testing\"\nfunc TestCapturedWorkerCheckFailure(t *testing.T) { t.Fatal(" + fmt.Sprintf("%q", detail) + ") }\n")
	}
	failurePath := filepath.Join(e.Path, "src/pkg/owned_check_test.go")
	proofRef := "src/pkg/result-proof.txt"
	proofPath := filepath.Join(e.Path, filepath.FromSlash(proofRef))
	if err := os.WriteFile(proofPath, []byte("The implementation is incomplete; inspect the source and captured failing check before closing this TASK.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failurePath, failureSource("first captured execution must fail"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstRun := runS6WorkspaceOwnedCheck(t, root, assignment, owner)
	if firstRun.code == 0 {
		t.Fatalf("the actual frozen Go check unexpectedly returned PASS; receipt output=%s stderr=%s", firstRun.stdout, firstRun.stderr)
	}
	firstWorkspaceState := stageSnapshot(t, root)
	firstRegistry, err := workspace.Decode(firstWorkspaceState.State)
	if err != nil || firstRegistry == nil {
		t.Fatalf("decode original receipt reception: %v", err)
	}
	e, exists = firstRegistry.Execution(assignment, record.RuntimeID, record.BaselineGeneration)
	if !exists || len(e.OwnedCheckReceipts) != 1 {
		t.Fatalf("original Workspace Writer did not accept the actual failed receipt: exists=%t execution=%+v", exists, e)
	}
	firstToolReceipt := e.OwnedCheckReceipts[0].Receipt
	if e.OwnedCheckReceipts[0].WorkerCheckIndex != 0 || firstToolReceipt.Path == "" || firstToolReceipt.Checkout != e.Path || firstToolReceipt.Binding.Responsibility != "owned_check" {
		t.Fatalf("received Workspace receipt lost its original tuple: %+v", e.OwnedCheckReceipts[0])
	}
	verifiedFirst, err := checkexec.VerifyToolReceipt(filepath.Join(root, ".claude/evidence/workspace-checks"), firstToolReceipt)
	if err != nil || verifiedFirst.Passed || verifiedFirst.Observation.Permissions == nil {
		t.Fatalf("original runner did not preserve an actual failed host fact: passed=%t observation=%+v err=%v", verifiedFirst.Passed, verifiedFirst.Observation, err)
	}
	firstRaw, err := os.ReadFile(firstToolReceipt.Path)
	if err != nil {
		t.Fatal(err)
	}
	var rawFirst struct {
		Status      string                            `json:"status"`
		SnapshotSHA string                            `json:"snapshot_sha256"`
		CheckIndex  int                               `json:"check_index"`
		Record      *assignmentrecord.RecordReference `json:"assignment_record"`
		RecordSHA   string                            `json:"assignment_record_sha256"`
		WorkerIndex int                               `json:"worker_check_index"`
		WorkerTuple map[string]any                    `json:"worker_check_snapshot"`
		Observation checkexec.Observation             `json:"profile_observation"`
	}
	if err := json.Unmarshal(firstRaw, &rawFirst); err != nil || rawFirst.Status != "fail" || rawFirst.CheckIndex != 0 || rawFirst.WorkerIndex != 0 || rawFirst.Record == nil || *rawFirst.Record != record.Reference() || rawFirst.RecordSHA == "" || rawFirst.SnapshotSHA == "" || rawFirst.WorkerTuple["owner_agent_id"] != owner {
		t.Fatalf("captured failed receipt does not match the current frozen Record/profile identity: raw=%+v err=%v", rawFirst, err)
	}
	if verifiedFirst.Observation.ProfileDigest != record.ClosingContract.WorkerChecks[0].Profile.Digest() || rawFirst.Observation.PermissionDigest != rawFirst.Observation.Permissions.Digest() {
		t.Fatalf("original profile or permission digest was not bound into the actual receipt: %+v", rawFirst.Observation)
	}
	if verifiedFirst.Observation.Permissions.Available && (!verifiedFirst.Observation.Permissions.Known() || firstRun.code == 0) {
		t.Fatal("an available host runner did not preserve its actual failing test result")
	}
	if !verifiedFirst.Observation.Permissions.Available && verifiedFirst.Observation.Permissions.Diagnostic == "" {
		t.Fatal("an unavailable host runner did not retain its original denial diagnostic")
	}

	fixture := s6CanonicalResultCLIFixture{
		root: root, sourceRef: proofRef, record: record, resultDraft: contextView.ResultDraft,
		assertion: assertion, oracle: oracle,
	}
	answer := func(messageID string) map[string]any {
		message := s6CanonicalCompletionMessage(fixture, messageID, "The implementation remains incomplete because the registered check produced an actual failure.")
		message["closing_assessment"] = map[string]any{
			"disposition": "incomplete",
			"conditions": []any{map[string]any{
				"done_when_index": 0, "judgment": "incomplete", "plan_assertion_indexes": []any{0},
				"rationale": "The original Workspace runner captured a failing result for the frozen package command; do not close this delivery yet.",
			}},
		}
		return message
	}
	firstMessagePath := ".claude/messages/s6-workspace-owned-first.json"
	stageJSON(t, root, firstMessagePath, answer("msg-s6-workspace-owned-first"))
	firstOutput := stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", firstMessagePath)
	first := s6DecodeCanonicalTaskCompleteOutput(t, firstOutput)
	if first.Acceptance.ResultRevision != 1 || first.Acceptance.ResultRef == "" || first.Acceptance.AnswerDigest == "" || first.Acceptance.Replayed || !first.Acceptance.Current {
		t.Fatalf("first Workspace-backed canonical Result receipt = %+v", first.Acceptance)
	}
	firstAccepted := s6ReadCLIResultEnvelope(t, root, first.Acceptance)
	if firstAccepted.AnswerDigest != first.Acceptance.AnswerDigest || len(firstAccepted.WorkerCheckFacts) != 1 || firstAccepted.WorkerCheckFacts[0].Status != "fail" ||
		firstAccepted.WorkerCheckFacts[0].ExecutionKind != "workspace" || firstAccepted.WorkerCheckFacts[0].OriginalReceipt == nil || firstAccepted.WorkerCheckFacts[0].Observation == nil ||
		firstAccepted.WorkerCheckFacts[0].OriginalReceipt.SHA256 != firstToolReceipt.SHA256 || firstAccepted.ToolClosureDigest == "" || assignmentresult.WorkerClosureComplete(record, firstAccepted) {
		t.Fatalf("canonical Result did not retain the current actual failed Workspace machine fact as fail: %+v", firstAccepted.WorkerCheckFacts)
	}
	s6AssertCanonicalBuilderTaskReceipt(t, fixture, first.Acceptance.ResultRef)

	beforeSameKeyState, beforeSameKeyJournal := s6CanonicalRuntimePair(t, root)
	sameKeyOutput := stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", firstMessagePath)
	sameKey := s6DecodeCanonicalTaskCompleteOutput(t, sameKeyOutput)
	if sameKey.Revision != first.Revision || sameKey.Acceptance.ResultRef != first.Acceptance.ResultRef || sameKey.Acceptance.ResultSHA256 != first.Acceptance.ResultSHA256 ||
		sameKey.Acceptance.AnswerDigest != first.Acceptance.AnswerDigest || !sameKey.Acceptance.Replayed || !sameKey.Acceptance.Current {
		t.Fatalf("same-key replay changed the accepted Result receipt: %+v", sameKey.Acceptance)
	}
	s6AssertCanonicalRuntimePair(t, root, beforeSameKeyState, beforeSameKeyJournal)

	aliasPath := ".claude/messages/s6-workspace-owned-alias.json"
	stageJSON(t, root, aliasPath, answer("msg-s6-workspace-owned-alias"))
	aliasOutput := stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", aliasPath)
	alias := s6DecodeCanonicalTaskCompleteOutput(t, aliasOutput)
	if alias.Revision != first.Revision+1 || alias.Acceptance.ResultRef != first.Acceptance.ResultRef || alias.Acceptance.ResultSHA256 != first.Acceptance.ResultSHA256 ||
		alias.Acceptance.AnswerDigest != first.Acceptance.AnswerDigest || alias.Acceptance.ResultRevision != 1 || !alias.Acceptance.Replayed || !alias.Acceptance.Current {
		t.Fatalf("new submission key with identical answer/closure was not bound as an alias: %+v", alias.Acceptance)
	}

	// Change an actual checked-tree input to force a fresh runner receipt. The
	// original profile/Record are unchanged; a second process capture must bind
	// the new source snapshot rather than reusing the earlier failed proof.
	if err := os.WriteFile(failurePath, failureSource("second captured execution has different source bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondRun := runS6WorkspaceOwnedCheck(t, root, assignment, owner)
	if secondRun.code == 0 {
		t.Fatalf("second actual frozen Go check unexpectedly returned PASS: %s", secondRun.stdout)
	}
	secondWorkspaceState := stageSnapshot(t, root)
	secondRegistry, err := workspace.Decode(secondWorkspaceState.State)
	if err != nil || secondRegistry == nil {
		t.Fatalf("decode second original receipt reception: %v", err)
	}
	secondExecution, exists := secondRegistry.Execution(assignment, record.RuntimeID, record.BaselineGeneration)
	if !exists || len(secondExecution.OwnedCheckReceipts) != 2 {
		t.Fatalf("second actual machine capture was not appended without eviction: exists=%t receipts=%+v", exists, secondExecution.OwnedCheckReceipts)
	}
	secondToolReceipt := secondExecution.OwnedCheckReceipts[1].Receipt
	if secondToolReceipt.SHA256 == firstToolReceipt.SHA256 {
		t.Fatal("different current source did not create a new original process-captured receipt")
	}
	verifiedSecond, err := checkexec.VerifyToolReceipt(filepath.Join(root, ".claude/evidence/workspace-checks"), secondToolReceipt)
	if err != nil || verifiedSecond.Passed {
		t.Fatalf("second original runner fact was not an actual failure: passed=%t err=%v", verifiedSecond.Passed, err)
	}
	secondMessagePath := ".claude/messages/s6-workspace-owned-second-machine-facts.json"
	stageJSON(t, root, secondMessagePath, answer("msg-s6-workspace-owned-second-machine-facts"))
	secondOutput := stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", secondMessagePath)
	second := s6DecodeCanonicalTaskCompleteOutput(t, secondOutput)
	if second.Revision != alias.Revision+1 || second.Acceptance.ResultRevision != 2 || second.Acceptance.AnswerDigest != first.Acceptance.AnswerDigest ||
		second.Acceptance.ResultRef == first.Acceptance.ResultRef || second.Acceptance.Replayed || !second.Acceptance.Current {
		t.Fatalf("same professional answer with new captured machine facts did not create Result revision 2: %+v", second.Acceptance)
	}
	secondAccepted := s6ReadCLIResultEnvelope(t, root, second.Acceptance)
	if len(secondAccepted.WorkerCheckFacts) != 1 || secondAccepted.WorkerCheckFacts[0].Status != "fail" || secondAccepted.WorkerCheckFacts[0].OriginalReceipt == nil ||
		secondAccepted.WorkerCheckFacts[0].OriginalReceipt.SHA256 != secondToolReceipt.SHA256 || secondAccepted.WorkerCheckFacts[0].SourceSnapshotSHA256 == firstAccepted.WorkerCheckFacts[0].SourceSnapshotSHA256 {
		t.Fatalf("revision 2 did not bind the latest captured source/receipt facts: %+v", secondAccepted.WorkerCheckFacts)
	}
	s6AssertCurrentResultRefs(t, second.State, record, second.Acceptance.ResultRef)
	s6AssertCanonicalBuilderTaskReceipt(t, fixture, second.Acceptance.ResultRef)

	// Replaying the earlier accepted key reads its immutable Result snapshots;
	// a later Worker checkout/source receipt cannot rebind historical proof.
	beforeHistoricalState, beforeHistoricalJournal := s6CanonicalRuntimePair(t, root)
	historicalOutput := stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", firstMessagePath)
	historical := s6DecodeCanonicalTaskCompleteOutput(t, historicalOutput)
	if historical.Revision != second.Revision || historical.Acceptance.ResultRef != first.Acceptance.ResultRef || historical.Acceptance.ResultRevision != 1 ||
		!historical.Acceptance.Replayed || historical.Acceptance.Current {
		t.Fatalf("historical Result replay changed current assignment receipt: %+v", historical.Acceptance)
	}
	s6AssertCanonicalRuntimePair(t, root, beforeHistoricalState, beforeHistoricalJournal)
	current, err := assignmentresult.LoadAccepted(historical.State, record, first.Acceptance.ResultRef, func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
	})
	if err != nil || current.WorkerCheckFacts[0].OriginalReceipt == nil || current.WorkerCheckFacts[0].OriginalReceipt.SHA256 != firstToolReceipt.SHA256 {
		t.Fatalf("historical replay rebound to the new live receipt: result=%+v err=%v", current.WorkerCheckFacts, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", mustJSONForS6(t, answer("msg-s6-workspace-owned-schema-check"))); err != nil {
		t.Fatalf("same professional answer shape stopped matching the canonical completion schema: %v", err)
	}
}

type s6WorkspaceOwnedRun struct {
	code           int
	stdout, stderr string
}

func runS6WorkspaceOwnedCheck(t *testing.T, root, assignmentID, owner string) s6WorkspaceOwnedRun {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "check", "--root", root, "--assignment", assignmentID, "--agent", owner, "--owned-check", "0"}, nil, &stdout, &stderr)
	return s6WorkspaceOwnedRun{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func mustJSONForS6(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
