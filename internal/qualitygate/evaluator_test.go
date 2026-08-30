package qualitygate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

type memoryFiles map[string][]byte

func (m memoryFiles) ReadFile(path string) ([]byte, error) {
	return append([]byte(nil), m[path]...), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestEngineImplementsEvaluatorContract(t *testing.T) {
	var evaluator qualitygate.Evaluator = qualitygate.NewEvaluator(nil)
	if evaluator == nil {
		t.Fatal("evaluator contract returned nil")
	}
}

func TestCompletionReportMatchesSchema(t *testing.T) {
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatalf("read agent-message examples: %v", err)
	}
	var examples []json.RawMessage
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatalf("decode examples: %v", err)
	}
	var completion json.RawMessage
	for _, raw := range examples {
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("decode example envelope: %v", err)
		}
		if envelope["message_type"] == "completion_report" {
			completion = raw
			break
		}
	}
	if len(completion) == 0 {
		t.Fatal("agent-message.examples.json missing completion_report")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", completion); err != nil {
		t.Fatalf("validate completion report: %v", err)
	}
}

func TestEvaluatorReturnsUnknownForUnregisteredGate(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	result, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Snapshot: runtime.Snapshot{Revision: 17, State: map[string]any{}},
		GateID:   "GATE-NOT-REGISTERED",
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown {
		t.Fatalf("status = %q, want unknown", result.Status)
	}
	if result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("error code = %q, want %s", result.ErrorCode, qualitygate.ErrorGateUnknown)
	}
	if result.ObservedRevision != 17 {
		t.Fatalf("observed revision = %d, want 17", result.ObservedRevision)
	}
	if result.TransitionCommitted {
		t.Fatal("evaluator must never report a committed transition")
	}
}

func TestPlanningGateNeedsQualifiedSemanticEvidence(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := planningDesignInput(t, false)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("status = %q, want not_ready", result.Status)
	}
	if result.CandidateTransition != "PTR-PLAN-01" {
		t.Fatalf("candidate = %q, want PTR-PLAN-01", result.CandidateTransition)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "evidence:planning_design_record" {
		t.Fatalf("missing = %#v", result.Missing)
	}
}

func TestPlanningGateAcceptsQualifiedSemanticEvidence(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := planningDesignInput(t, true)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("status = %q, want satisfied (error=%s conflicts=%v missing=%v)", result.Status, result.ErrorCode, result.Conflicts, result.Missing)
	}
	if len(result.EvidenceRefs) != 1 || result.EvidenceRefs[0] != "ev-design" {
		t.Fatalf("evidence refs = %#v", result.EvidenceRefs)
	}
	if len(result.Fingerprint) != len("sha256:")+64 || result.Fingerprint[:7] != "sha256:" {
		t.Fatalf("fingerprint = %q", result.Fingerprint)
	}
}

func TestEvaluatorRequiresCurrentReviewRound(t *testing.T) {
	evaluator := newTestEvaluator(t)

	stale, err := evaluator.Evaluate(context.Background(), reviewGateInput(t, 2))
	if err != nil {
		t.Fatalf("Evaluate stale round: %v", err)
	}
	if stale.Status != qualitygate.StatusNotReady {
		t.Fatalf("stale status = %q, want not_ready", stale.Status)
	}
	if len(stale.EvidenceRefs) != 0 {
		t.Fatalf("stale evidence refs = %#v, want none", stale.EvidenceRefs)
	}

	current, err := evaluator.Evaluate(context.Background(), reviewGateInput(t, 3))
	if err != nil {
		t.Fatalf("Evaluate current round: %v", err)
	}
	if current.Status != qualitygate.StatusSatisfied {
		t.Fatalf("current status = %q, want satisfied (error=%s conflicts=%v missing=%v)", current.Status, current.ErrorCode, current.Conflicts, current.Missing)
	}
	if len(current.EvidenceRefs) != 1 || current.EvidenceRefs[0] != "ev-qa" {
		t.Fatalf("current evidence refs = %#v", current.EvidenceRefs)
	}
}

func TestEvaluatorReturnsUnknownForConflictingRequestedEvents(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := conflictingRequestedEventsInput(t)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown {
		t.Fatalf("status = %q, want unknown", result.Status)
	}
	if result.ErrorCode != qualitygate.ErrorTriggerConflict {
		t.Fatalf("error code = %q, want %s", result.ErrorCode, qualitygate.ErrorTriggerConflict)
	}
	want := []string{"targeted_reverification_fail", "targeted_reverification_pass"}
	if len(result.Conflicts) != len(want) || result.Conflicts[0] != want[0] || result.Conflicts[1] != want[1] {
		t.Fatalf("conflicts = %#v, want %#v", result.Conflicts, want)
	}
	if result.TransitionCommitted {
		t.Fatal("conflicting requested events must not commit a transition")
	}
}

func TestDocumentPassRequiresIndependentNonAuthorProducers(t *testing.T) {
	evaluator := newTestEvaluator(t)
	tests := []struct {
		name        string
		specAgent   string
		taskAgent   string
		author      string
		wantMissing string
	}{
		{
			name:      "same agent with two responsibility labels",
			specAgent: "reviewer-1", taskAgent: "reviewer-1",
			wantMissing: "evidence:independent_document_reviewers",
		},
		{
			name:      "candidate author acts as reviewer",
			specAgent: "author-1", taskAgent: "reviewer-2", author: "author-1",
			wantMissing: "evidence:reviewer_not_candidate_author",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(context.Background(), documentPassInput(t, test.specAgent, test.taskAgent, test.author))
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if result.Status != qualitygate.StatusNotReady {
				t.Fatalf("status = %q, want not_ready", result.Status)
			}
			if !contains(result.Missing, test.wantMissing) {
				t.Fatalf("missing = %#v, want %q", result.Missing, test.wantMissing)
			}
		})
	}
}

