package qualitygate_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// listingFiles extends memoryFiles with directory listing so the evaluator
// can discover disk-declared artifacts.
type listingFiles map[string][]byte

func (m listingFiles) ReadFile(path string) ([]byte, error) {
	data, ok := m[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (m listingFiles) ReadDir(dir string) ([]os.DirEntry, error) {
	prefix := ""
	if dir != "." {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}
	var entries []os.DirEntry
	seen := map[string]bool{}
	for path := range m {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		name := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 2)[0]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, memoryDirEntry{name: name})
	}
	return entries, nil
}

type memoryDirEntry struct{ name string }

func (e memoryDirEntry) Name() string               { return e.name }
func (e memoryDirEntry) IsDir() bool                { return !strings.HasSuffix(e.name, ".md") }
func (e memoryDirEntry) Type() os.FileMode          { return 0 }
func (e memoryDirEntry) Info() (os.FileInfo, error) { return nil, os.ErrNotExist }

// TestPlanningGatesReadDiskDeclaredArtifacts verifies that the planning
// gates' document precondition must be satisfiable by disk-declared facts
// (contract Status: locked / task Status: complete) — the registration that
// documents[] carries is produced by the gated transitions themselves, so
// requiring it up front deadlocks the hook auto-advance path.
func TestPlanningGatesReadDiskDeclaredArtifacts(t *testing.T) {
	evaluator := newTestEvaluator(t)

	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-contract",
		"kind":                    "planning_contract",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "planner-1",
		"producer_responsibility": "Contract Planner",
		"subject_refs": []any{map[string]any{
			"path": "docs/dev/contracts/BE-001.md", "version": "v1.0.0",
			"sha256": sha256Hex(contractData),
		}},
		"conclusion": "pass",
		"created_at": "2026-08-17T00:00:00Z",
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 3,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "contracts", "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents":  []any{},
				"evidence": []any{map[string]any{
					"id": "ev-contract", "kind": "planning_contract", "path": "evidence/contract.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"planner-1"}, "invalidated_by": nil,
					"responsibility_id": "Contract Planner", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "PTR-PLAN-02",
		GateID:       "GATE-PLANNING-CONTRACTS-COMPLETE",
		Files: listingFiles{
			"docs/dev/contracts/BE-001.md": contractData,
			"evidence/contract.json":       envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("disk-declared locked contract + qualified planning evidence must satisfy the gate without a pre-existing documents[] registration; got status=%q missing=%#v", result.Status, result.Missing)
	}
}

// TestPlanningGatesStillRefuseWhenDiskAlsoLacks: the disk fallback must not
// weaken the gate — no disk contract and no registration stays NOT_READY.
func TestPlanningGatesStillRefuseWhenDiskAlsoLacks(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := planningInputForGate(t, "GATE-PLANNING-CONTRACTS-COMPLETE", "PTR-PLAN-02", "contracts", "planning_contract_record", "Contract Planner")
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("gate must stay NOT_READY when neither disk nor documents[] declares the artifact, got %q", result.Status)
	}
}

func TestPlanningContractsGateLeavesAnUnacceptedPacketAsMissing(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := planningInputForGate(t, "GATE-PLANNING-CONTRACTS-COMPLETE", "PTR-PLAN-02", "contracts", "planning_contract_record", "Contract Planner")
	state := input.Snapshot.State
	state["schema_version"] = "1.2.0"
	state["configuration"] = map[string]any{"optimization": map[string]any{"packet": true}}
	lifecycle := state["lifecycle"].(map[string]any)
	lifecycle["phase_revision"] = float64(3)
	state["entities"] = map[string]any{
		"work_packets": []any{map[string]any{
			"packet_id": "packet-unaccepted-s3", "template_id": "s3-contract-v1",
			"binding": map[string]any{
				"runtime_id": "loop-test", "generation": float64(1), "stage_entry": float64(3),
				"stage": "S3", "owner": "Main",
			},
		}},
		"submissions": []any{},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady || result.ErrorCode != "" {
		t.Fatalf("an issued but unaccepted S3 questionnaire should remain ordinary not_ready: status=%q error=%q conflicts=%v", result.Status, result.ErrorCode, result.Conflicts)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "evidence:planning_contract_record" {
		t.Fatalf("missing = %#v, want the unfilled S3 contract deliverable", result.Missing)
	}
}

// TestPlanningDesignGateReadsDiskDeclaredArchitecture verifies that:
// the S2 exit gate must accept a disk-declared locked architecture document
// without a pre-existing documents[] registration (the registration happens
// at PTR-PLAN-01's commit; nothing produced it before, so the organic S2
// exit was deadlocked).
func TestPlanningDesignGateReadsDiskDeclaredArchitecture(t *testing.T) {
	evaluator := newTestEvaluator(t)
	archData := []byte("# ARCHITECTURE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	reqData := []byte("# REQ-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-design",
		"kind":                    "planning_design",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "architect-1",
		"producer_responsibility": "Architect",
		"subject_refs": []any{
			map[string]any{"path": "docs/architecture/ARCHITECTURE-001.md", "version": "v1.0.0", "sha256": sha256Hex(archData)},
			map[string]any{"path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(reqData)},
		},
		"conclusion": "pass",
		"created_at": "2026-08-18T00:00:00Z",
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 2,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "design", "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"bound_req":  map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "status": "locked"},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{ // req registered by bind; design NOT registered (organic pre-commit state)
					map[string]any{"id": "REQ-001", "kind": "req", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(reqData), "status": "locked", "generation": float64(1)},
				},
				"evidence": []any{map[string]any{
					"id": "ev-design", "kind": "planning_design", "path": "evidence/design.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"architect-1"}, "invalidated_by": nil,
					"responsibility_id": "Architect", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "PTR-PLAN-01",
		GateID:       "GATE-PLANNING-DESIGN-COMPLETE",
		Files: listingFiles{
			"docs/architecture/ARCHITECTURE-001.md": archData,
			"docs/requirements/REQ-001.md":          reqData,
			"evidence/design.json":                  envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("disk-declared locked architecture + registered req must satisfy the S2 exit gate; got status=%q missing=%#v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}

// TestDocumentPassGateFlagsRegisteredDocumentDrift verifies that:
// a registered document whose on-disk sha no longer matches must block
// GATE-DOCUMENT-PASS with the path named — otherwise a document the
// reviewers never saw can be re-registered from disk and locked into
// building by TR-003's commit.
func TestDocumentPassGateFlagsRegisteredDocumentDrift(t *testing.T) {
	evaluator := newTestEvaluator(t)
	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	driftedData := []byte("# BE-001 (edited after review)\n\n> 状态：locked\n> 版本：v1.0.0\n")
	doc := func(data []byte) map[string]any {
		return map[string]any{"id": "BE-001", "kind": "contract", "path": "docs/dev/contracts/BE-001.md", "version": "v1.0.0", "sha256": sha256Hex(data), "status": "locked", "generation": float64(1)}
	}
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 5,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				// registered with the ORIGINAL sha; disk carries the edited bytes
				"documents": []any{doc(contractData)},
				"evidence":  []any{},
			},
		},
		TransitionID: "TR-003",
		GateID:       "GATE-DOCUMENT-PASS",
		Files: listingFiles{
			"docs/dev/contracts/BE-001.md": driftedData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	blocked := false
	for _, conflict := range result.Conflicts {
		if strings.Contains(conflict, "document_drift:docs/dev/contracts/BE-001.md") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("a registered document drifting on disk must produce a document_drift conflict naming the path; got status=%q conflicts=%v missing=%v", result.Status, result.Conflicts, result.Missing)
	}
}

// Frozen document-review records retain their complete original envelope
// validation. Normal S5 authoring is covered from actual issued questionnaires;
// a public professional template must not demand machine metadata copying.
func TestFrozenDocumentReviewEnvelopeQualifiesOriginalGate(t *testing.T) {
	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-dv-spec", "kind": "document_review",
		"runtime_id": "loop-test", "baseline_generation": 1,
		"producer_agent_id": "dv-spec-1", "producer_responsibility": "DV-SPEC-CONSISTENCY",
		"subject_refs": []any{map[string]any{"path": "docs/dev/contracts/BE-001.md", "version": "v1.0.0", "sha256": sha256Hex(contractData)}},
		"conclusion":   "pass", "requested_event": "", "created_at": "2026-08-18T00:00:00Z",
	}

	envelopeData, _ := json.Marshal(envelope)

	evaluator := newTestEvaluator(t)
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 5,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{map[string]any{
					"id": "BE-001", "kind": "contract", "path": "docs/dev/contracts/BE-001.md",
					"version": "v1.0.0", "sha256": sha256Hex(contractData), "status": "locked", "generation": float64(1),
				}},
				"evidence": []any{map[string]any{
					"id": "ev-dv-spec", "kind": "document_review", "path": "evidence/dv-spec.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"dv-spec-1"}, "invalidated_by": nil,
					"responsibility_id": "DV-SPEC-CONSISTENCY", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "TR-003",
		GateID:       "GATE-DOCUMENT-PASS",
		Files: listingFiles{
			"docs/dev/contracts/BE-001.md": contractData,
			"evidence/dv-spec.json":        envelopeData,
		},
	}
	// A single responsibility passes only its own requirement; the gate
	// stays not_ready for the other one — assert OUR evidence qualified.
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	qualified := false
	for _, ref := range result.EvidenceRefs {
		if ref == "ev-dv-spec" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("the original frozen envelope must qualify at the gate; got status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}

// TestREVTemplateEnvelopeFixRequiredVariant extends the C5 lock to the
// fix_required branch: the taught requested_event value must satisfy
// GATE-DOCUMENT-FIX-REQUIRED's requested requirement (round-3 H2/#1 —
// the template once taught a dead value on the req_change branch).
func TestREVTemplateEnvelopeFixRequiredVariant(t *testing.T) {
	evaluator := newTestEvaluator(t)
	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-dv-fix", "kind": "document_review",
		"runtime_id": "loop-test", "baseline_generation": 1,
		"producer_agent_id": "dv-spec-1", "producer_responsibility": "DV-SPEC-CONSISTENCY",
		"subject_refs": []any{map[string]any{"path": "docs/dev/contracts/BE-001.md", "version": "v1.0.0", "sha256": sha256Hex(contractData)}},
		"conclusion":   "fix_required", "requested_event": "document_fix_required",
		"created_at": "2026-08-18T00:00:00Z",
	}
	envelopeData, _ := json.Marshal(envelope)
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 5,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{map[string]any{
					"id": "BE-001", "kind": "contract", "path": "docs/dev/contracts/BE-001.md",
					"version": "v1.0.0", "sha256": sha256Hex(contractData), "status": "locked", "generation": float64(1),
				}},
				"evidence": []any{map[string]any{
					"id": "ev-dv-fix", "kind": "document_review", "path": "evidence/dv-fix.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"dv-spec-1"}, "invalidated_by": nil,
					"responsibility_id": "DV-SPEC-CONSISTENCY", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "TR-004",
		GateID:       "GATE-DOCUMENT-FIX-REQUIRED",
		Files: listingFiles{
			"docs/dev/contracts/BE-001.md": contractData,
			"evidence/dv-fix.json":         envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	qualified := false
	for _, ref := range result.EvidenceRefs {
		if ref == "ev-dv-fix" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("the taught fix_required envelope must satisfy GATE-DOCUMENT-FIX-REQUIRED; got status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}

// Existing packet-off records retain their original complete envelope reader.
// Normal authoring is exercised by the CLI questionnaire tests; this fixture
// is not a request to teach authors the internal persistence protocol.
func TestFrozenPlanningEnvelopeQualifiesOriginalDesignGate(t *testing.T) {
	archData := []byte("# ARCHITECTURE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	reqData := []byte("# REQ-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "planning-design-pass", "kind": "planning_design",
		"runtime_id": "loop-test", "baseline_generation": 1,
		"producer_agent_id": "architect-1", "producer_responsibility": "Architect",
		"subject_refs": []any{}, "conclusion": "pass", "created_at": "2026-08-18T00:00:00Z",
	}

	envelopeData, _ := json.Marshal(envelope)

	evaluator := newTestEvaluator(t)
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 2,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "design", "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{map[string]any{
					"id": "REQ-001", "kind": "req", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(reqData), "status": "locked", "generation": float64(1),
				}},
				"evidence": []any{map[string]any{
					"id": "planning-design-pass", "kind": "planning_design", "path": "evidence/design.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"architect-1"}, "invalidated_by": nil,
					"responsibility_id": "Architect", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "PTR-PLAN-01",
		GateID:       "GATE-PLANNING-DESIGN-COMPLETE",
		Files: listingFiles{
			"docs/architecture/ARCHITECTURE-001.md": archData,
			"docs/requirements/REQ-001.md":          reqData,
			"evidence/design.json":                  envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	qualified := false
	for _, ref := range result.EvidenceRefs {
		if ref == "planning-design-pass" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("the original frozen planning envelope must qualify the S2 design gate; got status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}
