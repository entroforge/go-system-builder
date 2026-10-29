package buildsurface

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/productbaseline"
)

func TestPinnedCanonicalDeclarationCannotBecomeLegacy(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"product_baseline":{}}`)
	if err := os.WriteFile(filepath.Join(root, "plan.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"review": map[string]any{"plan": map[string]any{
		"plan_id": "plan", "path": "plan.json", "sha256": hashBytes(data),
	}}}
	if canonical, err := SelectCanonical(root, state); err != nil || !canonical {
		t.Fatalf("declared canonical plan downgraded: canonical=%t err=%v", canonical, err)
	}
	if _, err := ReadRegistered(root, state); err == nil {
		t.Fatal("declared snapshot with absent original registry accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectCanonical(root, state); err == nil {
		t.Fatal("changed pinned plan downgraded to legacy")
	}
	state["review"].(map[string]any)["plan"].(map[string]any)["sha256"] = hashBytes([]byte(`{}`))
	if canonical, err := SelectCanonical(root, state); err != nil || canonical {
		t.Fatalf("readable legacy plan changed path: canonical=%t err=%v", canonical, err)
	}
}

func TestCurrentTASKDenominatorRequiresOriginalLockProof(t *testing.T) {
	locked := []productbaseline.Task{{TaskID: "TASK-1", Path: "tasks/one.md", Version: "v1", SHA256: strings.Repeat("a", 64)}}
	current := []Task{{ID: "TASK-1", Path: "tasks/one.md", Version: "v1", SHA256: strings.Repeat("a", 64)}}
	if err := verifyLockedTaskSet(locked, current); err != nil {
		t.Fatal(err)
	}
	for _, altered := range [][]Task{nil, {{ID: "TASK-2", Path: "tasks/one.md", Version: "v1", SHA256: strings.Repeat("a", 64)}}, {{ID: "TASK-1", Path: "tasks/one.md", Version: "v2", SHA256: strings.Repeat("b", 64)}}, append(append([]Task{}, current...), current[0])} {
		if err := verifyLockedTaskSet(locked, altered); err == nil {
			t.Fatalf("unproven lock-set change accepted: %+v", altered)
		}
	}
}

func TestCanonicalCompletionMarkerCannotLoseStrictSelection(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"assignment_record":null}`)
	if err := os.WriteFile(filepath.Join(root, "result.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "evidence": []any{map[string]any{
		"kind": "completion_report", "status": "valid", "baseline_generation": 1, "path": "result.json", "sha256": hashBytes(data),
	}}}
	if canonical, err := SelectCanonical(root, state); err != nil || !canonical {
		t.Fatalf("registered canonical completion downgraded after registry loss: canonical=%t err=%v", canonical, err)
	}
	if _, err := Read(root, state); err == nil {
		t.Fatal("completion marker conferred missing Assignment authority")
	}
}

func TestHistoricalCanonicalPlanDoesNotSelectNewLegacyGeneration(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"baseline_generation":1,"product_baseline":{}}`)
	if err := os.WriteFile(filepath.Join(root, "old-plan.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"baseline": map[string]any{"generation": 2}, "review": map[string]any{"plan": map[string]any{
		"plan_id": "old-plan", "path": "old-plan.json", "sha256": hashBytes(data),
	}}}
	if canonical, err := SelectCanonical(root, state); err != nil || canonical {
		t.Fatalf("historical Plan selected the new generation: canonical=%t err=%v", canonical, err)
	}
}