func TestDocumentPassRequiresExactManifest(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := documentPassInput(t, "reviewer-1", "reviewer-2", "")
	files := input.Files.(memoryFiles)
	data := files["evidence/ev-spec.json"]
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	subjects := envelope["subject_refs"].([]any)
	envelope["subject_refs"] = subjects[:len(subjects)-1]
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	files["evidence/ev-spec.json"] = data
	for _, raw := range input.Snapshot.State["evidence"].([]any) {
		index := raw.(map[string]any)
		if index["id"] == "ev-spec" {
			index["sha256"] = sha256Hex(data)
		}
	}

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate incomplete manifest: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady || !contains(result.Missing, "evidence:exact_document_manifest") {
		t.Fatalf("incomplete manifest result = status %q missing %#v", result.Status, result.Missing)
	}

	complete, err := evaluator.Evaluate(context.Background(), documentPassInput(t, "reviewer-1", "reviewer-2", ""))
	if err != nil {
		t.Fatalf("Evaluate complete manifest: %v", err)
	}
	if complete.Status != qualitygate.StatusSatisfied {
		t.Fatalf("complete manifest status = %q, want satisfied (missing=%v conflicts=%v)", complete.Status, complete.Missing, complete.Conflicts)
	}
}

func TestPlanningGatesRequireCurrentArtifacts(t *testing.T) {
	evaluator := newTestEvaluator(t)
	tests := []struct {
		name           string
		gateID         string
		transitionID   string
		phase          string
		evidenceKind   string
		responsibility string
		wantMissing    string
	}{
		{
			name: "contracts", gateID: "GATE-PLANNING-CONTRACTS-COMPLETE",
			transitionID: "PTR-PLAN-02", phase: "contracts",
			evidenceKind: "planning_contract_record", responsibility: "Contract Planner",
			wantMissing: "document:contract:locked",
		},
		{
			name: "tasks", gateID: "GATE-PLANNING-TASKS-COMPLETE",
			transitionID: "TR-002", phase: "tasks",
			evidenceKind: "planning_task_record", responsibility: "Task Planner",
			wantMissing: "document:task:complete",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := planningInputForGate(t, test.gateID, test.transitionID, test.phase, test.evidenceKind, test.responsibility)
			result, err := evaluator.Evaluate(context.Background(), input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if result.Status != qualitygate.StatusNotReady || !contains(result.Missing, test.wantMissing) {
				t.Fatalf("result = status %q missing %#v, want %q", result.Status, result.Missing, test.wantMissing)
			}
		})
	}
}

func TestBuilderBatchRequiresEveryActivatedTaskCompletion(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := builderBatchInput(t)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("status = %q, want not_ready", result.Status)
	}
	if !contains(result.Missing, "evidence:completion_report:TASK-TEST-02") {
		t.Fatalf("missing = %#v", result.Missing)
	}
}

func TestEvaluatorReturnsUnknownForUnauthorizedProducer(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := reviewGateInput(t, 3)
	files := input.Files.(memoryFiles)
	data := files["evidence/qa.json"]
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	envelope["producer_responsibility"] = "Builder"
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	files["evidence/qa.json"] = data
	index := input.Snapshot.State["evidence"].([]any)[0].(map[string]any)
	index["responsibility_id"] = "Builder"
	index["sha256"] = sha256Hex(data)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("result = status %q code %q, want unknown/%s", result.Status, result.ErrorCode, qualitygate.ErrorGateUnknown)
	}
	if !contains(result.Conflicts, "evidence:ev-qa:producer") {
		t.Fatalf("conflicts = %#v", result.Conflicts)
	}
}

func TestEvaluatorRejectsGateTransitionOrCursorMismatch(t *testing.T) {
	evaluator := newTestEvaluator(t)

	wrongTransition := planningDesignInput(t, true)
	wrongTransition.TransitionID = "PTR-PLAN-02"
	result, err := evaluator.Evaluate(context.Background(), wrongTransition)
	if err != nil {
		t.Fatalf("Evaluate transition mismatch: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || !contains(result.Conflicts, "transition:PTR-PLAN-02:gate_mismatch") {
		t.Fatalf("transition mismatch result = status %q conflicts %#v", result.Status, result.Conflicts)
	}

	wrongCursor := planningDesignInput(t, true)
	wrongCursor.Snapshot.State["lifecycle"].(map[string]any)["phase"] = "contracts"
	result, err = evaluator.Evaluate(context.Background(), wrongCursor)
	if err != nil {
		t.Fatalf("Evaluate cursor mismatch: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || !contains(result.Conflicts, "cursor:planning.contracts:gate_mismatch") {
		t.Fatalf("cursor mismatch result = status %q conflicts %#v", result.Status, result.Conflicts)
	}
}

func TestEvaluatorHonorsCanceledContext(t *testing.T) {
	evaluator := newTestEvaluator(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := evaluator.Evaluate(ctx, planningDesignInput(t, true))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("result = status %q code %q", result.Status, result.ErrorCode)
	}
	if !contains(result.Conflicts, "evaluation:context_canceled") {
		t.Fatalf("conflicts = %#v", result.Conflicts)
	}
}

func TestEvaluationJSONUsesContractFieldNames(t *testing.T) {
	evaluator := newTestEvaluator(t)
	result, err := evaluator.Evaluate(context.Background(), qualitygate.Input{
		Snapshot: runtime.Snapshot{Revision: 7, State: map[string]any{}},
		GateID:   "GATE-NOT-REGISTERED",
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var packet map[string]any
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"status", "gate_id", "candidate_transition", "observed_revision", "fingerprint",
		"missing", "evidence_refs", "conflicts", "error_code", "transition_committed", "next_cursor",
	} {
		if _, ok := packet[key]; !ok {
			t.Errorf("contract field %q is missing from %s", key, data)
		}
	}
	if _, leaked := packet["Status"]; leaked {
		t.Fatalf("Go field name leaked into contract JSON: %s", data)
	}
	if packet["missing"] == nil || packet["evidence_refs"] == nil || packet["conflicts"] == nil {
		t.Fatalf("contract arrays must be non-null: %s", data)
	}
}

func TestFingerprintIgnoresRevisionAndTimestamp(t *testing.T) {
	evaluator := newTestEvaluator(t)
	firstInput := reviewGateInput(t, 3)
	secondInput := reviewGateInput(t, 3)
	secondInput.Snapshot.Revision = 999
	files := secondInput.Files.(memoryFiles)
	data := files["evidence/qa.json"]
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	envelope["created_at"] = "2030-01-01T00:00:00Z"
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	files["evidence/qa.json"] = data
	secondInput.Snapshot.State["evidence"].([]any)[0].(map[string]any)["sha256"] = sha256Hex(data)

	first, err := evaluator.Evaluate(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("Evaluate first: %v", err)
	}
	second, err := evaluator.Evaluate(context.Background(), secondInput)
	if err != nil {
		t.Fatalf("Evaluate second: %v", err)
	}
	if first.Fingerprint == "" || first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprints differ: %q != %q", first.Fingerprint, second.Fingerprint)
	}
}

func TestEvaluatorIgnoresStaleOrHashMismatchEvidence(t *testing.T) {
	evaluator := newTestEvaluator(t)
	tests := []struct {
		name   string
		mutate func(qualitygate.Input)
	}{
		{
			name: "stale generation",
			mutate: func(input qualitygate.Input) {
				input.Snapshot.State["evidence"].([]any)[0].(map[string]any)["baseline_generation"] = 2
			},
		},
		{
			name: "hash mismatch",
			mutate: func(input qualitygate.Input) {
				input.Snapshot.State["evidence"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := reviewGateInput(t, 3)
			test.mutate(input)
			result, err := evaluator.Evaluate(context.Background(), input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if result.Status != qualitygate.StatusNotReady || len(result.EvidenceRefs) != 0 {
				t.Fatalf("result = status %q refs %#v conflicts %#v", result.Status, result.EvidenceRefs, result.Conflicts)
			}
		})
	}
}

func TestMissingIsDeterministicallySorted(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := reviewGateInput(t, 3)
	input.GateID = "GATE-ACCEPTANCE-COMPLETE"
	input.TransitionID = "TR-015"
	input.Snapshot.State["lifecycle"] = map[string]any{"state": "acceptance", "phase": nil}
	input.Snapshot.State["evidence"] = []any{}

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	want := []string{"evidence:acceptance_record", "evidence:clean_round_record"}
	if len(result.Missing) != len(want) || result.Missing[0] != want[0] || result.Missing[1] != want[1] {
		t.Fatalf("missing = %#v, want %#v", result.Missing, want)
	}
}

func TestAcceptanceGateRequiresStructuredS10Manifest(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{})

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("status = %q, want not_ready (refs=%v missing=%v conflicts=%v)", result.Status, result.EvidenceRefs, result.Missing, result.Conflicts)
	}
	if !contains(result.Missing, "s10:acceptance_manifest:ev-acc") {
		t.Fatalf("missing = %#v, want an actionable S10 manifest item", result.Missing)
	}
}

func TestAcceptanceGateConsumesStructuredS10Manifest(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "acceptance")
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = manifest

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("status = %q, want satisfied (missing=%v conflicts=%v)", result.Status, result.Missing, result.Conflicts)
	}
}

