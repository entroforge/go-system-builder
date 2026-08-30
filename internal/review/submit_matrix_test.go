package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ---------------------------------------------------------------------------
// §14.1 submit matrix: atomic rejection of malformed Canonical ReviewResults,
// Finding field discipline, and the cannot_clean -> discovery_draining ->
// observation_sealed drain state machine.
// ---------------------------------------------------------------------------

// dispatchedFixture registers the fixture plan and dispatches both
// assignments; returns the snapshot and loaded plan.
func dispatchedFixture(t *testing.T, root, statePath, journalPath string) (snapshotRevision int, plan *Plan) {
	t.Helper()
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	loaded, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	return snap.Revision, loaded
}

func qaPassResult(t *testing.T, root string, plan *Plan) string {
	t.Helper()
	return writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
}

// §14.1: Result 错 revision —— review_round 与运行时不一致即原子拒绝。
func TestSubmitResultRejectsWrongReviewRound(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	patchResultField(t, path, "review_round", 2)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "review_round 2") {
		t.Fatalf("wrong review_round must be rejected, got %v", err)
	}
}

// §14.1: Result 错 baseline generation —— 漂移的 baseline 使轮 stale，
// 不可提交。
func TestSubmitResultRejectsWrongBaselineGeneration(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	patchResultField(t, path, "baseline_generation", 7)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "baseline_generation 7") {
		t.Fatalf("wrong baseline_generation must be rejected, got %v", err)
	}
}

// §14.1: Result 错 digest —— subject_digest 不绑定当前 frozen baseline 即
// 原子拒绝。
func TestSubmitResultRejectsSubjectDigestMismatch(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	patchResultField(t, path, "subject_digest", strings.Repeat("f", 64))
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "subject_digest mismatch") {
		t.Fatalf("wrong subject_digest must be rejected, got %v", err)
	}
}

func TestSubmitResultRejectsAssignmentRevisionMismatch(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-old-plan", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	patchResultField(t, path, "assignment_revision", 2)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "assignment_revision") {
		t.Fatalf("result from an old plan revision must be rejected, got %v", err)
	}
}

// S7-8/RC-07: the Builder/Reviewer independence gate must scan every
// persisted builder delivery carrier, not just completion_report. A Builder
// who delivered via builder_report or agent_completion (the legacy
// `runtime evidence add` kinds) is equally non-independent.
func TestProducerIndependenceRejectsBuilderDeliveryOnAnyCarrierKind(t *testing.T) {
	result := &Result{ProducerAgentID: "agent-dv-1"}
	base := map[string]any{
		"baseline_generation": 1,
		"produced_by":         []any{"agent-dv-1"},
	}
	state := func(kind string) map[string]any {
		row := map[string]any{}
		for k, v := range base {
			row[k] = v
		}
		row["kind"] = kind
		return map[string]any{"evidence": []any{row}, "baseline": map[string]any{"generation": 1}}
	}
	for _, kind := range []string{"completion_report", "builder_report", "agent_completion"} {
		if err := validateProducerIndependence(state(kind), result); err == nil || !strings.Contains(err.Error(), "role independence") {
			t.Fatalf("delivery via %s must reject the producer as reviewer, got %v", kind, err)
		}
	}
	// A different agent's delivery does not taint this reviewer.
	other := map[string]any{
		"kind": "builder_report", "baseline_generation": 1,
		"produced_by": []any{"agent-builder-9"},
		"baseline":    map[string]any{"generation": 1},
	}
	if err := validateProducerIndependence(map[string]any{"evidence": []any{other}, "baseline": map[string]any{"generation": 1}}, result); err != nil {
		t.Fatalf("another agent's delivery must not block the reviewer: %v", err)
	}
	// Stale-generation delivery no longer binds.
	stale := map[string]any{
		"kind": "completion_report", "baseline_generation": 0,
		"produced_by": []any{"agent-dv-1"},
	}
	if err := validateProducerIndependence(map[string]any{"evidence": []any{stale}, "baseline": map[string]any{"generation": 1}}, result); err != nil {
		t.Fatalf("stale generation delivery must not block the reviewer: %v", err)
	}
}

