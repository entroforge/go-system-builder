package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// Pending recovery at a new S7 must not consume a late S9 return as an S7
// Stop, and an unrelated actor must not be guessed as the only S9 Worker.
func TestRecoveredOriginalS9SourcesUsesExactIssuedTask(t *testing.T) {
	root, _, packet, input, _ := nativeS9WorkerFixture(t)
	state := repairDispatchSnapshot(t, root).State
	brief := nativeS9BriefPath(packet)
	absolute := filepath.Join(root, brief)

	for _, tc := range []struct {
		name, message string
		want          bool
	}{
		{"original brief", "original responsibility: " + absolute, true},
		{"unrelated task", "ordinary S7 reviewer", false},
		{"role name only", "backend-builder", false},
		{"basename", filepath.Base(absolute), false},
		{"path suffix", absolute + ".backup", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			in.Event = "SubagentStop"
			in.LastAssistantMessage = tc.message
			selected, ok := recoveredOriginalS9Sources(root, in, state)
			if ok != tc.want {
				t.Fatalf("selected=%t, want %t for %q", ok, tc.want, tc.message)
			}
			if ok && !strings.Contains(selected.sources.text(), absolute) {
				t.Fatalf("selected event lost original task: %q", selected.sources.text())
			}
		})
	}
}
