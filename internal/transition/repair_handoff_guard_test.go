package transition_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestGenericTR012CannotBypassApprovedContractHandoff(t *testing.T) {
	guard, ok := transition.LookupGuard("all_targeted_reverification_passed")
	if !ok {
		t.Fatal("missing guard")
	}
	state := map[string]any{"review": map[string]any{"repair": map[string]any{"session_id": "repair-session-current", "status": "ready_for_full_review"}}}
	if err := guard(state, map[string]string{"targeted_reverification_record": "old-pass"}); err == nil || !strings.Contains(err.Error(), "runtime repair handoff commit") {
		t.Fatalf("generic transition bypassed the canonical handoff: %v", err)
	}
	if err := guard(map[string]any{}, nil); err != nil {
		t.Fatalf("legacy path changed: %v", err)
	}
}