func TestAcceptanceGateRejectsManifestOutsideAuthoritativeInventory(t *testing.T) {
	evaluator := newTestEvaluator(t)
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
		return sha256Hex(data)
	}
	req := []byte("# REQ-TEST\n| FR-001 | required behavior |\n")
	contract := []byte("# BE-TEST\n")
	task := []byte("# TASK-TEST\n## Closing Contract\n")
	plan := []byte(`{"review_plan_id":"review-plan-test","review_round":2,"baseline_generation":1,"claims":[{"claim_id":"claim-qa-1"}]}`)
	reqSHA := write("docs/requirements/REQ-TEST.md", req)
	contractSHA := write("docs/contracts/BE-TEST.md", contract)
	taskSHA := write("docs/tasks/TASK-TEST.md", task)
	planSHA := write(".claude/review/plans/review-plan-test.json", plan)
	manifest := validS10Manifest(t, "acceptance")
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Root = root
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = manifest
	input.Snapshot.State["bound_req"] = map[string]any{
		"id": "REQ-TEST", "path": "docs/requirements/REQ-TEST.md", "sha256": reqSHA,
	}
	input.Snapshot.State["documents"] = []any{
		map[string]any{"id": "BE-TEST", "kind": "contract", "path": "docs/contracts/BE-TEST.md", "sha256": contractSHA, "generation": 1},
		map[string]any{"id": "TASK-TEST", "kind": "task", "path": "docs/tasks/TASK-TEST.md", "sha256": taskSHA, "generation": 1},
	}
	input.Snapshot.State["review"].(map[string]any)["plan"] = map[string]any{
		"path": ".claude/review/plans/review-plan-test.json", "sha256": planSHA,
	}

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || !containsPrefix(result.Conflicts, "s10:acceptance_manifest:ev-acc:invalid:") {
		t.Fatalf("result = status %q conflicts=%#v, want authoritative inventory rejection", result.Status, result.Conflicts)
	}
	if !containsConflictFragment(result.Conflicts, "authoritative requirement inventory is missing REQ-TEST/FR-001") {
		t.Fatalf("conflicts = %#v, want missing authoritative requirement", result.Conflicts)
	}
}

func TestAcceptanceGateRejectsManifestEvidenceReferenceDrift(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "acceptance")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	items := decoded["coverage_inventory"].([]any)
	items[0].(map[string]any)["evidence_refs"] = []string{"ev-not-registered"}
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = manifest

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("status = %q code=%q, want unknown/%s", result.Status, result.ErrorCode, qualitygate.ErrorGateUnknown)
	}
	if !containsPrefix(result.Conflicts, "s10:acceptance_manifest:ev-acc:evidence_ref_missing") {
		t.Fatalf("conflicts = %#v, want missing evidence reference", result.Conflicts)
	}
}

func TestReleaseAuditGateRequiresAllAuditAreas(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "release_audit")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	decoded["audit_areas"] = []any{}
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	input := s10GateInput(t, "GATE-RELEASE-AUDIT-APPROVED", "TR-017", "release_audit", map[string]any{
		"audit_manifest_path":   "s10/release-audit-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	acceptanceManifest := validS10Manifest(t, "acceptance")
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = acceptanceManifest
	acceptanceEvidence := input.Files.(memoryFiles)["evidence/ev-acc.json"]
	var acceptanceEnvelope map[string]any
	if err := json.Unmarshal(acceptanceEvidence, &acceptanceEnvelope); err != nil {
		t.Fatalf("decode acceptance evidence: %v", err)
	}
	acceptanceEnvelope["audit_manifest_path"] = "s10/acceptance-manifest.json"
	acceptanceEnvelope["audit_manifest_sha256"] = sha256Hex(acceptanceManifest)
	acceptanceEvidence, err = json.Marshal(acceptanceEnvelope)
	if err != nil {
		t.Fatalf("marshal acceptance evidence: %v", err)
	}
	input.Files.(memoryFiles)["evidence/ev-acc.json"] = acceptanceEvidence
	for _, raw := range input.Snapshot.State["evidence"].([]any) {
		index := raw.(map[string]any)
		if index["id"] == "ev-acc" {
			index["sha256"] = sha256Hex(acceptanceEvidence)
		}
	}
	input.Files.(memoryFiles)["s10/release-audit-manifest.json"] = manifest

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("status = %q code=%q refs=%v, want unknown/%s", result.Status, result.ErrorCode, result.EvidenceRefs, qualitygate.ErrorGateUnknown)
	}
	if !containsPrefix(result.Conflicts, "s10:release_audit_manifest:ev-audit") {
		t.Fatalf("conflicts = %#v, want actionable manifest conflict", result.Conflicts)
	}
}

