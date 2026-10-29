package assignment_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// These are original legacy lifecycle Writer tests, not typed S9 receipt
// fixtures. Each initial state declares its actual generation before writing;
// the journal must retain it rather than the Store's unspecified default.
func TestAdvanceAgentRecordsCurrentBaselineGeneration(t *testing.T) {
	for _, generation := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("generation-%d", generation), func(t *testing.T) {
			root := assignmentTestRoot(t)
			dir := t.TempDir()
			statePath, journalPath := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
			state := activeState(t, root, "verification", "running", 7)
			state["baseline"].(map[string]any)["generation"] = generation
			state["entities"].(map[string]any)["agents"] = []any{map[string]any{
				"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
				"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
				"definition_ref": ".claude/agents/delivery-verifier.md", "prompt_ref": "manifest#assignment-ver-1",
				"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
				"updated_at": "2026-06-22T00:00:00Z",
			}}
			writeJSON(t, statePath, state)
			messagePath := writeAgentExample(t, root, dir, "readback_response", "agent-ver-1", "TASK-012", 7)
			next, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
				ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: messagePath,
			})
			if err != nil {
				t.Fatalf("original Agent lifecycle Writer: %v", err)
			}
			if next.Revision != 8 {
				t.Fatalf("original revision=%d, want 8", next.Revision)
			}
			raw, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			matched := 0
			for {
				var event map[string]any
				if err := decoder.Decode(&event); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if event["transition_id"] != "AGENT-LIFECYCLE" {
					continue
				}
				matched++
				if event["event"] != "transition_committed" || event["runtime_id"] != "loop-REQ-002" ||
					event["baseline_generation"] != float64(generation) || event["before_revision"] != float64(7) || event["after_revision"] != float64(8) ||
					event["event_id"] != "evt-agent-agent-ver-1-readback_submitted-r8" || event["idempotency_key"] != "runtime:agent:agent-ver-1:readback_submitted:7" {
					t.Errorf("original lifecycle event lost its current generation/identity: generation=%d event=%v", generation, event)
				}
			}
			if matched != 1 {
				t.Fatalf("original lifecycle events=%d, want 1", matched)
			}
		})
	}
}
