package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestS10ManifestValidateCommandPrintsDerivedSummary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "acceptance-manifest.json")
	data := validS10ManifestJSON(t, "acceptance")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "manifest", "validate", "--root", root, "--file", "acceptance-manifest.json", "--type", "acceptance"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s10 manifest validate failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("validate output is not JSON: %v", err)
	}
	if result["valid"] != true || result["manifest_type"] != "acceptance" || result["inventory_count"] != float64(4) {
		t.Fatalf("unexpected validation summary: %#v", result)
	}
	if !strings.Contains(result["next"].(string), "runtime evidence add") {
		t.Fatalf("summary must tell the Agent how to register evidence: %#v", result)
	}
}

func TestS10ManifestValidateCommandExplainsRecovery(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "incomplete.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "manifest", "validate", "--root", root, "--file", "incomplete.json", "--type", "acceptance"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("incomplete S10 manifest must fail")
	}
	for _, want := range []string{"s10-audit-manifest.schema.json", "coverage_inventory", "loop-harness s10 manifest validate"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr %q does not contain recovery hint %q", stderr.String(), want)
		}
	}
}

func TestS10ManifestValidateCommandAcceptsRoutedBlockedOutcome(t *testing.T) {
	root := t.TempDir()
	manifest := validS10ManifestJSON(t, "release_audit")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["blocking_findings"] = []any{map[string]any{"id": "BLOCK-1", "route": "TR-018"}}
	decoded["metrics"].(map[string]any)["blocking_finding_count"] = 1
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "blocked-release-audit.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "manifest", "validate", "--root", root, "--file", "blocked-release-audit.json", "--type", "release_audit", "--outcome", "blocked"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("blocked release audit validation failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("validate output is not JSON: %v", err)
	}
	if result["valid"] != true || result["outcome"] != "blocked" {
		t.Fatalf("unexpected blocked validation summary: %#v", result)
	}
	if !strings.Contains(result["next"].(string), "TR-018") {
		t.Fatalf("blocked validation must explain its route: %#v", result)
	}
}

func TestS10ManifestValidateRejectsSymlinkOutsideRepository(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	manifest := validS10ManifestJSON(t, "acceptance")
	externalPath := filepath.Join(external, "manifest.json")
	if err := os.WriteFile(externalPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalPath, filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "manifest", "validate", "--root", root, "--file", "manifest.json", "--type", "acceptance"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("manifest validation must reject a symlink escaping the repository: stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "inside the repository") {
		t.Fatalf("recovery must explain the repository boundary: %s", stderr.String())
	}
}

func TestS10StatusReportsTheCurrentRoundAndNextAction(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": "loop-test", "revision": 17,
		"lifecycle": map[string]any{"state": "acceptance", "phase": nil},
		"review":    map[string]any{"round": 3}, "evidence": []any{},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s10 status failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("status output is not JSON: %v", err)
	}
	if result["stage"] != "S10" || result["lifecycle_state"] != "acceptance" || result["review_round"] != float64(3) {
		t.Fatalf("unexpected S10 status: %#v", result)
	}
	acceptanceStatus := result["acceptance"].(map[string]any)
	if acceptanceStatus["state"] != "missing" || !strings.Contains(result["next"].(string), "manifest") {
		t.Fatalf("status must expose the missing manifest and next action: %#v", result)
	}
}

