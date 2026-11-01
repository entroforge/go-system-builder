package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestDomainChecksUseActualPassingReceiptsWithoutDigestOrOID(t *testing.T) {
	root := t.TempDir()
	e := workspace.Execution{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment", Checks: []string{"test product"}}
	path := filepath.Join(filepath.Dir(integration.CheckpointPath(root, e.RuntimeID, 1, e.AssignmentID)), "checks/receipt.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	cp := integration.Checkpoint{CheckReceipts: []string{path}}
	for _, kind := range []string{"failed", "missing-exit", "other-head", "other-command", "pass"} {
		t.Run(kind, func(t *testing.T) {
			r := map[string]any{"command": "test product", "cwd": root, "head_before": "tested-sha", "head_after": "tested-sha", "status": "pass", "exit_code": 0}
			switch kind {
			case "failed":
				r["status"] = "fail"
				r["exit_code"] = 1
			case "other-head":
				r["head_after"] = "other"
			case "missing-exit":
				delete(r, "exit_code")
			case "other-command":
				r["command"] = "fabricated"
			}
			data, _ := json.Marshal(r)
			os.WriteFile(path, data, 0600)
			checks, err := verifiedDomainChecks(root, e, cp)
			if kind != "pass" && kind != "other-head" {
				if err == nil {
					t.Fatal("unverified check promoted")
				}
				return
			}
			if err != nil || len(checks) != 1 || strings.Contains(checks[0].EvidenceRefs[0], "sha256") {
				t.Fatalf("checks=%+v err=%v", checks, err)
			}
		})
	}
}

func TestWorkspaceMainSelectsActualChecksAndRejectsFailure(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		name := "pass"
		if !succeeds {
			name = "fail"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			git := func(dir string, args ...string) string {
				t.Helper()
				out, err := workspace.Git(ctx, dir, args...)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			git(root, "init", "-b", "main")
			git(root, "config", "user.name", "Test")
			git(root, "config", "user.email", "test@example.invalid")
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n.worktrees/\n"), 0600); err != nil {
				t.Fatal(err)
			}
			git(root, "add", ".gitignore")
			git(root, "commit", "-m", "baseline")
			git(root, "rev-parse", "HEAD")
			worker := filepath.Join(root, ".worktrees/worker")
			git(root, "worktree", "add", "-b", "worker", worker)
			if err := os.WriteFile(filepath.Join(worker, "product.txt"), []byte("fixed\n"), 0600); err != nil {
				t.Fatal(err)
			}
			git(worker, "add", "product.txt")
			git(worker, "commit", "-m", "repair")
			e := workspace.Execution{RuntimeID: "loop-main-check", AssignmentID: "assignment-main-check", BaselineGeneration: 1, Path: worker, Branch: "worker", TargetBranch: "main", Checks: []string{"historical precheck"}}
			if _, err := workspaceMainCheckCommands(nil, nil, integration.Checkpoint{}); err == nil {
				t.Fatal("missing checks did not stay pending")
			}
			command := "test -f product.txt && printf 'actual Main output\\n'"
			if !succeeds {
				command = "printf 'actual Main failure\\n'; exit 7"
			}
			commands, err := workspaceMainCheckCommands(e.Checks, []string{command}, integration.Checkpoint{})
			if err != nil || len(commands) != 1 || commands[0] != command {
				t.Fatalf("Main selection=%v %v", commands, err)
			}
			inspection, err := integration.Inspect(ctx, integration.InspectRequest{Root: root, RuntimeID: e.RuntimeID, BaselineGeneration: 1, TargetBranch: "main", Assignment: hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: "builder", WorktreePath: worker, Branch: "worker", TargetBranch: "main", WritePaths: []string{"product.txt"}}}, integration.InspectConfig{SkipCompletionCheck: true})
			if err != nil || !inspection.Ready {
				t.Fatalf("inspection=%+v %v", inspection, err)
			}
			integrated, runErr := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: commands, CheckRunner: integration.CommandCheckRunner})
			cp := integrated.Checkpoint
			mainExecution := e
			mainExecution.Checks = commands
			checks, checksErr := verifiedDomainChecks(root, mainExecution, cp)
			if !succeeds {
				if runErr == nil || cp.State == integration.StateVerified || checksErr == nil {
					t.Fatalf("failed real command completed: state=%s run=%v checks=%v", cp.State, runErr, checksErr)
				}
				return
			}
			if runErr != nil || checksErr != nil || cp.State != integration.StateVerified || len(checks) != 1 || checks[0].Command != command {
				t.Fatalf("actual Main checks=%+v cp=%+v errors=%v %v", checks, cp, runErr, checksErr)
			}
			data, err := os.ReadFile(cp.CheckReceipts[0])
			if err != nil {
				t.Fatal(err)
			}
			var executed map[string]any
			if err := json.Unmarshal(data, &executed); err != nil {
				t.Fatal(err)
			}
			output, err := os.ReadFile(executed["output"].(string))
			if err != nil || !strings.Contains(string(output), "actual Main output") {
				t.Fatalf("real output=%s %v", output, err)
			}
		})
	}
}
