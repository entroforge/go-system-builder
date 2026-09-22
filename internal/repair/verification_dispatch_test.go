package repair_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

// RC-18: a single-unit RepairContract (schema-legal, repair_units minItems=1)
// leaves the RC-01/RC-15 verifier pool holding only the repair owner's own
// identity, so no independent verifier can ever be bound. These tests pin the
// synthesized verification identity and prove the pair it produces passes the
// untouched identity gate while the pre-RC-18 spellings keep failing.
func TestVerificationDispatchIdentityNaming(t *testing.T) {
	const original = "repair-assignment-repair-unit-5209-single-seat"
	if got, want := repair.VerificationAssignmentID(original), "repair-assignment-verification-repair-unit-5209-single-seat"; got != want {
		t.Fatalf("VerificationAssignmentID = %q, want %q", got, want)
	}
	if got, want := repair.ManifestVerificationAssignmentID(original), "assignment-s9-verification-repair-unit-5209-single-seat"; got != want {
		t.Fatalf("ManifestVerificationAssignmentID = %q, want %q", got, want)
	}
	// The manifest spelling must be the alias of the plan-side spelling, in
	// both directions: the Runtime resolves either spelling to one owner.
	if got := repair.ManifestVerificationAssignmentID(original); got != "assignment-s9-"+strings.TrimPrefix(repair.VerificationAssignmentID(original), "repair-assignment-") {
		t.Fatalf("manifest spelling %q is not the register-compatible alias of %q", got, repair.VerificationAssignmentID(original))
	}
}

func TestValidateVerificationDispatchCases(t *testing.T) {
	plan := repair.RepairPlan{PlanID: "repair-plan-single-seat", Assignments: []repair.RepairAssignment{{AssignmentID: "repair-assignment-repair-unit-5209-single-seat", UnitIDs: []string{"unit-5209"}}}}
	verificationID := repair.VerificationAssignmentID(plan.Assignments[0].AssignmentID)
	cases := []struct {
		name     string
		plan     repair.RepairPlan
		original string
		verifier string
		owners   map[string]string
		want     string
		errPart  string
	}{
		{
			name: "synthesis succeeds for a claimed repair unit and a different agent",
			plan: plan, original: plan.Assignments[0].AssignmentID, verifier: "qa-targeted-reverify-5209",
			owners: map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat"},
			want:   verificationID,
		},
		{
			name: "manifest-alias spelling of the original resolves too",
			plan: plan, original: "assignment-s9-repair-unit-5209-single-seat", verifier: "qa-targeted-reverify-5209",
			owners: map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat"},
			want:   verificationID,
		},
		{
			name: "original outside the plan is refused",
			plan: plan, original: "repair-assignment-some-other-unit", verifier: "qa",
			owners:  map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat"},
			errPart: "is not an assignment of RepairPlan",
		},
		{
			name: "unowned repair unit cannot anchor a verifier",
			plan: plan, original: plan.Assignments[0].AssignmentID, verifier: "qa",
			owners:  map[string]string{},
			errPart: "has no recorded owner",
		},
		{
			name: "repair owner cannot verify its own repair",
			plan: plan, original: plan.Assignments[0].AssignmentID, verifier: "agent-repair-5209-single-seat",
			owners:  map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat"},
			errPart: "is not independent",
		},
		{
			name: "re-dispatch to the same verifier is refused as already dispatched",
			plan: plan, original: plan.Assignments[0].AssignmentID, verifier: "qa-targeted-reverify-5209",
			owners:  map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat", verificationID: "qa-targeted-reverify-5209"},
			errPart: "already dispatched",
		},
		{
			name: "ownership is never replaced mid-session",
			plan: plan, original: plan.Assignments[0].AssignmentID, verifier: "qa-other",
			owners:  map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat", verificationID: "qa-targeted-reverify-5209"},
			errPart: "already owned by Agent",
		},
		{
			name: "empty verifier agent is refused",
			plan: plan, original: plan.Assignments[0].AssignmentID, verifier: "  ",
			owners:  map[string]string{"repair-assignment-repair-unit-5209-single-seat": "agent-repair-5209-single-seat"},
			errPart: "requires --agent-id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repair.ValidateVerificationDispatch(tc.plan, tc.original, tc.verifier, tc.owners)
			if tc.errPart != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errPart) {
					t.Fatalf("error = %v, want containing %q", err, tc.errPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("verification assignment id = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSingleUnitContractVerificationPairBinds is the end-to-end proof against
// the untouched RC-01/RC-15 gate: the single-unit plan's own identity cannot
// verify itself (the pre-RC-18 deadlock), while the synthesized verification
// assignment — in plan-side and manifest spelling — binds as the independent
// performer.
func TestSingleUnitContractVerificationPairBinds(t *testing.T) {
	plan := repair.RepairPlan{PlanID: "repair-plan-single-seat", Assignments: []repair.RepairAssignment{{AssignmentID: "repair-assignment-repair-unit-5209-single-seat", UnitIDs: []string{"unit-5209"}}}}
	original := plan.Assignments[0].AssignmentID
	owners := map[string]string{original: "agent-repair-5209-single-seat"}

	// Deadlock: the only identity the pool holds is owned by the repair owner.
	deadlock := repair.TargetedReverification{OriginalAssignmentID: original, PerformingAssignmentID: "assignment-s9-repair-unit-5209-single-seat"}
	pointer := map[string]any{"assignment_owners": map[string]any{original: "agent-repair-5209-single-seat"}}
	if err := repair.BindTargetedReverificationIdentitiesForTest(pointer, plan, deadlock); err == nil {
		t.Fatal("single-unit plan must not admit its own repair identity as verifier")
	}

	// RC-18: the synthesized verification assignment, registered by the
	// ordinary dispatch path, supplies the missing independent identity.
	verificationID, err := repair.ValidateVerificationDispatch(plan, original, "qa-targeted-reverify-5209", owners)
	if err != nil {
		t.Fatalf("ValidateVerificationDispatch: %v", err)
	}
	bound := map[string]any{"assignment_owners": map[string]any{original: "agent-repair-5209-single-seat", verificationID: "qa-targeted-reverify-5209"}}
	if err := repair.BindTargetedReverificationIdentitiesForTest(bound, plan, repair.TargetedReverification{OriginalAssignmentID: original, PerformingAssignmentID: verificationID}); err != nil {
		t.Fatalf("plan-side verification identity must bind: %v", err)
	}
	if err := repair.BindTargetedReverificationIdentitiesForTest(bound, plan, repair.TargetedReverification{OriginalAssignmentID: original, PerformingAssignmentID: repair.ManifestVerificationAssignmentID(original)}); err != nil {
		t.Fatalf("manifest verification identity must bind: %v", err)
	}
	// Independence is still enforced: the repair owner can never be the
	// performing agent, whatever spelling is synthesized.
	if err := repair.BindTargetedReverificationIdentitiesForTest(map[string]any{"assignment_owners": map[string]any{original: "agent-repair-5209-single-seat", verificationID: "agent-repair-5209-single-seat"}}, plan, repair.TargetedReverification{OriginalAssignmentID: original, PerformingAssignmentID: verificationID}); err == nil || !strings.Contains(err.Error(), "not independent") {
		t.Fatalf("self-owned verification identity must stay rejected, got %v", err)
	}
}
