package qualitygate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestCanonicalBuilderReceiptAcceptedResultRemainsUnassessed(t *testing.T) {
	fixture := newCanonicalReceiptFixture(t)
	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !receipt.Accepted || receipt.WorkComplete || receipt.Integrated {
		t.Fatalf("accepted Result must not imply professional completion or Main integration: %+v", receipt)
	}
	if receipt.ProfessionalStatus != "accepted_unassessed" || receipt.ResponsibilityStatus != "accepted_incomplete" {
		t.Fatalf("accepted Result status = %q / %q", receipt.ProfessionalStatus, receipt.ResponsibilityStatus)
	}
	if len(receipt.OwnPending) != 1 || receipt.OwnPending[0] != "professional_closing_contract_unproven" || len(receipt.WaitingOn) != 1 || receipt.WaitingOn[0].Reason != "integration_pending" {
		t.Fatalf("missing Main integration must remain a Main wait: %+v", receipt)
	}
	if receipt.ResultRef != fixture.request.CompletionRef || receipt.AssignmentRecord == nil || receipt.AssignmentRecordSHA256 == "" || receipt.AnswerDigest == "" {
		t.Fatalf("receipt lost exact accepted Result/Record binding: %+v", receipt)
	}
	assertCanonicalReceiptSchema(t, receipt)
}

func TestCanonicalBuilderReceiptReportsWaitingForResultBeforeAndAfterPlan(t *testing.T) {
	for _, test := range []struct {
		name         string
		acceptedPlan bool
	}{
		{name: "before PLAN"},
		{name: "after accepted PLAN", acceptedPlan: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCanonicalReceiptFixture(t)
			removeCanonicalResultSubmission(t, &fixture, test.acceptedPlan)

			receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
			if receipt.Accepted || receipt.DeliveryRecorded || receipt.WorkComplete || receipt.Integrated {
				t.Fatalf("unsubmitted Result must remain progress, not delivery: %+v", receipt)
			}
			if receipt.ResponsibilityStatus != "not_submitted" || receipt.ProfessionalStatus != "not_submitted" || len(receipt.OwnPending) != 1 || receipt.OwnPending[0] != "current_Result" {
				t.Fatalf("valid pending Assignment should wait for its Result, not report invalid registration: %+v", receipt)
			}
			assertCanonicalReceiptSchema(t, receipt)
		})
	}
}

func TestCanonicalBuilderReceiptPendingResultDoesNotMaskMissingAssignment(t *testing.T) {
	fixture := newCanonicalReceiptFixture(t)
	removeCanonicalResultSubmission(t, &fixture, false)
	fixture.input.Snapshot.State["entities"].(map[string]any)["assignments"] = []any{}

	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if receipt.Accepted || receipt.DeliveryRecorded || receipt.WorkComplete || receipt.Integrated {
		t.Fatalf("missing current Assignment must not be reported as a pending Result: %+v", receipt)
	}
	if receipt.ResponsibilityStatus != "invalid" || !containsString(receipt.OwnPending, "canonical_assignment_invalid") {
		t.Fatalf("damaged canonical registration must remain rejected: %+v", receipt)
	}
	assertCanonicalReceiptSchema(t, receipt)
}

func TestCanonicalBuilderReceiptCompletesOnlyExplicitEmptyOwnedCheckClosure(t *testing.T) {
	fixture := newCanonicalReceiptFixtureWithClosing(t)
	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !receipt.Accepted || !receipt.WorkComplete || receipt.Integrated {
		t.Fatalf("explicit complete closure with frozen empty Worker checks should complete only owned work: %+v", receipt)
	}
	if receipt.ProfessionalStatus != "complete" || receipt.ResponsibilityStatus != "complete" || len(receipt.OwnPending) != 0 {
		t.Fatalf("complete own closure was not projected: %+v", receipt)
	}
	if len(receipt.WaitingOn) != 1 || receipt.WaitingOn[0].Reason != "integration_pending" {
		t.Fatalf("Main must remain a separate wait after own completion: %+v", receipt)
	}
	assertCanonicalReceiptSchema(t, receipt)
}

func TestCanonicalBuilderReceiptKeepsIncompleteRiskAndOwnedCheckPending(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*assignmentresult.Answer)
		pending string
		status  string
		fixture func(*testing.T) canonicalReceiptFixture
	}{
		{
			name: "blocked judgment", pending: "builder_blocked", status: "incomplete",
			fixture: newCanonicalReceiptFixtureWithClosing,
			mutate: func(answer *assignmentresult.Answer) {
				answer.ClosingAssessment.Disposition = "blocked"
				answer.ClosingAssessment.Conditions[0].Judgment = "blocked"
			},
		},
		{
			name: "incomplete judgment", pending: "closing_conditions_incomplete", status: "incomplete",
			fixture: newCanonicalReceiptFixtureWithClosing,
			mutate: func(answer *assignmentresult.Answer) {
				answer.ClosingAssessment.Disposition = "incomplete"
				answer.ClosingAssessment.Conditions[0].Judgment = "incomplete"
			},
		},
		{
			name: "declared risk", pending: "declared_risks_or_blockers", status: "incomplete",
			fixture: newCanonicalReceiptFixtureWithClosing,
			mutate:  func(answer *assignmentresult.Answer) { answer.RisksBlockers = []string{"migration remains unverified"} },
		},
		{
			name: "scope deviation", pending: "unapproved_scope_deviation", status: "incomplete",
			fixture: newCanonicalReceiptFixtureWithClosing,
			mutate:  func(answer *assignmentresult.Answer) { answer.ScopeDeviations = []string{"changed an undeclared path"} },
		},
		{
			name: "owned runner unavailable", pending: "owned_check_evidence_unavailable:0", status: "accepted_unassessed",
			fixture: newCanonicalReceiptFixtureWithOwnedCheck,
			mutate:  func(*assignmentresult.Answer) {},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.fixture(t)
			mutateCanonicalAnswer(t, fixture, test.mutate)
			receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
			if !receipt.Accepted || receipt.WorkComplete || receipt.ProfessionalStatus != test.status || !containsString(receipt.OwnPending, test.pending) {
				t.Fatalf("incomplete canonical closure was projected as complete: %+v", receipt)
			}
			assertCanonicalReceiptSchema(t, receipt)
		})
	}
}

