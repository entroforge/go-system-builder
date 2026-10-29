package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestCanonicalRevisionRunsFreshMainMergeAndChecks(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixture(t, canonicalSourceFile)
	r1 := fixture.record
	r1Result := fixture.result

	// Complete the original Register → PLAN → Result → real Main chain first.
	r1Inspection := fixture.inspect(t, "")
	if !r1Inspection.Ready {
		t.Fatalf("r1 Inspect not ready: %v", r1Inspection.Blockers)
	}
	r1Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r1Inspection}, fixture.integrateConfig())
	if err != nil {
		t.Fatalf("integrate r1 through the original Main runner: %v", err)
	}
	if r1Integrated.Checkpoint.State != integration.StateVerified || fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("r1 Main result state=%q check count=%d, want verified/1", r1Integrated.Checkpoint.State, fileSize(t, fixture.counterPath))
	}
	assertActualCanonicalMainFacts(t, fixture, r1Integrated.Checkpoint, 1, r1Inspection.SourceHead, r1Inspection.MergeBase, r1Inspection.TargetHead)
	r1Path := integration.CheckpointPathForRecord(fixture.root, r1.Reference())
	r1Before, err := os.ReadFile(r1Path)
	if err != nil {
		t.Fatalf("read durable r1 checkpoint: %v", err)
	}
	r1Facts := r1Integrated.Checkpoint.CanonicalMainFacts
	r1Merge := r1Integrated.Checkpoint.MergeCommit
	if r1Facts == nil || r1Facts.Binding.ResultRevision != r1Result.ResultRevision || r1Facts.Binding.AnswerDigest != r1Result.AnswerDigest {
		t.Fatalf("r1 checkpoint lost its original accepted Result binding: %+v", r1Facts)
	}

	// Change the checked-in manifest's owner and Team while preserving the exact
	// locked TASK reference and bytes. The original Register Writer issues r2.
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(canonicalManifest))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = "team-manifest-s6-main-fixture-r2"
	manifest["version"] = "v1.0.2"
	manifest["workgroup_id"] = "workgroup-s6-main-fixture-r2"
	manifest["platform_team_id"] = "platform-team-s6-main-fixture-r2"
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) != 1 {
		t.Fatalf("manifest assignments = %#v", manifest["assignments"])
	}
	assignmentManifest, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatalf("manifest assignment = %#v", assignments[0])
	}
	assignmentManifest["agent_id"] = "agent-s6-main-fixture-r2"
	updatedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updatedManifest = append(updatedManifest, '\n')
	if err := os.WriteFile(manifestPath, updatedManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.root, "add", canonicalManifest)
	gitFixture(t, fixture.root, "commit", "-m", "prepare explicit S6 revision two manifest")

	// Give the revised owner a fresh branch based on current Main and one new
	// scoped commit. This makes the r2 merge denominator independent of r1.
	gitFixture(t, fixture.root, "worktree", "remove", fixture.worker)
	gitFixture(t, fixture.root, "branch", "-D", canonicalBranch)
	gitFixture(t, fixture.root, "worktree", "add", "-b", canonicalBranch, fixture.worker, "main")
	const r2ChangedPath = "src/pkg/revision-two.txt"
	if err := os.WriteFile(filepath.Join(fixture.worker, filepath.FromSlash(r2ChangedPath)), []byte("revision two scoped output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.worker, "add", r2ChangedPath)
	gitFixture(t, fixture.worker, "commit", "-m", "S6 revision two scoped change")

	beforeRevision := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	previousSHA, err := r1.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revisionIntent := "s6-main-fixture-revision-two"
	issued, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{
		ExpectedRevision: beforeRevision.Revision,
		ManifestPath:     manifestPath,
		TaskID:           r1.Task.ID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(canonicalTask)),
		Revision: &assignment.RevisionRequest{
			AssignmentID: r1.AssignmentID, ExpectedRecord: r1.Reference(),
			ExpectedRecordSHA256: previousSHA, IntentKey: revisionIntent,
		},
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("original Register revision Writer: %v", err)
	}
	if issued.Revision != beforeRevision.Revision+1 {
		t.Fatalf("r2 Register Runtime revision=%d, want %d", issued.Revision, beforeRevision.Revision+1)
	}
	r2, found, err := assignmentrecord.ForOwner(issued.State, "agent-s6-main-fixture-r2")
	if err != nil || !found {
		t.Fatalf("resolve current r2 Record: found=%t err=%v", found, err)
	}
	if r2.Revision != 2 || r2.AssignmentID != r1.AssignmentID || r2.Task != r1.Task || r2.OwnerAgentID == r1.OwnerAgentID || r2.TeamID == r1.TeamID {
		t.Fatalf("revision did not preserve Assignment/TASK identity with a fresh owner/Team: r1=%+v r2=%+v", r1.Reference(), r2.Reference())
	}
	oldProjection, err := hookctx.LoadFull(fixture.root, r1.OwnerAgentID)
	if err == nil || oldProjection != nil {
		t.Fatalf("superseded r1 owner still has current Hook context: loaded=%v err=%v", oldProjection != nil, err)
	}
	if err := integration.ValidateCheckpointRecordBinding(r1Integrated.Checkpoint, r2); err == nil {
		t.Fatal("current r2 accepted r1 Main checkpoint binding")
	}
	assertStaleOwnerAckRejected(t, fixture, r1)

	// Register issued the current revision; the new owner now submits its own
	// exact Record-bound PLAN and the original AgentBegin/CompleteTask Writers.
	r2PlanPath := writeCanonicalRevisionPlan(t, fixture.root, r2)
	if _, outcome, err := assignment.AgentBegin(fixture.root, fixture.statePath, fixture.journalPath, assignment.AgentBeginRequest{
		ExpectedRevision: -1, AgentID: r2.OwnerAgentID, PlanPath: r2PlanPath, OccurredAt: time.Now().UTC(),
	}); err != nil || !outcome.Chained {
		t.Fatalf("r2 original AgentBegin: outcome %+v err %v", outcome, err)
	}
	r2Result := acceptCanonicalRevisionResult(t, fixture, r2, "msg-s6-main-r2-result", "The new scoped revision was implemented and assessed.")
	fixture.record = r2
	fixture.planPath = r2PlanPath
	fixture.result = r2Result
	assertCanonicalQualitygateState(t, fixture, false, qualitygate.StatusNotReady)
	if fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("r2 PLAN/Result acceptance reran Main check; count=%d", fileSize(t, fixture.counterPath))
	}

	r2Inspection := fixture.inspect(t, "")
	if !r2Inspection.Ready || !reflect.DeepEqual(r2Inspection.ChangedPaths, []string{r2ChangedPath}) {
		t.Fatalf("r2 Inspect ready=%t changed paths=%v blockers=%v", r2Inspection.Ready, r2Inspection.ChangedPaths, r2Inspection.Blockers)
	}
	if r2Inspection.MergeBase != readGit(t, fixture.root, "rev-parse", "main") || r2Inspection.SourceHead == r1Inspection.SourceHead {
		t.Fatalf("r2 source was not freshly based on post-r1 Main: base=%s source=%s r1source=%s", r2Inspection.MergeBase, r2Inspection.SourceHead, r1Inspection.SourceHead)
	}
	r2Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r2Inspection}, fixture.integrateConfig())
	if err != nil {
		t.Fatalf("integrate r2 with fresh Main facts: %v", err)
	}
	if r2Integrated.Checkpoint.State != integration.StateVerified || r2Integrated.Checkpoint.MergeCommit == r1Merge || fileSize(t, fixture.counterPath) != 2 {
		t.Fatalf("r2 Main state=%q merge=%s r1merge=%s check count=%d", r2Integrated.Checkpoint.State, r2Integrated.Checkpoint.MergeCommit, r1Merge, fileSize(t, fixture.counterPath))
	}
	r2Path := integration.CheckpointPathForRecord(fixture.root, r2.Reference())
	if r2Path == r1Path || filepath.Base(filepath.Dir(r2Path)) != "a2" {
		t.Fatalf("r2 checkpoint slot=%s, r1 slot=%s", r2Path, r1Path)
	}
	r2Bytes, err := os.ReadFile(r2Path)
	if err != nil {
		t.Fatalf("read durable r2 checkpoint: %v", err)
	}
	var storedR2 integration.Checkpoint
	if err := json.Unmarshal(r2Bytes, &storedR2); err != nil || storedR2.CanonicalMainFacts == nil || storedR2.CanonicalMainFacts.Binding != r2Integrated.Checkpoint.CanonicalMainFacts.Binding {
		t.Fatalf("durable r2 checkpoint did not retain actual Result/Main binding: facts=%+v err=%v", storedR2.CanonicalMainFacts, err)
	}
	if got, err := os.ReadFile(r1Path); err != nil || !reflect.DeepEqual(got, r1Before) {
		t.Fatalf("r2 integration changed predecessor checkpoint bytes: err=%v", err)
	}
	if r2Integrated.Checkpoint.CanonicalMainFacts == nil || r2Integrated.Checkpoint.CanonicalMainFacts.Binding.AssignmentRecord != r2.Reference() || r2Integrated.Checkpoint.CanonicalMainFacts.Binding.ResultRevision != r2Result.ResultRevision || r2Integrated.Checkpoint.CanonicalMainFacts.Binding.AnswerDigest != r2Result.AnswerDigest {
		t.Fatalf("r2 Main facts do not bind the new Record and Result: %+v", r2Integrated.Checkpoint.CanonicalMainFacts)
	}
	facts := r2Integrated.Checkpoint.CanonicalMainFacts
	if facts.MergeCommit == r1Facts.MergeCommit || facts.SourceHead != r2Inspection.SourceHead || facts.MergeBase != r2Inspection.MergeBase || len(facts.ChangedPaths) != 1 || facts.ChangedPaths[0] != r2ChangedPath || len(facts.Checks) != len(r2.RequiredChecks) || len(facts.Checks) != len(r1Facts.Checks) {
		t.Fatalf("r2 Main facts did not retain its own source/diff/full checks: %+v", facts)
	}
	for index := range facts.Checks {
		if facts.Checks[index].ReceiptRef == r1Facts.Checks[index].ReceiptRef || facts.Checks[index].ReceiptSHA256 == r1Facts.Checks[index].ReceiptSHA256 || facts.Checks[index].TestedHead != facts.MergeCommit || facts.Checks[index].TestedHead == r1Facts.Checks[index].TestedHead {
			t.Fatalf("r2 Main check %d borrowed predecessor evidence: r1=%+v r2=%+v", index, r1Facts.Checks[index], facts.Checks[index])
		}
	}
	assertTwoParentMerge(t, fixture.root, facts.MergeCommit, r2Inspection.TargetHead, r2Inspection.SourceHead)
	assertCanonicalQualitygateState(t, fixture, true, qualitygate.StatusSatisfied)
}