func TestReleaseAuditGateRequiresCurrentCleanRound(t *testing.T) {
	evaluator := newTestEvaluator(t)
	acceptanceManifest := replaceManifestEvidenceRef(t, validS10Manifest(t, "acceptance"), "ev-clean", "ev-audit")
	releaseManifest := replaceManifestEvidenceRef(t, validS10Manifest(t, "release_audit"), "ev-clean", "ev-acc")
	input := s10GateInput(t, "GATE-RELEASE-AUDIT-APPROVED", "TR-017", "release_audit", map[string]any{
		"audit_manifest_path":   "s10/release-audit-manifest.json",
		"audit_manifest_sha256": sha256Hex(releaseManifest),
	})
	files := input.Files.(memoryFiles)
	files["s10/acceptance-manifest.json"] = acceptanceManifest
	files["s10/release-audit-manifest.json"] = releaseManifest
	for _, evidence := range input.Snapshot.State["evidence"].([]any) {
		entry := evidence.(map[string]any)
		switch entry["id"] {
		case "ev-acc":
			data := withS10ManifestBinding(t, files["evidence/ev-acc.json"], "s10/acceptance-manifest.json", acceptanceManifest)
			files["evidence/ev-acc.json"] = data
			entry["sha256"] = sha256Hex(data)
		case "ev-audit":
			data := withS10ManifestBinding(t, files["evidence/ev-audit.json"], "s10/release-audit-manifest.json", releaseManifest)
			files["evidence/ev-audit.json"] = data
			entry["sha256"] = sha256Hex(data)
		}
	}
	evidence := input.Snapshot.State["evidence"].([]any)
	filtered := make([]any, 0, len(evidence)-1)
	for _, raw := range evidence {
		if raw.(map[string]any)["id"] != "ev-clean" {
			filtered = append(filtered, raw)
		}
	}
	input.Snapshot.State["evidence"] = filtered

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady || !contains(result.Missing, "evidence:clean_round_record") {
		t.Fatalf("status = %q missing=%v conflicts=%v, want not_ready with current clean round missing", result.Status, result.Missing, result.Conflicts)
	}
}

// RC-16 (S10-M1): the review_required acceptance route runs the same S10
// manifest gate as the complete/blocked routes, so a tampered (re-hashed or
// otherwise edited) manifest body cannot pass the gate unverified.
func TestAcceptanceReviewRequiredGateRejectsTamperedManifest(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "acceptance")
	input := s10ReviewRequiredInput(t, "GATE-ACCEPTANCE-REVIEW-REQUIRED", "TR-016", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	files := input.Files.(memoryFiles)
	files["s10/acceptance-manifest.json"] = manifest
	// Simulate post-registration tampering: the manifest body on disk drifts
	// from the sha the envelope pinned.
	tampered, err := json.Marshal(replaceManifestEvidenceRef(t, manifest, "ev-clean", "ev-acc"))
	if err != nil {
		t.Fatalf("marshal tampered manifest: %v", err)
	}
	files["s10/acceptance-manifest.json"] = tampered

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("status = %q code=%q conflicts=%v, want unknown/%s for a tampered review_required manifest", result.Status, result.ErrorCode, result.Conflicts, qualitygate.ErrorGateUnknown)
	}
	if !containsPrefix(result.Conflicts, "s10:acceptance_manifest:ev-acc:sha256_mismatch") {
		t.Fatalf("conflicts = %#v, want a sha256_mismatch conflict", result.Conflicts)
	}
}

// RC-16 (S10-M1): an honest review_required route must satisfy the gate once
// its manifest is structurally complete, evidence-linked, and hash-pinned —
// the outcome-aware ValidateForOutcomeWithBaseline path (requireClean=false).
func TestAcceptanceReviewRequiredGateSatisfiedWithUnresolvedRows(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "acceptance")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	// Two unresolved rows that explain why the round must restart: one unknown
	// coverage row and one unknown counterevidence outcome. A requireClean
	// validation rejects both; the routed outcome keeps them.
	items := decoded["coverage_inventory"].([]any)
	items[0].(map[string]any)["disposition"] = "unknown"
	counterevidence := decoded["counterevidence"].([]any)
	counterevidence[0].(map[string]any)["outcome"] = "unknown"
	decoded["counterevidence"] = counterevidence
	// Metrics must stay derived from the frozen rows: the unknown coverage row
	// drops requirement coverage and both unresolved rows raise unknown_count.
	metrics := decoded["metrics"].(map[string]any)
	metrics["requirement_coverage"] = 0
	metrics["unknown_count"] = 2
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	input := s10ReviewRequiredInput(t, "GATE-ACCEPTANCE-REVIEW-REQUIRED", "TR-016", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = manifest

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("status = %q missing=%v conflicts=%v, want satisfied for an honest review_required route", result.Status, result.Missing, result.Conflicts)
	}
}

