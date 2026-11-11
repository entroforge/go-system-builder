package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRecordDocumentEvidenceKeepsResponsibilityWithoutReviewRound(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	if err := os.WriteFile(filepath.Join(root, "REV-001.md"), []byte(`{"schema_version":"1.0.0","evidence_id":"EV-DOCUMENT-001","kind":"document_review","runtime_id":"loop-REQ-002-example","baseline_generation":1,"producer_agent_id":"document-verifier","producer_responsibility":"DV-TRUTH-AUDIT","review_round":3}`), 0o644); err != nil {
		t.Fatal(err)
	}

	next, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "EV-DOCUMENT-001",
		Kind:             "document_review",
		Path:             "REV-001.md",
		ProducedBy:       []string{"document-verifier"},
		ResponsibilityID: "DV-TRUTH-AUDIT",
		Validator:        testCandidateValidator(),
	})
	if err != nil {
		t.Fatalf("RecordEvidence failed: %v", err)
	}
	if next.Revision != 2 {
		t.Fatalf("revision = %d, want 2", next.Revision)
	}
	evidence, _ := next.State["evidence"].([]any)
	if len(evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(evidence))
	}
	entry := evidence[0].(map[string]any)
	if entry["id"] != "EV-DOCUMENT-001" || entry["status"] != "valid" {
		t.Fatalf("unexpected evidence entry: %#v", entry)
	}
	if entry["path"] != "REV-001.md" || entry["responsibility_id"] != "DV-TRUTH-AUDIT" {
		t.Fatalf("original responsibility was not recorded: %#v", entry)
	}
	if _, exists := entry["review_round"]; exists {
		t.Fatalf("irrelevant old review round was recorded: %#v", entry)
	}
	if _, exists := entry["sha256"]; exists {
		t.Fatalf("obsolete source digest was recorded: %#v", entry)
	}
}

func TestRecordEvidenceRejectsUnsafePathBeforeMutation(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "EV-UNSAFE-001",
		Kind:             "document_review",
		Path:             "../outside.md",
		ProducedBy:       []string{"document-verifier"},
	}); err == nil {
		t.Fatal("unsafe evidence path must be rejected")
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unsafe evidence path must not mutate runtime")
	}
}

func TestRecordEvidenceAcceptsChangeImpactEvidence(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	if err := os.WriteFile(filepath.Join(root, "IMPACT-001.md"), []byte("scope impact\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	next, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "EV-IMPACT-001",
		Kind:             "change_impact",
		Path:             "IMPACT-001.md",
		ProducedBy:       []string{"orchestrator"},
		ResponsibilityID: "BUILD-WORK-PACKAGE",
		Validator:        testCandidateValidator(),
	})
	if err != nil {
		t.Fatalf("RecordEvidence change impact failed: %v", err)
	}
	evidence, _ := next.State["evidence"].([]any)
	if len(evidence) != 1 || evidence[0].(map[string]any)["kind"] != "change_impact" {
		t.Fatalf("unexpected change impact evidence: %#v", evidence)
	}
}