func writeCanonicalRevisionPlan(t *testing.T, root string, record assignmentrecord.Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	planRef := ".claude/evidence/workgroup-s6-main-fixture-round-2/TASK-001-01/plan-report.json"
	plan := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-s6-main-r2-plan",
		"correlation_id": "corr-s6-main-r2-plan", "runtime_id": record.RuntimeID,
		"agent_id": record.OwnerAgentID, "agent_definition_ref": record.AgentDefinition.Ref,
		"task_id": record.Task.ID, "bug_id": nil, "team_id": record.TeamID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "assignment_id": record.AssignmentID,
		"assignment_revision": record.Revision, "assignment_record_contract": record.Contract,
		"assignment_record_sha256": digest, "objective": "Implement the current revised builder responsibility.",
		"planned_paths":    []string{"src/pkg/revision-two.txt"},
		"assertion_checks": []any{map[string]any{"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes"}},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	path := filepath.Join(root, filepath.FromSlash(planRef))
	writeJSONFixture(t, path, plan)
	return path
}

func acceptCanonicalRevisionResult(t *testing.T, fixture *canonicalMainFixture, record assignmentrecord.Record, messageID, summary string) assignment.ResultAcceptanceReceipt {
	t.Helper()
	occurredAt := time.Now().UTC()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "message_id": messageID,
		"correlation_id": "corr-" + messageID, "runtime_id": record.RuntimeID,
		"baseline_generation": record.BaselineGeneration, "agent_id": record.OwnerAgentID,
		"agent_definition_ref": record.AgentDefinition.Ref, "task_id": record.Task.ID, "bug_id": nil,
		"team_id": record.TeamID, "occurred_at": occurredAt.Format(time.RFC3339Nano),
		"assignment_id": record.AssignmentID, "assignment_revision": record.Revision,
		"assignment_record_contract": record.Contract, "assignment_record_sha256": digest,
		"summary": summary,
		"assertion_evidence": []any{map[string]any{
			"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes",
			"evidence_refs": []string{canonicalProofRef},
		}},
		"risks_blockers": []string{}, "scope_deviations": []string{},
	}
	messagePath := filepath.Join(fixture.root, ".claude", "evidence", record.TeamID, record.Task.ID, messageID+".json")
	writeJSONFixture(t, messagePath, message)
	_, receipt, err := assignment.CompleteTaskWithReceipt(fixture.root, fixture.statePath, fixture.journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("r2 original CompleteTask Writer: %v", err)
	}
	if !receipt.Current || receipt.ResultRevision != 1 || receipt.ResultRef == "" || receipt.ResultSHA256 == "" {
		t.Fatalf("r2 accepted Result receipt = %+v", receipt)
	}
	return receipt
}

