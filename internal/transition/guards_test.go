// guards_test.go exercises the legacy evidenceBackedGuard stub removal
// mandated by TASK-003-C (REQ-003 FR-009). The three legacy DV/QA/E2E
// phase guards are gone; the evidenceBackedGuard helper itself is
// preserved for every other declarative guard.
package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/verification"
)

// TestLegacyEvidenceBackedStubsRemoved asserts FR-009: the legacy guard ids
// `delivery_team_complete`, `delivery_round_passed`, `qa_team_complete`,
// `qa_round_passed`, `e2e_team_complete`, `e2e_round_passed` are NOT
// registered. A re-introduction would silently re-enable the stub behavior
// the retired angle_complete guards once replaced. The four TR-001
// stub guards were removed for the same reason (L3-S1 v4.x): their
// semantics live in the bind prechecks and the human lock. L3-S6 P0-4
// removed the three TR-006 batch stubs the same way — the exact-set
// evaluation lives in GATE-BUILDER-BATCH-READY.
func TestLegacyEvidenceBackedStubsRemoved(t *testing.T) {
	legacy := []string{
		"delivery_team_complete", "delivery_round_passed",
		"qa_team_complete", "qa_round_passed",
		"e2e_team_complete", "e2e_round_passed",
		"req_exists", "req_locked", "req_questions_non_blocking",
		"pm_context_matches_req",
		"all_builder_tasks_in_review", "builder_reports_complete",
		"verification_team_manifest_complete",
		// RC-06 (S7-4): registered-but-never-declared stub names — their
		// real semantics live in verification.EvaluateCleanRound and the
		// sealed-batch/clean-round guards the definition actually wires.
		"all_required_dimensions_passed", "same_review_round",
		"no_invalidated_pass_evidence", "no_open_blocking_bugs",
		"verification_phase_clean_round_passed",
		"blocking_findings_present",
	}
	for _, name := range legacy {
		if _, ok := transition.LookupGuard(name); ok {
			t.Errorf("legacy guard %s must be removed after TASK-003-C", name)
		}
	}
}

// TestEvidenceBackedGuardHelperPreserved asserts FR-009 + Q-005: the
// evidenceBackedGuard helper itself is preserved (the legacy id was
// only REPLACED in three places, not deleted wholesale). The other
// declarative guards that depend on it must still pass through LookupGuard
// with enforcement = evidence_attestation.
func TestEvidenceBackedGuardHelperPreserved(t *testing.T) {
	declarativeGuards := []string{
		"joint_document_pass",
		"verified_versions_current",
		"task_manifest_complete", "task_closing_contract_passed",
	}
	for _, name := range declarativeGuards {
		registration, ok := transition.LookupGuardRegistration(name)
		if !ok {
			t.Errorf("declarative guard %s must remain registered", name)
			continue
		}
		if registration.Enforcement != transition.GuardEvidenceAttestation {
			t.Errorf("declarative guard %s enforcement = %q, want evidence_attestation",
				name, registration.Enforcement)
		}
	}
}

// TestSemanticGuardsResolvedAndHashed locks RC-06 (S10-14): acc_complete and
// release_audit_approved are no longer evidenceBackedGuard attestation stubs —
// they are semantic checks that resolve a current evidence entry of the right
// kind and re-hash its on-disk artifact. An empty evidence list must reject.
func TestSemanticGuardsResolvedAndHashed(t *testing.T) {
	for _, name := range []string{"acc_complete", "release_audit_approved"} {
		registration, ok := transition.LookupGuardRegistration(name)
		if !ok {
			t.Fatalf("guard %s must remain registered", name)
		}
		if registration.Enforcement != transition.GuardSemanticCheck {
			t.Fatalf("guard %s enforcement = %q, want semantic_check", name, registration.Enforcement)
		}
		if err := registration.Fn(map[string]any{"evidence": []any{}}, nil); err == nil {
			t.Fatalf("guard %s must reject an empty evidence list", name)
		}
	}
}

// TestForbiddenEventsBlocksACCWithoutCleanRound locks RC-06 (S10-2): the
// strong_block forbidden_events table is now consumed at Apply time. TR-015
// (acceptance→release_audit) must be refused when the machine CleanRound
// does not pass, no matter what evidence refs the caller supplies — the
// journal must stay untouched.
func TestForbiddenEventsBlocksACCWithoutCleanRound(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtAcceptanceMap(7)
	writeFullState(t, root, state)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")

	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-015",
		ExpectedRevision: 7,
		Actor:            "orchestrator",
		Evidence: map[string]string{
			"acceptance_record":  "ev-acc",
			"clean_round_record": "docs/reports/review/clean-round.md",
		},
	})
	if err == nil {
		t.Fatal("TR-015 must be rejected when no valid clean round exists (forbidden event create_acc_without_clean_round)")
	}
	if !containsStr(err.Error(), "create_acc_without_clean_round") {
		t.Fatalf("error must name the forbidden event, got: %v", err)
	}
	// The refusal must be pre-Apply: revision unchanged, no journal row.
	data, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var after map[string]any
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	if got := after["revision"].(float64); got != 7 {
		t.Fatalf("revision moved to %v for a refused mutation; the barrier must be pre-Apply", got)
	}
	journal, _ := os.ReadFile(journalPath)
	if len(strings.TrimSpace(string(journal))) != 0 {
		t.Fatalf("journal must stay empty for a forbidden-event refusal, got %d bytes", len(journal))
	}
}

