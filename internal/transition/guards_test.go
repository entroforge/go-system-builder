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
// the new angle_complete guards are explicitly replacing.
func TestLegacyEvidenceBackedStubsRemoved(t *testing.T) {
	legacy := []string{
		"delivery_team_complete", "delivery_round_passed",
		"qa_team_complete", "qa_round_passed",
		"e2e_team_complete", "e2e_round_passed",
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
		"req_exists", "req_locked", "req_questions_non_blocking",
		"pm_context_matches_req", "joint_document_pass",
		"verified_versions_current", "req_baseline_unchanged",
		"all_builder_tasks_in_review", "builder_reports_complete",
		"verification_team_manifest_complete", "blocking_findings_present",
		"acc_complete", "release_audit_approved",
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

// TestAngleCompleteEnforcementIsSemantic locks the registration strength
// asserted by FR-009: the three angle_complete guards are NOT stubs; they
// run semantic checks against on-disk evidence. Their enforcement must
// be GuardSemanticCheck, matching the README note in guards.go.
func TestAngleCompleteEnforcementIsSemantic(t *testing.T) {
	for _, name := range []string{"delivery_angle_complete", "qa_angle_complete", "e2e_angle_complete"} {
		registration, ok := transition.LookupGuardRegistration(name)
		if !ok {
			t.Fatalf("guard %s must be registered", name)
		}
		if registration.Enforcement != transition.GuardSemanticCheck {
			t.Errorf("guard %s enforcement = %q, want semantic_check", name, registration.Enforcement)
		}
	}
}
