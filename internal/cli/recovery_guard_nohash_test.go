package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRecoveryGuardUsesActualActiveWorkerLeaseOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A durable marker means the original candidate was already accepted;
	// retry must protect its active owner without revalidating old materials.
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json.recovery-pending.json"), []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := workspace.Execution{AssignmentID: "task-1", RuntimeID: "loop-REQ-001", BaselineGeneration: 1, Generation: 1, AgentID: "worker", TargetBranch: "develop", Status: "ready"}
	reg := &workspace.ExecutionRegistry{Version: 1, MainRoot: root, CommonDir: filepath.Join(root, ".git"), Branch: "develop", Executions: map[string]workspace.Execution{"task-1": e}}
	state := map[string]any{"runtime_id": "loop-REQ-001", "revision": 1, "baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"workspace": map[string]any{"project_root": root, "dev_branch": "develop", "release_upstream": "main"}}, "workspace": workspace.Encode(reg)}
	lease := filepath.Join(root, ".claude", "workspace-launch", e.RuntimeID, "g1-task-1-e1.lock")
	unlock, err := filelock.Acquire(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	guard, release := checkpointRecoveryGuard(context.Background(), root, nil)
	if err := guard(state, nil); err == nil || !strings.Contains(err.Error(), "launch lease") {
		unlock()
		release()
		t.Fatalf("active worker must be protected: %v", err)
	}
	unlock()
	if err := guard(state, nil); err != nil {
		release()
		t.Fatalf("non-conflicting retry should continue: %v", err)
	}
	release()
	unlock, err = filelock.Acquire(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	e.Status = "retired"
	reg.Executions["task-1"] = e
	state["workspace"] = workspace.Encode(reg)
	guard, release = checkpointRecoveryGuard(context.Background(), root, nil)
	defer release()
	if err := guard(state, nil); err != nil {
		t.Fatalf("retired responsibility must not block recovery: %v", err)
	}
}
