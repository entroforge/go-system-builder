package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Upstream clean review is synthetic, as in the original S10 fixture. The
// report receiver, evidence writer, TR-016 and new S7 plan receiver are real.
func s10ReviewRequiredRoot(t *testing.T) (string, string, string) {
	t.Helper()
	root, answer := s10QuestionnaireHookRoot(t)
	// Use the actual repository installation's runtime exclusion policy.
	ignore, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	writeS10QuestionnaireFile(t, root, ".git/info/exclude", ignore)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	ptr := state["review"].(map[string]any)["plan"].(map[string]any)
	planRef := ptr["path"].(string)
	plan := readS10QuestionnaireState(t, filepath.Join(root, planRef))
	product, err := os.ReadFile(filepath.Join(root, "internal/service.go"))
	if err != nil {
		t.Fatal(err)
	}
	plan["frozen_subjects"] = []any{map[string]any{"path": "internal/service.go", "sha256": sha256Hex(product), "kind": "product_code"}}
	data, _ := json.Marshal(plan)
	writeS10QuestionnaireFile(t, root, planRef, data)
	ptr["sha256"] = sha256Hex(data)
	data, _ = json.Marshal(state)
	writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", data)
	fillS10QuestionnaireAnswer(t, answer, "acceptance", "review_required")
	return root, answer, planRef
}

func saveS10ReviewAnswer(t *testing.T, root, path string) string {
	t.Helper()
	return callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"file_path": path}})
}

func TestS10ReviewRequiredReturnsOriginalFullReviewAndNewPlan(t *testing.T) {
	root, answer, _ := s10ReviewRequiredRoot(t)
	statePath, journal := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	for _, item := range []struct{ id, scope string }{{"affected-pass", "internal/service.go"}, {"unrelated-pass", "internal/other.go"}} {
		ref := "docs/reports/" + item.id + ".json"
		body, _ := json.Marshal(map[string]any{"schema_version": "1.0.0", "evidence_id": item.id, "kind": "document_review", "runtime_id": "loop-test", "baseline_generation": 1, "producer_agent_id": "qa", "producer_responsibility": "QA", "conclusion": "pass"})
		writeS10QuestionnaireFile(t, root, ref, body)
		if _, err := runtime.RecordEvidence(root, statePath, journal, runtime.EvidenceRequest{ExpectedRevision: -1, ID: item.id, Kind: "document_review", Path: ref, ProducedBy: []string{"qa"}, ResponsibilityID: "QA", ScopeRefs: []string{item.scope}, Validator: semantic.RuntimeCandidateValidator{}}); err != nil {
			t.Fatal(err)
		}
	}
	out := saveS10ReviewAnswer(t, root, answer)
	state := readS10QuestionnaireState(t, statePath)
	lifecycle, review := state["lifecycle"].(map[string]any), state["review"].(map[string]any)
	if lifecycle["state"] != "verification" || lifecycle["phase"] != "planned" || review["round"] != float64(2) || review["clean_round"] != nil {
		t.Fatalf("received negative decision did not return through TR-016: %v %v %s", lifecycle, review["round"], out)
	}
	if !strings.Contains(out, "TR-016") {
		t.Fatalf("return not reported: %s", out)
	}
	var impact int
	for _, raw := range state["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == "affected-pass" && row["status"] != "invalid" {
			t.Fatal("affected PASS survived")
		}
		if row["id"] == "unrelated-pass" && row["status"] != "valid" {
			t.Fatal("unrelated scope invalidated")
		}
		if row["kind"] == "change_impact" {
			impact++
			body := readS10QuestionnaireState(t, filepath.Join(root, row["path"].(string)))
			if body["changed_artifacts"] != nil {
				t.Fatal("reverification fabricated product changes")
			}
		}
	}
	if impact != 1 || !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") {
		t.Fatalf("original report/impact lost: %d", impact)
	}
	planPath := filepath.Join(root, ".claude/submissions/loop-test/g1/S7/r2/review-plan.json")
	if !strings.Contains(out, planPath) {
		t.Fatalf("missing exact next S7 responsibility: %s", out)
	}
	if _, err := os.Stat(planPath); err != nil {
		t.Fatal(err)
	}
	// Supply simulated professional content to that exact existing S7 form.
	professional := minimalReviewPlan(t, t.TempDir(), "review-plan-after-acceptance")
	plan := readS10QuestionnaireState(t, professional)
	generated := readS10QuestionnaireState(t, planPath)
	plan["coverage_inventory"] = generated["coverage_inventory"]
	plan["review_round"] = 2
	product, _ := os.ReadFile(filepath.Join(root, "internal/service.go"))
	plan["frozen_subjects"] = []any{map[string]any{"path": "internal/service.go", "sha256": sha256Hex(product), "kind": "product_code"}}
	for _, raw := range plan["claims"].([]any) {
		claim := raw.(map[string]any)
		claim["source_refs"] = []string{"REQ-001", "CONTRACT-001", "internal/service.go"}
		if claim["lens"] == "e2e" {
			claim["na_checklist_id"] = "REQ-001#ui_impact"
		}
	}
	data, _ := json.Marshal(plan)
	writeS10QuestionnaireFile(t, root, strings.TrimPrefix(planPath, root+string(filepath.Separator)), data)
	unknown := "s10/loop-test/g1/r1/unregistered.json"
	writeS10QuestionnaireFile(t, root, unknown, []byte("{}"))
	feedback := saveS10ReviewAnswer(t, root, planPath)
	if !strings.Contains(feedback, unknown) {
		t.Fatalf("unknown sibling was silently excluded from drift: %s", feedback)
	}
	if err := os.Remove(filepath.Join(root, unknown)); err != nil {
		t.Fatal(err)
	}
	if feedback := saveS10ReviewAnswer(t, root, planPath); !strings.Contains(feedback, "review-plan-after-acceptance") {
		t.Fatalf("original new S7 plan did not register: %s", feedback)
	}
	state = readS10QuestionnaireState(t, statePath)
	if state["review"].(map[string]any)["plan"].(map[string]any)["review_round"] != float64(2) {
		t.Fatal("old plan reused as current")
	}
}

