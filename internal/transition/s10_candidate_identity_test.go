package transition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Diagnostic only: exercises the real generic evidence boundary and S10 guard,
// not Apply/CLI or a complete S10 release audit. Every write stays in t.TempDir.
func TestS10GuardRejectsSupersededCitedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		aGood, bGood bool
	}{
		{"A_bad_B_good", false, true}, {"A_good_B_bad", true, false},
		{"both_good", true, true}, {"both_bad", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rows := []any{}
			counter := []any{}
			for _, category := range []string{"requirement", "contract", "changed_path"} {
				rows = append(rows, map[string]any{"id": category, "category": category, "source_refs": []string{"source:" + category}, "expected": "verified claim", "oracle": "disproof check", "owner": "Acceptance", "evidence_refs": []string{"support"}, "disposition": "pass"})
				counter = append(counter, map[string]any{"id": "ce-" + category, "inventory_id": category, "question": "what disproves it?", "evidence_refs": []string{"support"}, "outcome": "pass"})
			}
			manifest, _ := json.Marshal(map[string]any{"schema_version": "1.0.0", "manifest_type": "acceptance", "runtime_id": "loop-test", "baseline_generation": 1, "review_round": 1, "coverage_inventory": rows, "counterevidence": counter, "risks": []any{}, "technical_debt": []any{}, "blocking_findings": []any{}, "metrics": map[string]any{"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1, "unknown_count": 0, "unsupported_pass_count": 0, "unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0}})
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0644); err != nil {
				t.Fatal(err)
			}
			entries := []any{}
			for _, candidate := range []struct {
				id   string
				good bool
			}{{"A", tc.aGood}, {"B", tc.bGood}} {
				env := map[string]any{"schema_version": "1.0.0", "evidence_id": candidate.id, "kind": "acceptance", "runtime_id": "loop-test", "baseline_generation": 1, "review_round": 1, "producer_agent_id": "auditor", "producer_responsibility": "Acceptance", "subject_refs": []any{}, "conclusion": "pass"}
				if candidate.good {
					env["audit_manifest_path"] = "manifest.json"
					env["audit_manifest_sha256"] = SHA256(manifest)
				}
				data, _ := json.Marshal(env)
				path := candidate.id + ".json"
				if err := os.WriteFile(filepath.Join(root, path), data, 0644); err != nil {
					t.Fatal(err)
				}
				entries = append(entries, map[string]any{"id": candidate.id, "kind": "acceptance", "status": "valid", "baseline_generation": 1, "review_round": 1, "path": path, "sha256": SHA256(data), "invalidated_by": nil, "produced_by": []any{"auditor"}, "responsibility_id": "Acceptance"})
			}
			state := map[string]any{"root": root, "runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}, "review": map[string]any{"round": 1}, "evidence": entries}
			if err := validateCurrentEvidence(root, state, "acceptance_record", "A"); err != nil {
				t.Fatalf("generic layer rejected cited A: %v", err)
			}
			err := guardACCCurrentFn(state, map[string]string{"acceptance_record": "A"})
			t.Logf("cited=A A_manifest_valid=%v latest=B B_manifest_valid=%v guard_pass=%v err=%v", tc.aGood, tc.bGood, err == nil, err)
			if err == nil || !strings.Contains(err.Error(), "selection conflict") {
				t.Fatalf("must explicitly reject superseded A instead of judging B; err=%v", err)
			}
		})
	}
}
