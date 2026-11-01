package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestFrozenWorkerSubmissionRetainsOriginalResponsibilityWithoutDigest(t *testing.T) {
	main := t.TempDir()
	worker := t.TempDir()
	execution := workspace.Execution{RuntimeID: "loop-REQ-001", BaselineGeneration: 1, AssignmentID: "assignment-1", AgentID: "builder-1"}
	source := filepath.Join(worker, "plan.json")
	first := []byte(`{"message_id":"msg-1","agent_id":"builder-1","summary":"first"}`)
	if err := os.WriteFile(source, first, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"entities": map[string]any{"agents": []any{map[string]any{"id": execution.AgentID}}}}
	ref, err := freezeWorkerSubmission(main, worker, source, execution, state, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(ref), "submission-") {
		t.Fatalf("unexpected submission identity: %s", ref)
	}
	if got, err := os.ReadFile(ref); err != nil || string(got) != string(first) {
		t.Fatalf("original plan was not retained: %q %v", got, err)
	}
	// A real Runtime reference, not a matching filename, identifies the first
	// publication. Harmless JSON formatting does not create another copy.
	rel, err := filepath.Rel(main, ref)
	if err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["plan_reported_ref"] = filepath.ToSlash(rel)
	formatted, _ := json.MarshalIndent(map[string]any{"message_id": "msg-1", "agent_id": "builder-1", "summary": "first"}, "", "  ")
	if err := os.WriteFile(source, formatted, 0600); err != nil {
		t.Fatal(err)
	}
	replayed, err := freezeWorkerSubmission(main, worker, source, execution, state, true)
	if err != nil || replayed != ref {
		t.Fatalf("registered replay did not reuse its original: %q %v", replayed, err)
	}
	if err := os.WriteFile(source, []byte(`{"message_id":"msg-1","agent_id":"builder-1","summary":"changed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := freezeWorkerSubmission(main, worker, source, execution, state, true)
	if err != nil || changed == ref {
		t.Fatalf("new expression did not retain an independent candidate: %q %v", changed, err)
	}
	if got, _ := os.ReadFile(ref); string(got) != string(first) {
		t.Fatal("new expression overwrote the issued original")
	}
	// task-complete consumes the original once and persists its own Result.
	direct, err := freezeWorkerSubmission(main, worker, source, execution, state, false)
	if err != nil || direct != source {
		t.Fatalf("completion should use the original once: %q %v", direct, err)
	}
}

func TestFrozenWorkerSubmissionDoesNotClaimOtherOwnerOrUnknownFile(t *testing.T) {
	main, worker := t.TempDir(), t.TempDir()
	execution := workspace.Execution{RuntimeID: "loop-REQ-001", BaselineGeneration: 1, AssignmentID: "assignment-1", AgentID: "builder-1"}
	source := filepath.Join(worker, "plan.json")
	data := []byte(`{"message_id":"msg-1","agent_id":"builder-1"}`)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	unknown, err := freezeWorkerSubmission(main, worker, source, execution, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(main, unknown)
	other := map[string]any{"entities": map[string]any{"agents": []any{map[string]any{"id": "builder-2", "plan_reported_ref": filepath.ToSlash(rel)}}}}
	actual, err := freezeWorkerSubmission(main, worker, source, execution, other, true)
	if err != nil || actual == unknown {
		t.Fatalf("unknown/other-owner candidate was reused: %q %v", actual, err)
	}
	if got, _ := os.ReadFile(unknown); string(got) != string(data) {
		t.Fatal("unknown original was changed")
	}
}
