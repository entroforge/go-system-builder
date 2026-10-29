package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The fixture changes one freshly issued packet and its event into a known
// historical issuance before exercising production readers, issuers and
// consumers. Runtime is never rewritten this way by the implementation.
func seedS9HistoricalPacket(t *testing.T, root, currentID, oldID string, old any) {
	t.Helper()
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	index, err := packetbody.Publish(t.Context(), root, body)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	rows := state["entities"].(map[string]any)["work_packets"].([]any)
	count := 0
	for i, row := range rows {
		if row.(map[string]any)["packet_id"] == currentID {
			rows[i] = index
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one current issuance, got %d", count)
	}
	stateBytes, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	stateBytes = bytes.ReplaceAll(stateBytes, []byte(currentID), []byte(oldID))
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(journalBytes, []byte(currentID)) {
		t.Fatal("fixture did not find current issuance event")
	}
	journalBytes = bytes.ReplaceAll(journalBytes, []byte(currentID), []byte(oldID))
	if err := os.WriteFile(statePath, stateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journalBytes, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestS9HistoricalPlanTemplateReissueAndOriginalConsumer(t *testing.T) {
	root, _, current, _, _ := nativeS9WorkerFixture(t)
	old, err := workpacket.RebuildRepairPlanReport(workpacket.RepairPlanReportPacket{
		Binding: current.Binding, Inputs: current.Inputs,
		TemplateSHA: "04750203e18d01207a33aa3f84880f8451d4ff9b89446eddd27cef3ace0a0110",
	})
	if err != nil || old.ID == current.ID {
		t.Fatalf("historical Plan fixture: %v", err)
	}
	seedS9HistoricalPacket(t, root, current.ID, old.ID, old)
	snap := repairDispatchSnapshot(t, root)
	if err := runtime.VerifyRepairPlanReportPacketIssue(root, snap.State, old); err != nil {
		t.Fatalf("original Plan issuance: %v", err)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	replayed, err := service.IssueRepairPlanReports(t.Context(), old.Binding.OwnerAgentID)
	if err != nil || len(replayed) != 1 || !reflect.DeepEqual(replayed[0], old) {
		t.Fatalf("did not reuse old Plan: %+v %v", replayed, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	question := old.Draft
	question.Answer, _ = json.Marshal(workpacket.RepairPlanReportProfessionalAnswer{
		Plan:          "Review the original assertion and verify the scoped repair.",
		RedChecks:     []workpacket.RepairPlanReportCheck{{Name: "boundary", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"test://actual-precheck"}}},
		ProposedPaths: []string{"internal/api/one.go"},
	})
	path := filepath.Join(root, old.AnswerPath)
	if err := persistRepairLeadQuestionnaire(path, question); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveRepairPlanReportFrom(t.Context(), old.Binding.OwnerAgentID, path, root, nil)
	if err != nil || !receipt.Accepted || receipt.PacketID != old.ID || receipt.WorkComplete {
		t.Fatalf("old Plan original consumer: %+v %v", receipt, err)
	}
	before = repairDispatchRuntimeBytes(t, root)
	if replay, err := service.SaveRepairPlanReportFrom(t.Context(), old.Binding.OwnerAgentID, path, root, nil); err != nil || !replay.Accepted || replay.PacketID != old.ID {
		t.Fatalf("old Plan receipt replay: %+v %v", replay, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	snap = repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairPlanReportPackets(root, snap.State, old.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 || !reflect.DeepEqual(packets[0], old) {
		t.Fatalf("old Plan after acceptance: %+v %v", packets, err)
	}
}

func TestS9HistoricalResultTemplateReissueAndOriginalConsumer(t *testing.T) {
	root, worker, plan, _, professional := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := importNativeS9PlanReport(t.Context(), root, worker, plan, service, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("Plan: %+v %v", receipt, err)
	}
	if err := recordNativeS9Source(t.Context(), root, worker, repairLeadGit(t, root, "rev-parse", "HEAD"), plan); err != nil {
		t.Fatal(err)
	}
	current, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 {
		t.Fatalf("Result issue: %+v %v", current, err)
	}
	snap := repairDispatchSnapshot(t, root)
	if err := runtime.VerifyRepairResultPacketIssue(root, snap.State, current[0]); err != nil {
		t.Fatalf("current Result issuance before upgrade: %v", err)
	}
	beforeExecution, err := runtime.RepairResultExecution(snap.State, current[0])
	if err != nil {
		t.Fatalf("current Result execution before upgrade: %v", err)
	}
	old, err := workpacket.RebuildRepairResult(workpacket.RepairResultPacket{
		Binding: current[0].Binding, Inputs: current[0].Inputs,
		TemplateSHA: "56cd501c2d0ff973139cf3618785eda0c3a77386fbb750826721ec1fc52e40fe",
	})
	if err != nil || old.ID == current[0].ID {
		t.Fatalf("historical Result fixture: %v", err)
	}
	seedS9HistoricalPacket(t, root, current[0].ID, old.ID, old)
	snap = repairDispatchSnapshot(t, root)
	if err := runtime.VerifyRepairResultPacketIssue(root, snap.State, old); err != nil {
		t.Fatalf("original Result issuance: %v", err)
	}
	// The combined legacy current verifier requires a bootstrap SHA; native D
	// deliberately has none. The actual native consumer checks this exact
	// issued execution and ready attempt independently of that old launcher.
	execution, err := runtime.RepairResultExecution(snap.State, old)
	if err != nil || !reflect.DeepEqual(execution, beforeExecution) || execution["status"] != "ready" ||
		execution["purpose"] != "native_delivery" || execution["worktree_path"] != worker ||
		execution["agent_id"] != old.Binding.Sources.OwnerAgentID || stringValue(execution["rework_ref"]) != old.Binding.ReworkRef ||
		stringValue(execution["bootstrap_sha256"]) != "" {
		t.Fatalf("historical native execution/attempt changed: %#v %v", execution, err)
	}
	service, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	replayed, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
	if err != nil || len(replayed) != 1 || !reflect.DeepEqual(replayed[0], old) {
		t.Fatalf("did not reuse old Result: %+v %v", replayed, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	question := old.Draft
	question.Answer, _ = json.Marshal(professional)
	path := filepath.Join(worker, old.AnswerPath)
	if err := persistRepairLeadQuestionnaire(path, question); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveRepairResultFrom(t.Context(), plan.Binding.OwnerAgentID, path, worker, nil)
	if err != nil || !receipt.Accepted || receipt.PacketID != old.ID || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("old Result original consumer: %+v %v", receipt, err)
	}
	before = repairDispatchRuntimeBytes(t, root)
	if replay, err := service.SaveRepairResultFrom(t.Context(), plan.Binding.OwnerAgentID, path, worker, nil); err != nil || !replay.Accepted || replay.PacketID != old.ID {
		t.Fatalf("old Result receipt replay: %+v %v", replay, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	snap = repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairResultPackets(root, snap.State, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 || !reflect.DeepEqual(packets[0], old) {
		t.Fatalf("old Result after acceptance: %+v %v", packets, err)
	}
}