func TestRequiredEvidenceMustBePresentOnClaimResult(t *testing.T) {
	plan := &Plan{Claims: []Claim{{ClaimID: "claim-qa-1", RequiredEvidence: []string{"trace"}}}}
	assignment := &PlanAssignment{AssignmentID: "assignment-qa-1", ClaimIDs: []string{"claim-qa-1"}}
	result := &Result{ClaimResults: []ClaimResult{{ClaimID: "claim-qa-1", Conclusion: "pass"}}}
	if err := validateClaimEvidenceRequirements(plan, assignment, result); err == nil || !strings.Contains(err.Error(), "required evidence") {
		t.Fatalf("missing required evidence must be rejected, got %v", err)
	}
	result.ClaimResults[0].EvidenceRefs = []string{"trace:ev/trace.md"}
	if err := validateClaimEvidenceRequirements(plan, assignment, result); err != nil {
		t.Fatalf("claim result with evidence must be accepted: %v", err)
	}
}

func TestRequiredEvidenceMustMatchTypedReference(t *testing.T) {
	plan := &Plan{Claims: []Claim{{ClaimID: "claim-e2e-1", RequiredEvidence: []string{"console", "network"}}}}
	assignment := &PlanAssignment{AssignmentID: "assignment-e2e-1", ClaimIDs: []string{"claim-e2e-1"}}
	result := &Result{ClaimResults: []ClaimResult{{
		ClaimID: "claim-e2e-1", Conclusion: "pass",
		EvidenceRefs: []string{"path:browser-run.md", "network:net-1"},
	}}}
	if err := validateClaimEvidenceRequirements(plan, assignment, result); err == nil || !strings.Contains(err.Error(), "console:<id>") {
		t.Fatalf("path evidence must not satisfy a console requirement, got %v", err)
	}

	result.ClaimResults[0].EvidenceRefs = []string{"console:console-1", "network:net-1"}
	if err := validateClaimEvidenceRequirements(plan, assignment, result); err != nil {
		t.Fatalf("matching typed evidence must be accepted: %v", err)
	}
}

func TestRuntimeEvidenceReferenceMustResolveToIndexedArtifact(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "runtime-evidence.md")
	content := []byte("registered evidence")
	if err := os.WriteFile(artifact, content, 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"evidence": []any{map[string]any{
		"id": "ev-console-1", "path": "runtime-evidence.md", "sha256": sha256Of(content),
	}}}
	if err := validateEvidenceRefs(root, state, []string{"runtime:ev-console-1"}, "claim claim-e2e-1"); err != nil {
		t.Fatalf("registered runtime evidence must be accepted: %v", err)
	}
	if err := validateEvidenceRefs(root, state, []string{"runtime:missing"}, "claim claim-e2e-1"); err == nil || !strings.Contains(err.Error(), "runtime evidence") {
		t.Fatalf("unknown runtime evidence must be rejected with a recovery diagnostic, got %v", err)
	}
}