func TestS10StatusRejectsManifestFromStaleBaseline(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := validS10ManifestJSON(t, "acceptance")
	manifestPath := filepath.Join(root, "acceptance-manifest.json")
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-acc",
		"kind":                    "acceptance",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"review_round":            1,
		"producer_agent_id":       "s10-agent",
		"producer_responsibility": "Acceptance",
		"conclusion":              "pass",
		"audit_manifest_path":     "acceptance-manifest.json",
		"audit_manifest_sha256":   sha256Hex(manifest),
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "acceptance.json"), envelopeData, 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := []any{}
	for _, id := range []string{"ev-acc", "ev:REQ-AC-001", "ev:CONTRACT-001", "ev:PATH-001", "ev:AUDIT-001", "ev:counter:REQ-AC-001", "ev:counter:CONTRACT-001", "ev:counter:PATH-001", "ev:counter:AUDIT-001"} {
		path := "evidence/" + strings.ReplaceAll(id, ":", "-") + ".json"
		data := envelopeData
		if id != "ev-acc" {
			data = []byte("supporting evidence")
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), data, 0o644); err != nil {
			t.Fatal(err)
		}
		kind := "support"
		if id == "ev-acc" {
			kind = "acceptance"
		}
		evidence = append(evidence, map[string]any{
			"id": id, "kind": kind, "path": path, "sha256": sha256Hex(data),
			"status": "valid", "baseline_generation": 1, "review_round": 1,
		})
	}
	state := map[string]any{
		"runtime_id": "loop-test", "revision": 17,
		"lifecycle": map[string]any{"state": "acceptance", "phase": nil},
		"baseline":  map[string]any{"generation": 2},
		"review":    map[string]any{"round": 2}, "evidence": evidence,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s10 status failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	acceptanceStatus := result["acceptance"].(map[string]any)
	if acceptanceStatus["state"] != "invalid" || !strings.Contains(acceptanceStatus["error"].(string), "binding") {
		t.Fatalf("stale S10 manifest must be invalid with a binding explanation: %#v", acceptanceStatus)
	}
}

func TestS10StatusReportsBlockedManifestRoute(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := validS10ManifestJSON(t, "release_audit")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["blocking_findings"] = []any{map[string]any{"id": "BLOCK-1", "route": "TR-018"}}
	decoded["metrics"].(map[string]any)["blocking_finding_count"] = 1
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release-audit-manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-audit", "kind": "release_audit",
		"runtime_id": "loop-test", "baseline_generation": 1, "review_round": 1,
		"producer_agent_id": "s10-agent", "producer_responsibility": "Release Auditor",
		"conclusion": "blocked", "audit_manifest_path": "release-audit-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release-audit.json"), envelopeData, 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := []any{map[string]any{
		"id": "ev-audit", "kind": "release_audit", "path": "release-audit.json",
		"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": 1, "review_round": 1,
	}}
	// RC-16: the status path now applies the same evidence-reference audit as
	// the gate for every outcome, so each referenced id must resolve to a
	// current, SHA-verified evidence artifact on disk.
	supportData := []byte("supporting evidence for the blocked release audit")
	if err := os.WriteFile(filepath.Join(root, "support.json"), supportData, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"ev:REQ-AC-001", "ev:CONTRACT-001", "ev:PATH-001", "ev:AUDIT-001",
		"ev:counter:REQ-AC-001", "ev:counter:CONTRACT-001", "ev:counter:PATH-001", "ev:counter:AUDIT-001",
		"ev:area:state_machine", "ev:area:transaction_uow", "ev:area:concurrency_idempotency",
		"ev:area:data_migration", "ev:area:call_sites_topology", "ev:area:observability_errors",
		"ev:area:verification_evidence", "ev:area:docs_release_scope",
	} {
		evidence = append(evidence, map[string]any{
			"id": id, "kind": "human_decision", "path": "support.json", "sha256": sha256Hex(supportData),
			"status": "valid", "baseline_generation": 1, "review_round": 1,
		})
	}
	state := map[string]any{
		"runtime_id": "loop-test", "revision": 17,
		"lifecycle": map[string]any{"state": "release_audit", "phase": nil},
		"baseline":  map[string]any{"generation": 1}, "review": map[string]any{"round": 1},
		"evidence": evidence,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s10", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s10 status failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	release := result["release_audit"].(map[string]any)
	if release["state"] != "blocked" || !strings.Contains(release["next"].(string), "TR-018") {
		t.Fatalf("status must expose the blocked route: %#v", release)
	}
}

