package req039fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// S7 ReviewPlan fixtures (L3-S7): the verification stage runs on a registered
// ReviewPlan with an exact Claim set — not on per-lens aggregate envelopes.
// ---------------------------------------------------------------------------

// reviewRoundFromState reads the current review round (1 when unset).
func reviewRoundFromState(state map[string]any) int {
	review, _ := state["review"].(map[string]any)
	if review == nil {
		return 1
	}
	switch v := review["round"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 1
	}
}

// reviewPlanFixtureIDs are the canonical ids used by the S7 fixtures: three
// static assignments covering one claim each (delivery/qa/e2e).
var reviewPlanFixtureIDs = []struct {
	claimID      string
	assignmentID string
	lens         string
	agentID      string
}{
	{"claim-dv-1", "assignment-dv-1", "delivery", "agent-dv-1"},
	{"claim-qa-1", "assignment-qa-1", "qa", "agent-qa-1"},
	{"claim-e2e-1", "assignment-e2e-1", "e2e", "agent-e2e-1"},
}

// A frozen subject must have real bytes even when the test seeds upstream
// review facts directly. Prefer the actual registered task in a stage chain;
// never silently fill a missing registered source.
func reviewFrozenTaskSubject(t *testing.T, root string, state map[string]any) map[string]any {
	t.Helper()
	path := "docs/dev/tasks/TASK-039-01-loop-definition.md"
	registered := false
	if documents, ok := state["documents"].([]any); ok {
		for _, raw := range documents {
			doc, _ := raw.(map[string]any)
			if doc["kind"] == "task" {
				path, _ = doc["path"].(string)
				registered = true
				break
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if os.IsNotExist(err) && !registered {
		data = []byte("# TASK-039-01\n\n> Status: complete\n> Version: v1.0.2\n\nReviewed fixture delivery.\n")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(root, path), data, 0644)
	}
	if err != nil {
		t.Fatalf("freeze original fixture TASK %s: %v", path, err)
	}
	return map[string]any{"path": path, "kind": "task"}
}

// SeedReviewPlanRound registers a minimal ReviewPlan in the state and writes
// the pinned plan file under <root>/.claude/review/plans/. The phase moves to
// running. Reviewer agents are registered as working so review-result submit
// can advance them.
func SeedReviewPlanRound(t *testing.T, root string, state map[string]any) {
	t.Helper()
	EnsureStateRoot(state, root)
	round := reviewRoundFromState(state)
	if round < 1 {
		// Fixtures emulate the post-TR-006/TR-012 state: start_review_round
		// has already opened round 1.
		round = 1
	}

	claims := []any{}
	assignments := []any{}
	for _, spec := range reviewPlanFixtureIDs {
		claims = append(claims, map[string]any{
			"claim_id": spec.claimID, "lens": spec.lens, "target": "internal/example",
			"assertion": spec.claimID + " holds", "oracle": "observed evidence", "method": "review",
			"applicability": "required", "source_refs": []string{"REQ-039"},
		})
		assignments = append(assignments, map[string]any{
			"assignment_id": spec.assignmentID, "lens": spec.lens, "claim_ids": []string{spec.claimID},
			"non_overlap_boundary": "owns " + spec.claimID, "execution_wave": "static",
		})
	}
	planBody := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": "review-plan-fixture-1",
		"review_round": round, "baseline_generation": 1,
		"frozen_subjects": []any{reviewFrozenTaskSubject(t, root, state)},
		"claims":          claims, "assignments": assignments,
		"e2e_coverage_state":              "regression_available",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "orchestrator",
		"created_at":                      "2026-07-30T09:00:00Z",
	}
	planBytes := append(mustJSONIndent(t, planBody), '\n')
	planRel := ".claude/review/plans/review-plan-fixture-1.json"
	planAbs := filepath.Join(root, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planAbs, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	claimsProjection := map[string]any{}
	assignmentsProjection := map[string]any{}
	for _, spec := range reviewPlanFixtureIDs {
		claimsProjection[spec.claimID] = map[string]any{
			"lens": spec.lens, "applicability": "required", "disposition": "planned",
			"assignment_id": spec.assignmentID, "result_id": nil, "finding_ids": []any{},
		}
		assignmentsProjection[spec.assignmentID] = map[string]any{
			"lens": spec.lens, "claim_ids": []any{spec.claimID}, "status": "planned",
			"agent_id": nil, "result_ref": nil,
		}
	}
	state["review"] = map[string]any{
		"round": round, "clean_round": nil,
		"plan": map[string]any{
			"plan_id": "review-plan-fixture-1", "path": planRel,
			"revision": 1, "review_round": round,
			"status": "running", "e2e_coverage_state": "regression_available",
			"verification_artifact_workspace": nil, "submitted_at": "2026-07-30T09:00:00Z",
		},
		"claims":      claimsProjection,
		"assignments": assignmentsProjection,
	}
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "running", "phase_revision": 1}
	if milestone, ok := state["milestone"].(map[string]any); ok {
		milestone["stage"] = "S7"
		milestone["lifecycle_state"] = "verification"
		milestone["lifecycle_phase"] = "running"
	}
	entities, _ := state["entities"].(map[string]any)
	if entities == nil {
		entities = map[string]any{}
		state["entities"] = entities
	}
	entities["findings"] = []any{}
	agents, _ := entities["agents"].([]any)
	for _, spec := range reviewPlanFixtureIDs {
		agents = append(agents, map[string]any{
			"id": spec.agentID, "role": spec.lens + "-reviewer", "state": "working",
			"task_ids": []any{}, "team_id": "team-review-1",
			"definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-039/m.json#" + spec.assignmentID,
			"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-07-30T09:00:00Z",
		})
	}
	entities["agents"] = agents
}

// SeedReviewResultPass marks one fixture assignment consumed with a pass
// result (the projection the review-result submit verb would write).
func SeedReviewResultPass(t *testing.T, root string, state map[string]any, assignmentID string) {
	t.Helper()
	round := reviewRoundFromState(state)
	var spec *struct {
		claimID      string
		assignmentID string
		lens         string
		agentID      string
	}
	for i := range reviewPlanFixtureIDs {
		if reviewPlanFixtureIDs[i].assignmentID == assignmentID {
			spec = &reviewPlanFixtureIDs[i]
		}
	}
	if spec == nil {
		t.Fatalf("unknown fixture assignment %s", assignmentID)
	}
	envelope := EvidenceEnvelope(state, "ev-result-"+spec.assignmentID, "review_result", spec.agentID, responsibilityForLens(spec.lens), "pass", map[string]any{
		"review_round": round,
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-result-"+spec.assignmentID, "review_result", spec.agentID, responsibilityForLens(spec.lens), envelope, []any{}))
	reviewMap := state["review"].(map[string]any)
	claims := reviewMap["claims"].(map[string]any)
	claims[spec.claimID].(map[string]any)["disposition"] = "pass"
	claims[spec.claimID].(map[string]any)["result_id"] = "ev-result-" + spec.assignmentID
	reviewMap["assignments"].(map[string]any)[spec.assignmentID].(map[string]any)["status"] = "consumed"
}

// SeedCleanRoundReady puts the round into the clean state the round consumer
// produces: every claim passed, machine CleanRound evidence registered.
func SeedCleanRoundReady(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedReviewPlanRound(t, root, state)
	for _, spec := range reviewPlanFixtureIDs {
		SeedReviewResultPass(t, root, state, spec.assignmentID)
	}
	round := reviewRoundFromState(state)
	reviewMap := state["review"].(map[string]any)
	reviewMap["clean_round"] = round
	reviewMap["plan"].(map[string]any)["status"] = "clean"
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "clean", "phase_revision": 2}
	if milestone, ok := state["milestone"].(map[string]any); ok {
		milestone["lifecycle_phase"] = "clean"
	}
	envelope := EvidenceEnvelope(state, "clean-round-r1", "clean_round", "round-consumer", "Clean Round Evaluator", "pass", map[string]any{
		"review_round": round,
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "clean-round-r1", "clean_round", "round-consumer", "Clean Round Evaluator", envelope, []any{}))
}

// SeedAcceptedReviewFinding records one current original Finding for S8.
func SeedAcceptedReviewFinding(t *testing.T, root string, state map[string]any) {
	t.Helper()
	EnsureStateRoot(state, root)
	const planID = "review-plan-fixture-finding"
	finding := map[string]any{
		"schema_version": "1.0.0", "finding_id": "finding-qa-1", "claim_id": "claim-qa-1", "lens": "qa", "severity": "P1",
		"expected": "errors propagate", "authority_refs": []string{"CONTRACTS-039"}, "observed": "error dropped", "observation_mode": "code_inspection",
		"encounter":       map[string]any{"journey_summary": "read -> trace -> found drop", "inspection_entry": "internal/example", "symbol_trail": "a -> b", "last_good_checkpoint": "boundary holds", "wall_action": "drop at line 9", "first_bad_checkpoint": "nil after failure", "terminal_state": "success reported"},
		"reproducibility": "always", "evidence_refs": []string{"evidence/code-trail.md"},
	}
	findingRel := writeEvidenceFile(t, root, "finding-qa-1.json", append(mustJSONIndent(t, finding), '\n'))
	entities := state["entities"].(map[string]any)
	entities["findings"] = []any{map[string]any{"finding_id": "finding-qa-1", "path": findingRel, "review_plan_id": planID, "claim_id": "claim-qa-1", "assignment_id": "assignment-qa-1", "lens": "qa", "severity": "P1", "observation_mode": "code_inspection", "original_finder": "agent-qa-1", "created_at": "2026-07-30T10:00:00Z"}}
	state["review"] = map[string]any{"round_entry": map[string]any{"plan_id": planID, "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}}
	// This is the current S7 exit cursor; it contains no batch or count.
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "observation_sealed", "phase_revision": 2}
	if milestone, ok := state["milestone"].(map[string]any); ok {
		milestone["stage"], milestone["lifecycle_state"], milestone["lifecycle_phase"] = "S7", "verification", "observation_sealed"
	}
}

// SeedConflictingPauseVerdicts registers two qualified review_result verdicts
// (req_change_required + release_blocked) at the running cursor so the
// selector must report a conflict (CT-039-16).
func SeedConflictingPauseVerdicts(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedReviewPlanRound(t, root, state)
	round := reviewRoundFromState(state)
	add := func(id, conclusion string) {
		envelope := EvidenceEnvelope(state, id, "review_result", "agent-qa-1", "QA", conclusion, map[string]any{
			"review_round": round,
		})
		AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, id, "review_result", "agent-qa-1", "QA", envelope, []any{}))
	}
	add("ev-verdict-req", "req_change_required")
	add("ev-verdict-release", "release_blocked")
}

// SeedCleanRoundProjection overlays the machine-clean-round projection the
// S7 round consumer leaves behind, without moving the lifecycle. Acceptance
// (S10) and release-audit (S11) fixtures need it because TR-015/TR-017's
// clean_round_still_valid guard recomputes the CleanRound from the
// ReviewPlan projection (L3-S7 §10).
func SeedCleanRoundProjection(t *testing.T, root string, state map[string]any) {
	t.Helper()
	EnsureStateRoot(state, root)
	round := reviewRoundFromState(state)
	if round < 1 {
		round = 1
	}

	planBody := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": "review-plan-fixture-1",
		"review_round": round, "baseline_generation": 1,
		"frozen_subjects": []any{reviewFrozenTaskSubject(t, root, state)},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-qa-1", "lens": "qa", "target": "internal/example",
				"assertion": "holds", "oracle": "observed", "method": "review",
				"applicability": "required", "source_refs": []string{"REQ-039"},
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-qa-1", "lens": "qa", "claim_ids": []string{"claim-qa-1"},
				"non_overlap_boundary": "owns claim-qa-1", "execution_wave": "static",
			},
		},
		"e2e_coverage_state":              "not_applicable",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"coverage_justification":          "fixture round with a single QA claim",
		"created_by":                      "orchestrator",
		"created_at":                      "2026-07-30T09:00:00Z",
	}
	planBytes := append(mustJSONIndent(t, planBody), '\n')
	planRel := ".claude/review/plans/review-plan-fixture-1.json"
	planAbs := filepath.Join(root, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planAbs, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
		reviewMap = map[string]any{}
		state["review"] = reviewMap
	}
	reviewMap["round"] = round
	reviewMap["clean_round"] = round
	reviewMap["plan"] = map[string]any{
		"plan_id": "review-plan-fixture-1", "path": planRel,
		"revision": 1, "review_round": round,
		"status": "clean", "e2e_coverage_state": "not_applicable",
		"verification_artifact_workspace": nil, "submitted_at": "2026-07-30T09:00:00Z",
	}
	reviewMap["claims"] = map[string]any{
		"claim-qa-1": map[string]any{
			"lens": "qa", "applicability": "required", "disposition": "pass",
			"assignment_id": "assignment-qa-1", "result_id": "ev-result-assignment-qa-1", "finding_ids": []any{},
		},
	}
	reviewMap["assignments"] = map[string]any{
		"assignment-qa-1": map[string]any{
			"lens": "qa", "claim_ids": []any{"claim-qa-1"}, "status": "consumed",
			"agent_id": "agent-qa-1", "result_ref": "evidence/ev-result-assignment-qa-1.json",
		},
	}
	envelope := EvidenceEnvelope(state, "clean-round-r1", "clean_round", "round-consumer", "Clean Round Evaluator", "pass", map[string]any{
		"review_round": round,
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "clean-round-r1", "clean_round", "round-consumer", "Clean Round Evaluator", envelope, []any{}))
}

func responsibilityForLens(lens string) string {
	switch lens {
	case "delivery":
		return "Delivery Verifier"
	case "qa":
		return "QA"
	case "e2e":
		return "E2E Browser"
	}
	return ""
}

func mustJSONIndent(t *testing.T, body map[string]any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