// RC-16 (S10-H3): a completion artifact registered in the runtime index but
// missing from disk makes the external changed-surface denominator
// unverifiable. The gate must fail closed with an external_baseline_unverifiable
// conflict instead of silently waiving the exact-set check.
func TestS10GateFailsClosedWhenExternalBaselineUnverifiable(t *testing.T) {
	evaluator := newTestEvaluator(t)
	root := t.TempDir()
	manifest := validS10Manifest(t, "acceptance")
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Root = root
	files := input.Files.(memoryFiles)
	files["s10/acceptance-manifest.json"] = manifest
	// A current-generation completion envelope whose artifact was never
	// materialized: the projection cannot verify the denominator.
	envelope := []byte(`{"kind":"completion_report","changed_paths":["internal/api/handler.go"],"reviewed_paths":[]}` + "\n")
	files["evidence/ev-completion.json"] = envelope
	input.Snapshot.State["evidence"] = append(input.Snapshot.State["evidence"].([]any), map[string]any{
		"id": "ev-completion", "kind": "completion_report", "path": "evidence/ev-completion.json",
		"sha256": sha256Hex(envelope), "status": "valid", "baseline_generation": 1,
		"review_round": nil, "produced_by": []any{"builder-1"}, "invalidated_by": nil,
		"responsibility_id": "BUILD-WORK-PACKAGE", "scope_refs": []any{"internal/api/handler.go"},
	})

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("status = %q code=%q, want unknown/%s when the external baseline is unverifiable", result.Status, result.ErrorCode, qualitygate.ErrorGateUnknown)
	}
	if !containsPrefix(result.Conflicts, "s10:acceptance_manifest:ev-acc:external_baseline_unverifiable") {
		t.Fatalf("conflicts = %#v, want external_baseline_unverifiable", result.Conflicts)
	}
}

func s10ReviewRequiredInput(t *testing.T, gateID, transitionID string, extra map[string]any) qualitygate.Input {
	t.Helper()
	// The audit-manifest binding lives on the acceptance_record envelope the
	// gate re-hashes, so the caller's audit fields must reach s10GateInput's
	// envelope builder — not just the change_impact companion evidence.
	merged := map[string]any{
		"conclusion":      "review_required",
		"requested_event": "acceptance_review_required",
	}
	for key, value := range extra {
		merged[key] = value
	}
	input := s10GateInput(t, gateID, transitionID, "acceptance", merged)
	// The gate requires the change_impact_record companion evidence.
	impactEnvelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-impact",
		"kind":                    "change_impact",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"review_round":            2,
		"producer_agent_id":       "s10-agent",
		"producer_responsibility": "Acceptance",
		"conclusion":              "recorded",
		"requested_event":         "",
		"created_at":              "2026-07-29T00:00:00Z",
	}
	impactData, err := json.Marshal(impactEnvelope)
	if err != nil {
		t.Fatalf("marshal impact evidence: %v", err)
	}
	input.Files.(memoryFiles)["evidence/ev-impact.json"] = impactData
	input.Snapshot.State["evidence"] = append(input.Snapshot.State["evidence"].([]any), map[string]any{
		"id": "ev-impact", "kind": "change_impact", "path": "evidence/ev-impact.json",
		"sha256": sha256Hex(impactData), "status": "valid", "baseline_generation": 1,
		"review_round": 2, "produced_by": []any{"s10-agent"}, "invalidated_by": nil,
		"responsibility_id": "Acceptance", "scope_refs": []any{},
	})
	return input
}

func replaceManifestEvidenceRef(t *testing.T, data []byte, from, to string) []byte {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	var replace func(any)
	replace = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if key == "evidence_refs" {
					refs := child.([]any)
					for i, ref := range refs {
						if ref == from {
							refs[i] = to
						}
					}
				}
				replace(child)
			}
		case []any:
			for _, child := range typed {
				replace(child)
			}
		}
	}
	replace(decoded)
	result, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return result
}

func withS10ManifestBinding(t *testing.T, data []byte, path string, manifest []byte) []byte {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	decoded["audit_manifest_path"] = path
	decoded["audit_manifest_sha256"] = sha256Hex(manifest)
	result, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	return result
}

func TestDocumentPassListsBothMissingResponsibilities(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := documentPassInput(t, "reviewer-1", "reviewer-2", "")
	input.Snapshot.State["evidence"] = []any{}

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	want := []string{
		"evidence:document_review_record:DV-SPEC-CONSISTENCY",
		"evidence:document_review_record:DV-TASK-EXECUTABILITY",
	}
	if len(result.Missing) != len(want) || result.Missing[0] != want[0] || result.Missing[1] != want[1] {
		t.Fatalf("missing = %#v, want %#v", result.Missing, want)
	}
}

func TestDocumentPassRejectsStaleReviewRound(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := documentPassInput(t, "reviewer-1", "reviewer-2", "")
	input.Snapshot.State["review"].(map[string]any)["round"] = 2

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady || len(result.EvidenceRefs) != 0 {
		t.Fatalf("result = status %q refs %#v missing %#v", result.Status, result.EvidenceRefs, result.Missing)
	}
}

func TestEveryRegisteredGateReturnsEvaluatorOwnedStatus(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	evaluator := qualitygate.NewEvaluator(registry)
	taskData := []byte("# TASK\n")
	files := memoryFiles{"docs/tasks/TASK-TEST.md": taskData}

	for _, gateID := range registry.IDs() {
		spec, _ := registry.Lookup(gateID)
		result, err := evaluator.Evaluate(context.Background(), qualitygate.Input{
			Snapshot: runtime.Snapshot{
				Revision: 1,
				State: map[string]any{
					"runtime_id": "loop-test",
					"lifecycle":  map[string]any{"state": spec.CursorState, "phase": spec.CursorPhase},
					"baseline":   map[string]any{"generation": 1},
					"review":     map[string]any{"round": 1},
					"documents": []any{
						map[string]any{
							"id": "TASK-TEST", "kind": "task", "path": "docs/tasks/TASK-TEST.md",
							"version": "v1", "sha256": sha256Hex(taskData), "status": "locked", "generation": 1,
						},
					},
					"evidence": []any{},
				},
			},
			GateID:       gateID,
			TransitionID: spec.TransitionID,
			Files:        files,
		})
		if err != nil {
			t.Errorf("%s Evaluate: %v", gateID, err)
			continue
		}
		if result.Status != qualitygate.StatusNotReady {
			t.Errorf("%s status = %q, want not_ready", gateID, result.Status)
		}
		if result.TransitionCommitted {
			t.Errorf("%s evaluator reported committed transition", gateID)
		}
	}
}

