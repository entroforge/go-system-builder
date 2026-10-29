package submission

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// A blocker uses the existing lifecycle Writer and its opaque proof. Holding
// the original workspace lock through prepare and CAS keeps the source tuple
// stable; this adapter owns no additional Runtime transaction.
func (s *Service) saveRepairResultBlocker(ctx context.Context, packet workpacket.RepairResultPacket, owner, sourceRoot, verdict string, authorize func(string, map[string]any) error) (Receipt, error) {
	s = s.WithContext(ctx)
	ctx, unlock, err := integration.LockWorkspace(ctx, s.Root)
	if err != nil {
		return Receipt{}, err
	}
	defer unlock()
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	capability, prior, err := repair.PrepareIssuedRepairResultBlocker(ctx, s.Root, s.StatePath, s.JournalPath, repair.AcceptIssuedRepairResultBlockerRequest{
		ExpectedRevision: snapshot.Revision, PacketID: packet.ID, AnswerPath: packet.AnswerPath,
		OwnerAgentID: owner, SourceRoot: sourceRoot, AuthorizeSource: authorize,
	})
	if err != nil {
		return rejected(snapshot, packet.ID, err), err
	}
	if prior == nil {
		if capability == nil {
			err := errors.New("original S9 blocker preparation returned no current responsibility")
			return rejected(snapshot, packet.ID, err), err
		}
		next, advanceErr := assignment.AdvanceAgentWithContext(ctx, s.Root, s.StatePath, s.JournalPath, assignment.AgentEventRequest{
			ExpectedRevision: snapshot.Revision, AgentID: owner, Event: "work_blocked",
			MessagePath:               filepath.Join(s.Root, filepath.FromSlash(capability.MessageRef())),
			IssuedRepairResultBlocker: capability,
		})
		if advanceErr != nil {
			return rejected(snapshot, packet.ID, advanceErr), advanceErr
		}
		snapshot = next
	} else {
		// Prepare may recover an already committed marker. Rebuild from that
		// original pair instead of returning the pre-prepare snapshot.
		snapshot, err = s.snapshot()
		if err != nil {
			return Receipt{}, err
		}
	}
	result, found, err := repairResultBlockerReceipt(s.Root, snapshot, packet, owner)
	if err == nil && !found {
		err = errors.New("original S9 blocker record is unavailable")
	}
	if err == nil && found && result.currentRepairResultBlocker {
		if verdict == "contract_conflict" {
			result.NextAction = "原 Contract 冲突答卷已接收，本次执行可结束，交付未完成；Main 必须回原 S8 新 Contract 审批人闸，不能在同一 Contract/Session/Plan 上普通 retry。"
		} else if path, publishErr := publishRepairResultResolutionDraft(s.Root, snapshot, packet, owner); publishErr != nil {
			result.Error = "original blocker accepted; Main resolution draft publication failed: " + publishErr.Error()
			result.NextAction += " 原阻塞接收已保留；Main 草稿发布失败。保全来源并原样重放本答卷以恢复该路径的草稿，不重建 Runtime 接收。"
		} else {
			result.NextAction = "原阻塞答卷已接收，本次执行可结束，交付未完成；等待 Main 填写并保存 " + filepath.Join(s.Root, filepath.FromSlash(path)) + " 的恢复理由与依据。"
		}
	}
	return result, err
}

func repairResultBlockerReceipt(root string, snapshot loopruntime.Snapshot, packet workpacket.RepairResultPacket, owner string) (Receipt, bool, error) {
	original, found, err := loopruntime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, packet.ID, owner)
	if err != nil || !found {
		return Receipt{}, found, err
	}
	result := emptyReceipt(snapshot, packet.ID)
	result.Accepted, result.WorkComplete = true, false
	result.ResponsibilityStatus = "blocker_recorded"
	result.SubmissionKey = original.AcceptedIdempotencyKey
	result.DomainRefs = []string{original.ArtifactRef, original.Artifact.MessageRef}
	result.ConsumerOwner, result.ConsumerPending = "main", true
	mayEnd, currentErr := repair.RepairResultBlockerExecutionMayEnd(root, snapshot.State, packet.ID, owner)
	result.currentRepairResultBlocker = currentErr == nil && mayEnd
	result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "original S9 blocker requires Main recovery", Owner: "Main", Ref: original.ArtifactRef})
	result.NextAction = "阻塞答卷已记录，交付未完成；当前责任已变化，由 Main 检查当前工作安排。原答卷：" + filepath.Join(original.Artifact.ExecutionRoot, filepath.FromSlash(packet.AnswerPath)) + "；Main 按原当前责任接手。"
	if result.currentRepairResultBlocker {
		var answer struct {
			Verdict string `json:"verdict"`
		}
		_ = json.Unmarshal(original.Artifact.SubmittedAnswer, &answer)
		if answer.Verdict == "contract_conflict" {
			result.NextAction = "原 Contract 冲突答卷已接收，本次执行可结束，交付未完成；Main 必须回原 S8 新 Contract 审批人闸，不能在同一 Contract/Session/Plan 上普通 retry。"
		} else if path, pathErr := workpacket.RepairResultResolutionAuthoringPath(packet); pathErr != nil {
			result.NextAction = "原阻塞答卷已接收；本人交付尚未完成。Main 恢复问卷的原路径投影不可用：" + pathErr.Error()
		} else {
			path = filepath.Join(root, filepath.FromSlash(path))
			result.WaitingOn = []Waiting{{Reason: "original S9 blocker requires Main recovery", Owner: "Main", Ref: path}}
			result.NextAction = "原阻塞答卷已接收，本次执行可结束，交付未完成；等待 Main 填写并保存 " + path + " 的恢复理由与依据。"
		}
	}
	if currentErr != nil {
		result.Error = "blocker recorded; current responsibility information unavailable: " + currentErr.Error()
	}
	return result, true, nil
}