func TestRecordEvidenceRejectsRepeatedIdentityWithoutWritingRuntime(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	if err := os.WriteFile(filepath.Join(root, "REV-001.md"), []byte(`{"schema_version":"1.0.0","evidence_id":"EV-DOCUMENT-001","kind":"document_review","runtime_id":"loop-REQ-002-example","baseline_generation":1,"producer_agent_id":"document-verifier","producer_responsibility":"DV-TRUTH-AUDIT","review_round":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	request := runtime.EvidenceRequest{
		ExpectedRevision: -1,
		ID:               "EV-DOCUMENT-001",
		Kind:             "document_review",
		Path:             "REV-001.md",
		ProducedBy:       []string{"document-verifier"},
		ResponsibilityID: "DV-TRUTH-AUDIT",
		Validator:        testCandidateValidator(),
	}
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err != nil {
		t.Fatalf("RecordEvidence failed: %v", err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	// G18 reverse assertion: the evidence identity is the event identity, so a
	// repeated record of the same evidence id is still rejected by the
	// duplicate evidence guard and writes no Runtime fact and no journal fact.
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err == nil || !strings.Contains(err.Error(), "is already registered") {
		t.Fatalf("repeated evidence id was not rejected as a duplicate: %v", err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateBefore) != string(stateAfter) || string(journalBefore) != string(journalAfter) {
		t.Fatal("repeated evidence id wrote a new Runtime fact")
	}
	// G18 reverse assertion: the event id names the evidence identity and
	// carries no Runtime write count.
	ids := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(journalAfter)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if id, ok := event["event_id"].(string); ok {
			ids[id]++
		}
	}
	if ids["evt-evidence-EV-DOCUMENT-001"] != 1 {
		t.Fatalf("evidence event identity was not written exactly once: %v", ids)
	}
	for id := range ids {
		if strings.HasPrefix(id, "evt-evidence-") && strings.Contains(id, "-r") {
			t.Fatalf("evidence event id still carries a Runtime write count: %q", id)
		}
	}
}

func TestRecordEvidenceRejectsIncompleteS10ArtifactBeforeMutation(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateBefore, &current); err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(map[string]any{
		"schema_version": "1.0.0", "evidence_id": "EV-ACC-001", "kind": "acceptance_record",
		"runtime_id": current["runtime_id"], "baseline_generation": current["baseline"].(map[string]any)["generation"],
		"review_round":      current["review"].(map[string]any)["round"],
		"producer_agent_id": "agent-1", "producer_responsibility": "Acceptance", "conclusion": "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "acceptance.json"), envelope, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "EV-ACC-001",
		Kind:             "acceptance",
		Path:             "acceptance.json",
		ProducedBy:       []string{"agent-1"},
		ResponsibilityID: "Acceptance",
		Validator:        testCandidateValidator(),
	})
	if err == nil {
		t.Error("incomplete acceptance evidence must be rejected before mutation")
	} else {
		for _, want := range []string{"audit_manifest_path", "S10", "manifest"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not identify the original missing report reference %q", err, want)
			}
		}
	}
	state, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	journal, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(state) != string(stateBefore) || string(journal) != string(journalBefore) {
		t.Fatal("missing professional report changed Runtime or journal")
	}
}

func writeExampleRuntime(t *testing.T, path string) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Regression for the 2026-08-28 S10 walkthrough defect B: registering an S10
// artifact without --review-round used to persist review_round=null, which the
// s10 board immediately reports as a stale binding. The envelope already
// carries the round, so RecordEvidence must inherit it.
func TestRecordEvidenceS10KeepsOriginalPlanWithoutReviewRound(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)

	// Use the original Plan responsibility without an obsolete review round.
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		t.Fatal(err)
	}
	current["runtime_id"] = "loop-test"
	current["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-s10", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	stateData, err = json.MarshalIndent(current, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(stateData, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_type": "acceptance",
		"runtime_id": "loop-test", "baseline_generation": 1, "review_plan_id": "review-plan-s10",
		"coverage_inventory": []any{map[string]any{
			"id": "REQ-AC-1", "category": "requirement",
			"source_refs": []any{"REV-001.md#ac"}, "expected": "e", "oracle": "o",
			"owner": "Acceptance", "evidence_refs": []any{"test://original-observation"}, "disposition": "pass",
		}, map[string]any{
			"id": "CON-N-A", "category": "contract",
			"source_refs": []any{"REV-001.md#contract"}, "expected": "n/a scope",
			"oracle": "scope decision recorded", "owner": "Acceptance",
			"evidence_refs": []any{}, "disposition": "not_applicable",
			"na_reason": "no contract surface in this REQ",
		}, map[string]any{
			"id": "PATH-N-A", "category": "changed_path",
			"source_refs": []any{"REV-001.md"}, "expected": "n/a path",
			"oracle": "recorded decision", "owner": "Evidence Integrity",
			"evidence_refs": []any{}, "disposition": "not_applicable",
			"na_reason": "docs-only change",
		}},
		"counterevidence": []any{
			map[string]any{
				"id": "CE-1", "inventory_id": "REQ-AC-1", "question": "q?",
				"evidence_refs": []any{"test://original-observation"}, "outcome": "pass",
			},
			map[string]any{
				"id": "CE-2", "inventory_id": "CON-N-A", "question": "why n/a?",
				"evidence_refs": []any{"test://original-observation"}, "outcome": "not_applicable",
			},
			map[string]any{
				"id": "CE-3", "inventory_id": "PATH-N-A", "question": "which paths moved?",
				"evidence_refs": []any{"test://original-observation"}, "outcome": "pass",
			},
		},
		"risks": []any{}, "technical_debt": []any{}, "blocking_findings": []any{},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1,
			"audit_area_coverage": 1, "unknown_count": 0, "unsupported_pass_count": 0,
			"unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0,
		},
	}
	manifestData, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "m.json"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schema_version": "1.0.0", "kind": "acceptance_record",
		"evidence_id": "acc-record-r2", "runtime_id": "loop-test",
		"baseline_generation": 1, "review_plan_id": "review-plan-s10",
		"producer_agent_id": "orchestrator", "producer_responsibility": "Acceptance",
		"conclusion": "pass", "created_at": "2026-07-29T00:00:00Z",
		"audit_manifest_path": "m.json",
	}
	envData, _ := json.Marshal(envelope)
	envPath := filepath.Join(root, "acc-env.json")
	if err := os.WriteFile(envPath, envData, 0o644); err != nil {
		t.Fatal(err)
	}

	next, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "acc-record-r2",
		Kind:             "acceptance",
		Path:             "acc-env.json",
		ProducedBy:       []string{"orchestrator"},
		ResponsibilityID: "Acceptance",
		Validator:        testCandidateValidator(),
	})
	if err != nil {
		t.Fatalf("RecordEvidence(acceptance) failed: %v", err)
	}
	found := false
	for _, raw := range next.State["evidence"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == "acc-record-r2" {
			if item["review_plan_id"] != "review-plan-s10" {
				t.Fatalf("acceptance lost its original Plan: %v", item)
			}
			if _, exists := item["review_round"]; exists {
				t.Fatalf("acceptance recorded obsolete review round: %v", item)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("acceptance evidence entry not found after registration")
	}
	for _, field := range []string{"runtime_id", "baseline_generation", "coverage_inventory"} {
		t.Run("report association "+field, func(t *testing.T) {
			original := manifest[field]
			defer func() { manifest[field] = original }()
			wantError := "referenced report association"
			if field == "coverage_inventory" {
				manifest[field] = "not an array of professional dispositions"
				wantError = "evidence manifest is invalid"
			} else if field == "runtime_id" {
				manifest[field] = "loop-other"
			} else {
				manifest[field] = 7
			}
			changed, _ := json.Marshal(manifest)
			if err := os.WriteFile(filepath.Join(root, "m.json"), changed, 0644); err != nil {
				t.Fatal(err)
			}
			stateBefore, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			journalBefore, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			// A fresh registration must reach the report association guard rather
			// than being rejected by the already-recorded envelope's duplicate ID.
			id := "acc-record-r2-" + field
			envelope["evidence_id"] = id
			freshEnvelope, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(envPath, freshEnvelope, 0644); err != nil {
				t.Fatal(err)
			}
			_, err = runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
				ExpectedRevision: -1, ID: id, Kind: "acceptance", Path: "acc-env.json",
				ProducedBy: []string{"orchestrator"}, ResponsibilityID: "Acceptance", Validator: testCandidateValidator(),
			})
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Errorf("a matching envelope accepted an invalid referenced report (%s): %v", wantError, err)
			}
			stateAfter, _ := os.ReadFile(statePath)
			journalAfter, _ := os.ReadFile(journalPath)
			if string(stateBefore) != string(stateAfter) || string(journalBefore) != string(journalAfter) {
				t.Error("mismatched report changed Runtime or journal")
			}
		})
	}
}
