package transition

// These audit cases exercise the registered S3/S5 transition actions against
// real Git trees. They deliberately keep the runtime map small: the question
// under audit is whether the formal action source, registration, and freeze
// boundary agree on the same committed model closure.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type auditSharedRepo struct {
	root string
}

func newAuditSharedRepo(t *testing.T, withDependency bool) *auditSharedRepo {
	t.Helper()
	r := &auditSharedRepo{root: t.TempDir()}
	auditSharedGit(t, r.root, "init", "-q", "-b", "dev")
	auditSharedGit(t, r.root, "config", "user.name", "shared-model-audit")
	auditSharedGit(t, r.root, "config", "user.email", "shared-model-audit@example.test")
	auditSharedGit(t, r.root, "config", "commit.gpgsign", "false")

	copyAuditSharedExample(t, r.root)
	if withDependency {
		writeAuditSharedDependency(t, r.root)
	}
	if err := os.MkdirAll(filepath.Join(r.root, "docs", "dev", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.root, "docs", "dev", "tasks", "TASK-AUDIT.md"), []byte("# TASK-AUDIT\n\n> Status: complete\n> Version: v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	auditSharedGit(t, r.root, "add", ".")
	auditSharedGit(t, r.root, "commit", "-qm", "shared model baseline")
	return r
}

func copyAuditSharedExample(t *testing.T, root string) {
	t.Helper()
	src := filepath.Join("..", "..", "docs", "examples", "shared-model", "project")
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeAuditSharedDependency(t *testing.T, root string) {
	t.Helper()
	dependency := `{"type":"object","required":["order_id"],"additionalProperties":false,"properties":{"order_id":{"type":"string","minLength":1}}}`
	if err := os.WriteFile(filepath.Join(root, "docs", "architecture", "data-model", "request-fragment.json"), []byte(dependency), 0o644); err != nil {
		t.Fatal(err)
	}
	rootSchema := `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$defs": {
    "request": {"$ref": "request-fragment.json"},
    "response": {"type": "object", "required": ["state"], "additionalProperties": false, "properties": {"state": {"enum": ["cancelled", "completed"]}}}
  }
}
`
	if err := os.WriteFile(filepath.Join(root, "docs", "architecture", "data-model", "orders.schema.json"), []byte(rootSchema), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *auditSharedRepo) view(t *testing.T) *fileview.View {
	t.Helper()
	view, err := fileview.New(r.root, "refs/heads/dev", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func auditSharedState(root string) map[string]any {
	return map[string]any{
		"root":       root,
		"baseline":   map[string]any{"generation": 1},
		"documents":  []any{},
		"bound_req":  map[string]any{"id": "REQ-001"},
		"lifecycle":  map[string]any{"state": "planning", "phase": "contracts"},
		"runtime_id": "loop-shared-model-audit",
	}
}

func auditSharedContext(root string, view fileview.Reader, evidence bool) *ActionContext {
	ctx := &ActionContext{
		Root:       root,
		OccurredAt: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
		Request:    &Request{Actor: "shared-model-audit", Files: view},
	}
	if evidence {
		ctx.Evidence = map[string]string{"execution_batch": "audit"}
	}
	return ctx
}

func auditSharedAction(t *testing.T, name string, state map[string]any, ctx *ActionContext) ActionResult {
	t.Helper()
	fn, ok := LookupAction(name)
	if !ok {
		t.Fatalf("formal transition action %q is not registered", name)
	}
	result, err := fn(state, ctx)
	if err != nil {
		t.Fatalf("formal transition action %s failed: %v", name, err)
	}
	if result.Status != "committed" {
		t.Fatalf("formal transition action %s status=%q detail=%q", name, result.Status, result.Detail)
	}
	return result
}

func auditSharedRegister(t *testing.T, repo *auditSharedRepo) (map[string]any, *fileview.View) {
	t.Helper()
	state := auditSharedState(repo.root)
	view := repo.view(t)
	auditSharedAction(t, "register_locked_contracts", state, auditSharedContext(repo.root, view, false))
	return state, view
}

func sharedModelPaths(state map[string]any) map[string]string {
	out := map[string]string{}
	documents, _ := state["documents"].([]any)
	for _, raw := range documents {
		doc, _ := raw.(map[string]any)
		id, _ := doc["id"].(string)
		if strings.HasPrefix(id, "shared-model:") {
			path, _ := doc["path"].(string)
			sha, _ := doc["sha256"].(string)
			out[path] = sha
		}
	}
	return out
}

func auditSharedGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestAuditSharedModelsS3ToS5UsesCommittedGitTree covers the formal action
// registry from S3 registration through the S5 freeze action. A worker-only
// schema is referenced by committed contract text but exists only in the
// worktree; the git_tree source must refuse to use it.
func TestAuditSharedModelsS3ToS5UsesCommittedGitTree(t *testing.T) {
	t.Run("committed closure registers and freezes", func(t *testing.T) {
		repo := newAuditSharedRepo(t, false)
		state, view := auditSharedRegister(t, repo)
		if got := len(sharedModelPaths(state)); got != 4 {
			t.Fatalf("S3 registration captured %d model files, want 4", got)
		}
		auditSharedAction(t, "register_execution_batch", state, auditSharedContext(repo.root, view, true))
	})

	t.Run("worker-only schema cannot satisfy formal source", func(t *testing.T) {
		repo := newAuditSharedRepo(t, false)
		for _, name := range []string{"CONTRACTS-001.md", "FE-001.md", "BE-001.md", "SYNC-001.md"} {
			path := filepath.Join(repo.root, "docs", "dev", "contracts", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.ReplaceAll(string(data), "orders.schema.json", "worker-only.json"))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		workerSchema := `{"$defs":{"request":{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}}},"response":{"type":"object"}}}`
		workerPath := filepath.Join(repo.root, "docs", "architecture", "data-model", "worker-only.json")
		if err := os.WriteFile(workerPath, []byte(workerSchema), 0o644); err != nil {
			t.Fatal(err)
		}
		auditSharedGit(t, repo.root, "add", "docs/dev/contracts")
		auditSharedGit(t, repo.root, "commit", "-qm", "reference worker-only model without delivering it")
		if status := auditSharedGit(t, repo.root, "status", "--porcelain", "--untracked-files=all"); !strings.Contains(status, "worker-only.json") {
			t.Fatalf("worker-only schema unexpectedly tracked or absent from worktree: status=%q", status)
		}
		state := auditSharedState(repo.root)
		view := repo.view(t)
		fn, ok := LookupAction("register_locked_contracts")
		if !ok {
			t.Fatal("register_locked_contracts action is not registered")
		}
		result, err := fn(state, auditSharedContext(repo.root, view, false))
		if err == nil || !strings.Contains(err.Error(), "schema") {
			t.Fatalf("formal S3 accepted worker-only schema: result=%+v err=%v", result, err)
		}
		if len(sharedModelPaths(state)) != 0 {
			t.Fatalf("failed S3 registration mutated model documents: %#v", sharedModelPaths(state))
		}
	})
}

// TestAuditSharedModelsFreezeRejectsTransitiveClosureChanges checks content
// drift plus both closure directions. Every changed closure is rejected until
// S3 explicitly re-registers the current generation, after which S5 freezes it.
func TestAuditSharedModelsFreezeRejectsTransitiveClosureChanges(t *testing.T) {
	tests := []struct {
		name           string
		baselineDeps   bool
		mutate         func(t *testing.T, root string)
		wantModelFiles int
	}{
		{
			name:         "transitive dependency content drift",
			baselineDeps: true,
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "docs", "architecture", "data-model", "request-fragment.json")
				if err := os.WriteFile(path, []byte(`{"type":"object","required":["order_id"],"additionalProperties":false,"properties":{"order_id":{"type":"string","minLength":2}}}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantModelFiles: 5,
		},
		{
			name:         "dependency closure removal",
			baselineDeps: true,
			mutate: func(t *testing.T, root string) {
				inline := `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$defs": {
    "request": {"type":"object","required":["order_id"],"additionalProperties":false,"properties":{"order_id":{"type":"string","minLength":1}}},
    "response": {"type":"object","required":["state"],"additionalProperties":false,"properties":{"state":{"enum":["cancelled","completed"]}}}
  }
}
`
				if err := os.WriteFile(filepath.Join(root, "docs", "architecture", "data-model", "orders.schema.json"), []byte(inline), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, "docs", "architecture", "data-model", "request-fragment.json")); err != nil {
					t.Fatal(err)
				}
			},
			wantModelFiles: 4,
		},
		{
			name:         "dependency closure addition",
			baselineDeps: false,
			mutate: func(t *testing.T, root string) {
				writeAuditSharedDependency(t, root)
			},
			wantModelFiles: 5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newAuditSharedRepo(t, tc.baselineDeps)
			state, _ := auditSharedRegister(t, repo)
			if got := len(sharedModelPaths(state)); got != 4 && got != 5 {
				t.Fatalf("unexpected initial model closure size: %d", got)
			}
			tc.mutate(t, repo.root)
			auditSharedGit(t, repo.root, "add", "docs/architecture/data-model")
			auditSharedGit(t, repo.root, "commit", "-qm", "change shared model closure")
			changedView := repo.view(t)
			fn, ok := LookupAction("register_execution_batch")
			if !ok {
				t.Fatal("register_execution_batch action is not registered")
			}
			result, err := fn(state, auditSharedContext(repo.root, changedView, true))
			if err == nil || !strings.Contains(err.Error(), "shared model") {
				t.Fatalf("S5 accepted changed model closure: result=%+v err=%v", result, err)
			}

			// Returning to S3 is the only legal way to replace this generation's
			// closure. The same formal action then permits S5 to freeze it.
			auditSharedAction(t, "register_locked_contracts", state, auditSharedContext(repo.root, changedView, false))
			if got := len(sharedModelPaths(state)); got != tc.wantModelFiles {
				t.Fatalf("re-registration captured %d model files, want %d", got, tc.wantModelFiles)
			}
			auditSharedAction(t, "register_execution_batch", state, auditSharedContext(repo.root, changedView, true))
		})
	}
}

// TestAuditSharedModelsDifferentCheckoutSameClosure verifies that design
// identity is the referenced closure, not the whole checkout commit.
func TestAuditSharedModelsDifferentCheckoutSameClosure(t *testing.T) {
	repo := newAuditSharedRepo(t, true)
	state, baselineView := auditSharedRegister(t, repo)
	baselineCommit := baselineView.Commit

	other := filepath.Join(t.TempDir(), "other-checkout")
	auditSharedGit(t, repo.root, "worktree", "add", "-b", "audit-other", other, "dev")
	if err := os.WriteFile(filepath.Join(other, "unrelated-implementation.txt"), []byte("implementation-only change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	auditSharedGit(t, other, "add", "unrelated-implementation.txt")
	auditSharedGit(t, other, "commit", "-qm", "unrelated implementation change")
	otherView, err := fileview.New(other, "refs/heads/audit-other", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	if otherView.Commit == baselineCommit {
		t.Fatalf("independent checkout did not advance commit: baseline=%s other=%s", baselineCommit, otherView.Commit)
	}
	otherState := auditSharedState(other)
	otherState["documents"] = state["documents"]
	auditSharedAction(t, "register_execution_batch", otherState, auditSharedContext(other, otherView, true))
}
