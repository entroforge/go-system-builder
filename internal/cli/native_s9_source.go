package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type nativeS9StopActorKey struct{}

func nativeS9RetainedSource(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source string) (*repair.NativeS9Source, error) {
	registry, err := workspace.Decode(state)
	if err != nil || registry == nil {
		return nil, err
	}
	e, ok := registry.Executions[p.Binding.ManifestAssignmentID]
	if !ok {
		return nil, nil
	}
	if e.Path != source || e.RuntimeID != p.Binding.RuntimeID || e.BaselineGeneration != p.Binding.BaselineGeneration || e.AgentID != p.Binding.OwnerAgentID {
		return nil, fmt.Errorf("native S9 source differs from current execution")
	}
	retained, err := repair.RetainedNativeS9Source(ctx, root, e, p.Binding)
	if err != nil || retained == nil {
		return retained, err
	}
	sha, err := workpacket.RepairPlanReportPacketDigest(p)
	if err != nil {
		return nil, err
	}
	if retained.PacketID != p.ID || retained.PacketSHA != sha {
		return nil, fmt.Errorf("retained native source differs from original issued Plan")
	}
	return retained, nil
}

// The pre-Plan boundary is limited to new native nochange sources. Legacy
// and changed deliveries retain their original Plan -> source failure order.
func nativeS9PrePlanRetentionEligible(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source, verdict string) (bool, error) {
	actor, _ := ctx.Value(nativeS9StopActorKey{}).(string)
	if actor == "" || actor == "Main" || verdict != "completed" {
		return false, nil
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return false, err
	}
	if registry != nil {
		if _, exists := registry.Executions[p.Binding.ManifestAssignmentID]; exists {
			return false, nil
		}
	}
	_, pin, _ := nativeS9Paths(p)
	body, err := workpacket.ReadFile(root, pin, 4096)
	if err != nil {
		return false, nil
	} // Preserve the original post-Plan source error.
	var saved struct {
		Version int `json:"source_retention_version"`
	}
	if json.Unmarshal(body, &saved) != nil || saved.Version != 1 {
		return false, nil
	}
	head, err := workspace.Git(ctx, source, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	mainHead, err := workspace.Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	_, err = workspace.Git(ctx, root, "merge-base", "--is-ancestor", head, mainHead)
	if canceled := ctx.Err(); canceled != nil {
		return false, canceled
	}
	return err == nil, nil
}

func nativeS9CaptureSource(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source, base, actor, preparedBase, result string, professional []byte) (repair.ArtifactRef, error) {
	head, err := workspace.Git(ctx, source, "rev-parse", "HEAD")
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	if head != base {
		return repair.ArtifactRef{}, nil
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	if registry != nil {
		if old, ok := registry.Executions[p.Binding.ManifestAssignmentID]; ok {
			// An execution already registered under an older contract is frozen.
			// It must never be upgraded by a later Stop.
			if repair.NativeS9SourceReference(old).Path == "" {
				return repair.ArtifactRef{}, nil
			}
			if _, err := repair.RetainedNativeS9Source(ctx, root, old, p.Binding); err != nil {
				return repair.ArtifactRef{}, err
			}
			return repair.ArtifactRef{Path: repair.NativeS9SourceReference(old).Path, SHA256: repair.NativeS9SourceReference(old).SHA256}, nil
		}
	}
	branch, err := workspace.Git(ctx, source, "branch", "--show-current")
	if err != nil || branch == "" {
		return repair.ArtifactRef{}, fmt.Errorf("native source needs actual branch")
	}
	var assignment repair.RepairAssignment
	if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
		return repair.ArtifactRef{}, err
	}
	b := p.Binding
	targetBranch, err := workspace.Git(ctx, root, "branch", "--show-current")
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	e := workspace.Execution{Purpose: workspace.NativeDeliveryPurpose, AssignmentID: b.ManifestAssignmentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, Generation: 1, AgentID: b.OwnerAgentID, Path: source, Branch: branch, TargetBranch: targetBranch, BaseCommit: base, Status: "ready", WritePaths: assignment.Scope, Checks: []string{}, Inputs: map[string]string{}, CheckLocation: "main"}
	return repair.CaptureNativeS9Source(ctx, root, p, e, actor, preparedBase, result, professional)
}
