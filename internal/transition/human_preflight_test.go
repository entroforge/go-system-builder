package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestHumanDecisionAuthoringAndConsumptionAgree(t *testing.T) {
	for _, test := range []struct {
		name, want string
		mutate     func(map[string]any)
	}{
		{"valid", "", func(a map[string]any) {}},
		{"approver_placeholder", "placeholder", func(a map[string]any) { a["approved_by"] = "<HUMAN-IDENTITY>" }},
		{"decision_placeholder", "placeholder", func(a map[string]any) { a["decision"] = "<EXACT-HUMAN-DECISION>" }},
		{"wrong_runtime", "targets runtime", func(a map[string]any) { a["runtime_id"] = "loop-other" }},
		{"wrong_scope", "scope_refs", func(a map[string]any) { a["scope_refs"] = []string{"runtime_pause:loop-test"} }},
		{"stale_cursor", "phase_revision", func(a map[string]any) { a["target_cursor"].(map[string]any)["phase_revision"] = 0 }},
		{"stale_generation", "baseline_generation", func(a map[string]any) { a["baseline_generation"] = 999 }},
		{"fractional_round", "review_round", func(a map[string]any) { a["review_round"] = 1.5 }},
		{"expired", "expired", func(a map[string]any) { a["expires_at"] = "2000-01-01T00:00:00Z" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			setupRepoWithDefinition(t, root)
			state := stateAtVerificationMap(5)
			state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": 1}
			artifact := transition.HumanDecisionDraft(state)
			artifact["decision_id"] = "decision-authoring"
			artifact["disposition"] = "approve"
			artifact["approved_by"] = "Fixture release owner"
			artifact["decision"] = "Fixture human approval of this release candidate."
			artifact["scope_refs"] = []string{"runtime_release:loop-test"}
			test.mutate(artifact)
			data, _ := json.Marshal(artifact)
			report := transition.PreflightHumanReleaseDecision(root, state, data, "orchestrator")
			if report.TransitionReady {
				t.Fatal("preflight manufactured release permission")
			}
			if test.want == "" && !report.ArtifactValid {
				t.Fatalf("valid draft: %+v", report)
			}
			if test.want != "" && (report.ArtifactValid || !strings.Contains(strings.Join(report.Problems, "\n"), test.want)) {
				t.Fatalf("expected %q: %+v", test.want, report)
			}
			path := "docs/reports/human/decision.json"
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, path), data, 0644); err != nil {
				t.Fatal(err)
			}
			state["evidence"] = []any{map[string]any{
				"id": "decision-authoring", "kind": "human_decision", "path": path, "sha256": transition.SHA256(data), "status": "valid",
				"baseline_generation": 1, "review_round": 1, "produced_by": []any{"user"}, "scope_refs": []any{"runtime_release:loop-test"},
				"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": nil,
			}}
			writeFullState(t, root, state)
			err := applyT(t, root, "TR-025", 5, "orchestrator", map[string]string{"human_decision_record": "decision-authoring"})
			if test.want == "" && err != nil {
				t.Fatalf("valid consumption: %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("consumption bypassed lint %q: %v", test.want, err)
			}
		})
	}
}

func TestHumanDecisionPreflightRejectsWrongActorAndDuplicateID(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtVerificationMap(5)
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": 1}
	a := transition.HumanDecisionDraft(state)
	data, _ := json.Marshal(a)
	if report := transition.PreflightHumanReleaseDecision(root, state, data, "orchestrator"); report.ArtifactValid {
		t.Fatal("scaffold must never authorize a decision")
	}
	a["decision_id"], a["disposition"], a["approved_by"], a["decision"] = "decision-new", "approve", "Fixture Human", "Fixture approval"
	a["scope_refs"] = []string{"runtime_release:loop-test"}
	data, _ = json.Marshal(a)
	if report := transition.PreflightHumanReleaseDecision(root, state, data, "Fixture Human"); report.ArtifactValid {
		t.Fatal("human name accepted as execution role")
	}
	state["evidence"] = []any{map[string]any{"id": "decision-new", "kind": "human_decision", "status": "valid", "sha256": transition.SHA256(data), "consumed_by": "TR-025"}}
	if report := transition.PreflightHumanReleaseDecision(root, state, data, "user"); report.ArtifactValid {
		t.Fatal("consumed decision passed preflight")
	}
}
