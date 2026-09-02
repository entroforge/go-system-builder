package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ---------------------------------------------------------------------------
// fixture helpers
// ---------------------------------------------------------------------------

func writeState(t *testing.T, root string, state map[string]any) (string, string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The candidate validator reads the authoritative definition from
	// <root>/docs/loop-definition.json; mirror the repository's copy.
	defData, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), defData, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return statePath, journalPath
}

// baseVerificationState returns a schema-valid runtime in verification with
// an open round 1 and two reviewer agents in working state. It starts from
// the embedded loop-state example (full required shape) and patches the
// fields the S7 verbs read.
func baseVerificationState() map[string]any {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		panic(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		panic(err)
	}
	state["revision"] = 1
	state["runtime_id"] = "loop-REQ-TEST"
	lifecycle := state["lifecycle"].(map[string]any)
	lifecycle["state"] = "verification"
	lifecycle["phase"] = "planned"
	state["baseline"].(map[string]any)["generation"] = 1
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	agent := func(id, role string) map[string]any {
		return map[string]any{
			"id": id, "role": role, "state": "working",
			"task_ids": []any{}, "team_id": "team-review-1",
			"definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-TEST/m.json#" + id,
			"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-18T00:00:00Z",
		}
	}
	state["entities"] = map[string]any{
		"agents": []any{agent("agent-dv-1", "delivery-verifier"), agent("agent-qa-1", "qa")},
		"tasks":  []any{},
		"bugs":   []any{},
		"teams":  []any{},
	}
	state["evidence"] = []any{}
	state["documents"] = []any{}
	return state
}

// writePlanFile writes a plan JSON to a temp file and returns the path.
// The plan has: 1 DV claim (assignment-dv-1), 2 QA claims (assignment-qa-1),
// one N/A e2e claim; all static wave.
func writePlanFile(t *testing.T, root string) string {
	t.Helper()
	// The registration/submit gates verify frozen_subjects against disk. Keep
	// this fixture honest by creating the subject and pinning its real digest.
	subjectPath := filepath.Join(root, "internal", "example", "service.go")
	if err := os.MkdirAll(filepath.Dir(subjectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	subjectBytes := []byte("fixture baseline")
	if err := os.WriteFile(subjectPath, subjectBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      "review-plan-t-1",
		"review_round":        1,
		"baseline_generation": 1,
		"frozen_subjects": []any{
			map[string]any{"path": "internal/example/service.go", "sha256": sha256Of(subjectBytes), "kind": "product_code"},
		},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-dv-1", "lens": "delivery",
				"target": "internal/example", "assertion": "REQ covered", "oracle": "AC maps to code",
				"method": "traceability", "applicability": "required", "source_refs": []string{"REQ-001"},
			},
			map[string]any{
				"claim_id": "claim-qa-1", "lens": "qa",
				"target": "internal/example", "assertion": "errors propagate", "oracle": "no dropped error",
				"method": "code review", "applicability": "required", "source_refs": []string{"CONTRACTS-001"},
			},
			map[string]any{
				"claim_id": "claim-qa-2", "lens": "qa",
				"target": "internal/example", "assertion": "states complete", "oracle": "no orphan transition",
				"method": "state walk", "applicability": "required", "source_refs": []string{"ARCH-001"},
			},
			map[string]any{
				"claim_id": "claim-e2e-na", "lens": "e2e",
				"target": "n/a", "assertion": "no user surface", "oracle": "impact shows internal only",
				"method": "impact analysis", "applicability": "not_applicable",
				"na_rationale": "pure internal change", "na_checklist_id": "REQ-001#ui_impact", "source_refs": []string{"REQ-001#ui"},
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-dv-1", "lens": "delivery", "claim_ids": []string{"claim-dv-1"},
				"non_overlap_boundary": "owns traceability", "execution_wave": "static",
			},
			map[string]any{
				"assignment_id": "assignment-qa-1", "lens": "qa", "claim_ids": []string{"claim-qa-1", "claim-qa-2"},
				"non_overlap_boundary": "owns logic/state", "execution_wave": "static",
			},
		},
		"e2e_coverage_state":              "not_applicable",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "orchestrator",
		"created_at":                      "2026-08-18T00:00:00Z",
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plan.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// registerFixturePlan registers the fixture plan and returns the new snapshot.
func registerFixturePlan(t *testing.T, root, statePath, journalPath string) loopruntime.Snapshot {
	t.Helper()
	// Findings built after this point bind real evidence artifacts (S7-11).
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	planPath := writePlanFile(t, root)
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: -1,
		PlanPath:         planPath,
	})
	if err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}
	return snap
}