// TestForbiddenEventsBlocksReleaseAuditWithoutACC extends the S10-2 barrier
// to TR-017: the release audit cannot reach the human release boundary on a
// failed CleanRound evaluation either.
func TestForbiddenEventsBlocksReleaseAuditWithoutACC(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtReleaseAuditMap(9)
	writeFullState(t, root, state)

	err := applyT(t, root, "TR-017", 9, "release_auditor", map[string]string{
		"release_audit_record": "docs/release_audits/audit.md",
		"acceptance_record":    "ev-acc",
		"clean_round_record":   "docs/reports/review/clean-round.md",
	})
	if err == nil {
		t.Fatal("TR-017 must be rejected when no valid clean round exists (forbidden event run_release_audit_without_acc)")
	}
	if !containsStr(err.Error(), "forbidden event") {
		t.Fatalf("error must name the forbidden event, got: %v", err)
	}
}

// TestForbiddenEventBarrierPassesWithValidCleanRound proves the S10-2 barrier
// is a real predicate, not an unconditional TR-015/TR-017 deny: a state whose
// CleanRound evaluates PASS gets past the forbidden-event check (the Apply may
// still fail later on evidence resolution, but never with the forbidden-event
// error).
func TestForbiddenEventBarrierPassesWithValidCleanRound(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtAcceptanceMap(7)
	state["review"] = map[string]any{
		"round":       float64(1),
		"clean_round": float64(1),
		"plan": map[string]any{
			"plan_id": "PLAN-1", "path": "docs/review/plan.md", "sha256": "x",
			"review_round": float64(1), "status": "clean",
		},
		"claims": map[string]any{
			"CLM-1": map[string]any{
				"lens": "delivery", "applicability": "applicable",
				"disposition": "pass", "result_id": "RES-1",
			},
		},
	}
	state["evidence"] = []any{
		map[string]any{
			"id": "ev-cr", "kind": "clean_round", "status": "valid",
			"baseline_generation": float64(1), "review_round": float64(1),
			"path": "docs/reports/review/clean-round.md", "sha256": "x",
			"produced_by": []any{"a"}, "responsibility_id": "VER-1", "scope_refs": []any{},
			"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		},
	}
	if result := verification.EvaluateCleanRound(state); !result.Passed {
		t.Fatalf("fixture must evaluate as a valid CleanRound before the Apply: %v", result.Reasons)
	}
	writeFullState(t, root, state)

	err := applyT(t, root, "TR-015", 7, "orchestrator", map[string]string{
		"acceptance_record":  "ev-acc",
		"clean_round_record": "docs/reports/review/clean-round.md",
	})
	if err != nil && containsStr(err.Error(), "create_acc_without_clean_round") {
		t.Fatalf("forbidden-event barrier must pass a valid CleanRound, got: %v", err)
	}
}

// stateAtAcceptanceMap returns a minimal acceptance-phase state for the S10-2
// forbidden-event barrier tests (no clean round registered).
func stateAtAcceptanceMap(rev int) map[string]any {
	state := stateAtVerificationMap(rev)
	state["lifecycle"] = map[string]any{"state": "acceptance", "phase": nil, "phase_revision": float64(1)}
	return state
}

// stateAtReleaseAuditMap mirrors stateAtAcceptanceMap for the release_audit state.
func stateAtReleaseAuditMap(rev int) map[string]any {
	state := stateAtVerificationMap(rev)
	state["lifecycle"] = map[string]any{"state": "release_audit", "phase": nil, "phase_revision": float64(1)}
	return state
}

// TestAngleLifecycleRemoved locks the L3-S7 retirement: the angle_complete
// guards (and their vocabulary) must stay out of the registry — their intent
// lives in ReviewPlan Claims, enforced by the plan validator.
func TestAngleLifecycleRemoved(t *testing.T) {
	for _, name := range []string{"delivery_angle_complete", "qa_angle_complete", "e2e_angle_complete"} {
		if _, ok := transition.LookupGuard(name); ok {
			t.Errorf("retired angle guard %s must not be registered", name)
		}
	}
}
