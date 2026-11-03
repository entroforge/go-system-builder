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
			// The original Result is already accepted. The accepted Final and
			// its original ACK/cleanup are preserved; this path neither
			// receives the work again nor reopens the removed live input.
			fmt.Fprintln(diagnostic, "Original S9 Result is already received; Main preserves the accepted Final and resumes the original checkpoint ACK/cleanup independently of the current repair responsibility; no live source is reopened.")
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
	if os.IsNotExist(resultErr) {
		return pendingResult()
	}
	if resultErr != nil {
		return resultErr
	}
	if !json.Valid(raw) {
		// The original file exists but its bytes are not a valid original
		// document. Report the original validator's own parse problem against
		// the actual file instead of treating it as an unfinished submission.
		if err := workpacket.ValidateUniqueJSON(raw); err != nil {
			return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
		}
	}
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
	}
	if strings.TrimSpace(verdict.Verdict) == "" {
		return pendingResult()
	}
	if capture != nil {
		capture.ObserveInput(filepath.Join(source, result), raw)
		capture.SetInput(filepath.Join(source, result))
		capture.EnterConsumer("original_result_consumer")
	}
	// Only author-owned professional fields belong in this private file. The
	// canonical consumer stays lenient about unrelated extensions, so this
	// entry point keeps the strictness the original flat entry owned.
	var resultAnswer workpacket.RepairResultProfessionalAnswer
	if err := nativeS9FlatUnknownAuthorField(raw, &resultAnswer); err != nil {
		return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
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
			return nativeS9FlatArtifactError("Result", filepath.Join(source, result), err)
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
	recorded, exists := registry.Executions[b.ManifestAssignmentID]
	if exists {
		if recorded.AgentID != b.OwnerAgentID || !samePath(recorded.Path, source) {
			return fmt.Errorf("S9 original responsibility has another recorded authoring location")
		}
		// The recorded responsibility coordinate is authoritative once it exists.
		// Only a missing branch is completed; a recorded branch is never
		// recomputed or overwritten.
		if recorded.Branch != "" {
			return nil
		}
	}
	// The native delivery carries the branch the author actually checked out.
	// It is a verifiable coordinate, not a content proof; an unresolvable
	// branch is reported instead of being recorded as an empty value.
	branch, err := workspace.Git(ctx, source, "branch", "--show-current")
	if err != nil || branch == "" {
		return fmt.Errorf("native source needs its actual Git branch")
	}
	eventID := "native-s9-location-" + p.ID
	if exists {
		recorded.Branch = branch
		registry.Executions[b.ManifestAssignmentID] = recorded
		eventID = "native-s9-location-branch-" + p.ID
	} else {
		var assignment repair.RepairAssignment
		if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
			return err
		}
		e := workspace.Execution{Purpose: workspace.NativeDeliveryPurpose, AssignmentID: b.ManifestAssignmentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, Generation: 1, AgentID: b.OwnerAgentID, Path: source, Branch: branch, TargetBranch: registry.Branch, Status: "ready", WritePaths: append([]string{}, assignment.Scope...), Checks: []string{}, Inputs: map[string]string{}, CheckLocation: "main"}
		registry.Executions[e.AssignmentID] = e
	}
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	_, err = store.Update(snapshot.Revision, runtime.Mutation{EventID: eventID, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "Main", RuntimeID: b.RuntimeID, BaselineGeneration: integerValue(baseline["generation"]), IdempotencyKey: eventID, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		pointer := repairPointerCLI(state)
		if state["runtime_id"] != b.RuntimeID || stringValue(pointer["session_id"]) != b.SessionID || stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
			return fmt.Errorf("S9 original responsibility changed before reception")
		}
		state["workspace"] = workspace.Encode(registry)
		return nil
	}})
	return err
}
