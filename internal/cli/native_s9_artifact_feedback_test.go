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

	"github.com/entroforge/go-system-builder/internal/submission"
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
	root, worker, packet, _, _ := nativeS9WorkerFixture(t)
	// The current PlanReport is the flat private file; the removed envelope
	// form (/answer/...) is no longer produced or read
	// (L4-workflow-convergence §"S6／S9 当前工作区"). The original consumer
	// must still return the exact field error of the current flat shape
	// (L3-S9 §"字段缺失时保留原稿,反馈实际文件与字段").
	plan, _ := nativeS9TestAuthorPaths(t, root, packet)
	raw, err := os.ReadFile(filepath.Join(worker, plan))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["plan"] = ""
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, plan), document); err != nil {
		t.Fatal(err)
	}
	service, serviceErr := submission.New(root)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	err = receiveNativeS9ReadyPlan(context.Background(), root, worker, packet, service, &bytes.Buffer{}, nil)
	var fields *workpacket.AnswerError
	if !errors.As(err, &fields) || len(fields.Fields) == 0 {
		t.Fatalf("original consumer did not return the field error: %+v %v", fields, err)
	}
	if fields.Fields[0].Pointer != "/answer/plan" {
		t.Fatalf("field error pointer = %q", fields.Fields[0].Pointer)
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
