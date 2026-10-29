package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS6ClosingContractCLISeparatesOwnedCompletionFromMain(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
	manifestBytes, err := os.ReadFile(filepath.Join(root, manifestRef))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	item := manifest["assignments"].([]any)[0].(map[string]any)
	item["closing_contract"] = map[string]any{"contract": "s6-closing-v1", "worker_checks": []any{}}
	item["integration_check_mode"] = "post_merge"
	item["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	snapshot := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, "agent-s6-native-context")
	if err != nil || !found || record.ClosingContract == nil || record.ClosingContract.WorkerChecks == nil || len(record.ClosingContract.WorkerChecks) != 0 {
		t.Fatalf("original Writer did not freeze explicit empty owned-check contract: record=%+v found=%t err=%v", record, found, err)
	}
	output := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID)
	var binding struct {
		Context agentcontext.Reference `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(output), &binding); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(binding.Context.Path)))
	if err != nil || workpacket.Digest(body) != binding.Context.SHA256 {
		t.Fatalf("bound context: %v", err)
	}
	var view struct {
		PlanPath    string         `json:"plan_draft_path"`
		ResultPath  string         `json:"result_path"`
		ResultDraft map[string]any `json:"result_draft"`
	}
	if err := json.Unmarshal(body, &view); err != nil || view.PlanPath == "" || view.ResultPath == "" {
		t.Fatalf("context transport paths: %+v %v", view, err)
	}
	if answer, present := view.ResultDraft["closing_assessment"]; !present || answer != nil {
		t.Fatal("closing judgment must be an unanswered professional question")
	}
	planBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(view.PlanPath)))
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	assertion, oracle := "The owned TASK delivery meets its frozen closing condition.", "Inspect the delivered implementation and its owned evidence."
	plan["objective"] = "Deliver the registered TASK within its reviewed package scope."
	plan["planned_paths"] = []any{"src/pkg/output.go"}
	plan["assertion_checks"] = []any{map[string]any{"assertion": assertion, "oracle": oracle, "done_when_indexes": []any{0}}}
	plan["dependencies"], plan["risks_blockers"] = []any{}, []any{}
	// A structurally thin PLAN still needs an explicit mapping to all of
	// this Record's professional closing predicates before activation.
	assertionEntry := plan["assertion_checks"].([]any)[0].(map[string]any)
	delete(assertionEntry, "done_when_indexes")
	stageJSON(t, root, view.PlanPath, plan)
	stateBeforePlan, journalBeforePlan := s6CanonicalRuntimePair(t, root)
	var rejectedOut, rejectedErr strings.Builder
	code := cli.Run([]string{"runtime", "agent-begin", "--root", root, "--agent-id", record.OwnerAgentID, "--plan", view.PlanPath}, nil, &rejectedOut, &rejectedErr)
	if code == 0 {
		t.Fatal("unmapped closing PLAN activated the original Assignment")
	}
	s6AssertCanonicalRuntimePair(t, root, stateBeforePlan, journalBeforePlan)
	assertionEntry["done_when_indexes"] = []any{0}
	stageJSON(t, root, view.PlanPath, plan)
	s6SaveQuestionnaireHook(t, root, root, record.OwnerAgentID, filepath.Join(root, filepath.FromSlash(view.PlanPath)))
	proofRef := "src/pkg/closing-proof.txt"
	s6WriteRecordContextFile(t, root, proofRef, []byte("original owned delivery evidence\n"))
	resultBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(view.ResultPath)))
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := json.Unmarshal(resultBytes, &message); err != nil {
		t.Fatal(err)
	}
	rows, ok := message["assertion_evidence"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("accepted PLAN assertion/oracle was not projected into the exact Result draft: %v", message["assertion_evidence"])
	}
	row, ok := rows[0].(map[string]any)
	refs, refsOK := row["evidence_refs"].([]any)
	if !ok || !refsOK || row["assertion"] != assertion || row["oracle"] != oracle || len(refs) != 0 {
		t.Fatalf("Result draft did not retain the accepted PLAN mapping with empty author evidence refs: %+v", rows)
	}
	assessmentDraft, ok := message["closing_assessment"].(map[string]any)
	if !ok {
		t.Fatalf("Result draft did not project the registered closing assessment: %v", message["closing_assessment"])
	}
	if disposition, exists := assessmentDraft["disposition"]; !exists || disposition != nil {
		t.Fatalf("Result scaffold must show the unanswered professional disposition: %+v", assessmentDraft)
	}
	conditions, conditionsOK := assessmentDraft["conditions"].([]any)
	if !conditionsOK || len(conditions) != 1 {
		t.Fatalf("registered closing condition was not projected into the exact Result draft: %v", message["closing_assessment"])
	}
	conditionDraft, ok := conditions[0].(map[string]any)
	indexes, indexesOK := conditionDraft["plan_assertion_indexes"].([]any)
	if !ok || !indexesOK || conditionDraft["done_when_index"] != float64(0) || len(indexes) != 1 || indexes[0] != float64(0) {
		t.Fatalf("Result draft omitted the accepted PLAN closing mapping: %+v", conditions)
	}
	if judgment, exists := conditionDraft["judgment"]; !exists || judgment != nil {
		t.Fatalf("Result scaffold must leave the professional condition judgment blank: %+v", conditionDraft)
	}
	if rationale, exists := conditionDraft["rationale"]; !exists || rationale != nil {
		t.Fatalf("Result scaffold must leave the professional condition rationale blank: %+v", conditionDraft)
	}
	message["summary"] = "Delivered the owned implementation with its closing evidence."
	message["assertion_evidence"] = []any{map[string]any{"assertion": assertion, "oracle": oracle, "evidence_refs": []any{proofRef}}}
	message["risks_blockers"], message["scope_deviations"] = []any{}, []any{}
	assessment := assessmentDraft
	assessment["disposition"] = "complete"
	conditionDraft["judgment"] = "satisfied"
	conditionDraft["rationale"] = "The delivered implementation and referenced evidence satisfy the owned condition."
	message["closing_assessment"] = assessment
	stageJSON(t, root, view.ResultPath, message)
	fixture := s6CanonicalResultCLIFixture{root: root, record: record}
	firstOutput, firstFeedback := s6SaveQuestionnaireHookFeedback(t, root, root, record.OwnerAgentID, filepath.Join(root, filepath.FromSlash(view.ResultPath)))
	first := s6DecodeCanonicalTaskCompleteOutput(t, firstOutput)
	if first.Acceptance.ResultRevision != 1 {
		t.Fatalf("first closing Result: %+v", first.Acceptance)
	}
	if !strings.Contains(firstFeedback, "no recorded professional or WorkerCheck gaps") || !strings.Contains(firstFeedback, "waiting for Main's original integration") || !strings.Contains(firstFeedback, view.ResultPath) {
		t.Fatalf("successful Result save did not show the accepted answer and Main waiting responsibility: %s", firstFeedback)
	}
	assertS6ClosingReceipt(t, fixture, true, "complete")
	if code, progress := stageHookCode(t, root, root, record.OwnerAgentID, "PreToolUse", filepath.Join(root, filepath.FromSlash(view.ResultPath))); code != 0 || !strings.Contains(progress, "no recorded professional or WorkerCheck gaps") || !strings.Contains(progress, "waiting for Main's original integration") || !strings.Contains(progress, view.ResultPath) {
		t.Fatalf("complete receipt did not give Main the exact integration status: code=%d context=%s", code, progress)
	}
	// Main must see the original failed integration, rather than infer that
	// its checks have not run yet. This observation preserves the checkpoint.
	checkpointPath := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	stageJSON(t, root, assignmentrecord.IntegrationCheckpointPath("", record.Reference()), map[string]any{"state": "preserved", "failure_reason": "required check failed: go vet ./..."})
	checkpointBefore, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	if code, progress := stageHookCode(t, root, root, "Main", "PreToolUse", filepath.Join(root, "README.md")); code != 0 || !strings.Contains(progress, "required check failed: go vet ./...") || !strings.Contains(progress, checkpointPath) || !strings.Contains(progress, "Do not resubmit the accepted Result") || !strings.Contains(progress, "original retry conditions") {
		t.Fatalf("Main did not receive the recorded integration failure: code=%d context=%s", code, progress)
	}
	checkpointAfter, err := os.ReadFile(checkpointPath)
	if err != nil || !bytes.Equal(checkpointBefore, checkpointAfter) {
		t.Fatalf("failure reminder changed the preserved checkpoint: %v", err)
	}
	if err := os.Remove(checkpointPath); err != nil {
		t.Fatal(err)
	}
	state, journal := s6CanonicalRuntimePair(t, root)
	acceptedDraft, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(view.ResultPath)))
	if err != nil {
		t.Fatal(err)
	}
	replayedOutput := s6SaveQuestionnaireHook(t, root, root, record.OwnerAgentID, filepath.Join(root, filepath.FromSlash(view.ResultPath)))
	replayed := s6DecodeCanonicalTaskCompleteOutput(t, replayedOutput)
	if !replayed.Acceptance.Replayed || replayed.Acceptance.ResultRef != first.Acceptance.ResultRef {
		t.Fatalf("same empty-check closing answer did not replay its accepted Result: first=%+v replay=%+v", first.Acceptance, replayed.Acceptance)
	}
	afterReplayDraft, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(view.ResultPath)))
	if err != nil || !bytes.Equal(acceptedDraft, afterReplayDraft) {
		t.Fatalf("accepted empty-check Result replay rewrote machine bytes: read err=%v", err)
	}
	s6AssertCanonicalRuntimePair(t, root, state, journal)

	// A legitimate blocked answer is accepted as the next Result, without
	// retaining the prior complete professional projection.
	message["message_id"] = "msg-s6-closing-blocked-2"
	message["summary"] = "New evidence exposes an unresolved owned delivery blocker."
	message["risks_blockers"] = []any{"The owned closing assertion needs a revision."}
	assessment["disposition"] = "blocked"
	condition := assessment["conditions"].([]any)[0].(map[string]any)
	condition["judgment"], condition["rationale"] = "blocked", "The new observation blocks the owned closing predicate."
	stageJSON(t, root, view.ResultPath, message)
	second := s6DecodeCanonicalTaskCompleteOutput(t, stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", record.OwnerAgentID, "--message", view.ResultPath))
	if second.Acceptance.ResultRevision != 2 || second.Acceptance.AnswerDigest == first.Acceptance.AnswerDigest {
		t.Fatalf("blocked judgment must create a distinct accepted Result: %+v", second.Acceptance)
	}
	assertS6ClosingReceipt(t, fixture, false, "incomplete")
	if code, progress := stageHookCode(t, root, root, record.OwnerAgentID, "PreToolUse", filepath.Join(root, filepath.FromSlash(view.ResultPath))); code != 0 || !strings.Contains(progress, "recorded unfinished items") || !strings.Contains(progress, "DoneWhen 0 is blocked") || !strings.Contains(progress, view.ResultPath) || strings.Contains(progress, "waiting for Main's original integration") {
		t.Fatalf("incomplete receipt did not identify the original missing condition: code=%d context=%s", code, progress)
	}

	// A professional `complete` disposition cannot override an authored risk;
	// the read-only Builder receipt remains the source of completion status.
	message["message_id"], message["correlation_id"] = "msg-s6-closing-risk-3", "corr-s6-closing-risk-3"
	message["summary"] = "The implementation is present, with one unresolved delivery risk."
	message["risks_blockers"] = []any{"The registered output has not been independently confirmed."}
	assessment["disposition"] = "complete"
	condition["judgment"], condition["rationale"] = "satisfied", "The referenced implementation evidence supports the condition, subject to the stated risk."
	stageJSON(t, root, view.ResultPath, message)
	thirdOutput, thirdFeedback := s6SaveQuestionnaireHookFeedback(t, root, root, record.OwnerAgentID, filepath.Join(root, filepath.FromSlash(view.ResultPath)))
	third := s6DecodeCanonicalTaskCompleteOutput(t, thirdOutput)
	if third.Acceptance.ResultRevision != 3 || third.Acceptance.AnswerDigest == second.Acceptance.AnswerDigest {
		t.Fatalf("complete disposition with a risk must be a distinct accepted Result: %+v", third.Acceptance)
	}
	if !strings.Contains(thirdFeedback, "recorded unfinished items") || !strings.Contains(thirdFeedback, "risk/blocker:") || !strings.Contains(thirdFeedback, "not been independently confirmed") || !strings.Contains(thirdFeedback, view.ResultPath) || !strings.Contains(thirdFeedback, "Main owns the original source rework/revision decision") || strings.Contains(thirdFeedback, "waiting for Main's original integration") {
		t.Fatalf("successful Result save told Main to integrate despite its incomplete receipt: %s", thirdFeedback)
	}
	assertS6ClosingReceipt(t, fixture, false, "incomplete")
	if code, progress := stageHookCode(t, root, root, record.OwnerAgentID, "PreToolUse", filepath.Join(root, filepath.FromSlash(view.ResultPath))); code != 0 || !strings.Contains(progress, "recorded unfinished items") || !strings.Contains(progress, "risk/blocker:") || !strings.Contains(progress, "not been independently confirmed") || !strings.Contains(progress, view.ResultPath) || strings.Contains(progress, "waiting for Main's original integration") || strings.Contains(progress, "Worker responsibility is complete") {
		t.Fatalf("receipt ignored risk because the professional disposition said complete: code=%d context=%s", code, progress)
	}

	// Missing professional judgment fails before the original Writer can
	// register a fourth Result, even though the rest of the answer is present.
	message["message_id"] = "msg-s6-closing-missing-4"
	delete(message, "closing_assessment")
	stageJSON(t, root, view.ResultPath, message)
	state, journal = s6CanonicalRuntimePair(t, root)
	resultCode, _, stderr := s6RunCanonicalTaskComplete(fixture, view.ResultPath)
	if resultCode == 0 || !strings.Contains(stderr, "closing") {
		t.Fatalf("missing assessment result: code=%d stderr=%s", resultCode, stderr)
	}
	s6AssertCanonicalRuntimePair(t, root, state, journal)
}

func assertS6ClosingReceipt(t *testing.T, fixture s6CanonicalResultCLIFixture, complete bool, professional string) {
	t.Helper()
	output := stageCLI(t, "runtime", "task-receipt", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--assignment-id", fixture.record.AssignmentID)
	var receipt qualitygate.BuilderReceipt
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Accepted || receipt.WorkComplete != complete || receipt.Integrated || receipt.ProfessionalStatus != professional {
		t.Fatalf("owned/Main receipt = %+v", receipt)
	}
	waitingMain := false
	for _, waiting := range receipt.WaitingOn {
		if waiting.Owner == "main" && waiting.Reason == "integration_pending" {
			waitingMain = true
		}
	}
	if !waitingMain {
		t.Fatalf("owned closing must preserve the original Main consumer: %+v", receipt)
	}
}