// markDispatched simulates the register-workgroup binding: agent assigned.
func markDispatched(t *testing.T, root, statePath, journalPath string, snapshot loopruntime.Snapshot, assignmentID, agentID string) loopruntime.Snapshot {
	t.Helper()
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	next, err := store.Update(snapshot.Revision, loopruntime.Mutation{
		EventID:        "evt-test-dispatch-" + assignmentID,
		TransitionID:   "TEST",
		Event:          "test_dispatch",
		Actor:          "test",
		IdempotencyKey: "test:dispatch:" + assignmentID,
		Apply: func(state map[string]any) error {
			reviewMap := state["review"].(map[string]any)
			row := reviewMap["assignments"].(map[string]any)[assignmentID].(map[string]any)
			row["status"] = "dispatched"
			row["agent_id"] = agentID
			return nil
		},
	})
	if err != nil {
		t.Fatalf("markDispatched: %v", err)
	}
	return next
}

// fixtureEvidenceRef writes a real evidence artifact under the repository and
// returns its typed path: reference with the digest bound (S7-11/RC-07: bare
// refs no longer satisfy the evidence gate, so fixtures bind real files).
func fixtureEvidenceRef(t *testing.T, root, name string) string {
	t.Helper()
	rel := "docs/reports/" + name
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("fixture evidence: " + name + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return "path:" + rel + "#sha256=" + sha256Of(content)
}

// writeResultFile builds a ReviewResult JSON covering the given claim
// conclusions (claimID -> conclusion) with optional findings.
func writeResultFile(t *testing.T, root string, plan *Plan, assignmentID, resultID, producer, verdict string, conclusions map[string]string, findings []Finding) string {
	t.Helper()
	claimResults := []any{}
	for claimID, conclusion := range conclusions {
		claimResults = append(claimResults, map[string]any{
			"claim_id": claimID, "conclusion": conclusion,
			"observed": "observed " + claimID, "evidence_refs": []string{fixtureEvidenceRef(t, root, claimID+"-observed.md")},
		})
	}
	payload := map[string]any{
		"schema_version":      "1.0.0",
		"result_id":           resultID,
		"assignment_id":       assignmentID,
		"assignment_revision": 1,
		"review_plan_id":      plan.ReviewPlanID,
		"review_round":        plan.ReviewRound,
		"baseline_generation": plan.BaselineGeneration,
		"producer_agent_id":   producer,
		"subject_digest":      SubjectDigest(plan),
		"claim_results":       claimResults,
		"verdict":             verdict,
	}
	if len(findings) > 0 {
		payload["findings"] = findings
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, resultID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixtureEvidenceRoot is set by dispatchedFixture (and any other test that
// builds findings against a temp repository root) so codeInspectionFinding
// can bind its evidence to a real artifact on disk (S7-11/RC-07: bare ghost
// refs no longer satisfy the evidence gate).
var fixtureEvidenceRoot string

func codeInspectionFinding(findingID, claimID string) Finding {
	ref := ""
	if fixtureEvidenceRoot != "" {
		rel := "docs/reports/" + findingID + "-code-trail.md"
		path := filepath.Join(fixtureEvidenceRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			panic(err)
		}
		content := []byte("fixture code-inspection evidence for " + findingID + "\n")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			panic(err)
		}
		ref = "path:" + rel + "#sha256=" + sha256Of(content)
	}
	return Finding{
		SchemaVersion:   "1.0.0",
		FindingID:       findingID,
		ClaimID:         claimID,
		Lens:            "qa",
		Severity:        "P1",
		Expected:        "expected behavior per contract",
		AuthorityRefs:   []string{"CONTRACTS-001"},
		Observed:        "observed deviation",
		ObservationMode: "code_inspection",
		Encounter: Encounter{
			JourneySummary:     "read code -> trace call -> found deviation",
			InspectionEntry:    "internal/example/service.go",
			SymbolTrail:        "Update -> store.Write",
			LastGoodCheckpoint: "caller boundary holds",
			WallAction:         "error dropped at line 87",
			FirstBadCheckpoint: "nil return after failure",
			TerminalState:      "success reported on failure",
		},
		Reproducibility: "always",
		EvidenceRefs:    []string{ref},
	}
}

// ---------------------------------------------------------------------------
// RegisterPlan
// ---------------------------------------------------------------------------

func TestRegisterPlanHappyPath(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)

	ptr := PlanPointerFromState(snap.State)
	if ptr == nil || ptr.PlanID != "review-plan-t-1" || ptr.Status != "running" {
		t.Fatalf("plan pointer wrong: %+v", ptr)
	}
	lifecycle := snap.State["lifecycle"].(map[string]any)
	if lifecycle["phase"] != "running" {
		t.Fatalf("phase = %v, want running", lifecycle["phase"])
	}
	dispositions := Dispositions(snap.State)
	if dispositions["claim-dv-1"].Disposition != "planned" {
		t.Fatalf("claim-dv-1 disposition = %s", dispositions["claim-dv-1"].Disposition)
	}
	if dispositions["claim-e2e-na"].Disposition != "not_applicable" {
		t.Fatalf("N/A claim disposition = %s", dispositions["claim-e2e-na"].Disposition)
	}
	assignments := snap.State["review"].(map[string]any)["assignments"].(map[string]any)
	if assignments["assignment-qa-1"].(map[string]any)["status"] != "planned" {
		t.Fatalf("assignment-qa-1 not planned")
	}
}

func TestRegisterPlanRejectsWrongStage(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["lifecycle"].(map[string]any)["state"] = "building"
	statePath, journalPath := writeState(t, root, state)
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: writePlanFile(t, root)})
	if err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("expected stage rejection, got %v", err)
	}
}

