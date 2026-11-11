package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// Old metrics remain readable in their original store, but the current
// professional board does not expose numeric review statistics.
func TestS7StatusOmitsLegacyRoundStatistics(t *testing.T) {
	root := t.TempDir()

	// Minimal runtime with a registered plan pointer (round 1).
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	subjectBytes := []byte("fixture baseline")
	subjectSum := sha256.Sum256(subjectBytes)
	subjectRel := "internal/example/service.go"
	if err := os.MkdirAll(filepath.Join(root, "internal", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, subjectRel), subjectBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	planBytes, err := json.MarshalIndent(map[string]any{
		"schema_version": "1.0.0",
		"review_plan_id": "review-plan-cli-1",
		"review_round":   1,
		"frozen_subjects": []any{
			map[string]any{"path": subjectRel, "sha256": fmt.Sprintf("%x", subjectSum[:]), "kind": "product_code"},
		},
		"claims":      []any{},
		"assignments": []any{},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	planBytes = append(planBytes, '\n')
	sum := sha256.Sum256(planBytes)
	planRel := filepath.Join(".claude", "review", "plans", "review-plan-cli-1.json")
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(planRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, planRel), planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["review"] = map[string]any{
		"round":       1,
		"clean_round": nil,
		"plan": map[string]any{
			"plan_id":            "review-plan-cli-1",
			"path":               filepath.ToSlash(planRel),
			"sha256":             fmt.Sprintf("%x", sum[:]),
			"revision":           1,
			"review_round":       1,
			"status":             "running",
			"e2e_coverage_state": "not_applicable",
			"submitted_at":       "2026-08-20T00:00:00Z",
		},
		"claims":      map[string]any{},
		"assignments": map[string]any{},
	}
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(stateData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// Metrics recorded by earlier runtime verbs.
	if err := metrics.RecordS7ResultSubmit(root, "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordS7ResultSubmit(root, "rejected"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runS7Status(root, &out, false); code != 0 {
		t.Fatalf("runS7Status exit = %d, output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "plan: review-plan-cli-1") || strings.Contains(out.String(), "subject_digest:") {
		t.Fatalf("status must show the plan without digest registration: %s", out.String())
	}
	for _, oldStatistic := range []string{"metrics (S7", "loop_s7_", "round=", "round 1 of"} {
		if strings.Contains(out.String(), oldStatistic) {
			t.Fatalf("status exposed legacy round statistics %q: %s", oldStatistic, out.String())
		}
	}
}

func TestS7PacketStatusShowsPreparationAndDispatchGaps(t *testing.T) {
	root := t.TempDir()
	plan := &review.Plan{ReviewPlanID: "plan-current", BaselineGeneration: 1,
		Assignments: []review.PlanAssignment{
			{AssignmentID: "assignment-missing", ClaimIDs: []string{"claim-b"}},
			{AssignmentID: "assignment-queued", ClaimIDs: []string{"claim-c"}},
		}}
	state := map[string]any{
		"runtime_id": "runtime-current",
		"review": map[string]any{
			"plan": map[string]any{"plan_id": "plan-current", "revision": 1, "review_round": 2, "sha256": "current-plan-digest"},
			"assignments": map[string]any{
				"assignment-missing": map[string]any{"agent_id": "reviewer-b"},
				"assignment-queued":  map[string]any{"queued_agent_id": "reviewer-c", "queue_reason": "platform capacity"},
			},
		},
	}
	before, _ := json.Marshal(state)
	var out bytes.Buffer
	printS7PacketQuestionnaires(root, &out, state, plan, []string{"claim-b", "claim-c"})
	for _, want := range []string{"preparation gap: assignment-missing", "Main/system", "assignment-queued", "reviewer-c", "platform capacity"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in packet status: %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "review-result submit") || strings.Contains(out.String(), "<TASK>") {
		t.Fatalf("packet status requested manual delivery: %s", &out)
	}
	after, _ := json.Marshal(state)
	if !bytes.Equal(before, after) {
		t.Fatal("status projection mutated original facts")
	}
}