func TestS10ReviewRequiredRestoresSourceWithoutRefillingReceivedAnswer(t *testing.T) {
	root, answer, planRef := s10ReviewRequiredRoot(t)
	planPath := filepath.Join(root, planRef)
	original, _ := os.ReadFile(planPath)
	writeS10QuestionnaireFile(t, root, planRef, []byte("broken original source"))
	out := saveS10ReviewAnswer(t, root, answer)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	if !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") || !strings.Contains(out, planPath) || !strings.Contains(out, "Main") {
		t.Fatalf("missing source misattributed: %s", out)
	}
	before, _ := os.ReadFile(statePath)
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("passive Pre wrote return facts")
	}
	writeS10QuestionnaireFile(t, root, planRef, original)
	// Normal Main observation resumes the original consumer. No report save.
	out = callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": planPath}})
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatalf("restored source still spins: %s", out)
	}
	var count int
	for _, raw := range state["evidence"].([]any) {
		if raw.(map[string]any)["kind"] == "acceptance" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("received professional report registered again: %d", count)
	}
}

func TestS10ReviewRequiredRejectsUnavailableOrWrongScopeWithoutInventingImpact(t *testing.T) {
	for _, fault := range []string{"missing_plan", "missing_file", "old_round", "wrong_generation", "empty_scope"} {
		t.Run(fault, func(t *testing.T) {
			root, answer, planRef := s10ReviewRequiredRoot(t)
			statePath := filepath.Join(root, ".claude/loop-state.json")
			state := readS10QuestionnaireState(t, statePath)
			ptr := state["review"].(map[string]any)["plan"].(map[string]any)
			switch fault {
			case "missing_plan":
				state["review"].(map[string]any)["plan"] = nil
			case "missing_file":
				if err := os.Remove(filepath.Join(root, planRef)); err != nil {
					t.Fatal(err)
				}
			default:
				plan := readS10QuestionnaireState(t, filepath.Join(root, planRef))
				switch fault {
				case "old_round":
					plan["review_round"] = 2
				case "wrong_generation":
					plan["baseline_generation"] = 2
				case "empty_scope":
					plan["frozen_subjects"] = []any{}
				}
				data, _ := json.Marshal(plan)
				writeS10QuestionnaireFile(t, root, planRef, data)
				ptr["sha256"] = sha256Hex(data)
			}
			data, _ := json.Marshal(state)
			writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", data)
			out := saveS10ReviewAnswer(t, root, answer)
			state = readS10QuestionnaireState(t, statePath)
			if state["lifecycle"].(map[string]any)["state"] != "acceptance" || !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") || !strings.Contains(out, "Main") || !strings.Contains(out, "received") {
				t.Fatalf("source gap lost or completed author sent in circles: %s", out)
			}
			for _, raw := range state["evidence"].([]any) {
				if raw.(map[string]any)["kind"] == "change_impact" {
					t.Fatal("missing source manufactured impact")
				}
			}
		})
	}
}

