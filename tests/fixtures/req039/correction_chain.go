package req039fixtures

import (
	"testing"
)

// WriteRootCauseComplete appends root_cause_record evidence for PTR-BUG-01.
func WriteRootCauseComplete(t *testing.T, root string, state map[string]any) {
	t.Helper()
	envelope := EvidenceEnvelope(state, "ev-root-cause", "bug", "investigator-1", "Investigator", "complete", nil)
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-root-cause", "bug", "investigator-1", "Investigator", envelope, []any{}))
}

// WriteBugBatchAccepted appends accepted bug_batch_record for PTR-BUG-02.
func WriteBugBatchAccepted(t *testing.T, root string, state map[string]any) {
	t.Helper()
	envelope := EvidenceEnvelope(state, "ev-bug-batch", "bug", "orchestrator-1", "Orchestrator", "accepted", nil)
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-bug-batch", "bug", "orchestrator-1", "Orchestrator", envelope, []any{}))
}

// WriteRepairActivationApproved appends activation_record for PTR-BUG-04.
func WriteRepairActivationApproved(t *testing.T, root string, state map[string]any) {
	t.Helper()
	envelope := EvidenceEnvelope(state, "ev-activation", "agent_activation", "orchestrator-1", "Orchestrator", "approved", nil)
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-activation", "agent_activation", "orchestrator-1", "Orchestrator", envelope, []any{}))
}

// WriteRepairBatchReported appends repair + change_impact evidence for PTR-BUG-05.
func WriteRepairBatchReported(t *testing.T, root string, state map[string]any) {
	t.Helper()
	repair := EvidenceEnvelope(state, "ev-repair", "bug", "builder-1", "BUILD-WORK-PACKAGE", "reported", nil)
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-repair", "bug", "builder-1", "BUILD-WORK-PACKAGE", repair, []any{}))
	impact := EvidenceEnvelope(state, "ev-change-impact", "change_impact", "builder-1", "BUILD-WORK-PACKAGE", "recorded", nil)
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-change-impact", "change_impact", "builder-1", "BUILD-WORK-PACKAGE", impact, []any{}))
}

// WriteTargetedReverificationPass appends pass evidence for PTR-BUG-06 / TR-012.
func WriteTargetedReverificationPass(t *testing.T, root string, state map[string]any) {
	t.Helper()
	round := reviewRoundFromState(state)
	envelope := EvidenceEnvelope(state, "ev-tgt-pass", "targeted_reverification", "finder-1", "Original Finder", "pass", map[string]any{
		"review_round": round,
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-tgt-pass", "targeted_reverification", "finder-1", "Original Finder", envelope, []any{}))
}

// SeedFindingSpecChangeRequired seeds bug_resolution.bug_report_review with
// bug_batch spec_change_required + change_impact (TR-023).
func SeedFindingSpecChangeRequired(t *testing.T, root string, state map[string]any) {
	t.Helper()
	round := reviewRoundFromState(state)
	if round < 1 {
		round = 1
	}
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "bug_report_review", "phase_revision": 0}
	state["review"] = map[string]any{"round": round, "clean_round": nil}
	if ms, ok := state["milestone"].(map[string]any); ok {
		ms["stage"] = "S8"
		ms["lifecycle_state"] = "bug_resolution"
		ms["lifecycle_phase"] = "bug_report_review"
	}
	batch := EvidenceEnvelope(state, "ev-finding-spec", "bug", "orchestrator-1", "Orchestrator", "spec_change_required", map[string]any{
		"review_round":    round,
		"requested_event": "finding_spec_change_required",
	})
	impact := EvidenceEnvelope(state, "ev-finding-impact", "change_impact", "orchestrator-1", "Orchestrator", "recorded", map[string]any{
		"review_round": round,
	})
	state["evidence"] = []any{
		WriteEvidenceEnvelope(t, root, state, "ev-finding-spec", "bug", "orchestrator-1", "Orchestrator", batch, []any{}),
		WriteEvidenceEnvelope(t, root, state, "ev-finding-impact", "change_impact", "orchestrator-1", "Orchestrator", impact, []any{}),
	}
}

// SeedFindingReqChangeRequired seeds bug_resolution.bug_report_review with
// bug_batch req_change_required + pause_record (TR-024).
// Uses schema-valid evidence.kind=human_decision (pause_record alias).
func SeedFindingReqChangeRequired(t *testing.T, root string, state map[string]any) {
	t.Helper()
	round := reviewRoundFromState(state)
	if round < 1 {
		round = 1
	}
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "bug_report_review", "phase_revision": 0}
	state["review"] = map[string]any{"round": round, "clean_round": nil}
	if ms, ok := state["milestone"].(map[string]any); ok {
		ms["stage"] = "S8"
		ms["lifecycle_state"] = "bug_resolution"
		ms["lifecycle_phase"] = "bug_report_review"
	}
	batch := EvidenceEnvelope(state, "ev-finding-req", "bug", "orchestrator-1", "Orchestrator", "req_change_required", map[string]any{
		"review_round":    round,
		"requested_event": "finding_req_change_required",
	})
	pause := EvidenceEnvelope(state, "ev-finding-pause", "human_decision", "orchestrator-1", "Orchestrator", "recorded", map[string]any{
		"review_round": round,
	})
	state["evidence"] = []any{
		WriteEvidenceEnvelope(t, root, state, "ev-finding-req", "bug", "orchestrator-1", "Orchestrator", batch, []any{}),
		WriteEvidenceEnvelope(t, root, state, "ev-finding-pause", "human_decision", "orchestrator-1", "Orchestrator", pause, []any{}),
	}
}
