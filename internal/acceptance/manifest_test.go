package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// validAcceptanceManifest returns a clean, complete acceptance manifest
// (no audit_area rows — that hard category is release-audit-only).
func validAcceptanceManifest(t *testing.T) []byte {
	t.Helper()
	items := []any{}
	counterevidence := []any{}
	for _, item := range []struct{ id, category string }{
		{"REQ-1", "requirement"},
		{"CONTRACT-1", "contract"},
		{"PATH-1", "changed_path"},
	} {
		items = append(items, map[string]any{
			"id": item.id, "category": item.category, "source_refs": []string{"source:" + item.id},
			"expected": "expected " + item.id, "oracle": "oracle " + item.id, "owner": "S10 reviewer",
			"evidence_refs": []string{"ev-audit"}, "disposition": "pass",
		})
		counterevidence = append(counterevidence, map[string]any{
			"id": "CE-" + item.id, "inventory_id": item.id, "question": "what disproves " + item.id + "?",
			"evidence_refs": []string{"ev-check"}, "outcome": "pass",
		})
	}
	data, err := json.Marshal(map[string]any{
		"schema_version": "1.0.0", "manifest_type": "acceptance", "runtime_id": "loop-1",
		"baseline_generation": 1, "review_round": 1, "coverage_inventory": items,
		"counterevidence": counterevidence, "risks": []any{}, "technical_debt": []any{},
		"blocking_findings": []any{},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1,
			"unknown_count": 0, "unsupported_pass_count": 0,
			"unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0,
		},
	})
	if err != nil {
		t.Fatalf("marshal acceptance manifest: %v", err)
	}
	return data
}

func TestValidateManifestAcceptsCompleteAcceptanceAudit(t *testing.T) {
	data := []byte(`{
  "schema_version": "1.0.0",
  "manifest_type": "acceptance",
  "runtime_id": "loop-req-1",
  "baseline_generation": 3,
  "review_round": 2,
  "coverage_inventory": [
    {"id":"REQ-AC-001","category":"requirement","source_refs":["REQ-001#ac-1"],"expected":"record saves","oracle":"reload shows value","owner":"Acceptance","evidence_refs":["ev-e2e-1"],"disposition":"pass"},
    {"id":"CONTRACT-001","category":"contract","source_refs":["docs/contracts/CONTRACT-001.md"],"expected":"contract is honored","oracle":"contract checks pass","owner":"Contract Reviewer","evidence_refs":["ev-contract-1"],"disposition":"pass"},
    {"id":"PATH-001","category":"changed_path","source_refs":["internal/service.go"],"expected":"path reviewed","oracle":"code review and test","owner":"Architecture / Code Audit","evidence_refs":["ev-code-1"],"disposition":"pass"},
    {"id":"AUDIT-001","category":"audit_area","source_refs":["release scope"],"expected":"system impact reviewed","oracle":"release audit","owner":"Release Auditor","evidence_refs":["ev-audit-1"],"disposition":"pass"},
    {"id":"OPS-001","category":"operations","source_refs":["runbook"],"expected":"rollback is documented","oracle":"operator can execute rollback","owner":"Data / Migration / Operations","evidence_refs":["ev-ops-1"],"disposition":"not_applicable","na_reason":"No deployment or migration surface is in scope for this REQ."}
  ],
  "counterevidence": [
    {"id":"CE-REQ-AC-001","inventory_id":"REQ-AC-001","question":"What would prove saving is not durable?","evidence_refs":["ev-e2e-negative-1"],"outcome":"pass"},
    {"id":"CE-CONTRACT-001","inventory_id":"CONTRACT-001","question":"What would prove the contract is broken?","evidence_refs":["ev-contract-2"],"outcome":"pass"},
    {"id":"CE-PATH-001","inventory_id":"PATH-001","question":"What would prove the changed path is unreviewed?","evidence_refs":["ev-code-2"],"outcome":"pass"},
    {"id":"CE-AUDIT-001","inventory_id":"AUDIT-001","question":"What would prove a system impact was omitted?","evidence_refs":["ev-audit-2"],"outcome":"pass"},
    {"id":"CE-OPS-001","inventory_id":"OPS-001","question":"What would prove an operational surface was omitted?","evidence_refs":["ev-scope-1"],"outcome":"not_applicable"}
  ],
  "risks": [],
  "technical_debt": [],
  "blocking_findings": [],
  "metrics": {
    "requirement_coverage": 1,
    "contract_coverage": 1,
    "changed_path_coverage": 1,
    "audit_area_coverage": 1,
    "unknown_count": 0,
    "unsupported_pass_count": 0,
    "unowned_risk_count": 0,
    "untracked_debt_count": 0,
    "blocking_finding_count": 0
  }
}`)

	report, err := Validate(data, "acceptance")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.InventoryCount != 5 || report.CounterevidenceCount != 5 {
		t.Fatalf("report = %#v", report)
	}
	if report.UnknownCount != 0 || report.UnsupportedPassCount != 0 {
		t.Fatalf("report should be clean: %#v", report)
	}
}

