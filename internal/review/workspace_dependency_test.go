package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealedWorkspaceMissingDependenciesAreUnknown(t *testing.T) {
	root := t.TempDir()
	const workspace = "e2e-workspace/dependency-proof"
	if err := os.MkdirAll(filepath.Join(root, workspace), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(workspace+"/flow.spec.ts", "executed script")
	write("output.log", "PASS")
	bound, err := WorkspaceDigest(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{ClaimResults: []ClaimResult{{ClaimID: "claim-e2e-1", EvidenceRefs: []string{"command_output:output.log#sha256=" + sha256Of([]byte("PASS")) + " (bytes=4, truncated=false)"}}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	write("accepted.json", string(encoded))
	row := map[string]any{"status": "consumed", "lens": "e2e", "result_ref": "accepted.json", "artifact_digest": bound}
	rows := map[string]any{"assignment-e2e-1": row}
	ptr := &PlanPointer{VerificationArtifactWorkspace: workspace}
	if err := verifySealedArtifactDigests(root, ptr, rows, nil, "", ""); err != nil {
		t.Fatalf("historical result rejected despite unchanged aggregate proof: %v", err)
	}
	// The original output remains identical while its actual script changes.
	write(workspace+"/flow.spec.ts", "different script")
	err = verifySealedArtifactDigests(root, ptr, rows, nil, "", "")
	for _, detail := range []string{"unknown script/fixture dependencies", "claim_results[].evidence_refs", "checks[].evidence_refs", filepath.Join(root, workspace), "accepted.json", "immutable"} {
		if err == nil || !strings.Contains(err.Error(), detail) {
			t.Fatalf("missing original-field recovery detail %q: %v", detail, err)
		}
	}
	if got := string(mustReadTestFile(t, filepath.Join(root, "accepted.json"))); got != string(encoded) {
		t.Fatal("verification upgraded a historical receipt")
	}
}

func TestSealedEmptyWorkspaceProofAllowsLaterAdditions(t *testing.T) {
	root := t.TempDir()
	const workspace = "e2e-workspace/empty-at-consumption"
	bound, err := WorkspaceDigest(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "accepted.json"), []byte(`{"claim_results":[{"claim_id":"claim-e2e-1","evidence_refs":["path:output.log"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, workspace), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspace, "other.spec.ts"), []byte("later reviewer"), 0600); err != nil {
		t.Fatal(err)
	}
	rows := map[string]any{"assignment-e2e-1": map[string]any{"status": "consumed", "lens": "e2e", "result_ref": "accepted.json", "artifact_digest": bound}}
	if err := verifySealedArtifactDigests(root, &PlanPointer{VerificationArtifactWorkspace: workspace}, rows, nil, "", ""); err != nil {
		t.Fatalf("known empty baseline was treated as unknown dependency: %v", err)
	}
}

func TestSealedSelectedReusePreservesOriginalAssetBinding(t *testing.T) {
	root := t.TempDir()
	const workspace = "e2e-workspace/partial-reuse"
	if err := os.MkdirAll(filepath.Join(root, workspace), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reuse.spec.ts"), []byte("original reused script"), 0600); err != nil {
		t.Fatal(err)
	}
	scenario := e2eScenario{ID: "CASE-001", CasesPath: "docs/design/prototypes/settings/cases.json", Module: "settings", Title: "save settings", FlowRefs: []string{"PATH-001"}}
	plan := &Plan{
		Claims:    []Claim{{ClaimID: "claim-e2e-1", Lens: "e2e", Target: e2eScenarioTarget(scenario), SourceRefs: e2eScenarioSourceRefs(scenario)}},
		E2EAssets: []E2EAsset{{AssetID: "asset-1", CaseRef: scenarioRef(scenario), Path: "reuse.spec.ts", SHA256: sha256Of([]byte("original reused script"))}},
	}
	data, _ := json.Marshal(Result{ClaimResults: []ClaimResult{{ClaimID: "claim-e2e-1", EvidenceRefs: []string{"path:output.log"}}}})
	if err := os.WriteFile(filepath.Join(root, "accepted.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"result_ref": "accepted.json", "artifact_digest": sha256Of([]byte("original workspace aggregate"))}
	if err := verifyResultWorkspaceAnchors(root, root, workspace, "assignment-e2e-1", row, plan); err != nil {
		t.Fatalf("known reusable asset required duplicate authored metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "reuse.spec.ts"), []byte("changed reused script"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyResultWorkspaceAnchors(root, root, workspace, "assignment-e2e-1", row, plan); err == nil || !strings.Contains(err.Error(), "fingerprint is stale") {
		t.Fatalf("selected reuse silently refreshed its original binding: %v", err)
	}
	plan.Claims = append(plan.Claims, Claim{ClaimID: "claim-e2e-2", Lens: "e2e", Target: "CASE-002 module=settings", SourceRefs: []string{scenario.CasesPath + "#CASE-002"}})
	result := &Result{ClaimResults: []ClaimResult{{ClaimID: "claim-e2e-1"}, {ClaimID: "claim-e2e-2"}}}
	if resultUsesBoundReuseAssets(plan, result) {
		t.Fatal("partial reuse declaration explained an uncovered Claim")
	}
}

func TestSelectedReuseRequiresEveryExplicitCaseSource(t *testing.T) {
	const first = "docs/design/prototypes/settings/cases.json#CASE-001"
	const second = "docs/design/prototypes/settings/cases.json#CASE-002"
	plan := &Plan{
		Claims:    []Claim{{ClaimID: "claim-e2e-1", Lens: "e2e", Target: "multiple settings behaviors", SourceRefs: []string{first, second}}},
		E2EAssets: []E2EAsset{{CaseRef: first, Path: "reuse.spec.ts", SHA256: sha256Of([]byte("reuse"))}},
	}
	result := &Result{ClaimResults: []ClaimResult{{ClaimID: "claim-e2e-1"}}}
	if resultUsesBoundReuseAssets(plan, result) {
		t.Fatal("one reused CASE explained a Claim declaring two CASEs")
	}
	plan.E2EAssets = append(plan.E2EAssets, E2EAsset{CaseRef: second, Path: "second.spec.ts", SHA256: sha256Of([]byte("second"))})
	if !resultUsesBoundReuseAssets(plan, result) {
		t.Fatal("complete original CASE bindings were ignored")
	}
	plan.Claims[0].SourceRefs = []string{"TASK-001"}
	plan.Claims[0].Target = first
	if resultUsesBoundReuseAssets(plan, result) {
		t.Fatal("target prose was interpreted as dependency proof without a declared CASE source")
	}
}