func TestCanonicalBuilderReceiptCompletesPassingWorkerChecksAndWaitsForMain(t *testing.T) {
	fixture := newCanonicalReceiptFixtureWithOwnedCheck(t)
	resultPath := filepath.Join(fixture.input.Root, filepath.FromSlash(fixture.request.CompletionRef))
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result assignmentresult.AcceptedResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}

	profile := fixture.record.ClosingContract.WorkerChecks[0].Profile
	checkout := filepath.Join(fixture.input.Root, "worker-checkout")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	originalDir := filepath.Join(fixture.input.Root, ".claude", "evidence", fixture.record.RuntimeID, "g3", "worktree", fixture.record.AssignmentID, "checks", "check-0")
	if err := os.MkdirAll(originalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalReceiptPath := filepath.Join(originalDir, "receipt.json")
	logPath := filepath.Join(originalDir, "output.log")
	logBytes := []byte("worker check passed\n")
	if err := os.WriteFile(logPath, logBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	permission := &checkexec.PermissionObservation{
		Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: "linux/amd64", Kernel: "test-kernel",
		UID: 1000, GID: 1000, Groups: []int{}, BinarySHA256: strings.Repeat("1", 64), ShellSHA256: strings.Repeat("2", 64),
		LibrariesSHA256: strings.Repeat("3", 64), FilterSHA256: strings.Repeat("4", 64), PolicySHA256: strings.Repeat("5", 64),
		AuthoritySHA256: strings.Repeat("6", 64), MainRoot: fixture.input.Root, CommonDir: fixture.input.Root,
		WorkerRoot: checkout, CWD: checkout, Available: true,
	}
	observation := &checkexec.Observation{
		ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileDigest: profile.Digest(),
		SubjectDigest: strings.Repeat("a", 64), RunnerDigest: strings.Repeat("b", 64), EnvironmentDigest: strings.Repeat("c", 64),
		DependencyDigest: strings.Repeat("d", 64), FixtureDigest: strings.Repeat("e", 64), Environment: []checkexec.EnvFact{},
		VerificationScope: "whole disposable checked tree", ReuseEligible: false,
		Permissions: permission, PermissionDigest: permission.Digest(),
	}
	snapshotSHA := strings.Repeat("7", 64)
	bootstrapSHA := strings.Repeat("f", 64)
	binding := checkexec.Binding{
		RuntimeID: fixture.record.RuntimeID, Generation: fixture.record.BaselineGeneration,
		AssignmentID: fixture.record.AssignmentID, Owner: fixture.record.OwnerAgentID,
		ExecutionGeneration: 1, Responsibility: "owned_check",
	}
	sealed, err := checkexec.SealReceipt(map[string]any{
		"version": 2, "runtime_id": fixture.record.RuntimeID, "baseline_generation": fixture.record.BaselineGeneration,
		"assignment_id": fixture.record.AssignmentID, "execution_generation": 1,
		"check_index": 0, "command": profile.Command, "status": "pass", "snapshot_sha256": snapshotSHA,
		"check_binding": binding, "profile_observation": observation, "output": logPath, "disposable_copy": true,
		"assignment_record": fixture.record.Reference(), "assignment_record_sha256": canonicalReceiptRecordSHA(t, fixture.record),
		"worker_check_index": 0, "worker_check_contract": assignmentrecord.ClosingContractVersion,
		"worker_check_snapshot": map[string]any{
			"runtime_id": fixture.record.RuntimeID, "baseline_generation": fixture.record.BaselineGeneration,
			"assignment_id": fixture.record.AssignmentID, "owner_agent_id": fixture.record.OwnerAgentID,
			"execution_generation": 1, "bootstrap_sha256": bootstrapSHA,
		},
	}, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalReceiptPath, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	toolReceipt := &checkexec.ToolReceipt{
		Path: originalReceiptPath, SHA256: checkexec.Hash(sealed), LogSHA256: checkexec.Hash(logBytes),
		Checkout: checkout, Command: profile.Command, ProfileDigest: profile.Digest(), Binding: binding,
	}
	fact := assignmentresult.WorkerCheckFact{
		Index: 0, Command: profile.Command, ProfileDigest: profile.Digest(), Status: "pass", ExecutionKind: "workspace",
		Record: fixture.record.Reference(), RecordSHA256: canonicalReceiptRecordSHA(t, fixture.record),
		RuntimeID: fixture.record.RuntimeID, BaselineGeneration: fixture.record.BaselineGeneration,
		OwnerAgentID: fixture.record.OwnerAgentID, ExecutionGeneration: 1, Checkout: checkout,
		BootstrapSHA256: bootstrapSHA, SourceSnapshotSHA256: snapshotSHA, Observation: observation, OriginalReceipt: toolReceipt,
	}
	fact.ReceiptArtifact = &assignmentresult.ResolvedEvidence{
		Ref:  assignmentresult.WorkerCheckEvidenceRef(0, "receipt"),
		Path: assignmentresult.WorkerCheckEvidenceArtifactPath(fixture.record, result.EvidenceID, 0, "receipt", toolReceipt.SHA256), SHA256: toolReceipt.SHA256,
	}
	fact.LogArtifact = &assignmentresult.ResolvedEvidence{
		Ref:  assignmentresult.WorkerCheckEvidenceRef(0, "log"),
		Path: assignmentresult.WorkerCheckEvidenceArtifactPath(fixture.record, result.EvidenceID, 0, "log", toolReceipt.LogSHA256), SHA256: toolReceipt.LogSHA256,
	}
	writeCanonicalReceiptFile(t, fixture.input.Root, fact.ReceiptArtifact.Path, sealed)
	writeCanonicalReceiptFile(t, fixture.input.Root, fact.LogArtifact.Path, logBytes)
	result.WorkerCheckFacts = []assignmentresult.WorkerCheckFact{fact}
	result.ToolClosureDigest, err = assignmentresult.WorkerClosureDigest(fixture.record, result.WorkerCheckFacts)
	if err != nil {
		t.Fatal(err)
	}
	resultBytes = canonicalReceiptJSON(t, result)
	writeCanonicalReceiptFile(t, fixture.input.Root, fixture.request.CompletionRef, resultBytes)
	for _, raw := range fixture.input.Snapshot.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["path"] == fixture.request.CompletionRef {
			row["sha256"] = canonicalReceiptSHA256(resultBytes)
		}
	}

	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !receipt.Accepted || !receipt.WorkComplete || receipt.Integrated {
		t.Fatalf("passing owned check closure should complete Worker work while Main remains separate: %+v", receipt)
	}
	if receipt.ProfessionalStatus != "complete" || receipt.ResponsibilityStatus != "complete" || len(receipt.OwnPending) != 0 || len(receipt.WaitingOn) != 1 || receipt.WaitingOn[0].Reason != "integration_pending" {
		t.Fatalf("passing Worker check receipt projection = %+v", receipt)
	}
	assertCanonicalReceiptSchema(t, receipt)
}

func TestCanonicalBuilderReceiptFailsClosedOnCurrentSourceDrift(t *testing.T) {
	for _, test := range []string{"frozen Assignment source", "accepted Result artifact"} {
		t.Run(test, func(t *testing.T) {
			fixture := newCanonicalReceiptFixture(t)
			ref := fixture.record.AgentDefinition.Ref
			if test == "accepted Result artifact" {
				ref = fixture.request.CompletionRef
			}
			writeCanonicalReceiptFile(t, fixture.input.Root, ref, []byte("changed bytes after issuance\n"))
			receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
			if receipt.Accepted || receipt.WorkComplete || receipt.Integrated || receipt.ProfessionalStatus != "not_submitted" {
				t.Fatalf("drifted canonical source must not be accepted or fall through to legacy completion: %+v", receipt)
			}
			if len(receipt.OwnPending)+len(receipt.WaitingOn) == 0 {
				t.Fatalf("missing canonical-source failure reason: %+v", receipt)
			}
			assertCanonicalReceiptSchema(t, receipt)
		})
	}
}

func TestCanonicalBuilderReceiptMarkerWithoutCurrentRecordDoesNotUseLegacyReport(t *testing.T) {
	fixture := newCanonicalReceiptFixture(t)
	entities := fixture.input.Snapshot.State["entities"].(map[string]any)
	entities["assignments"] = []any{}
	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if receipt.Accepted || receipt.WorkComplete || receipt.Integrated || receipt.ProfessionalStatus != "not_submitted" {
		t.Fatalf("explicit S6 marker without the current Runtime Record must fail closed: %+v", receipt)
	}
	if !containsString(receipt.OwnPending, "canonical_assignment_invalid") {
		t.Fatalf("missing exact canonical registration reason: %+v", receipt)
	}
	assertCanonicalReceiptSchema(t, receipt)
}

func TestCanonicalBuilderReceiptAcceptsExactMainFactsWithoutCompletingProfessionalWork(t *testing.T) {
	fixture := newCanonicalReceiptFixture(t)
	root := fixture.input.Root
	gitReceipt(t, root, "init", "--initial-branch=main")
	gitReceipt(t, root, "add", ".")
	gitReceipt(t, root, "commit", "-m", "original result and context")
	targetHead := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))
	gitReceipt(t, root, "checkout", "-b", "worker/qualitygate-s6")
	changedPath := "internal/pkg/handler.go"
	writeCanonicalReceiptFile(t, root, changedPath, []byte("package pkg\n\nfunc Handle() string { return \"ok\" }\n"))
	gitReceipt(t, root, "add", changedPath)
	gitReceipt(t, root, "commit", "-m", "scoped implementation")
	sourceHead := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))
	gitReceipt(t, root, "checkout", "main")
	gitReceipt(t, root, "merge", "--no-ff", "worker/qualitygate-s6", "-m", "integrate scoped implementation")
	mergeCommit := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))

	resultBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(fixture.request.CompletionRef)))
	if err != nil {
		t.Fatal(err)
	}
	var result assignmentresult.AcceptedResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}
	changedPaths, changedPathsSHA, err := assignmentresult.CanonicalChangedPaths([]string{changedPath})
	if err != nil {
		t.Fatal(err)
	}
	state := fixture.input.Snapshot.State
	state["bound_req"].(map[string]any)["workspace"].(map[string]any)["dev_branch"] = "main"
	facts := assignmentresult.MainFacts{
		Binding: assignmentresult.ResultBinding{
			AssignmentRecord: fixture.record.Reference(), AssignmentRecordSHA256: canonicalReceiptRecordSHA(t, fixture.record),
			ResultPath: fixture.request.CompletionRef, ResultSHA256: canonicalReceiptSHA256(resultBytes),
			ResultRevision: result.ResultRevision, AnswerDigest: result.AnswerDigest,
		},
		MainRoot: root, SourceHead: sourceHead, MergeBase: targetHead, TestedHead: mergeCommit,
		TargetBranch: "main", MergeCommit: mergeCommit, ExecutionGeneration: 0,
		ChangedPaths: changedPaths, ChangedPathsSHA256: changedPathsSHA, Checks: []assignmentresult.MainCheckFact{},
	}
	checkpointRef := path.Join(".claude/evidence", fixture.record.RuntimeID, "g3", "worktree", fixture.record.AssignmentID, "checkpoint.json")
	checkpoint := map[string]any{
		"task_id": fixture.request.TaskID, "assignment_id": fixture.record.AssignmentID, "baseline_generation": 3,
		"revision": 1,
		"state":    "verified", "verified_at": "2026-10-14T00:03:00Z",
		"completion_report_path": fixture.request.CompletionRef, "completion_report_sha256": canonicalReceiptSHA256(resultBytes),
		"source_head": sourceHead, "target_head": targetHead, "merge_base": targetHead, "target_branch": "main",
		"merge_commit": mergeCommit, "tested_head": mergeCommit, "check_receipts": []string{},
		"tool_check_receipts": []any{}, "canonical_main_facts": facts,
	}
	writeCanonicalReceiptFile(t, root, checkpointRef, canonicalReceiptJSON(t, checkpoint))
	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !receipt.Accepted || !receipt.Integrated || receipt.WorkComplete {
		t.Fatalf("exact original Main facts should integrate without deciding professional completion: %+v", receipt)
	}
	if receipt.ProfessionalStatus != "accepted_unassessed" || receipt.ResponsibilityStatus != "accepted_incomplete" || len(receipt.WaitingOn) != 0 {
		t.Fatalf("canonical professional/Main statuses = %q / %q waiting %+v", receipt.ProfessionalStatus, receipt.ResponsibilityStatus, receipt.WaitingOn)
	}
	if receipt.MainCheckpointRef != checkpointRef || receipt.MainCheckpointSHA256 != canonicalReceiptSHA256(canonicalReceiptJSON(t, checkpoint)) || len(receipt.ActualChangedPaths) != 1 || receipt.ActualChangedPaths[0] != changedPath {
		t.Fatalf("receipt lost checkpoint or exact Main changed-path evidence: %+v", receipt)
	}
	assertCanonicalReceiptSchema(t, receipt)

	// A valid Main receipt remains bound to the original pre-merge TargetHead.
	// Substituting post-merge HEAD as the original diff denominator invalidates it.
	checkpoint["target_head"] = mergeCommit
	writeCanonicalReceiptFile(t, root, checkpointRef, canonicalReceiptJSON(t, checkpoint))
	changed := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !changed.Accepted || changed.Integrated || changed.WorkComplete || changed.ProfessionalStatus != "accepted_unassessed" {
		t.Fatalf("checkpoint with a changed original target/diff denominator must lose integration only: %+v", changed)
	}
	if len(changed.WaitingOn) != 1 || changed.WaitingOn[0].Reason != "integration_receipt_invalid" {
		t.Fatalf("invalidated Main denominator must produce a Main wait: %+v", changed)
	}
	checkpoint["target_head"] = targetHead
	for _, test := range []string{"Result binding", "Main receipt coverage"} {
		t.Run("reject changed "+test, func(t *testing.T) {
			var candidate map[string]any
			if err := json.Unmarshal(canonicalReceiptJSON(t, checkpoint), &candidate); err != nil {
				t.Fatal(err)
			}
			switch test {
			case "Result binding":
				facts := candidate["canonical_main_facts"].(map[string]any)
				facts["binding"].(map[string]any)["result_sha256"] = strings.Repeat("0", 64)
			case "Main receipt coverage":
				candidate["check_receipts"] = []string{filepath.Join(root, "unregistered-main-receipt.json")}
			}
			writeCanonicalReceiptFile(t, root, checkpointRef, canonicalReceiptJSON(t, candidate))
			invalid := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
			if !invalid.Accepted || invalid.Integrated || invalid.WorkComplete || invalid.ProfessionalStatus != "accepted_unassessed" {
				t.Fatalf("changed %s must invalidate Main integration only: %+v", test, invalid)
			}
			if len(invalid.WaitingOn) != 1 || invalid.WaitingOn[0].Reason != "integration_receipt_invalid" {
				t.Fatalf("changed %s did not retain an explicit Main wait: %+v", test, invalid)
			}
		})
	}
}

