package review

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPR8ProductDirectoryMustNotDisappear(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/feature.go"), []byte("package feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"kind":"completion_report","changed_paths":["src/"]}`)
	if err := os.WriteFile(filepath.Join(root, "completion.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	state := baseDraftState(t)
	state["evidence"] = []any{map[string]any{"id": "completion-pr8", "kind": "completion_report", "path": "completion.json", "sha256": sha256Of(data), "status": "valid", "baseline_generation": 1, "scope_refs": []any{}}}
	p := buildS7BaselineProjection(root, state)
	t.Logf("ChangedPaths=%v Diagnostics=%v", p.ChangedPaths, p.Diagnostics)
	if len(p.ChangedPaths) == 0 && len(p.Diagnostics) == 0 {
		t.Fatal("product directory silently removed from S7 projection")
	}
}
