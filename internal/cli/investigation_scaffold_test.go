package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestS8IntakeScaffold proves the RC-12 Step C contract on
// `runtime investigation ingest --emit-template`:
//
//  1. The ingest still commits the real Case through the CAS (the scaffold
//     is additive, never a substitute for the authority transaction).
//  2. --emit-template <path> writes a case-template.json scaffold with the
//     ingested Case identity, a RouteRequest draft and a RepairContract
//     placeholder — no s8 subcommand is involved.
//  3. The scaffold is dry-run: every actionable field is a TODO marker and
//     the document discloses that it must not be submitted.
//  4. --emit-template - prints the scaffold to stdout instead of a file.
func TestS8IntakeScaffold(t *testing.T) {
	root, _ := seedS8ScaffoldBatch(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")

	templateRel := ".claude/review/investigation/case-template.json"
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "investigation", "ingest", "--root", root,
		"--grouping-rationale", "the sealed batch is the provisional grouping boundary",
		"--emit-template", templateRel,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ingest --emit-template code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	// 1. The authority transaction still happened.
	committed, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(committed), `"investigation-case-observation-batch-r1"`) {
		t.Fatalf("ingest must still commit the Case pointer:\n%s", string(committed))
	}

	// 2. The scaffold file exists with the full dry-run shape.
	data, err := os.ReadFile(filepath.Join(root, templateRel))
	if err != nil {
		t.Fatalf("read scaffold: %v; stderr=%s", err, stderr.String())
	}
	var scaffold map[string]any
	if err := json.Unmarshal(data, &scaffold); err != nil {
		t.Fatal(err)
	}
	if scaffold["template"] != "case-template" || scaffold["case_id"] != "investigation-case-observation-batch-r1" {
		t.Fatalf("scaffold must carry the ingested Case identity: %#v", scaffold)
	}
	if findings, ok := scaffold["source_finding_ids"].([]any); !ok || len(findings) != 1 || findings[0] != "finding-1" {
		t.Fatalf("scaffold must carry the exact sealed Finding set: %#v", scaffold["source_finding_ids"])
	}
	route, ok := scaffold["route_request_draft"].(map[string]any)
	if !ok || route["case_id"] != "investigation-case-observation-batch-r1" {
		t.Fatalf("scaffold must embed the RouteRequest draft: %#v", scaffold["route_request_draft"])
	}
	for _, field := range []string{"route", "route_reason", "primary_root_cause"} {
		if value, _ := route[field].(string); !strings.HasPrefix(value, "TODO(") {
			t.Fatalf("RouteRequest draft %s must be a TODO placeholder, got %q", field, value)
		}
	}
	if !strings.Contains(route["next_verb"].(string), "runtime investigation route") {
		t.Fatalf("RouteRequest draft must project the route verb: %v", route["next_verb"])
	}
	contract, ok := scaffold["repair_contract_placeholder"].(map[string]any)
	if !ok || contract["status"] != "draft" {
		t.Fatalf("scaffold must embed the RepairContract placeholder in draft status: %#v", scaffold["repair_contract_placeholder"])
	}
	if units, ok := contract["repair_units"].([]any); !ok || len(units) != 1 {
		t.Fatalf("RepairContract placeholder must carry the unit skeleton: %#v", contract["repair_units"])
	}
	if !strings.Contains(contract["next_verb"].(string), "runtime investigation contract approve") {
		t.Fatalf("RepairContract placeholder must project the approve verb: %v", contract["next_verb"])
	}
	if disclosure, _ := scaffold["disclosure"].(string); !strings.Contains(disclosure, "dry-run") {
		t.Fatalf("scaffold must disclose its dry-run nature: %v", scaffold["disclosure"])
	}
}

// TestS8IntakeScaffoldStdout proves the `-` path renders the scaffold to
// stdout instead of writing a file, and still commits the Case.
func TestS8IntakeScaffoldStdout(t *testing.T) {
	root, _ := seedS8ScaffoldBatch(t)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "investigation", "ingest", "--root", root,
		"--grouping-rationale", "the sealed batch is the provisional grouping boundary",
		"--emit-template", "-",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ingest --emit-template - code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var scaffold map[string]any
	// The stdout stream carries the scaffold first, then the normal ingest
	// result JSON; the scaffold is the first top-level JSON object.
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&scaffold); err != nil {
		t.Fatalf("stdout scaffold is not JSON: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if scaffold["template"] != "case-template" {
		t.Fatalf("stdout scaffold must be the case-template: %#v", scaffold)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "review", "investigation", "case-template.json")); !os.IsNotExist(err) {
		t.Fatalf("stdout mode must not write the scaffold file (stat err=%v)", err)
	}
}

// TestS8IntakeWithoutTemplateFlag keeps the additive-flag contract: without
// --emit-template the verb behaves exactly as before (no scaffold file).
func TestS8IntakeWithoutTemplateFlag(t *testing.T) {
	root, _ := seedS8ScaffoldBatch(t)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "investigation", "ingest", "--root", root,
		"--grouping-rationale", "the sealed batch is the provisional grouping boundary",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("plain ingest code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "review", "investigation", "case-template.json")); !os.IsNotExist(err) {
		t.Fatalf("plain ingest must not write a scaffold file (stat err=%v)", err)
	}
}

// seedS8ScaffoldBatch builds the same ingesti-ready runtime the pointer
// test uses: a sealed observation-batch pointer, one finding artifact and
// the Investigator definition.
func seedS8ScaffoldBatch(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 0)
	batch := map[string]any{
		"schema_version": "1.0.0", "observation_batch_id": "observation-batch-r1", "conclusion": "sealed",
		"evidence_id": "observation-batch-r1", "kind": "observation_batch", "runtime_id": req039fixtures.RuntimeIDFromState(state),
		"producer_agent_id": "round-consumer", "producer_responsibility": "Orchestrator", "review_plan_id": "review-plan-r1",
		"review_round": 1, "baseline_generation": 1, "subject_digest": strings.Repeat("a", 64),
		"finding_ids": []string{"finding-1"}, "drain_policy": "complete_required_claims",
		"claim_coverage_summary": map[string]any{
			"total_required": 1, "pass": 0, "finding": 1, "not_applicable": 0, "blocked": 0,
			"blocked_claims": []any{}, "plan_revision": 1,
		},
		"unobserved_claim_ids": []any{}, "original_finder_routes": []any{},
		"investigation_readiness": []any{map[string]any{"finding_id": "finding-1", "status": "ready"}},
		"sealed_at":               "2026-08-25T00:00:00Z", "sealed_by": "round-consumer", "revision": 1,
	}
	data, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	batchRel := ".claude/evidence/observation-batch-r1.json"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, batchRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, batchRel), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "investigator.md"), []byte("# Investigator\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	review := state["review"].(map[string]any)
	review["observation_batch"] = map[string]any{
		"batch_id": "observation-batch-r1", "path": batchRel, "sha256": sha256HexForCLI(data),
		"finding_ids": []any{"finding-1"}, "drain_policy": "complete_required_claims", "sealed_at": "2026-08-25T00:00:00Z",
	}
	seedCLIFinding(t, root, state)
	req039fixtures.WriteState(t, root, state)
	return root, state
}