func TestValidateManifestRejectsMissingHardMetricCategory(t *testing.T) {
	data := []byte(`{
  "schema_version":"1.0.0","manifest_type":"acceptance","runtime_id":"loop-req-1","baseline_generation":1,"review_round":1,
  "coverage_inventory":[{"id":"REQ-1","category":"requirement","source_refs":["REQ-1"],"expected":"x","oracle":"y","owner":"Acceptance","evidence_refs":["ev-1"],"disposition":"pass"}],
  "counterevidence":[{"id":"CE-1","inventory_id":"REQ-1","question":"what disproves x?","evidence_refs":["ev-2"],"outcome":"pass"}],
  "risks":[],"technical_debt":[],"blocking_findings":[],
  "metrics":{"requirement_coverage":1,"contract_coverage":1,"changed_path_coverage":1,"audit_area_coverage":1,"unknown_count":0,"unsupported_pass_count":0,"unowned_risk_count":0,"untracked_debt_count":0,"blocking_finding_count":0}
}`)

	_, err := Validate(data, "acceptance")
	if err == nil || !strings.Contains(err.Error(), "contract") {
		t.Fatalf("manifest without a contract category must be rejected explicitly: %v", err)
	}
}

func TestValidateManifestRejectsIncompleteCoverageAndCounterevidence(t *testing.T) {
	data := []byte(`{
  "schema_version":"1.0.0",
  "manifest_type":"acceptance",
  "runtime_id":"loop-req-1",
  "baseline_generation":1,
  "review_round":1,
  "coverage_inventory":[{"id":"REQ-1","category":"requirement","source_refs":["REQ-1"],"expected":"x","oracle":"y","owner":"Acceptance","evidence_refs":["ev-unknown"],"disposition":"unknown"}],
  "counterevidence":[{"id":"CE-1","inventory_id":"REQ-1","question":"what disproves x?","evidence_refs":["ev-negative"],"outcome":"unknown"}],
  "risks":[],"technical_debt":[],"blocking_findings":[],
  "metrics":{"requirement_coverage":0,"contract_coverage":1,"changed_path_coverage":1,"audit_area_coverage":1,"unknown_count":0,"unsupported_pass_count":0,"unowned_risk_count":0,"untracked_debt_count":0,"blocking_finding_count":0}
}`)

	_, err := Validate(data, "acceptance")
	if err == nil {
		t.Fatal("incomplete manifest must be rejected")
	}
	for _, want := range []string{"REQ-1", "unknown", "unknown_count"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not identify %q", err, want)
		}
	}
}

func TestValidateBlockedManifestAllowsUnknownCounterevidenceWithoutEvidenceRefs(t *testing.T) {
	data := blockedManifest(t, ManifestAcceptance)
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	counterevidence := decoded["counterevidence"].([]any)
	counterevidence[0].(map[string]any)["outcome"] = "unknown"
	counterevidence[0].(map[string]any)["evidence_refs"] = []any{}
	decoded["counterevidence"] = counterevidence
	metrics := decoded["metrics"].(map[string]any)
	metrics["unknown_count"] = 1
	data, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if _, err := ValidateForOutcome(data, ManifestAcceptance, "review_required"); err != nil {
		t.Fatalf("review-required manifest may record an unknown counterevidence question without evidence refs: %v", err)
	}
}

