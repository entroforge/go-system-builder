package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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
			brief := nativeS9BriefPath(plan)
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
	brief := nativeS9BriefPath(plan)
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
