package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9FlatValidationFeedbackUsesPrivateFileFields(t *testing.T) {
	for _, kind := range []string{"PlanReport", "completed Result", "blocked Result", "unknown Result field", "wrong Result type"} {
		t.Run(kind, func(t *testing.T) {
			root, worker, packet, input, _ := nativeS9WorkerFixture(t)
			plan, result := nativeS9TestAuthorPaths(t, root, packet)
			path, pointer := filepath.Join(worker, result), "/migration_ref"
			if kind == "PlanReport" {
				path, pointer = filepath.Join(worker, plan), "/red_checks/0/result"
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			if kind == "PlanReport" {
				document["red_checks"].([]any)[0].(map[string]any)["result"] = "invented"
			} else if kind == "unknown Result field" {
				document["unrecognized_field"] = true
				pointer = "unrecognized_field"
			} else if kind == "wrong Result type" {
				document["before_fix_results"] = "not an array"
				pointer = "before_fix_results"
			} else {
				document["migration_ref"] = nil
				if kind == "blocked Result" {
					document["verdict"] = "blocked"
				}
			}
			if err := persistRepairLeadQuestionnaire(path, document); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			input.Event = "SubagentStop"
			var out, diagnostic bytes.Buffer
			if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
				t.Fatalf("native return: %t %d %s", handled, code, &diagnostic)
			}
			feedback := diagnostic.String()
			for _, want := range []string{path, pointer, "keep professional fields at the top level", "Main owns recovery"} {
				if !strings.Contains(feedback, want) {
					t.Errorf("missing %q in feedback: %s", want, feedback)
				}
			}
			for _, forbidden := range []string{"/answer/", " /answer:", "填写 answer", "professional Result received"} {
				if strings.Contains(feedback, forbidden) {
					t.Errorf("feedback contains %q: %s", forbidden, feedback)
				}
			}
			if kind == "PlanReport" {
				repairDispatchAssertUnchanged(t, root, before)
			} else {
				registry, err := workspace.Decode(repairDispatchSnapshot(t, root).State)
				if err != nil || registry != nil && registry.Executions[packet.Binding.ManifestAssignmentID].DeliveryRef != "" {
					t.Fatalf("invalid Result created a delivery: %+v %v", registry, err)
				}
			}
		})
	}
}

func TestNativeS9LegacyPlanValidationKeepsOriginalEnvelopeFeedback(t *testing.T) {
	root, worker, packet, _, _ := nativeS9LegacyWorkerFixture(t)
	draft := packet.Draft
	draft.Answer = json.RawMessage(`{"plan":"","red_checks":[],"proposed_paths":[]}`)
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	_, original := workpacket.ValidateRepairPlanReportAnswer(packet, data)
	if len(original) == 0 || original[0].Pointer != "/answer/plan" {
		t.Fatalf("original validator lost envelope contract: %+v", original)
	}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, packet.AnswerPath), draft); err != nil {
		t.Fatal(err)
	}
	err = importNativeS9PlanReport(context.Background(), root, worker, packet, nil, &bytes.Buffer{})
	var fields *workpacket.AnswerError
	if !errors.As(err, &fields) || fields.Fields[0] != original[0] {
		t.Fatalf("legacy wrapped feedback changed: %+v %v", fields, err)
	}
}

func TestNativeS9FlatFeedbackPreservesOriginalError(t *testing.T) {
	original := &workpacket.AnswerError{Label: "S9 answer", Fields: []workpacket.FieldError{{Pointer: "/answer/unit_results/0/status", Expected: "pass | fail | blocked", Observed: "unknown", Repair: "original envelope repair"}}}
	mapped := nativeS9FlatArtifactError("Result", "/worker/private/result.json", original)
	var fields *workpacket.AnswerError
	if !errors.Is(mapped, original) || !errors.As(mapped, &fields) || fields.Fields[0].Pointer != "/unit_results/0/status" {
		t.Fatalf("lost original rejection or mapped field: %v %+v", mapped, fields)
	}
	if original.Fields[0].Pointer != "/answer/unit_results/0/status" || original.Fields[0].Repair != "original envelope repair" {
		t.Fatalf("mutated original consumer error: %+v", original.Fields)
	}
	sourceErr := errors.New("bound source is unavailable")
	if nativeS9FlatArtifactError("Result", "/worker/private/result.json", sourceErr) != sourceErr {
		t.Fatal("source failure became a professional correction")
	}
}
