package hook_test

import (
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestNativeObserverDecisionBoundsExternalErrorText(t *testing.T) {
	decision := hook.NativeObserverDecision(policy.Input{
		Event: "PostToolUseFailure",
		Error: strings.Repeat("x", 5000),
	}, time.Millisecond)
	if len(decision.Reason) > 512 {
		t.Fatalf("native observer reason must be bounded, got %d bytes", len(decision.Reason))
	}
}