func TestRegisterPlanStaleRevisionDoesNotWritePinnedPlan(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 0, PlanPath: planPath})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale plan registration must fail before writing control-plane artifact, got %v", err)
	}
	pinned := filepath.Join(root, ".claude", "review", "plans", "review-plan-t-1.json")
	if _, statErr := os.Stat(pinned); !os.IsNotExist(statErr) {
		t.Fatalf("stale registration must not leave a pinned plan, stat error=%v", statErr)
	}
}

func TestQueuedAssignmentIsReleasedWhenLockHolderIsConsumed(t *testing.T) {
	state := map[string]any{
		"entities": map[string]any{
			"agents": []any{map[string]any{"id": "agent-queued", "state": "queued"}},
		},
		"review": map[string]any{
			"assignments": map[string]any{
				"assignment-holder": map[string]any{
					"status": "consumed", "resource_locks": []any{"port:8080"}, "result_ref": "result-holder",
				},
				"assignment-queued": map[string]any{
					"status": "planned", "resource_locks": []any{"port:8080"}, "result_ref": nil,
					"queued_agent_id": "agent-queued", "queue_reason": "resource_lock:port:8080",
					"claim_ids": []any{"claim-queued"},
				},
			},
			"claims": map[string]any{
				"claim-queued": map[string]any{"disposition": "planned"},
			},
		},
	}
	if err := releaseQueuedReviewAssignments(state); err != nil {
		t.Fatalf("release queued assignment: %v", err)
	}
	row := state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-queued"].(map[string]any)
	if row["status"] != "dispatched" || row["agent_id"] != "agent-queued" || row["queue_reason"] != nil {
		t.Fatalf("queued assignment was not released: %v", row)
	}
	claim := state["review"].(map[string]any)["claims"].(map[string]any)["claim-queued"].(map[string]any)
	if claim["disposition"] != "running" {
		t.Fatalf("queued claim disposition = %v, want running", claim["disposition"])
	}
	agent := state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	if agent["state"] != "reading" {
		t.Fatalf("released queued agent must be woken into reading state, got %v", agent["state"])
	}
}

