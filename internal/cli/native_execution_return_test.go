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
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestNativeExecutionReturnUsesActualFields(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response map[string]any
		want     nativeExecutionReturn
	}{
		{"launch", map[string]any{"status": "async_launched", "isAsync": true}, nativeExecutionLaunched},
		{"launch without flag", map[string]any{"status": "async_launched"}, nativeExecutionLaunched},
		{"flag only", map[string]any{"isAsync": true}, nativeExecutionLaunched},
		{"completed", map[string]any{"status": "completed"}, nativeExecutionCompleted},
		{"completed background", map[string]any{"status": "completed", "isAsync": true}, nativeExecutionCompleted},
		{"contradictory launch", map[string]any{"status": "async_launched", "isAsync": false}, nativeExecutionUnknown},
		{"wrong flag type", map[string]any{"status": "async_launched", "isAsync": "true"}, nativeExecutionUnknown},
		{"unrecognized status", map[string]any{"status": "running", "isAsync": true}, nativeExecutionUnknown},
		{"prose not state", map[string]any{"text": "completed successfully"}, nativeExecutionUnknown},
		{"missing", nil, nativeExecutionUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeExecutionFromResponse(tc.response); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func nativeExecutionMainOutput(t *testing.T, root, brief string, response map[string]any) string {
	t.Helper()
	input := policy.Input{Event: "PostToolUse", ToolName: "Agent", AgentID: "Main", CWD: root, Context: context.Background(), ToolInput: map[string]any{"prompt": brief}, ToolResponse: response}
	var out bytes.Buffer
	if handled, code := nativeMainReturnHook(root, input, &out); !handled || code != 0 {
		t.Fatalf("Main event: handled=%v code=%d output=%s", handled, code, &out)
	}
	var payload struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Output.Context
}

func TestHandoffReviewAsyncStartMustNotRequestReceptionRepair(t *testing.T) {
	root, _, plan, _, _ := nativeS9WorkerFixture(t)
	before := repairDispatchRuntimeBytes(t, root)
	brief, _, _ := nativeS9Paths(plan)
	for _, response := range []map[string]any{{"status": "async_launched", "isAsync": true}, {"status": "async_launched"}, nil} {
		text := nativeExecutionMainOutput(t, root, filepath.Join(root, brief), response)
		for _, bad := range []string{"last Stop diagnostic", "interrupted consumer transaction", "runtime stage-answer consume", "restores sources/prerequisites"} {
			if strings.Contains(text, bad) {
				t.Errorf("launch/unknown invented repair %q: %s", bad, text)
			}
		}
		if strings.Contains(text, "Native return for ") {
			t.Errorf("launch/unknown became returned Worker: %s", text)
		}
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeExecutionLaunchPreservesOriginalPartialReceipt(t *testing.T) {
	root, _, plan, _, _ := s9HandoffMissingCandidate(t)
	before := repairDispatchRuntimeBytes(t, root)
	brief, _, _ := nativeS9Paths(plan)
	text := nativeExecutionMainOutput(t, root, filepath.Join(root, brief), map[string]any{"status": "async_launched", "isAsync": true})
	for _, want := range []string{"PlanReport receipt is accepted", "Consumer diagnostic", "launch acknowledgement"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q: %s", want, text)
		}
	}
	if strings.Contains(text, "runtime stage-answer consume") {
		t.Fatalf("new launch reissued old recovery duty: %s", text)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9CapturedRecoveryCommandExecutesOriginalTypedConsumer(t *testing.T) {
	root, worker, plan, execution, packet := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	handoff := s9HandoffForBinding(t.Context(), root, snap, plan.Binding)
	text := s9UnreceivedActionContext(t.Context(), root, handoff)
	command := s9OriginalConsumeCommand(root, filepath.Join(worker, packet.AnswerPath))
	if !strings.Contains(text, command) {
		t.Fatalf("missing actual typed recovery command: %s", text)
	}
	writeRepairLeadFile(t, worker, ".claude/submissions/logs/missing-handoff.txt", "original evidence restored\n")
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", filepath.Join(worker, packet.AnswerPath)}, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("receiver: %d %s %s", code, &out, &diagnostic)
	}
	current := repairDispatchSnapshot(t, root)
	_, e := repairExecutionForChoice(current.State, repairDispatchChoice{AssignmentID: execution.AssignmentID})
	if stringValue(e["delivery_ref"]) == "" {
		t.Fatal("feedback command did not form original candidate")
	}
	// The old failed attempt may remain on disk after a separate original
	// consumer succeeds. Its recovery instruction must no longer be current.
	projected := s9HandoffForBinding(t.Context(), root, current, plan.Binding)
	if projected.Diagnostic != "" || strings.Contains(projected.summary(root), "resume the original consumer") {
		t.Fatalf("accepted candidate retained obsolete recovery: %+v", projected)
	}
}

func TestS9CapturedInputCoordinateAloneIsNotSavedTypedInput(t *testing.T) {
	root, worker, plan, _, packet := s9HandoffMissingCandidate(t)
	p := s9HandoffForBinding(t.Context(), root, repairDispatchSnapshot(t, root), plan.Binding)
	p.Attempt.Stage = "typed_result_prepared"
	p.Attempt.TypedInputRef = filepath.Join(worker, packet.AnswerPath)
	text := s9UnreceivedActionContext(t.Context(), root, p)
	flat := nativeS9TestResultPath(t, root, plan)
	if !strings.Contains(text, s9OriginalConsumeCommand(root, filepath.Join(worker, flat))) {
		t.Fatal("prepared draft incorrectly selected typed saved window: " + text)
	}
	if _, err := os.Stat(filepath.Join(worker, packet.AnswerPath)); err != nil {
		t.Fatal(err)
	}
}

func TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput(t *testing.T) {
	root, worker, execution, packet, _, ref := s9BeforeIntegrationAck(t, true)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	accepted, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil {
		t.Fatal(err)
	}
	planPath := stringValue(repairPointerCLI(snap.State)["plan_ref"])
	if err := os.Remove(filepath.Join(root, planPath)); err != nil {
		t.Fatal(err)
	}
	var originalPlan workpacket.RepairPlanReportPacket
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if row["template_id"] == "s9-plan-report-v1" {
			originalPlan = workpacket.RepairPlanReportPacket{ID: stringValue(row["packet_id"]), ViewPath: stringValue(row["view_path"]), AnswerPath: stringValue(row["answer_path"])}
			break
		}
	}
	if originalPlan.ID == "" || originalPlan.ViewPath == "" {
		t.Fatal("missing exact original Plan packet coordinates")
	}
	relative := nativeS9TestResultPath(t, root, originalPlan)
	if err := os.Remove(filepath.Join(worker, relative)); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", filepath.Join(worker, relative)}, nil, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "ACK") {
		t.Fatalf("already accepted Final reopened live sources: code=%d output=%s error=%s packet=%s owner=%s", code, &out, &diagnostic, packet.ID, execution.AgentID)
	}
	current, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil || !bytes.Equal(accepted, current) {
		t.Fatal("Final bytes changed", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9PublicFeedbackRecoveryCommandUsesReturnedArgv(t *testing.T) {
	root, worker, _, execution, _ := s9HandoffMissingCandidate(t)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "agent_id": "Main", "tool_name": "Read", "tool_input": map[string]any{"file_path": "internal/api/one.go"}})
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("public Main feedback: %d %s %s", code, &out, &diagnostic)
	}
	var envelope struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	_, command, found := strings.Cut(envelope.Output.Context, "then run this exact original receiver: ")
	if !found {
		t.Fatal("no original receiver in actual model channel: " + envelope.Output.Context)
	}
	command, _, found = strings.Cut(command, ". This explicit recovery")
	if !found {
		t.Fatal("receiver command has no exact boundary: " + command)
	}
	argv, err := nativeLiveCommandWords(command)
	if err != nil || len(argv) == 0 || argv[0] != filepath.Join(root, ".claude/bin/loop-harness") {
		t.Fatalf("actual emitted command: %q %v", command, err)
	}
	writeRepairLeadFile(t, worker, ".claude/submissions/logs/missing-handoff.txt", "original evidence restored\n")
	out.Reset()
	diagnostic.Reset()
	if code := Run(argv[1:], nil, &out, &diagnostic); code != 0 {
		t.Fatalf("actual emitted argv failed: %d %s %s", code, &out, &diagnostic)
	}
	current := repairDispatchSnapshot(t, root)
	_, e := repairExecutionForChoice(current.State, repairDispatchChoice{AssignmentID: execution.AssignmentID})
	if stringValue(e["delivery_ref"]) == "" {
		t.Fatal("actual public recovery command did not record original candidate")
	}
}
