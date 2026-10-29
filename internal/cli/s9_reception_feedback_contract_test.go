package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// New execution observations may carry the original failure as background. They
// must not turn it into a new Main receiver command or author continuation duty.
func TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical(t *testing.T) {
	for _, kind := range []string{"system", "author"} {
		t.Run(kind, func(t *testing.T) {
			var root string
			var plan workpacket.RepairPlanReportPacket
			var concreteFailure string
			if kind == "system" {
				root, _, plan, _, _ = s9HandoffMissingCandidate(t)
				concreteFailure = "S9 local evidence source gap"
			} else {
				var worker string
				var input policy.Input
				root, worker, plan, input, _ = nativeS9WorkerFixture(t)
				flat := nativeS9TestResultPath(t, root, plan)
				malformed := []byte(`{"verdict":`)
				originalError := workpacket.ValidateUniqueJSON(malformed)
				if originalError == nil {
					t.Fatal("malformed author fixture must fail the original JSON validator")
				}
				if err := os.WriteFile(filepath.Join(worker, flat), malformed, 0600); err != nil {
					t.Fatal(err)
				}
				input.Event = "SubagentStop"
				var diagnostic bytes.Buffer
				if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
					t.Fatalf("original author Stop: handled=%v code=%d diagnostic=%s", handled, code, &diagnostic)
				}
				concreteFailure = originalError.Error()
			}
			snap := repairDispatchSnapshot(t, root)
			previous, found := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, plan.ID)
			if !found || previous.Attempt == nil || !strings.Contains(previous.Message, concreteFailure) || kind == "author" && previous.Attempt.FailureKind != "author" {
				t.Fatalf("fixture did not preserve original %s failure: %+v found=%v", kind, previous, found)
			}
			before := repairDispatchRuntimeBytes(t, root)
			brief, _, _ := nativeS9Paths(plan)
			for _, tc := range []struct {
				name     string
				response map[string]any
			}{
				{"missing fields", nil},
				{"contradictory launch", map[string]any{"status": "async_launched", "isAsync": false}},
				{"new asynchronous launch", map[string]any{"status": "async_launched", "isAsync": true}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got := nativeExecutionMainOutput(t, root, filepath.Join(root, brief), tc.response)
					if !strings.Contains(got, concreteFailure) || !strings.Contains(got, plan.Binding.OwnerAgentID) {
						t.Errorf("new execution lost original failure/responsibility background: %s", got)
					}
					for _, input := range []string{previous.Attempt.InputRef, previous.Attempt.TypedInputRef} {
						if input != "" && strings.Contains(got, s9OriginalConsumeCommand(root, input)) {
							t.Errorf("new execution issued a receiver command from old failure: %s", got)
						}
					}
					for _, action := range []string{
						"Main owns the system reception continuation.",
						"Main continues that same original author responsibility to correct only the reported omission",
						"resume the original consumer with the exact original input after restoring its prerequisites",
					} {
						if strings.Contains(got, action) {
							t.Errorf("new execution assigned old recovery action %q: %s", action, got)
						}
					}
				})
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

func TestS9ReceptionFeedbackMissingSavedTypedInputNamesOriginalWindowBeforeRetry(t *testing.T) {
	root, worker, plan, _, packet := s9HandoffMissingCandidate(t)
	typed := filepath.Join(worker, packet.AnswerPath)
	beforeDelete := repairDispatchSnapshot(t, root)
	previous, found := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, beforeDelete, plan.ID)
	if !found || previous.Attempt == nil || previous.Attempt.Stage != "typed_result_consumer" || previous.Attempt.TypedInputRef != typed || len(previous.Files[typed]) != 64 {
		t.Fatalf("fixture did not reach actual saved typed boundary: %+v found=%v", previous, found)
	}
	if err := os.Remove(typed); err != nil {
		t.Fatal(err)
	}
	// No consume/Stop retry occurs between deleting the original saved input and
	// these fresh Main observations. A retry-created diagnostic cannot hide the gap.
	before := repairDispatchRuntimeBytes(t, root)
	brief, _, _ := nativeS9Paths(plan)
	flat := nativeS9TestResultPath(t, root, plan)
	for _, got := range []string{
		currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}),
		nativeExecutionMainOutput(t, root, filepath.Join(root, brief), map[string]any{"status": "completed"}),
	} {
		for _, want := range []string{typed, "Original delivery input/source is unavailable", "PlanReport receipt is accepted"} {
			if !strings.Contains(got, want) {
				t.Errorf("fresh Main lost exact saved-input recovery %q: %s", want, got)
			}
		}
		if strings.Contains(got, s9OriginalConsumeCommand(root, typed)) || strings.Contains(got, s9OriginalConsumeCommand(root, filepath.Join(worker, flat))) {
			t.Errorf("lost saved typed input was replaced with mutable flat recovery: %s", got)
		}
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9ReceptionFeedbackFrozenTypedResponsibilityDoesNotInventNativeFlat(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	execution := mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions")[f.packet.binding.ManifestAssignmentID].(map[string]any)
	if stringValue(execution["purpose"]) == workspace.NativeDeliveryPurpose {
		t.Fatal("fixture must represent an existing frozen typed execution")
	}
	// The original fixture has only a Result index. Add its original Plan index
	// so a native path synthesized from Plan ID is observable, without publishing
	// a native brief or claiming this frozen execution was ever a native source.
	planID := "frozen-original-plan"
	bindingBytes, err := json.Marshal(f.packet.binding)
	if err != nil {
		t.Fatal(err)
	}
	var binding map[string]any
	if err := json.Unmarshal(bindingBytes, &binding); err != nil {
		t.Fatal(err)
	}
	planRow := map[string]any{
		"template_id": "s9-plan-report-v1", "packet_id": planID, "binding": binding,
		"view_path": ".claude/frozen-plan/view.json", "answer_path": ".claude/frozen-plan/answer.json",
	}
	mapFieldCLI(f.state, "entities")["work_packets"] = append(mapFieldCLI(f.state, "entities")["work_packets"].([]any), planRow)
	f.write(t)
	brief, _, flat := nativeS9Paths(workpacket.RepairPlanReportPacket{ID: planID, ViewPath: stringValue(planRow["view_path"])})
	if _, err := os.Stat(filepath.Join(f.root, brief)); !os.IsNotExist(err) {
		t.Fatalf("frozen fixture unexpectedly has native brief: %v", err)
	}
	before := repairDispatchRuntimeBytes(t, f.root)
	ctx := context.WithValue(t.Context(), nativeExecutionReturnKey{}, nativeExecutionCompleted)
	handoff := s9HandoffForBinding(ctx, f.root, runtime.Snapshot{State: f.state}, f.packet.binding, f.packet.id)
	if handoff.Plan.id != planID || handoff.Result == nil || handoff.Result.originalRoot != f.worker {
		t.Fatalf("frozen original indexes were not projected: %+v", handoff)
	}
	got := handoff.summary(f.root) + " " + s9ObservedHandoffAction(ctx, f.root, runtime.Snapshot{State: f.state}, handoff, execution)
	if !strings.Contains(got, filepath.Join(f.worker, f.packet.answer)) {
		t.Errorf("frozen original typed reference was lost: %s", got)
	}
	if strings.Contains(got, s9OriginalConsumeCommand(f.root, filepath.Join(f.worker, flat))) {
		t.Errorf("frozen typed responsibility was given an invented native flat receiver: %s", got)
	}
	if strings.Contains(got, "PreToolUse observes ready original answers") || !strings.Contains(got, "original issued responsibility's reception and recovery contract") {
		t.Errorf("frozen responsibility gained native natural-event guidance: %s", got)
	}
	repairDispatchAssertUnchanged(t, f.root, before)
}

func TestS9ReceptionFeedbackMissingBriefPreservesAcceptedPlanBeforeExecution(t *testing.T) {
	root, worker, plan, input, _ := nativeS9WorkerFixture(t)
	brief, pin, legacyResult := nativeS9Paths(plan)
	privatePlan, privateResult := nativeS9TestAuthorPaths(t, root, plan)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := importNativeS9PlanReport(t.Context(), root, worker, plan, service, &diagnostic); err != nil {
		t.Fatalf("import original Plan: %v", err)
	}
	receipt, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan consumer did not accept: %+v %v", receipt, err)
	}
	// Lose the pin only after the original Plan receipt. The actual Stop observes
	// this source gap before execution registration; later lose the display brief.
	if err := os.Remove(filepath.Join(root, pin)); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	stopBody, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var stopOut bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(stopBody), &stopOut, &diagnostic); code != 0 || !strings.Contains(stopOut.String()+diagnostic.String(), filepath.Join(root, pin)) {
		t.Fatalf("original public Stop did not reach pin failure: code=%d stdout=%s diagnostic=%s", code, &stopOut, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	if receipt, err := service.RepairPlanReportReceipt(plan.Binding.OwnerAgentID, plan.ID); err != nil || !receipt.Accepted {
		t.Fatalf("source gap erased accepted Plan: %+v %v", receipt, err)
	}
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry != nil && len(registry.Executions) != 0 {
		t.Fatalf("fixture must fail before execution registration: %+v %v", registry, err)
	}
	// While the display brief still identifies native responsibility, both
	// questionnaire projections must name the real error. Only Main receives
	// the preparation source as a recovery task.
	for _, owner := range []string{"Main", plan.Binding.OwnerAgentID} {
		request := policy.Input{Event: "PreToolUse", AgentID: owner, CWD: root}
		got := s9QuestionnaireContext(t.Context(), root, request, nil, snap, owner, false)
		filter := owner
		if owner == "Main" {
			filter = ""
		}
		known := s9ListenerKnownPaths(root, snap.State, filter, false)
		for _, text := range []string{got, known} {
			if !strings.Contains(text, "frozen private author coordinates unavailable") {
				t.Errorf("%s lost the original source error: %s", owner, text)
			}
			if strings.Contains(text, filepath.Join(root, pin)) != (owner == "Main") {
				t.Errorf("%s got wrong preparation-source audience: %s", owner, text)
			}
			for _, guessed := range []string{privatePlan, privateResult, legacyResult} {
				if strings.Contains(text, guessed) {
					t.Errorf("%s guessed private coordinate %q: %s", owner, guessed, text)
				}
			}
		}
		if !strings.Contains(got, "PlanReport is recorded") || strings.Contains(got, "using its exact prepared brief") {
			t.Errorf("%s lost accepted Plan or continued through missing preparation: %s", owner, got)
		}
	}
	if err := os.Remove(filepath.Join(root, brief)); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, tc := range []struct {
		name, event, tool string
		toolInput         map[string]any
		toolResponse      map[string]any
	}{
		{"ordinary progress", "PreToolUse", "Read", map[string]any{"file_path": filepath.Join(root, "internal/api/one.go")}, nil},
		{"unknown execution", "PostToolUse", "Agent", map[string]any{"prompt": filepath.Join(root, brief)}, nil},
		{"completed execution", "PostToolUse", "Agent", map[string]any{"prompt": filepath.Join(root, brief)}, map[string]any{"status": "completed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"hook_event_name": tc.event, "agent_id": "Main", "cwd": root, "tool_name": tc.tool, "tool_input": tc.toolInput, "tool_response": tc.toolResponse})
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			if code := Run([]string{"hook", "--root", root, "--event", tc.event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
				t.Fatalf("public Main %s: %d stdout=%s stderr=%s", tc.event, code, &out, &diagnostic)
			}
			got := nativeS9ListenerFeedback(t, &out)
			for _, want := range []string{
				"PlanReport receipt is accepted", plan.Binding.OwnerAgentID, filepath.Join(root, brief),
				filepath.Join(root, pin), "frozen private author coordinates unavailable", "PreToolUse", "PostToolUse", "Stop/SubagentStop",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("missing brief erased %q: %s", want, got)
				}
			}
			for _, guessed := range []string{privatePlan, privateResult, legacyResult, "Normal SubagentStop remains the receiver"} {
				if strings.Contains(got, guessed) {
					t.Errorf("missing pin or old reception text inferred %q: %s", guessed, got)
				}
			}
			if tc.name == "unknown execution" && (!strings.Contains(got, "Platform execution is unknown") || strings.Contains(got, "Native return for ") || strings.Contains(got, " runtime stage-answer consume ")) {
				t.Errorf("unknown execution gained completion or recovery: %s", got)
			}
		})
	}
	repairDispatchAssertUnchanged(t, root, before)
}
