package repair

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/integration"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type WorkspaceCandidate struct {
	Version             int    `json:"version"`
	RecordType          string `json:"record_type"`
	RuntimeID           string `json:"runtime_id"`
	BaselineGeneration  int    `json:"baseline_generation"`
	ExecutionGeneration int    `json:"execution_generation"`
	AssignmentID        string `json:"assignment_id"`

	SessionID string `json:"session_id"`
	PlanID    string `json:"plan_id"`

	Proposal   RepairResultRequest                         `json:"proposal"`
	Provenance *runtimepkg.RepairResultCandidateProvenance `json:"provenance,omitempty"`
}

// createReceivedRepairCandidate saves the professional contribution. Source
// inspection, changes and necessary checks belong to Main's actual integration.
func createReceivedRepairCandidate(root string, packet workpacket.RepairResultPacket, e workspace.Execution, proposal RepairResultRequest, provenance runtimepkg.RepairResultCandidateProvenance) (WorkspaceCandidate, ArtifactRef, error) {
	candidate := WorkspaceCandidate{Version: 1, RecordType: "repair_workspace_candidate", RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration,
		ExecutionGeneration: e.Generation, AssignmentID: e.AssignmentID,
		SessionID: packet.Binding.Sources.SessionID, PlanID: packet.Binding.Sources.PlanID, Proposal: proposal, Provenance: &provenance}
	ref, err := writeResponsibilityCandidate(root, packet.ID, candidate)
	return candidate, ref, err
}

// IntegratedRepairChanges uses the Main integration's actual changed-path list
// and observes only those paths in the current checkout. It does not claim to
// reconstruct a historical tree, scan the product or authenticate author data.
func IntegratedRepairChanges(ctx context.Context, root string, checkpoint integration.Checkpoint) ([]ChangedArtifact, error) {
	out := make([]ChangedArtifact, 0, len(checkpoint.ChangedPaths))
	for _, path := range checkpoint.ChangedPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filepath.IsLocal(filepath.FromSlash(path)) || filepath.ToSlash(filepath.Clean(path)) != path {
			return nil, fmt.Errorf("integrated change path is not local: %s", path)
		}
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		status := "modified"
		if os.IsNotExist(err) {
			status = "deleted"
		} else if err != nil {
			return nil, fmt.Errorf("observe current integrated path %s: %w", path, err)
		}
		out = append(out, ChangedArtifact{Path: path, Status: status})
	}
	return out, nil
}

// writeResponsibilityCandidate chooses an internal coordinate from the
// original responsibility, never from its contents. Interrupted attempts
// preserve their own candidate; an unrelated existing file is not replaced.
func writeResponsibilityCandidate(root, identity string, candidate WorkspaceCandidate) (ArtifactRef, error) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(identity))
	return writeUniqueArtifact(root, responsibilityArtifactPath("candidates", "responsibility-"+encoded), "repair-workspace-candidate.schema.json", candidate)
}

func ValidateWorkspaceCandidate(root string, state map[string]any, e workspace.Execution, ref ArtifactRef) (WorkspaceCandidate, error) {
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return WorkspaceCandidate{}, err
	}
	registry, err := workspace.DecodeRegistry(raw)
	if err != nil {
		return WorkspaceCandidate{}, err
	}
	current, ok := registry.Execution(e.AssignmentID, workspace.RuntimeID(state), workspace.Generation(state))
	if !ok || current.Generation != e.Generation || current.AgentID != e.AgentID ||
		current.ReworkRef != e.ReworkRef || !sameAbsolutePath(current.Path, e.Path) {
		return WorkspaceCandidate{}, fmt.Errorf("candidate receiver is not the current S9 execution attempt")
	}
	c, err := ReadWorkspaceCandidate(root, ref)
	if err != nil {
		return c, err
	}
	if c.RuntimeID != e.RuntimeID || c.BaselineGeneration != e.BaselineGeneration ||
		c.ExecutionGeneration != e.Generation || c.AssignmentID != e.AssignmentID ||
		c.Proposal.ProducerAgentID != e.AgentID ||
		(current.DeliveryRef != "" && current.DeliveryRef != ref.Path) {
		return c, fmt.Errorf("candidate belongs to a different current S9 execution attempt")
	}
	if p := c.Provenance; p != nil && (p.ExecutionAssignmentID != e.AssignmentID ||
		p.ExecutionGeneration != e.Generation || p.RuntimeID != e.RuntimeID ||
		p.BaselineGeneration != e.BaselineGeneration || p.OwnerAgentID != e.AgentID ||
		p.ReworkRef != e.ReworkRef || !sameAbsolutePath(p.ExecutionRoot, e.Path)) {
		return c, fmt.Errorf("candidate belongs to a different Worker attempt or checkout")
	}
	return c, nil
}

func ReadWorkspaceCandidate(root string, ref ArtifactRef) (WorkspaceCandidate, error) {
	data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
	if err != nil {
		return WorkspaceCandidate{}, err
	}
	return DecodeWorkspaceCandidate(data)
}

func RegisteredWorkspaceResult(root string, state map[string]any, c WorkspaceCandidate) (ArtifactRef, bool, error) {
	return RegisteredWorkspaceResultContext(context.Background(), root, state, c)
}
func RegisteredWorkspaceResultContext(ctx context.Context, root string, state map[string]any, c WorkspaceCandidate) (ArtifactRef, bool, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, false, err
	}
	for _, ref := range existingArtifactRefs(repairPointer(state)["result_refs"]) {
		if c.Provenance != nil && ref.Path != artifactRoot+"/results/"+c.Proposal.ResultID+".json" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return ArtifactRef{}, false, err
		}
		data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
		var result RepairResult
		if err == nil {
			err = json.Unmarshal(data, &result)
		}
		if err != nil {
			return ArtifactRef{}, false, err
		}
		if result.ResultID == c.Proposal.ResultID {
			if result.Result != "pass" {
				return ArtifactRef{}, false, fmt.Errorf("recorded Result is %s; actual repair/check work remains pending", result.Result)
			}
			if result.AssignmentID != c.Proposal.AssignmentID || result.SessionID != c.SessionID || result.ProducerAgentID != c.Proposal.ProducerAgentID {
				return ArtifactRef{}, false, fmt.Errorf("candidate result identity collision")
			}
			return ref, true, nil
		}
	}
	return ArtifactRef{}, false, nil
}
