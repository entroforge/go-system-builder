package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
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
	// The Plan identity of the round being returned from, captured before the
	// answer is saved. The next round must be a different Plan, never the same
	// one re-opened.
	beforeState := readS10QuestionnaireState(t, statePath)
	previousPlanID, _ := beforeState["review"].(map[string]any)["plan"].(map[string]any)["plan_id"].(string)
	out := saveS10ReviewAnswer(t, root, answer)
	state := readS10QuestionnaireState(t, statePath)
	lifecycle, review := state["lifecycle"].(map[string]any), state["review"].(map[string]any)
	// The round coordinate is a stage coordinate bound to the entering
	// transition, not a counter. L3-S7-verification-round.md:205 forbids asking
	// for "review.round" or a round count; the current carrier is the saved
	// entry coordinate (internal/transition/actions.go:263).
	entry, ok := review["round_entry"].(map[string]any)
	if !ok {
		t.Fatalf("TR-016 did not save the next S7 entry coordinate: %v %s", review, out)
	}
	if lifecycle["state"] != "verification" || lifecycle["phase"] != "planned" || review["clean_round"] != nil {
		t.Fatalf("received negative decision did not return through TR-016: %v %v %s", lifecycle, entry, out)
	}
	// Reverse assertion: the retired scalar round counter must not come back
	// as a gate or as a record.
	if _, exists := review["round"]; exists {
		t.Fatalf("retired review round counter came back: %v", review["round"])
	}
	if entry["transition_id"] != "TR-016" {
		t.Fatalf("the next S7 entry is not bound to the TR-016 return: %v", entry)
	}
	if entry["baseline_generation"] != state["baseline"].(map[string]any)["generation"] {
		t.Fatalf("the next S7 entry does not freeze the current baseline generation: %v", entry)
	}
	if !strings.Contains(out, "TR-016") {
		t.Fatalf("return not reported: %s", out)
	}
	var impact int
	var impactRef string
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
			impactRef, _ = row["id"].(string)
			body := readS10QuestionnaireState(t, filepath.Join(root, row["path"].(string)))
			if body["changed_artifacts"] != nil {
				t.Fatal("reverification fabricated product changes")
			}
		}
	}
	if impact != 1 || !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") {
		t.Fatalf("original report/impact lost: %d", impact)
	}
	// The next entry must be bound to the registered change impact, so the
	// new round is anchored to the actual re-verification scope.
	if impactRef == "" || entry["change_impact_ref"] != impactRef {
		t.Fatalf("the next S7 entry is not bound to the registered change impact: entry=%v impact=%q", entry, impactRef)
	}
	planPath := reviewpkg.PlanQuestionnairePath(root, state)
	if !strings.Contains(out, planPath) {
		t.Fatalf("missing exact next S7 responsibility: %s", out)
	}
	// The entry coordinate must locate the exact prepared S7 questionnaire,
	// and that questionnaire must carry the same Plan identity.
	generated := readS10QuestionnaireState(t, planPath)
	newPlanID, _ := generated["review_plan_id"].(string)
	if newPlanID == "" || entry["plan_id"] != newPlanID {
		t.Fatalf("the next S7 entry does not locate the prepared Plan: entry=%v questionnaire=%q", entry, newPlanID)
	}
	if newPlanID == previousPlanID {
		t.Fatalf("TR-016 reused the returned round's Plan identity: %q", newPlanID)
	}
	if _, err := os.Stat(planPath); err != nil {
		t.Fatal(err)
	}
	// Supply simulated professional content to that exact existing S7 form.
	professional := minimalReviewPlan(t, t.TempDir(), "review-plan-after-acceptance")
	plan := readS10QuestionnaireState(t, professional)
	plan["coverage_inventory"] = generated["coverage_inventory"]
	plan["schema_version"] = generated["schema_version"]
	plan["review_plan_id"] = generated["review_plan_id"]
	delete(plan, "review_round")
	plan["frozen_subjects"] = []any{}
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
	if !strings.Contains(feedback, generated["review_plan_id"].(string)) {
		t.Fatalf("original new S7 plan did not register: %s", feedback)
	}
	state = readS10QuestionnaireState(t, statePath)
	if state["review"].(map[string]any)["plan"].(map[string]any)["plan_id"] != generated["review_plan_id"] {
		t.Fatalf("old plan reused as current: pointer=%v generated=%v feedback=%s", state["review"].(map[string]any)["plan"], generated["review_plan_id"], feedback)
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
	for _, fault := range []string{"missing_plan", "missing_file", "different_plan", "old_round", "wrong_generation", "empty_scope"} {
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
				case "different_plan":
					plan["review_plan_id"] = "review-plan-unrelated-original"
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
			// Historical round metadata and an empty product scope do not
			// replace this original Plan's identity or the received decision.
			if fault == "old_round" || fault == "empty_scope" {
				if state["lifecycle"].(map[string]any)["state"] != "verification" || !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") || !strings.Contains(out, "TR-016") {
					t.Fatalf("original review decision was gated on optional metadata: %s", out)
				}
				for _, raw := range state["evidence"].([]any) {
					row := raw.(map[string]any)
					if row["kind"] == "change_impact" {
						impact := readS10QuestionnaireState(t, filepath.Join(root, row["path"].(string)))
						if impact["changed_artifacts"] != nil {
							t.Fatal("re-review decision fabricated product edits")
						}
					}
				}
				return
			}
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
			beforeState := readS10QuestionnaireState(t, statePath)
			beforeEvidence := acceptanceEvidenceBytes(t, root, beforeState)
			beforeEntry, _ := beforeState["review"].(map[string]any)["round_entry"].(map[string]any)
			out := callS10QuestionnaireHook(t, root, request)
			after, _ := os.ReadFile(statePath)
			afterState := readS10QuestionnaireState(t, statePath)
			afterEvidence := acceptanceEvidenceBytes(t, root, afterState)
			afterReview := afterState["review"].(map[string]any)

			// The received decision is reused, never received again: exactly one
			// acceptance record, with the same identity and the same bytes.
			if countS10QuestionnaireEvidence(afterState, "acceptance") != 1 {
				t.Fatalf("the received decision was received again: %d acceptance records", countS10QuestionnaireEvidence(afterState, "acceptance"))
			}
			if !bytes.Equal(beforeEvidence, afterEvidence) {
				t.Fatalf("the received decision was rewritten: before=%s after=%s", beforeEvidence, afterEvidence)
			}
			// Reverse assertion: the changed private draft never reaches the
			// Runtime state nor the received record.
			if bytes.Contains(after, []byte("Unsubmitted changed professional content.")) ||
				bytes.Contains(afterEvidence, []byte("Unsubmitted changed professional content.")) {
				t.Fatal("the changed private draft was received")
			}
			// Reverse assertion: the retired scalar round counter must not
			// reappear, and no machine CleanRound proof may be invented.
			if _, exists := afterReview["round"]; exists {
				t.Fatalf("retired review round counter came back: %v", afterReview["round"])
			}
			if afterReview["clean_round"] != nil {
				t.Fatalf("an unreceived clean round proof was invented: %v", afterReview["clean_round"])
			}

			if !changed {
				// Worker subcase: role isolation. The original byte-equality
				// assertion is kept verbatim.
				if !bytes.Equal(before, after) {
					t.Fatal("unrelated tool received changed work or Worker consumed Main continuation")
				}
				// Reverse assertion: an ordinary Worker Read is not Main's
				// continuation, so it must not carry a continuation receipt.
				if strings.Contains(out, "Committed TR-016") || strings.Contains(out, "S10 acceptance recorded") {
					t.Fatalf("Worker's ordinary Read produced a Main continuation receipt: %s", out)
				}
				if afterState["lifecycle"].(map[string]any)["state"] != "acceptance" {
					t.Fatalf("Worker's ordinary Read consumed Main's continuation: %v", afterState["lifecycle"])
				}
				if afterReview["round_entry"].(map[string]any)["transition_id"] != "TR-006" {
					t.Fatalf("Worker's ordinary Read started a new review round: %v", afterReview["round_entry"])
				}
				return
			}

			// Main subcase: the unfinished successor must continue, and it must
			// not swallow the received decision.
			if bytes.Equal(before, after) {
				t.Fatal("the pending return did not continue the unfinished successor")
			}
			lifecycle := afterState["lifecycle"].(map[string]any)
			if lifecycle["state"] != "verification" || lifecycle["phase"] != "planned" {
				t.Fatalf("the pending return did not resume the S7 round: %v", lifecycle)
			}
			entry, _ := afterReview["round_entry"].(map[string]any)
			if entry["transition_id"] != "TR-016" {
				t.Fatalf("the pending return did not commit the TR-016 return: %v", entry)
			}
			if entry["plan_id"] == beforeEntry["plan_id"] {
				t.Fatalf("the pending return reused the returned round's Plan identity: %v", entry)
			}
			// The new round must be bound to the registered change impact.
			impactRef, _ := entry["change_impact_ref"].(string)
			if impactRef == "" || countS10QuestionnaireEvidence(afterState, "change_impact") != 1 {
				t.Fatalf("the pending return did not register exactly one change impact: %v", entry)
			}
			registered := false
			for _, raw := range afterState["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["kind"] == "change_impact" && row["id"] == impactRef {
					registered = true
				}
			}
			if !registered {
				t.Fatalf("the pending return is not bound to a registered change impact: %v", impactRef)
			}
			// The reported receipt must name the already received decision, not
			// a newly received one.
			if id := acceptanceEvidenceID(afterState); id == "" || !strings.Contains(out, id) {
				t.Fatalf("the pending return did not reuse the received decision: %v %s", id, out)
			}
		})
	}
}