func TestRegisterPlanRejectsDuplicate(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: snap.Revision, PlanPath: writePlanFile(t, root)})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
}

func TestValidatePlanCoverageRules(t *testing.T) {
	root := t.TempDir()
	base, err := os.ReadFile(writePlanFile(t, root))
	if err != nil {
		t.Fatal(err)
	}
	load := func() *Plan {
		var plan Plan
		if err := json.Unmarshal(base, &plan); err != nil {
			t.Fatal(err)
		}
		return &plan
	}

	// unassigned required claim
	plan := load()
	plan.Assignments[0].ClaimIDs = nil
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "no owning Assignment") {
		t.Fatalf("expected unassigned claim rejection, got %v", err)
	}
	// claim in two assignments (same lens, so the partition check fires)
	plan = load()
	plan.Assignments = append(plan.Assignments, PlanAssignment{
		AssignmentID: "assignment-qa-2", Lens: "qa", ClaimIDs: []string{"claim-qa-1"},
		NonOverlapBoundary: "duplicates qa claim", ExecutionWave: "static",
	})
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "partitioned exactly") {
		t.Fatalf("expected double-assignment rejection, got %v", err)
	}
	// lens merge
	plan = load()
	plan.Assignments[0].ClaimIDs = append(plan.Assignments[0].ClaimIDs, "claim-qa-1")
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "lens") {
		t.Fatalf("expected lens-mix rejection, got %v", err)
	}
	// N/A dispatched
	plan = load()
	plan.Assignments = append(plan.Assignments, PlanAssignment{
		AssignmentID: "assignment-e2e-1", Lens: "e2e", ClaimIDs: []string{"claim-e2e-na"},
		NonOverlapBoundary: "n/a", ExecutionWave: "behavior",
	})
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "not_applicable") {
		t.Fatalf("expected N/A dispatch rejection, got %v", err)
	}
	// dependency cycle
	plan = load()
	plan.Claims[1].DependsOn = []string{"claim-qa-2"}
	plan.Claims[2].DependsOn = []string{"claim-qa-1"}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle rejection, got %v", err)
	}
	// A dependency cannot be satisfied by another Claim in the same
	// Assignment: the Assignment has only one Result boundary, so there is no
	// upstream Result to consume before it starts.
	plan = load()
	plan.Claims[1].DependsOn = []string{"claim-qa-2"}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "same Assignment") {
		t.Fatalf("expected same-Assignment dependency rejection, got %v", err)
	}
	// Execution waves are semantic, not just labels: white-box delivery/QA
	// work is static and behavior work is reserved for E2E/specialty lenses.
	plan = load()
	plan.Assignments[1].ExecutionWave = "behavior"
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "behavior wave") {
		t.Fatalf("expected QA behavior-wave rejection, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SubmitResult
// ---------------------------------------------------------------------------

func TestSubmitResultCleanRoundOnFinalPass(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	// First result: QA pass — round stays running.
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	ptr := PlanPointerFromState(snap.State)
	if ptr.Status != "running" {
		t.Fatalf("status = %s, want running (DV claim still pending)", ptr.Status)
	}

	// Final required claim: DV pass — machine CleanRound closes the round.
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	ptr = PlanPointerFromState(snap.State)
	if ptr.Status != "clean" {
		t.Fatalf("status = %s, want clean", ptr.Status)
	}
	if snap.State["review"].(map[string]any)["clean_round"] != 1 {
		t.Fatalf("clean_round not set: %v", snap.State["review"])
	}
	lifecycle := snap.State["lifecycle"].(map[string]any)
	if lifecycle["phase"] != "clean" {
		t.Fatalf("phase = %v, want clean", lifecycle["phase"])
	}
	// clean_round evidence registered
	found := false
	for _, raw := range snap.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["kind"] == "clean_round" && entry["status"] == "valid" {
			found = true
		}
	}
	if !found {
		t.Fatal("no clean_round evidence registered")
	}
}

func TestSubmitResultFindingSealsObservationBatch(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	// QA reports one fail claim with a finding (the second claim passes).
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	ptr := PlanPointerFromState(snap.State)
	if ptr.Status != "cannot_clean" {
		t.Fatalf("status = %s, want cannot_clean", ptr.Status)
	}
	// Finding entity registered.
	findings := snap.State["entities"].(map[string]any)["findings"].([]any)
	if len(findings) != 1 || findings[0].(map[string]any)["finding_id"] != "finding-qa-1" {
		t.Fatalf("findings not registered: %v", findings)
	}
	// Round not sealed yet — DV claim pending (drain continues).
	if snap.State["review"].(map[string]any)["observation_batch"] != nil {
		t.Fatal("batch sealed before the final claim disposition; ordinary findings drain first")
	}

	// DV pass lands the final disposition -> batch seals atomically.
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	ptr = PlanPointerFromState(snap.State)
	if ptr.Status != "observation_sealed" {
		t.Fatalf("status = %s, want observation_sealed", ptr.Status)
	}
	batch := snap.State["review"].(map[string]any)["observation_batch"].(map[string]any)
	if batch == nil || batch["drain_policy"] != "complete_required_claims" {
		t.Fatalf("batch missing or wrong drain policy: %v", batch)
	}
	ids := batch["finding_ids"].([]any)
	if len(ids) != 1 || ids[0] != "finding-qa-1" {
		t.Fatalf("batch finding set wrong: %v", ids)
	}
}

func TestSubmitResultP0SealsImmediately(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	// A queued Assignment is already registered in the runtime, but must not
	// be promoted by the same transaction that seals an observation batch.
	// P0 means stop new dispatch immediately; the queue must remain planned.
	reviewMap := snap.State["review"].(map[string]any)
	reviewMap["assignments"].(map[string]any)["assignment-queued"] = map[string]any{
		"lens": "qa", "claim_ids": []any{"claim-queued"}, "status": "planned",
		"agent_id": nil, "result_ref": nil, "queued_agent_id": "agent-queued",
		"queue_reason": "resource_lock:port:8080", "resource_locks": []any{"port:8080"},
	}
	reviewMap["claims"].(map[string]any)["claim-queued"] = map[string]any{
		"lens": "qa", "applicability": "required", "disposition": "planned",
		"assignment_id": "assignment-queued", "result_id": nil, "finding_ids": []any{},
	}
	stateBytes, err := json.MarshalIndent(snap.State, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _, _ := LoadPlan(root, snap.State)

	p0 := codeInspectionFinding("finding-p0-1", "claim-qa-1")
	p0.Severity = "P0"
	p0.Encounter.CaptureGaps = []string{"stopped before touching the destructive path"}
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{p0})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult: %v", err)
	}
	ptr := PlanPointerFromState(snap.State)
	if ptr.Status != "observation_sealed" {
		t.Fatalf("status = %s, want observation_sealed (immediate stop)", ptr.Status)
	}
	batch := snap.State["review"].(map[string]any)["observation_batch"].(map[string]any)
	if batch["drain_policy"] != "immediate_stop" {
		t.Fatalf("drain_policy = %v", batch["drain_policy"])
	}
	queued := snap.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-queued"].(map[string]any)
	if queued["status"] != "planned" || queued["agent_id"] != nil {
		t.Fatalf("P0 seal must not release queued dispatch: %v", queued)
	}
}

