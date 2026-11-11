package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Reach the original Stop's real partial boundary: the ready Plan and the
// live execution are accepted, while the genuine Result consumer has no
// saved original Result to capture.
func s9HandoffMissingCandidate(t *testing.T) (string, string, workpacket.RepairPlanReportPacket, workspace.Execution, workpacket.RepairResultPacket) {
	t.Helper()
	root, worker, plan, input, _ := nativeS9WorkerFixture(t)
	ctx := t.Context()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(ctx, root, worker, plan, service, &diagnostic, nil); err != nil {
		t.Fatalf("import the original Plan: %v", err)
	}
	receipt, err := service.SaveRepairPlanReportFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan was not received: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(ctx, root, plan, worker); err != nil {
		t.Fatalf("register original native source: %v", err)
	}
	issued, err := service.IssueRepairResults(ctx, plan.Binding.OwnerAgentID)
	if err != nil || len(issued) != 1 {
		t.Fatalf("issue original Result before its saved-input attempt: %v %+v", err, issued)
	}
	// The original Result is not saved yet, so the original Stop reaches the
	// real partial boundary: the ready Plan and the live execution are
	// accepted while the genuine Result consumer has nothing to capture.
	result := nativeS9TestResultPath(t, root, plan)
	if err := os.Remove(filepath.Join(worker, result)); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); code != 0 || !strings.Contains(diagnostic.String(), "Result is not submitted or is unfinished") {
		t.Fatalf("original Stop did not reach the pending Result boundary: %d %s", code, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original execution: %v", err)
	}
	e := registry.Executions[plan.Binding.ManifestAssignmentID]
	if e.DeliveryRef != "" || e.Status != "ready" {
		t.Fatalf("expected registered execution without delivery: %+v", e)
	}
	packet, found, err := typedRepairResultPacketForAssignmentContext(context.Background(), root, snap.State, e.AssignmentID, e.AgentID)
	if err != nil || !found || packet.ID != issued[0].ID {
		t.Fatalf("original issued Result: %t %v %+v", found, err, packet)
	}
	previous, found := nativeS9ReturnDiagnosticRecordContext(ctx, root, snap, plan.ID)
	if !found || previous.Attempt == nil || previous.Attempt.TypedInputRef != "" {
		t.Fatalf("original consumer did not preserve the pending Result boundary: %+v found=%v", previous, found)
	}
	// The preserved attempt names the exact original Plan and Result paths, so
	// Main can locate the actual responsibility. No digest is recorded for it.
	if !strings.Contains(previous.Message, filepath.Join(worker, nativeS9TestPlanPath(t, root, plan))) || !strings.Contains(previous.Message, filepath.Join(worker, result)) {
		t.Fatalf("original consumer did not name the exact Plan and Result paths: %s", previous.Message)
	}
	return root, worker, plan, e, packet
}

func TestS9HandoffMissingCandidatePreservesPlanAcrossAllEntrances(t *testing.T) {
	root, worker, plan, e, result := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	brief := nativeS9BriefPath(plan)
	for _, subject := range []nativeReturnSubject{{kind: "s9-plan-report-v1", id: plan.ID}, {kind: "s9-repair-result-v1", id: result.ID}} {
		summary, err := nativeReturnSummary(root, snap, subject)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Only the PlanReport receipt is accepted", "Candidate reception is not established", "Consumer diagnostic", filepath.Join(root, brief), worker} {
			if !strings.Contains(summary, want) {
				t.Errorf("Main %s missing %q: %s", subject.kind, want, summary)
			}
		}
	}
	progress := s9QuestionnaireContext(context.Background(), root, policy.Input{AgentID: "Main"}, nil, snap, "Main", true)
	for _, want := range []string{"The PlanReport receipt is accepted", "Candidate reception is not established", "Consumer diagnostic"} {
		if !strings.Contains(progress, want) {
			t.Errorf("progress missing %q: %s", want, progress)
		}
	}
	var out, diagnostic bytes.Buffer
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID}, &out, &diagnostic); code == 0 {
		t.Fatal("missing candidate was integrated")
	}
	for _, want := range []string{"The PlanReport receipt is accepted", "Candidate reception is not established", "Consumer diagnostic", filepath.Join(root, brief), worker} {
		if !strings.Contains(diagnostic.String(), want) {
			t.Errorf("integrate missing %q: %s", want, &diagnostic)
		}
	}
	if strings.Contains(diagnostic.String(), "invalid packet artifact path") {
		t.Fatal(diagnostic.String())
	}
	repairDispatchAssertUnchanged(t, root, before)
	if _, err := os.Stat(workspaceExecutionCheckpointPath(root, e)); !os.IsNotExist(err) {
		t.Fatalf("missing candidate opened integration: %v", err)
	}
}

