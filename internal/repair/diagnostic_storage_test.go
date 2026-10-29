package repair

import (
	"strings"
	"testing"
)

func TestRepairBaselineSeparatesOriginalDiagnosticFromProductNeighbor(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	path := ".claude/diagnostics/planning/" + strings.Repeat("a", 64) + ".json"
	boundaryWrite(t, root, path, `{"message":"original interrupted attempt","at":"2026-10-25T12:00:00Z"}`)
	baseline, _, err := captureRepositoryBaseline(root)
	if err != nil || len(baseline) != 0 {
		t.Fatalf("display file entered baseline: %v %v", baseline, err)
	}
	neighbor := ".claude/diagnostics/planning/product.go"
	boundaryWrite(t, root, neighbor, "package product\n")
	baseline, _, err = captureRepositoryBaseline(root)
	if err != nil || len(baseline) != 1 || baseline[0].Path != neighbor {
		t.Fatalf("product neighbor escaped baseline: %v %v", baseline, err)
	}
}
