package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func CheckBinding(e Execution) checkexec.Binding {
	return checkexec.Binding{RuntimeID: e.RuntimeID, Generation: e.BaselineGeneration, AssignmentID: e.AssignmentID, Owner: e.AgentID, ExecutionGeneration: e.Generation, Responsibility: "owned_check"}
}

// AcceptToolReceipt binds only the current tool facts. The CLI calls this
// under the original Writer/CAS with a process-local runner capture.
func (b *ExecutionRegistry) AcceptToolReceipt(e *Execution, ref checkexec.ToolReceipt) error {
	if ref.Binding != CheckBinding(*e) || ref.Checkout != e.Path || e.CheckLocation == "main" {
		return fmt.Errorf("tool receipt differs from original Worker responsibility")
	}
	found := false
	for _, p := range e.CheckProfiles {
		if p.Command == ref.Command && p.Digest() == ref.ProfileDigest {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("tool receipt has no original frozen profile")
	}
	if _, err := checkexec.VerifyToolReceipt(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), ref); err != nil {
		return err
	}
	for _, previous := range e.ToolCheckReceipts {
		if previous.SHA256 == ref.SHA256 {
			return nil
		}
	}
	e.ToolCheckReceipts = append(e.ToolCheckReceipts, ref)
	if len(e.ToolCheckReceipts) > 64 {
		e.ToolCheckReceipts = e.ToolCheckReceipts[len(e.ToolCheckReceipts)-64:]
	}
	return nil
}

type CheckReuseProjection struct {
	CheckKind  string                  `json:"check_kind,omitempty"`
	ProfileID  string                  `json:"profile_id"`
	CheckIndex int                     `json:"check_index"`
	Decision   checkexec.ReuseDecision `json:"decision"`
}

// CheckReuseProjection is read-only and cannot skip an actual execution.
// It uses the actual disposable-copy algorithm and original isolation probe.
// Unavailable/unknown permissions cannot produce a hit. Main always runs.
func (b *ExecutionRegistry) CheckReuse(ctx context.Context, e Execution) []CheckReuseProjection {
	result := []CheckReuseProjection{}
	for _, p := range e.CheckProfiles {
		binding := CheckBinding(e)
		source := e.Path
		if e.CheckLocation == "main" {
			source = b.MainRoot
			binding.Responsibility = "main_required_integration"
		}
		current := checkexec.ReuseRequest{Profile: p, Binding: binding, Checkout: source}
		guard := checkexec.CurrentReuseGuard(ctx)
		current.Fresh, current.NewHypothesis = guard.Fresh, guard.NewHypothesis
		var decision checkexec.ReuseDecision
		if e.CheckLocation == "main" || p.ReusePolicy == "never" {
			decision = checkexec.DecideReuse(b.MainRoot, current, e.ToolCheckReceipts)
		} else {
			if !checkexec.ClosedStaticCheck(p.Command) {
				decision = checkexec.ReuseDecision{Action: "run", Reason: "unclosed_command_inputs"}
				result = append(result, CheckReuseProjection{ProfileID: p.ID, CheckIndex: p.CheckIndex, Decision: decision})
				continue
			}
			observation, err := b.observeCopiedCheck(ctx, e, p)
			if err != nil {
				decision = checkexec.ReuseDecision{Action: "run", Reason: "current_inputs_unavailable"}
			} else {
				current.Observation = observation
				decision = checkexec.DecideReuse(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), current, e.ToolCheckReceipts)
			}
		}
		result = append(result, CheckReuseProjection{ProfileID: p.ID, CheckIndex: p.CheckIndex, Decision: decision})
	}
	for index, check := range e.WorkerChecks {
		binding := CheckBinding(e)
		current := checkexec.ReuseRequest{Profile: check.Profile, Binding: binding, Checkout: e.Path}
		guard := checkexec.CurrentReuseGuard(ctx)
		current.Fresh, current.NewHypothesis = guard.Fresh, guard.NewHypothesis
		var decision checkexec.ReuseDecision
		if check.Profile.ReusePolicy == "never" {
			decision = checkexec.ReuseDecision{Action: "run", Reason: "profile_reuse_never"}
		} else {
			observation, err := b.observeCopiedOwnedCheck(ctx, e, index)
			if err != nil {
				decision = checkexec.ReuseDecision{Action: "run", Reason: "current_inputs_unavailable"}
			} else {
				current.Observation = observation
				decision = checkexec.DecideReuse(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), current, latestOwnedCheckReceipts(e.OwnedCheckReceipts, index))
			}
		}
		result = append(result, CheckReuseProjection{CheckKind: "worker_owned", ProfileID: check.Profile.ID, CheckIndex: index, Decision: decision})
	}
	return result
}

func (b *ExecutionRegistry) observeCopiedCheck(ctx context.Context, e Execution, p checkexec.Profile) (checkexec.Observation, error) {
	scratch, err := os.MkdirTemp("", "loop-check-reuse-observe-")
	if err != nil {
		return checkexec.Observation{}, err
	}
	defer os.RemoveAll(scratch)
	if _, err := copyCheckTree(ctx, e.Path, scratch); err != nil {
		return checkexec.Observation{}, err
	}
	ctx = checkexec.WithProfile(ctx, p)
	observation, err := checkexec.ObserveProfile(ctx, scratch, p)
	if err != nil {
		return observation, err
	}
	permissions := b.observeCheckPermissions(ctx, e, scratch)
	observation.Permissions = &permissions
	observation.PermissionDigest = permissions.Digest()
	return observation, nil
}
