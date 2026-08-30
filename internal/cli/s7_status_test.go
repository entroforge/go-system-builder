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
	"github.com/entroforge/go-system-builder/internal/schema"
)

// TestS7StatusIncludesMetricsSummary proves the read-only board appends the
// L3-S7 §14.2 machine-collectible metrics section for the current round.
func TestS7StatusIncludesMetricsSummary(t *testing.T) {
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
	digestLines := subjectRel + ":" + fmt.Sprintf("%x", subjectSum[:])
	digestSum := sha256.Sum256([]byte(digestLines))
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
		"claims":            map[string]any{},
		"assignments":       map[string]any{},
		"observation_batch": nil,
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
	if err := metrics.RecordS7RoundShape(root, 1, 2, 4, 1); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordS7ResultSubmit(root, "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordS7ResultSubmit(root, "rejected"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordS7ClaimLeadTime(root, 1, "claim-qa-1", 250); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runS7Status(root, &out, false); code != 0 {
		t.Fatalf("runS7Status exit = %d, output:\n%s", code, out.String())
	}
	for _, want := range []string{
		"metrics (S7 §14.2 machine-collectible):",
		fmt.Sprintf("subject_digest: %x", digestSum[:]),
		`loop_s7_assignments{round="1"} 2`,
		`loop_s7_claims{round="1"} 4`,
		`loop_s7_result_submits_total{outcome="accepted"} 1`,
		`loop_s7_result_submits_total{outcome="rejected"} 1`,
		`loop_s7_claim_lead_time_ms{claim="r1:claim-qa-1"} count=1 sum_ms=250`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("s7 status output lacks %q:\n%s", want, out.String())
		}
	}
}