func TestPathEvidenceReferenceWithDigestRejectsDrift(t *testing.T) {
	root := t.TempDir()
	rel := "evidence/local-trace.json"
	path := filepath.Join(root, rel)
	content := []byte(`{"event":"save","result":"failed"}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "path:" + rel + "#sha256=" + sha256Of(content)
	if err := validateEvidenceRefs(root, map[string]any{}, []string{ref}, "claim claim-qa-1"); err != nil {
		t.Fatalf("matching path digest must pass: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"event":"tampered"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRefs(root, map[string]any{}, []string{ref}, "claim claim-qa-1"); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("drifted path evidence must fail with a digest diagnostic, got %v", err)
	}
}

func TestPathEvidenceReferenceRequiresDigest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "evidence", "local-trace.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"event":"save"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRefs(root, map[string]any{}, []string{"path:evidence/local-trace.json"}, "claim claim-qa-1"); err == nil || !strings.Contains(err.Error(), "no sha256 digest") {
		t.Fatalf("bare local path evidence must be rejected, got %v", err)
	}
}

func TestSubmitResultRejectsMissingExplicitEvidencePath(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var planBody map[string]any
	if err := json.Unmarshal(planData, &planBody); err != nil {
		t.Fatal(err)
	}
	for _, raw := range planBody["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["required_evidence"] = []any{"trace"}
		}
	}
	planData, err = json.MarshalIndent(planBody, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(planData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-missing-evidence", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var resultBody map[string]any
	if err := json.Unmarshal(resultData, &resultBody); err != nil {
		t.Fatal(err)
	}
	for _, raw := range resultBody["claim_results"].([]any) {
		claimResult := raw.(map[string]any)
		if claimResult["claim_id"] == "claim-qa-1" {
			claimResult["evidence_refs"] = []any{"path:.claude/evidence/missing-trace.md"}
		}
	}
	resultData, err = json.MarshalIndent(resultBody, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, append(resultData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "evidence reference") {
		t.Fatalf("missing explicit evidence path must be rejected, got %v", err)
	}
}

func TestSubmitResultRejectsFrozenSubjectDrift(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	path := filepath.Join(root, "internal", "example", "service.go")
	if err := os.WriteFile(path, []byte("drifted after plan registration"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-drifted-baseline", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "frozen subject") {
		t.Fatalf("drifted frozen baseline must reject submit, got %v", err)
	}
	snapshot, snapshotErr := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	if ptr := PlanPointerFromState(snapshot.State); ptr == nil || ptr.Status != "stale" {
		t.Fatalf("frozen subject drift must persist plan status=stale, got %+v", ptr)
	}
}

func TestSubmitResultMarksPlanStaleWhenPinnedPlanDrifts(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	pinned := filepath.Join(root, ".claude", "review", "plans", plan.ReviewPlanID+".json")
	data, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinned, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-plan-drift", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("pinned plan drift must report stale, got %v", err)
	}
	snapshot, snapshotErr := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	if ptr := PlanPointerFromState(snapshot.State); ptr == nil || ptr.Status != "stale" {
		t.Fatalf("pinned plan drift must persist stale status, got %+v", ptr)
	}
}

// ReviewResults are a verification-stage verb; other stages reject them.
func TestSubmitResultRejectsNonVerificationStage(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["lifecycle"].(map[string]any)["state"] = "building"
	statePath, journalPath := writeState(t, root, state)
	planPath := writePlanFile(t, root)
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	path := writeResultFile(t, root, &plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: 1, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "verification stage") {
		t.Fatalf("non-verification stage must reject submits, got %v", err)
	}
}

// §14.1: submit 原子失败 —— 拒绝后无部分状态（运行时字节不变、evidence
// 索引不增长）。
func TestSubmitResultRejectionLeavesNoPartialState(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// Missing claim-qa-2: exact-set violation, rejected before the CAS.
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass"}, nil)
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	}); err == nil {
		t.Fatal("expected exact-set rejection")
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected submit mutated the runtime state — no partial state is allowed")
	}
}

// Producer identity is checked inside the CAS against the dispatched Agent;
// a mismatch must roll the transaction back cleanly.
func TestSubmitResultRejectsProducerMismatchWithoutMutation(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// assignment-qa-1 is dispatched to agent-qa-1 but the result claims
	// agent-dv-1 produced it.
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-dv-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the dispatched Agent") {
		t.Fatalf("producer mismatch must be rejected, got %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("CAS-time rejection mutated the runtime state")
	}
}

// §14.1: fail Claim 无 Finding —— 每个 fail 结论必须绑定恰好一个
// immutable Finding。
func TestSubmitResultRejectsFailClaimWithoutFinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "fail"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "fail claim claim-qa-2 has no Finding") {
		t.Fatalf("fail claim without a Finding must be rejected, got %v", err)
	}
}

func TestSubmitResultRejectsTwoFindingsForOneFailClaim(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	first := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	second := codeInspectionFinding("finding-qa-1b", "claim-qa-1")
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{first, second})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one immutable Finding") {
		t.Fatalf("two Findings on one fail claim must be rejected, got %v", err)
	}
}

// §14.1: user-flow Finding 只有“保存失败” —— schema/readiness 要求
// journey、last-good/wall/first-bad、终态和 step-bound evidence。
func TestSubmitResultRejectsUserFlowFindingMissingFailureBoundary(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// A bare symptom: no journey summary, no entrypoint, no timeline, no
	// terminal state — the user_flow schema conditional rejects it.
	bare := Finding{
		SchemaVersion:   "1.0.0",
		FindingID:       "finding-qa-1",
		ClaimID:         "claim-qa-1",
		Lens:            "qa",
		Severity:        "P1",
		Expected:        "save persists the form",
		AuthorityRefs:   []string{"CONTRACTS-001"},
		Observed:        "保存失败",
		ObservationMode: "user_flow",
		Encounter: Encounter{
			WallAction:         "clicked save",
			FirstBadCheckpoint: "error toast",
		},
		Reproducibility: "always",
		EvidenceRefs:    []string{"ev/save-fail.md"},
	}
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{bare})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "schema") ||
		!strings.Contains(err.Error(), "terminal_state") {
		t.Fatalf("bare user-flow symptom must be rejected naming the missing failure boundary, got %v", err)
	}
}

// Ordinary findings are not investigation-ready without last-good and a
// terminal state (readiness gate beyond the schema).
func TestSubmitResultRejectsOrdinaryFindingWithoutLastGood(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Encounter.LastGoodCheckpoint = ""
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "last_good_checkpoint") {
		t.Fatalf("ordinary finding without last-good must be rejected, got %v", err)
	}
}

// §14.1: timeline step 无 evidence ref —— 普通 Finding 不得
// investigation-ready。
func TestSubmitResultRejectsTimelineStepWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Encounter.Timeline = []TimelineStep{
		{Sequence: 1, Action: "open the form", ObservedCheckpoint: "form rendered", EvidenceRefs: []string{fixtureEvidenceRef(t, root, "timeline-step-1.png")}},
		{Sequence: 2, Action: "submit", ObservedCheckpoint: "error toast"}, // no evidence bound
	}
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "timeline step 2 has no evidence_refs") {
		t.Fatalf("step without bound evidence must be rejected, got %v", err)
	}
}

func TestSubmitResultStaleRevisionDoesNotCreateReviewArtifacts(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-stale", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)

	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision - 1, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale submit must fail with stale revision, got %v", err)
	}
	artifact := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "reviews", "agent-qa-1", "review-result-stale.json")
	if _, statErr := os.Stat(artifact); !os.IsNotExist(statErr) {
		t.Fatalf("stale submit must not leave a review artifact, stat error=%v", statErr)
	}
}

// Apply-time rejection (reviewer Agent not in working state) fails the CAS
// without committing; the staged result artifact must be cleaned up so the
// corrected resubmit does not hit "file exists".
func TestSubmitResultApplyRejectionCleansStagedArtifacts(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// Park the QA reviewer in `reading`: submit passes every pre-CAS check,
	// stages artifacts, then the Apply-time agent-state check rejects.
	setAgentState := func(agentState string, rev int) int {
		store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
		next, err := store.Update(rev, loopruntime.Mutation{
			EventID:        "evt-test-agent-state-" + agentState,
			TransitionID:   "TEST",
			Event:          "test_agent_state",
			Actor:          "test",
			IdempotencyKey: "test:agent-state:" + agentState,
			Apply: func(state map[string]any) error {
				for _, row := range state["entities"].(map[string]any)["agents"].([]any) {
					if agent := row.(map[string]any); agent["id"] == "agent-qa-1" {
						agent["state"] = agentState
					}
				}
				return nil
			},
		})
		if err != nil {
			t.Fatalf("setAgentState: %v", err)
		}
		return next.Revision
	}
	revision = setAgentState("reading", revision)

	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-orphan", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "requires working state") {
		t.Fatalf("submit with a reading reviewer must be rejected, got %v", err)
	}
	artifact := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "reviews", "agent-qa-1", "review-result-qa-orphan.json")
	if _, statErr := os.Stat(artifact); !os.IsNotExist(statErr) {
		t.Fatalf("rejected submit must clean its staged artifact, stat error=%v", statErr)
	}

	// The corrected resubmit (agent now working) must succeed on the same
	// result_id — no orphan in the way.
	revision = setAgentState("working", revision)
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	}); err != nil {
		t.Fatalf("resubmit after correction must succeed, got %v", err)
	}
}

func TestWriteArtifactNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := writeArtifact(root, "evidence/immutable.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifact(root, "evidence/immutable.json", []byte("second")); err == nil {
		t.Fatal("artifact writer must reject overwrite")
	}
	data, err := os.ReadFile(filepath.Join(root, "evidence", "immutable.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first" {
		t.Fatalf("artifact was overwritten: %q", data)
	}
}

// §14.1: code-inspection Finding 接受 inspection/call/data-flow trail，不
// 要求伪造 UI steps。
func TestSubmitResultAcceptsCodeInspectionWithoutUISteps(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	if finding.Encounter.Entrypoint != "" || len(finding.Encounter.Timeline) != 0 {
		t.Fatal("fixture must carry no UI steps")
	}
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err != nil {
		t.Fatalf("code-inspection finding without UI steps must be accepted: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "cannot_clean" {
		t.Fatalf("status = %s, want cannot_clean", ptr.Status)
	}
}

// §14.1: 间歇 Finding 只发生一次但有 deterministic trace —— 允许
// once_with_deterministic_trace，不为次数重复危险操作。
func TestSubmitResultAcceptsOnceWithDeterministicTrace(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Reproducibility = "once_with_deterministic_trace"
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	}); err != nil {
		t.Fatalf("once_with_deterministic_trace must be accepted: %v", err)
	}
}

// §14.1: 高危 Finding 不为补字段重复危险动作，但必须显式记录 capture
// gaps —— P0 缺 last-good 且无 capture_gaps 被拒绝。
func TestSubmitResultRejectsP0WithoutCaptureGaps(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	p0 := codeInspectionFinding("finding-p0-1", "claim-qa-1")
	p0.Severity = "P0"
	p0.Encounter.LastGoodCheckpoint = ""
	p0.Encounter.CaptureGaps = nil
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{p0})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "capture_gaps") {
		t.Fatalf("P0 without last-good and without capture gaps must be rejected, got %v", err)
	}
}

// §14.1: 两个表象疑似同根 —— S7 保留两个 source Findings，不合并、不
// 去重；sealed batch 携带 exact set。
func TestSubmitResultKeepsTwoFindingsForTheSameSymptom(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// QA and DV independently observe the same symptom on their own claims.
	qaFinding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	qaFinding.Observed = "error dropped: caller sees success on failure"
	dvFinding := codeInspectionFinding("finding-dv-1", "claim-dv-1")
	dvFinding.Lens = "delivery"
	dvFinding.Observed = "error dropped: caller sees success on failure"

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{qaFinding})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{"claim-dv-1": "fail"}, []Finding{dvFinding})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	batch := snap.State["review"].(map[string]any)["observation_batch"].(map[string]any)
	if batch == nil {
		t.Fatal("batch must seal once the final claim lands")
	}
	ids := batch["finding_ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("both source findings must survive in the exact set, got %v", ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id.(string)] = true
	}
	if !seen["finding-qa-1"] || !seen["finding-dv-1"] {
		t.Fatalf("exact set wrong: %v", ids)
	}
	batchPtr := batch
	batchBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(batchPtr["path"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	var batchArtifact map[string]any
	if err := json.Unmarshal(batchBytes, &batchArtifact); err != nil {
		t.Fatal(err)
	}
	drained := batchArtifact["drained_assignment_ids"].([]any)
	if !containsAnyString(drained, "assignment-qa-1") || !containsAnyString(drained, "assignment-dv-1") {
		t.Fatalf("sealed batch must include the assignment whose Result triggered sealing, got %v", drained)
	}
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// §14.1 状态机: 普通 finding 标 cannot_clean 后继续 drain；第二个普通
// finding 把轮推进 discovery_draining；最终 disposition 落地统一 seal，
// 且 ordinary batch 的 unobserved_claim_ids 为空。
func TestSubmitResultDrainsThroughDiscoveryDrainingBeforeSeal(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	agents := state["entities"].(map[string]any)["agents"].([]any)
	agents = append(agents, map[string]any{
		"id": "agent-qa-2", "role": "qa", "state": "working",
		"task_ids": []any{}, "team_id": "team-review-1",
		"definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-TEST/m.json#agent-qa-2",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-18T00:00:00Z",
	})
	state["entities"].(map[string]any)["agents"] = agents
	statePath, journalPath := writeState(t, root, state)

	planPath := writeThreeAssignmentPlan(t, root)
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-2", "agent-qa-2")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	// First ordinary finding: cannot_clean, drain continues.
	qa1 := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail"}, []Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qa1,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa-1: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "cannot_clean" {
		t.Fatalf("status = %s, want cannot_clean", ptr.Status)
	}

	// Second ordinary finding while claims remain: discovery_draining, still
	// not sealed.
	qa2 := writeResultFile(t, root, plan, "assignment-qa-2", "review-result-qa-2", "agent-qa-2", "finding",
		map[string]string{"claim-qa-2": "fail"}, []Finding{codeInspectionFinding("finding-qa-2", "claim-qa-2")})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-2", ResultPath: qa2,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa-2: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "discovery_draining" {
		t.Fatalf("status = %s, want discovery_draining", ptr.Status)
	}
	if snap.State["review"].(map[string]any)["observation_batch"] != nil {
		t.Fatal("batch must not seal while a required claim is still pending")
	}

	// Final disposition lands: the batch seals atomically with the exact
	// finding set and an empty unobserved_claim_ids (ordinary batches never
	// carry unobserved required Claims).
	dv := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dv,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "observation_sealed" {
		t.Fatalf("status = %s, want observation_sealed", ptr.Status)
	}
	batchPtr := snap.State["review"].(map[string]any)["observation_batch"].(map[string]any)
	batchBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(batchPtr["path"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	var batch map[string]any
	if err := json.Unmarshal(batchBytes, &batch); err != nil {
		t.Fatal(err)
	}
	if unobserved, _ := batch["unobserved_claim_ids"].([]any); len(unobserved) != 0 {
		t.Fatalf("ordinary batch must seal with empty unobserved_claim_ids, got %v", unobserved)
	}
	if batch["drain_policy"] != "complete_required_claims" {
		t.Fatalf("drain_policy = %v", batch["drain_policy"])
	}
	if ids, _ := batch["finding_ids"].([]any); len(ids) != 2 {
		t.Fatalf("batch must carry both findings, got %v", ids)
	}
}

// §14.1: seal/clean 之后不再接受任何 Result —— 终态是终态。
func TestSubmitResultRejectedAfterObservationSealed(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "observation_sealed" {
		t.Fatalf("precondition failed: status = %s", ptr.Status)
	}

	late := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-2", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: late,
	})
	if err == nil || !strings.Contains(err.Error(), "observation_sealed") {
		t.Fatalf("submit after seal must be rejected, got %v", err)
	}
}

// §14.1: 每个 Assignment 只提交一份 Canonical ReviewResult —— 第二次提交
// 被拒绝，CleanRound 不重复记录。
func TestSubmitResultRejectsSecondResultForSameAssignment(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := qaPassResult(t, root, plan)
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	again := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1b", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: again,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one Result") {
		t.Fatalf("second result for one assignment must be rejected, got %v", err)
	}
}

// §14.1: capture buffer —— --captures 并入空 timeline 的 finding；
// reviewer 自己写的 timeline 不被重写。
func TestSubmitResultMergesCaptureBufferIntoEmptyTimeline(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	buffer := CaptureFile(root, "loop-REQ-TEST", 1, "assignment-qa-1")
	if err := os.MkdirAll(filepath.Dir(buffer), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"sequence":1,"action":"open /settings","observed":"page rendered","evidence_refs":["` + fixtureEvidenceRef(t, root, "capture-shot-1.png") + `"]}
{"sequence":2,"action":"toggle flag","observed":"save failed","evidence_refs":["` + fixtureEvidenceRef(t, root, "capture-shot-2.png") + `"]}
`
	if err := os.WriteFile(buffer, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	if len(finding.Encounter.Timeline) != 0 {
		t.Fatal("precondition: empty timeline")
	}
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{finding})
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
		CaptureDir: buffer,
	}); err != nil {
		t.Fatalf("SubmitResult with capture buffer: %v", err)
	}
	persisted, err := os.ReadFile(findingArtifactPath(root, "finding-qa-1"))
	if err != nil {
		t.Fatal(err)
	}
	var stored Finding
	if err := json.Unmarshal(persisted, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Encounter.Timeline) != 2 || stored.Encounter.Timeline[1].ObservedCheckpoint != "save failed" {
		t.Fatalf("buffered steps must merge into the empty timeline: %+v", stored.Encounter.Timeline)
	}
	if len(stored.Encounter.Timeline[0].EvidenceRefs) != 1 {
		t.Fatalf("step-bound evidence refs must survive the merge: %+v", stored.Encounter.Timeline[0])
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// patchResultField rewrites one top-level field of a result JSON file.
func patchResultField(t *testing.T, path string, key string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	body[key] = value
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeThreeAssignmentPlan writes a plan with three required static claims in
// three assignments (dv-1, qa-1, qa-2) plus the N/A e2e claim.
func writeThreeAssignmentPlan(t *testing.T, root string) string {
	t.Helper()
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	planPath := writePlanFile(t, root)
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	// Split the two QA claims into their own assignments.
	for _, raw := range body["assignments"].([]any) {
		assignment := raw.(map[string]any)
		if assignment["assignment_id"] == "assignment-qa-1" {
			assignment["claim_ids"] = []any{"claim-qa-1"}
		}
	}
	body["assignments"] = append(body["assignments"].([]any), map[string]any{
		"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []any{"claim-qa-2"},
		"non_overlap_boundary": "owns the state-completeness focus", "execution_wave": "static",
	})
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return planPath
}
