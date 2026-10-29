package transition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanProductConsumersRejectMissingPlanAtOriginalBoundary(t *testing.T) {
	for _, id := range []string{"TR-009", "TR-015", "TR-017"} {
		t.Run(id, func(t *testing.T) {
			state := map[string]any{
				"runtime_id": "loop-canonical", "baseline": map[string]any{"generation": 1},
				"entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-canonical", "baseline_generation": 1}}},
				"review":   map[string]any{"round": 1, "clean_round": map[string]any{"status": "passed"}},
			}
			err := validateRequest(t.TempDir(), state, TransitionSpec{ID: id}, Request{Actor: "orchestrator"})
			if id == "TR-009" {
				if err == nil || !strings.Contains(err.Error(), "current product baseline") {
					t.Fatalf("persisted clean label bypassed current product proof: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("S10 request preflight repeated the upstream product proof: %v", err)
			}
			guard, found := LookupGuard("clean_round_still_valid")
			if !found {
				t.Fatal("original S10 clean-round guard is not registered")
			}
			if err := guard(state, map[string]string{"clean_round_record": "persisted-label"}); err == nil || !strings.Contains(err.Error(), "no ReviewPlan") {
				t.Fatalf("persisted clean label bypassed the actual S10 round consumer: %v", err)
			}
		})
	}
}

func TestCleanProductConsumersPreserveLegacyRequestValidation(t *testing.T) {
	for _, id := range []string{"TR-009", "TR-015", "TR-017"} {
		if err := validateRequest(t.TempDir(), map[string]any{}, TransitionSpec{ID: id}, Request{Actor: "orchestrator"}); err != nil {
			t.Fatalf("legacy %s gained canonical source requirement: %v", id, err)
		}
	}
}

func TestBuilderReportSlotDoesNotTrustCompletionKindAlone(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"changed_paths":[]}`)
	if err := os.WriteFile(filepath.Join(root, "result.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"id": "result", "kind": "completion_report", "status": "valid", "baseline_generation": 1, "path": "result.json", "sha256": SHA256(data)}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "evidence": []any{row}}
	if err := validateCurrentEvidenceWithFiles(root, state, "builder_report_record", "result", nil); err == nil || !strings.Contains(err.Error(), "canonical builder report source") {
		t.Fatalf("completion kind granted original builder report authority without canonical source: %v", err)
	}
	row["kind"] = "builder_report"
	if err := validateCurrentEvidenceWithFiles(root, state, "builder_report_record", "result", nil); err != nil {
		t.Fatalf("original legacy builder report binding changed: %v", err)
	}
}

func TestCanonicalTR006RequiresBatchBeforeCompatibleReportBinding(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-canonical", "baseline": map[string]any{"generation": 1},
		"entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-canonical", "baseline_generation": 1}}},
	}
	err := validateRequest(t.TempDir(), state, TransitionSpec{ID: "TR-006"}, Request{Actor: "orchestrator"})
	if err == nil || !strings.Contains(err.Error(), "current canonical Main batch") {
		t.Fatalf("canonical TR-006 skipped batch proof before binding a compatible legacy report: %v", err)
	}
	if err := validateRequest(t.TempDir(), map[string]any{}, TransitionSpec{ID: "TR-006"}, Request{Actor: "orchestrator"}); err == nil || !strings.Contains(err.Error(), "execution_batch_empty") {
		t.Fatalf("direct legacy TR-006 must enforce the quality gate's nonempty exact batch: %v", err)
	}
}
