package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS9LegacyResolverSharesRequestCacheAndInvalidatesOnRevision(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{
		"task.json":     `{"assignment_id":"repair-a"}`,
		"manifest.json": `{"runtime_id":"runtime-1","assignments":[{"assignment_id":"manifest-a"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{
		"runtime_id": "runtime-1", "revision": 1,
		"review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{"repair-a": "owner"}}},
		"entities": map[string]any{
			"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a"}, "prompt_ref": "manifest.json#manifest-a"}},
			"tasks":  []any{map[string]any{"id": "task-a", "path": "task.json"}},
		},
	}
	ctx := context.WithValue(context.Background(), hookReaderKey{}, &hookReader{})
	row := repairDispatchChoice{AssignmentID: "repair-a"}
	first := newRepairExecutionResolver(ctx, root, state).resolve(row)
	if first.err != nil || first.id != "manifest-a" {
		t.Fatalf("original selection: %#v", first)
	}
	if err := os.Remove(filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	cached := newRepairExecutionResolver(ctx, root, state).resolve(row)
	if cached.err != nil || cached.id != first.id {
		t.Fatalf("same request/snapshot should reuse projection: %#v", cached)
	}
	state["revision"] = 2
	newSnapshot := newRepairExecutionResolver(ctx, root, state).resolve(row)
	if newSnapshot.err == nil || newSnapshot.id != "" {
		t.Fatalf("new Writer revision must not reuse previous coordinates: %#v", newSnapshot)
	}
	text := prepareRepairExecutionFromChoiceContext(ctx, root, state, row, "owner")
	if !strings.Contains(text, "unavailable") || strings.Contains(text, "runtime workspace prepare") {
		t.Fatalf("unknown source guessed a command: %s", text)
	}
}

func TestS9LegacyRegisteredObservationKeepsOwnerWhenExecutionUnknown(t *testing.T) {
	root := t.TempDir()
	plan := []byte(`{"assignments":[{"assignment_id":"repair-a","scope":["internal/a"]}]}`)
	report := []byte(`{"agent_id":"owner","assignment_id":"repair-a"}`)
	if err := os.WriteFile(filepath.Join(root, "plan.json"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.json"), report, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "runtime-1", "revision": 1, "review": map[string]any{"repair": map[string]any{"plan_ref": "plan.json", "plan_sha256": repairLeadDigest(plan), "plan_report_refs": []any{map[string]any{"path": "report.json", "sha256": repairLeadDigest(report)}}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "role": "backend-builder", "task_ids": []string{"task-a"}, "prompt_ref": "missing.json#manifest-a"}}}}
	r := newRepairExecutionResolver(context.Background(), root, state).resolve(repairDispatchChoice{AssignmentID: "repair-a"})
	if r.owner != "owner" || r.err == nil || r.id != "" {
		t.Fatalf("owner and unknown coordinate not separated: %#v", r)
	}
	same, err := s9ObservedDispatchRegisteredContext(context.Background(), root, state, []byte(`[{"assignment_id":"repair-a","role_family":"backend-builder","check_location":"main"}]`), false)
	if err != nil || !same {
		t.Fatalf("registered dispatch lost on unknown coordinate: same=%v err=%v", same, err)
	}
}
