package policy_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestLifecycleActivityKeepsLegacyAuthorizationSeparateFromFiveState(t *testing.T) {
	if got := (&policy.AgentContext{State: "working"}).LifecycleActivity(); got != "working" {
		t.Fatalf("legacy working activity = %q", got)
	}
	if got := (&policy.AgentContext{State: "active"}).LifecycleActivity(); got != "" {
		t.Fatalf("canonical active without an original event was guessed as %q", got)
	}
	if got := (&policy.AgentContext{State: "active", Activity: "working"}).LifecycleActivity(); got != "working" {
		t.Fatalf("current lifecycle event activity = %q", got)
	}
}
