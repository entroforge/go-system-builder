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
    {"id":"CONTRACT-001","category":"contract","source_refs":["docs/dev/contracts/CONTRACT-001.md"],"expected":"contract is honored","oracle":"contract checks pass","owner":"Contract Reviewer","evidence_refs":["ev-contract-1"],"disposition":"pass"},
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

func TestValidateManifestDoesNotInventHardMetricCategories(t *testing.T) {
	data := []byte(`{
  "schema_version":"1.0.0","manifest_type":"acceptance","runtime_id":"loop-req-1","baseline_generation":1,"review_round":1,
  "coverage_inventory":[{"id":"REQ-1","category":"requirement","source_refs":["REQ-1"],"expected":"x","oracle":"y","owner":"Acceptance","evidence_refs":["ev-1"],"disposition":"pass"}],
  "counterevidence":[{"id":"CE-1","inventory_id":"REQ-1","question":"what disproves x?","evidence_refs":["ev-2"],"outcome":"pass"}],
  "risks":[],"technical_debt":[],"blocking_findings":[],
  "metrics":{"requirement_coverage":1,"contract_coverage":1,"changed_path_coverage":1,"audit_area_coverage":1,"unknown_count":0,"unsupported_pass_count":0,"unowned_risk_count":0,"untracked_debt_count":0,"blocking_finding_count":0}
}`)

	summary, err := Validate(data, "acceptance")
	if err != nil || summary.InventoryCount != 1 {
		t.Fatalf("professional report should not require invented contract rows: %+v %v", summary, err)
	}
}

