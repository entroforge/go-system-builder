package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRuntimeInvestigationRequiresAnAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "investigation"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runtime investigation without an action code=%d, want 2; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "runtime investigation requires <ingest|status|hypothesis|route|contract>") {
		t.Fatalf("missing actionable investigation usage: %s", stderr.String())
	}
}

func TestRuntimeInvestigationIngestCLICommitsCasePointer(t *testing.T) {
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

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "investigation", "ingest", "--root", root,
		"--grouping-rationale", "the sealed batch is the provisional grouping boundary",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("investigation ingest code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "investigation-case-observation-batch-r1") {
		t.Fatalf("ingest output does not expose the Case identity: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"runtime", "investigation", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "investigating") {
		t.Fatalf("investigation status code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var status map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	board, ok := status["board"].(map[string]any)
	if !ok || board["unexplained_finding_ids"] == nil || !strings.Contains(status["next"].(string), "hypothesis") {
		t.Fatalf("status must expose the investigation board and next action: %#v", status)
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"runtime", "investigation", "status", "--all", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("investigation status --all code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var aggregate map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &aggregate); err != nil {
		t.Fatal(err)
	}
	if cases, ok := aggregate["cases"].([]any); !ok || len(cases) == 0 {
		t.Fatalf("status --all must expose the Case aggregate: %#v", aggregate)
	} else {
		// S8-10: the aggregate has no Runtime state, so it must never guess a
		// dispatch verdict. Undischarged hypotheses must land in
		// dispatch_unknown rather than pending or awaiting_result. This
		// ingest-only root has no hypotheses, so the buckets must all be
		// empty rather than misclassified.
		board := cases[0].(map[string]any)["hypothesis_summary"].(map[string]any)
		for _, key := range []string{"pending", "awaiting_result", "dispatch_unknown"} {
			if bucket := board[key].([]any); len(bucket) != 0 {
				t.Fatalf("aggregate without hypotheses must keep %s empty, got %v", key, bucket)
			}
		}
	}

	// A read-only status view must fail closed when the pinned Case file drifts;
	// otherwise an Investigator can plan against bytes different from the
	// Runtime authority.
	casePointer, ok := status["case"].(map[string]any)
	if !ok {
		t.Fatalf("status must expose the pinned Case pointer: %#v", status)
	}
	casePath, _ := casePointer["path"].(string)
	caseBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(casePath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(casePath)), append(caseBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"runtime", "investigation", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "sha256 drifted") {
		t.Fatalf("status must reject a drifted pinned Case: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeInvestigationContractApproveCLIHandsOffToS9(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 0)
	batch := map[string]any{
		"schema_version": "1.0.0", "observation_batch_id": "observation-batch-r1", "conclusion": "sealed",
		"evidence_id": "observation-batch-r1", "kind": "observation_batch", "runtime_id": req039fixtures.RuntimeIDFromState(state),
		"producer_agent_id": "round-consumer", "producer_responsibility": "Orchestrator", "review_plan_id": "review-plan-r1",
		"review_round": 1, "baseline_generation": 1, "subject_digest": strings.Repeat("a", 64),
		"finding_ids": []string{"finding-1"}, "drain_policy": "complete_required_claims",
		"claim_coverage_summary": map[string]any{"total_required": 1, "pass": 0, "finding": 1, "not_applicable": 0, "blocked": 0, "blocked_claims": []any{}, "plan_revision": 1},
		"unobserved_claim_ids":   []any{}, "original_finder_routes": []any{}, "investigation_readiness": []any{map[string]any{"finding_id": "finding-1", "status": "ready"}},
		"sealed_at": "2026-08-25T00:00:00Z", "sealed_by": "round-consumer", "revision": 1,
	}
	batchBytes, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	batchBytes = append(batchBytes, '\n')
	batchRel := ".claude/evidence/observation-batch-r1.json"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, batchRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, batchRel), batchBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["review"].(map[string]any)["observation_batch"] = map[string]any{
		"batch_id": "observation-batch-r1", "path": batchRel, "sha256": sha256HexForCLI(batchBytes), "finding_ids": []any{"finding-1"},
		"drain_policy": "complete_required_claims", "sealed_at": "2026-08-25T00:00:00Z",
	}
	seedCLIFinding(t, root, state)
	req039fixtures.WriteState(t, root, state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "investigation", "ingest", "--root", root, "--grouping-rationale", "one sealed batch is one provisional case"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("investigation ingest code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	prepareCLIInvestigationCase(t, root)
	contractRel := ".claude/review/investigation/repair-contract-draft.json"
	contract := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-r1", "case_id": "investigation-case-observation-batch-r1", "revision": 1, "status": "draft", "source_finding_ids": []string{"finding-1"},
		"root_cause_statement": "the payload boundary has two incompatible owners", "violated_invariant": "one owner defines the payload shape", "causal_model_ref": "case://investigation-case-observation-batch-r1/causal-model", "architecture_intent": "restore the authoritative boundary",
		"repair_units": []any{map[string]any{"id": "unit-1", "description": "centralize the payload contract"}}, "prospective_scope": []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions": []string{"finding-1 is eliminated"}, "root_invariant_assertions": []string{"one payload owner exists"}, "detection_gap_assertions": []string{"contract drift is tested"}, "stop_escalation_conditions": []string{"locked requirement changes"},
	}
	contractBytes, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(root, filepath.FromSlash(contractRel))
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, append(contractBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	approvalHash := sha256HexForCLI(append(append([]byte(nil), contractBytes...), '\n'))
	decision := map[string]any{
		"decision": "approve_contract", "decision_id": "ev-contract-approval",
		"runtime_id": req039fixtures.RuntimeIDFromState(state),
		"case_id":    "investigation-case-observation-batch-r1", "contract_id": "repair-contract-r1",
		"approved_by": "main-session", "approval_hash": approvalHash,
	}
	decisionBytes, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	decisionRel := ".claude/decisions/contract-approval.json"
	decisionPath := filepath.Join(root, filepath.FromSlash(decisionRel))
	if err := os.MkdirAll(filepath.Dir(decisionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionPath, append(decisionBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	approvalEvidenceID := "ev-contract-approval"
	approvalExpectedRevision := readStateRevision(t, root)
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{
		"runtime", "evidence", "add", "--root", root,
		"--expected-revision", fmt.Sprint(approvalExpectedRevision), "--id", approvalEvidenceID,
		"--kind", "human_decision", "--path", decisionRel, "--produced-by", "main-session",
		"--scope-ref", fmt.Sprintf("s8_contract_approval:%s@%d", req039fixtures.RuntimeIDFromState(state), approvalExpectedRevision+1),
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("contract approval evidence code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"runtime", "investigation", "contract", "approve", "--root", root, "--case-id", "investigation-case-observation-batch-r1", "--file", contractPath, "--approved-by", "main-session", "--approval-hash", approvalHash, "--approval-evidence-id", approvalEvidenceID}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("contract approve code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "contract_approved") || !strings.Contains(stderr.String(), "S9 consume") {
		t.Fatalf("approval output lacks S9 handoff: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	var approvedState map[string]any
	stateBytes, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stateBytes, &approvedState); err != nil {
		t.Fatal(err)
	}
	if approvedState["lifecycle"].(map[string]any)["phase"] != "repair_readback" {
		t.Fatalf("lifecycle phase = %#v, want repair_readback", approvedState["lifecycle"])
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"runtime", "investigation", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status after Contract approval code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var approvedStatus map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &approvedStatus); err != nil {
		t.Fatal(err)
	}
	next, _ := approvedStatus["next"].(string)
	if !strings.Contains(next, "runtime repair session open") || !strings.Contains(next, "repair_contract_ref") {
		t.Fatalf("approved Case status must expose the executable S9 session action, got %q", next)
	}

	// A targeted S9 failure returns the lifecycle cursor to S8 while the
	// approved Case artifact remains the immutable historical revision. The
	// S8 status entry must surface the repair recovery action instead of
	// sending the Investigator back into S9 with the superseded Contract.
	stateBytes, err = os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recoveryState map[string]any
	if err := json.Unmarshal(stateBytes, &recoveryState); err != nil {
		t.Fatal(err)
	}
	recoveryState["lifecycle"].(map[string]any)["phase"] = "investigation"
	recoveryState["review"].(map[string]any)["repair"] = map[string]any{
		"session_id": "repair-session-r1", "case_id": "investigation-case-observation-batch-r1", "contract_id": "repair-contract-r1",
		"contract_ref": ".claude/review/investigation/contracts/repair-contract-r1-r2.json", "contract_sha256": strings.Repeat("b", 64),
		"path": ".claude/review/repair/sessions/repair-session-r1.json", "sha256": strings.Repeat("c", 64), "revision": 1,
		"status": "blocked", "targeted_reverification_refs": []string{".claude/review/repair/reverification/failure.json"},
		"targeted_reverification_artifacts": []any{}, "failure_route": "fail_same_cause", "updated_at": "2026-08-25T00:00:00Z",
		"next_action": "re-open Case investigation-case-observation-batch-r1 in S8 with `runtime investigation route --route investigate_more --reassessment-evidence failure.json`",
	}
	recoveryBytes, err := json.MarshalIndent(recoveryState, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), append(recoveryBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"runtime", "investigation", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status after targeted failure code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var recoveryStatus map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &recoveryStatus); err != nil {
		t.Fatal(err)
	}
	recoveryNext, _ := recoveryStatus["next"].(string)
	if !strings.Contains(recoveryNext, "runtime investigation route") || !strings.Contains(recoveryNext, "--reassessment-evidence") || strings.Contains(recoveryNext, "runtime repair session open") {
		t.Fatalf("S8 status must surface targeted-failure recovery, got %q", recoveryNext)
	}
	if recovery, ok := recoveryStatus["repair_recovery"].(map[string]any); !ok || recovery["status"] != "blocked" {
		t.Fatalf("S8 status must expose the blocked S9 recovery projection: %#v", recoveryStatus["repair_recovery"])
	}
}

func sha256HexForCLI(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func seedCLIFinding(t *testing.T, root string, state map[string]any) {
	t.Helper()
	findingBody := map[string]any{
		"schema_version": "1.0.0", "finding_id": "finding-1", "claim_id": "claim-intake-1", "lens": "qa", "severity": "P1",
		"expected": "the expected behavior holds", "authority_refs": []string{"REQ-039"}, "observed": "the observed behavior deviates", "observation_mode": "code_inspection",
		"encounter":       map[string]any{"journey_summary": "inspect -> trace -> observe deviation", "inspection_entry": "internal/example", "symbol_trail": "entry -> boundary", "wall_action": "inspect the boundary", "first_bad_checkpoint": "invariant is false", "terminal_state": "review stopped"},
		"reproducibility": "always", "evidence_refs": []string{"evidence://finding-1"},
	}
	data, err := json.MarshalIndent(findingBody, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	rel := ".claude/evidence/findings/finding-1.json"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["findings"] = []any{map[string]any{
		"finding_id": "finding-1", "path": rel, "sha256": sha256HexForCLI(data), "claim_id": "claim-intake-1", "assignment_id": "assignment-intake", "lens": "qa", "severity": "P1", "observation_mode": "code_inspection", "original_finder": "agent-intake", "review_round": 1, "created_at": "2026-08-25T00:00:00Z",
	}}
}

func prepareCLIInvestigationCase(t *testing.T, root string) {
	t.Helper()
	statePath := filepath.Join(root, ".claude/loop-state.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	pointer := state["review"].(map[string]any)["investigation"].(map[string]any)
	casePath := filepath.Join(root, filepath.FromSlash(pointer["path"].(string)))
	caseBytes, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatal(err)
	}
	var caseDocument map[string]any
	if err := json.Unmarshal(caseBytes, &caseDocument); err != nil {
		t.Fatal(err)
	}
	caseDocument["unexplained_finding_ids"] = []any{}
	caseDocument["causal_model"] = map[string]any{"trigger": "payload crosses boundary", "violated_invariant": "one owner", "faulty_mechanism": "duplicate schema", "propagation": "decoder rejects fields", "symptoms": []any{"finding-1"}}
	caseDocument["primary_root_cause"] = "the payload contract has two incompatible owners"
	caseDocument["blast_radius"] = map[string]any{"paths": []any{"internal/api"}}
	caseDocument["detection_gap"] = map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://contract-drift"}}
	caseDocument["no_competing_hypothesis"] = "the single boundary hypothesis explains the finding; no alternative mechanism was credible"
	caseDocument["route"] = "s9_repair"
	caseDocument["route_reason"] = "implementation boundary must be repaired"
	updatedCase, err := json.MarshalIndent(caseDocument, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updatedCase = append(updatedCase, '\n')
	if err := os.WriteFile(casePath, updatedCase, 0o644); err != nil {
		t.Fatal(err)
	}
	pointer["sha256"] = sha256HexForCLI(updatedCase)
	updatedState, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(updatedState, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRuntimeInvestigationHypothesisAndRouteCLI drives the S8 investigation
// workflow verbs the round-8 review wired into the CLI (register → result →
// route). Without them an ingested Case can never record a hypothesis, so
// unexplained_finding_ids never empties and contract approve rejects forever.
func TestRuntimeInvestigationHypothesisAndRouteCLI(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 0)
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "investigator.md"), []byte("# Investigator\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "agent-protocol.md"), []byte("# Protocol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	review := state["review"].(map[string]any)
	review["observation_batch"] = map[string]any{
		"batch_id": "observation-batch-r1", "path": batchRel, "sha256": sha256HexForCLI(data),
		"finding_ids": []any{"finding-1"}, "drain_policy": "complete_required_claims", "sealed_at": "2026-08-25T00:00:00Z",
	}
	// RC-14 attestation: the hypothesis/register + result verbs validate every
	// non-anchor --evidence against the runtime evidence index, so the sealed
	// batch must also be a current-generation index entry, not just a pointer.
	state["evidence"] = append(state["evidence"].([]any), map[string]any{
		"id": "observation-batch-r1", "kind": "observation_batch", "path": batchRel, "sha256": sha256HexForCLI(data),
		"status": "valid", "baseline_generation": 1, "review_round": 1,
		"produced_by": []any{"round-consumer"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": "Orchestrator", "scope_refs": []any{},
	})
	seedCLIFinding(t, root, state)
	req039fixtures.WriteState(t, root, state)

	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}

	if out, _, code := run("runtime", "investigation", "ingest", "--root", root,
		"--grouping-rationale", "the sealed batch is the provisional grouping boundary"); code != 0 {
		t.Fatalf("ingest code=%d out=%s", code, out)
	}

	// Read the Case CAS coordinates the way an agent would.
	out, _, code := run("runtime", "investigation", "status", "--root", root)
	if code != 0 {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	var status struct {
		Case struct {
			Revision int    `json:"revision"`
			SHA256   string `json:"sha256"`
		} `json:"case"`
		Next string `json:"next"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Next, "runtime investigation hypothesis register") ||
		!strings.Contains(status.Next, "--case-id") ||
		!strings.Contains(status.Next, "--assignment-id") {
		t.Fatalf("initial investigation status must expose an executable hypothesis-register command, got %q", status.Next)
	}
	out, errOut, code := run("runtime", "investigation", "hypothesis", "register", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1",
		"--expected-case-revision", fmtInt(status.Case.Revision), "--expected-case-sha256", status.Case.SHA256,
		"--id", "hyp-1", "--statement", "the boundary discards the store error",
		"--assignment-id", "assignment-s8-hyp-1",
		"--invariant", "every store error reaches the caller",
		"--discriminator", "force the store failure and observe the boundary",
		"--support", "boundary returns nil", "--refute", "boundary propagates",
		"--source-finding", "finding-1", "--evidence", "observation-batch-r1")
	if code != 0 {
		t.Fatalf("hypothesis register code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if out, errOut, code := run("runtime", "investigation", "dispatch", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1", "--hypothesis-id", "hyp-1", "--agent-id", "agent-investigator-1", "--assignment-id", "assignment-s8-other"); code == 0 || !strings.Contains(errOut, "does not match") {
		t.Fatalf("investigator dispatch must reject an Assignment override that drifts from the Hypothesis binding: code=%d stderr=%s stdout=%s", code, errOut, out)
	}

	if out, errOut, code := run("runtime", "investigation", "dispatch", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1", "--hypothesis-id", "hyp-1", "--agent-id", "agent-investigator-1"); code != 0 {
		t.Fatalf("investigator dispatch code=%d stderr=%s stdout=%s", code, errOut, out)
	} else if !strings.Contains(out, "workgroup-s8-") || !strings.Contains(out, "runtime investigation hypothesis result") || !strings.Contains(out, "--assignment-id") || !strings.Contains(errOut, "PLAN_REPORT") {
		t.Fatalf("dispatch must expose the registered workgroup and plan checkpoint: stdout=%s stderr=%s", out, errOut)
	}

	// Refresh the Case coordinates after the revision bump.
	out, _, code = run("runtime", "investigation", "status", "--root", root)
	if code != 0 {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Next, "runtime investigation hypothesis result") ||
		!strings.Contains(status.Next, "--case-id") ||
		!strings.Contains(status.Next, "--hypothesis-id") ||
		!strings.Contains(status.Next, "--assignment-id") {
		t.Fatalf("investigation status must expose an executable hypothesis-result command after dispatch, got %q", status.Next)
	}

	out, errOut, code = run("runtime", "investigation", "hypothesis", "result", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1",
		"--expected-case-revision", fmtInt(status.Case.Revision), "--expected-case-sha256", status.Case.SHA256,
		"--hypothesis-id", "hyp-1", "--assignment-id", "assignment-s8-hyp-1", "--method", "forced failure", "--observed", "nil under store error",
		"--counterfactual", "a propagating boundary would surface the typed error",
		"--evidence", "observation-batch-r1", "--evidence", "evidence://boundary-trace",
		"--result", "supported", "--explains", "finding-1", "--source-boundary", "service.go:87", "--source-boundary", "decoder.go:12")
	if code != 0 {
		t.Fatalf("hypothesis result code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	// The CLI advertises these fields as repeatable. Verify that repeated
	// occurrences survive the flag parser instead of silently keeping only the
	// last value (comma-separated input remains supported separately).
	caseStateBytes, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var caseState map[string]any
	if err := json.Unmarshal(caseStateBytes, &caseState); err != nil {
		t.Fatal(err)
	}
	casePointer := caseState["review"].(map[string]any)["investigation"].(map[string]any)
	caseBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(casePointer["path"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	var caseDocument map[string]any
	if err := json.Unmarshal(caseBytes, &caseDocument); err != nil {
		t.Fatal(err)
	}
	results, ok := caseDocument["hypothesis_results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected one hypothesis result, got %#v", caseDocument["hypothesis_results"])
	}
	resultDocument := results[0].(map[string]any)
	if got := resultDocument["evidence_refs"].([]any); len(got) != 2 || got[0] != "observation-batch-r1" || got[1] != "evidence://boundary-trace" {
		t.Fatalf("repeated --evidence values were not preserved: %#v", got)
	}
	if got := resultDocument["source_boundary_refs"].([]any); len(got) != 2 || got[0] != "service.go:87" || got[1] != "decoder.go:12" {
		t.Fatalf("repeated --source-boundary values were not preserved: %#v", got)
	}

	out, _, code = run("runtime", "investigation", "status", "--root", root)
	if code != 0 {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}

	causal := map[string]any{"chain": []string{"store error", "discarded", "nil to caller"}}
	causalBytes, _ := json.Marshal(causal)
	causalPath := filepath.Join(root, "causal.json")
	if err := os.WriteFile(causalPath, causalBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	blast := map[string]any{"paths": []string{"internal/example/service.go"}}
	blastBytes, _ := json.Marshal(blast)
	blastPath := filepath.Join(root, "blast.json")
	if err := os.WriteFile(blastPath, blastBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	gap := map[string]any{"gap_type": "test", "evidence_refs": []string{"observation-batch-r1"}}
	gapBytes, _ := json.Marshal(gap)
	gapPath := filepath.Join(root, "gap.json")
	if err := os.WriteFile(gapPath, gapBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Missing authoring inputs must identify the file and recovery action; a
	// generic "causal_model is required" message forces the agent to debug the
	// CLI implementation instead of fixing its command input.
	out, errOut, code = run("runtime", "investigation", "route", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1",
		"--expected-case-revision", fmtInt(status.Case.Revision), "--expected-case-sha256", status.Case.SHA256,
		"--route", "s9_repair", "--reason", "single explained root cause",
		"--primary-root-cause", "the boundary discards the store error",
		"--causal-model-file", "missing-causal.json", "--blast-radius-file", "blast.json", "--detection-gap-file", "gap.json")
	if code == 0 || !strings.Contains(errOut, "missing-causal.json") || !strings.Contains(errOut, "create the JSON file") {
		t.Fatalf("missing route input must identify the file and recovery action: code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	nullCausalPath := filepath.Join(root, "null-causal.json")
	if err := os.WriteFile(nullCausalPath, []byte("null\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code = run("runtime", "investigation", "route", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1",
		"--expected-case-revision", fmtInt(status.Case.Revision), "--expected-case-sha256", status.Case.SHA256,
		"--route", "s9_repair", "--reason", "single explained root cause",
		"--primary-root-cause", "the boundary discards the store error",
		"--causal-model-file", "null-causal.json", "--blast-radius-file", "blast.json", "--detection-gap-file", "gap.json")
	if code == 0 || !strings.Contains(errOut, "null-causal.json") || !strings.Contains(errOut, "JSON object") {
		t.Fatalf("null route input must be rejected as a non-object: code=%d stderr=%s stdout=%s", code, errOut, out)
	}

	out, errOut, code = run("runtime", "investigation", "route", "--root", root,
		"--case-id", "investigation-case-observation-batch-r1",
		"--expected-case-revision", fmtInt(status.Case.Revision), "--expected-case-sha256", status.Case.SHA256,
		"--route", "s9_repair", "--reason", "single explained root cause",
		"--primary-root-cause", "the boundary discards the store error",
		"--causal-model-file", "causal.json", "--blast-radius-file", "blast.json", "--detection-gap-file", "gap.json",
		"--no-competing-hypothesis", "the single boundary hypothesis explains the finding; no alternative mechanism was credible")
	if code != 0 {
		t.Fatalf("route code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"route": "s9_repair"`) && !strings.Contains(out, `"route":"s9_repair"`) {
		t.Fatalf("route output must expose the s9_repair disposition: %s", out)
	}
	if !strings.Contains(errOut, "draft the RepairContract") ||
		!strings.Contains(errOut, "runtime investigation contract approve --case-id investigation-case-observation-batch-r1 --file <draft> --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id>") {
		t.Fatalf("route coaching must point at the RepairContract step: %s", errOut)
	}
}

func fmtInt(value int) string {
	return fmt.Sprint(value)
}
