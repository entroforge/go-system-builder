package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// §14.1 E2E cold-start workspace matrix (L3-S7 §1.4.1, §10.1.6): results
// bind the workspace digest they ran against; a spec that drifts after its
// Result was consumed makes the round stale instead of sealing clean.
// ---------------------------------------------------------------------------

// writeColdStartPlan writes a plan with one DV claim, one QA claim and one
// required behavior-wave E2E claim over a cold-start workspace.
func writeColdStartPlan(t *testing.T, root, workspace string) string {
	t.Helper()
	subjectPath := filepath.Join(root, "internal", "example", "service.go")
	if err := os.MkdirAll(filepath.Dir(subjectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	subjectBytes := []byte("fixture baseline")
	if err := os.WriteFile(subjectPath, subjectBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      "review-plan-cs-1",
		"review_round":        1,
		"baseline_generation": 1,
		"frozen_subjects": []any{
			map[string]any{"path": "internal/example/service.go", "sha256": sha256Of(subjectBytes), "kind": "product_code"},
		},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-dv-1", "lens": "delivery",
				"target": "internal/example", "assertion": "REQ covered", "oracle": "AC maps to code",
				"method": "traceability", "applicability": "required", "source_refs": []string{"REQ-001"},
			},
			map[string]any{
				"claim_id": "claim-qa-1", "lens": "qa",
				"target": "internal/example", "assertion": "errors propagate", "oracle": "no dropped error",
				"method": "code review", "applicability": "required", "source_refs": []string{"CONTRACTS-001"},
			},
			map[string]any{
				"claim_id": "claim-e2e-1", "lens": "e2e",
				"target": "settings save flow", "assertion": "the declared flow produces the expected behavior",
				"oracle": "flow-level oracle with console/network evidence",
				"method": "real-browser execution", "applicability": "required",
				"source_refs": []string{"REQ-001#flow"}, "focus_key": "user-flow",
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-dv-1", "lens": "delivery", "claim_ids": []string{"claim-dv-1"},
				"non_overlap_boundary": "owns traceability", "execution_wave": "static",
			},
			map[string]any{
				"assignment_id": "assignment-qa-1", "lens": "qa", "claim_ids": []string{"claim-qa-1"},
				"non_overlap_boundary": "owns logic/state", "execution_wave": "static",
			},
			map[string]any{
				"assignment_id": "assignment-e2e-1", "lens": "e2e", "claim_ids": []string{"claim-e2e-1"},
				"non_overlap_boundary": "owns the declared flows; spec authoring stays inside the verification workspace",
				"execution_wave":       "behavior",
			},
		},
		"e2e_coverage_state":              "cold_start",
		"verification_artifact_workspace": workspace,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "orchestrator",
		"created_at":                      "2026-08-18T00:00:00Z",
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plan-cold-start.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// coldStartFixture registers the cold-start plan and dispatches all three
// assignments (agent-e2e-1 is added to the fixture state).
func coldStartFixture(t *testing.T, root, statePath, journalPath, workspace string) (int, *Plan) {
	t.Helper()
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: writeColdStartPlan(t, root, workspace),
	})
	if err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-e2e-1", "agent-e2e-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	return snap.Revision, plan
}

// coldStartState extends the base fixture with an E2E agent.
func coldStartState() map[string]any {
	state := baseVerificationState()
	agents := state["entities"].(map[string]any)["agents"].([]any)
	agents = append(agents, map[string]any{
		"id": "agent-e2e-1", "role": "e2e-browser", "state": "working",
		"task_ids": []any{}, "team_id": "team-review-1",
		"definition_ref": "agents/e2e-browser.md", "prompt_ref": ".claude/workgroups/REQ-TEST/m.json#agent-e2e-1",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-18T00:00:00Z",
	})
	state["entities"].(map[string]any)["agents"] = agents
	return state
}

