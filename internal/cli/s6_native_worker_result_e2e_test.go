package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This follows the original native check capture through the original
// CompleteTask Writer. Every status is read from the runner's sealed bytes;
// the test never fabricates a passing receipt.
func TestNativeWorkerCheckResultPersistsDurableFactsAndBindsAnswerAndSource(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const (
		manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
		owner       = "agent-s6-native-context"
		worktreeRel = ".worktrees/native-result-owner"
		branch      = "work/native-result-owner"
		proofRef    = "src/pkg/result-proof.txt"
	)
	worktree := filepath.Join(root, filepath.FromSlash(worktreeRel))
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", branch, worktree, "HEAD"); err != nil {
		t.Fatal(err)
	}
	proofBytes := []byte("The original TASK delivery evidence.\n")
	if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(proofRef)), proofBytes, 0o644); err != nil {
		t.Fatal(err)
	}

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
	row["worktree_path"], row["branch"], row["target_branch"] = worktreeRel, branch, "test-development"
	mainProfile := row["check_profiles"].([]any)[0].(map[string]any)
	mainProfile["location"] = "main"
	workerProfile := map[string]any{
		"id": "profile-s6-native-result", "version": "v1.0.0", "check_index": 0,
		"command": "go test ./src/pkg", "location": "worker", "timeout_seconds": 30,
		"environment_policy": "declared_only", "environment": []any{},
		"dependencies": []any{
			map[string]any{"path": "src/pkg/input.go", "sha256": workpacket.Digest([]byte("package pkg\n"))},
		},
		"fixtures": []any{
			map[string]any{"path": "testdata/input.txt", "sha256": workpacket.Digest([]byte("registered profile input\n"))},
		},
		"input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
	}
	row["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{map[string]any{
		"index": 0, "command": "go test ./src/pkg", "profile": workerProfile,
	}}}
	row["integration_check_mode"] = "post_merge"
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	registered := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(registered.State, owner)
	if err != nil || !found || record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) != 1 {
		t.Fatalf("Register did not freeze the native WorkerCheck: found=%t err=%v", found, err)
	}

	contextOutput := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", owner)
	var contextBinding struct {
		Context agentcontext.Reference `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(contextOutput), &contextBinding); err != nil || contextBinding.Context.Path == "" {
		t.Fatalf("decode fixed native context binding: %+v err=%v", contextBinding, err)
	}
	contextBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextBinding.Context.Path)))
	if err != nil || workpacket.Digest(contextBytes) != contextBinding.Context.SHA256 {
		t.Fatalf("fixed native context does not match its bound bytes: %v", err)
	}
	var contextView struct {
		PlanPath   string         `json:"plan_draft_path"`
		Plan       map[string]any `json:"plan_draft"`
		ResultPath string         `json:"result_path"`
		Result     map[string]any `json:"result_draft"`
	}
	if err := json.Unmarshal(contextBytes, &contextView); err != nil || contextView.PlanPath == "" || contextView.ResultPath == "" || contextView.Result == nil {
		t.Fatalf("decode current native PLAN/Result scaffold: %+v err=%v", contextView, err)
	}
	assertion := "The reviewed package TASK passes its frozen WorkerCheck."
	oracle := "Run the exact go test command in the original isolated owner runner and inspect its process-captured result."
	contextView.Plan["objective"] = "Implement the reviewed package TASK within its frozen source scope."
	contextView.Plan["planned_paths"] = []any{"src/pkg/input.go"}
	contextView.Plan["assertion_checks"] = []any{map[string]any{
		"assertion": assertion, "oracle": oracle, "done_when_indexes": []any{0},
	}}
	contextView.Plan["dependencies"], contextView.Plan["risks_blockers"] = []any{}, []any{}
	stageJSON(t, root, contextView.PlanPath, contextView.Plan)
	stageCLI(t, "runtime", "agent-begin", "--root", root, "--agent-id", owner, "--plan", contextView.PlanPath)

	var checkOut, checkErr bytes.Buffer
	checkCode := cli.Run([]string{"runtime", "assignment-context", "check", "--root", root, "--agent-id", owner, "--check-index", "0"}, nil, &checkOut, &checkErr)
	stateAfterCheck := stageSnapshot(t, root)
	var wrappers []assignment.NativeOwnedCheckReceipt
	for _, raw := range stateAfterCheck.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		encoded, _ := json.Marshal(row["owned_check_receipts"])
		if err := json.Unmarshal(encoded, &wrappers); err != nil {
			t.Fatal(err)
		}
	}
	if len(wrappers) != 1 || wrappers[0].ToolReceipt.Path == "" {
		t.Fatalf("original native Writer did not persist a real captured runner receipt: exit=%d stdout=%s stderr=%s wrappers=%+v", checkCode, checkOut.String(), checkErr.String(), wrappers)
	}
	wrapper := wrappers[0]
	verified, err := checkexec.VerifyToolReceipt(filepath.Dir(wrapper.ToolReceipt.Path), wrapper.ToolReceipt)
	if err != nil {
		t.Fatalf("verify the original runner's captured receipt: %v", err)
	}
	var rawReceipt struct {
		Status string `json:"status"`
	}
	rawBytes, err := os.ReadFile(wrapper.ToolReceipt.Path)
	if err != nil || json.Unmarshal(rawBytes, &rawReceipt) != nil || (rawReceipt.Status != "pass" && rawReceipt.Status != "fail") {
		t.Fatalf("captured runner receipt has no actual pass/fail status: status=%q err=%v", rawReceipt.Status, err)
	}
	if verified.Passed != (rawReceipt.Status == "pass") || verified.Passed != (checkCode == 0) {
		t.Fatalf("CLI/runner status was not derived from the actual receipt: exit=%d status=%q verified=%t stderr=%s", checkCode, rawReceipt.Status, verified.Passed, checkErr.String())
	}
	t.Logf("actual host isolated WorkerCheck: status=%s exit=%d receipt=%s", rawReceipt.Status, checkCode, wrapper.ToolReceipt.Path)

	proofPath := filepath.Join(root, filepath.FromSlash(contextView.ResultPath))
	resultDraftBytes, err := os.ReadFile(proofPath)
	if err != nil {
		t.Fatal(err)
	}
	var resultDraft map[string]any
	if err := json.Unmarshal(resultDraftBytes, &resultDraft); err != nil {
		t.Fatal(err)
	}
	resultFixture := s6CanonicalResultCLIFixture{
		root: root, sourceRef: proofRef, record: record, resultDraft: resultDraft,
		assertion: assertion, oracle: oracle,
	}
	message := s6CanonicalCompletionMessage(resultFixture, "msg-native-worker-result-first", "I delivered the scoped package implementation and its original check evidence.")
	conditionJudgment, disposition := "satisfied", "complete"
	if rawReceipt.Status != "pass" {
		conditionJudgment, disposition = "blocked", "blocked"
		message["risks_blockers"] = []string{"The frozen WorkerCheck returned an actual failure receipt in this environment."}
	}
	message["closing_assessment"] = map[string]any{"disposition": disposition, "conditions": []any{map[string]any{
		"done_when_index": 0, "judgment": conditionJudgment, "plan_assertion_indexes": []any{0},
		"rationale": "This owner assessment follows the exact process-captured WorkerCheck status stored with the answer.",
	}}}
	firstPath := ".claude/messages/native-worker-result-first.json"
	stageJSON(t, root, firstPath, message)
	first := s6DecodeCanonicalTaskCompleteOutput(t, stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", firstPath))
	if first.Acceptance.ResultRef == "" || first.Acceptance.ResultRevision != 1 || !first.Acceptance.Current {
		t.Fatalf("CompleteTask did not accept the canonical professional Result: %+v", first.Acceptance)
	}
	firstAccepted := s6ReadCLIResultEnvelope(t, root, first.Acceptance)
	if firstAccepted.Conclusion != "submitted" || len(firstAccepted.WorkerCheckFacts) != 1 || firstAccepted.WorkerCheckFacts[0].Status != rawReceipt.Status || firstAccepted.ToolClosureDigest == "" {
		t.Fatalf("CompleteTask did not preserve the actual captured Worker fact: %+v", firstAccepted)
	}
	firstFact := firstAccepted.WorkerCheckFacts[0]
	if firstFact.OriginalReceipt == nil || firstFact.OriginalReceipt.Path != wrapper.ToolReceipt.Path || firstFact.ReceiptArtifact == nil || firstFact.LogArtifact == nil || firstFact.SourceSnapshotSHA256 == "" {
		t.Fatalf("accepted Result lost original native provenance/artifact references: %+v", firstFact)
	}
	read := func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
	}
	accepted, err := assignmentresult.LoadAccepted(first.State, record, first.Acceptance.ResultRef, read)
	if err != nil {
		t.Fatalf("original Result did not validate its durable runner evidence: %v", err)
	}
	for _, artifact := range []*assignmentresult.ResolvedEvidence{firstFact.ReceiptArtifact, firstFact.LogArtifact} {
		body, err := read(artifact.Path)
		if err != nil || workpacket.Digest(body) != artifact.SHA256 {
			t.Fatalf("durable Worker fact artifact is missing or changed: %+v err=%v", artifact, err)
		}
	}
	complete := assignmentresult.WorkerClosureComplete(record, accepted)
	if complete != (rawReceipt.Status == "pass") {
		t.Fatalf("work closure changed the process result into a different judgment: complete=%t actual=%s", complete, rawReceipt.Status)
	}
	if rawReceipt.Status == "fail" {
		if code, progress := stageHookCode(t, root, root, owner, "PreToolUse", filepath.Join(root, filepath.FromSlash(contextView.ResultPath))); code != 0 || !strings.Contains(progress, "recorded unfinished items") || !strings.Contains(progress, "WorkerCheck 0 is fail") || strings.Contains(progress, "Worker responsibility is complete") {
			t.Fatalf("Main progress ignored the original failed WorkerCheck receipt: code=%d context=%s", code, progress)
		}
	}

	// Exact key replay consumes the accepted durable Result and does not run
	// checks or write a new Runtime revision.
	revisionAfterFirst := stageSnapshot(t, root).Revision
	replayed := s6DecodeCanonicalTaskCompleteOutput(t, stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", firstPath))
	if !replayed.Acceptance.Replayed || replayed.Acceptance.ResultRef != first.Acceptance.ResultRef || stageSnapshot(t, root).Revision != revisionAfterFirst {
		t.Fatalf("exact Result key replay changed its original acceptance: first=%+v replay=%+v", first.Acceptance, replayed.Acceptance)
	}

	// A fresh key with the same Answer and unchanged current runner receipt is
	// an alias to the original immutable Result.
	aliasMessage := cloneJSONMap(message)
	aliasMessage["message_id"] = "msg-native-worker-result-alias"
	aliasMessage["correlation_id"] = "corr-native-worker-result-alias"
	aliasPath := ".claude/messages/native-worker-result-alias.json"
	stageJSON(t, root, aliasPath, aliasMessage)
	aliased := s6DecodeCanonicalTaskCompleteOutput(t, stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", aliasPath))
	if !aliased.Acceptance.Replayed || aliased.Acceptance.ResultRef != first.Acceptance.ResultRef || aliased.Acceptance.AnswerDigest != first.Acceptance.AnswerDigest || aliased.Acceptance.ResultRevision != 1 {
		t.Fatalf("same Answer+durable closure did not bind as a Result alias: %+v", aliased.Acceptance)
	}

	// Change an actual allowed source and run the original isolated Worker
	// adapter again. The fresh observation must invalidate the old tool closure.
	changedProof := []byte("The source bytes changed after the first Result.\n")
	if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(proofRef)), changedProof, 0o644); err != nil {
		t.Fatal(err)
	}
	var rerunOut, rerunErr bytes.Buffer
	rerunCode := cli.Run([]string{"runtime", "assignment-context", "check", "--root", root, "--agent-id", owner, "--check-index", "0"}, nil, &rerunOut, &rerunErr)
	stateAfterRerun := stageSnapshot(t, root)
	wrappers = nil
	for _, raw := range stateAfterRerun.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		encoded, _ := json.Marshal(row["owned_check_receipts"])
		if err := json.Unmarshal(encoded, &wrappers); err != nil {
			t.Fatal(err)
		}
	}
	if len(wrappers) != 2 {
		t.Fatalf("fresh source check did not append its own original receipt: count=%d exit=%d stdout=%s stderr=%s", len(wrappers), rerunCode, rerunOut.String(), rerunErr.String())
	}
	newVerification, err := checkexec.VerifyToolReceipt(filepath.Dir(wrappers[1].ToolReceipt.Path), wrappers[1].ToolReceipt)
	if err != nil || newVerification.Passed != (rerunCode == 0) {
		t.Fatalf("rerun result was not the actual captured host result: verified=%+v exit=%d err=%v stderr=%s", newVerification, rerunCode, err, rerunErr.String())
	}
	var secondRawReceipt struct {
		Status string `json:"status"`
	}
	secondRawBytes, err := os.ReadFile(wrappers[1].ToolReceipt.Path)
	if err != nil || json.Unmarshal(secondRawBytes, &secondRawReceipt) != nil || (secondRawReceipt.Status != "pass" && secondRawReceipt.Status != "fail") {
		t.Fatalf("fresh captured receipt has no actual pass/fail status: status=%q err=%v", secondRawReceipt.Status, err)
	}
	if newVerification.Passed != (secondRawReceipt.Status == "pass") {
		t.Fatalf("fresh captured receipt status differs from its verified runner result: status=%q verified=%+v", secondRawReceipt.Status, newVerification)
	}
	t.Logf("actual host WorkerCheck after source change: status=%s exit=%d receipt=%s", secondRawReceipt.Status, rerunCode, wrappers[1].ToolReceipt.Path)

	secondMessage := cloneJSONMap(message)
	secondMessage["message_id"] = "msg-native-worker-result-new-source"
	secondMessage["correlation_id"] = "corr-native-worker-result-new-source"
	secondJudgment, secondDisposition := "satisfied", "complete"
	if secondRawReceipt.Status != "pass" {
		secondJudgment, secondDisposition = "blocked", "blocked"
		secondMessage["risks_blockers"] = []string{"The frozen WorkerCheck returned an actual failure receipt in this environment."}
	} else {
		delete(secondMessage, "risks_blockers")
	}
	secondMessage["closing_assessment"] = map[string]any{"disposition": secondDisposition, "conditions": []any{map[string]any{
		"done_when_index": 0, "judgment": secondJudgment, "plan_assertion_indexes": []any{0},
		"rationale": "This owner assessment follows the exact process-captured WorkerCheck status stored with the answer.",
	}}}
	secondPath := ".claude/messages/native-worker-result-new-source.json"
	stageJSON(t, root, secondPath, secondMessage)
	second := s6DecodeCanonicalTaskCompleteOutput(t, stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", secondPath))
	if second.Acceptance.ResultRef == first.Acceptance.ResultRef || second.Acceptance.ResultRevision != 2 || second.Acceptance.Replayed || !second.Acceptance.Current {
		t.Fatalf("new source/closure was incorrectly aliased to stale Result revision 1: %+v", second.Acceptance)
	}
	secondAccepted := s6ReadCLIResultEnvelope(t, root, second.Acceptance)
	if len(secondAccepted.WorkerCheckFacts) != 1 || secondAccepted.WorkerCheckFacts[0].Status != secondRawReceipt.Status || secondAccepted.WorkerCheckFacts[0].SourceSnapshotSHA256 == firstFact.SourceSnapshotSHA256 || secondAccepted.ToolClosureDigest == firstAccepted.ToolClosureDigest {
		t.Fatalf("new Result did not bind the changed Worker source closure: old=%+v new=%+v", firstFact, secondAccepted.WorkerCheckFacts)
	}
	secondDurable, err := assignmentresult.LoadAccepted(second.State, record, second.Acceptance.ResultRef, read)
	if err != nil {
		t.Fatalf("new Result did not validate its durable Worker evidence: %v", err)
	}
	if assignmentresult.WorkerClosureComplete(record, secondDurable) != (secondRawReceipt.Status == "pass") {
		t.Fatalf("new Result closure differs from the fresh actual receipt status %q", secondRawReceipt.Status)
	}
	var proofEvidence *assignmentresult.ResolvedEvidence
	for i := range secondAccepted.ResolvedEvidenceRefs {
		if secondAccepted.ResolvedEvidenceRefs[i].Ref == proofRef {
			proofEvidence = &secondAccepted.ResolvedEvidenceRefs[i]
			break
		}
	}
	if proofEvidence == nil || proofEvidence.SHA256 != workpacket.Digest(changedProof) {
		t.Fatalf("new Result did not snapshot the changed professional evidence bytes: %+v", secondAccepted.ResolvedEvidenceRefs)
	}

	// If the actual runner status stayed the same, the authored professional
	// Answer digest also stays the same while its frozen source closure changes.
	if rawReceipt.Status == secondAccepted.WorkerCheckFacts[0].Status {
		if first.Acceptance.AnswerDigest != second.Acceptance.AnswerDigest {
			t.Fatalf("same authored answer changed digest across source-only revision: %s vs %s", first.Acceptance.AnswerDigest, second.Acceptance.AnswerDigest)
		}
	}
	if !strings.HasPrefix(secondAccepted.WorkerCheckFacts[0].ContextRef, ".claude/assignment-context/native/") || !reflect.DeepEqual(secondAccepted.WorkerCheckFacts[0].Record, record.Reference()) {
		t.Fatalf("fresh Worker Result lost its current fixed native context/Record: %+v", secondAccepted.WorkerCheckFacts[0])
	}
	secondFact := secondAccepted.WorkerCheckFacts[0]
	if secondFact.CurrentSourceSnapshotSHA256 == "" || secondFact.CurrentSourceSnapshotSHA256 != secondFact.SourceSnapshotSHA256 {
		t.Fatalf("fresh check did not bind its current source snapshot: %+v", secondFact)
	}

	// Change the same non-frozen professional evidence source twice without
	// rerunning the check. Each Result must retain the latest stale source
	// digest in its closure identity, rather than aliasing the old receipt.
	previousCurrentSnapshot := secondFact.CurrentSourceSnapshotSHA256
	previousClosureDigest := secondAccepted.ToolClosureDigest
	latestState := second.State
	staleProofs := [][]byte{
		[]byte("The professional evidence changed again without another check.\n"),
		[]byte("The professional evidence changed once more without another check.\n"),
	}
	for i, staleProof := range staleProofs {
		if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(proofRef)), staleProof, 0o644); err != nil {
			t.Fatal(err)
		}
		staleMessage := cloneJSONMap(secondMessage)
		staleMessage["message_id"] = []string{"msg-native-worker-result-stale-3", "msg-native-worker-result-stale-4"}[i]
		staleMessage["correlation_id"] = []string{"corr-native-worker-result-stale-3", "corr-native-worker-result-stale-4"}[i]
		stalePath := []string{".claude/messages/native-worker-result-stale-3.json", ".claude/messages/native-worker-result-stale-4.json"}[i]
		stageJSON(t, root, stalePath, staleMessage)
		stale := s6DecodeCanonicalTaskCompleteOutput(t, stageCLI(t, "runtime", "task-complete", "--root", root, "--agent-id", owner, "--message", stalePath))
		wantRevision := 3 + i
		if stale.Acceptance.ResultRef == second.Acceptance.ResultRef || stale.Acceptance.ResultRevision != wantRevision || stale.Acceptance.Replayed || !stale.Acceptance.Current || stale.Acceptance.AnswerDigest != second.Acceptance.AnswerDigest {
			t.Fatalf("source-only stale submission did not create its own Result revision with the same professional Answer: %+v", stale.Acceptance)
		}
		staleAccepted := s6ReadCLIResultEnvelope(t, root, stale.Acceptance)
		if len(staleAccepted.WorkerCheckFacts) != 1 {
			t.Fatalf("stale Result lacks exactly one Worker fact: %+v", staleAccepted.WorkerCheckFacts)
		}
		staleFact := staleAccepted.WorkerCheckFacts[0]
		if staleFact.Status != "stale" || staleFact.CurrentSourceSnapshotSHA256 == "" || staleFact.CurrentSourceSnapshotSHA256 == previousCurrentSnapshot || staleAccepted.ToolClosureDigest == previousClosureDigest {
			t.Fatalf("source drift without a rerun did not produce a distinct stale closure fact: previous=%s fact=%+v digest=%s", previousCurrentSnapshot, staleFact, staleAccepted.ToolClosureDigest)
		}
		staleDurable, err := assignmentresult.LoadAccepted(stale.State, record, stale.Acceptance.ResultRef, read)
		if err != nil {
			t.Fatalf("stale Result is not durably readable: %v", err)
		}
		if assignmentresult.WorkerClosureComplete(record, staleDurable) {
			t.Fatalf("stale Worker fact was treated as complete at revision %d", wantRevision)
		}
		// Completing the professional answer must not run a hidden check.
		var finalWrappers []assignment.NativeOwnedCheckReceipt
		for _, raw := range stale.State["entities"].(map[string]any)["agents"].([]any) {
			row := raw.(map[string]any)
			if row["id"] != owner {
				continue
			}
			encoded, _ := json.Marshal(row["owned_check_receipts"])
			if err := json.Unmarshal(encoded, &finalWrappers); err != nil {
				t.Fatal(err)
			}
		}
		if len(finalWrappers) != 2 {
			t.Fatalf("stale Result submission unexpectedly ran or registered another WorkerCheck: receipts=%d", len(finalWrappers))
		}
		latestState = stale.State
		previousCurrentSnapshot = staleFact.CurrentSourceSnapshotSHA256
		previousClosureDigest = staleAccepted.ToolClosureDigest
	}
	// Earlier immutable Result and check artifacts remain readable after the
	// owner's live source has drifted and newer stale Results are current.
	for _, acceptedResult := range []struct {
		ref      string
		revision int
	}{{first.Acceptance.ResultRef, 1}, {second.Acceptance.ResultRef, 2}} {
		archived, err := assignmentresult.LoadAccepted(latestState, record, acceptedResult.ref, read)
		if err != nil || archived.ResultRevision != acceptedResult.revision {
			t.Fatalf("archived Result revision %d is no longer readable: result=%+v err=%v", acceptedResult.revision, archived, err)
		}
	}
}

func cloneJSONMap(source map[string]any) map[string]any {
	encoded, _ := json.Marshal(source)
	var clone map[string]any
	_ = json.Unmarshal(encoded, &clone)
	return clone
}
