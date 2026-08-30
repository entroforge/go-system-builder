package bugprojection_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/bugprojection"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestProjectApprovedContractIsIdempotentAndSchemaValid(t *testing.T) {
	fixture := newProjectionFixture(t, []string{"finding-one", "finding-two"})
	request := fixture.request()

	first, err := bugprojection.ProjectApprovedContract(fixture.root, request)
	if err != nil {
		t.Fatalf("ProjectApprovedContract() error = %v", err)
	}
	if first.BugID != "BUG-042" || first.JSONPath == "" || first.MarkdownPath == "" {
		t.Fatalf("unexpected projection result: %#v", first)
	}
	jsonBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(first.JSONPath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-evidence.schema.json", jsonBytes); err != nil {
		t.Fatalf("canonical BUG schema: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(jsonBytes, &document); err != nil {
		t.Fatal(err)
	}
	if document["status"] != "accepted" || document["record_type"] != "canonical_bug" {
		t.Fatalf("unexpected canonical BUG: %#v", document)
	}
	if got := document["source_finding_ids"].([]any); len(got) != 2 {
		t.Fatalf("source finding set = %#v, want two findings", got)
	}

	second, err := bugprojection.ProjectApprovedContract(fixture.root, request)
	if err != nil {
		t.Fatalf("idempotent retry error = %v", err)
	}
	if second != first {
		t.Fatalf("idempotent result changed: first=%#v second=%#v", first, second)
	}
	jsonAfter, _ := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(first.JSONPath)))
	mdAfter, _ := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(first.MarkdownPath)))
	if sha256Hex(jsonAfter) != first.JSONSHA256 || sha256Hex(mdAfter) != first.MarkdownSHA256 {
		t.Fatal("idempotent retry changed a projection")
	}
	md, _ := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(first.MarkdownPath)))
	for _, required := range []string{"Canonical BUG Compatibility Projection: BUG-042", "InvestigationCase", "RepairContract", "not an S8 intake"} {
		if !strings.Contains(string(md), required) {
			t.Fatalf("markdown projection missing %q", required)
		}
	}
}

func TestProjectApprovedContractRejectsHashDriftAndExactSetMismatch(t *testing.T) {
	fixture := newProjectionFixture(t, []string{"finding-one", "finding-two"})
	request := fixture.request()

	request.Case.SHA256 = strings.Repeat("0", 64)
	if _, err := bugprojection.Project(fixture.root, request); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("Case hash drift error = %v, want actionable sha256 error", err)
	}

	request = fixture.request()
	request.FindingRefs = request.FindingRefs[:1]
	if _, err := bugprojection.Project(fixture.root, request); err == nil || !strings.Contains(err.Error(), "exact") || !strings.Contains(err.Error(), "finding-two") {
		t.Fatalf("Finding exact-set error = %v, want missing finding guidance", err)
	}
}

