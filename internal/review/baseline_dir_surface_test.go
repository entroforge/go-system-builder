// baseline_dir_surface_test.go guards the S7 baseline projection against
// completion envelopes whose changed_paths contain an evidence-staging
// directory (with or without a trailing slash) alongside real files. The
// directory must be dropped from the changed-surface denominator so plan
// registration stays satisfiable, while missing-on-disk entries keep the
// existing fail-closed diagnostics.
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterFreezableChangedPathsDropsDirectories(t *testing.T) {
	root := t.TempDir()
	handler := filepath.Join(root, "internal", "api", "handler.go")
	if err := os.MkdirAll(filepath.Dir(handler), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handler, []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude", "evidence", "REQ-42"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := filterFreezableChangedPaths(root, []string{
		".claude/evidence/REQ-42", "internal/api/handler.go", "missing/on/disk.go",
	})
	want := []string{"internal/api/handler.go", "missing/on/disk.go"}
	if len(got) != len(want) {
		t.Fatalf("filterFreezableChangedPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterFreezableChangedPaths = %v, want %v", got, want)
		}
	}
}

func TestDraftPlanSkipsDirectoryChangedPathFromEnvelope(t *testing.T) {
	root := t.TempDir()
	state := baseDraftState(t)
	state["documents"] = []any{taskFixture("TASK-1", "internal/example/service.go")}
	handler := filepath.Join(root, "internal", "api", "handler.go")
	if err := os.MkdirAll(filepath.Dir(handler), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handler, []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude", "evidence", "REQ-42"), 0o755); err != nil {
		t.Fatal(err)
	}
	completionRel := ".claude/evidence/REQ-42/completion.json"
	completion := []byte(`{"kind":"completion_report","changed_paths":[".claude/evidence/REQ-42/","internal/api/handler.go"],"reviewed_paths":[]}` + "\n")
	completionPath := filepath.Join(root, filepath.FromSlash(completionRel))
	if err := os.WriteFile(completionPath, completion, 0o644); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{map[string]any{
		"id": "completion-1", "kind": "completion_report", "path": completionRel,
		"sha256": sha256Of(completion), "status": "valid", "baseline_generation": 1,
		"scope_refs": []any{},
	}}

	plan, notes := DraftPlanForRoot(root, state, 1)
	if plan == nil {
		t.Fatal("DraftPlanForRoot returned nil plan")
	}
	for _, note := range notes {
		if strings.Contains(note, "cannot be frozen") {
			t.Fatalf("directory entry leaked into projection diagnostics: %v", notes)
		}
	}
	for _, item := range plan.CoverageInventory {
		if strings.Contains(item.SourceRef, "REQ-42") {
			t.Fatalf("directory entry leaked into coverage_inventory: %+v", plan.CoverageInventory)
		}
	}
	if len(plan.CoverageInventory) != 1 || plan.CoverageInventory[0].SourceRef != "internal/api/handler.go" {
		t.Fatalf("coverage_inventory = %+v, want only the file surface", plan.CoverageInventory)
	}
	for _, subject := range plan.FrozenSubjects {
		if strings.Contains(subject.Path, "REQ-42") {
			t.Fatalf("directory entry leaked into frozen_subjects: %+v", plan.FrozenSubjects)
		}
	}
}
