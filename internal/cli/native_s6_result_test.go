package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
)

func compactNativeResultFixture(t *testing.T) (map[string]any, map[string]any, assignmentrecord.Record) {
	t.Helper()
	var plan, result map[string]any
	for text, target := range map[string]*map[string]any{
		`{"assertion_checks":[{"assertion":"authorized value persists","oracle":"read back committed value","done_when_indexes":[0,1]},{"assertion":"unauthorized write is rejected","oracle":"observe rejection without mutation","done_when_indexes":[1]}]}`:                                                                                                                                                                                                                                                                                &plan,
		`{"summary":"Exercised the actual persistence and rejection paths.","assertion_evidence":[{"plan_assertion_index":1,"evidence_refs":["tests/reject.txt"]},{"plan_assertion_index":0,"evidence_refs":["tests/persist.txt"]}],"risks_blockers":[],"scope_deviations":[],"closing_assessment":{"disposition":"complete","conditions":[{"done_when_index":0,"judgment":"satisfied","rationale":"The stored value matches."},{"done_when_index":1,"judgment":"satisfied","rationale":"Unauthorized writes leave the value unchanged."}]}}`: &result,
	} {
		if err := json.Unmarshal([]byte(text), target); err != nil {
			t.Fatal(err)
		}
	}
	r := assignmentrecord.Record{DoneWhen: []string{"value persists", "unauthorized writes preserve state"}, ClosingContract: &assignmentrecord.ClosingContract{Contract: assignmentrecord.ClosingContractVersion}}
	return plan, result, r
}

func validateProjectedNativeResult(plan, result map[string]any, record assignmentrecord.Record) (assignmentresult.Answer, error) {
	var source struct {
		Assertions []assignmentresult.PlanAssertion `json:"assertion_checks"`
	}
	var answer assignmentresult.Answer
	data, _ := json.Marshal(plan)
	if err := json.Unmarshal(data, &source); err != nil {
		return answer, err
	}
	data, _ = json.Marshal(result)
	if err := json.Unmarshal(data, &answer); err != nil {
		return answer, err
	}
	if err := assignmentresult.ValidateAnswer(answer, source.Assertions); err != nil {
		return answer, err
	}
	return answer, assignmentresult.ValidateClosingAssessment(record, source.Assertions, answer)
}

func TestNativeS6CompactResultUsesOriginalPlanWithoutAuthoringItsFactsAgain(t *testing.T) {
	plan, result, record := compactNativeResultFixture(t)
	before, _ := json.Marshal(plan)
	if err := projectNativeS6Result(plan, result, len(record.DoneWhen)); err != nil {
		t.Fatal(err)
	}
	answer, err := validateProjectedNativeResult(plan, result, record)
	if err != nil {
		t.Fatalf("projected answer failed original consumers: %v", err)
	}
	if answer.AssertionEvidence[0].Assertion != "unauthorized write is rejected" || !reflect.DeepEqual(answer.ClosingAssessment.Conditions[0].PlanAssertionIndexes, []int{0}) || !reflect.DeepEqual(answer.ClosingAssessment.Conditions[1].PlanAssertionIndexes, []int{0, 1}) {
		t.Fatalf("wrong assertion or closing association: %+v", answer)
	}
	after, _ := json.Marshal(plan)
	if !bytes.Equal(before, after) {
		t.Fatal("Result projection changed the original Plan")
	}
	// Already full authoring remains unchanged, including its canonical digest.
	full, _ := json.Marshal(result)
	digest, _ := assignmentresult.AnswerDigest(answer)
	if err := projectNativeS6Result(plan, result, len(record.DoneWhen)); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(result)
	if !bytes.Equal(full, again) {
		t.Fatal("legacy full Result was rewritten")
	}
	answer, err = validateProjectedNativeResult(plan, result, record)
	nextDigest, _ := assignmentresult.AnswerDigest(answer)
	if err != nil || nextDigest != digest {
		t.Fatalf("original canonical answer signature changed: %v", err)
	}
}

