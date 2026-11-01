package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordedAuditUsesAcceptedReportWithoutAuthorDraftOrHistoricalHash(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "s10", "loop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "receipt.json"), []byte(`{"audit_manifest_path":"s10/loop/accepted.json","audit_questionnaire_path":"s10/loop/author.json","conclusion":"review_required"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "accepted.json"), []byte(`{"outcome":"review_required"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"review":   map[string]any{"plan": map[string]any{"plan_id": "plan-1"}},
		"evidence": []any{map[string]any{"id": "receipt-1", "kind": "acceptance", "path": "s10/loop/receipt.json", "sha256": "obsolete-wrong-hash", "status": "valid", "baseline_generation": 1, "review_plan_id": "plan-1"}},
	}
	id, path, actual, err := recordedAuditAnswer(root, state, "acceptance", "s10/loop/author.json", nil)
	if err != nil || id != "receipt-1" || path != "s10/loop/receipt.json" || string(actual) != `{"outcome":"review_required"}` {
		t.Fatalf("original accepted report not recovered: %q %q %q %v", id, path, actual, err)
	}
	if !receivedAuditReviewPending(root, state, "s10/loop/author.json") {
		t.Fatal("accepted review_required must remain pending after author draft disappears")
	}
	id, _, _, err = recordedAuditAnswer(root, state, "acceptance", "s10/loop/author.json", []byte(`{"outcome":"pass"}`))
	if err != nil || id != "" {
		t.Fatalf("new professional answer must not replay the old one: %q %v", id, err)
	}
}
