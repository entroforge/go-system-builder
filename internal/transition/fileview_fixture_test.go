package transition_test

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"os"
)

// These tests isolate transition guards/actions from Git delivery. Integration
// tests use production binding and committed snapshots.
func applyFixture(root, statePath, journalPath string, request transition.Request) (runtime.Snapshot, error) {
	if request.Files == nil {
		fixtureRoot := root
		if request.TransitionID != "TR-001" {
			var state map[string]any
			data, _ := os.ReadFile(statePath)
			_ = json.Unmarshal(data, &state)
			if r, ok := state["root"].(string); ok && r != "" {
				fixtureRoot = r
			}
		}
		request.Files = fileview.Disk{Root: fixtureRoot}
	}
	return transition.Apply(root, statePath, journalPath, request)
}