func assertStaleOwnerAckRejected(t *testing.T, fixture *canonicalMainFixture, old assignmentrecord.Record) {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	messageID := "msg-s6-stale-r1-ack-after-revision"
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_ack", "message_id": messageID,
		"correlation_id": "corr-" + messageID, "runtime_id": old.RuntimeID,
		"agent_id": old.OwnerAgentID, "agent_definition_ref": old.AgentDefinition.Ref,
		"task_id": old.Task.ID, "bug_id": nil, "team_id": old.TeamID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"body":        "Attempt to acknowledge a predecessor revision after supersession.",
	}
	path := filepath.Join(fixture.root, ".claude", "evidence", old.TeamID, old.Task.ID, messageID+".json")
	writeJSONFixture(t, path, message)
	if _, err := assignment.AdvanceAgent(fixture.root, fixture.statePath, fixture.journalPath, assignment.AgentEventRequest{
		ExpectedRevision: -1, AgentID: old.OwnerAgentID, Event: "completion_acknowledged", MessagePath: path, OccurredAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("superseded r1 owner ACK was accepted after Register issued r2")
	}
	oldSHA, err := old.Digest()
	if err != nil {
		t.Fatal(err)
	}
	oldResultID := "msg-s6-stale-r1-result-after-revision"
	oldResult := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "message_id": oldResultID,
		"correlation_id": "corr-" + oldResultID, "runtime_id": old.RuntimeID,
		"baseline_generation": old.BaselineGeneration, "agent_id": old.OwnerAgentID,
		"agent_definition_ref": old.AgentDefinition.Ref, "task_id": old.Task.ID, "bug_id": nil,
		"team_id": old.TeamID, "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"assignment_id": old.AssignmentID, "assignment_revision": old.Revision,
		"assignment_record_contract": old.Contract, "assignment_record_sha256": oldSHA,
		"summary": "The superseded owner attempts to submit another Result.",
		"assertion_evidence": []any{map[string]any{
			"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes",
			"evidence_refs": []string{canonicalProofRef},
		}},
		"risks_blockers": []string{}, "scope_deviations": []string{},
	}
	oldResultPath := filepath.Join(fixture.root, ".claude", "evidence", old.TeamID, old.Task.ID, oldResultID+".json")
	writeJSONFixture(t, oldResultPath, oldResult)
	if _, _, err := assignment.CompleteTaskWithReceipt(fixture.root, fixture.statePath, fixture.journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1, AgentID: old.OwnerAgentID, MessagePath: oldResultPath, OccurredAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("superseded r1 owner Result was accepted after Register issued r2")
	}
	after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if after.Revision != snapshot.Revision {
		t.Fatalf("rejected predecessor ACK mutated Runtime revision %d → %d", snapshot.Revision, after.Revision)
	}
	if _, err := hookctx.LoadFull(fixture.root, old.OwnerAgentID); err == nil {
		t.Fatal("superseded owner reloaded as current after its ACK was rejected")
	}
	if fixture.result.ResultRevision == 0 {
		t.Fatalf("fixture lost predecessor Result receipt: %+v", fixture.result)
	}
	if err := loopruntime.ValidateAssignmentRevisionReceipts(after.State, mustReadJournal(t, fixture.journalPath)); err != nil {
		t.Fatalf("original revision Writer receipt invalid after stale ACK rejection: %v", err)
	}
}

func mustReadJournal(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
