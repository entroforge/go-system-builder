package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestCanonicalS6WorkspaceReworkRequiresNamedAssignmentRevision(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
	manifestBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestRef)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignmentRow := manifest["assignments"].([]any)[0].(map[string]any)
	assignmentRow["integration_check_mode"] = "post_merge"
	assignmentRow["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root,
		"--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)

	const owner = "agent-s6-native-context"
	const assignmentID = "assignment-s6-native-context"
	prepareOutput := stageCLI(t, "runtime", "workspace", "prepare", "--root", root,
		"--assignment", assignmentID, "--agent", owner, "--check-location", "main")
	var e workspace.Execution
	if err := json.Unmarshal([]byte(prepareOutput), &e); err != nil || e.Status != "ready" {
		t.Fatalf("decode original workspace prepare: execution=%+v err=%v", e, err)
	}
	snapshot := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, owner)
	if err != nil || !found {
		t.Fatalf("load original canonical Assignment Record: found=%t err=%v", found, err)
	}
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatalf("validate original canonical Assignment Record: %v", err)
	}
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original workspace registry: %v", err)
	}
	contextView, err := registry.ReadDispatchContext(e)
	if err != nil || contextView.PlanDraft == nil || contextView.ResultDraft == nil {
		t.Fatalf("read original frozen context: plan=%v result=%v err=%v", contextView.PlanDraft != nil, contextView.ResultDraft != nil, err)
	}

	assertion := "The reviewed package change meets the TASK's registered closing assertion."
	oracle := "Inspect the committed package source and its allowed evidence file before submitting the Result."
	plan := contextView.PlanDraft
	plan["objective"] = "Implement the registered TASK within its frozen package scope."
	plan["planned_paths"] = []any{"src/pkg/service.go"}
	plan["assertion_checks"] = []any{map[string]any{"assertion": assertion, "oracle": oracle}}
	plan["dependencies"] = []any{}
	plan["risks_blockers"] = []any{}
	stageJSON(t, e.Path, workspace.PlanDraftPath, plan)
	s6SaveQuestionnaireHook(t, root, e.Path, e.AgentID, filepath.Join(e.Path, workspace.PlanDraftPath))

	productRef := "src/pkg/service.go"
	product := []byte("package pkg\n\n// Value is the reviewed service result.\nfunc Value() string { return \"ready\" }\n")
	if err := os.WriteFile(filepath.Join(e.Path, filepath.FromSlash(productRef)), product, 0o600); err != nil {
		t.Fatal(err)
	}
	commitPath := filepath.Join(e.Path, filepath.FromSlash(workspace.CommitRequestPath))
	commitBytes, err := os.ReadFile(commitPath)
	if err != nil {
		t.Fatalf("read bootstrap commit request draft: %v", err)
	}
	var commitRequest workspace.CommitRequest
	if err := json.Unmarshal(commitBytes, &commitRequest); err != nil {
		t.Fatalf("decode bootstrap commit request draft: %v", err)
	}
	head, err := workspace.Git(context.Background(), e.Path, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if commitRequest.ExpectedHead != head || commitRequest.ExpectedHead != e.BaseCommit || commitRequest.Message != "" || len(commitRequest.Paths) != 0 {
		t.Fatalf("bootstrap commit request does not carry the current registered Worker HEAD scaffold: draft=%+v current=%s base=%s", commitRequest, head, e.BaseCommit)
	}
	commitRequest.Message = "Record the canonical S6 candidate"
	commitRequest.Paths = []string{productRef}
	stageJSON(t, e.Path, workspace.CommitRequestPath, commitRequest)
	stageCLI(t, "runtime", "workspace", "commit", "--root", e.Path,
		"--assignment", e.AssignmentID, "--agent", e.AgentID)

	fixture := s6CanonicalResultCLIFixture{
		root: root, sourceRef: productRef, record: record, resultDraft: contextView.ResultDraft,
		assertion: assertion, oracle: oracle,
	}
	completion := s6CanonicalCompletionMessage(fixture, "msg-s6-canonical-rework-result", "The reviewed package candidate is committed and ready for Main integration.")
	completionPath := filepath.Join(e.Path, workspace.ResultDraftPath)
	stageJSON(t, e.Path, workspace.ResultDraftPath, completion)
	output := s6SaveQuestionnaireHook(t, root, e.Path, e.AgentID, completionPath)
	accepted := s6DecodeCanonicalTaskCompleteOutput(t, output).Acceptance
	if accepted.ResultRef == "" || !accepted.Current {
		t.Fatalf("original Writer did not accept the current canonical Result: %+v", accepted)
	}
	beforeReplay := stageSnapshot(t, root).Revision
	replayed := s6DecodeCanonicalTaskCompleteOutput(t, s6SaveQuestionnaireHook(t, root, e.Path, e.AgentID, completionPath)).Acceptance
	if replayed.ResultRef != accepted.ResultRef || stageSnapshot(t, root).Revision != beforeReplay {
		t.Fatal("same saved Result repeated its domain reception")
	}
	resultBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(accepted.ResultRef)))
	if err != nil || workpacket.Digest(resultBytes) != accepted.ResultSHA256 {
		t.Fatalf("read original accepted Result: %v", err)
	}

	dirtyRef := "src/pkg/uncommitted-after-result.txt"
	dirtyBytes := []byte("preserve this real uncommitted integration blocker\n")
	if err := os.WriteFile(filepath.Join(e.Path, filepath.FromSlash(dirtyRef)), dirtyBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	var integrateOut, integrateErr bytes.Buffer
	integrateCode := cli.Run([]string{"runtime", "task-integrate", "--root", root,
		"--assignment-id", assignmentID, "--agent-id", owner}, nil, &integrateOut, &integrateErr)
	if integrateCode != 0 {
		t.Fatalf("original task-integrate command failed before recording the dirty checkpoint: code=%d stderr=%s stdout=%s", integrateCode, integrateErr.String(), integrateOut.String())
	}
	checkpointPath := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	checkpoint, found, err := integration.DefaultCheckpointStore().Load(checkpointPath)
	dirtyBlocker := false
	for _, blocker := range checkpoint.Blockers {
		if strings.Contains(strings.ToLower(blocker), "dirty") {
			dirtyBlocker = true
		}
	}
	if err != nil || !found || checkpoint.State != integration.StatePreserved || !dirtyBlocker {
		t.Fatalf("original task-integrate did not create a preserved dirty checkpoint: found=%t checkpoint=%+v err=%v stdout=%s stderr=%s", found, checkpoint, err, integrateOut.String(), integrateErr.String())
	}
	checkpointBytes, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, journalBefore := s6CanonicalRuntimePair(t, root)
	headBefore, err := workspace.Git(context.Background(), e.Path, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	statusBefore, err := workspace.Git(context.Background(), e.Path, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}

	var reworkOut, reworkErr bytes.Buffer
	reworkCode := cli.Run([]string{"runtime", "workspace", "rework", "--root", root,
		"--assignment", assignmentID, "--agent", owner, "--reason", "Correct the failed integration candidate"}, nil, &reworkOut, &reworkErr)
	recordRefJSON, err := json.Marshal(record.Reference())
	if err != nil {
		t.Fatal(err)
	}
	recordSHA, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if reworkCode == 0 || !strings.Contains(reworkErr.String(), "--revise-assignment") ||
		!strings.Contains(reworkErr.String(), "--revision-intent-key") ||
		!strings.Contains(reworkErr.String(), "--expected-record-ref '") ||
		!strings.Contains(reworkErr.String(), string(recordRefJSON)) ||
		!strings.Contains(reworkErr.String(), "--expected-record-sha256 "+recordSHA) {
		t.Fatalf("canonical same-Record rework was not rejected with the original named-revision action: code=%d stderr=%s stdout=%s", reworkCode, reworkErr.String(), reworkOut.String())
	}
	s6AssertCanonicalRuntimePair(t, root, stateBefore, journalBefore)
	if after, err := os.ReadFile(checkpointPath); err != nil || !bytes.Equal(after, checkpointBytes) {
		t.Fatalf("rejected rework changed the preserved checkpoint: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(accepted.ResultRef))); err != nil || !bytes.Equal(after, resultBytes) {
		t.Fatalf("rejected rework changed the immutable accepted Result: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(e.Path, filepath.FromSlash(dirtyRef))); err != nil || !bytes.Equal(after, dirtyBytes) {
		t.Fatalf("rejected rework changed the dirty Worker file: %v", err)
	}
	headAfter, err := workspace.Git(context.Background(), e.Path, "rev-parse", "HEAD")
	if err != nil || headAfter != headBefore {
		t.Fatalf("rejected rework changed the Worker branch head: before=%s after=%s err=%v", headBefore, headAfter, err)
	}
	statusAfter, err := workspace.Git(context.Background(), e.Path, "status", "--porcelain")
	if err != nil || statusAfter != statusBefore {
		t.Fatalf("rejected rework changed the dirty Worker tree: before=%q after=%q err=%v", statusBefore, statusAfter, err)
	}
	afterSnapshot := stageSnapshot(t, root)
	afterRegistry, err := workspace.Decode(afterSnapshot.State)
	if err != nil || afterRegistry == nil {
		t.Fatalf("decode unchanged current Workspace after rejected rework: %v", err)
	}
	afterExecution, exists := afterRegistry.Execution(assignmentID, record.RuntimeID, record.BaselineGeneration)
	if !exists || afterExecution.Status != "ready" || afterExecution.ReworkRef != "" || afterExecution.Path != e.Path || afterExecution.Branch != e.Branch {
		t.Fatalf("rejected rework changed current execution authority: %+v", afterExecution)
	}
}