func TestCanonicalBuilderBatchWaitsForClosingEvenAfterMainIntegration(t *testing.T) {
	fixture := newCanonicalReceiptFixtureWithClosing(t)
	mutateCanonicalAnswer(t, fixture, func(answer *assignmentresult.Answer) {
		answer.ClosingAssessment.Disposition = "incomplete"
		answer.ClosingAssessment.Conditions[0].Judgment = "incomplete"
	})
	installCanonicalMainFacts(t, fixture)

	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !receipt.Accepted || !receipt.Integrated || receipt.WorkComplete || receipt.ProfessionalStatus != "incomplete" {
		t.Fatalf("Main integration must remain independent of the incomplete Worker assessment: %+v", receipt)
	}
	progress := qualitygate.PlannedBuilderProgress(fixture.input)[fixture.request.TaskID]
	if progress.State != "active" || !progress.Integrated || !progress.DeliveryRecorded || progress.Reason != "professional_closure_incomplete:closing_conditions_incomplete" {
		t.Fatalf("dispatch progress released an incompletely closed Result: %+v", progress)
	}

	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.GateID, fixture.input.TransitionID = "GATE-BUILDER-BATCH-READY", "TR-006"
	evaluation, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Status != qualitygate.StatusNotReady || !containsString(evaluation.Missing, "professional_closure:"+fixture.request.TaskID+":professional_closure_incomplete:closing_conditions_incomplete") {
		t.Fatalf("TR-006 treated integrated-but-incomplete canonical work as ready: %+v", evaluation)
	}
}

