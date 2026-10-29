package submission

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Revise is a Main scheduling operation. It never edits an existing answer or
// grants new responsibility/activation. Publication is recoverable after the
// original issuance transaction; its presence is not authority.
func (s *Service) Revise(ctx context.Context, previous, reason string) (workpacket.Packet, error) {
	s = s.WithContext(ctx)
	if err := workspace.RequireMain(s.Root); err != nil {
		return workpacket.Packet{}, err
	}
	locked, release, err := integration.LockWorkspace(ctx, s.Root)
	if err != nil {
		return workpacket.Packet{}, err
	}
	defer release()
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.Packet{}, err
	}
	r, ok, err := runtime.FindSubmissionKey(snap.State, previous)
	if err != nil || !ok {
		return workpacket.Packet{}, fmt.Errorf("named revision requires the previous receipt's submission_key")
	}
	old, ok, err := runtime.FindWorkPacket(snap.State, r.PacketID)
	if err != nil || !ok {
		return workpacket.Packet{}, fmt.Errorf("previous reception has no packet")
	}
	// Historical issuance replay does not require a live old execution and does
	// not restore its answer grant. The Writer checks the exact original reason.
	req := runtime.DocumentReviewRevisionRequest{PreviousSubmissionKey: previous, Reason: reason, Files: s.Files, Validator: s.Validator}
	if r.RevisionPacketID == "" {
		e, declared, err := workspace.ReviewExecution(snap.State, old)
		if err != nil {
			return workpacket.Packet{}, err
		}
		source := authoringSource{Root: s.Root, Path: old.AnswerPath}
		if declared {
			if e.Status != "ready" || e.Path == "" {
				return workpacket.Packet{}, fmt.Errorf("previous independent review has no ready registered source")
			}
			answerPath, err := workerAnswerAuthoringPath(snap.State, e)
			if err != nil {
				return workpacket.Packet{}, err
			}
			source = authoringSource{Root: e.Path, Path: answerPath, Execution: &e}
			leaseCtx, cancel := context.WithTimeout(locked, 100*time.Millisecond)
			lease, err := filelock.Acquire(leaseCtx, filepath.Join(s.Root, ".claude/workspace-launch", e.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", e.BaselineGeneration, e.AssignmentID, e.Generation)))
			cancel()
			if err != nil {
				return workpacket.Packet{}, fmt.Errorf("previous Worker still owns its launch lease: %w", err)
			}
			defer lease()
		}
		req.ExecutionRequired = declared
		req.Authorize = func(state map[string]any, previous, successor workpacket.Packet) error {
			if err := locked.Err(); err != nil {
				return err
			}
			if err := validateS5ApprovalMode(s.Root, state, previous.Binding.Owner); err != nil {
				return err
			}
			if err := s.validateSource(locked, state, previous, source); err != nil {
				return err
			}
			if source.Execution == nil {
				if successor.Binding.ExecutionRequired {
					return fmt.Errorf("independent revision cannot fall back to Main")
				}
				return nil
			}
			b, err := workspace.Decode(state)
			if err != nil || b == nil {
				return fmt.Errorf("revision Worker registry unavailable: %v", err)
			}
			current, ok := b.Executions[previous.Binding.AssignmentID]
			if !ok || !reflect.DeepEqual(current, *source.Execution) {
				return fmt.Errorf("previous execution changed before revision issuance")
			}
			if err := b.ValidateInputs(current); err != nil {
				return err
			}
			current.Status = "retired"
			b.History = append(b.History, current)
			delete(b.Executions, current.AssignmentID)
			state["workspace"] = workspace.Encode(b)
			return nil
		}
	}
	_, next, err := runtime.RecordDocumentReviewRevision(s.Root, s.StatePath, s.JournalPath, req)
	if err != nil {
		return workpacket.Packet{}, err
	}
	// Same command after an interrupted projection restores only the current
	// tip, preserving any authored bytes and independent-source requirement.
	if _, err := s.IssueContext(locked, next.Binding.Owner); err != nil {
		return next, err
	}
	return next, nil
}
