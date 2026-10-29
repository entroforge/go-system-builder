package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"regexp"

	"github.com/entroforge/go-system-builder/internal/integration"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// NativeS9PrivatePaths identifies the two professional originals of one issued
// Plan responsibility. It never changes the packet's machine input coordinates.
func NativeS9PrivatePaths(planPacketID string) (plan, result string, err error) {
	if !regexp.MustCompile(`^packet-[a-f0-9]{64}$`).MatchString(planPacketID) {
		return "", "", fmt.Errorf("invalid original native Plan packet identity")
	}
	dir := ".loop-submissions/native-s9/" + planPacketID + "/"
	return dir + "plan.json", dir + "result.json", nil
}

// NativeS9PrivateArea identifies the original D responsibility's private
// runtime area. It is a derived coordinate, not a file inventory or permission
// to accept an answer or remove a worktree. Frozen C and non-native executions
// retain their original paths and return no area.
func NativeS9PrivateArea(ctx context.Context, root string, packet workpacket.RepairResultPacket, e workspace.Execution) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	report, enabled, err := nativeS9PrivateContract(ctx, root, packet, e)
	if err != nil || !enabled {
		return "", err
	}
	b := packet.Binding.Sources
	if b.RuntimeID != e.RuntimeID || b.BaselineGeneration != e.BaselineGeneration {
		return "", fmt.Errorf("native private area Runtime/generation differs from its original execution")
	}
	plan, _, err := NativeS9PrivatePaths(report.Receipt.PacketID)
	if err != nil {
		return "", err
	}
	return path.Dir(plan), nil
}

