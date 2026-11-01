package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalPlanAssetRegistrationAndRevisionDoNotReadSources(t *testing.T) {
	for _, identity := range []string{"legacy", "plan"} {
		for _, stateName := range []string{"regression_available", "cold_start"} {
			t.Run(identity+"/"+stateName, func(t *testing.T) {
				root := t.TempDir() // No Git repository, CASE file, executable or definition.
				state := revisionSourceState()
				state["entities"].(map[string]any)["agents"] = coldStartState()["entities"].(map[string]any)["agents"]
				path := writePlanFile(t, root)
				rawBytes, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var raw map[string]any
				if err := json.Unmarshal(rawBytes, &raw); err != nil {
					t.Fatal(err)
				}
				if identity == "plan" {
					raw["schema_version"] = "1.1.0"
					delete(raw, "review_round")
					state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-t-1", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
				}
				raw["e2e_coverage_state"] = stateName
				if stateName == "cold_start" {
					raw["verification_artifact_workspace"] = "e2e-workspace/original"
				}
				c := raw["claims"].([]any)[3].(map[string]any)
				c["applicability"] = "required"
				c["target"] = "internal/example"
				c["assertion"] = "save persists state"
				c["oracle"] = "subsequent read returns saved value"
				c["method"] = "run save/read journey"
				c["focus_key"] = "save-read"
				c["source_refs"] = []string{"docs/design/prototypes/example/cases.json#CASE-001"}
				delete(c, "na_rationale")
				delete(c, "na_checklist_id")
				raw["assignments"] = append(raw["assignments"].([]any), map[string]any{"assignment_id": "assignment-e2e-1", "lens": "e2e", "claim_ids": []string{"claim-e2e-na"}, "behavior_surface": "browser", "execution_wave": "behavior", "non_overlap_boundary": "owns save/read journey"})
				raw["e2e_assets"] = []any{
					map[string]any{"case_ref": "docs/design/prototypes/example/cases.json#CASE-001", "path": "absent.spec.ts", "selector_ref": "testid:save", "route_ref": "/save", "environment": "local browser"},
					map[string]any{"case_ref": "CASE-legacy", "path": "legacy.spec.ts", "asset_id": "historical", "sha256": strings.Repeat("f", 64), "selector_ref": "testid:read", "route_ref": "/read", "environment": "historical browser"},
					map[string]any{"case_ref": "docs/design/prototypes/example/cases.json#CASE-002", "path": "second-absent.spec.ts", "selector_ref": "testid:load", "route_ref": "/load", "environment": "local browser"},
				}
				write := func() {
					t.Helper()
					b, e := json.Marshal(raw)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(path, b, 0600); e != nil {
						t.Fatal(e)
					}
				}
				write()
				sp, jp := writeState(t, root, state)
				snap, err := RegisterPlan(root, sp, jp, PlanRequest{ExpectedRevision: 1, PlanPath: path})
				if err != nil {
					t.Fatal(err)
				}
				original, _, err := LoadPlan(root, snap.State)
				if err != nil {
					t.Fatal(err)
				}
				// Asset bytes and scenario source now exist with unrelated content. Neither
				// original registration nor original revision can derive authority from them.
				for _, p := range []string{"absent.spec.ts", "legacy.spec.ts", "docs/design/prototypes/example/cases.json"} {
					if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, p), []byte("changed unrelated bytes"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				qa := raw["claims"].([]any)[1].(map[string]any)
				qa["oracle"] = "observed revised error contract"
				qa["source_refs"] = []string{"CONTRACTS-001", "review-result-qa-1"}
				write()
				revised, err := RevisePlan(root, sp, jp, ReviseRequest{ExpectedRevision: snap.Revision, PlanPath: path, SourceRef: "review-result-qa-1", AffectedSurface: "internal/example"})
				if err != nil {
					t.Fatal(err)
				}
				accepted, ptr, err := LoadPlan(root, revised.State)
				if err != nil {
					t.Fatal(err)
				}
				if ptr.Revision != 2 || accepted.ReviewPlanID != original.ReviewPlanID {
					t.Fatal("original plan revision identity changed")
				}
				if len(accepted.E2EAssets) != 3 || accepted.E2EAssets[0].AssetID != "" || accepted.E2EAssets[2].AssetID != "" {
					t.Fatalf("reception invented machine fields: %+v", accepted.E2EAssets)
				}
				encoded, err := json.Marshal(accepted.E2EAssets[0])
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(encoded, []byte(`"asset_id"`)) || bytes.Contains(encoded, []byte(`"sha256"`)) {
					t.Fatalf("omitted machine metadata reappeared: %s", encoded)
				}

				if accepted.E2EAssets[1].AssetID != "historical" {
					t.Fatal("historical metadata was refreshed")
				}

				// The accepted E2E responsibility remains consumable downstream without
				// inventing an asset digest or requiring the selected files as admission.
				revised = markDispatched(t, root, sp, jp, revised, "assignment-e2e-1", "agent-e2e-1")
				resultPath := writeResultFile(t, root, accepted, "assignment-e2e-1", "review-result-original-e2e", "agent-e2e-1", "pass", map[string]string{"claim-e2e-na": "pass"}, nil)
				{
					body, err := os.ReadFile(resultPath)
					if err != nil {
						t.Fatal(err)
					}
					var result map[string]any
					if err = json.Unmarshal(body, &result); err != nil {
						t.Fatal(err)
					}
					result["assignment_revision"] = 2
					if identity == "plan" {
						result["schema_version"] = "1.1.0"
						delete(result, "review_round")
					}
					body, err = json.Marshal(result)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(resultPath, body, 0600); err != nil {
						t.Fatal(err)
					}
				}
				received, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: revised.Revision, AssignmentID: "assignment-e2e-1", ResultPath: resultPath})
				if err != nil {
					t.Fatal(err)
				}
				if Dispositions(received.State)["claim-e2e-na"].Disposition != "pass" {
					t.Fatal("original E2E answer was not received")
				}

			})
		}
	}
}