func TestSubmitResultRejectsClaimSetMismatch(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)

	// Missing claim-qa-2.
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass"}, nil)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "missing claim-qa-2") {
		t.Fatalf("expected exact-set rejection, got %v", err)
	}
}

// RC-02 (L3-S7 §10.1): blocking is a business judgment carried with the
// Finding. A P0 Finding is implicitly blocking=true and must not claim
// blocking=false; a non-P0 Finding may carry blocking=true and keeps the
// marker through the entity row.
func TestSubmitResultRejectsP0FindingWithBlockingFalse(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)

	p0 := codeInspectionFinding("finding-p0-1", "claim-qa-1")
	p0.Severity = "P0"
	no := false
	p0.Blocking = &no
	p0.Encounter.CaptureGaps = []string{"stopped before the destructive path"}
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{p0})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "blocking=false") {
		t.Fatalf("P0 with blocking=false must be rejected, got %v", err)
	}
}

func TestSubmitResultCarriesBlockingMarkerIntoFindingEntity(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)

	yes := true
	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Blocking = &yes // P1, business-blocking
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult: %v", err)
	}
	rows := snap.State["entities"].(map[string]any)["findings"].([]any)
	if len(rows) != 1 {
		t.Fatalf("finding rows = %d, want 1", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["blocking"] != true {
		t.Fatalf("blocking marker not persisted on the finding entity row: %v", row)
	}
}

func TestSubmitResultRejectsBuilderProducer(t *testing.T) {
	root := t.TempDir()
	// A completion_report from agent-qa-1 this generation makes it a Builder.
	// RC-16: the registered artifact must exist on disk with a matching
	// sha256 — the S7 projection no longer bridges a missing artifact with
	// the agent-controllable scope_refs.
	envelopeRel := ".claude/evidence/x.json"
	envelopeAbs := filepath.Join(root, filepath.FromSlash(envelopeRel))
	if err := os.MkdirAll(filepath.Dir(envelopeAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := []byte(`{"kind":"completion_report","changed_paths":[]}`)
	if err := os.WriteFile(envelopeAbs, envelope, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(envelope)
	state := baseVerificationState()
	state["evidence"] = []any{map[string]any{
		"id": "ev-completion-1", "kind": "completion_report", "baseline_generation": 1,
		"produced_by": []any{"agent-qa-1"}, "status": "valid",
		"path": envelopeRel, "sha256": hex.EncodeToString(sum[:]),
		"review_round": nil, "invalidated_by": nil, "invalidation_rule": nil,
		"invalidation_reason": nil, "responsibility_id": "BUILD-WORK-PACKAGE", "scope_refs": []any{},
	}}
	statePath, journalPath := writeState(t, root, state)
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "independence") {
		t.Fatalf("expected independence rejection, got %v", err)
	}
}

func TestSubmitResultRejectsVerdictContradiction(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, nil)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("expected verdict contradiction rejection, got %v", err)
	}
}

func TestSubmitResultPauseVerdictCreatesCheckpoint(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "req_change_required",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult: %v", err)
	}
	pause := snap.State["pause"].(map[string]any)
	if pause == nil || pause["from_state"] != "verification" {
		t.Fatalf("pause checkpoint not created: %v", snap.State["pause"])
	}
	ptr := PlanPointerFromState(snap.State)
	if ptr.Status != "paused" {
		t.Fatalf("status = %s, want paused", ptr.Status)
	}
}

