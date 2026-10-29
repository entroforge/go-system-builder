package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The compatibility entry must describe the same missing candidate as the
// original workspace entry, rather than asking native work to use PostToolUse.
func TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing(t *testing.T) {
	root, worker, packet, input, answer := nativeS9WorkerFixture(t)
	answer.UnitResults = nil // Real professional omission, after the Plan was saved.
	result := nativeS9TestResultPath(t, root, packet)
	privateResult := filepath.Join(worker, result)
	if err := persistRepairLeadQuestionnaire(privateResult, answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var output, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &output, &diagnostic); !handled || code != 0 {
		t.Fatalf("original Stop: %v %d %s", handled, code, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("original execution unavailable: %v", err)
	}
	execution := registry.Executions[packet.Binding.ManifestAssignmentID]
	if execution.Status != "ready" || execution.DeliveryRef != "" {
		t.Fatalf("wrong failure window: %+v; %s", execution, &diagnostic)
	}
	service, err := submission.NewContext(t.Context(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan was not accepted: %+v %v", receipt, err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	answerBefore, err := os.ReadFile(privateResult)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostic.Reset()
	code := Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", execution.AssignmentID, "--agent-id", execution.AgentID}, strings.NewReader(""), &output, &diagnostic)
	if code == 0 {
		t.Fatalf("missing candidate entered integration: %s", &output)
	}
	for _, reference := range []string{execution.AssignmentID, execution.AgentID, packet.ID} {
		if !strings.Contains(diagnostic.String(), reference) {
			t.Errorf("compatibility entry lost original reference %q: %s", reference, &diagnostic)
		}
	}
	for _, forbidden := range []string{"invalid packet artifact path", "through PostToolUse"} {
		if strings.Contains(diagnostic.String(), forbidden) {
			t.Errorf("compatibility entry bypassed original handoff projection: %s", &diagnostic)
		}
	}
	repairDispatchAssertUnchanged(t, root, before)
	after, err := os.ReadFile(privateResult)
	if err != nil || !bytes.Equal(answerBefore, after) {
		t.Fatalf("integration precondition rewrote the professional answer: %v", err)
	}
}
