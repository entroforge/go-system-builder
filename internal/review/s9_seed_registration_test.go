package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func s9SeedRegistrationFixture(t *testing.T) (string, string, string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	state := baseVerificationState()
	state["review"] = map[string]any{
		"round": 2.0, "clean_round": nil,
		"round_entry": map[string]any{
			"transition_id": "TR-012", "round": 2.0, "baseline_generation": 1.0,
			"change_impact_ref": "ev-impact-1",
		},
	}

	planPath := writePlanFile(t, root)
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var planBody map[string]any
	if err := json.Unmarshal(planData, &planBody); err != nil {
		t.Fatal(err)
	}
	planBody["review_round"] = 2
	planBody["change_impact"] = map[string]any{"source_refs": []string{"ev-impact-1"}}
	planBody["coverage_inventory"] = []any{map[string]any{
		"id": "surface:internal/example/service.go", "kind": "changed_surface",
		"source_ref": "internal/example/service.go", "target": "internal/example/service.go", "lens": "qa",
	}}
	for _, raw := range planBody["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["source_refs"] = append(claim["source_refs"].([]any), "internal/example/service.go")
		}
	}
	updatedPlan, err := json.MarshalIndent(planBody, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(updatedPlan, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	impactRel := ".claude/evidence/impact.json"
	impactValue := map[string]any{
		"schema_version": "1.0.0", "record_type": "change_impact", "impact_id": "impact-BUG-001-attempt-1",
		"runtime_id": "loop-REQ-TEST", "req_id": "REQ-001", "baseline_generation": 1,
		"source_bug_ids": []string{"BUG-001"}, "change_types": []string{"implementation"},
		"changed_artifacts": []any{map[string]any{"id": "handler", "path": "internal/example/service.go", "sha256": sha256Of([]byte("fixture baseline"))}},
		"decisions": []any{map[string]any{
			"source_id": "handler", "target_id": "evidence-verification", "relation": "implements repair",
			"rule_id": "IM-IMPLEMENTATION-MODULE", "decision": "invalidate", "responsibility_id": "VER-MODULE-COMPLETE",
			"scope": []string{"internal/example/service.go"}, "rationale": "repair changed the implementation", "recovery_evidence": []string{"BUG-001"},
		}},
		"escalation_level": "review_round", "invalidated_evidence_ids": []string{"ev-old"},
		"superseded_evidence_ids": []string{}, "retained_evidence_ids": []string{},
		"required_reverification_ids": []string{"reverify-BUG-001"}, "analyzed_by": "orchestrator", "analyzed_at": "2026-08-25T00:00:00Z",
	}
	impactBytes, err := json.Marshal(impactValue)
	if err != nil {
		t.Fatal(err)
	}
	impactPath := filepath.Join(root, filepath.FromSlash(impactRel))
	if err := os.MkdirAll(filepath.Dir(impactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(impactPath, impactBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{map[string]any{
		"id": "ev-impact-1", "kind": "change_impact", "path": impactRel,
		"sha256": sha256Of(impactBytes), "status": "valid", "baseline_generation": 1,
		"review_round": nil, "produced_by": []any{"orchestrator"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": "orchestrator", "scope_refs": []any{},
	}}
	statePath, journalPath := writeState(t, root, state)
	var professional Plan
	if err := json.Unmarshal(updatedPlan, &professional); err != nil {
		t.Fatal(err)
	}
	seed := professional
	seed.CreatedBy = "s9-handoff"
	seed.E2ECoverageState = "cold_start"
	ws := "e2e-workspace/original-seed"
	seed.VerificationArtifactWorkspace = &ws
	seed.Claims = append([]Claim(nil), professional.Claims...)
	for i := range seed.Claims {
		seed.Claims[i].Assertion = "PLANNER-REFINE assertion"
		seed.Claims[i].Oracle = "PLANNER-REFINE oracle"
		seed.Claims[i].Method = "PLANNER-REFINE method"
		if seed.Claims[i].Lens == "e2e" {
			seed.Claims[i].Applicability = "required"
			seed.Claims[i].NARationale = ""
			seed.Claims[i].NAChecklistID = ""
			seed.Assignments = append(append([]PlanAssignment(nil), seed.Assignments...), PlanAssignment{AssignmentID: "assignment-seed-e2e", Lens: "e2e", ClaimIDs: []string{seed.Claims[i].ClaimID}, NonOverlapBoundary: "unresolved seed applicability", ExecutionWave: "behavior"})
		}
	}
	seedData, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	seedRef := ".claude/review/repair/s7-seeds/original.json"
	seedPath := filepath.Join(root, filepath.FromSlash(seedRef))
	if err := os.MkdirAll(filepath.Dir(seedPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, seedData, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRegisteredPlanProjection(state, seed, seedRef, sha256Of(seedData), 2, ws, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	entry := state["review"].(map[string]any)["round_entry"].(map[string]any)
	entry["review_plan_seed_ref"], entry["review_plan_seed_sha256"] = seedRef, sha256Of(seedData)
	statePath, journalPath = writeState(t, root, state)
	return root, statePath, journalPath, planPath, seedData
}

func TestS9SeedFirstProfessionalRegistrationCanChooseGenuineNA(t *testing.T) {
	root, statePath, journalPath, planPath, seedBytes := s9SeedRegistrationFixture(t)
	snapshot, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("original first professional registration cannot resolve seed cold_start to N/A: %v", err)
	}
	ptr := PlanPointerFromState(snapshot.State)
	if currentReviewRound(snapshot.State) != 2 || ptr.Revision != 1 || ptr.E2ECoverageState != "not_applicable" || ptr.VerificationArtifactWorkspace != "" {
		t.Fatalf("wrong first professional projection: %+v", ptr)
	}
	seedAfter, err := os.ReadFile(filepath.Join(root, ".claude/review/repair/s7-seeds/original.json"))
	if err != nil || !bytes.Equal(seedBytes, seedAfter) {
		t.Fatal("first registration rewrote original seed")
	}
	if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath}); err == nil {
		t.Fatal("second professional registration replaced accepted plan")
	}
}

func TestS9SeedRegistrationCannotReplaceStartedOrUnboundPlan(t *testing.T) {
	for _, test := range []string{"dispatched", "queued owner", "result", "missing assignment", "wrong seed hash", "wrong entry", "revision two", "different id", "drifted seed", "drifted product"} {
		t.Run(test, func(t *testing.T) {
			root, statePath, journalPath, planPath, _ := s9SeedRegistrationFixture(t)
			data, _ := os.ReadFile(statePath)
			var state map[string]any
			_ = json.Unmarshal(data, &state)
			rev := state["review"].(map[string]any)
			ptr := rev["plan"].(map[string]any)
			entry := rev["round_entry"].(map[string]any)
			assignments := rev["assignments"].(map[string]any)
			row := assignments["assignment-qa-1"].(map[string]any)
			switch test {
			case "dispatched":
				row["status"] = "active"
				row["last_lifecycle_event"] = "workgroup_registered"
				row["agent_id"] = "agent-qa-1"
			case "queued owner":
				row["queued_agent_id"] = "agent-qa-1"
			case "result":
				row["result_ref"] = ".claude/evidence/accepted.json"
			case "missing assignment":
				delete(assignments, "assignment-qa-1")
			case "wrong seed hash":
				entry["review_plan_seed_sha256"] = strings.Repeat("a", 64)
			case "wrong entry":
				entry["transition_id"] = "TR-006"
			case "revision two":
				ptr["revision"] = 2
			case "different id":
				d, _ := os.ReadFile(planPath)
				var p map[string]any
				_ = json.Unmarshal(d, &p)
				p["review_plan_id"] = "another-plan"
				d, _ = json.Marshal(p)
				_ = os.WriteFile(planPath, d, 0644)
			case "drifted seed":
				_ = os.WriteFile(filepath.Join(root, ".claude/review/repair/s7-seeds/original.json"), []byte("changed"), 0644)
			case "drifted product":
				_ = os.WriteFile(filepath.Join(root, "internal/example/service.go"), []byte("changed"), 0644)
			}
			before, _ := json.Marshal(state)
			_ = os.WriteFile(statePath, before, 0644)
			if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath}); err == nil {
				t.Fatalf("%s allowed seed replacement", test)
			}
			after, _ := os.ReadFile(statePath)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected registration mutated Runtime")
			}
		})
	}
}
