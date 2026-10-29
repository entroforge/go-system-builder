package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The public division-save preparation must give the author the same original
// assertion set that the existing Result consumer will require. The submitted
// passing Plan precheck is a subset, and one Result observation was already
// satisfied before repair. These are synthetic observations, not product claims.
func TestNativeS9PreparedBriefAndConsumerPreserveSatisfiedAssertion(t *testing.T) {
	for _, omitSatisfied := range []bool{false, true} {
		name := "complete-original-set"
		if omitSatisfied {
			name = "omitted-satisfied-assertion"
		}
		t.Run(name, func(t *testing.T) {
			root, worker, p, input, answer := nativeS9WorkerFixture(t, 2)
			if len(answer.BeforeFixResults) != 6 {
				t.Fatalf("original assignment has %d observations, want 6", len(answer.BeforeFixResults))
			}
			var assignment repair.RepairAssignment
			if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
				t.Fatal(err)
			}
			brief, _, _ := nativeS9Paths(p)
			planPath, result := nativeS9TestAuthorPaths(t, root, p)
			data, err := os.ReadFile(filepath.Join(root, brief))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			_, after, ok := strings.Cut(text, "assertion_id in this original RepairAssignment, in its original order:\n")
			if !ok {
				t.Fatal("prepared Result field lacks the original ordered assertion set")
			}
			idsLine, _, _ := strings.Cut(after, "\n")
			var suppliedIDs []string
			if err := json.Unmarshal([]byte(idsLine), &suppliedIDs); err != nil || !reflect.DeepEqual(suppliedIDs, assignment.AssertionIDs) {
				t.Fatalf("prepared brief changed original assignment assertions: %q, %v", idsLine, err)
			}
			for _, required := range []string{
				"prechecks may cover a subset; they do not define or reduce the",
				"including when Main selects a RED experiment subset",
				"Include assertions already satisfied",
				"observed_red\nis a historical field name",
				"without manufacturing a failure or omitting an already satisfied assertion",
				"assignment " + assignment.AssignmentID + " in the original RepairPlan " + filepath.Join(root, p.Binding.PlanRef),
			} {
				if !strings.Contains(text, required) {
					t.Fatalf("prepared professional guidance missing %q", required)
				}
			}
			var plan workpacket.RepairPlanReportProfessionalAnswer
			readRepairLeadJSON(t, filepath.Join(worker, planPath), &plan)
			if len(plan.RedChecks) != 1 || plan.RedChecks[0].Result != "pass" {
				t.Fatalf("fixture must actually submit a passing precheck subset: %#v", plan.RedChecks)
			}
			// symptom-2 is already satisfied; an honest non-RED observation still
			// belongs in the original six-row Result responsibility.
			satisfiedIndex := -1
			for i := range answer.BeforeFixResults {
				if answer.BeforeFixResults[i].AssertionID == "symptom-2" {
					satisfiedIndex = i
					answer.BeforeFixResults[i].ObservedRed = "Already satisfied before repair: actual precheck passed; no failure reproduced."
				}
			}
			if satisfiedIndex < 0 {
				t.Fatal("original six-assertion fixture lacks symptom-2")
			}
			if omitSatisfied {
				answer.BeforeFixResults = append(answer.BeforeFixResults[:satisfiedIndex], answer.BeforeFixResults[satisfiedIndex+1:]...)
			}
			if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
				t.Fatal(err)
			}
			input.Event = "SubagentStop"
			var out, diagnostic bytes.Buffer
			if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 || out.Len() != 0 {
				t.Fatalf("first Stop must hand back through original route: %v %d %s %s", handled, code, &out, &diagnostic)
			}
			service, err := submission.NewReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			planReceipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
			if err != nil || !planReceipt.Accepted {
				t.Fatalf("passing precheck subset was not accepted: %v %#v %s", err, planReceipt, &diagnostic)
			}
			snap := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snap.State)
			if err != nil || registry == nil {
				t.Fatalf("original native source not registered: %v", err)
			}
			execution := registry.Executions[p.Binding.ManifestAssignmentID]
			packets, err := runtime.CurrentRepairResultPackets(root, snap.State, p.Binding.OwnerAgentID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("original Result packet missing: %v", err)
			}
			resultReceipt, receiptErr := service.RepairResultReceipt(p.Binding.OwnerAgentID, packets[0].ID)
			if omitSatisfied {
				if !strings.Contains(diagnostic.String(), "before_fix_results") || !strings.Contains(diagnostic.String(), "got 5 want 6") || execution.DeliveryRef != "" || receiptErr == nil && resultReceipt.Accepted {
					t.Fatalf("omitted satisfied assertion bypassed original cardinality: %v %#v %s", receiptErr, resultReceipt, &diagnostic)
				}
				main := nativeExecutionMainOutput(t, root, filepath.Join(root, brief), map[string]any{"status": "completed"})
				if !strings.Contains(main, "got 5 want 6") || !strings.Contains(main, "PlanReport receipt is accepted") {
					t.Fatalf("Main lacks original partial reception and concrete gap: %s", main)
				}
				return
			}
			if receiptErr != nil || !resultReceipt.Accepted || execution.DeliveryRef == "" {
				t.Fatalf("complete Result with already-satisfied observation not accepted as candidate: %v %#v %s", receiptErr, resultReceipt, &diagnostic)
			}
		})
	}
}
