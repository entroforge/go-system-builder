package transition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedEvidenceKindOnlyPauseRecord(t *testing.T) {
	if !generatedEvidenceKind("pause_record") {
		t.Fatal("pause_record must be a generated evidence kind")
	}
	if generatedEvidenceKind("bug_batch_record") {
		t.Fatal("bug_batch_record must not be a generated evidence kind")
	}
}

func TestBugBatchRecordValidatesAsCurrentEvidence(t *testing.T) {
	root := t.TempDir()
	evidencePath := "evidence/bug-batch.json"
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-bug-batch",
		"kind":                    "bug",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "orchestrator-1",
		"producer_responsibility": "Orchestrator",
		"subject_refs":            []any{},
		"conclusion":              "rejected",
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, evidencePath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"review":   map[string]any{"round": 0},
		"evidence": []any{
			map[string]any{
				"id": "ev-bug-batch", "kind": "bug", "path": evidencePath,
				"sha256": SHA256(data), "status": "valid", "baseline_generation": 1,
				"review_round": 0,
			},
		},
	}
	if err := validateCurrentEvidence(root, state, "bug_batch_record", "ev-bug-batch"); err != nil {
		t.Fatalf("indexed bug evidence must satisfy bug_batch_record without bug_id param: %v", err)
	}
}

func TestTeamManifestRecordRejectsDocumentReviewOnly(t *testing.T) {
	allowed := allowedEvidenceKinds("team_manifest_record")
	if contains(allowed, "document_review") {
		t.Fatalf("team_manifest_record must not accept document_review, got %v", allowed)
	}
	for _, kind := range []string{"builder_report", "team_manifest"} {
		if !contains(allowed, kind) {
			t.Fatalf("team_manifest_record must accept %s, got %v", kind, allowed)
		}
	}
}

func TestBuilderReportRecordAcceptsShortKinds(t *testing.T) {
	allowed := allowedEvidenceKinds("builder_report_record")
	for _, kind := range []string{"builder_report", "agent_completion"} {
		if !contains(allowed, kind) {
			t.Fatalf("builder_report_record must accept %s, got %v", kind, allowed)
		}
	}
}

func TestPlanningCompleteAcceptsRuntimeContractDocuments(t *testing.T) {
	root := t.TempDir()
	contractPath := "docs/contracts/BE-039-loop-controller.md"
	taskPath := "docs/tasks/TASK-039-01-loop-definition.md"
	contractData := []byte("# BE-039\n> Status: locked\n")
	taskData := []byte("# TASK-039-01\n> Status: complete\n")
	for _, pair := range []struct {
		path string
		data []byte
	}{
		{contractPath, contractData},
		{taskPath, taskData},
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, pair.path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pair.path), pair.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{
		"root":     root,
		"baseline": map[string]any{"generation": 1},
		"documents": []any{
			map[string]any{
				"id": "BE-039", "kind": "contract", "path": contractPath, "version": "v1.0.2",
				"sha256": SHA256(contractData), "status": "locked", "generation": 1,
			},
			map[string]any{
				"id": "TASK-039-01", "kind": "task", "path": taskPath, "version": "v1.0.2",
				"sha256": SHA256(taskData), "status": "complete", "generation": 1,
			},
		},
	}
	if err := guardPlanningCompleteFn(state, nil); err != nil {
		t.Fatalf("planning_complete must accept runtime contract/task documents: %v", err)
	}
}