func TestValidateAcceptanceDoesNotRequireAuditAreaMetric(t *testing.T) {
	data := validAcceptanceManifestWithoutAuditAreaMetric(t)
	if _, err := Validate(data, ManifestAcceptance); err != nil {
		t.Fatalf("acceptance manifest should not require release-only audit_area_coverage: %v", err)
	}
}

func validAcceptanceManifestWithoutAuditAreaMetric(t *testing.T) []byte {
	t.Helper()
	data := blockedManifest(t, ManifestAcceptance)
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	decoded["blocking_findings"] = []any{}
	decoded["metrics"].(map[string]any)["blocking_finding_count"] = 0
	items := decoded["coverage_inventory"].([]any)
	filteredItems := make([]any, 0, len(items))
	for _, raw := range items {
		if raw.(map[string]any)["category"] != "audit_area" {
			filteredItems = append(filteredItems, raw)
		}
	}
	decoded["coverage_inventory"] = filteredItems
	counterevidence := decoded["counterevidence"].([]any)
	filteredCounterevidence := make([]any, 0, len(counterevidence))
	for _, raw := range counterevidence {
		if decodedItem, ok := raw.(map[string]any); ok && strings.HasPrefix(decodedItem["inventory_id"].(string), "AUDIT") {
			continue
		}
		filteredCounterevidence = append(filteredCounterevidence, raw)
	}
	decoded["counterevidence"] = filteredCounterevidence
	delete(decoded["metrics"].(map[string]any), "audit_area_coverage")
	data, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return data
}

func TestValidateManifestRejectsMismatchedTypeAndDuplicateRows(t *testing.T) {
	data := []byte(`{
  "schema_version":"1.0.0",
  "manifest_type":"release_audit",
  "runtime_id":"loop-req-1",
  "baseline_generation":1,
  "review_round":1,
  "coverage_inventory":[
    {"id":"A","category":"audit_area","source_refs":["x"],"expected":"x","oracle":"y","owner":"Release Auditor","evidence_refs":["ev"],"disposition":"pass"},
    {"id":"A","category":"audit_area","source_refs":["x"],"expected":"x","oracle":"y","owner":"Release Auditor","evidence_refs":["ev"],"disposition":"pass"}
  ],
  "counterevidence":[{"id":"CE-A","inventory_id":"A","question":"what disproves x?","evidence_refs":["ev-ce"],"outcome":"pass"}],
  "risks":[],"technical_debt":[],"blocking_findings":[],
  "metrics":{"requirement_coverage":1,"contract_coverage":1,"changed_path_coverage":1,"audit_area_coverage":1,"unknown_count":0,"unsupported_pass_count":0,"unowned_risk_count":0,"untracked_debt_count":0,"blocking_finding_count":0}
}`)

	_, err := Validate(data, "acceptance")
	if err == nil {
		t.Fatal("mismatched or duplicate manifest must be rejected")
	}
	for _, want := range []string{"manifest_type", "release_audit", "duplicate"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not identify %q", err, want)
		}
	}
}

func TestValidateManifestRequiresReleaseAuditAreas(t *testing.T) {
	data := []byte(`{
  "schema_version":"1.0.0",
  "manifest_type":"release_audit",
  "runtime_id":"loop-req-1",
  "baseline_generation":1,
  "review_round":1,
  "coverage_inventory":[{"id":"A","category":"audit_area","source_refs":["x"],"expected":"x","oracle":"y","owner":"Release Auditor","evidence_refs":["ev"],"disposition":"pass"}],
  "counterevidence":[{"id":"CE-A","inventory_id":"A","question":"what disproves x?","evidence_refs":["ev-ce"],"outcome":"pass"}],
  "audit_areas":[{"id":"state_machine","conclusion":"pass","owner":"Release Auditor","evidence_refs":["ev"]}],
  "risks":[],"technical_debt":[],"blocking_findings":[],
  "metrics":{"requirement_coverage":1,"contract_coverage":1,"changed_path_coverage":1,"audit_area_coverage":0.125,"unknown_count":0,"unsupported_pass_count":0,"unowned_risk_count":0,"untracked_debt_count":0,"blocking_finding_count":0}
}`)

	_, err := Validate(data, "release_audit")
	if err == nil {
		t.Fatal("release audit must require all audit areas")
	}
	for _, want := range []string{"audit_areas", "8", "missing"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not identify %q", err, want)
		}
	}
}

