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
	"github.com/entroforge/go-system-builder/internal/repair"
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
	brief := nativeS9BriefPath(plan)
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
	brief := nativeS9BriefPath(plan)
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

// s9RestoreAuthorOriginalResult puts the author's original Result back at the
// exact flat author path. The shared handoff fixture removes that original to
// reach the missing-candidate boundary; the recovery command form is only
// observable when the original is present again. The restored bytes are the
// same professional answer the worker fixture persisted
// (native_s9_test.go:62-74): the Result of the plan's own assertion and unit
// rows.
// s9OriginalResultAnswer is the author's original professional Result for the
// current original repair plan (native_s9_test.go:62-74): the Result of the
// plan's own assertion and unit rows.
func s9OriginalResultAnswer(t *testing.T, root string) workpacket.RepairResultProfessionalAnswer {
	t.Helper()
	snap := repairDispatchSnapshot(t, root)
	planRef := stringValue(repairPointerCLI(snap.State)["plan_ref"])
	if planRef == "" {
		t.Fatal("the original repair plan reference is missing")
	}
	var original repair.RepairPlan
	readRepairLeadJSON(t, filepath.Join(root, planRef), &original)
	if len(original.Assignments) == 0 {
		t.Fatal("the original repair plan has no assignment")
	}
	answer := workpacket.RepairResultProfessionalAnswer{
		Verdict:               "completed",
		BeforeFixResults:      []workpacket.RepairResultBeforeFixResult{},
		UnitResults:           []workpacket.RepairResultUnitResult{},
		DetectionGapArtifacts: []string{},
		EvidenceRefs:          []string{"internal/api/one.go"},
		ScopeDeviations:       []string{},
		ResidualRisks:         []string{},
	}
	for _, id := range original.Assignments[0].AssertionIDs {
		answer.BeforeFixResults = append(answer.BeforeFixResults, workpacket.RepairResultBeforeFixResult{AssertionID: id, ObservedRed: "synthetic fixture observation before repair", EvidenceRefs: []string{"internal/api/one.go"}})
	}
	for _, id := range original.Assignments[0].UnitIDs {
		answer.UnitResults = append(answer.UnitResults, workpacket.RepairResultUnitResult{UnitID: id, Status: "pass", EvidenceRefs: []string{"internal/api/one.go"}, ImplementationSummary: "Restored first API authority", RestoredInvariantRefs: []string{}})
	}
	return answer
}

func s9RestoreAuthorOriginalResult(t *testing.T, root, worker string, plan workpacket.RepairPlanReportPacket) string {
	t.Helper()
	result := nativeS9TestResultPath(t, root, plan)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(worker, result)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), s9OriginalResultAnswer(t, root)); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestS9CapturedRecoveryCommandExecutesOriginalTypedConsumer(t *testing.T) {
	// The command form: with the author's original Result present, the context
	// hands Main the executable original receiver for that exact original path.
	root, worker, plan, _, packet := s9HandoffMissingCandidate(t)
	flat := nativeS9TestResultPath(t, root, plan)
	typed := filepath.Join(worker, packet.AnswerPath)
	s9RestoreAuthorOriginalResult(t, root, worker, plan)
	handoff := s9HandoffForBinding(t.Context(), root, repairDispatchSnapshot(t, root), plan.Binding)
	text := s9UnreceivedActionContext(t.Context(), root, handoff)
	command := s9OriginalConsumeCommand(root, filepath.Join(worker, flat))
	if !strings.Contains(text, command) {
		t.Fatalf("missing the original author recovery command: %s", text)
	}
	// The typed window is a coordinate, never the recovery target: the saved
	// original author path is the only original source (L3-S9-repair.md:134
	// "唯一作者路径…不创建另一套 typed／flat 作者副本").
	if strings.Contains(text, s9OriginalConsumeCommand(root, typed)) {
		t.Fatalf("the typed coordinate became the recovery target: %s", text)
	}
}

func TestS9CapturedRecoveryCommandReportsMissingOriginalMaterial(t *testing.T) {
	// The source-missing form: with no saved original Result, Main must report
	// the material gap and hand out no executable consume command.
	root, worker, plan, _, packet := s9HandoffMissingCandidate(t)
	flat := nativeS9TestResultPath(t, root, plan)
	typed := filepath.Join(worker, packet.AnswerPath)
	handoff := s9HandoffForBinding(t.Context(), root, repairDispatchSnapshot(t, root), plan.Binding)
	text := s9UnreceivedActionContext(t.Context(), root, handoff)
	if !strings.Contains(text, filepath.Join(worker, flat)) {
		t.Fatalf("the recovery context did not name the original author path: %s", text)
	}
	if !strings.Contains(text, "no such file or directory") {
		t.Fatalf("the recovery context did not state the concrete source gap: %s", text)
	}
	if strings.Contains(text, "runtime stage-answer consume") || strings.Contains(text, "then run this exact original receiver") {
		t.Fatalf("a missing original must not carry an executable consume command: %s", text)
	}
	if strings.Contains(text, typed) {
		t.Fatalf("the typed coordinate became a consumer target: %s", text)
	}
}