// ---------------------------------------------------------------------------
// RevisePlan (L3-S7 §5.3: one controlled revision, source+surface bound)
// ---------------------------------------------------------------------------

// reviseFixturePlan writes a v2 plan adding one claim under the given
// surface and returns the path.
func reviseFixturePlan(t *testing.T, root string, plan *Plan, extraClaim map[string]any, extraAssignment map[string]any, sourceRefs []string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "review", "plans", plan.ReviewPlanID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	claims := body["claims"].([]any)
	if extraClaim != nil {
		extraClaim["source_refs"] = sourceRefs
		claims = append(claims, extraClaim)
	}
	body["claims"] = claims
	if extraAssignment != nil {
		body["assignments"] = append(body["assignments"].([]any), extraAssignment)
	}
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plan-v2.json")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func revisionSourceState() map[string]any {
	state := baseVerificationState()
	state["evidence"] = []any{map[string]any{
		"id":                  "review-result-qa-1",
		"kind":                "review_result",
		"path":                ".claude/evidence/review-result-qa-1.json",
		"sha256":              strings.Repeat("a", 64),
		"status":              "valid",
		"baseline_generation": 1,
		"review_round":        1,
		"produced_by":         []any{"agent-qa-1"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "QA",
		"scope_refs":          []any{"internal/example"},
	}}
	return state
}

func TestRevisePlanHappyPath(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, revisionSourceState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, _ := LoadPlan(root, snap.State)

	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "new surface covered", "oracle": "observed", "method": "review",
			"applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	next, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	ptr := PlanPointerFromState(next.State)
	if ptr.Revision != 2 {
		t.Fatalf("revision = %d, want 2", ptr.Revision)
	}
	dispositions := Dispositions(next.State)
	if dispositions["claim-qa-3"].Disposition != "planned" {
		t.Fatalf("new claim disposition = %s", dispositions["claim-qa-3"].Disposition)
	}
	// untouched claims keep their disposition
	if dispositions["claim-dv-1"].Disposition != "planned" {
		t.Fatalf("unchanged claim drifted: %s", dispositions["claim-dv-1"].Disposition)
	}
}

func TestRevisePlanRejectsSecondRevision(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, revisionSourceState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, _ := LoadPlan(root, snap.State)
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "covered", "oracle": "observed", "method": "review", "applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	next, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatalf("first revision: %v", err)
	}
	_, err = RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: next.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err == nil || !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("second revision must fail, got %v", err)
	}
}

