package transition

import (
	"testing"
	"time"
)

func TestPauseResumeUsesActualResponsibilityWithoutDigest(t *testing.T) {
	state := map[string]any{
		"lifecycle": map[string]any{"state": "planning", "phase": "design", "phase_revision": 1},
		"baseline":  map[string]any{"generation": 1},
		"review":    map[string]any{"round": 42},
		"documents": []any{map[string]any{"path": "missing.md", "sha256": "historical"}},
	}
	if err := CapturePauseCheckpoint(state, "actual pause", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	pause := state["pause"].(map[string]any)
	if _, ok := pause["review_round"]; ok {
		t.Fatal("new checkpoint recorded an obsolete review number")
	}
	if _, ok := pause["document_fingerprints"]; ok {
		t.Fatal("new checkpoint copied historical digests")
	}
	// A legacy checkpoint can contain hashes and unavailable source files. Its
	// original saved responsibility, not hash reconstruction, determines resume.
	pause["document_fingerprints"] = []any{map[string]any{"path": "missing.md", "sha256": "historical"}}
	pause["review_round"] = "unrelated historical field"
	if err := restoreFromPauseAction(t.TempDir(), state); err != nil {
		t.Fatal(err)
	}
	if state["pause"] != nil {
		t.Fatal("resume did not consume saved checkpoint")
	}
}
