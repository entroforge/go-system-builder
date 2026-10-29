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
	bound["sha256"] = sha256Hex(req)
	runGit(t, root, "checkout", "-B", "develop")
	runGit(t, root, "add", "docs/control/loop-definition.json", reqPath)
	runGit(t, root, "commit", "-m", "Pin existing budget fixture authority")
	runGit(t, root, "branch", "-f", "main", "develop")
	bound["workspace"] = map[string]any{"project_root": root, "dev_branch": "develop", "release_upstream": "main", "bound_commit": recheckGitOutput(t, root, "rev-parse", "HEAD")}
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

func budgetApplicationFromEntry(t *testing.T, root string) (string, map[string]any) {
	t.Helper()
	output := budgetHook(t, root, "SessionStart", "")
	paths, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s7-budget-*.application.json"))
	if err != nil || len(paths) != 1 || !strings.Contains(output, paths[0]) {
		t.Fatalf("actual SessionStart entry did not prepare and supply exactly one application: %v %v\n%s", paths, err, output)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var app map[string]any
	if err := json.Unmarshal(data, &app); err != nil {
		t.Fatal(err)
	}
	if app["decision"] != "" || app["new_max_full_review_rounds"] != nil || app["reason"] != "" {
		t.Fatalf("entry prepared a human choice: %v", app)
	}
	return paths[0], app
}

func saveBudgetProfessionalChoice(t *testing.T, root, path string, app map[string]any) string {
	t.Helper()
	data, _ := json.Marshal(app)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return budgetHook(t, root, "PostToolUse", path)
}

func TestS7BudgetNormalApplicationRecordsTwoActualHumanChoicesAndReadOnlyReplay(t *testing.T) {
	for _, choice := range []string{"increase_budget", "return_to_governance"} {
		t.Run(choice, func(t *testing.T) {
			root := s7BudgetHookRoot(t)
			path, app := budgetApplicationFromEntry(t, root)
			// The actual stage entry supplies every machine value. The author
			// changes only these professional fields, never copies Runtime.
			app["decision"], app["reason"] = choice, "The human explicitly chose this scope and budget disposition."
			if choice == "increase_budget" {
				app["new_max_full_review_rounds"] = 8
			}
			output := saveBudgetProfessionalChoice(t, root, path, app)
			statePath := filepath.Join(root, ".claude", "loop-state.json")
			state := readS10QuestionnaireState(t, statePath)
			repair := state["configuration"].(map[string]any)["repair"].(map[string]any)
			if len(state["evidence"].([]any)) != 1 {
				t.Fatalf("normal professional choice did not reach the original Writer: %s", output)
			}
			if choice == "increase_budget" {
				if repair["max_full_review_rounds"] != float64(8) || !strings.Contains(output, "TR-012") {
					t.Fatalf("increase did not expose the original pending successor: %s", output)
				}
			} else if state["lifecycle"].(map[string]any)["state"] != "planning" || repair["max_full_review_rounds"] != float64(5) || state["review"].(map[string]any)["round"] != float64(0) {
				t.Fatalf("governance did not follow original GTR-006: %s\n%v", output, state["lifecycle"])
			}
			stateBefore, _ := os.ReadFile(statePath)
			journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
			journalBefore, _ := os.ReadFile(journalPath)
			output = budgetHook(t, root, "PostToolUse", path)
			stateAfter, _ := os.ReadFile(statePath)
			journalAfter, _ := os.ReadFile(journalPath)
			if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) || !strings.Contains(output, "Read-only replay") {
				t.Fatalf("accepted old application regranted budget or repeated exit: %s", output)
			}
		})
	}
}

func TestS7BudgetApplicationKeepsDraftAndRejectsWrongOrStaleInputs(t *testing.T) {
	for _, fault := range []string{"empty", "no-increase", "missing-new-max", "missing-reason", "wrong-old-max", "wrong-round", "wrong-runtime", "changed-limit"} {
		t.Run(fault, func(t *testing.T) {
			root := s7BudgetHookRoot(t)
			path, app := budgetApplicationFromEntry(t, root)
			if fault != "empty" {
				app["decision"], app["new_max_full_review_rounds"], app["reason"] = "increase_budget", 8, "Actual human reason."
			}
			switch fault {
			case "no-increase":
				app["new_max_full_review_rounds"] = 5
			case "missing-new-max":
				app["new_max_full_review_rounds"] = nil
			case "missing-reason":
				app["reason"] = ""
			case "wrong-old-max":
				app["previous_max_full_review_rounds"] = 4
			case "wrong-round":
				app["review_round"] = 4
			case "wrong-runtime":
				app["runtime_id"] = "another-runtime"
			case "changed-limit":
				statePath := filepath.Join(root, ".claude", "loop-state.json")
				state := readS10QuestionnaireState(t, statePath)
				state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = 6
				data, _ := json.Marshal(state)
				if err := os.WriteFile(statePath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			output := saveBudgetProfessionalChoice(t, root, path, app)
			state := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
			if len(state["evidence"].([]any)) != 0 || state["lifecycle"].(map[string]any)["state"] != "bug_resolution" {
				t.Fatalf("invalid/draft budget decision changed authority: %s", output)
			}
			if !strings.Contains(output, "S7 budget decision") {
				t.Fatalf("exact original application was not handled: %s", output)
			}
		})
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
				if cursor == "acceptance" && strings.Contains(out.String(), "s7_budget_gateway") {
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

func TestS7BudgetActualAcceptanceReviewRequiredReceivesHumanIncreaseThenTR016(t *testing.T) {
	root, answer, _ := s10ReviewRequiredRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = 1
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	// The actual S10 report receiver creates the original current-scope
	// acceptance envelope; this test does not prefill a budget DTO or receipt.
	output := saveS10ReviewAnswer(t, root, answer)
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "acceptance" || !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") {
		t.Fatalf("real negative acceptance was bypassed or lost: %s", output)
	}
	paths, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s7-budget-*.application.json"))
	if err != nil || len(paths) != 1 || !strings.Contains(output, paths[0]) {
		t.Fatalf("actual accepted review_required did not supply budget application: %v %v\n%s", paths, err, output)
	}
	app := readS10QuestionnaireState(t, paths[0])
	app["decision"], app["new_max_full_review_rounds"], app["reason"] = "increase_budget", 2, "The human authorized one further complete review for the actual acceptance finding."
	output = saveBudgetProfessionalChoice(t, root, paths[0], app)
	if !strings.Contains(output, "TR-016") {
		t.Fatalf("actual acceptance continuation was not precise: %s", output)
	}
	output = callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "verification" || state["review"].(map[string]any)["round"] != float64(2) {
		t.Fatalf("authorized original TR-016 did not open its actual new review: %s", output)
	}
}
