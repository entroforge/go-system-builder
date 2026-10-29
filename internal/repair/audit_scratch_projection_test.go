package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairRecognizesOnlyExactAuditIndexScratchFiles(t *testing.T) {
	for _, path := range []string{".claude/hook-audit-index-0.scratch", ".claude/hook-audit-index-1234567890.scratch"} {
		if !isControlPlanePath(path, false) {
			t.Fatalf("audit cache scratch entered product baseline: %s", path)
		}
		if isControlPlanePath(path, true) {
			t.Fatalf("directory acquired file cache exemption: %s", path)
		}
	}
	for _, path := range []string{
		".claude/hook-audit-index-.scratch", ".claude/hook-audit-index--123.scratch", ".claude/hook-audit-index-abc.scratch",
		".claude/hook-audit-index-123.scratch.go", ".claude/hook-audit-index-123.scratch/product.go", ".claude/nested/hook-audit-index-123.scratch",
		"hook-audit-index-123.scratch", ".claude/hooks/hook-audit-index-123.scratch", ".claude/hook-audit-index-１２３.scratch",
	} {
		if isControlPlanePath(path, false) {
			t.Fatalf("neighbor acquired audit cache exemption: %s", path)
		}
	}
	root := t.TempDir()
	boundaryGit(t, root, "init")
	rel := ".claude/hook-audit-index-123.scratch"
	path := filepath.Join(root, filepath.FromSlash(rel))
	boundaryWrite(t, root, rel, "partially written cache")
	before, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("actual audit scratch entered baseline: %+v", before)
	}
	neighbor := rel + ".go"
	boundaryWrite(t, root, neighbor, "package product\n")
	changes, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != neighbor {
		t.Fatalf("cache ownership hid neighboring product file: %+v", changes)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if isControlPlanePath(rel, false, root) {
		t.Fatal("root-aware file classification accepted an actual directory")
	}
}
