package review

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
)

func TestDraftPlanNoUIDoesNotDecideGeneralBehaviorApplicability(t *testing.T) {
	root := t.TempDir()
	state := baseDraftState(t)
	req := caseScopeREQ(t, root, "docs/design/prototypes/command/\n")
	req["metadata"] = map[string]any{"ui_impact": "none"}
	state["bound_req"] = req
	putCaseScopeFile(t, root, "docs/design/prototypes/command/cases.json", `{"module":"command","cases":[{"id":"CASE-API-001","title":"command persists state","required":true,"browser_required":false}]}`)
	plan, notes := DraftPlanForRoot(root, state, 1)
	if plan == nil {
		t.Fatalf("no draft: %v", notes)
	}
	if plan.E2ECoverageState != "" {
		t.Fatalf("UI fact decided general E2E: %s", plan.E2ECoverageState)
	}
	for _, claim := range plan.Claims {
		if claim.Lens == "e2e" && claim.Applicability == "not_applicable" {
			t.Fatalf("invented N/A: %+v", claim)
		}
	}
	if !strings.Contains(strings.Join(notes, "\n"), "API, command, state") {
		t.Fatalf("missing non-browser decision: %v", notes)
	}
	for _, a := range plan.Assignments {
		if a.Lens == "e2e" && a.BehaviorSurface != "" {
			t.Fatalf("unknown surface inferred: %+v", a)
		}
	}
}

func TestDraftManifestUsesExplicitBehaviorSurface(t *testing.T) {
	for _, surface := range []string{"", "non_browser", "browser"} {
		t.Run("surface-"+surface, func(t *testing.T) {
			root := t.TempDir()
			state := baseVerificationState()
			plan := manifestDraftFixturePlan()
			workspace := "e2e-workspace/behavior"
			plan["e2e_coverage_state"] = "cold_start"
			plan["verification_artifact_workspace"] = workspace
			plan["claims"] = append(plan["claims"].([]any), map[string]any{"claim_id": "claim-e2e-command", "lens": "e2e", "target": "command exit and persisted state", "assertion": "command preserves contract", "oracle": "exit status and actual state", "method": "real-browser execution prose must not select applicability", "applicability": "required", "source_refs": []string{"TASK-001"}})
			a := map[string]any{"assignment_id": "assignment-e2e-command", "lens": "e2e", "claim_ids": []string{"claim-e2e-command"}, "non_overlap_boundary": "owns command contract", "execution_wave": "behavior"}
			if surface != "" {
				a["behavior_surface"] = surface
			}
			plan["assignments"] = append(plan["assignments"].([]any), a)
			pinPlan(t, root, state, plan)
			draft, _, err := DraftManifest(root, state, "assignment-e2e-command")
			if err != nil {
				t.Fatal(err)
			}
			row := draft.Assignments[0]
			if surface == "non_browser" {
				if row.ResponsibilityID != "E2E-BEHAVIOR" || strings.Contains(strings.Join(row.SkillRefs, ","), "playwright") || len(draft.ResponsibilityDispositions) != 1 {
					t.Fatalf("non-browser got browser duties: %+v", draft)
				}
			} else if row.ResponsibilityID != "E2E-USER-FLOW" || !strings.Contains(strings.Join(row.SkillRefs, ","), "playwright-e2e") || len(draft.ResponsibilityDispositions) != 2 {
				t.Fatalf("browser lost duties: %+v", draft)
			}
			draft.RiskTags = []ManifestRiskTag{{Tag: "regression", Source: "original behavior", Derivation: "explicit"}}
			data, _ := json.Marshal(draft)
			if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", data); err != nil {
				t.Fatal(err)
			}
			if err := team.ValidateBytes(data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOptionalBehaviorSurfacePreservesHistoricalAssignmentBytes(t *testing.T) {
	original := `{"assignment_id":"assignment-old","lens":"e2e","claim_ids":["claim-old"],"non_overlap_boundary":"original boundary","execution_wave":"behavior"}`
	var a PlanAssignment
	if err := json.Unmarshal([]byte(original), &a); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(a)
	if string(encoded) != original {
		t.Fatalf("historical digest bytes changed: %s", encoded)
	}
}

func TestKnownBrowserCaseCannotUseNonBrowserResponsibility(t *testing.T) {
	root, state, plan, _ := knownCaseFixture(t)
	ref := "docs/design/prototypes/shareholder/cases.json#CASE-001"
	plan.Claims = append(plan.Claims, Claim{ClaimID: "claim-browser-required", Lens: "e2e", SourceRefs: []string{ref}, Applicability: "required"})
	plan.Assignments = append(plan.Assignments, PlanAssignment{AssignmentID: "assignment-browser-required", Lens: "e2e", ClaimIDs: []string{"claim-browser-required"}, BehaviorSurface: "non_browser"})
	if err := validateKnownE2ECaseCoverage(context.Background(), root, state, plan); err == nil || !strings.Contains(err.Error(), "declares non_browser") {
		t.Fatalf("required browser CASE lost browser obligation: %v", err)
	}
}