// Both dispatch-projected TASKs and TASKs omitted by that projection remain
// members of the exact execution batch and consume the same canonical checks.
func TestCanonicalBuilderBatchProgressReusePreservesExactBatch(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, taskState := range []string{"active", "candidate", "reviewed", "locked"} {
		for _, outcome := range []string{"complete", "main pending", "closing failed"} {
			t.Run(taskState+"/"+outcome, func(t *testing.T) {
				fixture := newCanonicalReceiptFixtureWithClosing(t)
				if outcome == "closing failed" {
					mutateCanonicalAnswer(t, fixture, func(answer *assignmentresult.Answer) {
						answer.ClosingAssessment.Disposition = "incomplete"
						answer.ClosingAssessment.Conditions[0].Judgment = "incomplete"
					})
				}
				if outcome != "main pending" {
					installCanonicalMainFacts(t, fixture)
				}
				task := fixture.input.Snapshot.State["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
				task["state"] = taskState
				delete(task, "last_lifecycle_event")
				_, projected := qualitygate.PlannedBuilderProgress(fixture.input)[fixture.request.TaskID]
				if projected != (taskState == "active") {
					t.Fatalf("TASK projection membership = %t for %s", projected, taskState)
				}
				fixture.input.GateID, fixture.input.TransitionID = "GATE-BUILDER-BATCH-READY", "TR-006"
				evaluation, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), fixture.input)
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "complete" {
					if evaluation.Status != qualitygate.StatusSatisfied || len(evaluation.Missing) != 0 {
						t.Fatalf("complete exact-batch TASK must satisfy: %+v", evaluation)
					}
					return
				}
				missing := "integration_checkpoint:" + fixture.request.TaskID + ":canonical result/integration not verified"
				if outcome == "closing failed" {
					missing = "professional_closure:" + fixture.request.TaskID + ":professional_closure_incomplete:closing_conditions_incomplete"
				}
				if evaluation.Status != qualitygate.StatusNotReady || !containsString(evaluation.Missing, missing) {
					t.Fatalf("unfinished exact-batch TASK must remain not_ready with %s: %+v", missing, evaluation)
				}
			})
		}
	}
}