func nativeS9PrivateContract(ctx context.Context, root string, packet workpacket.RepairResultPacket, e workspace.Execution) (PlanReport, bool, error) {
	var report PlanReport
	if e.Purpose != workspace.NativeDeliveryPurpose || NativeS9SourceReference(e).Path != "" || len(e.Inputs) == 0 {
		return report, false, nil
	}
	if err := json.Unmarshal(packet.Inputs.PlanReport, &report); err != nil {
		return report, false, err
	}
	if report.Receipt == nil {
		return report, false, nil
	}
	b := packet.Binding.Sources
	// This is the original issued packet coordinate, derived from its frozen
	// binding and accepted Plan receipt. No mutable registry read occurs under CAS.
	pin := fmt.Sprintf(".claude/work-packets/%s/g%d/S9-plan/r%d/%s/%s/native/source.json", b.RuntimeID, b.BaselineGeneration, b.ReviewRound, b.RepairAssignmentID, report.Receipt.PacketID)
	hash, anchored := e.Inputs[pin]
	if !anchored {
		return report, false, nil
	}
	raw, err := workpacket.ReadFile(root, pin, 4096)
	if err != nil {
		return report, false, err
	}
	if workpacket.Digest(raw) != hash {
		return report, false, fmt.Errorf("native private source pin changed")
	}
	var saved struct {
		Version int `json:"private_submission_version"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return report, false, err
	}
	if saved.Version != 1 {
		return report, false, fmt.Errorf("invalid frozen private submission version")
	}
	if report.Receipt.OwnerAgentID != e.AgentID || b.OwnerAgentID != e.AgentID || b.ManifestAssignmentID != e.AssignmentID {
		return report, false, fmt.Errorf("native private source responsibility differs")
	}
	return report, true, nil
}

func captureNativeS9Private(ctx context.Context, root string, packet workpacket.RepairResultPacket, e workspace.Execution, answer json.RawMessage) ([]runtimepkg.RepairResultPrivateSubmission, error) {
	report, enabled, err := nativeS9PrivateContract(ctx, root, packet, e)
	if err != nil || !enabled {
		return nil, err
	}
	plan, result, err := NativeS9PrivatePaths(report.Receipt.PacketID)
	if err != nil {
		return nil, err
	}
	out := []runtimepkg.RepairResultPrivateSubmission{}
	for _, item := range []struct {
		kind, path string
		expected   json.RawMessage
	}{{"plan", plan, report.Receipt.SubmittedAnswer}, {"result", result, answer}} {
		raw, err := readNativeS9Private(ctx, e, packet, item.path, false)
		if err != nil {
			return nil, err
		}
		if err := workpacket.ValidateUniqueJSON(raw); err != nil {
			return nil, err
		}
		var actual, want any
		if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(item.expected, &want) != nil || !reflect.DeepEqual(actual, want) {
			return nil, fmt.Errorf("private professional content differs from original accepted answer: %s", item.path)
		}
		out = append(out, runtimepkg.RepairResultPrivateSubmission{Kind: item.kind, Path: item.path, SHA256: workpacket.Digest(raw)})
	}
	return out, nil
}

func readNativeS9Private(ctx context.Context, e workspace.Execution, packet workpacket.RepairResultPacket, path string, allowMissing bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := workpacket.ReadFile(e.Path, path, repairResultAnswerLimit)
	if os.IsNotExist(err) && allowMissing {
		tracked, gitErr := workspace.Git(ctx, e.Path, "ls-files", "--", path)
		if gitErr != nil {
			return nil, gitErr
		}
		if tracked != "" {
			return nil, fmt.Errorf("missing private original is tracked: %s", path)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	status, err := workspace.GitBytes(ctx, e.Path, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", path)
	if err != nil || string(status) != "?? "+path+"\x00" {
		return nil, fmt.Errorf("native private original is not exactly untracked (possibly ignored or staged): %s", path)
	}
	tracked, err := workspace.Git(ctx, e.Path, "ls-files", "--", path)
	if err != nil {
		return nil, err
	}
	if tracked != "" {
		return nil, fmt.Errorf("native private original is tracked: %s", path)
	}
	if _, err := workspace.Git(ctx, e.Path, "cat-file", "-e", e.BaseCommit+":"+path); err == nil {
		return nil, fmt.Errorf("private original collides with baseline: %s", path)
	}
	for _, scope := range e.WritePaths {
		if workspace.PathMatchesScope(path, scope) {
			return nil, fmt.Errorf("private original collides with product scope: %s", path)
		}
	}
	var session RepairSession
	if err := json.Unmarshal(packet.Inputs.Sources.Session, &session); err != nil {
		return nil, err
	}
	for _, artifact := range session.BaselineArtifacts {
		if artifact.Path == path {
			return nil, fmt.Errorf("private original collides with session baseline: %s", path)
		}
	}
	return raw, nil
}

// NativeS9PrivateProjection projects only original accepted byte identities.
// allowMissing is reserved for original verified/ACK/cleanup recovery; it never
// reconstructs missing originals or adopts newer bytes.
func NativeS9PrivateProjection(ctx context.Context, root string, packet workpacket.RepairResultPacket, e workspace.Execution, candidate WorkspaceCandidate, allowMissing bool) (map[string]string, error) {
	out := map[string]string{}
	report, enabled, err := nativeS9PrivateContract(ctx, root, packet, e)
	if err != nil {
		return nil, err
	}
	if !enabled {
		if candidate.Provenance != nil && len(candidate.Provenance.PrivateSubmissions) != 0 {
			return nil, fmt.Errorf("private projection lacks original frozen contract")
		}
		return out, nil
	}
	if candidate.Provenance == nil || candidate.Provenance.PacketID != packet.ID || len(candidate.Provenance.PrivateSubmissions) != 2 {
		return nil, fmt.Errorf("private projection requires original typed candidate")
	}
	plan, result, err := NativeS9PrivatePaths(report.Receipt.PacketID)
	if err != nil {
		return nil, err
	}
	missingRoot := false
	info, rootErr := os.Lstat(e.Path)
	if rootErr == nil {
		canonical, err := workspace.Canonical(e.Path)
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || err != nil || canonical != e.Path {
			return nil, fmt.Errorf("private source root is not its original canonical directory")
		}
	} else if !os.IsNotExist(rootErr) {
		return nil, rootErr
	}
	if os.IsNotExist(rootErr) && allowMissing {
		// A successful worktree removal can precede the final checkpoint CAS.
		// Only its original verified checkpoint may retire that absent root;
		// an accepted Candidate alone never authorizes a missing Git source.
		_, relative, err := repairResultCheckpointPath(root, packet)
		if err != nil {
			return nil, err
		}
		raw, err := workpacket.ReadFile(root, relative, 20<<20)
		if err != nil {
			return nil, err
		}
		var cp integration.Checkpoint
		if err := json.Unmarshal(raw, &cp); err != nil {
			return nil, err
		}
		if (cp.State != integration.StateVerified && cp.State != integration.StateAcknowledged && cp.State != integration.StateCleanupPending && cp.State != integration.StateComplete) || cp.VerifiedAt == "" || cp.AssignmentID != e.AssignmentID || cp.WorktreePath != e.Path || cp.SourceHead != candidate.SourceHead || cp.TargetBranch != e.TargetBranch || cp.BaselineGeneration != e.BaselineGeneration {
			return nil, fmt.Errorf("missing private source lacks its original verified cleanup checkpoint")
		}
		if _, err := integration.RecoveryBaseForCheckpoint(ctx, root, cp); err != nil {
			return nil, fmt.Errorf("missing private source checkpoint no longer matches its target: %w", err)
		}
		missingRoot = true
	}
	for i, path := range []string{plan, result} {
		item := candidate.Provenance.PrivateSubmissions[i]
		kind := []string{"plan", "result"}[i]
		if item.Path != path || item.Kind != kind || len(item.SHA256) != 64 {
			return nil, fmt.Errorf("private original identity differs")
		}
		if missingRoot {
			continue
		}
		raw, err := readNativeS9Private(ctx, e, packet, path, allowMissing)
		if err != nil {
			return nil, err
		}
		if raw == nil && allowMissing {
			continue
		}
		if workpacket.Digest(raw) != item.SHA256 {
			return nil, fmt.Errorf("accepted private original bytes changed: %s", path)
		}
		out[path] = item.SHA256
	}
	return out, nil
}
