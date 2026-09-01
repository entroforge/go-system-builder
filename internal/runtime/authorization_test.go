package runtime_test

import (
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestConsumeHumanDecisionEvidenceIsOneTimeAndAuditable(t *testing.T) {
	state := map[string]any{
		"evidence": []any{map[string]any{
			"id": "decision-1", "kind": "human_decision", "status": "valid",
		}},
	}
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := runtime.ConsumeHumanDecisionEvidence(state, "decision-1", "TR-025", at); err != nil {
		t.Fatalf("first consumption failed: %v", err)
	}
	item := state["evidence"].([]any)[0].(map[string]any)
	if item["consumed_by"] != "TR-025" || item["consumed_at"] != at.Format(time.RFC3339Nano) {
		t.Fatalf("consumption metadata = %#v", item)
	}
	if item["status"] != "valid" {
		t.Fatalf("consumption must preserve audit status=valid, got %v", item["status"])
	}
	if err := runtime.ConsumeHumanDecisionEvidence(state, "decision-1", "TR-026", at); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("second consumption error = %v, want already-consumed rejection", err)
	}
}