func TestS9CapturedTypedConsumerEntryStillReceivesOriginalCandidate(t *testing.T) {
	// The original typed consumer entry stays usable: the original consumer
	// still forms the original candidate from the typed coordinate.
	root, worker, plan, execution, packet := s9HandoffMissingCandidate(t)
	// The typed coordinate receives the author's complete professional Result,
	// so the original typed consumer entry can still receive it.
	typedPath := filepath.Join(worker, packet.AnswerPath)
	var envelope map[string]any
	readRepairLeadJSON(t, typedPath, &envelope)
	envelope["answer"] = s9OriginalResultAnswer(t, root)
	writeRepairLeadJSON(t, typedPath, envelope)
	writeRepairLeadFile(t, worker, ".claude/submissions/logs/missing-handoff.txt", "original evidence restored\n")
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", typedPath}, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("receiver: %d %s %s", code, &out, &diagnostic)
	}
	current := repairDispatchSnapshot(t, root)
	// The original candidate is only real when the original agent's execution
	// carries a delivery coordinate.
	executions := mapFieldCLI(mapFieldCLI(current.State, "workspace"), "executions")
	delivered := map[string]any{}
	for key := range executions {
		row := mapFieldCLI(executions, key)
		if stringValue(row["delivery_ref"]) != "" && stringValue(row["agent_id"]) == execution.AgentID {
			delivered = row
		}
	}
	if len(delivered) == 0 {
		t.Fatalf("feedback command did not form original candidate: executions=%v", executions)
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
	flat := nativeS9TestResultPath(t, root, plan)
	typed := filepath.Join(worker, packet.AnswerPath)
	// The typed coordinate names a real file, yet a coordinate alone is not a
	// saved typed input.
	if _, err := os.Stat(typed); err != nil {
		t.Fatal(err)
	}
	claim := func() s9HandoffProjection {
		p := s9HandoffForBinding(t.Context(), root, repairDispatchSnapshot(t, root), plan.Binding)
		p.Attempt.Stage = "typed_result_prepared"
		p.Attempt.TypedInputRef = typed
		return p
	}
	// The original author coordinate is the flat author path. While that
	// original is missing Main must report the material gap, and the typed
	// coordinate must not become a consumer target.
	missing := s9UnreceivedActionContext(t.Context(), root, claim())
	if !strings.Contains(missing, filepath.Join(worker, flat)) {
		t.Fatalf("the recovery context did not name the original author path: %s", missing)
	}
	if !strings.Contains(missing, "no such file or directory") {
		t.Fatalf("the recovery context did not state the concrete source gap: %s", missing)
	}
	if strings.Contains(missing, "runtime stage-answer consume") || strings.Contains(missing, "then run this exact original receiver") {
		t.Fatalf("a missing original must not carry an executable consume command: %s", missing)
	}
	if strings.Contains(missing, typed) {
		t.Fatalf("the typed coordinate alone became a consumer target: %s", missing)
	}
	// With the author's original Result present, the same coordinate claim
	// still resolves to the original author path and never to the typed window.
	s9RestoreAuthorOriginalResult(t, root, worker, plan)
	present := s9UnreceivedActionContext(t.Context(), root, claim())
	command := s9OriginalConsumeCommand(root, filepath.Join(worker, flat))
	if !strings.Contains(present, command) {
		t.Fatalf("the original author path did not carry the executable recovery command: %s", present)
	}
	if strings.Contains(present, s9OriginalConsumeCommand(root, typed)) {
		t.Fatalf("the typed coordinate became the recovery target: %s", present)
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
	// The command form: with the author's original Result present, the public
	// Main channel hands out the executable original receiver, and that exact
	// argv really runs.
	root, worker, plan, execution, packet := s9HandoffMissingCandidate(t)
	flat := nativeS9TestResultPath(t, root, plan)
	typed := filepath.Join(worker, packet.AnswerPath)
	s9RestoreAuthorOriginalResult(t, root, worker, plan)
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
	// The emitted receiver names the saved original author path, never the
	// typed coordinate (L3-S9-repair.md:134).
	if !strings.Contains(command, filepath.Join(worker, flat)) {
		t.Fatalf("the emitted receiver did not name the original author path: %q", command)
	}
	if strings.Contains(command, typed) {
		t.Fatalf("the typed coordinate became the recovery target: %q", command)
	}
	writeRepairLeadFile(t, worker, ".claude/submissions/logs/missing-handoff.txt", "original evidence restored\n")
	out.Reset()
	diagnostic.Reset()
	if code := Run(argv[1:], nil, &out, &diagnostic); code != 0 {
		t.Fatalf("actual emitted argv failed: %d %s %s", code, &out, &diagnostic)
	}
	current := repairDispatchSnapshot(t, root)
	// The original candidate is only real when the original agent's execution
	// carries a delivery coordinate.
	executions := mapFieldCLI(mapFieldCLI(current.State, "workspace"), "executions")
	delivered := map[string]any{}
	for key := range executions {
		row := mapFieldCLI(executions, key)
		if stringValue(row["delivery_ref"]) != "" && stringValue(row["agent_id"]) == execution.AgentID {
			delivered = row
		}
	}
	if len(delivered) == 0 {
		t.Fatal("actual public recovery command did not record original candidate")
	}
}

func TestS9PublicFeedbackReportsMissingOriginalMaterial(t *testing.T) {
	// The source-missing form: with no saved original Result the public Main
	// channel must report the material gap and hand out no executable receiver.
	root, worker, plan, _, _ := s9HandoffMissingCandidate(t)
	flat := nativeS9TestResultPath(t, root, plan)
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
	context := envelope.Output.Context
	if strings.Contains(context, "then run this exact original receiver: ") {
		t.Fatalf("a missing original must not hand out an executable receiver: %s", context)
	}
	if !strings.Contains(context, filepath.Join(worker, flat)) {
		t.Fatalf("the public feedback did not name the original author path: %s", context)
	}
	if !strings.Contains(context, "no such file or directory") {
		t.Fatalf("the public feedback did not state the concrete source gap: %s", context)
	}
}
