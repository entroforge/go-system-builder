package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestCheckpointJSONEmbeddingAndCurrentReader(t *testing.T) {
	t.Run("anonymous embedding keeps sibling fields", func(t *testing.T) {
		var value struct {
			integration.Checkpoint
			ContextMode string `json:"context_mode"`
		}
		data := []byte(`{"assignment_id":"assignment-1","baseline_generation":1,"state":"verified","revision":2,"context_mode":"enabled"}`)
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if value.ContextMode != "enabled" || value.AssignmentID != "assignment-1" || value.Revision != 2 {
			t.Fatalf("outer or original checkpoint fields lost: %+v", value)
		}
	})
	t.Run("actual responsibility type remains checked", func(t *testing.T) {
		_, err := integration.DecodeCheckpoint([]byte(`{"assignment_id":42,"state":"verified"}`))
		if err == nil {
			t.Fatal("invalid original responsibility must not be ignored")
		}
	})
}
