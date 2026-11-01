package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// Both responsibilities are issued by the real dispatch consumer. This proves
// routing separately from the shared Writer's full checkpoint obligation. The
// saved original bytes below are a test backup, not production automatic repair.
func TestNativeS9SiblingBodyFaultSelectsWorkerAndKeepsWriterPending(t *testing.T) {
	root, worker, packet, in, _ := nativeS9WorkerFixture(t)
	snap := repairDispatchSnapshot(t, root)
	plan, err := repairDispatchPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	repairDispatchSave(t, root, repairDispatchPaths(root, snap.State)[0], []repairDispatchChoice{
		{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"},
		{AssignmentID: plan.Assignments[1].AssignmentID, RoleFamily: "backend-builder"},
	})
	snap = repairDispatchSnapshot(t, root)
	routes, err := nativeS9Routes(t.Context(), snap.State)
	if err != nil || len(routes) != 2 {
		t.Fatalf("original responsibilities: %d %v", len(routes), err)
	}
	siblingBody := ""
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if row["template_id"] != "s9-plan-report-v1" || row["packet_id"] == packet.ID {
			continue
		}
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil || !external {
			t.Fatalf("original external sibling: %v %v", external, err)
		}
		siblingBody = filepath.Join(root, filepath.FromSlash(ref.Path))
	}
	if siblingBody == "" {
		t.Fatal("missing original sibling body")
	}
	original, err := os.ReadFile(siblingBody)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := nativeS9AuthorPaths(root, packet)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worker, result)); err != nil {
		t.Fatal(err)
	}
	brief := nativeS9BriefPath(packet)
	parent := filepath.Join(t.TempDir(), "parent.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+in.AgentID+".jsonl")
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": in.AgentID, "message": map[string]any{"role": "user", "content": "Repair using this exact brief " + filepath.Join(root, brief)}})
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("actual parent session"), 0600); err != nil {
		t.Fatal(err)
	}
	in.Event, in.CWD, in.ToolName, in.LastAssistantMessage, in.TranscriptPath = "PreToolUse", worker, "Bash", "", parent
	request, _ := json.Marshal(in)
	if err := os.Remove(siblingBody); err != nil {
		t.Fatal(err)
	}
	// Routing is index-only and retains the actual control root and selected body.
	if got := nativeS6ControlRoot(worker, in); got != root {
		t.Fatalf("control root lost to sibling fault: %s", got)
	}
	selectedRoute := routes[0]
	if selectedRoute.ID != packet.ID {
		selectedRoute = routes[1]
	}
	selected, err := nativeS9RoutePacket(t.Context(), root, snap.State, selectedRoute)
	if err != nil || selected.ID != packet.ID {
		t.Fatalf("selected body: %s %v", selected.ID, err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", worker, "--event", in.Event}, bytes.NewReader(request), &out, &diagnostic); code != 0 {
		t.Fatalf("ordinary tool was blocked: %d %s", code, &diagnostic)
	}
	feedback := out.String() + diagnostic.String()
	if !strings.Contains(feedback, packet.Binding.OwnerAgentID) || !strings.Contains(feedback, filepath.Base(siblingBody)) || strings.Contains(out.String(), "was accepted") {
		t.Fatalf("selected Worker failure was silent or fabricated acceptance: %s", feedback)
	}
	// Assert the actual Worker context independently of stderr diagnostics.
	workerFeedback := nativeS9ListenerFeedback(t, &out)
	relativeBody, err := filepath.Rel(root, siblingBody)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{root, filepath.ToSlash(relativeBody), "Original consumer diagnostic:", "does not confirm a receipt", "may remain pending", "Main must preserve original artifacts", "Framework maintainers", "does not require the Builder to resubmit"} {
		if !strings.Contains(workerFeedback, want) {
			t.Errorf("Worker feedback lost %q: %s", want, workerFeedback)
		}
	}
	for _, wrong := range []string{"correct any identified professional fields", "was accepted", "runtime recover", "internal Go"} {
		if strings.Contains(workerFeedback, wrong) {
			t.Errorf("Worker feedback invented an author obligation or recovery: %q: %s", wrong, workerFeedback)
		}
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if _, err := os.Stat(statePath + ".commit-pending.json"); err != nil {
		t.Fatalf("shared Writer pending was lost: %v %s", err, feedback)
	}
	// Restore only known original bytes; the unchanged author input is observed
	// again after the original Writer has completed its full checkpoint.
	if err := os.WriteFile(siblingBody, original, 0600); err != nil {
		t.Fatal(err)
	}
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(t.Context())
	if _, err := writer.RecoverPendingOperations(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	diagnostic.Reset()
	if code := Run([]string{"hook", "--root", worker, "--event", in.Event}, bytes.NewReader(request), &out, &diagnostic); code != 0 {
		t.Fatalf("original continuation: %d %s", code, &diagnostic)
	}
	service := &submission.Service{Root: root, StatePath: statePath, JournalPath: journalPath}
	receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original formal receipt after trusted restoration: %+v %v %s %s", receipt, err, &out, &diagnostic)
	}
}
