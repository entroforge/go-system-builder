package runtime_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestFreshInactiveIgnoresUnrelatedHistoricalReviewRound(t *testing.T) {
	for _, value := range []any{nil, "old", float64(7), []any{"old"}, map[string]any{"old": true}} {
		state := freshInactiveState(t)
		state["review"].(map[string]any)["round"] = value
		if err := runtime.ValidateFreshInactiveState(state); err != nil {
			t.Fatalf("historical round blocked inactive original state: %v", err)
		}
	}
	state := freshInactiveState(t)
	state["review"].(map[string]any)["clean_round"] = map[string]any{"status": "passed"}
	if err := runtime.ValidateFreshInactiveState(state); err == nil {
		t.Fatal("actual completed review was ignored")
	}
}