func TestS9HandoffIncompleteBindingNeverOpensCandidate(t *testing.T) {
	for _, side := range []string{"path"} {
		t.Run(side, func(t *testing.T) {
			root, _, _, e, _ := s9HandoffMissingCandidate(t)
			snap := repairDispatchSnapshot(t, root)
			row := snap.State["workspace"].(map[string]any)["executions"].(map[string]any)[e.AssignmentID].(map[string]any)
			row["delivery_ref"] = ".claude/nonexistent-original-candidate.json"
			raw, err := json.Marshal(snap.State)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			var diagnostic bytes.Buffer
			if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID}, &bytes.Buffer{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "original candidate source") || !strings.Contains(diagnostic.String(), "is unavailable") {
				t.Fatalf("half-bound source: %d %s", code, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

func TestS9HandoffTypedCandidateCannotLoseProvenanceOrSource(t *testing.T) {
	root, _, e, packet, _ := s9NativeIntegrationCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef})
	if err != nil {
		t.Fatal(err)
	}
	candidate.Provenance = nil
	before := repairDispatchRuntimeBytes(t, root)
	if _, typed, err := currentRepairResultPacketForDeliveryContext(context.Background(), root, snap.State, e, candidate); err == nil || !typed || !strings.Contains(err.Error(), "no legacy fallback") {
		t.Fatalf("missing typed provenance escaped: %t %v", typed, err)
	}
	wrongOwner := e
	wrongOwner.AgentID = "different-owner"
	if _, _, err := currentRepairResultPacketForDeliveryContext(context.Background(), root, snap.State, wrongOwner, candidate); err == nil {
		t.Fatal("typed responsibility selected for wrong owner")
	}
	// A selected packet-body failure is not evidence that no typed packet exists.
	var bodyPath string
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if row["packet_id"] != packet.ID {
			continue
		}
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil || !external {
			t.Fatalf("expected real external Result body: %v", err)
		}
		bodyPath = filepath.Join(root, ref.Path)
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bodyPath); err != nil {
		t.Fatal(err)
	}
	if _, typed, err := currentRepairResultPacketForDeliveryContext(context.Background(), root, snap.State, e, candidate); err == nil || !typed {
		t.Fatalf("missing original body allowed legacy: %t %v", typed, err)
	}
	if err := os.WriteFile(bodyPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := currentRepairResultPacketForDeliveryContext(ctx, root, snap.State, e, candidate); err != context.Canceled {
		t.Fatalf("lost caller cancellation: %v", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9HandoffResultDiagnosticUsesUniqueOriginalPlanBinding(t *testing.T) {
	root, _, plan, _, result := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	diagnostic := nativeS9ReturnDiagnosticContext(context.Background(), root, snap, result.ID)
	if diagnostic == "" || diagnostic != nativeS9ReturnDiagnostic(root, snap, plan.ID) {
		t.Fatalf("Result lost original Plan diagnostic: %s", diagnostic)
	}
	rows := objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"])
	var original map[string]any
	for _, row := range rows {
		if row["packet_id"] == plan.ID {
			original = row
		}
	}
	data, _ := json.Marshal(original)
	var duplicate map[string]any
	_ = json.Unmarshal(data, &duplicate)
	duplicate["packet_id"] = "different-same-owner-plan"
	binding := duplicate["binding"].(map[string]any)
	binding["repair_assignment_id"] = "different-assignment"
	entities := mapFieldCLI(snap.State, "entities")
	entities["work_packets"] = append(entities["work_packets"].([]any), duplicate)
	if key, err := nativeS9PlanDiagnosticKey(snap.State, result.ID); err != nil || key != plan.ID {
		t.Fatalf("same owner neighbor stole diagnostic: %s %v", key, err)
	}
	binding["repair_assignment_id"] = plan.Binding.RepairAssignmentID
	if _, err := nativeS9PlanDiagnosticKey(snap.State, result.ID); err == nil {
		t.Fatal("ambiguous same-binding Plan selected arbitrarily")
	}
}

func TestS9HandoffCanceledIntegrationAndProjectionPreserveKnownFacts(t *testing.T) {
	root, _, plan, e, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	projected := s9HandoffForBinding(ctx, root, snap, plan.Binding)
	if projected.Plan.id != plan.ID || projected.ReadError != context.Canceled || !strings.Contains(projected.summary(root), e.AgentID) {
		t.Fatalf("cancellation erased known responsibility: %+v", projected)
	}
	var diagnostic bytes.Buffer
	if code := runWorkspaceDeliveryContext(ctx, []string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID}, &bytes.Buffer{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "context canceled") {
		t.Fatalf("canceled integration: %d %s", code, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9HandoffFinalProjectionRetainsOriginalCleanupResponsibility(t *testing.T) {
	root, _, e, packet, _, _ := s9BeforeIntegrationAck(t, true)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	summary, err := nativeReturnSummary(root, snap, nativeReturnSubject{kind: "s9-repair-result-v1", id: packet.ID})
	if err != nil {
		t.Fatal(err)
	}
	progress := s9QuestionnaireContext(context.Background(), root, policy.Input{AgentID: "Main"}, nil, snap, "Main", true)
	for _, text := range []string{summary, progress} {
		if !strings.Contains(text, "checkpoint ACK/cleanup") || strings.Contains(text, s9CandidateAction(root, e.AssignmentID, e.AgentID)) {
			t.Fatalf("Final projected as unreceived candidate: %s", text)
		}
	}
	repairDispatchAssertUnchanged(t, root, before)
}

// The typed selector must use a frozen execution, not owner or alias alone.
func TestS9HandoffTypedSelectorPreservesChangedAttemptFence(t *testing.T) {
	root, _, e, packet, _ := s9NativeIntegrationCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	row := mapFieldCLI(mapFieldCLI(snap.State, "workspace"), "executions")[e.AssignmentID].(map[string]any)
	row["execution_generation"] = e.Generation + 1
	selected, found, err := typedRepairResultPacketForAssignmentContext(context.Background(), root, snap.State, e.AssignmentID, e.AgentID)
	if err == nil || !found || selected.ID != packet.ID {
		t.Fatalf("changed attempt lost typed fence: %t %v", found, err)
	}
	if _, err := runtime.RepairResultExecution(snap.State, selected); err == nil {
		t.Fatal("changed generation accepted as original attempt")
	}
	if _, found, err := typedRepairResultPacketForAssignmentContext(context.Background(), root, snap.State, "unrelated-same-owner-assignment", e.AgentID); err != nil || found {
		t.Fatalf("same owner implied assignment identity: %t %v", found, err)
	}
}

func TestS9HandoffDamagedTypedCandidateStopsBeforeMerge(t *testing.T) {
	for _, damage := range []string{"missing provenance", "missing source", "invalid JSON"} {
		t.Run(damage, func(t *testing.T) {
			root, _, e, _, _ := s9NativeIntegrationCandidate(t)
			path := filepath.Join(root, e.DeliveryRef)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "missing provenance":
				var candidate map[string]any
				if err := json.Unmarshal(raw, &candidate); err != nil {
					t.Fatal(err)
				}
				delete(candidate, "provenance")
				raw, err = json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
			case "invalid JSON":
				raw = []byte("{broken original candidate")
			}
			if damage == "missing source" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}

			}
			before := repairDispatchRuntimeBytes(t, root)
			head := repairLeadGit(t, root, "rev-parse", "HEAD")
			var out, diagnostic bytes.Buffer
			code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "false"}, &out, &diagnostic)
			if code == 0 {
				t.Fatal("damaged candidate reached Main integration")
			}
			if damage == "missing provenance" && !strings.Contains(diagnostic.String(), "no legacy fallback") {
				t.Fatalf("typed provenance not diagnosed: %s", &diagnostic)
			}
			if after := repairLeadGit(t, root, "rev-parse", "HEAD"); after != head {
				t.Fatalf("damaged source merged: %s -> %s", head, after)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

func TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty(t *testing.T) {
	root, worker, plan, input, answer := nativeS9WorkerFixture(t)
	if len(answer.UnitResults) == 0 {
		t.Fatal("fixture has no assigned repair unit")
	}
	answer.UnitResults[0].Status = "fail"
	resultPath := nativeS9TestResultPath(t, root, plan)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if _, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); code != 0 {
		t.Fatal(diagnostic.String())
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original registry: %v", err)
	}
	e := registry.Executions[plan.Binding.ManifestAssignmentID]
	if e.DeliveryRef == "" {
		t.Fatalf("partial work not recorded by original consumer: %s", &diagnostic)
	}
	before := repairDispatchRuntimeBytes(t, root)
	summary, err := nativeReturnSummary(root, snap, nativeReturnSubject{kind: "s9-plan-report-v1", id: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "candidate_accepted") || !strings.Contains(summary, "无需重复") {
		t.Fatalf("partial receipt lost acceptance: %s", summary)
	}
	progress := s9QuestionnaireContext(context.Background(), root, policy.Input{AgentID: "Main"}, nil, snap, "Main", true)
	if !strings.Contains(progress, "unfinished") || strings.Contains(progress, s9CandidateAction(root, e.AssignmentID, e.AgentID)) {
		t.Fatalf("partial candidate became merge duty: %s", progress)
	}
	diagnostic.Reset()
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "false"}, &bytes.Buffer{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "professional work remains unfinished") {
		t.Fatalf("partial candidate integrated: %d %s", code, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9HandoffSameOwnerNeighborCannotSupplyOrBreakOriginalCandidate(t *testing.T) {
	root, _, e, packet, _ := s9NativeIntegrationCandidate(t)
	service, err := submission.NewContext(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	snap := repairDispatchSnapshot(t, root)
	// Retain a different assignment under the same owner with an unavailable
	// candidate. Reading this exact receipt must never open that neighbor.
	executions := mapFieldCLI(mapFieldCLI(snap.State, "workspace"), "executions")
	raw, err := json.Marshal(executions[e.AssignmentID])
	if err != nil {
		t.Fatal(err)
	}
	var neighbor map[string]any
	if err := json.Unmarshal(raw, &neighbor); err != nil {
		t.Fatal(err)
	}
	neighbor["assignment_id"] = "unrelated-same-owner"
	neighbor["worktree_path"] = filepath.Join(root, "unrelated-worker")
	neighbor["delivery_ref"] = ".claude/missing-neighbor-candidate.json"
	executions["unrelated-same-owner"] = neighbor
	if complete, err := repair.RepairResultWorkCompleteWithContext(context.Background(), root, snap.State, packet.ID, e.AgentID); err != nil || !complete {
		t.Fatalf("neighbor polluted exact completion: %t %v", complete, err)
	}
	// Exercise the original read-only receipt on the complete Runtime snapshot;
	// no responsibilities are deleted or hidden to obtain this result.
	raw, err = json.Marshal(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	receipt, err := service.RepairResultReceipt(e.AgentID, packet.ID)
	if err != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("neighbor polluted original receipt: %+v %v", receipt, err)
	}
	// Removing this assignment's actual delivery cannot borrow its neighbor.
	current := executions[e.AssignmentID].(map[string]any)
	current["delivery_ref"] = ""
	current["delivery_sha256"] = ""
	if complete, err := repair.RepairResultWorkCompleteWithContext(context.Background(), root, snap.State, packet.ID, e.AgentID); err != nil || complete {
		t.Fatalf("neighbor supplied missing original candidate: %t %v", complete, err)
	}
}

func TestS9HandoffLaterIndexFailureDoesNotEraseAcceptedPlan(t *testing.T) {
	root, _, plan, e, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	entities := mapFieldCLI(snap.State, "entities")
	// Keep the already accepted Plan and damage only a later Result index.
	for _, row := range objectSliceAny(entities["work_packets"]) {
		if row["template_id"] == "s9-repair-result-v1" {
			row["binding"] = "unreadable later Result binding"
		}
	}
	projected := s9HandoffForExecution(context.Background(), root, snap, e)
	if !projected.PlanAccepted || projected.ReadError == nil || !strings.Contains(projected.summary(root), "PlanReport receipt is accepted") || projected.Plan.id != plan.ID {
		t.Fatalf("later index erased original Plan: %+v", projected)
	}
}

func TestS9HandoffFinalReceiptSurvivesMissingPlanIndex(t *testing.T) {
	root, _, _, packet, _, _ := s9BeforeIntegrationAck(t, true)
	service, err := submission.NewContext(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	snap := repairDispatchSnapshot(t, root)
	entities := mapFieldCLI(snap.State, "entities")
	var retained []any
	for _, raw := range entities["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] != "s9-plan-report-v1" {
			retained = append(retained, row)
		}
	}
	entities["work_packets"] = retained
	before := repairDispatchRuntimeBytes(t, root)
	summary, err := nativeS9MainReturnSummary(context.Background(), root, snap, nativeReturnSubject{kind: "s9-repair-result-v1", id: packet.ID}, service)
	if err != nil || !strings.Contains(summary, "main_result_accepted") || !strings.Contains(summary, "checkpoint ACK/cleanup") {
		t.Fatalf("missing Plan index erased independent Final: %v %s", err, summary)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

// Use the public Hook command: Main's ordinary return path starts in dispatch,
// before the direct questionnaire helper, after a background actor's Stop.
func TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts(t *testing.T) {
	for _, scenario := range []string{"no candidate", "partial candidate", "accepted candidate", "accepted Final"} {
		t.Run(scenario, func(t *testing.T) {
			var root string
			switch scenario {
			case "no candidate":
				root, _, _, _, _ = s9HandoffMissingCandidate(t)
			case "accepted Final":
				root, _, _, _, _, _ = s9BeforeIntegrationAck(t, true)
			default:
				actual, worker, plan, input, answer := nativeS9WorkerFixture(t)
				root = actual
				if scenario == "partial candidate" {
					answer.UnitResults[0].Status = "fail"
					path := nativeS9TestResultPath(t, root, plan)
					if err := persistRepairLeadQuestionnaire(filepath.Join(worker, path), answer); err != nil {
						t.Fatal(err)
					}
				}
				input.Event = "SubagentStop"
				var diagnostic bytes.Buffer
				if _, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); code != 0 {
					t.Fatal(diagnostic.String())
				}
			}
			before := repairDispatchRuntimeBytes(t, root)
			body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "internal/api/one.go")}})
			var out, diagnostic bytes.Buffer
			if code := Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
				t.Fatalf("ordinary Main Hook: %d %s %s", code, &out, &diagnostic)
			}
			text := out.String()
			switch scenario {
			case "no candidate":
				for _, want := range []string{"Candidate reception is not established", "PlanReport receipt is accepted", "Consumer diagnostic"} {
					if !strings.Contains(text, want) {
						t.Errorf("missing %q: %s", want, text)
					}
				}
			case "partial candidate":
				if !strings.Contains(text, "unfinished") || strings.Contains(text, "Main next runs runtime workspace integrate") {
					t.Fatalf("partial background return became integrate duty: %s", text)
				}
			case "accepted candidate":
				// The original ChangeImpact is prepared as soon as Main takes
				// over the candidate; no mandatory workspace integrate action
				// precedes its reception.
				if !strings.Contains(text, "change-impact.json") || strings.Contains(text, "Main next runs runtime workspace integrate") {
					t.Fatalf("accepted background return lacks actual Main handoff: %s", text)
				}
			case "accepted Final":
				if !strings.Contains(text, "checkpoint ACK/cleanup") {
					t.Fatalf("Final lost original close-out: %s", text)
				}
			}
			// The empty original ChangeImpact may be received along this Pre
			// event (L4-workflow-convergence.md:110), so the Runtime changes by
			// exactly that one original Impact reception. Every other already
			// received S9 fact must stay put.
			repairDispatchAssertOnlyImpactReception(t, root, before, scenario != "no candidate")
		})
	}
}

func TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection(t *testing.T) {
	root, _, e, _, _, ref := s9BeforeIntegrationAck(t, true)
	snap := repairDispatchSnapshot(t, root)
	original, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil {
		t.Fatal(err)
	}
	// The original Final already consumed its source proof. A missing old Plan
	// is now a later Main-material issue, not permission to re-merge or recheck.
	planPath := stringValue(repairPointerCLI(snap.State)["plan_ref"])
	if err := os.Remove(filepath.Join(root, planPath)); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	_ = runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID}, &out, &diagnostic)
	checkpoint, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
	if err != nil || !found {
		t.Fatalf("old Plan blocked already accepted Final cleanup: %+v %v %s", checkpoint, err, &diagnostic)
	}
	// The missing old Plan is a later Main-material issue and must not hold
	// the cleanup of an already accepted Final. The author's private answer
	// material may still sit in the worktree, and a retained material keeps
	// the cleanup pending without revoking the acceptance or the
	// acknowledgement (L4-workflow-convergence.md:302).
	if checkpoint.State != integration.StateComplete && checkpoint.State != integration.StateCleanupPending {
		t.Fatalf("old Plan blocked already accepted Final cleanup: %+v %v %s", checkpoint, err, &diagnostic)
	}
	if checkpoint.State == integration.StateCleanupPending {
		reason := checkpoint.FailureReason + " " + strings.Join(checkpoint.Blockers, " ")
		if strings.Contains(reason, planPath) {
			t.Fatalf("cleanup was held by the missing old Plan: %+v", checkpoint)
		}
		if checkpoint.WorktreePath == "" {
			t.Fatalf("cleanup_pending did not report the retained worktree: %+v", checkpoint)
		}
		if _, statErr := os.Stat(checkpoint.WorktreePath); statErr != nil {
			t.Fatalf("cleanup_pending did not retain the author's worktree: %v", statErr)
		}
	}
	current, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatalf("cleanup changed accepted Final: %v", err)
	}
}

func TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof(t *testing.T) {
	root, _, e, packet, checkpoint, ref := s9BeforeIntegrationAck(t, true)
	snap := repairDispatchSnapshot(t, root)
	original, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := runtime.VerifiedRepairResultReceiptContext(ctx, root, snap.State, packet.ID, e.AgentID); err != context.Canceled {
		t.Fatalf("Final receipt lost parent context: %v", err)
	}
	if _, err := acknowledgeAcceptedRepairResult(ctx, root, snap, packet, e, checkpoint); err != context.Canceled {
		t.Fatalf("ACK lost parent context: %v", err)
	}
	current, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatalf("canceled ACK changed accepted Final: %v", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
