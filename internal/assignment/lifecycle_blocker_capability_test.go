package assignment_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestAdvanceAgentRejectsZeroS9BlockerCapabilityBeforeRuntimeRead(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	_, err := assignment.AdvanceAgentWithContext(context.Background(), root, statePath, journalPath, assignment.AgentEventRequest{
		AgentID: "agent-s9-1", Event: "work_blocked",
		IssuedRepairResultBlocker: &repair.IssuedRepairResultBlockerCapability{},
	})
	if err == nil || !strings.Contains(err.Error(), "capability has no complete immutable lifecycle refs") {
		t.Fatalf("zero capability must fail before message/Runtime/recovery/lock access: %v", err)
	}
	for _, path := range []string{statePath, journalPath, filepath.Join(root, ".claude", "integration.lock")} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("zero capability refusal accessed or created %s: %v", path, statErr)
		}
	}
}