func installCanonicalMainFacts(t *testing.T, fixture canonicalReceiptFixture) {
	t.Helper()
	root := fixture.input.Root
	gitReceipt(t, root, "init", "--initial-branch=main")
	gitReceipt(t, root, "add", ".")
	gitReceipt(t, root, "commit", "-m", "original result and context")
	targetHead := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))
	gitReceipt(t, root, "checkout", "-b", "worker/qualitygate-s6")
	changedPath := "internal/pkg/handler.go"
	writeCanonicalReceiptFile(t, root, changedPath, []byte("package pkg\n\nfunc Handle() string { return \"ok\" }\n"))
	gitReceipt(t, root, "add", changedPath)
	gitReceipt(t, root, "commit", "-m", "scoped implementation")
	sourceHead := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))
	gitReceipt(t, root, "checkout", "main")
	gitReceipt(t, root, "merge", "--no-ff", "worker/qualitygate-s6", "-m", "integrate scoped implementation")
	mergeCommit := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))

	resultBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(fixture.request.CompletionRef)))
	if err != nil {
		t.Fatal(err)
	}
	var result assignmentresult.AcceptedResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}
	changedPaths, changedPathsSHA, err := assignmentresult.CanonicalChangedPaths([]string{changedPath})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Snapshot.State["bound_req"].(map[string]any)["workspace"].(map[string]any)["dev_branch"] = "main"
	facts := assignmentresult.MainFacts{
		Binding: assignmentresult.ResultBinding{
			AssignmentRecord: fixture.record.Reference(), AssignmentRecordSHA256: canonicalReceiptRecordSHA(t, fixture.record),
			ResultPath: fixture.request.CompletionRef, ResultSHA256: canonicalReceiptSHA256(resultBytes),
			ResultRevision: result.ResultRevision, AnswerDigest: result.AnswerDigest,
		},
		MainRoot: root, SourceHead: sourceHead, MergeBase: targetHead, TestedHead: mergeCommit,
		TargetBranch: "main", MergeCommit: mergeCommit, ExecutionGeneration: 0,
		ChangedPaths: changedPaths, ChangedPathsSHA256: changedPathsSHA, Checks: []assignmentresult.MainCheckFact{},
	}
	checkpointRef := path.Join(".claude/evidence", fixture.record.RuntimeID, "g3", "worktree", fixture.record.AssignmentID, "checkpoint.json")
	checkpoint := map[string]any{
		"task_id": fixture.request.TaskID, "assignment_id": fixture.record.AssignmentID, "baseline_generation": 3,
		"revision": 1,
		"state":    "verified", "verified_at": "2026-10-14T00:03:00Z",
		"completion_report_path": fixture.request.CompletionRef, "completion_report_sha256": canonicalReceiptSHA256(resultBytes),
		"source_head": sourceHead, "target_head": targetHead, "merge_base": targetHead, "target_branch": "main",
		"merge_commit": mergeCommit, "tested_head": mergeCommit, "check_receipts": []string{},
		"tool_check_receipts": []any{}, "canonical_main_facts": facts,
	}
	writeCanonicalReceiptFile(t, root, checkpointRef, canonicalReceiptJSON(t, checkpoint))
}