func TestRevisePlanRejectsOutOfSurfaceChange(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, revisionSourceState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, _ := LoadPlan(root, snap.State)
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/other",
			"assertion": "covered", "oracle": "observed", "method": "review", "applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	_, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err == nil || !strings.Contains(err.Error(), "outside the affected surface") {
		t.Fatalf("out-of-surface change must fail, got %v", err)
	}
}

func TestRevisePlanRejectsMissingSourceRef(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, _ := LoadPlan(root, snap.State)
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "covered", "oracle": "observed", "method": "review", "applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"REQ-001"}, // wrong source: must be the triggering evidence id
	)
	_, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err == nil || !strings.Contains(err.Error(), "source_ref") {
		t.Fatalf("missing source binding must fail, got %v", err)
	}
}

func TestRevisePlanRejectsGhostSourceRef(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, _ := LoadPlan(root, snap.State)
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "covered", "oracle": "observed", "method": "review", "applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"ghost-result-999"},
	)
	_, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "ghost-result-999", AffectedSurface: "internal/example",
	})
	if err == nil || !strings.Contains(err.Error(), "S7_REVISION_SOURCE") || !strings.Contains(err.Error(), "ghost-result-999") {
		t.Fatalf("revision must reject a ghost source_ref with a repair diagnostic, got %v", err)
	}
}

func TestRevisePlanRejectsPreviousRoundSource(t *testing.T) {
	root := t.TempDir()
	state := revisionSourceState()
	state["evidence"].([]any)[0].(map[string]any)["review_round"] = 2
	statePath, journalPath := writeState(t, root, state)
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "new surface covered", "oracle": "observed", "method": "review",
			"applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	_, err = RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err == nil || !strings.Contains(err.Error(), "review_round") || !strings.Contains(err.Error(), "current round") {
		t.Fatalf("wrong-round source must be rejected with its round boundary, got %v", err)
	}
}

func TestRevisePlanCleansArtifactAfterNonStaleApplyFailure(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, revisionSourceState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "new surface covered", "oracle": "observed", "method": "review",
			"applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	broken := snap.State["review"].(map[string]any)
	broken["claims"] = nil
	stateBytes, err := json.MarshalIndent(snap.State, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	}); err == nil {
		t.Fatal("broken review projection must make the CAS apply fail")
	}
	artifact := filepath.Join(root, ".claude", "review", "plans", plan.ReviewPlanID+"-r2.json")
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("failed revision artifact still exists: %v", err)
	}
}

