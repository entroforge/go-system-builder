package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointKeepsHistoricalReceiptsWithoutDigestProof(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-checkpoint",
		"journal":    map[string]any{"last_sequence": 0, "last_event_id": nil},
		"workspace": map[string]any{"executions": map[string]any{
			"original": map[string]any{"tool_check_receipts": []any{map[string]any{"path": "missing-old-file", "sha256": "historical-only"}}},
		}},
	}
	if err := ValidateCheckpointIndices(t.TempDir(), state, nil); err != nil {
		t.Fatalf("historical receipt must not require source or digest proof: %v", err)
	}
	state["journal"].(map[string]any)["last_sequence"] = 1
	if err := ValidateCheckpointIndices(t.TempDir(), state, nil); err == nil {
		t.Fatal("state and journal pair mismatch was not reported")
	}
}

func TestCheckpointIndexValidationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateCheckpointIndicesContext(ctx, t.TempDir(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation = %v", err)
	}
}

func TestCheckpointRecoveryUsesOriginalIdentityRatherThanWholeStateEquality(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	source := map[string]any{"runtime_id": "loop-1", "revision": 1, "bound_req": map[string]any{"id": "REQ-1", "path": "req.md", "version": "v1"}, "baseline": map[string]any{"generation": 1}, "journal": map[string]any{"last_sequence": 0, "last_event_id": nil}, "business": map[string]any{"notes": "original"}}
	current := map[string]any{"runtime_id": "loop-1", "revision": 1, "bound_req": map[string]any{"id": "REQ-1", "path": "req.md", "version": "v1", "unrelated": "extra"}, "baseline": map[string]any{"generation": 1}, "journal": map[string]any{"last_sequence": 0, "last_event_id": nil}, "business": map[string]any{"notes": "changed"}}
	data, _ := json.Marshal(current)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	pair := CheckpointPair{State: source, Manifest: RuntimeCheckpoint{RuntimeID: "loop-1", Generation: 1, Revision: 1}}
	if err := ValidateCheckpointRestore(root, pair); err != nil {
		t.Fatalf("unrelated professional content became a qualification: %v", err)
	}
	current["revision"] = 2
	data, _ = json.Marshal(current)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckpointRestore(root, pair); err == nil {
		t.Fatal("newer original Runtime was not protected")
	}
}
