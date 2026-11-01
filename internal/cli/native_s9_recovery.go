package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func nativeS9Capture(captures []*nativeS9AttemptCapture) *nativeS9AttemptCapture {
	if len(captures) == 0 {
		return nil
	}
	return captures[0]
}

// Only the exact private native Result coordinate or original Targeted request
// enters this adapter. Typed paths retain their existing submission selector.
func consumeNativeS9Original(ctx context.Context, service *submission.Service, path string) (any, bool, error) {
	flat := (strings.Contains(filepath.ToSlash(path), "/.claude/submissions/native-s9/") || strings.Contains(filepath.ToSlash(path), "/.loop-submissions/native-s9/")) && filepath.Base(path) == "result.json"
	targeted := (strings.Contains(filepath.ToSlash(path), "/.claude/submissions/s9/") && strings.HasPrefix(filepath.Base(path), "targeted-reverification-") || strings.Contains(filepath.ToSlash(path), "/.loop-submissions/s9/") && strings.HasPrefix(filepath.Base(path), "targeted-")) && strings.HasSuffix(path, ".json")
	if !flat && !targeted {
		return nil, false, nil
	}
	snap, err := runtime.NewWriter(service.StatePath, service.JournalPath, service.Root, service.Validator).WithContext(ctx).Snapshot()
	if err != nil {
		return nil, true, err
	}
	if !runtime.PacketEnabled(snap.State) {
		return nil, true, fmt.Errorf("original S9 input has no current packet-enabled responsibility")
	}
	var feedback bytes.Buffer
	if targeted {
		selections, err := repairTargetedSelections(service.Root, snap.State)
		if err != nil {
			return nil, true, err
		}
		for _, selection := range selections {
			ref, err := repairTargetedQuestionnairePath(service.Root, snap.State, selection.performingAssignmentID)
			if err != nil {
				return nil, true, err
			}
			if filepath.Clean(ref) != filepath.Clean(path) {
				continue
			}
			receipt := lookupTargetedForSelection(service.Root, repairPointerCLI(snap.State), selection, "pass")
			if receipt.found {
				return map[string]any{"input_ref": path, "accepted_ref": receipt.ref.Path, "next_action": "Targeted is already received. " + repairHandoffProjectionContext(service.Root, snap.State)}, true, nil
			}
			if receipt.unknown || receipt.previous.Path != "" {
				if available, _ := s9OriginalInputAvailable(ctx, ref); !available {
					return map[string]any{"input_ref": path, "next_action": targetedHistoricalUnknownMessage(receipt)}, true, nil
				}
			}
			status := stringValue(repairPointerCLI(snap.State)["status"])
			if status != "targeted_reverification" && status != "blocked" {
				return nil, true, fmt.Errorf("Targeted answer has no current unreceived independent responsibility or accepted receipt")
			}
			capture := newNativeS9AttemptCapture(ctx, service.Root, snap, ref, selection.verifierOwner, "Targeted")
			capture.SetResponsibilityRef(ref)
			err = receiveNativeS9Targeted(ctx, service.Root, snap, ref, &feedback, capture)
			if publishErr := capture.Publish(ctx, err); publishErr != nil {
				fmt.Fprintln(&feedback, "S9 reception diagnostic could not be delivered:", publishErr)
			}
			return map[string]any{"input_ref": path, "next_action": strings.TrimSpace(feedback.String())}, true, err
		}
		return nil, true, fmt.Errorf("Targeted answer path has no exact current independent responsibility")
	}
	life := mapFieldCLI(snap.State, "lifecycle")
	if life["state"] != "bug_resolution" || !s9ProfessionalPhase(stringValue(life["phase"])) {
		return nil, true, fmt.Errorf("native Result has no current professional S9 responsibility")
	}
	var selected *workpacket.RepairPlanReportPacket
	var source string
	routes, err := nativeS9Routes(ctx, snap.State)
	if err != nil {
		return nil, true, err
	}
	for _, route := range routes {
		if filepath.Base(filepath.Dir(path)) != route.ID {
			continue
		}
		packet, err := nativeS9RoutePacket(ctx, service.Root, snap.State, route)
		if err != nil {
			return nil, true, err
		}
		_, result, err := nativeS9AuthorPaths(service.Root, packet)
		if err != nil {
			return nil, true, fmt.Errorf("read native author paths: %w", err)
		}
		suffix := string(filepath.Separator) + filepath.FromSlash(result)
		if !strings.HasSuffix(path, suffix) {
			continue
		}
		prepared, err := nativeS9RecoveryPrepared(ctx, service.Root, snap.State, packet)
		if err != nil {
			return nil, true, err
		}
		if !prepared {
			return nil, true, fmt.Errorf("original Plan responsibility is unavailable; Main continues its preparation")
		}
		if selected != nil {
			return nil, true, fmt.Errorf("native Result path selects multiple current responsibilities")
		}
		selected, source = &packet, strings.TrimSuffix(path, suffix)
	}
	if selected == nil {
		return nil, true, fmt.Errorf("native Result path has no exact current original Plan packet")
	}
	// CanonicalProspective permits a cleaned accepted source; the recorded
	// execution path is still checked by the shared receiver before projection.
	source, err = workspace.CanonicalProspective(source)
	if err != nil {
		return nil, true, err
	}
	capture := newNativeS9AttemptCapture(ctx, service.Root, snap, selected.ID, selected.Binding.OwnerAgentID, "Builder")
	brief := nativeS9BriefPath(*selected)
	capture.SetResponsibilityRef(filepath.Join(service.Root, brief))
	capture.SetInput(path)
	err = receiveNativeS9BuilderFrom(ctx, service.Root, snap.State, *selected, source, true, &feedback, capture)
	if publishErr := capture.Publish(ctx, err); publishErr != nil {
		fmt.Fprintln(&feedback, "S9 reception diagnostic could not be delivered:", publishErr)
	}
	return map[string]any{"input_ref": path, "next_action": strings.TrimSpace(feedback.String())}, true, err
}

