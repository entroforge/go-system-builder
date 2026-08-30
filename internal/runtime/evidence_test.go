package runtime_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRecordEvidenceAppendsFingerprintedEvidenceWithCAS(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	if err := os.WriteFile(filepath.Join(root, "REV-001.md"), []byte("document pass\n"), 0o644); err != nil {
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
	if entry["sha256"] == "" {
		t.Fatal("evidence fingerprint must be recorded")
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

func TestRecordEvidenceRejectsIncompleteS10ArtifactBeforeMutation(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	envelope := []byte(`{"schema_version":"1.0.0","evidence_id":"EV-ACC-001","kind":"acceptance","runtime_id":"loop-example","baseline_generation":1,"producer_agent_id":"agent-1","producer_responsibility":"Acceptance","conclusion":"pass"}`)
	if err := os.WriteFile(filepath.Join(root, "acceptance.json"), envelope, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "EV-ACC-001",
		Kind:             "acceptance",
		Path:             "acceptance.json",
		ProducedBy:       []string{"agent-1"},
		ResponsibilityID: "Acceptance",
		Validator:        testCandidateValidator(),
	})
	if err == nil {
		t.Fatal("incomplete acceptance evidence must be rejected before mutation")
	}
	for _, want := range []string{"audit_manifest_path", "S10", "manifest"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not identify recovery field %q", err, want)
		}
	}
	state, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(state), `"revision": 1`) {
		t.Fatalf("rejected S10 evidence must not mutate runtime: %s", state)
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
func TestRecordEvidenceS10InheritsReviewRoundFromEnvelope(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)

	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_type": "acceptance",
		"runtime_id": "loop-test", "baseline_generation": 1, "review_round": 2,
		"coverage_inventory": []any{map[string]any{
			"id": "REQ-AC-1", "category": "requirement",
			"source_refs": []any{"REV-001.md#ac"}, "expected": "e", "oracle": "o",
			"owner": "Acceptance", "evidence_refs": []any{"EV-CLEAN"}, "disposition": "pass",
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
				"evidence_refs": []any{"EV-CLEAN"}, "outcome": "pass",
			},
			map[string]any{
				"id": "CE-2", "inventory_id": "CON-N-A", "question": "why n/a?",
				"evidence_refs": []any{"EV-CLEAN"}, "outcome": "not_applicable",
			},
			map[string]any{
				"id": "CE-3", "inventory_id": "PATH-N-A", "question": "which paths moved?",
				"evidence_refs": []any{"EV-CLEAN"}, "outcome": "pass",
			},
		},
		"risks": []any{}, "technical_debt": []any{}, "blocking_findings": []any{},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1,
			"audit_area_coverage": 1, "unknown_count": 0, "unsupported_pass_count": 0,
			"unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0,
		},
	}
	clean := map[string]any{
		"schema_version": "1.0.0", "clean_round_id": "clean-round-r2",
		"conclusion": "pass", "evaluated_at": "2026-07-29T00:00:00Z",
		"evaluated_by": "round-consumer", "evidence_id": "EV-CLEAN",
		"kind": "clean_round", "producer_agent_id": "round-consumer",
		"producer_responsibility": "Clean Round Evaluator",
		"review_round":            2, "runtime_id": "loop-test",
	}
	manifestData, _ := json.Marshal(manifest)
	cleanData, _ := json.Marshal(clean)
	for name, data := range map[string][]byte{"m.json": manifestData, "clean.json": cleanData} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	envelope := map[string]any{
		"schema_version": "1.0.0", "kind": "acceptance_record",
		"evidence_id": "acc-record-r2", "runtime_id": "loop-test",
		"baseline_generation": 1, "review_round": 2,
		"producer_agent_id": "orchestrator", "producer_responsibility": "Acceptance",
		"conclusion": "pass", "created_at": "2026-07-29T00:00:00Z",
		"audit_manifest_path":   "m.json",
		"audit_manifest_sha256": sha256ForTest(manifestData),
	}
	envData, _ := json.Marshal(envelope)
	envPath := filepath.Join(root, "acc-env.json")
	if err := os.WriteFile(envPath, envData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Register the referenced clean-round evidence first so the manifest's
	// evidence_refs resolve during the S10 artifact validation.
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 1,
		ID:               "EV-CLEAN",
		Kind:             "clean_round",
		Path:             "clean.json",
		ProducedBy:       []string{"round-consumer"},
		ResponsibilityID: "Clean Round Evaluator",
		ReviewRound:      func() *int { v := 2; return &v }(),
		Validator:        testCandidateValidator(),
	}); err != nil {
		t.Fatalf("RecordEvidence(clean) failed: %v", err)
	}

	next, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: 2,
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
	for _, raw := range next.State["evidence"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == "acc-record-r2" {
			got := fmt.Sprintf("%v", item["review_round"])
			if got != "2" {
				t.Fatalf("acceptance entry review_round = %s, want 2 (inherited from envelope)", got)
			}
			return
		}
	}
	t.Fatalf("acceptance evidence entry not found after registration")
}

func sha256ForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
