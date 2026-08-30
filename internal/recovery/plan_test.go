package recovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlanHashIsDeterministicAndExcludesAbsoluteRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	for _, root := range []string{rootA, rootB} {
		writeRecoveryREQ(t, root, "docs/requirements/REQ-020.md", "locked", "v2.0.0")
		writeRecoveryFile(t, root, ".claude/loop-state.json", []byte{0xef, 0xbb, 0xbf, '{', 'x'})
		writeRecoveryFile(t, root, "docs/design/ARCH-020.md", []byte("same design"))
	}

	inventoryA, err := Inspect(rootA, "docs/requirements/REQ-020.md")
	if err != nil {
		t.Fatalf("Inspect A returned error: %v", err)
	}
	inventoryB, err := Inspect(rootB, "docs/requirements/REQ-020.md")
	if err != nil {
		t.Fatalf("Inspect B returned error: %v", err)
	}
	planA, err := BuildPlan(inventoryA)
	if err != nil {
		t.Fatalf("BuildPlan A returned error: %v", err)
	}
	planB, err := BuildPlan(inventoryB)
	if err != nil {
		t.Fatalf("BuildPlan B returned error: %v", err)
	}
	if planA.PlanSHA256 == "" || planA.PlanSHA256 != planB.PlanSHA256 {
		t.Fatalf("plan hashes A=%q B=%q, want equal non-empty hashes", planA.PlanSHA256, planB.PlanSHA256)
	}
	encoded, err := json.Marshal(planA)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if strings.Contains(string(encoded), rootA) || strings.Contains(string(encoded), rootB) {
		t.Fatalf("plan contains an absolute inspection root: %s", encoded)
	}
}

func TestBuildPlanIsIdempotentForEquivalentInputOrder(t *testing.T) {
	root := t.TempDir()
	writeRecoveryREQ(t, root, "docs/requirements/REQ-021.md", "locked", "v1.0.0")
	writeRecoveryFile(t, root, "docs/design/ARCH-021.md", []byte("design"))
	writeRecoveryFile(t, root, ".claude/loop-state.json", []byte("broken"))

	inventory, err := Inspect(root, "docs/requirements/REQ-021.md")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	first, err := BuildPlan(inventory)
	if err != nil {
		t.Fatalf("BuildPlan first returned error: %v", err)
	}
	for left, right := 0, len(inventory.Inputs)-1; left < right; left, right = left+1, right-1 {
		inventory.Inputs[left], inventory.Inputs[right] = inventory.Inputs[right], inventory.Inputs[left]
	}
	second, err := BuildPlan(inventory)
	if err != nil {
		t.Fatalf("BuildPlan second returned error: %v", err)
	}
	if first.PlanSHA256 != second.PlanSHA256 {
		t.Fatalf("equivalent input order changed plan hash: %q != %q", first.PlanSHA256, second.PlanSHA256)
	}
}

func TestBuildPlanHashChangesWhenAnInputChanges(t *testing.T) {
	root := t.TempDir()
	writeRecoveryREQ(t, root, "docs/requirements/REQ-022.md", "locked", "v1.0.0")
	designPath := filepath.Join(root, "docs", "design", "ARCH-022.md")
	writeRecoveryFile(t, root, "docs/design/ARCH-022.md", []byte("design-v1"))

	inventory, err := Inspect(root, "docs/requirements/REQ-022.md")
	if err != nil {
		t.Fatalf("Inspect first returned error: %v", err)
	}
	first, err := BuildPlan(inventory)
	if err != nil {
		t.Fatalf("BuildPlan first returned error: %v", err)
	}
	if err := os.WriteFile(designPath, []byte("design-v2"), 0o644); err != nil {
		t.Fatalf("change design: %v", err)
	}
	inventory, err = Inspect(root, "docs/requirements/REQ-022.md")
	if err != nil {
		t.Fatalf("Inspect second returned error: %v", err)
	}
	second, err := BuildPlan(inventory)
	if err != nil {
		t.Fatalf("BuildPlan second returned error: %v", err)
	}
	if first.PlanSHA256 == second.PlanSHA256 {
		t.Fatalf("changed input kept plan hash %q", first.PlanSHA256)
	}
}

func TestBuildPlanForReplayCursorFingerprintsFormalProgress(t *testing.T) {
	root := t.TempDir()
	writeRecoveryREQ(t, root, "docs/requirements/REQ-023.md", "locked", "v1.0.0")
	inventory, err := Inspect(root, "docs/requirements/REQ-023.md")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlanForCursor(inventory, "planning.tasks", PlanConfidenceFormalReplay)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetCursor != "planning.tasks" || plan.Confidence != PlanConfidenceFormalReplay {
		t.Fatalf("unexpected replay plan: %#v", plan)
	}
	conservative, err := BuildPlan(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanSHA256 == conservative.PlanSHA256 {
		t.Fatal("formally replayed cursor must change the plan fingerprint")
	}
}
