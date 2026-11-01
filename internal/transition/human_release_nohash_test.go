package transition

import (
	"path/filepath"
	"testing"
)

func TestHumanReleaseReplayUsesConsumedDecisionWhenPrivateDraftIsGone(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join(".claude", "decisions", "decision.json")
	state := map[string]any{"runtime_id": "loop-REQ-001", "evidence": []any{map[string]any{
		"id": "decision-1", "kind": "human_decision", "path": rel, "consumed_by": "TR-025", "sha256": "historical-and-ignored",
	}}}
	request := Request{TransitionID: "TR-025", Actor: "Main", Params: map[string]any{
		"decision_id": "decision-1", "decision_path": rel, "disposition": "approve", "reason": "actual human choice",
	}}
	replayed, err := humanReleaseReplay(root, state, request)
	if err != nil || !replayed {
		t.Fatalf("consumed human decision must replay without old private file: %v %v", replayed, err)
	}
	request.TransitionID = "TR-026"
	request.Params["disposition"] = "defer"
	if replayed, err = humanReleaseReplay(root, state, request); err == nil || replayed {
		t.Fatalf("existing approval must not become a new defer decision: %v %v", replayed, err)
	}
}
