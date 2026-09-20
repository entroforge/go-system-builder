package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workerFixture(t *testing.T) (*Binding, Execution, map[string]any, context.Context) {
	t.Helper()
	b, ctx := repository(t)
	state := map[string]any{"runtime_id": "loop-test", "revision": 0, "baseline": map[string]any{"generation": 1}}
	e, err := b.Plan(ctx, state, "assignment", "builder", []string{"input.md"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	state["workspace"] = Encode(b)
	saveControlState(t, b.MainRoot, state)
	return b, e, state, ctx
}
func saveControlState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWorkerControlRootFromSubdirectoryDoesNotChangeCWD(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	sub := filepath.Join(e.Path, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Getwd()
	got, err := ResolveControl(ctx, sub)
	if err != nil || got != b.MainRoot {
		t.Fatalf("control=%q err=%v", got, err)
	}
	after, _ := os.Getwd()
	if before != after {
		t.Fatal("resolver changed process directory")
	}
	if err := RequireMain(sub); err == nil {
		t.Fatal("worker subdirectory accepted for main initialization")
	}
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		if _, err := os.Stat(filepath.Join(e.Path, ".claude", name)); !os.IsNotExist(err) {
			t.Fatal("resolver created secondary control state")
		}
	}
}
func TestWorkerControlRootRejectsUntrustedOrUnstableAuthority(t *testing.T) {
	for _, kind := range []string{"pending", "generation", "runtime", "main_root", "assignment", "target_branch", "duplicate_state", "duplicate_journal", "main_branch", "worker_branch"} {
		t.Run(kind, func(t *testing.T) {
			b, e, state, ctx := workerFixture(t)
			switch kind {
			case "pending":
				os.WriteFile(filepath.Join(b.MainRoot, ".claude/loop-state.json.commit-pending.json"), []byte("{}"), 0600)
			case "generation":
				state["baseline"] = map[string]any{"generation": 2}
				saveControlState(t, b.MainRoot, state)
			case "runtime":
				state["runtime_id"] = "loop-other"
				saveControlState(t, b.MainRoot, state)
			case "main_root":
				b.MainRoot = t.TempDir()
			case "assignment":
				e.AssignmentID = "different"
				b.Executions["assignment"] = e
			case "target_branch":
				e.TargetBranch = "test3"
				b.Executions["assignment"] = e
			case "duplicate_state":
				os.WriteFile(filepath.Join(e.Path, ".claude/loop-state.json"), []byte("{}"), 0600)
			case "duplicate_journal":
				os.WriteFile(filepath.Join(e.Path, ".claude/loop-events.jsonl"), nil, 0600)
			case "main_branch":
				Git(ctx, b.MainRoot, "switch", "-c", "test3")
			case "worker_branch":
				Git(ctx, e.Path, "switch", "-c", "other")
			}
			if kind == "main_root" || kind == "assignment" || kind == "target_branch" {
				data, _ := os.ReadFile(filepath.Join(e.Path, ".claude/loop-workspace.json"))
				var pointer map[string]any
				json.Unmarshal(data, &pointer)
				state["workspace"] = Encode(b)
				saveControlState(t, pointer["control_root"].(string), state)
			}
			if got, err := ResolveControl(ctx, e.Path); err == nil {
				t.Fatalf("accepted %s: %s", kind, got)
			}
			if kind == "pending" {
				if _, err := os.Stat(filepath.Join(b.MainRoot, ".claude/loop-state.json.commit-pending.json")); err != nil {
					t.Fatal("resolver repaired pending transaction implicitly")
				}
			}
		})
	}
}
func TestPointerLookupStopsAtNestedRepository(t *testing.T) {
	_, e, _, _ := workerFixture(t)
	nested := filepath.Join(e.Path, "nested")
	os.MkdirAll(filepath.Join(nested, ".git"), 0700)
	got, marked, err := PointerRoot(nested)
	if err != nil || marked || got != nested {
		t.Fatalf("inherited parent pointer across Git root: %s %v %v", got, marked, err)
	}
}
func TestDamagedPointerStillPreventsMainInitialization(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".claude"), 0700)
	os.WriteFile(filepath.Join(root, ".claude/loop-workspace.json"), []byte("{"), 0600)
	if err := RequireMain(root); err == nil || !strings.Contains(err.Error(), "second Runtime") {
		t.Fatalf("damaged pointer initialization: %v", err)
	}
}

func TestUncreatedDirectoryStillHonorsWorkerBoundary(t *testing.T) {
	root := t.TempDir()
	if err := RequireMain(filepath.Join(root, "new-project")); err != nil {
		t.Fatalf("new unmarked project rejected: %v", err)
	}
	os.Mkdir(filepath.Join(root, ".claude"), 0700)
	os.WriteFile(filepath.Join(root, ".claude/loop-workspace.json"), []byte("{"), 0600)
	if err := RequireMain(filepath.Join(root, "new", "subdir")); err == nil {
		t.Fatal("missing subdirectory bypassed Worker boundary")
	}
}