func assertCanonicalReceiptSchema(t *testing.T, receipt qualitygate.BuilderReceipt) {
	t.Helper()
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data); err != nil {
		t.Fatalf("builder-receipt schema rejected actual S6 projection: %v\n%s", err, data)
	}
}

type canonicalReceiptFixture struct {
	input   qualitygate.Input
	request qualitygate.BuilderReceiptRequest
	record  assignmentrecord.Record
}

func newCanonicalReceiptFixture(t *testing.T) canonicalReceiptFixture {
	return newCanonicalReceiptFixtureConfigured(t, false, false)
}

func newCanonicalReceiptFixtureWithClosing(t *testing.T) canonicalReceiptFixture {
	return newCanonicalReceiptFixtureConfigured(t, true, false)
}

func newCanonicalReceiptFixtureWithOwnedCheck(t *testing.T) canonicalReceiptFixture {
	return newCanonicalReceiptFixtureConfigured(t, true, true)
}

func newCanonicalReceiptFixtureConfigured(t *testing.T, withClosing, withWorkerCheck bool) canonicalReceiptFixture {
	t.Helper()
	const (
		runtimeID    = "loop-qualitygate-s6"
		teamID       = "workgroup-qualitygate-s6"
		agentID      = "agent-qualitygate-s6"
		assignmentID = "assignment-qualitygate-s6"
		taskID       = "TASK-042-01"
		manifestRef  = "docs/workgroups/s6/qualitygate.json"
		taskRef      = "docs/dev/tasks/TASK-042-01.md"
		roleRef      = ".claude/agents/backend-builder.md"
		skillRef     = ".claude/skills/backend-engineering/SKILL.md"
	)
	root := t.TempDir()
	taskBytes := []byte("# TASK-042-01\n\n> Version: v1.0.0\n\nImplement the scoped behavior.\n")
	roleBytes := []byte("original backend-builder role\n")
	skillBytes := []byte("original backend engineering skill\n")
	taskSHA, roleSHA, skillSHA := canonicalReceiptSHA256(taskBytes), canonicalReceiptSHA256(roleBytes), canonicalReceiptSHA256(skillBytes)
	manifestAssignment := map[string]any{
		"assignment_id": assignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"agent_id": agentID, "agent_definition_ref": roleRef, "skill_refs": []string{"backend-engineering"},
		"read_paths": []string{}, "scope": []string{"internal/"}, "write_paths": []string{"internal/pkg/"},
		"output_paths": []string{}, "depends_on": []string{}, "required_checks": []string{},
		"done_when": []string{"the owned behavior is implemented and reviewed"}, "dispatch_mode": "plan_checkpoint",
	}
	if withClosing {
		workerChecks := []any{}
		if withWorkerCheck {
			workerChecks = append(workerChecks, map[string]any{
				"index": 0, "command": "go test ./internal/pkg",
				"profile": map[string]any{
					"id": "worker-package-test", "version": "1.0.0", "check_index": 0,
					"command": "go test ./internal/pkg", "location": "worker", "timeout_seconds": 30,
					"environment_policy": "declared_only", "environment": []any{}, "dependencies": []any{}, "fixtures": []any{},
					"input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
				},
			})
		}
		manifestAssignment["closing_contract"] = map[string]any{"contract": assignmentresult.S6ClosingContract, "worker_checks": workerChecks}
		manifestAssignment["integration_check_mode"] = "post_merge"
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-qualitygate-s6", "version": "v1.2.0", "runtime_id": runtimeID,
		"baseline_generation": 3, "workgroup_id": teamID, "workgroup_kind": "builder",
		"documents":   []any{map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": taskSHA}},
		"assignments": []any{manifestAssignment},
	}
	manifestBytes := canonicalReceiptJSON(t, manifest)
	initialState := map[string]any{
		"runtime_id": runtimeID, "revision": 8, "lifecycle": map[string]any{"state": "building"},
		"baseline": map[string]any{"generation": 3},
		"documents": []any{map[string]any{
			"id": taskID, "kind": "task", "path": taskRef, "version": "v1.0.0", "sha256": taskSHA,
			"status": "locked", "generation": 3,
		}},
		"entities": map[string]any{
			"assignments": []any{},
			"tasks":       []any{map[string]any{"id": taskID, "path": taskRef, "sha256": taskSHA, "owner_agent_ids": []any{agentID}}},
			"teams":       []any{map[string]any{"id": teamID, "kind": "builder", "manifest_ref": manifestRef}},
			"agents": []any{map[string]any{
				"id": agentID, "team_id": teamID, "task_ids": []any{taskID}, "role": "backend-builder",
				"definition_ref": roleRef, "dispatch_mode": "plan_checkpoint",
			}},
		},
		"bound_req": map[string]any{"workspace": map[string]any{"project_root": root}},
	}
	record, err := assignmentrecord.BuildRecord(
		manifestBytes, manifestRef, initialState, taskBytes, taskID, assignmentID, []string{"internal/"},
		[]assignmentrecord.ContextSource{
			{Kind: "agent_definition", Ref: roleRef, ID: "backend-builder", SHA256: roleSHA},
			{Kind: "authorized_skill", Ref: skillRef, ID: "backend-engineering", Version: "1.0.0", SHA256: skillSHA},
		},
	)
	if err != nil {
		t.Fatalf("BuildRecord: %v", err)
	}
	for ref, body := range map[string][]byte{manifestRef: manifestBytes, taskRef: taskBytes, roleRef: roleBytes, skillRef: skillBytes} {
		writeCanonicalReceiptFile(t, root, ref, body)
	}
	recordBytes := canonicalReceiptJSON(t, record)
	var recordRow map[string]any
	if err := json.Unmarshal(recordBytes, &recordRow); err != nil {
		t.Fatal(err)
	}
	state := initialState
	state["revision"] = record.RegistrationRevision + 3
	entities := state["entities"].(map[string]any)
	entities["assignments"] = []any{recordRow}
	state["lifecycle"] = map[string]any{"state": "building"}

	resultPrefix := canonicalReceiptOwnerPrefix(record)
	planRef := resultPrefix + "plan-report.json"
	assertion := "the assigned behavior is implemented"
	oracle := "the original evidence demonstrates the behavior"
	assertionCheck := map[string]any{"assertion": assertion, "oracle": oracle}
	if withClosing {
		assertionCheck["done_when_indexes"] = []int{0}
	}
	planMessage := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-plan-qualitygate-s6",
		"correlation_id": "corr-plan-qualitygate-s6", "runtime_id": runtimeID, "agent_id": agentID,
		"agent_definition_ref": roleRef, "task_id": taskID, "bug_id": nil, "team_id": teamID,
		"occurred_at": "2026-10-14T00:00:00Z", "assignment_id": assignmentID, "assignment_revision": record.Revision,
		"assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"assignment_record_sha256":   canonicalReceiptRecordSHA(t, record),
		"objective":                  "Implement the reviewed bounded builder responsibility.",
		"planned_paths":              []string{"internal/pkg/handler.go"},
		"assertion_checks":           []any{assertionCheck},
		"dependencies":               []string{}, "risks_blockers": []string{},
	}
	planBytes := canonicalReceiptJSON(t, planMessage)
	writeCanonicalReceiptFile(t, root, planRef, planBytes)
	activation, err := assignment.BuildPlanCheckpointActivationMessage(
		root, runtimeID, agentID, roleRef, taskID, teamID, -1, planRef,
		assignment.ActivationSourceEntry{
			AssignmentRecord:       func() *assignmentrecord.RecordReference { ref := record.Reference(); return &ref }(),
			AssignmentRecordSHA256: canonicalReceiptRecordSHA(t, record), AgentID: agentID,
			AgentDefinitionRef: roleRef, SkillRefs: record.SkillRefs, WritePaths: record.EffectiveWritePaths,
		}, "2026-10-14T00:01:00Z",
	)
	if err != nil {
		t.Fatalf("BuildPlanCheckpointActivationMessage: %v", err)
	}
	activationRef := resultPrefix + "activation.json"
	writeCanonicalReceiptFile(t, root, activationRef, canonicalReceiptJSON(t, activation))

	resultRef := resultPrefix + "result-1.json"
	sourceRef := resultPrefix + "sources/proof.txt"
	sourceBytes := []byte("immutable original evidence snapshot\n")
	writeCanonicalReceiptFile(t, root, sourceRef, sourceBytes)
	answer := assignmentresult.Answer{
		Summary: "Implemented the assigned behavior and reviewed the relevant output.",
		AssertionEvidence: []assignmentresult.AssertionEvidence{{
			Assertion: assertion, Oracle: oracle, EvidenceRefs: []string{"worker://assertion/evidence"},
		}},
		RisksBlockers: []string{}, ScopeDeviations: []string{},
	}
	if withClosing {
		answer.ClosingAssessment = &assignmentresult.ClosingAssessment{Disposition: "complete", Conditions: []assignmentresult.ClosingCondition{{
			DoneWhenIndex: 0, Judgment: "satisfied", PlanAssertionIndexes: []int{0},
			Rationale: "The accepted assertion evidence covers the frozen delivery condition.",
		}}}
	}
	answerDigest, err := assignmentresult.AnswerDigest(answer)
	if err != nil {
		t.Fatal(err)
	}
	result := assignmentresult.AcceptedResult{
		SchemaVersion: "1.0.0", EvidenceID: "ev-result-qualitygate-s6", Kind: "completion_report",
		RuntimeID: runtimeID, BaselineGeneration: 3, ProducerAgentID: agentID,
		ProducerResponsibility: "BUILD-WORK-PACKAGE",
		SubjectRefs:            []assignmentresult.SubjectReference{{ID: taskID, Kind: "task", Path: taskRef, Version: "v1.0.0", SHA256: taskSHA}},
		Conclusion:             "submitted", TaskID: taskID, AssignmentRecord: record.Reference(),
		AssignmentRecordSHA256: canonicalReceiptRecordSHA(t, record), ResultRevision: 1,
		SubmissionKey: "msg-result-qualitygate-s6", AnswerDigest: answerDigest, Answer: answer,
		PlanRef: planRef, PlanSHA256: canonicalReceiptSHA256(planBytes),
		ResolvedEvidenceRefs: []assignmentresult.ResolvedEvidence{{Ref: "worker://assertion/evidence", Path: sourceRef, SHA256: canonicalReceiptSHA256(sourceBytes)}},
		CreatedAt:            "2026-10-14T00:02:00Z",
	}
	if withWorkerCheck {
		profile := record.ClosingContract.WorkerChecks[0].Profile
		result.WorkerCheckFacts = []assignmentresult.WorkerCheckFact{{
			Index: 0, Command: profile.Command, ProfileDigest: profile.Digest(), Status: "unavailable",
			ExecutionKind: "workspace", Record: record.Reference(), RecordSHA256: canonicalReceiptRecordSHA(t, record),
			RuntimeID: runtimeID, BaselineGeneration: 3, OwnerAgentID: agentID, ExecutionGeneration: 1,
		}}
		result.ToolClosureDigest, err = assignmentresult.WorkerClosureDigest(record, result.WorkerCheckFacts)
		if err != nil {
			t.Fatalf("build explicit unavailable Worker closure: %v", err)
		}
	}
	resultBytes := canonicalReceiptJSON(t, result)
	writeCanonicalReceiptFile(t, root, resultRef, resultBytes)
	agent := entities["agents"].([]any)[0].(map[string]any)
	agent["prompt_ref"] = manifestRef + "#" + assignmentID
	agent["state"] = "reported"
	agent["readback_ref"], agent["plan_reported_ref"], agent["activation_ref"] = planRef, planRef, activationRef
	agent["completion_reported_ref"] = resultRef
	task := entities["tasks"].([]any)[0].(map[string]any)
	task["completion_report_ref"] = resultRef
	state["evidence"] = []any{map[string]any{
		"id": result.EvidenceID, "kind": "completion_report", "path": resultRef, "sha256": canonicalReceiptSHA256(resultBytes),
		"status": "valid", "baseline_generation": 3, "responsibility_id": "BUILD-WORK-PACKAGE", "produced_by": []any{agentID},
	}}
	return canonicalReceiptFixture{
		input:   qualitygate.Input{Root: root, Snapshot: runtime.Snapshot{Revision: record.RegistrationRevision + 3, State: state}, Files: fileview.Disk{Root: root}},
		request: qualitygate.BuilderReceiptRequest{Owner: agentID, AssignmentID: assignmentID, TaskID: taskID, CompletionRef: resultRef},
		record:  record,
	}
}

