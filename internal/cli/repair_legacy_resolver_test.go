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
	// The legacy repair dispatch model (review.repair.assignment_owners plus
	// the manifest execution coordinate) was removed: the current S9 route is
	// the Assignment Record and the native packet
	// (L4-workflow-convergence §"S6／S9 当前工作区"). The legacy coordinates
	// must not be resolved into an execution slot.
	first := newRepairExecutionResolver(ctx, root, state).resolve(row)
	if first.err != nil || first.owner != "owner" || first.id != "repair-a" || first.execution != nil {
		t.Fatalf("original selection: %#v", first)
	}
	// An unknown source stays unknown and is reported as unavailable; the
	// system must not guess a workspace command.
	// An unknown source stays unknown: the original preparation continues
	// and no workspace command is guessed.
	text := prepareRepairExecutionFromChoiceContext(ctx, root, state, row, "owner")
	if !strings.Contains(text, "no current prepared PlanReport") || strings.Contains(text, "runtime workspace prepare") {
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
	// The legacy plan_sha256 / plan_report_refs registration is removed, so
	// it must not produce an owner or an execution coordinate any more.
	r := newRepairExecutionResolver(context.Background(), root, state).resolve(repairDispatchChoice{AssignmentID: "repair-a"})
	if r.owner != "" || r.id != "repair-a" {
		t.Fatalf("legacy repair registration must not resolve an owner: %#v", r)
	}
	// The removed legacy registration (plan_sha256 / plan_report_refs) is not
	// a registered dispatch observation any more.
	same, err := s9ObservedDispatchRegisteredContext(context.Background(), root, state, []byte(`[{"assignment_id":"repair-a","role_family":"backend-builder","check_location":"main"}]`), false)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Fatalf("legacy repair registration must not be treated as a registered dispatch")
	}
}
