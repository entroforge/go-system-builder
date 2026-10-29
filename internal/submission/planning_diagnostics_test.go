package submission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanningDiagnosticIsScopedNonAuthoritativeDisplay(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{"runtime_id": "runtime-a", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 3}}
	RecordPlanningDiagnostic(root, state, "preparation", "missing committed docs/design")
	if got := PlanningDiagnostic(root, state, "preparation"); !strings.Contains(got, "Last preparation attempt: missing committed docs/design") {
		t.Fatal(got)
	}
	for _, change := range []func(){
		func() { state["runtime_id"] = "runtime-b" },
		func() { state["runtime_id"] = "runtime-a"; state["baseline"].(map[string]any)["generation"] = 2 },
		func() {
			state["baseline"].(map[string]any)["generation"] = 1
			state["lifecycle"].(map[string]any)["phase_revision"] = 4
		},
	} {
		change()
		if got := PlanningDiagnostic(root, state, "preparation"); got != "" {
			t.Fatalf("new responsibility inherited diagnostic: %s", got)
		}
	}
	state["lifecycle"].(map[string]any)["phase_revision"] = 3
	path := filepath.Join(root, planningDiagnosticPath(state, "preparation"))
	if err := os.WriteFile(path, []byte("broken cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := PlanningDiagnostic(root, state, "preparation"); got != "" {
		t.Fatalf("damaged display became a fact: %s", got)
	}
	RecordPlanningDiagnostic(root, state, "preparation", "new original error")
	RecordPlanningDiagnostic(root, state, "preparation", "")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("successful attempt did not clear failure: %v", err)
	}
}