func TestNativeS6CompactResultRejectsInvalidReferencesAndPreservesExplicitErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"negative", func(r map[string]any) {
			r["assertion_evidence"].([]any)[0].(map[string]any)["plan_assertion_index"] = float64(-1)
		}},
		{"fraction", func(r map[string]any) {
			r["assertion_evidence"].([]any)[0].(map[string]any)["plan_assertion_index"] = 0.5
		}},
		{"outside_plan", func(r map[string]any) {
			r["assertion_evidence"].([]any)[0].(map[string]any)["plan_assertion_index"] = float64(2)
		}},
		{"string_index", func(r map[string]any) {
			r["assertion_evidence"].([]any)[0].(map[string]any)["plan_assertion_index"] = "0"
		}},
		{"null_index", func(r map[string]any) {
			r["assertion_evidence"].([]any)[0].(map[string]any)["plan_assertion_index"] = nil
		}},
		{"duplicate_reference", func(r map[string]any) {
			r["assertion_evidence"].([]any)[1].(map[string]any)["plan_assertion_index"] = float64(1)
		}},
		{"mixed_assertion", func(r map[string]any) {
			r["assertion_evidence"].([]any)[0].(map[string]any)["assertion"] = "different assertion"
		}},
		{"missing_evidence", func(r map[string]any) { delete(r["assertion_evidence"].([]any)[0].(map[string]any), "evidence_refs") }},
		{"missing_assertion", func(r map[string]any) { r["assertion_evidence"] = r["assertion_evidence"].([]any)[:1] }},
		{"wrong_explicit_relation", func(r map[string]any) {
			r["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["plan_assertion_indexes"] = []any{float64(1)}
		}},
		{"null_explicit_relation", func(r map[string]any) {
			r["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["plan_assertion_indexes"] = nil
		}},
		{"missing_judgment", func(r map[string]any) {
			delete(r["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any), "judgment")
		}},
		{"missing_rationale", func(r map[string]any) {
			delete(r["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any), "rationale")
		}},
		{"false_complete", func(r map[string]any) {
			r["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["judgment"] = "blocked"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, result, record := compactNativeResultFixture(t)
			tc.mutate(result)
			err := projectNativeS6Result(plan, result, len(record.DoneWhen))
			if err == nil {
				_, err = validateProjectedNativeResult(plan, result, record)
			}
			if err == nil {
				t.Fatal("invalid professional answer was silently repaired or accepted")
			}
		})
	}
}

func TestNativeS6CompactProjectionDoesNotCreateSuccessOrRepairLegacyText(t *testing.T) {
	plan, result, record := compactNativeResultFixture(t)
	closing := result["closing_assessment"].(map[string]any)
	closing["disposition"] = "blocked"
	closing["conditions"].([]any)[0].(map[string]any)["judgment"] = "blocked"
	if err := projectNativeS6Result(plan, result, len(record.DoneWhen)); err != nil {
		t.Fatal(err)
	}
	answer, err := validateProjectedNativeResult(plan, result, record)
	if err != nil || assignmentresult.ClosingAssessmentComplete(answer) {
		t.Fatalf("blocked judgment was rejected or promoted to completion: %v %+v", err, answer)
	}
	result["assertion_evidence"].([]any)[0].(map[string]any)["oracle"] = "wrong explicit oracle"
	if err := projectNativeS6Result(plan, result, len(record.DoneWhen)); err != nil {
		t.Fatal(err)
	}
	if _, err := validateProjectedNativeResult(plan, result, record); err == nil {
		t.Fatal("projection silently replaced an explicitly wrong original oracle")
	}
}

func TestNativeS6BriefPreservesExactLegacyAuthoringAndRejectsDrift(t *testing.T) {
	root := t.TempDir()
	r := assignmentrecord.Record{AssignmentID: "assignment-compact", Revision: 1, RoleFamily: "backend-builder", Contract: assignmentrecord.NativeDeliveryContract, Task: assignmentrecord.ContextSource{ID: "TASK-001", Ref: "docs/dev/tasks/TASK-001.md"}, EffectiveWritePaths: []string{"src/"}, DoneWhen: []string{"behavior verified"}}
	state := map[string]any{"bound_req": map[string]any{"workspace": map[string]any{"dev_branch": "dev"}}}
	if err := prepareNativeS6Brief(root, state, r, "original-base", io.Discard); err != nil {
		t.Fatal(err)
	}
	brief, plan, result := assignmentrecord.NativePaths(r)
	path := filepath.Join(root, brief)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(current, []byte(`"plan_assertion_index":0`)) || bytes.Contains(current, []byte(`"judgment":null,"plan_assertion_indexes":[]`)) {
		t.Fatal("normal preparation did not deliver compact authoring")
	}
	previousSourceAligned := strings.TrimSuffix(string(current), nativeS6CurrentAuthoringGuidance)
	previousCurrent := strings.TrimPrefix(previousSourceAligned, nativeS6SourceAlignment(r, "original-base", plan, result))
	previousInputs := strings.Replace(previousCurrent, nativeS6BriefInputs(root, r, false), nativeS6BriefInputs(root, r, true), 1)
	legacy := strings.Replace(previousInputs, nativeS6ProfessionalInstructions("original-base", path, plan, result), nativeS6LegacyProfessionalInstructions("original-base", path, plan, result), 1)
	compact := strings.Replace(previousInputs, nativeS6ProfessionalInstructions("original-base", path, plan, result), fmt.Sprintf(nativeS6CompactProfessionalFormat, "original-base", path, plan, result), 1)
	for _, text := range []string{string(current), previousSourceAligned, previousCurrent + nativeS6LegacySourceAlignment(r, "original-base", plan, result), previousCurrent, compact, strings.Replace(compact, nativeS6MethodGuidance(root, r), "", 1), legacy, strings.Replace(legacy, nativeS6MethodGuidance(root, r), "", 1)} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if err := prepareNativeS6Brief(root, state, r, "original-base", io.Discard); err != nil {
			t.Fatalf("exact prepared brief no longer recoverable: %v", err)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != text {
			t.Fatal("recovery rewrote original brief")
		}
	}
	if err := os.WriteFile(path, []byte(strings.Replace(legacy, "original-base", "different-base", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareNativeS6Brief(root, state, r, "original-base", io.Discard); err == nil || !strings.Contains(err.Error(), "original native task brief differs") {
		t.Fatalf("legacy compatibility accepted changed sources: %v", err)
	}
}

func TestNativeS6BriefSeparatesWorkerClosingChecksFromMainChecks(t *testing.T) {
	for _, workerCommand := range []string{"", "go test ./worker/..."} {
		t.Run(workerCommand, func(t *testing.T) {
			root := t.TempDir()
			r := assignmentrecord.Record{AssignmentID: "assignment-checks", Revision: 1, Contract: assignmentrecord.NativeDeliveryContract, Task: assignmentrecord.ContextSource{ID: "TASK-001", Ref: "docs/dev/tasks/TASK-001.md"}, EffectiveWritePaths: []string{"src/"}, RequiredChecks: []string{"make main-only-check"}, ClosingContract: &assignmentrecord.ClosingContract{}}
			if workerCommand != "" {
				r.ClosingContract.WorkerChecks = []assignmentrecord.WorkerCheck{{Index: 0, Command: workerCommand}}
			}
			if err := prepareNativeS6Brief(root, map[string]any{}, r, "base", io.Discard); err != nil {
				t.Fatal(err)
			}
			brief, _, _ := assignmentrecord.NativePaths(r)
			data, err := os.ReadFile(filepath.Join(root, brief))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, "make main-only-check") {
				t.Fatal("assigned Main check to Worker")
			}
			if workerCommand != "" && !strings.Contains(text, "- "+workerCommand) {
				t.Fatal("lost explicit Worker check")
			}
			if workerCommand == "" && !strings.Contains(text, "No Worker runner checks were assigned") {
				t.Fatal("empty Closing Contract borrowed Main checks")
			}
		})
	}
}
