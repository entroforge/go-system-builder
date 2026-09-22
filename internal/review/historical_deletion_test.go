package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoricalDeletionRequiresPinnedEvidenceAndRemainsAbsent(t *testing.T) {
	for _, name := range []string{"historical cited", "self declared", "not cited", "tampered", "foreign runtime", "old generation", "invalidated", "newer modified", "restored same bytes", "wrong digest", "stale plan"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := "internal/retired.go"
			before := []byte("package retired\n")
			digest := sha256Of(before)
			body := map[string]any{"runtime_id": "r", "baseline_generation": 1, "changed_artifacts": []any{map[string]any{"path": path, "sha256": digest, "status": "deleted"}}}
			if name == "foreign runtime" {
				body["runtime_id"] = "foreign"
			}
			data, _ := json.Marshal(body)
			if err := os.WriteFile(filepath.Join(root, "impact.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			row := map[string]any{"id": "old-impact", "kind": "change_impact", "path": "impact.json", "sha256": sha256Of(data), "status": "valid", "baseline_generation": 1, "review_round": 1}
			plan := &Plan{ReviewRound: 3, BaselineGeneration: 1, FrozenSubjects: []FrozenSubject{{Path: path, SHA256: digest, Kind: "deleted"}}, ChangeImpact: &ChangeImpact{SourceRefs: []string{"old-impact"}}}
			state := map[string]any{"runtime_id": "r", "baseline": map[string]any{"generation": 1}, "review": map[string]any{"round": 3, "round_entry": map[string]any{"round": 3, "baseline_generation": 1, "change_impact_ref": nil}}, "evidence": []any{row}}
			switch name {
			case "self declared":
				state["evidence"] = []any{}
			case "not cited":
				plan.ChangeImpact = nil
			case "tampered":
				row["sha256"] = sha256Of([]byte("different"))
			case "old generation":
				row["baseline_generation"] = 0
			case "invalidated":
				row["invalidated_by"] = "repair"
			case "wrong digest":
				plan.FrozenSubjects[0].SHA256 = sha256Of([]byte("wrong"))
			case "stale plan":
				plan.ReviewRound = 2
			case "newer modified":
				body["changed_artifacts"].([]any)[0].(map[string]any)["status"] = "modified"
				newData, _ := json.Marshal(body)
				if err := os.WriteFile(filepath.Join(root, "new.json"), newData, 0600); err != nil {
					t.Fatal(err)
				}
				newer := map[string]any{"id": "new-impact", "kind": "change_impact", "path": "new.json", "sha256": sha256Of(newData), "status": "valid", "baseline_generation": 1, "review_round": 2}
				state["evidence"] = []any{row, newer}
			case "restored same bytes":
				if err := os.MkdirAll(filepath.Join(root, "internal"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, path), before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := verifyFrozenSubjects(root, plan, state)
			if (err == nil) != (name == "historical cited") {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