func TestEvaluatorReturnsUnknownForUnreadableOrMalformedCurrentEvidence(t *testing.T) {
	evaluator := newTestEvaluator(t)
	tests := []struct {
		name         string
		mutate       func(*qualitygate.Input)
		wantConflict string
	}{
		{
			name: "unreadable",
			mutate: func(input *qualitygate.Input) {
				input.Files = nil
			},
			wantConflict: "evidence:ev-qa:unreadable",
		},
		{
			name: "malformed schema",
			mutate: func(input *qualitygate.Input) {
				files := input.Files.(memoryFiles)
				data := []byte("{")
				files["evidence/qa.json"] = data
				input.Snapshot.State["evidence"].([]any)[0].(map[string]any)["sha256"] = sha256Hex(data)
			},
			wantConflict: "evidence:ev-qa:schema",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := reviewGateInput(t, 3)
			test.mutate(&input)
			result, err := evaluator.Evaluate(context.Background(), input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
				t.Fatalf("result = status %q code %q", result.Status, result.ErrorCode)
			}
			if !contains(result.Conflicts, test.wantConflict) {
				t.Fatalf("conflicts = %#v, want %q", result.Conflicts, test.wantConflict)
			}
		})
	}
}

func newTestEvaluator(t *testing.T) *qualitygate.Engine {
	t.Helper()
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return qualitygate.NewEvaluator(registry)
}

func planningDesignInput(t *testing.T, withEvidence bool) qualitygate.Input {
	t.Helper()
	reqData := []byte("# REQ\n")
	architectureData := []byte("# Architecture\n")
	files := memoryFiles{
		"docs/requirements/REQ-TEST.md": reqData,
		"docs/design/ARCH-TEST.md":      architectureData,
	}
	documents := []any{
		map[string]any{
			"id": "REQ-TEST", "kind": "req", "path": "docs/requirements/REQ-TEST.md",
			"version": "v1.0.0", "sha256": sha256Hex(reqData), "status": "locked", "generation": 1,
		},
		map[string]any{
			"id": "ARCH-TEST", "kind": "design", "path": "docs/design/ARCH-TEST.md",
			"version": "v1.0.0", "sha256": sha256Hex(architectureData), "status": "locked", "generation": 1,
		},
	}
	var evidence []any
	if withEvidence {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             "ev-design",
			"kind":                    "planning_design_record",
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"producer_agent_id":       "architect-1",
			"producer_responsibility": "Architect",
			"subject_refs": []any{
				map[string]any{
					"path": "docs/design/ARCH-TEST.md", "version": "v1.0.0",
					"sha256": sha256Hex(architectureData),
				},
			},
			"conclusion": "pass",
			"created_at": "2026-07-29T00:00:00Z",
		}
		envelopeData, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal evidence: %v", err)
		}
		files["evidence/design.json"] = envelopeData
		evidence = append(evidence, map[string]any{
			"id": "ev-design", "kind": "planning_design_record", "path": "evidence/design.json",
			"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": 1,
			"review_round": nil, "produced_by": []any{"architect-1"}, "invalidated_by": nil,
			"responsibility_id": "Architect", "scope_refs": []any{"docs/design/ARCH-TEST.md"},
		})
	}
	return qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 17,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "design"},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 0},
				"documents":  documents,
				"evidence":   evidence,
			},
		},
		GateID:       "GATE-PLANNING-DESIGN-COMPLETE",
		TransitionID: "PTR-PLAN-01",
		Files:        files,
	}
}

func planningInputForGate(t *testing.T, gateID, transitionID, phase, evidenceKind, responsibility string) qualitygate.Input {
	t.Helper()
	input := planningDesignInput(t, true)
	input.GateID = gateID
	input.TransitionID = transitionID
	input.Snapshot.State["lifecycle"].(map[string]any)["phase"] = phase
	files := input.Files.(memoryFiles)
	data := files["evidence/design.json"]
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	envelope["kind"] = evidenceKind
	envelope["producer_responsibility"] = responsibility
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	files["evidence/design.json"] = data
	index := input.Snapshot.State["evidence"].([]any)[0].(map[string]any)
	index["kind"] = evidenceKind
	index["responsibility_id"] = responsibility
	index["sha256"] = sha256Hex(data)
	return input
}

func reviewGateInput(t *testing.T, evidenceRound int) qualitygate.Input {
	t.Helper()
	taskData := []byte("# TASK\n")
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-qa",
		"kind":                    "review_result",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"review_round":            evidenceRound,
		"producer_agent_id":       "qa-1",
		"producer_responsibility": "QA",
		"subject_refs": []any{
			map[string]any{
				"path": "docs/tasks/TASK-TEST.md", "version": "v1.0.0",
				"sha256": sha256Hex(taskData),
			},
		},
		"conclusion": "req_change_required",
		"created_at": "2026-07-29T00:00:00Z",
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	files := memoryFiles{
		"docs/tasks/TASK-TEST.md": taskData,
		"evidence/qa.json":        envelopeData,
	}
	return qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 21,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "verification", "phase": "qa"},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 3},
				"documents": []any{
					map[string]any{
						"id": "TASK-TEST", "kind": "task", "path": "docs/tasks/TASK-TEST.md",
						"version": "v1.0.0", "sha256": sha256Hex(taskData), "status": "locked", "generation": 1,
					},
				},
				"evidence": []any{
					map[string]any{
						"id": "ev-qa", "kind": "review_result", "path": "evidence/qa.json",
						"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": 1,
						"review_round": evidenceRound, "produced_by": []any{"qa-1"}, "invalidated_by": nil,
						"responsibility_id": "QA", "scope_refs": []any{"docs/tasks/TASK-TEST.md"},
					},
				},
			},
		},
		GateID:       "GATE-VERIFY-REQ-CHANGE-REQUIRED",
		TransitionID: "TR-010",
		Files:        files,
	}
}

// TestMissingS10EvidenceRefsRejectsPhantom proves the RC-14 (S10-H1)
// phantom-reference defense: an S10 manifest coverage row that cites a
// non-existent evidence id is reported as a missing reference instead of
// silently accepted as content. Execution anchors (scheme://) are likewise
// rejected because they are not runtime evidence ids and cannot satisfy the
// S10 reference contract.
func TestMissingS10EvidenceRefsRejectsPhantom(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "acceptance")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	items := decoded["coverage_inventory"].([]any)
	items[0].(map[string]any)["evidence_refs"] = []string{"evidence/phantom.json"}
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = manifest

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !containsPrefix(result.Conflicts, "s10:acceptance_manifest:ev-acc:evidence_ref_missing") {
		t.Fatalf("conflicts = %#v, want phantom evidence_ref_missing", result.Conflicts)
	}
}

