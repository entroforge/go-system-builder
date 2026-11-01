package cli

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"path/filepath"
	"strings"
	"testing"
)

func TestS7LegacyBudgetNumbersDoNotCreateProjectedGateway(t *testing.T) {
	for _, cursor := range []string{"verification", "bug_resolution", "acceptance"} {
		state := map[string]any{
			"lifecycle":     map[string]any{"state": cursor, "phase": "ready_for_full_review"},
			"review":        map[string]any{"round": 500},
			"configuration": map[string]any{"repair": map[string]any{"max_full_review_rounds": 1}},
		}
		data, err := json.Marshal(projectedGateway(state, "S7", t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "s7_budget") {
			t.Fatalf("legacy numbers created a human gate: %s", data)
		}
	}
}

func TestS7QuestionnaireObservationUsesOriginalPlanIdentity(t *testing.T) {
	state := map[string]any{
		"lifecycle": map[string]any{"state": "verification", "phase": "running"},
		"review":    map[string]any{"round_entry": map[string]any{"plan_id": "current-plan"}},
		"entities": map[string]any{"work_packets": []any{
			map[string]any{"template_id": "s7-custom", "answer_path": "current.json", "binding": map[string]any{"review_plan_id": "current-plan"}},
			map[string]any{"template_id": "s7-custom", "answer_path": "old.json", "binding": map[string]any{"review_plan_id": "old-plan"}},
			map[string]any{"template_id": "s7-custom", "answer_path": "unknown.json", "binding": map[string]any{"review_round": nil}},
		}},
	}
	rows := observableQuestionnaires(t.TempDir(), runtime.Snapshot{State: state})
	var seenCurrent bool
	for _, row := range rows {
		switch filepath.Base(row.path) {
		case "current.json":
			seenCurrent = true
		case "old.json", "unknown.json":
			t.Fatalf("unrelated or unknown S7 responsibility selected: %s", row.path)
		}
	}
	if !seenCurrent {
		t.Fatal("current original Plan responsibility was not selected")
	}
}