func TestRevisePlanRetainsArtifactWhenRuntimeCommitIsPending(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, revisionSourceState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "new surface covered", "oracle": "observed", "method": "review",
			"applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	if err := os.WriteFile(statePath+".commit-pending.json", []byte(`{"schema_version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	}); err == nil {
		t.Fatal("pending runtime commit must make revise fail closed")
	}
	artifact := filepath.Join(root, ".claude", "review", "plans", plan.ReviewPlanID+"-r2.json")
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("pending commit cleanup removed a potentially reachable artifact: %v", err)
	}
}

func TestDiffClaimsRejectsSurfacePrefixCollision(t *testing.T) {
	v1 := &Plan{Claims: []Claim{{ClaimID: "claim-1", Target: "internal/example", SourceRefs: []string{"review-result-1"}}}}
	v2 := &Plan{Claims: []Claim{{ClaimID: "claim-1", Target: "internal/example2", SourceRefs: []string{"review-result-1"}}}}
	if _, err := diffClaims(v1, v2, "review-result-1", "internal/example"); err == nil || !strings.Contains(err.Error(), "outside the affected surface") {
		t.Fatalf("surface prefix collision must be rejected, got %v", err)
	}
}

func TestRevisePlanRechecksCurrentTaskCoverage(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	// A new S6 task may appear after registration but before the controlled
	// revision. The v2 plan must not silently omit it.
	snap.State["documents"] = []any{map[string]any{
		"id": "TASK-099", "kind": "task", "generation": 1,
	}}
	stateBytes, err := json.MarshalIndent(snap.State, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	v2 := reviseFixturePlan(t, root, plan,
		map[string]any{
			"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example",
			"assertion": "new surface covered", "oracle": "observed", "method": "review",
			"applicability": "required",
		},
		map[string]any{
			"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"},
			"non_overlap_boundary": "owns the new surface", "execution_wave": "static",
		},
		[]string{"review-result-qa-1"},
	)
	if _, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	}); err == nil || !strings.Contains(err.Error(), "TASK-099") {
		t.Fatalf("revision must recheck current task coverage, got %v", err)
	}
}

func TestRevisePlanInvalidatesConsumedResultsOnChangedClaims(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, _ := LoadPlan(root, snap.State)
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Revise: modify claim-qa-1's assertion (same target/surface, with source).
	data, _ := os.ReadFile(filepath.Join(root, ".claude", "review", "plans", plan.ReviewPlanID+".json"))
	var body map[string]any
	json.Unmarshal(data, &body)
	claims := body["claims"].([]any)
	for _, raw := range claims {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["assertion"] = "errors propagate AND surface in the typed result"
			claim["source_refs"] = []string{"review-result-qa-1"}
		}
	}
	out, _ := json.MarshalIndent(body, "", "  ")
	v2 := filepath.Join(root, "plan-v2.json")
	os.WriteFile(v2, append(out, '\n'), 0o644)

	next, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: snap.Revision, PlanPath: v2,
		SourceRef: "review-result-qa-1", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	// claim-qa-1 back to planned; its consumed result invalidated.
	dispositions := Dispositions(next.State)
	if dispositions["claim-qa-1"].Disposition != "planned" {
		t.Fatalf("changed claim disposition = %s", dispositions["claim-qa-1"].Disposition)
	}
	if dispositions["claim-qa-2"].Disposition != "pass" {
		t.Fatalf("unchanged claim must keep pass, got %s", dispositions["claim-qa-2"].Disposition)
	}
	assignment := next.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	if assignment["status"] != "planned" || assignment["agent_id"] != nil {
		t.Fatalf("changed assignment must be redispatched from planned with no stale Agent binding: %v", assignment)
	}
	for _, raw := range next.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == "review-result-qa-1" && entry["status"] != "invalid" {
			t.Fatalf("consumed result must be invalidated after revision, got %v", entry["status"])
		}
	}
}