func TestValidateManifestRecordsUnknownRowsButCompletionRejectsThem(t *testing.T) {
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

	summary, err := Validate(data, ManifestAcceptance)
	if err != nil || summary.UnknownCount != 2 {
		t.Fatalf("recorded unknown rows must remain visible despite claimed zero metrics: %+v %v", summary, err)
	}
	if err := currentS10FixtureCompletion(t, data, ManifestAcceptance, "pass"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("unknown rows cannot complete S10: %v", err)
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
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Outcome = "pass"
	manifest.CoverageInventory = []CoverageItem{{ID: "actual-promise", Category: "requirement", Disposition: "unknown", Oracle: "", EvidenceRefs: []string{}}}
	manifest.Counterevidence = nil
	manifest.AuditAreas = nil
	// Displayed numbers cannot erase the current author's unfinished observation.
	manifest.Metrics = Metrics{RequirementCoverage: 1, ContractCoverage: 1, ChangedPathCoverage: 1}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := ValidateForOutcome(data, ManifestAcceptance, manifest.Outcome)
	if err != nil {
		t.Fatalf("professional unknown with no extra evidence/oracle should be recorded: %v", err)
	}
	if summary.UnknownCount != 1 || summary.AuditAreaCount != 0 || summary.Metrics.RequirementCoverage != 0 {
		t.Fatalf("display metrics hid unfinished professional work or required release-only areas: %+v", summary)
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

func TestValidateManifestChecksResponsibilityWithoutRewritingDuplicateRows(t *testing.T) {
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
	if !strings.Contains(err.Error(), "manifest_type") {
		t.Fatalf("wrong responsibility must be rejected: %v", err)
	}
	summary, err := Validate(data, ManifestReleaseAudit)
	if err != nil || summary.InventoryCount != 2 {
		t.Fatalf("reader must preserve the two professionally authored rows: %+v %v", summary, err)
	}
}

func TestValidateManifestAllowsRelevantReleaseAuditAreas(t *testing.T) {
	data := []byte(`{
  "schema_version":"1.0.0",
  "manifest_type":"release_audit",
  "runtime_id":"loop-req-1",
  "baseline_generation":1,
  "review_round":1,
  "coverage_inventory":[{"id":"A","category":"audit_area","source_refs":["x"],"expected":"x","oracle":"","owner":"Release Auditor","evidence_refs":[],"disposition":"pass"}],
  "counterevidence":[],
  "audit_areas":[{"id":"state_machine","conclusion":"pass","owner":"Release Auditor","evidence_refs":[]}],
  "risks":[],"technical_debt":[],"blocking_findings":[],
  "metrics":{"requirement_coverage":1,"contract_coverage":1,"changed_path_coverage":1,"audit_area_coverage":0.125,"unknown_count":0,"unsupported_pass_count":0,"unowned_risk_count":0,"untracked_debt_count":0,"blocking_finding_count":0}
}`)

	summary, err := Validate(data, ManifestReleaseAudit)
	if err != nil {
		t.Fatalf("one relevant audit area needs no invented categories or counterevidence: %v", err)
	}
	if summary.AuditAreaCount != 1 || summary.CounterevidenceCount != 0 || summary.UnsupportedPassCount != 0 || summary.UnknownCount != 0 {
		t.Fatalf("relevant professional pass was treated as an evidence qualification: %+v", summary)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Outcome = "approved"
	manifest.AuditAreas[0].Conclusion = "fail"
	manifest.Metrics.AuditAreaCoverage = 1
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	summary, err = ValidateForOutcome(data, ManifestReleaseAudit, manifest.Outcome)
	if err != nil || summary.UnknownCount != 1 || summary.Metrics.AuditAreaCoverage != 0 {
		t.Fatalf("honest failed observation must be recorded and remain unfinished despite displayed 1.0: summary=%+v err=%v", summary, err)
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

func TestValidateEvidenceArtifactRecordsBlockedLedgerButRejectsCompletion(t *testing.T) {
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
	if err := ValidateEvidenceArtifact(root, ManifestReleaseAudit, envelope); err != nil {
		t.Fatalf("negative professional facts must be recordable: %v", err)
	}
	if err := currentS10FixtureCompletion(t, manifest, ManifestReleaseAudit, "approved"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("approval text cannot override reported blockers: %v", err)
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
func TestValidateManifestPreservesProfessionalRiskClassification(t *testing.T) {
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
	summary, err := Validate(encoded, ManifestAcceptance)
	if err != nil || summary.BlockingFindingCount != 0 {
		t.Fatalf("reader must preserve the author's risk classification: %+v %v", summary, err)
	}
	var recorded Manifest
	if err := json.Unmarshal(encoded, &recorded); err != nil || len(recorded.Risks) != 1 || recorded.Risks[0].Severity != "P0" {
		t.Fatalf("professional severity must remain available for review: %+v %v", recorded.Risks, err)
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
	contractSHA := write("docs/dev/contracts/BE-001.md", contract)
	taskSHA := write("docs/dev/tasks/TASK-001.md", task)
	planSHA := write(".claude/review/plans/review-plan-2.json", plan)
	state := map[string]any{
		"bound_req": map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "sha256": reqSHA},
		"baseline":  map[string]any{"generation": 1},
		"review": map[string]any{
			"round": 2,
			"plan":  map[string]any{"path": ".claude/review/plans/review-plan-2.json", "sha256": planSHA},
		},
		"documents": []any{
			map[string]any{"id": "BE-001", "kind": "contract", "path": "docs/dev/contracts/BE-001.md", "sha256": contractSHA, "generation": 1},
			map[string]any{"id": "TASK-001", "kind": "task", "path": "docs/dev/tasks/TASK-001.md", "sha256": taskSHA, "generation": 1},
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

// Exercise the actual completion consumer independently from report ingestion.
func currentS10FixtureCompletion(t *testing.T, data []byte, kind, conclusion string) error {
	t.Helper()
	root := t.TempDir()
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	key := "audit_manifest_path"
	if kind == ManifestReleaseAudit {
		key = "audit_manifest_path"
	}
	envelope, err := json.Marshal(map[string]any{key: "manifest.json", "conclusion": conclusion})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evidence.json"), envelope, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": manifest.RuntimeID,
		"baseline":   map[string]any{"generation": manifest.BaselineGeneration},
		"review":     map[string]any{"round": manifest.ReviewRound},
		"evidence":   []any{map[string]any{"id": "current-report", "path": "evidence.json", "kind": kind, "status": "valid", "baseline_generation": manifest.BaselineGeneration, "review_round": manifest.ReviewRound}},
	}
	return ValidateCurrentS10Evidence(root, state, kind)
}