// acceptanceEvidenceBytes returns the recorded acceptance evidence bytes.
func acceptanceEvidenceBytes(t *testing.T, root string, state map[string]any) []byte {
	t.Helper()
	for _, raw := range state["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row == nil || row["kind"] != "acceptance" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(row["path"].(string))))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return nil
}

// acceptanceEvidenceID returns the identity of the recorded acceptance fact.
func acceptanceEvidenceID(state map[string]any) string {
	for _, raw := range state["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row == nil || row["kind"] != "acceptance" {
			continue
		}
		id, _ := row["id"].(string)
		return id
	}
	return ""
}

// countS10QuestionnaireEvidence counts the recorded evidence rows of one kind.
func countS10QuestionnaireEvidence(state map[string]any, kind string) int {
	count := 0
	for _, raw := range state["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row != nil && row["kind"] == kind {
			count++
		}
	}
	return count
}

func TestS10ReviewRequiredDoesNotUseReviewCountAsGate(t *testing.T) {
	root, answer, _ := s10ReviewRequiredRoot(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = 1
	data, _ := json.Marshal(state)
	writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", data)
	output := saveS10ReviewAnswer(t, root, answer)
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatalf("review_required was blocked by a review count: %s", output)
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
		t.Fatalf("review return duplicated original facts: %d/%d", reports, impacts)
	}
}
