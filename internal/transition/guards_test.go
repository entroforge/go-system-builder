// guards_test.go exercises the legacy evidenceBackedGuard stub removal
// mandated by TASK-003-C (REQ-003 FR-009). The three legacy DV/QA/E2E
// phase guards are gone; the evidenceBackedGuard helper itself is
// preserved for every other declarative guard.
package transition_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
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

// The S10 transitions consume their accepted professional reports without a second S7 verification gate.
func TestS10TransitionsDoNotRecheckS7(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"TR-015", "TR-017"} {
		spec, ok := catalog.Transitions[id]
		if !ok {
			t.Fatalf("missing %s", id)
		}
		for _, guard := range spec.Guards {
			if guard == "clean_round_still_valid" || guard == "clean_round_valid" {
				t.Fatalf("%s retains old S7 gate %s", id, guard)
			}
		}
	}
	if _, ok := transition.LookupGuard("clean_round_still_valid"); ok {
		t.Fatal("old repeated S7 guard remains registered")
	}
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
