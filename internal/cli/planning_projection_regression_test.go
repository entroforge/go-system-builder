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
	if !hasProjectionMissing(c.Missing, "locked_contract_set") {
		t.Fatalf("S3 must keep its own locked contract requirement, missing=%v", c.Missing)
	}
	for _, m := range c.Missing {
		if m == "task_batch" {
			t.Fatal("foreign contract advanced projection")
		}
	}
	if len(c.Missing) != 2 || c.Missing[1] != "planning_contract_record" {
		t.Fatalf("missing=%v", c.Missing)
	}
}

func TestS2ArchitectureArtifactWithoutAcceptedDesignKeepsS2Projection(t *testing.T) {
	root := t.TempDir()
	architectureDir := filepath.Join(root, "docs/architecture")
	if err := os.MkdirAll(architectureDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(architectureDir, "ARCHITECTURE-001.md"), []byte("# Architecture\n> 状态：locked\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// No accepted submission/evidence is present in this state. The locked
	// file is only a design artifact; it does not move projection into S3/S4.
	c := contractFor("S2", map[string]any{"bound_req": map[string]any{"id": "REQ-001"}}, root)
	if !hasProjectionMissing(c.Missing, "architecture_record") {
		t.Fatalf("S2 projection lost its local target, missing=%v", c.Missing)
	}
	for _, missing := range c.Missing {
		if missing == "contract_set" || missing == "locked_contract_set" || missing == "task_batch" || missing == "complete_task_batch" {
			t.Fatalf("S2 architecture artifact projected a later-stage requirement %q: %v", missing, c.Missing)
		}
	}
}

func hasProjectionMissing(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
