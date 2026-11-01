package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func receiveNativeS9OriginalBuilder(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source string, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := workspace.CanonicalProspective(source)
	if err != nil {
		return err
	}
	if source == "" || samePath(source, root) {
		return fmt.Errorf("S9 actual Worker authoring location is missing; Main must locate this original responsibility")
	}
	service := (&submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}).WithContext(ctx)
	registry, err := workspace.Decode(state)
	if err != nil {
		return err
	}
	if registry != nil {
		if e, ok := registry.Executions[p.Binding.ManifestAssignmentID]; ok && e.DeliveryRef != "" {
			fmt.Fprintln(diagnostic, "Original S9 Result is already received; Main continues actual integration and checks.")
			return nil
		}
	}
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	stop, _ := ctx.Value(nativeS9PlanStopKey{}).(bool)
	// A draft remains quiet during ordinary work. A real return must preserve
	// the selected responsibility and say who continues its unfinished work.
	pendingResult := func() error {
		if !stop {
			return nativeS9ProgressPending
		}
		return &nativeS9PlanAcceptedIncomplete{
			error: nativeS9ProgressPending,
			plan:  filepath.Join(source, plan), result: filepath.Join(source, result),
		}
	}
	// A genuine blocked result can be delivered even if the independent Plan
	// has not been written. Read this original once and reuse it for reception.
	raw, resultErr := workpacket.ReadFile(source, result, 20<<20)
	var verdict struct {
		Verdict string `json:"verdict"`
	}
	if resultErr == nil && json.Valid(raw) {
		_ = json.Unmarshal(raw, &verdict)
	}
	blocked := verdict.Verdict == "blocked" || verdict.Verdict == "contract_conflict"
	// Plan is its own contribution. Receiving it creates neither a Result nor
	// a product execution, and the Worker continues the original authorization.
	if err := receiveNativeS9ReadyPlan(ctx, root, source, p, service, diagnostic, capture); err != nil && !blocked {
		if stop && err == nativeS9ProgressPending {
			return fmt.Errorf("original PlanReport is missing or unfinished at %s for %s; Result is not received at %s. Main owns continuation of this original responsibility; no repair completion is established", filepath.Join(source, plan), p.Binding.OwnerAgentID, filepath.Join(source, result))
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.IsNotExist(resultErr) || resultErr == nil && !json.Valid(raw) {
		return pendingResult()
	}
	if resultErr != nil {
		return resultErr
	}
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return err
	}
	if strings.TrimSpace(verdict.Verdict) == "" {
		return pendingResult()
	}
	if capture != nil {
		capture.ObserveInput(filepath.Join(source, result), raw)
		capture.SetInput(filepath.Join(source, result))
		capture.EnterConsumer("original_result_consumer")
	}
	if err := recordNativeS9OriginalLocation(ctx, root, p, source); err != nil {
		return err
	}
	packets, err := repair.IssueRepairResultPackets(ctx, root, service.StatePath, service.JournalPath, -1, p.Binding.OwnerAgentID)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		if packet.Binding.Sources.RepairAssignmentID != p.Binding.RepairAssignmentID {
			continue
		}
		receipt, err := service.SaveRepairResultBytes(ctx, p.Binding.OwnerAgentID, packet.ID, source, raw)
		if err != nil {
			return err
		}
		if !receipt.Accepted {
			return fmt.Errorf("original S9 Result remains unreceived: %s", receipt.Error)
		}
		if capture != nil {
			capture.ConsumerSucceeded()
			capture.SetStage("result_received")
		}
		if blocked {
			fmt.Fprintln(diagnostic, "Original S9 blocked Result received. "+receipt.NextAction)
			nativeS9WorkerMessage(ctx, "Your original S9 blocked Result is received. Main will handle the reported blocker; this does not claim repair or integration completion.")
			return nil
		}
		// Preparing Main's original question does not record Main completion.
		if current, snapshotErr := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot(); snapshotErr != nil {
			fmt.Fprintln(diagnostic, "Original Result remains received; Main Impact preparation: "+snapshotErr.Error())
		} else if path, prepareErr := prepareRepairImpactQuestionnaire(root, current.State); prepareErr != nil {
			fmt.Fprintln(diagnostic, "Original Result remains received; Main Impact preparation: "+prepareErr.Error())
		} else {
			fmt.Fprintln(diagnostic, "Main original ChangeImpact answer: "+path+". Main performs actual integration/checks and saves its original impact judgment; reception does not certify that work.")
		}
		fmt.Fprintln(diagnostic, "Original S9 Result received. "+receipt.NextAction)
		nativeS9WorkerMessage(ctx, "Your original S9 Result is received. Main owns the actual integration, necessary checks, impact judgment and independent verification.")
		return nil
	}
	return fmt.Errorf("original S9 Result responsibility was not prepared for %s", p.Binding.RepairAssignmentID)
}

func recordNativeS9OriginalLocation(ctx context.Context, root string, p workpacket.RepairPlanReportPacket, source string) error {
	store := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snapshot, err := store.Snapshot()
	if err != nil {
		return err
	}
	registry, err := workspace.Decode(snapshot.State)
	if err != nil {
		return err
	}
	if registry == nil {
		registry, err = workspace.NewNativeReceipt(snapshot.State, root)
		if err != nil {
			return fmt.Errorf("Main authority for this original responsibility is unavailable: %w", err)
		}
	}
	if registry.Executions == nil {
		registry.Executions = map[string]workspace.Execution{}
	}
	b := p.Binding
	if e, ok := registry.Executions[b.ManifestAssignmentID]; ok {
		if e.AgentID != b.OwnerAgentID || !samePath(e.Path, source) {
			return fmt.Errorf("S9 original responsibility has another recorded authoring location")
		}
		return nil
	}
	var assignment repair.RepairAssignment
	if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
		return err
	}
	e := workspace.Execution{Purpose: workspace.NativeDeliveryPurpose, AssignmentID: b.ManifestAssignmentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, Generation: 1, AgentID: b.OwnerAgentID, Path: source, Branch: "", TargetBranch: registry.Branch, Status: "ready", WritePaths: append([]string{}, assignment.Scope...), Checks: []string{}, Inputs: map[string]string{}, CheckLocation: "main"}
	registry.Executions[e.AssignmentID] = e
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	_, err = store.Update(snapshot.Revision, runtime.Mutation{EventID: "native-s9-location-" + p.ID, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "Main", RuntimeID: b.RuntimeID, BaselineGeneration: integerValue(baseline["generation"]), IdempotencyKey: "native-s9-location-" + p.ID, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		pointer := repairPointerCLI(state)
		if state["runtime_id"] != b.RuntimeID || stringValue(pointer["session_id"]) != b.SessionID || stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
			return fmt.Errorf("S9 original responsibility changed before reception")
		}
		state["workspace"] = workspace.Encode(registry)
		return nil
	}})
	return err
}