func mutateCanonicalAnswer(t *testing.T, fixture canonicalReceiptFixture, mutate func(*assignmentresult.Answer)) {
	t.Helper()
	resultPath := filepath.Join(fixture.input.Root, filepath.FromSlash(fixture.request.CompletionRef))
	body, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result assignmentresult.AcceptedResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	mutate(&result.Answer)
	result.AnswerDigest, err = assignmentresult.AnswerDigest(result.Answer)
	if err != nil {
		t.Fatal(err)
	}
	body = canonicalReceiptJSON(t, result)
	writeCanonicalReceiptFile(t, fixture.input.Root, fixture.request.CompletionRef, body)
	for _, row := range fixture.input.Snapshot.State["evidence"].([]any) {
		entry := row.(map[string]any)
		if entry["path"] == fixture.request.CompletionRef {
			entry["sha256"] = canonicalReceiptSHA256(body)
			return
		}
	}
	t.Fatal("canonical Result registration missing from fixture")
}

func removeCanonicalResultSubmission(t *testing.T, fixture *canonicalReceiptFixture, acceptedPlan bool) {
	t.Helper()
	state := fixture.input.Snapshot.State
	state["schema_version"] = "1.3.0"
	entities := state["entities"].(map[string]any)
	agent := entities["agents"].([]any)[0].(map[string]any)
	task := entities["tasks"].([]any)[0].(map[string]any)
	delete(agent, "completion_reported_ref")
	delete(task, "completion_report_ref")
	state["evidence"] = []any{}
	if acceptedPlan {
		agent["state"], agent["last_lifecycle_event"] = "active", "work_started"
		return
	}
	agent["state"], agent["last_lifecycle_event"] = "pending", "workgroup_registered"
	delete(agent, "readback_ref")
	delete(agent, "plan_reported_ref")
	delete(agent, "activation_ref")
}

func canonicalReceiptRecordSHA(t *testing.T, record assignmentrecord.Record) string {
	t.Helper()
	sha, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func canonicalReceiptOwnerPrefix(record assignmentrecord.Record) string {
	return path.Join(".claude/evidence", record.RuntimeID, "g"+strconv.Itoa(record.BaselineGeneration), "assignments", record.OwnerAgentID) + "/"
}

func canonicalReceiptSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gitReceipt(t *testing.T, root string, args ...string) string {
	t.Helper()
	base := []string{"-C", root, "-c", "user.name=Quality Gate Test", "-c", "user.email=qualitygate@example.com"}
	cmd := exec.Command("git", append(base, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func canonicalReceiptJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeCanonicalReceiptFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