func TestS10ReviewRequiredPendingReturnDoesNotReceiveChangedOrWorkerAnswer(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "unsubmitted_change"}[changed], func(t *testing.T) {
			root, answer, planRef := s10ReviewRequiredRoot(t)
			original, _ := os.ReadFile(filepath.Join(root, planRef))
			writeS10QuestionnaireFile(t, root, planRef, []byte("broken original source"))
			saveS10ReviewAnswer(t, root, answer)
			writeS10QuestionnaireFile(t, root, planRef, original)
			request := map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, planRef)}}
			if changed {
				manifest := readS10QuestionnaireState(t, answer)
				manifest["systemic_analysis"] = "Unsubmitted changed professional content."
				data, _ := json.Marshal(manifest)
				if err := os.WriteFile(answer, data, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				request["agent_id"] = "some-worker"
			}
			statePath := filepath.Join(root, ".claude/loop-state.json")
			before, _ := os.ReadFile(statePath)
			callS10QuestionnaireHook(t, root, request)
			after, _ := os.ReadFile(statePath)
			if !bytes.Equal(before, after) {
				t.Fatal("unrelated tool received changed work or Worker consumed Main continuation")
			}
		})
	}
}

func TestS10ReviewRequiredBudgetRecoveryKeepsOriginalScopeAndOneReport(t *testing.T) {
	root, answer, _ := s10ReviewRequiredRoot(t)
	statePath, journal := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	state := readS10QuestionnaireState(t, statePath)
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = 1
	data, _ := json.Marshal(state)
	writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", data)
	out := saveS10ReviewAnswer(t, root, answer)
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "acceptance" || !strings.Contains(out, "budget") {
		t.Fatalf("original budget bypassed or no recovery: %s", out)
	}
	decision := runtime.S7BudgetDecision{Decision: "increase_budget", RuntimeID: "loop-test", ReviewRound: 1, PreviousMaxFullReviewRounds: 1, NewMaxFullReviewRounds: 2, Reason: "SIMULATION: authorize one further full review for this fixture", AuthorizedBy: "fixture-human"}
	data, _ = json.Marshal(decision)
	ref := "docs/reports/synthetic-budget-decision.json"
	writeS10QuestionnaireFile(t, root, ref, data)
	if _, err := runtime.ApplyS7BudgetDecision(root, statePath, journal, runtime.S7BudgetDecisionRequest{ExpectedRevision: -1, DecisionPath: ref, Actor: "fixture-human", Validator: semantic.RuntimeCandidateValidator{}}); err != nil {
		t.Fatal(err)
	}
	// Pre remains read-only after the original budget writer. Normal Main
	// completion resumes from the recorded scope without another report save.
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": answer}})
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatal("passive Pre wrote the S10 return")
	}
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": answer}})
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatal("original budget decision did not release the pending return")
	}
	var reports, impacts int
	for _, raw := range state["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "acceptance" {
			reports++
		}
		if row["kind"] == "change_impact" {
			impacts++
		}
	}
	if reports != 1 || impacts != 1 {
		t.Fatalf("budget recovery duplicated original facts: %d/%d", reports, impacts)
	}
}