func TestValidateEvidenceArtifactAcceptsBlockedReleaseAudit(t *testing.T) {
	root := t.TempDir()
	manifest := blockedManifest(t, "release_audit")
	manifestPath := filepath.Join(root, "release-audit-manifest.json")
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(manifest)
	envelope, err := json.Marshal(map[string]any{
		"audit_manifest_path":   "release-audit-manifest.json",
		"audit_manifest_sha256": hex.EncodeToString(hash[:]),
		"conclusion":            "blocked",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEvidenceArtifact(root, ManifestReleaseAudit, envelope); err != nil {
		t.Fatalf("blocked release audit should be recordable: %v", err)
	}
}

func TestValidateEvidenceArtifactRejectsBlockedLedgerForApproval(t *testing.T) {
	root := t.TempDir()
	manifest := blockedManifest(t, ManifestReleaseAudit)
	manifestPath := filepath.Join(root, "release-audit-manifest.json")
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(manifest)
	envelope, err := json.Marshal(map[string]any{
		"audit_manifest_path":   "release-audit-manifest.json",
		"audit_manifest_sha256": hex.EncodeToString(hash[:]),
		"conclusion":            "approved",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEvidenceArtifact(root, ManifestReleaseAudit, envelope); err == nil {
		t.Fatal("an approval envelope must not accept a manifest with blocking findings")
	}
}

func TestValidateEvidenceArtifactAcceptsReviewRequiredAcceptance(t *testing.T) {
	root := t.TempDir()
	manifest := blockedManifest(t, ManifestAcceptance)
	manifestPath := filepath.Join(root, "acceptance-manifest.json")
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(manifest)
	envelope, err := json.Marshal(map[string]any{
		"audit_manifest_path":   "acceptance-manifest.json",
		"audit_manifest_sha256": hex.EncodeToString(hash[:]),
		"conclusion":            "review_required",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEvidenceArtifact(root, ManifestAcceptance, envelope); err != nil {
		t.Fatalf("review-required acceptance should be recordable: %v", err)
	}
}

func blockedManifest(t *testing.T, manifestType string) []byte {
	t.Helper()
	items := []any{}
	counterevidence := []any{}
	for _, item := range []struct {
		id, category string
	}{
		{"REQ-1", "requirement"},
		{"CONTRACT-1", "contract"},
		{"PATH-1", "changed_path"},
		{"AUDIT-1", "audit_area"},
	} {
		items = append(items, map[string]any{
			"id": item.id, "category": item.category, "source_refs": []string{"source:" + item.id},
			"expected": "expected " + item.id, "oracle": "oracle " + item.id, "owner": "S10 reviewer",
			"evidence_refs": []string{"ev-audit"}, "disposition": "pass",
		})
		counterevidence = append(counterevidence, map[string]any{
			"id": "CE-" + item.id, "inventory_id": item.id, "question": "what disproves " + item.id + "?",
			"evidence_refs": []string{"ev-check"}, "outcome": "pass",
		})
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_type": manifestType, "runtime_id": "loop-1",
		"baseline_generation": 1, "review_round": 1, "coverage_inventory": items,
		"counterevidence": counterevidence, "risks": []any{}, "technical_debt": []any{},
		"blocking_findings": []any{map[string]any{"id": "BLOCK-1", "route": "TR-018"}},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1,
			"audit_area_coverage": 1, "unknown_count": 0, "unsupported_pass_count": 0,
			"unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 1,
		},
	}
	if manifestType == ManifestReleaseAudit {
		areas := []any{}
		for _, id := range AuditAreaIDs {
			areas = append(areas, map[string]any{"id": id, "conclusion": "pass", "owner": "Release Auditor", "evidence_refs": []string{"ev-audit"}})
		}
		manifest["audit_areas"] = areas
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal blocked manifest: %v", err)
	}
	return data
}

// RC-02 (S10-10): a P0 risk is business-blocking by definition. Parking it
// as a monitored non-blocking risk lets a known blocker ride into S11; the
// manifest must refuse it.
func TestValidateManifestRejectsP0RiskAsNonBlocking(t *testing.T) {
	data := validAcceptanceManifest(t)
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	decoded["risks"] = []any{map[string]any{
		"id": "RISK-1", "severity": "P0", "impact": "data loss on rollback",
		"owner": "Release Auditor", "tracking_ref": "TASK-1", "recovery_point": "pre-deploy snapshot",
	}}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	_, err = Validate(encoded, ManifestAcceptance)
	if err == nil {
		t.Fatal("a P0 risk must not be parkable as a non-blocking risk entering S11")
	}
	for _, want := range []string{"RISK-1", "P0", "blocking_findings"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not identify %q", err, want)
		}
	}
}

// A P1 risk remains a monitorable non-blocking risk with owner/tracking.
func TestValidateManifestAcceptsP1Risk(t *testing.T) {
	data := validAcceptanceManifest(t)
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	decoded["risks"] = []any{map[string]any{
		"id": "RISK-2", "severity": "P1", "impact": "slower cold start",
		"owner": "Release Auditor", "tracking_ref": "TASK-2", "recovery_point": "n/a",
	}}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if _, err := Validate(encoded, ManifestAcceptance); err != nil {
		t.Fatalf("a P1 risk is monitorable and must validate: %v", err)
	}
}

func TestInventoryAuthorityRejectsShrunkAndInventedRows(t *testing.T) {
	authority := InventoryAuthority{
		RequirementIDs: []string{"REQ-001/FR-001", "REQ-001/FR-002"},
		ContractIDs:    []string{"BE-001"},
		TaskIDs:        []string{"TASK-001#closing-contract"},
		ClaimIDs:       []string{"claim-qa-1"},
		ChangedPaths:   []string{"internal/service.go"},
	}
	items := []CoverageItem{
		{ID: "REQ-001/FR-001", Category: "requirement"},
		{ID: "CONTRACT-FAKE", Category: "contract"},
		{ID: "TASK-001#closing-contract", Category: "task"},
		{ID: "claim-qa-1", Category: "claim"},
		{ID: "path:internal/service.go", Category: "changed_path"},
	}
	issues := inventoryAuthorityIssues(items, authority)
	joined := strings.Join(issues, "; ")
	for _, want := range []string{"REQ-001/FR-002", "BE-001", "CONTRACT-FAKE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("authority issues %q do not identify %q", joined, want)
		}
	}
}

func TestBuildS10InventoryAuthorityUsesPinnedRuntimeFacts(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, data []byte) string {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	req := []byte("# REQ-001\n| FR-001 | first |\n| FR-002 | second |\n")
	contract := []byte("# BE-001\n")
	task := []byte("# TASK-001\n## Closing Contract\n")
	plan := []byte(`{"review_plan_id":"review-plan-2","review_round":2,"baseline_generation":1,"claims":[{"claim_id":"claim-qa-1"}]}`)
	reqSHA := write("docs/requirements/REQ-001.md", req)
	contractSHA := write("docs/contracts/BE-001.md", contract)
	taskSHA := write("docs/tasks/TASK-001.md", task)
	planSHA := write(".claude/review/plans/review-plan-2.json", plan)
	state := map[string]any{
		"bound_req": map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "sha256": reqSHA},
		"baseline":  map[string]any{"generation": 1},
		"review": map[string]any{
			"round": 2,
			"plan":  map[string]any{"path": ".claude/review/plans/review-plan-2.json", "sha256": planSHA},
		},
		"documents": []any{
			map[string]any{"id": "BE-001", "kind": "contract", "path": "docs/contracts/BE-001.md", "sha256": contractSHA, "generation": 1},
			map[string]any{"id": "TASK-001", "kind": "task", "path": "docs/tasks/TASK-001.md", "sha256": taskSHA, "generation": 1},
		},
	}
	authority, err := BuildS10InventoryAuthority(root, state, Baseline{ChangedPaths: []string{"internal/service.go"}})
	if err != nil {
		t.Fatalf("BuildS10InventoryAuthority: %v", err)
	}
	if got, want := authority.RequirementIDs, []string{"REQ-001/FR-001", "REQ-001/FR-002"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requirement authority = %#v, want %#v", got, want)
	}
	if got, want := authority.TaskIDs, []string{"TASK-001#closing-contract"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task authority = %#v, want %#v", got, want)
	}
	if got, want := authority.ClaimIDs, []string{"claim-qa-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claim authority = %#v, want %#v", got, want)
	}
}