// A display brief may disappear after the Plan has been received. Select from
// original packet coordinates and native preparation evidence; an existing
// only the actual current responsibility is selected.
func nativeS9RecoveryPrepared(ctx context.Context, root string, state map[string]any, plan workpacket.RepairPlanReportPacket) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return false, err
	}
	if registry != nil {
		if execution, ok := registry.Executions[plan.Binding.ManifestAssignmentID]; ok {
			if execution.Purpose != workspace.NativeDeliveryPurpose || execution.PlatformSessionID != "" {
				return false, nil
			}
			b := plan.Binding
			if execution.AgentID != b.OwnerAgentID || execution.RuntimeID != b.RuntimeID || execution.BaselineGeneration != b.BaselineGeneration {
				return false, fmt.Errorf("original native preparation differs from the current Plan responsibility")
			}
			return true, nil
		}
	}
	return true, nil
}

// Continue a real completed return from the currently issued original native
// responsibility. The actual platform worktree or an existing execution
// supplies the source; platform actor strings and ToolUseID are not author
// fields or additional reception conditions.
func continueNativeOriginalS9MainReturn(ctx context.Context, root string, in policy.Input, snap runtime.Snapshot, subject nativeReturnSubject) (runtime.Snapshot, bool, error) {
	if ctx.Err() != nil || subject.kind != "s9-plan-report-v1" || in.Error != "" || in.ToolResponse["is_error"] == true || nativeExecutionFromResponse(in.ToolResponse) != nativeExecutionCompleted {
		return snap, false, nil
	}
	prompt, _ := in.ToolInput["prompt"].(string)
	if !nativeS9ResponsibilityMatch(prompt, root, nativeS9RegisteredResponsibilityRelative(root, subject.ref)) {
		return snap, false, nil
	}
	routes, err := nativeS9Routes(ctx, snap.State)
	if err != nil {
		return snap, true, err
	}
	var packet *workpacket.RepairPlanReportPacket
	for _, route := range routes {
		if route.ID != subject.id || (subject.ref != filepath.Join(root, nativeS9RouteBrief(route)) && subject.ref != filepath.Join(root, route.ViewPath) && subject.ref != filepath.Join(root, route.AnswerPath)) {
			continue
		}
		if packet != nil {
			return snap, true, fmt.Errorf("multiple issued S9 routes for the original responsibility")
		}
		p, err := nativeS9RoutePacket(ctx, root, snap.State, route)
		if err != nil {
			return snap, true, err
		}
		packet = &p
	}
	if packet == nil {
		return snap, false, nil
	}
	projection := s9HandoffForBinding(ctx, root, snap, packet.Binding, "")
	if projection.Result != nil {
		service, err := submission.NewContext(ctx, root, true)
		if err != nil {
			return snap, true, err
		}
		receipt, err := service.RepairResultReceipt(packet.Binding.OwnerAgentID, projection.Result.id)
		if err != nil {
			return snap, true, err
		}
		if receipt.Accepted && receipt.WorkComplete {
			return snap, true, nil
		}
	}
	path, _ := in.ToolResponse["worktreePath"].(string)
	registry, err := workspace.Decode(snap.State)
	if err != nil {
		return snap, true, err
	}
	if registry != nil {
		if execution, ok := registry.Executions[packet.Binding.ManifestAssignmentID]; ok {
			if execution.Purpose != workspace.NativeDeliveryPurpose || execution.PlatformSessionID != "" {
				return snap, false, nil
			}
			if path != "" && !samePath(path, execution.Path) {
				return snap, true, fmt.Errorf("completed S9 return worktree differs from the original recorded authoring location; Main must resolve the actual source")
			}
			path = execution.Path
		}
	}
	if path == "" || !filepath.IsAbs(path) {
		return snap, true, fmt.Errorf("completed S9 return has no actual authoring location; Main must locate the original result without requiring another author save")
	}
	var diagnostic bytes.Buffer
	capture := newNativeS9AttemptCapture(ctx, root, snap, packet.ID, packet.Binding.OwnerAgentID, "Builder")
	capture.SetResponsibilityRef(subject.ref)
	consumeCtx := context.WithValue(ctx, nativeS9PlanStopKey{}, true)
	err = receiveNativeS9OriginalBuilder(consumeCtx, root, snap.State, *packet, path, &diagnostic, capture)
	if publishErr := capture.Publish(ctx, err); publishErr != nil && err == nil {
		err = publishErr
	}
	if cached, ok := ctx.Value(nativeRoutingKey{}).(*nativeRoutingRead); ok {
		*cached = nativeRoutingRead{root: root}
	}
	current, readErr := nativeRoutingSnapshot(root, in)
	if readErr != nil {
		return snap, true, readErr
	}
	return current, true, err
}