// TestS10SelfEvidenceRefRejectsEnvelopeSelfProof proves the RC-14 (S10-H1)
// self-proof defense: the S10 envelope's own id (ev-acc) cannot satisfy an
// evidence_ref in the same manifest. The missingS10EvidenceRefs gate skips
// the envelope's own id from `available` so an envelope that lists itself
// as its own evidence is reported as a missing reference instead of
// silently closing the gate.
func TestS10SelfEvidenceRefRejectsEnvelopeSelfProof(t *testing.T) {
	evaluator := newTestEvaluator(t)
	manifest := validS10Manifest(t, "acceptance")
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	items := decoded["coverage_inventory"].([]any)
	items[0].(map[string]any)["evidence_refs"] = []string{"ev-acc"}
	manifest, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	input := s10GateInput(t, "GATE-ACCEPTANCE-COMPLETE", "TR-015", "acceptance", map[string]any{
		"audit_manifest_path":   "s10/acceptance-manifest.json",
		"audit_manifest_sha256": sha256Hex(manifest),
	})
	input.Files.(memoryFiles)["s10/acceptance-manifest.json"] = manifest

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !containsPrefix(result.Conflicts, "s10:acceptance_manifest:ev-acc:evidence_ref_missing") {
		t.Fatalf("conflicts = %#v, want self-proof evidence_ref_missing", result.Conflicts)
	}
}

func s10GateInput(t *testing.T, gateID, transitionID, lifecycleState string, extra map[string]any) qualitygate.Input {
	t.Helper()
	addEvidence := func(id, kind, responsibility, conclusion string) (map[string]any, []byte) {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    kind,
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"review_round":            2,
			"producer_agent_id":       "s10-agent",
			"producer_responsibility": responsibility,
			"conclusion":              conclusion,
			"created_at":              "2026-07-29T00:00:00Z",
		}
		for key, value := range extra {
			envelope[key] = value
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal S10 evidence: %v", err)
		}
		return map[string]any{
			"id": id, "kind": kind, "path": "evidence/" + id + ".json",
			"sha256": sha256Hex(data), "status": "valid", "baseline_generation": 1,
			"review_round": 2, "produced_by": []any{"s10-agent"}, "invalidated_by": nil,
			"responsibility_id": responsibility, "scope_refs": []any{},
		}, data
	}
	files := memoryFiles{}
	acceptance, acceptanceData := addEvidence("ev-acc", "acceptance", "Orchestrator", "pass")
	files["evidence/ev-acc.json"] = acceptanceData
	clean, cleanData := addEvidence("ev-clean", "clean_round", "Orchestrator", "pass")
	files["evidence/ev-clean.json"] = cleanData
	evidence := []any{acceptance, clean}
	if lifecycleState == "release_audit" {
		audit, auditData := addEvidence("ev-audit", "release_audit", "Release Auditor", "approved")
		files["evidence/ev-audit.json"] = auditData
		evidence = []any{audit, acceptance, clean}
	}
	return qualitygate.Input{
		Snapshot: runtime.Snapshot{Revision: 10, State: map[string]any{
			"runtime_id": "loop-test",
			"lifecycle":  map[string]any{"state": lifecycleState, "phase": nil},
			"baseline":   map[string]any{"generation": 1},
			"review":     map[string]any{"round": 2},
			"documents":  []any{},
			"evidence":   evidence,
		}},
		GateID: gateID, TransitionID: transitionID, Files: files,
	}
}

func validS10Manifest(t *testing.T, kind string) []byte {
	t.Helper()
	items := []any{}
	counterevidence := []any{}
	for _, item := range []struct {
		id, category string
	}{
		{"REQ-AC-001", "requirement"},
		{"CONTRACT-001", "contract"},
		{"PATH-001", "changed_path"},
		{"AUDIT-001", "audit_area"},
	} {
		items = append(items, map[string]any{
			"id": item.id, "category": item.category, "source_refs": []string{"source:" + item.id},
			"expected": "expected " + item.id, "oracle": "oracle " + item.id, "owner": "S10 reviewer",
			"evidence_refs": []string{"ev-clean"}, "disposition": "pass",
		})
		counterevidence = append(counterevidence, map[string]any{
			"id": "CE-" + item.id, "inventory_id": item.id, "question": "what disproves " + item.id + "?",
			"evidence_refs": []string{"ev-clean"}, "outcome": "pass",
		})
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_type": kind, "runtime_id": "loop-test",
		"baseline_generation": 1, "review_round": 2, "coverage_inventory": items,
		"counterevidence":   counterevidence,
		"risks":             []any{},
		"technical_debt":    []any{},
		"blocking_findings": []any{},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1,
			"audit_area_coverage": 1, "unknown_count": 0, "unsupported_pass_count": 0,
			"unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0,
		},
	}
	if kind == "release_audit" {
		areas := []any{}
		for _, id := range []string{"state_machine", "transaction_uow", "concurrency_idempotency", "data_migration", "call_sites_topology", "observability_errors", "verification_evidence", "docs_release_scope"} {
			areas = append(areas, map[string]any{"id": id, "conclusion": "pass", "owner": "Release Auditor", "evidence_refs": []string{"ev-clean"}})
		}
		manifest["audit_areas"] = areas
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal valid S10 manifest: %v", err)
	}
	return data
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func conflictingRequestedEventsInput(t *testing.T) qualitygate.Input {
	t.Helper()
	taskData := []byte("# TASK\n")
	files := memoryFiles{"docs/tasks/TASK-TEST.md": taskData}
	var evidence []any
	add := func(id, conclusion, event string) {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    "targeted_reverification_record",
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"review_round":            3,
			"producer_agent_id":       "finder-1",
			"producer_responsibility": "Original Finder",
			"subject_refs": []any{
				map[string]any{
					"path": "docs/tasks/TASK-TEST.md", "version": "v1.0.0",
					"sha256": sha256Hex(taskData),
				},
			},
			"conclusion":      conclusion,
			"requested_event": event,
			"created_at":      "2026-07-29T00:00:00Z",
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal evidence: %v", err)
		}
		path := "evidence/" + id + ".json"
		files[path] = data
		evidence = append(evidence, map[string]any{
			"id": id, "kind": "targeted_reverification_record", "path": path,
			"sha256": sha256Hex(data), "status": "valid", "baseline_generation": 1,
			"review_round": 3, "produced_by": []any{"finder-1"}, "invalidated_by": nil,
			"responsibility_id": "Original Finder", "scope_refs": []any{"docs/tasks/TASK-TEST.md"},
		})
	}
	add("ev-pass", "pass", "targeted_reverification_pass")
	add("ev-fail", "fail", "targeted_reverification_fail")

	return qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 22,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "bug_resolution", "phase": "targeted_reverification"},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 3},
				"documents": []any{
					map[string]any{
						"id": "TASK-TEST", "kind": "task", "path": "docs/tasks/TASK-TEST.md",
						"version": "v1.0.0", "sha256": sha256Hex(taskData), "status": "locked", "generation": 1,
					},
				},
				"evidence": evidence,
			},
		},
		GateID:       "GATE-TARGETED-REVERIFICATION-PASS",
		TransitionID: "PTR-BUG-06",
		Files:        files,
	}
}

