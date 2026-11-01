package transition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS7ExitReadsOriginalResponsibilitiesWithoutCleanProof(t *testing.T) {
	state := map[string]any{"review": map[string]any{
		"plan":        map[string]any{"plan_id": "review-plan-current", "path": "review/plan.json", "status": "running"},
		"claims":      map[string]any{"claim-a": map[string]any{"applicability": "required", "assignment_id": "assignment-a"}},
		"assignments": map[string]any{"assignment-a": map[string]any{"status": "consumed", "agent_id": "reviewer", "claim_ids": []any{"claim-a"}, "result_receipt": map[string]any{"packet_id": "packet-a"}}},
	}, "entities": map[string]any{"findings": []any{}}}
	if err := guardCleanRoundValidFn(state, nil); err != nil {
		t.Fatalf("original accepted conclusions rejected: %v", err)
	}
	state["entities"].(map[string]any)["findings"] = []any{map[string]any{"finding_id": "finding-a", "review_plan_id": "review-plan-current"}}
	if err := guardCleanRoundValidFn(state, nil); err == nil {
		t.Fatal("actual Finding allowed no-Finding exit")
	}
}

func TestBuilderReportSlotDoesNotTrustCompletionKindAlone(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"changed_paths":[]}`)
	if err := os.WriteFile(filepath.Join(root, "result.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"id": "result", "kind": "completion_report", "status": "valid", "baseline_generation": 1, "path": "result.json"}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "evidence": []any{row}}
	if err := validateCurrentEvidenceWithFiles(root, state, "builder_report_record", "result", nil); err == nil || !strings.Contains(err.Error(), "canonical builder report source") {
		t.Fatalf("completion kind granted original builder report authority without canonical source: %v", err)
	}
	row["kind"] = "builder_report"
	if err := validateCurrentEvidenceWithFiles(root, state, "builder_report_record", "result", nil); err != nil {
		t.Fatalf("original legacy builder report binding changed: %v", err)
	}
}

func TestCanonicalTR006RequiresActualOriginalResponsibility(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-canonical", "baseline": map[string]any{"generation": 1},
		"entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-canonical", "baseline_generation": 1}}},
	}
	err := validateRequest(t.TempDir(), state, TransitionSpec{ID: "TR-006"}, Request{Actor: "orchestrator"})
	if err == nil {
		t.Fatal("TR-006 accepted no current Main execution responsibility")
	}
}
