package review

// supplement_gate_enumerate_test.go — regression tests for the
// per-field enumeration in enforceSupplementGate's error messages
// (L3-S7 §14.1, L3-S8 §2.2). The fix replaces the single-line "missing
// one of three" rejection with a per-field breakdown so the caller
// can fix every gap in a single edit instead of doing half-experiments
// field by field.
//
// The previous message buried all three field names in one clause, so a
// caller testing each fix could not tell which field was the actual
// cause. The new message lists each missing field on its own line and
// describes what the field is for.

import (
	"strings"
	"testing"
)

// TestEnforceSupplementGateEnumeratesEveryMissingField is the headline
// regression: with hypothesis_id + discriminator + expected_outcomes all
// missing, the gate must enumerate all three with a brief description
// of each. The caller must be able to fix every gap in a single edit.
func TestEnforceSupplementGateEnumeratesEveryMissingField(t *testing.T) {
	sup := Supplement{HypothesisID: "", Discriminator: ""}
	err := enforceSupplementGate(sup, nil, false)
	if err == nil {
		t.Fatal("expected gate rejection, got nil")
	}
	msg := err.Error()
	for _, keyword := range []string{
		"hypothesis_id",
		"discriminator",
		"expected_outcomes",
		"L3-S8 §2.2",
		"L3-S7 §14.1",
	} {
		if !strings.Contains(msg, keyword) {
			t.Errorf("gate must mention %q, got:\n%s", keyword, msg)
		}
	}
	// Multi-line per-field breakdown: every missing field gets its own
	// "- missing …" line so the caller can grep / editor-jump to fix them.
	for _, marker := range []string{"- missing hypothesis_id", "- missing discriminator", "- missing expected_outcomes"} {
		if !strings.Contains(msg, marker) {
			t.Errorf("gate must enumerate %q on its own line, got:\n%s", marker, msg)
		}
	}
}

// TestEnforceSupplementGateMissingHypothesisOnly isolates the
// single-field case: with only hypothesis_id missing, the gate must
// surface hypothesis_id alone (not lump it together with the other
// two). This is the "fix one and resubmit" path the previous message
// obscured.
func TestEnforceSupplementGateMissingHypothesisOnly(t *testing.T) {
	sup := Supplement{Discriminator: "nil vs empty store distinguishes H1 from H2"}
	err := enforceSupplementGate(sup, []string{"nil store panics"}, false)
	if err == nil {
		t.Fatal("expected gate rejection, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "- missing hypothesis_id") {
		t.Errorf("single-field gate must list hypothesis_id, got:\n%s", msg)
	}
	// The other fields are present, so they must NOT be listed as missing —
	// otherwise the caller wastes time fixing fields that already have
	// values.
	for _, present := range []string{"- missing discriminator", "- missing expected_outcomes"} {
		if strings.Contains(msg, present) {
			t.Errorf("present field must NOT appear as missing (%q), got:\n%s", present, msg)
		}
	}
}

// TestEnforceSupplementGateMissingDiscriminatorOnly exercises the
// discriminator-only gap and confirms the gate names discriminator on
// its own line. Reviewers and S8 agents reading the error must be able
// to identify the single missing field without re-reading the spec.
func TestEnforceSupplementGateMissingDiscriminatorOnly(t *testing.T) {
	sup := Supplement{HypothesisID: "hyp-1"}
	err := enforceSupplementGate(sup, []string{"nil store panics"}, false)
	if err == nil {
		t.Fatal("expected gate rejection, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "- missing discriminator") {
		t.Errorf("single-field gate must list discriminator, got:\n%s", msg)
	}
	for _, present := range []string{"- missing hypothesis_id", "- missing expected_outcomes"} {
		if strings.Contains(msg, present) {
			t.Errorf("present field must NOT appear as missing (%q), got:\n%s", present, msg)
		}
	}
}

// TestEnforceSupplementGateMissingOutcomesOnly exercises the
// expected_outcomes-only gap, including that the description explains
// what expected_outcomes is for (distinguishing outcomes that support
// vs refute the hypothesis).
func TestEnforceSupplementGateMissingOutcomesOnly(t *testing.T) {
	sup := Supplement{
		HypothesisID:  "hyp-1",
		Discriminator: "nil vs empty store distinguishes H1 from H2",
	}
	err := enforceSupplementGate(sup, nil, false)
	if err == nil {
		t.Fatal("expected gate rejection, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "- missing expected_outcomes") {
		t.Errorf("single-field gate must list expected_outcomes, got:\n%s", msg)
	}
	for _, present := range []string{"- missing hypothesis_id", "- missing discriminator"} {
		if strings.Contains(msg, present) {
			t.Errorf("present field must NOT appear as missing (%q), got:\n%s", present, msg)
		}
	}
}

// TestEnforceSupplementGateAcceptsCompleteBinding guards the positive
// path: with all three fields populated, the gate must return nil and
// the previous per-field breakdown must not leak into the message.
func TestEnforceSupplementGateAcceptsCompleteBinding(t *testing.T) {
	sup := Supplement{
		HypothesisID:  "hyp-1",
		Discriminator: "nil vs empty store distinguishes H1 from H2",
	}
	err := enforceSupplementGate(sup, []string{"nil store panics"}, false)
	if err != nil {
		t.Fatalf("complete binding must pass the gate, got: %v", err)
	}
}

// TestEnforceSupplementGateInRoundNoteExempt verifies the S7
// in-round-note exemption is unchanged by the new per-field breakdown:
// a payload with all three fields missing + --in-round-note is still
// accepted, and the error path with partial binding still mentions
// the spec anchors.
func TestEnforceSupplementGateInRoundNoteExempt(t *testing.T) {
	sup := Supplement{}
	err := enforceSupplementGate(sup, nil, true)
	if err != nil {
		t.Fatalf("in-round-note with all fields empty must pass, got: %v", err)
	}
}

// TestEnforceSupplementGateInRoundNotePartialBinding enumerates the
// partial-binding error inside the in-round-note branch: when
// --in-round-note is set but a discriminator-bound field sneaks in
// without hypothesis_id, the message must still mention the spec
// anchors (L3-S8 §2.2, L3-S7 §14.1) so a confused caller can find the
// rationale.
func TestEnforceSupplementGateInRoundNotePartialBinding(t *testing.T) {
	sup := Supplement{Discriminator: "disc-only"}
	err := enforceSupplementGate(sup, nil, true)
	if err == nil {
		t.Fatal("in-round-note + partial binding must reject")
	}
	msg := err.Error()
	for _, keyword := range []string{"L3-S8 §2.2", "L3-S7 §14.1", "--in-round-note"} {
		if !strings.Contains(msg, keyword) {
			t.Errorf("partial-binding error must mention %q, got:\n%s", keyword, msg)
		}
	}
}

// TestEnforceSupplementGateInRoundNoteRejectsHypothesis guards the
// in-round-note + hypothesis_id contradiction: the rejection must
// still surface hypothesis_id by name so a caller who mis-declared the
// flag can fix it without reading source code.
func TestEnforceSupplementGateInRoundNoteRejectsHypothesis(t *testing.T) {
	sup := Supplement{HypothesisID: "hyp-1"}
	err := enforceSupplementGate(sup, nil, true)
	if err == nil {
		t.Fatal("in-round-note + hypothesis_id must reject")
	}
	if !strings.Contains(err.Error(), "hypothesis_id") {
		t.Errorf("contradiction must name hypothesis_id, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--in-round-note") {
		t.Errorf("contradiction must mention --in-round-note flag, got: %v", err)
	}
}
