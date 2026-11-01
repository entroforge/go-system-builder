package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func s7BudgetHookRoot(t *testing.T) string {
	t.Helper()
	root := worktreeProjectRoot(t)
	state := writeS7BudgetCLIState(t, root, "bug_resolution", "ready_for_full_review", 5, 5)
	bound := state["bound_req"].(map[string]any)
	reqPath := bound["path"].(string)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, reqPath)), 0700); err != nil {
		t.Fatal(err)
	}
	req := []byte("# Existing bound requirement for budget decision fixture\n")
	if err := os.WriteFile(filepath.Join(root, reqPath), req, 0600); err != nil {
		t.Fatal(err)
	}
	delete(bound, "sha256")
	runGit(t, root, "checkout", "-B", "develop")
	runGit(t, root, "add", "docs/control/loop-definition.json", reqPath)
	runGit(t, root, "commit", "-m", "Pin existing budget fixture authority")
	runGit(t, root, "branch", "-f", "main", "develop")
	bound["workspace"] = map[string]any{"project_root": root, "dev_branch": "develop", "release_upstream": "main"}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func budgetHook(t *testing.T, root, event, path string) string {
	t.Helper()
	input := map[string]any{"hook_event_name": event, "cwd": root, "session_id": "s7-budget-normal-test", "tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"file_path": path}}
	data, _ := json.Marshal(input)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(data), &stdout, &stderr); code != 0 {
		t.Fatalf("budget Hook %s exit=%d stdout=%s stderr=%s", event, code, stdout.String(), stderr.String())
	}
	return stdout.String() + stderr.String()
}

func TestS7BudgetLegacyNumbersDoNotPrepareHumanApplications(t *testing.T) {
	root := s7BudgetHookRoot(t)
	for _, event := range []string{"SessionStart", "PreToolUse", "PostToolUse", "Stop"} {
		output := budgetHook(t, root, event, "")
		if strings.Contains(output, "budget is exhausted") || strings.Contains(output, "s7_budget_gateway") {
			t.Fatalf("legacy numbers created a budget gate: %s", output)
		}
		paths, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s7-budget-*.application.json"))
		if err != nil || len(paths) != 0 {
			t.Fatalf("%s created a budget application: %v %v", event, paths, err)
		}
	}
}

func TestS7BudgetStatusDoesNotPrepareDecisionOrGateNormalAcceptance(t *testing.T) {
	for _, cursor := range []string{"bug_resolution", "acceptance"} {
		t.Run(cursor, func(t *testing.T) {
			root := t.TempDir()
			phase := "ready_for_full_review"
			if cursor == "acceptance" {
				phase = ""
			}
			writeS7BudgetCLIState(t, root, cursor, phase, 5, 5)
			for _, command := range []string{"next", "status"} {
				var out, diagnostic bytes.Buffer
				if code := cli.Run([]string{command, "--root", root}, nil, &out, &diagnostic); code != 0 {
					t.Fatal(code, out.String(), diagnostic.String())
				}
				if strings.Contains(out.String(), "s7_budget_gateway") {
					t.Fatalf("normal acceptance with no request for another round was human budget gated: %s", out.String())
				}
			}
			var board, diagnostic bytes.Buffer
			if code := cli.Run([]string{"s7", "status", "--root", root}, nil, &board, &diagnostic); code != 0 {
				t.Fatal(code, board.String(), diagnostic.String())
			}
			if strings.Contains(board.String(), "s7-budget-decision --file") || strings.Contains(board.String(), "--actor") {
				t.Fatalf("status still asks the author to build a decision DTO: %s", board.String())
			}
			if paths, _ := filepath.Glob(filepath.Join(root, ".claude", "decisions", "*.application.json")); len(paths) != 0 {
				t.Fatalf("read-only status created decision applications: %v", paths)
			}
		})
	}
}
