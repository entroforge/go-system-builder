package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	})
	if err != nil {
		t.Fatalf("RecordEvidence change impact failed: %v", err)
	}
	evidence, _ := next.State["evidence"].([]any)
	if len(evidence) != 1 || evidence[0].(map[string]any)["kind"] != "change_impact" {
		t.Fatalf("unexpected change impact evidence: %#v", evidence)
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
