package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestS3DoesNotTreatForeignLockedContractAsTaskReadiness(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/dev/contracts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/dev/contracts/CONTRACTS-052.md"), []byte("# Contract\n> 状态：locked\nREQ-052\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := contractFor("S3", map[string]any{"bound_req": map[string]any{"id": "REQ-053"}}, root)
	for _, m := range c.Missing {
		if m == "task_batch" {
			t.Fatal("foreign contract advanced projection")
		}
	}
	if len(c.Missing) != 2 || c.Missing[1] != "planning_contract_record" {
		t.Fatalf("missing=%v", c.Missing)
	}
}