func TestProjectApprovedContractNeverOverwritesConflictingProjection(t *testing.T) {
	fixture := newProjectionFixture(t, []string{"finding-one"})
	request := fixture.request()
	path := filepath.Join(fixture.root, filepath.FromSlash("docs/reports/bugs/BUG-042.md"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("human-authored conflicting projection\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bugprojection.Project(fixture.root, request); err == nil || !strings.Contains(err.Error(), "conflict") || !strings.Contains(err.Error(), "BUG-042") {
		t.Fatalf("conflicting projection error = %v, want conflict guidance", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "human-authored conflicting projection\n" {
		t.Fatal("conflicting Markdown projection was overwritten")
	}
}

type projectionFixture struct {
	root        string
	caseRef     bugprojection.ArtifactRef
	contractRef bugprojection.ArtifactRef
	findingRefs []bugprojection.FindingRef
}

func newProjectionFixture(t *testing.T, findingIDs []string) *projectionFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &projectionFixture{root: root}
	for _, id := range findingIDs {
		finding := map[string]any{
			"schema_version": "1.0.0", "finding_id": id, "claim_id": "claim-" + id,
			"lens": "qa", "severity": "P1", "expected": "the value persists",
			"authority_refs": []string{"REQ-042"}, "observed": "the value disappears",
			"observation_mode": "code_inspection",
			"encounter": map[string]any{
				"journey_summary": "read -> trace -> boundary mismatch", "inspection_entry": "internal/api",
				"symbol_trail": "handler -> decoder", "wall_action": "submit payload",
				"first_bad_checkpoint": "decoded value is empty", "terminal_state": "value lost",
			},
			"reproducibility": "always", "evidence_refs": []string{"evidence/" + id + ".log"},
		}
		data := mustJSON(t, finding)
		rel := filepath.ToSlash(filepath.Join(".claude", "evidence", id+".json"))
		writeFile(t, root, rel, data)
		fixture.findingRefs = append(fixture.findingRefs, bugprojection.FindingRef{ID: id, Path: rel, SHA256: sha256Hex(data)})
	}
	caseDocument := map[string]any{
		"schema_version": "1.0.0", "case_id": "investigation-case-batch-1", "revision": 2,
		"status": "contract_approved", "source_finding_ids": findingIDs,
		"observation_batch_id": "observation-batch-1", "observation_batch_ref": ".claude/evidence/observation-batch-1.json",
		"observation_batch_sha256": strings.Repeat("a", 64), "baseline_generation": 1, "baseline_digest": strings.Repeat("b", 64),
		"grouping_rationale": "same boundary", "unexplained_finding_ids": []string{}, "failure_boundary_refs": []string{"internal/api"},
		"cross_layer_trace": map[string]any{"boundary": "api"}, "evidence_gaps": []string{}, "hypotheses": []any{}, "hypothesis_results": []any{},
		"causal_model":       map[string]any{"trigger": "payload crosses boundary", "propagation": "decoder drops value"},
		"primary_root_cause": "two incompatible payload contracts", "blast_radius": map[string]any{"surfaces": []string{"api", "storage"}},
		"detection_gap": map[string]any{"missing": "contract test"}, "route": "s9_repair", "route_reason": "implementation fault",
		"repair_contract_ref":    ".claude/evidence/repair-contract-1.json",
		"repair_contract_sha256": strings.Repeat("c", 64),
	}
	caseBytes := mustJSON(t, caseDocument)
	caseRel := ".claude/evidence/investigation-case-1.json"
	writeFile(t, root, caseRel, caseBytes)
	fixture.caseRef = bugprojection.ArtifactRef{Path: caseRel, SHA256: sha256Hex(caseBytes)}

	contractDocument := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-1", "case_id": "investigation-case-batch-1",
		"revision": 2, "status": "approved", "source_finding_ids": findingIDs,
		"root_cause_statement": "two incompatible payload contracts", "violated_invariant": "one payload authority",
		"causal_model_ref": "case://investigation-case-batch-1/causal-model", "architecture_intent": "centralize the contract",
		"repair_units":      []any{map[string]any{"id": "unit-1", "description": "centralize payload contract"}},
		"prospective_scope": []string{"internal/api", "internal/storage"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions": []string{"finding symptoms disappear"}, "root_invariant_assertions": []string{"one payload authority"},
		"detection_gap_assertions": []string{"contract test catches drift"}, "stop_escalation_conditions": []string{"REQ changes require human"},
		"approved_by": "main-session", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": strings.Repeat("d", 64),
	}
	contractBytes := mustJSON(t, contractDocument)
	contractRel := ".claude/evidence/repair-contract-1.json"
	writeFile(t, root, contractRel, contractBytes)
	fixture.contractRef = bugprojection.ArtifactRef{Path: contractRel, SHA256: sha256Hex(contractBytes)}
	// The Case pins the actual Contract hash; replace the fixture Case with its final bytes.
	caseDocument["repair_contract_sha256"] = fixture.contractRef.SHA256
	caseBytes = mustJSON(t, caseDocument)
	writeFile(t, root, caseRel, caseBytes)
	fixture.caseRef.SHA256 = sha256Hex(caseBytes)
	return fixture
}

func (f *projectionFixture) request() bugprojection.Request {
	return bugprojection.Request{
		Case: f.caseRef, Contract: f.contractRef, BugID: "BUG-042", RuntimeID: "loop-REQ-042", ReqID: "REQ-042",
		FindingRefs: f.findingRefs, OriginalAssignmentID: "assignment-s8-investigation", OriginalResponsibilityID: "S8-INVESTIGATION",
		RequiredSkillRefs: []string{"bug-resolution"}, ReviewedBy: "main-session",
	}
}

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
