package hookctx

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyLoaderCompatLargeValidManifest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.json")
	b := []byte(`{"runtime_id":"runtime-1","assignments":[{"assignment_id":"manifest-a","agent_id":"owner"}]}` + strings.Repeat(" ", 4*1024*1024))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "runtime-1", "review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{"repair-a": "owner"}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a"}, "prompt_ref": "manifest.json#manifest-a"}}}}
	old := loadAgentAssignmentID(root, []string{"task-a"}, "owner", registeredManifestRef{PromptRef: "manifest.json#manifest-a"}, "runtime-1")
	owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	t.Logf("old=%q newOwner=%q newID=%q newError=%v bytes=%d", old, owner, id, err, len(b))
	if old != "manifest-a" || owner != "owner" || id != old || err != nil {
		t.Fatalf("original loader contract changed: old=%q owner=%q id=%q error=%v", old, owner, id, err)
	}
}
func TestLegacyLoaderCompatUnavailableFirstTask(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, ".claude", "workgroups", "REQ-1", "task-a", "manifest.json")
	if err := os.MkdirAll(a, 0755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(root, ".claude", "workgroups", "REQ-1", "task-b", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(b), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"assignments":[{"assignment_id":"manifest-b","agent_id":"owner"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "runtime-1", "review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{"repair-a": "owner"}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a", "task-b"}}}}}
	old := loadAgentAssignmentID(root, []string{"task-a", "task-b"}, "owner", registeredManifestRef{}, "runtime-1")
	owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	t.Logf("old=%q newOwner=%q newID=%q newError=%v", old, owner, id, err)
	if old != "manifest-b" || owner != "owner" || id != old || err != nil {
		t.Fatalf("original task-order contract changed: old=%q owner=%q id=%q error=%v", old, owner, id, err)
	}
}

func TestLegacyLoaderCompatLargePlanOriginalOwner(t *testing.T) {
	root := t.TempDir()
	plan := []byte(`{"assignments":[{"assignment_id":"repair-a","owner_agent_id":"owner","scope":["internal/a"]}]}` + strings.Repeat(" ", 4*1024*1024))
	if err := os.WriteFile(filepath.Join(root, "plan.json"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"assignments":[{"assignment_id":"manifest-a","agent_id":"owner"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	pointer := map[string]any{"plan_ref": "plan.json"}
	state := map[string]any{"runtime_id": "runtime-1", "review": map[string]any{"repair": pointer}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a"}, "prompt_ref": "manifest.json#manifest-a"}}}}
	original := &policy.AgentContext{}
	loadRepairAgentScope(root, state, pointer, "owner", "manifest-a", original)
	owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	t.Logf("oldRepairID=%q oldScope=%v newOwner=%q newID=%q newError=%v bytes=%d", original.RepairAssignmentID, original.RepairAllowedWritePaths, owner, id, err, len(plan))
	if original.RepairAssignmentID != "repair-a" || owner != "owner" || id != "manifest-a" || err != nil {
		t.Fatalf("original owner contract changed: repair=%q owner=%q id=%q error=%v", original.RepairAssignmentID, owner, id, err)
	}
}
func TestLegacyLoaderCompatUnboundUnreadableCandidate(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, ".claude", "workgroups", "REQ-1", "task-a", "manifest.json")
	if err := os.MkdirAll(a, 0755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(root, ".claude", "workgroups", "REQ-2", "task-a", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(b), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"assignments":[{"assignment_id":"manifest-a","agent_id":"owner"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "runtime-1", "review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{"repair-a": "owner"}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "task_ids": []string{"task-a"}}}}}
	old := loadAgentAssignmentID(root, []string{"task-a"}, "owner", registeredManifestRef{}, "runtime-1")
	owner, id, err := ResolveLegacyRepairResponsibility(context.Background(), root, runtime.Snapshot{State: state}, "repair-a", "")
	t.Logf("old=%q newOwner=%q newID=%q newError=%v", old, owner, id, err)
	if old != "manifest-a" || owner != "owner" || id != old || err != nil {
		t.Fatalf("original unbound contract changed: old=%q owner=%q id=%q error=%v", old, owner, id, err)
	}
}
