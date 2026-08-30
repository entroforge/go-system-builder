package controller

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestEvidenceKindCompatibleRequirementEnvelopeAliases(t *testing.T) {
	tests := []struct {
		required, actual string
		want             bool
	}{
		{"finding_record", "bug", true},
		{"finding_record", "finding", true}, // L3-S7: immutable Finding kind

		{"finding_record", "bug_batch_record", false},
		{"root_cause_record", "bug", true},
		{"root_cause_record", "root_cause", false},
		{"repair_record", "bug", true},
		{"repair_record", "repair", false},
		{"bug_batch_record", "bug", true},
		{"team_manifest_record", "builder_report", true},
		{"team_manifest_record", "team_manifest", true},
		{"team_manifest_record", "document_review", false},
		{"completion_report", "agent_completion", true},
		{"completion_report", "completion_report", true},
		{"builder_report_record", "builder_report", true},
		{"builder_report_record", "agent_completion", true},
		{"clean_round_record", "clean_round", true},
		{"targeted_reverification_record", "targeted_reverification", true},
		{"document_review_record", "document_review", true},
		{"delivery_review_record", "delivery_review", true},
		{"contract_set_record", "document_review", true},
		{"task_batch_record", "document_review", true},
		{"activation_record", "agent_activation", true},
		{"activation_record", "activation", false},
		{"change_impact_record", "change_impact", true},
		{"arbitrary_kind", "other_kind", false},
	}
	for _, tc := range tests {
		if got := evidenceKindCompatible(tc.required, tc.actual); got != tc.want {
			t.Errorf("evidenceKindCompatible(%q, %q) = %v, want %v", tc.required, tc.actual, got, tc.want)
		}
	}
}

func TestEvidenceKindPreferenceFollowsCatalogAndFailsClosed(t *testing.T) {
	catalog := evidence.DefaultCatalog()

	for _, actual := range []string{"team_manifest", "team_manifest_record", "builder_report", "document_review", "unknown_kind"} {
		want := catalog.IsPreferred("team_manifest_record", actual)
		if got := evidenceKindPreferred("team_manifest_record", actual); got != want {
			t.Fatalf("preference(%q) = %v, want catalog result %v", actual, got, want)
		}
	}
}

func TestBuildTransitionEvidenceUsesCatalogGeneratedReference(t *testing.T) {
	got := buildTransitionEvidence(runtime.Snapshot{}, &transition.TransitionSpec{
		RequiredEvidence: []string{"pause_record"},
	}, qualitygate.Evaluation{})
	if got["pause_record"] != "generated:pause_checkpoint" {
		t.Fatalf("pause_record generated reference = %q, want catalog reference", got["pause_record"])
	}
}

// BUG-039-35: OrganicSpine after TR-006 keeps S6 builder_report (round=1) while
// WriteVerificationDimensionPass adds team_manifest (round=2). PTR-VERIFY-01
// must bind the current-round team_manifest, not the stale builder_report alias.
func TestBuildTransitionEvidencePrefersCurrentRoundTeamManifest(t *testing.T) {
	snapshot := runtime.Snapshot{
		Revision: 1,
		State: map[string]any{
			"review": map[string]any{"round": 2, "clean_round": nil},
			"evidence": []any{
				map[string]any{
					"id": "ev-team", "kind": "builder_report", "path": "evidence/ev-team.json",
					"status": "valid", "baseline_generation": 1, "review_round": 1,
				},
				map[string]any{
					"id": "ev-manifest-delivery", "kind": "team_manifest", "path": "evidence/ev-manifest-delivery.json",
					"status": "valid", "baseline_generation": 1, "review_round": 2,
				},
				map[string]any{
					"id": "ev-delivery-pass", "kind": "delivery_review", "path": "evidence/ev-delivery-pass.json",
					"status": "valid", "baseline_generation": 1, "review_round": 2,
				},
			},
		},
	}
	candidate := &transition.TransitionSpec{
		ID:               "PTR-VERIFY-01",
		RequiredEvidence: []string{"team_manifest_record", "delivery_review_record"},
	}
	// Gate only surfaces the delivery review; index scan must still prefer team_manifest.
	evaluation := qualitygate.Evaluation{
		EvidenceRefs: []string{"ev-delivery-pass"},
	}

	got := buildTransitionEvidence(snapshot, candidate, evaluation)
	if got["team_manifest_record"] != "ev-manifest-delivery" {
		t.Fatalf("team_manifest_record = %q, want ev-manifest-delivery (not S6 builder_report)", got["team_manifest_record"])
	}
	if got["delivery_review_record"] != "ev-delivery-pass" {
		t.Fatalf("delivery_review_record = %q, want ev-delivery-pass", got["delivery_review_record"])
	}
}

// TR-006 still needs builder_report as a team_manifest_record fallback when no
// team_manifest envelope exists (GATE-BUILDER-BATCH-READY / SeedBuilderBatchReady).
func TestBuildTransitionEvidenceFallsBackToBuilderReportForTeamManifest(t *testing.T) {
	snapshot := runtime.Snapshot{
		Revision: 1,
		State: map[string]any{
			"review": map[string]any{"round": 1, "clean_round": nil},
			"evidence": []any{
				map[string]any{
					"id": "ev-completion-1", "kind": "agent_completion", "path": "evidence/ev-completion-1.json",
					"status": "valid", "baseline_generation": 1, "review_round": 1,
				},
				map[string]any{
					"id": "ev-team", "kind": "builder_report", "path": "evidence/ev-team.json",
					"status": "valid", "baseline_generation": 1, "review_round": 1,
				},
			},
		},
	}
	candidate := &transition.TransitionSpec{
		ID:               "TR-006",
		RequiredEvidence: []string{"builder_report_record", "team_manifest_record"},
	}
	evaluation := qualitygate.Evaluation{
		EvidenceRefs: []string{"ev-completion-1", "ev-team"},
	}

	got := buildTransitionEvidence(snapshot, candidate, evaluation)
	if got["builder_report_record"] == "" {
		t.Fatal("builder_report_record must bind")
	}
	if got["team_manifest_record"] != "ev-team" {
		t.Fatalf("team_manifest_record = %q, want ev-team fallback when no team_manifest present", got["team_manifest_record"])
	}
	if got["builder_report_record"] == got["team_manifest_record"] {
		t.Fatalf("builder_report_record and team_manifest_record must not share the same ref: %q", got["builder_report_record"])
	}
}