// writeE2EResultFile writes a passing result for assignment-e2e-1 binding the
// given workspace digest (nil/empty binds nothing).
func writeE2EResultFile(t *testing.T, root string, plan *Plan, resultID, artifactDigest string) string {
	t.Helper()
	payload := map[string]any{
		"schema_version":      "1.0.0",
		"result_id":           resultID,
		"assignment_id":       "assignment-e2e-1",
		"assignment_revision": 1,
		"review_plan_id":      plan.ReviewPlanID,
		"review_round":        plan.ReviewRound,
		"baseline_generation": plan.BaselineGeneration,
		"producer_agent_id":   "agent-e2e-1",
		"subject_digest":      SubjectDigest(plan),
		"claim_results": []any{
			map[string]any{
				"claim_id": "claim-e2e-1", "conclusion": "pass",
				"observed": "flow behaves as declared", "evidence_refs": []string{fixtureEvidenceRef(t, root, "e2e-run.md")},
			},
		},
		"verdict": "pass",
	}
	if artifactDigest != "" {
		payload["verification_artifact_digest"] = artifactDigest
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, resultID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// §14.1: cold-start E2E Result 必须绑定实际 workspace digest。
func TestColdStartResultMustBindWorkspaceDigest(t *testing.T) {
	workspace := "e2e-workspace/plan-cs-1"

	// Missing digest: rejected.
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, coldStartState())
	revision, plan := coldStartFixture(t, root, statePath, journalPath, workspace)
	path := writeE2EResultFile(t, root, plan, "review-result-e2e-1", "")
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-e2e-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "verification_artifact_digest") {
		t.Fatalf("E2E result without a workspace digest must be rejected, got %v", err)
	}

	// Wrong digest: rejected.
	root = t.TempDir()
	statePath, journalPath = writeState(t, root, coldStartState())
	revision, plan = coldStartFixture(t, root, statePath, journalPath, workspace)
	path = writeE2EResultFile(t, root, plan, "review-result-e2e-1", strings.Repeat("9", 64))
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-e2e-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("E2E result with a stale digest must be rejected, got %v", err)
	}

	// Actual digest: accepted, and the frozen product baseline digest is
	// unaffected by the spec/fixture write (cold-start authoring does not
	// stale the product baseline).
	root = t.TempDir()
	statePath, journalPath = writeState(t, root, coldStartState())
	revision, plan = coldStartFixture(t, root, statePath, journalPath, workspace)
	specPath := filepath.Join(root, workspace, "settings.spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte("test('settings save', ...)"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := WorkspaceDigest(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if got := SubjectDigest(plan); got == digest {
		t.Fatal("workspace digest must be separate from the frozen product baseline digest")
	}
	path = writeE2EResultFile(t, root, plan, "review-result-e2e-1", digest)
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-e2e-1", ResultPath: path,
	})
	if err != nil {
		t.Fatalf("E2E result binding the actual digest must be accepted: %v", err)
	}
	row := snap.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-e2e-1"].(map[string]any)
	if row["status"] != "consumed" || row["artifact_digest"] != digest {
		t.Fatalf("consumed assignment must record the bound digest: %v", row)
	}
	resultRef, ok := row["result_ref"].(string)
	if !ok || resultRef == "" {
		t.Fatalf("consumed assignment must retain its result artifact reference: %v", row)
	}
	resultData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(resultRef)))
	if err != nil {
		t.Fatal(err)
	}
	var resultEnvelope map[string]any
	if err := json.Unmarshal(resultData, &resultEnvelope); err != nil {
		t.Fatal(err)
	}
	if resultEnvelope["verification_artifact_digest"] != digest {
		t.Fatalf("stored Result envelope must preserve verification_artifact_digest, got %v", resultEnvelope["verification_artifact_digest"])
	}
}

func TestRegressionAssetFingerprintMustMatchDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "e2e", "settings-save.spec.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("test('settings save', ...)")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &Plan{
		E2ECoverageState: "regression_available",
		E2EAssets:        []E2EAsset{{AssetID: "asset-settings", CaseRef: "CASE-001", Path: "e2e/settings-save.spec.ts", SHA256: strings.Repeat("0", 64)}},
	}
	if err := verifyRegressionAssetFingerprints(root, plan); err == nil || !strings.Contains(err.Error(), "S7_E2E_ASSET_FINGERPRINT") || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale regression asset must be rejected with repair guidance, got %v", err)
	}
	plan.E2EAssets[0].SHA256 = sha256Of(content)
	if err := verifyRegressionAssetFingerprints(root, plan); err != nil {
		t.Fatalf("current regression asset fingerprint must pass: %v", err)
	}
}

func TestSubmitResultRechecksRegressionAssetFingerprint(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, coldStartState())
	assetPath := filepath.Join(root, "e2e", "settings-save.spec.ts")
	assetBytes := []byte("test('settings save', ...)")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, assetBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	planPath := writeColdStartPlan(t, root, "unused-cold-start-workspace")
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(planBytes, &raw); err != nil {
		t.Fatal(err)
	}
	raw["e2e_coverage_state"] = "regression_available"
	raw["verification_artifact_workspace"] = nil
	raw["e2e_assets"] = []any{map[string]any{
		"asset_id": "asset-settings", "case_ref": "CASE-001", "path": "e2e/settings-save.spec.ts",
		"sha256":       sha256Of(assetBytes),
		"selector_ref": "testid:save-button", "route_ref": "settings/save",
		"environment": "chromium/localhost:3000/profile=default",
	}}
	regressionPath := filepath.Join(root, "plan-regression.json")
	regressionBytes, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(regressionPath, append(regressionBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: regressionPath,
	})
	if err != nil {
		t.Fatalf("RegisterPlan regression fixture: %v", err)
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-e2e-1", "agent-e2e-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	// The asset changes after registration but before the E2E result arrives.
	if err := os.WriteFile(assetPath, []byte("test('settings save', 'changed')"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := writeE2EResultFile(t, root, plan, "review-result-e2e-regression", "")
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-e2e-1", ResultPath: resultPath,
	}); err == nil || !strings.Contains(err.Error(), "S7_E2E_ASSET_FINGERPRINT") || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("submit must recheck a drifted regression asset with repair guidance, got %v", err)
	}
}

func TestRegisterPlanCleansNewWorkspaceWhenCASApplyFails(t *testing.T) {
	workspace := "e2e-workspace/orphaned-plan"
	root := t.TempDir()
	state := coldStartState()
	state["entities"].(map[string]any)["findings"] = "invalid" // force post-artifact candidate validation to fail
	statePath, journalPath := writeState(t, root, state)
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: writeColdStartPlan(t, root, workspace),
	})
	if err == nil {
		t.Fatal("invalid review projection must reject registration")
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(workspace))); !os.IsNotExist(statErr) {
		t.Fatalf("failed registration must clean its newly-created workspace, stat error=%v", statErr)
	}
}

// §14.1: 已 consumed E2E Result 对应 spec 后续被修改 —— 轮在 seal/clean
// 时被判 stale，而不是悄悄通过。
func TestWorkspaceDriftAfterConsumptionBlocksRoundClose(t *testing.T) {
	workspace := "e2e-workspace/plan-cs-1"
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, coldStartState())
	revision, plan := coldStartFixture(t, root, statePath, journalPath, workspace)

	specPath := filepath.Join(root, workspace, "settings.spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte("spec v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := WorkspaceDigest(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	e2ePath := writeE2EResultFile(t, root, plan, "review-result-e2e-1", digest)
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-e2e-1", ResultPath: e2ePath,
	})
	if err != nil {
		t.Fatalf("SubmitResult e2e: %v", err)
	}

	// The spec drifts after the result was consumed.
	if err := os.WriteFile(specPath, []byte("spec v2 — edited after consumption"), 0o644); err != nil {
		t.Fatal(err)
	}

	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}

	// The final required claim would close the round clean — but the
	// consumed E2E result's bound digest no longer matches the workspace.
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("workspace drift after consumption must block the round close as stale, got %v", err)
	}
}
