package runtime_test

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/schema"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceRefreshPreservesBaselineAndHistoricalEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "loop-state.json")
	journal := filepath.Join(root, "loop-events.jsonl")
	raw, e := schema.ReadAsset("loop-state.example.json")
	if e != nil {
		t.Fatal(e)
	}
	var state map[string]any
	_ = json.Unmarshal(raw, &state)
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["last_transition"] = nil
	state["baseline"].(map[string]any)["generation"] = 1
	state["review"].(map[string]any)["round"] = 1
	stale := strings.Repeat("a", 64)
	state["bound_req"].(map[string]any)["sha256"] = stale
	row := func(id, kind string, gen int) map[string]any {
		return map[string]any{"id": id, "kind": kind, "path": "evidence.json", "sha256": stale, "status": "valid", "baseline_generation": gen, "review_round": 1, "produced_by": []string{"reviewer"}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": nil, "scope_refs": []string{}}
	}
	child := row("child", "document_review", 1)
	child["path"] = "child.json"
	state["evidence"] = []any{row("fresh", "document_review", 1), row("old", "document_review", 0), row("frozen", "clean_round", 1), child}
	raw, _ = json.Marshal(state)
	if e = os.WriteFile(path, raw, 0644); e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(filepath.Join(root, "evidence.json"), []byte(`{"evidence":[{"path":"child.json","sha256":"old"}],"subject_refs":[{"path":"child.json","sha256":"frozen"}]}`), 0644)
	_ = os.WriteFile(filepath.Join(root, "child.json"), []byte("merged child evidence"), 0644)
	writer := testWriter(path, journal)
	result, e := writer.RefreshEvidenceFingerprints(root, map[string]bool{"document_review": true}, func(p string) ([]byte, error) { return os.ReadFile(filepath.Join(root, p)) }, func(string) bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Updated) != 2 {
		t.Fatalf("updated=%v", result.Updated)
	}
	snap, e := writer.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	rows := snap.State["evidence"].([]any)
	if rows[0].(map[string]any)["sha256"] == stale {
		t.Fatal("current evidence not refreshed")
	}
	for _, index := range []int{1, 2} {
		if rows[index].(map[string]any)["sha256"] != stale {
			t.Fatal("historical/frozen evidence changed")
		}
	}
	if snap.State["bound_req"].(map[string]any)["sha256"] != stale {
		t.Fatal("REQ hash laundered")
	}
	result, e = writer.RefreshEvidenceFingerprints(root, map[string]bool{"document_review": true}, func(p string) ([]byte, error) { return os.ReadFile(filepath.Join(root, p)) }, func(string) bool { return true })
	if e != nil || len(result.Updated) != 0 {
		t.Fatalf("non-idempotent refresh: %v %v", result, e)
	}
}
