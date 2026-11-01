package cli

import (
	"testing"
)

func TestRelocationIntentDecodesNestedCheckpointWithoutHistoricalDigest(t *testing.T) {
	data := []byte(`{"request":{},"checkpoints":[{"path":"checkpoint.json","before":{"assignment_id":"a","state":"complete","source_head":{"old":true}},"after":{"assignment_id":"a","state":"complete","tested_head":42}}]}`)
	intent, err := decodeRelocationIntent(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Checkpoints) != 1 || intent.Checkpoints[0].Path != "checkpoint.json" || intent.Checkpoints[0].Before.AssignmentID != "a" || intent.Checkpoints[0].After.State != "complete" {
		t.Fatalf("original checkpoint coordinates lost: %+v", intent.Checkpoints)
	}
}