func documentPassInput(t *testing.T, specAgent, taskAgent, author string) qualitygate.Input {
	t.Helper()
	type doc struct {
		id, kind, path, version string
		data                    []byte
	}
	docs := []doc{
		{id: "REQ-TEST", kind: "req", path: "docs/requirements/REQ-TEST.md", version: "v1", data: []byte("# REQ\n")},
		{id: "ARCH-TEST", kind: "design", path: "docs/design/ARCH-TEST.md", version: "v1", data: []byte("# ARCH\n")},
		{id: "BE-TEST", kind: "contract", path: "docs/contracts/BE-TEST.md", version: "v1", data: []byte("# BE\n")},
		{id: "TASK-TEST", kind: "task", path: "docs/tasks/TASK-TEST.md", version: "v1", data: []byte("# TASK\n")},
	}
	files := memoryFiles{}
	var documents []any
	var subjects []any
	for _, document := range docs {
		files[document.path] = document.data
		entry := map[string]any{
			"id": document.id, "kind": document.kind, "path": document.path,
			"version": document.version, "sha256": sha256Hex(document.data),
			"status": "locked", "generation": 1,
		}
		if document.kind == "design" && author != "" {
			entry["author_agent_id"] = author
		}
		documents = append(documents, entry)
		subjects = append(subjects, map[string]any{
			"path": document.path, "version": document.version, "sha256": sha256Hex(document.data),
		})
	}

	var evidence []any
	add := func(id, responsibility, agent string) {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    "document_review_record",
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"review_round":            1,
			"producer_agent_id":       agent,
			"producer_responsibility": responsibility,
			"subject_refs":            subjects,
			"conclusion":              "pass",
			"created_at":              "2026-07-29T00:00:00Z",
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal evidence: %v", err)
		}
		path := "evidence/" + id + ".json"
		files[path] = data
		evidence = append(evidence, map[string]any{
			"id": id, "kind": "document_review_record", "path": path,
			"sha256": sha256Hex(data), "status": "valid", "baseline_generation": 1,
			"review_round": 1, "produced_by": []any{agent}, "invalidated_by": nil,
			"responsibility_id": responsibility, "scope_refs": []any{},
		})
	}
	add("ev-spec", "DV-SPEC-CONSISTENCY", specAgent)
	add("ev-task", "DV-TASK-EXECUTABILITY", taskAgent)

	return qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 18,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 1},
				"documents":  documents,
				"evidence":   evidence,
			},
		},
		GateID:       "GATE-DOCUMENT-PASS",
		TransitionID: "TR-003",
		Files:        files,
	}
}

func builderBatchInput(t *testing.T) qualitygate.Input {
	t.Helper()
	taskOne := []byte("# TASK 1\n")
	taskTwo := []byte("# TASK 2\n")
	files := memoryFiles{
		"docs/tasks/TASK-TEST-01.md": taskOne,
		"docs/tasks/TASK-TEST-02.md": taskTwo,
	}
	documents := []any{
		map[string]any{
			"id": "TASK-TEST-01", "kind": "task", "path": "docs/tasks/TASK-TEST-01.md",
			"version": "v1", "sha256": sha256Hex(taskOne), "status": "locked", "generation": 1,
		},
		map[string]any{
			"id": "TASK-TEST-02", "kind": "task", "path": "docs/tasks/TASK-TEST-02.md",
			"version": "v1", "sha256": sha256Hex(taskTwo), "status": "locked", "generation": 1,
		},
	}
	subjects := []any{
		map[string]any{"path": "docs/tasks/TASK-TEST-01.md", "version": "v1", "sha256": sha256Hex(taskOne)},
		map[string]any{"path": "docs/tasks/TASK-TEST-02.md", "version": "v1", "sha256": sha256Hex(taskTwo)},
	}
	var evidence []any
	add := func(id, kind, responsibility, conclusion, taskID string) {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    kind,
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"producer_agent_id":       "orchestrator-1",
			"producer_responsibility": responsibility,
			"subject_refs":            subjects,
			"conclusion":              conclusion,
			"created_at":              "2026-07-29T00:00:00Z",
		}
		if taskID != "" {
			envelope["task_id"] = taskID
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal evidence: %v", err)
		}
		path := "evidence/" + id + ".json"
		files[path] = data
		evidence = append(evidence, map[string]any{
			"id": id, "kind": kind, "path": path, "sha256": sha256Hex(data),
			"status": "valid", "baseline_generation": 1, "review_round": nil,
			"produced_by": []any{"orchestrator-1"}, "invalidated_by": nil,
			"responsibility_id": responsibility, "scope_refs": []any{},
		})
	}
	add("ev-completion-1", "completion_report", "BUILD-WORK-PACKAGE", "completed", "TASK-TEST-01")
	add("ev-team", "team_manifest_record", "Orchestrator", "complete", "")

	return qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 19,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "building", "phase": nil},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 0},
				"documents":  documents,
				"evidence":   evidence,
				"entities": map[string]any{
					"tasks": []any{
						map[string]any{"id": "TASK-TEST-01", "state": "review"},
						map[string]any{"id": "TASK-TEST-02", "state": "review"},
					},
				},
			},
		},
		GateID:       "GATE-BUILDER-BATCH-READY",
		TransitionID: "TR-006",
		Files:        files,
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsConflictFragment(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
