package hookctx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestResolveLegacyRepairManifestAssignmentUsesOriginalRegisteredSources(t *testing.T) {
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
		"runtime_id": "runtime-1",
		"review":     map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{"repair-a": "owner"}}},
		"entities": map[string]any{
			"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a"}, "prompt_ref": "manifest.json#manifest-a"}},
			"tasks":  []any{map[string]any{"id": "task-a", "path": "task.json"}},
		},
	}
	snapshot := runtime.Snapshot{State: state}
	id, err := ResolveLegacyRepairManifestAssignment(context.Background(), root, snapshot, "repair-a", "owner")
	if err != nil || id != "manifest-a" {
		t.Fatalf("original optional legacy fields: id=%q error=%v", id, err)
	}
	if _, err := ResolveLegacyRepairManifestAssignment(context.Background(), root, snapshot, "repair-other", "owner"); err == nil {
		t.Fatal("an unrelated repair responsibility must not borrow the registered manifest")
	}
	if _, err := ResolveLegacyRepairManifestAssignment(context.Background(), root, snapshot, "repair-a", "other-owner"); err == nil {
		t.Fatal("another owner must not borrow the registered manifest")
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"runtime_id":"runtime-1","assignments":[{"assignment_id":"manifest-a","agent_id":"other"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLegacyRepairManifestAssignment(context.Background(), root, snapshot, "repair-a", "owner"); err == nil {
		t.Fatal("manifest selection must retain the original owner check")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveLegacyRepairManifestAssignment(ctx, root, snapshot, "repair-a", "owner"); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancellation must remain unknown: %v", err)
	}
}

func TestResolveLegacyRepairUsesOriginalPlanReportAndREQFallback(t *testing.T) {
	for _, tc := range []struct {
		name, runtimeID, bound, location string
		report                           bool
		two                              bool
		want                             string
	}{
		{"bound-plan", "runtime-1", "REQ-1", "REQ-1", false, false, "manifest-a"},
		{"runtime-report", "loop-REQ-1", "", "REQ-1", true, false, "manifest-a"},
		{"unbound-report", "runtime-1", "", "REQ-1", true, false, "manifest-a"},
		{"unbound-ambiguous", "runtime-1", "", "REQ-1", true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			plan := []byte(`{"assignments":[{"assignment_id":"repair-a","owner_agent_id":"owner","scope":["internal/a"]}]}`)
			report := []byte(`{"assignment_id":"repair-a","agent_id":"owner"}`)
			if tc.report {
				plan = []byte(`{"assignments":[{"assignment_id":"repair-a","scope":["internal/a"]}]}`)
			}
			for path, data := range map[string][]byte{"plan.json": plan, "report.json": report, filepath.Join(".claude", "workgroups", tc.location, "task-a", "manifest.json"): []byte(`{"assignments":[{"assignment_id":"manifest-a","agent_id":"owner"}]}`)} {
				full := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.two {
				path := filepath.Join(root, ".claude", "workgroups", "REQ-2", "task-a", "manifest.json")
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"assignments":[{"assignment_id":"manifest-b","agent_id":"owner"}]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			pointer := map[string]any{"plan_ref": "plan.json"}
			if tc.report {
				pointer["plan_report_refs"] = []any{map[string]any{"path": "report.json"}}
			}
			state := map[string]any{"runtime_id": tc.runtimeID, "review": map[string]any{"repair": pointer}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a"}}}, "tasks": []any{map[string]any{"id": "task-a", "path": "absent-task.json"}}}}
			if tc.bound != "" {
				state["bound_req"] = map[string]any{"id": tc.bound}
			}
			owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
			if tc.want == "" {
				if err == nil || id != "" {
					t.Fatalf("ambiguous result %q %q %v", owner, id, err)
				}
				return
			}
			if err != nil || id != tc.want || owner != "owner" {
				t.Fatalf("got %q %q %v", owner, id, err)
			}
		})
	}
}

func TestResolveLegacyRepairManifestOriginalTaskOrderAndExplicitRef(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(`{"documents":[{"kind":"task","id":"task-b"}],"assignments":[{"assignment_id":"manifest-b","agent_id":"owner"}]}`)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "runtime-1", "review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{"repair-a": "owner"}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a", "task-b"}, "prompt_ref": "manifest.json#manifest-b"}}}}
	owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	if err != nil || owner != "owner" || id != "manifest-b" {
		t.Fatalf("original task order: %q %q %v", owner, id, err)
	}
	original := loadAgentAssignmentID(root, []string{"task-a", "task-b"}, "owner", registeredManifestRef{PromptRef: "manifest.json#manifest-b"}, "runtime-1")
	if original != id {
		t.Fatalf("new projection %q differs from original loader %q", id, original)
	}
	path := filepath.Join(root, ".claude", "workgroups", "REQ-1", "task-a", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"assignments":[{"assignment_id":"incorrect","agent_id":"owner"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	state["bound_req"] = map[string]any{"id": "REQ-1"}
	agents := state["entities"].(map[string]any)["agents"].([]any)
	agents[0].(map[string]any)["prompt_ref"] = "missing.json#manifest-b"
	owner, id, err = ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	if owner != "owner" || id != "" || err == nil {
		t.Fatalf("formal missing ref must not fall back: %q %q %v", owner, id, err)
	}
}

func TestResolveLegacyRepairAmbiguousOwnersAreUnknown(t *testing.T) {
	root := t.TempDir()
	plan := []byte(`{"assignments":[{"assignment_id":"repair-a"}]}`)
	reportA := []byte(`{"agent_id":"owner-a","assignment_id":"repair-a"}`)
	reportB := []byte(`{"agent_id":"owner-b","assignment_id":"repair-a"}`)
	for path, data := range map[string][]byte{"plan.json": plan, "a.json": reportA, "b.json": reportB} {
		if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{"runtime_id": "runtime-1", "review": map[string]any{"repair": map[string]any{"plan_ref": "plan.json", "plan_report_refs": []any{map[string]any{"path": "a.json"}, map[string]any{"path": "b.json"}}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner-a"}, map[string]any{"id": "owner-b"}}}}
	owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	if err == nil || owner != "" || id != "" {
		t.Fatalf("must not guess an owner: %q %q %v", owner, id, err)
	}
}