func validS10ManifestJSON(t *testing.T, kind string) []byte {
	t.Helper()
	items := []any{}
	counterevidence := []any{}
	for _, item := range []struct{ id, category string }{
		{"REQ-AC-001", "requirement"}, {"CONTRACT-001", "contract"},
		{"PATH-001", "changed_path"}, {"AUDIT-001", "audit_area"},
	} {
		items = append(items, map[string]any{
			"id": item.id, "category": item.category, "source_refs": []string{"source:" + item.id},
			"expected": "expected " + item.id, "oracle": "oracle " + item.id,
			"owner": "S10 reviewer", "evidence_refs": []string{"ev:" + item.id}, "disposition": "pass",
		})
		counterevidence = append(counterevidence, map[string]any{
			"id": "CE-" + item.id, "inventory_id": item.id, "question": "what disproves " + item.id + "?",
			"evidence_refs": []string{"ev:counter:" + item.id}, "outcome": "pass",
		})
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_type": kind, "runtime_id": "loop-test",
		"baseline_generation": 1, "review_round": 1, "coverage_inventory": items, "counterevidence": counterevidence,
		"risks": []any{}, "technical_debt": []any{}, "blocking_findings": []any{},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1, "audit_area_coverage": 1,
			"unknown_count": 0, "unsupported_pass_count": 0, "unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0,
		},
	}
	if kind == "release_audit" {
		areas := []any{}
		for _, id := range []string{"state_machine", "transaction_uow", "concurrency_idempotency", "data_migration", "call_sites_topology", "observability_errors", "verification_evidence", "docs_release_scope"} {
			areas = append(areas, map[string]any{"id": id, "conclusion": "pass", "owner": "Release Auditor", "evidence_refs": []string{"ev:area:" + id}})
		}
		manifest["audit_areas"] = areas
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestS10ManifestInitEmitsSchemaCompleteTemplate covers RC-18 F-H2: the init
// verb scaffolds a manifest template that already carries the full
// s10-audit-manifest.schema.json required set (audit_area_coverage included
// for release_audit), so an Agent never has to reverse-engineer the envelope
// from source. Every agent-supplied fact stays a <PLACEHOLDER>.
func TestS10ManifestInitEmitsSchemaCompleteTemplate(t *testing.T) {
	for _, manifestType := range []string{"acceptance", "release_audit"} {
		var stdout, stderr bytes.Buffer
		code := cli.Run([]string{"s10", "manifest", "init", "--type", manifestType}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("s10 manifest init --type %s failed: code=%d stderr=%s", manifestType, code, stderr.String())
		}
		var template map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &template); err != nil {
			t.Fatalf("template is not JSON: %v", err)
		}
		if template["schema_version"] != "1.0.0" || template["manifest_type"] != manifestType {
			t.Fatalf("unexpected template header: %#v", template)
		}
		metrics, _ := template["metrics"].(map[string]any)
		if metrics == nil {
			t.Fatalf("template metrics missing: %#v", template)
		}
		if _, hasAudit := metrics["audit_area_coverage"]; hasAudit != (manifestType == "release_audit") {
			t.Fatalf("metrics.audit_area_coverage presence = %v for %s", hasAudit, manifestType)
		}
		for _, key := range []string{"coverage_inventory", "counterevidence", "risks", "technical_debt", "blocking_findings"} {
			if _, ok := template[key]; !ok {
				t.Fatalf("template is missing required key %s: %#v", key, template)
			}
		}
		if template["runtime_id"] != "<RUNTIME-ID>" || template["baseline_generation"] != "<BASELINE-GENERATION-INT>" {
			t.Fatalf("template must keep agent-supplied facts as <PLACEHOLDER> tokens: %#v", template)
		}
	}
}

// TestS10ManifestInitRejectsUnknownTypeAndRefusesOverwrite proves the init
// verb fails closed on an unknown --type and never overwrites an existing
// manifest file (O_EXCL).
func TestS10ManifestInitRejectsUnknownTypeAndRefusesOverwrite(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"s10", "manifest", "init", "--type", "chaos"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("unknown --type must be rejected")
	}

	root := t.TempDir()
	existing := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(existing, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"s10", "manifest", "init", "--type", "acceptance", "--root", root, "--emit-template", "manifest.json"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("init must refuse to overwrite an existing manifest")
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}" {
		t.Fatalf("existing manifest was modified: %q", data)
	}
}
