package submission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const repairPlanReportAnswerLimit int64 = 20 << 20

// IssueRepairPlanReports delegates publication to the original S9 packet
// Writer. It adds no packet registry or receipt authority of its own.
func (s *Service) IssueRepairPlanReports(ctx context.Context, ownerFilter string) ([]workpacket.RepairPlanReportPacket, error) {
	return s.IssueRepairPlanReportsPrepared(ctx, ownerFilter, nil)
}

func (s *Service) IssueRepairPlanReportsPrepared(ctx context.Context, ownerFilter string, prepare repair.RepairPlanReportPreparation) ([]workpacket.RepairPlanReportPacket, error) {
	s = s.WithContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The original packet Issuer owns pending-marker recovery and its own CAS.
	// Do not pre-read through the read-only service snapshot here: a pending
	// original PACKET-ISSUE must be recoverable by this named mutation path.
	packets, err := repair.IssueRepairPlanReportPacketsPreparedContext(ctx, s.Root, s.StatePath, s.JournalPath, -1, ownerFilter, prepare)
	if err != nil {
		return nil, err
	}
	return packets, nil
}

// IssueRepairPlanReportForAssignment publishes only the requested original
// responsibility and returns whether this call actually issued its packet.
func (s *Service) IssueRepairPlanReportForAssignment(ctx context.Context, owner, assignmentID string) ([]repair.RepairPlanReportIssuance, error) {
	return s.IssueRepairPlanReportForAssignmentPrepared(ctx, owner, assignmentID, nil)
}

// IssueRepairPlanReportForAssignmentPrepared keeps native source preparation
// inside the original assignment-scoped issuer, before durable issuance.
func (s *Service) IssueRepairPlanReportForAssignmentPrepared(ctx context.Context, owner, assignmentID string, prepare repair.RepairPlanReportPreparation) ([]repair.RepairPlanReportIssuance, error) {
	s = s.WithContext(ctx)
	results, err := repair.IssueRepairPlanReportForAssignmentPreparedContext(ctx, s.Root, s.StatePath, s.JournalPath, -1, owner, assignmentID, prepare)
	if err != nil {
		return nil, err
	}
	packets := make([]workpacket.RepairPlanReportPacket, 0, len(results))
	for _, result := range results {
		packets = append(packets, result.Packet)
	}
	return results, nil
}

// RepairPlanReportSourceForPath resolves a current answer path to its exact
// packet and source root. The pre-execution S9 profile retains its original
// Main-root authoring source.
func (s *Service) RepairPlanReportSourceForPath(path string) (workpacket.RepairPlanReportPacket, string, bool, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, "", false, err
	}
	return currentRepairPlanReportSourceForPath(s.Root, snapshot.State, path, s.Context())
}

// SaveRepairPlanReportFrom accepts a typed author answer through the original
// RepairPlanReport Writer. The legacy authorization callback is not a save
// qualification; the registered packet supplies the current responsibility.
func (s *Service) SaveRepairPlanReportFrom(ctx context.Context, owner, path, sourceRoot string, authorize func(root string, state map[string]any) error) (Receipt, error) {
	return s.saveRepairPlanReport(ctx, owner, path, sourceRoot, nil)
}

// SaveRepairPlanReportBytes consumes the already located author's original.
// No typed authoring copy is written, and accepted receipts never reopen it.
func (s *Service) SaveRepairPlanReportBytes(ctx context.Context, owner, packetID, sourceRoot string, raw []byte) (Receipt, error) {
	return s.saveRepairPlanReport(ctx, owner, packetID, sourceRoot, raw)
}

func (s *Service) saveRepairPlanReport(ctx context.Context, owner, path, sourceRoot string, raw []byte) (Receipt, error) {
	s = s.WithContext(ctx)
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, "", err), err
	}
	var packet workpacket.RepairPlanReportPacket
	var actualRoot string
	var found bool
	if raw != nil {
		packet, found, err = repairPlanReportByReference(s.Root, snapshot.State, path, ctx)
		actualRoot = sourceRoot
	} else {
		packet, actualRoot, found, err = currentRepairPlanReportSourceForPath(s.Root, snapshot.State, path, s.Context())
	}
	if err != nil {
		return rejected(snapshot, "", err), err
	}
	if !found {
		err := errors.New("S9 answer path has no current registered Plan responsibility")
		return rejected(snapshot, "", err), err
	}
	owner = packet.Binding.OwnerAgentID
	sourceRoot = actualRoot
	next, report, submitErr := repair.SubmitIssuedRepairPlanReport(s.Root, s.StatePath, s.JournalPath, repair.IssuedPlanReportRequest{
		Context:          ctx,
		AnswerBytes:      raw,
		ExpectedRevision: -1,
		PacketID:         packet.ID,
		AnswerPath:       packet.AnswerPath,
		OwnerAgentID:     owner,
		SourceRoot:       actualRoot,
		OccurredAt:       time.Now().UTC(),
	})
	if submitErr != nil {
		return rejected(snapshot, packet.ID, submitErr), submitErr
	}
	if report.Receipt == nil {
		return rejected(next, packet.ID, errors.New("original S9 consumer returned without a recorded PlanReport")), nil
	}
	receipt, err := acceptedRepairPlanReportReceipt(next, packet, *report.Receipt)
	if err != nil {
		return receipt, err
	}
	receipt.NextAction = "本人 S9 PlanReport 已记录；原生 Builder 继续原专业修复、自检，在实际工作树的准确原路径保存 Result；适用 PreToolUse、成功 PostToolUse 和 Stop/SubagentStop 经原消费者接收，无需额外提交或返回路径。Main 单独完成原集成与必要检查，无需重复提交 Plan 或分工卷。"
	if packet.BriefPath != "" {
		brief := filepath.Join(s.Root, filepath.FromSlash(packet.BriefPath))
		if _, err := os.Stat(brief); err != nil {
			receipt.NextAction += fmt.Sprintf(" 原生简报 %s 暂不可读，由 Main/系统恢复该原准备资料；不重填已接收 Plan。", brief)
		}
	}
	return receipt, nil
}

// RepairPlanReportReceipt reconstructs an accepted pre-execution receipt by
// packet ID or exact registered answer path. It does not read the answer file.
func (s *Service) RepairPlanReportReceipt(owner, ref string) (Receipt, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	packet, found, err := repairPlanReportByReference(s.Root, snapshot.State, ref, s.Context())
	if err != nil {
		return Receipt{}, err
	}
	if !found || owner == "" || packet.Binding.OwnerAgentID != owner {
		return rejected(snapshot, "", errors.New("S9 packet reference has no matching registered Builder")), nil
	}
	accepted, ok, err := repair.IssuedRepairPlanReportReceipt(s.Root, snapshot.State, packet.ID, owner)
	if err != nil {
		return Receipt{}, err
	}
	if !ok {
		return rejected(snapshot, packet.ID, errors.New("PlanReport has not been accepted")), nil
	}
	return acceptedRepairPlanReportReceipt(snapshot, packet, accepted)
}

func currentRepairPlanReportSourceForPath(root string, state map[string]any, inputPath string, contexts ...context.Context) (workpacket.RepairPlanReportPacket, string, bool, error) {
	ctx := submissionRepairPacketContext(contexts...)
	routes, err := loopruntime.CurrentRepairPlanReportRoutesContext(ctx, state)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, "", false, err
	}
	// Compare exact original coordinates before reading any selected body.
	coordinates := make([]workpacket.RepairPlanReportPacket, 0, len(routes))
	for _, route := range routes {
		coordinates = append(coordinates, workpacket.RepairPlanReportPacket{ID: route.ID, AnswerPath: route.AnswerPath})
	}
	selected, source, found, err := repairPlanReportPathInPackets(root, inputPath, coordinates)
	if err != nil || !found {
		return selected, source, found, err
	}
	packet, found, err := loopruntime.FindRepairPlanReportPacketContext(loopruntime.WithPacketBodyRoot(ctx, root), state, selected.ID)
	return packet, source, found, err
}

func repairPlanReportByReference(root string, state map[string]any, ref string, contexts ...context.Context) (workpacket.RepairPlanReportPacket, bool, error) {
	if packet, found, err := loopruntime.FindRepairPlanReportPacketContext(loopruntime.WithPacketBodyRoot(submissionRepairPacketContext(contexts...), root), state, ref); err != nil || found {
		return packet, found, err
	}
	packets, err := loopruntime.RepairPlanReportPacketsForPathContext(loopruntime.WithPacketBodyRoot(submissionRepairPacketContext(contexts...), root), state, ref)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, false, err
	}
	packet, _, found, err := repairPlanReportPathInPackets(root, ref, packets)
	return packet, found, err
}

func repairPlanReportPathInPackets(root, inputPath string, packets []workpacket.RepairPlanReportPacket) (workpacket.RepairPlanReportPacket, string, bool, error) {
	abs := inputPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(inputPath))
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, "", false, err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, "", false, err
	}
	var selected *workpacket.RepairPlanReportPacket
	for _, packet := range packets {
		candidate, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(packet.AnswerPath)))
		if err != nil || filepath.Clean(candidate) != filepath.Clean(abs) {
			continue
		}
		if err := workpacket.CheckPath(rootAbs, packet.AnswerPath); err != nil {
			return workpacket.RepairPlanReportPacket{}, "", false, err
		}
		if selected != nil && selected.ID != packet.ID {
			return workpacket.RepairPlanReportPacket{}, "", false, errors.New("S9 answer path is ambiguous across issued packets")
		}
		copyPacket := packet
		selected = &copyPacket
	}
	if selected == nil {
		return workpacket.RepairPlanReportPacket{}, "", false, nil
	}
	return *selected, rootAbs, true, nil
}

func sameRepairPlanReportPacket(a, b workpacket.RepairPlanReportPacket) bool {
	return a.ID == b.ID && a.Binding.RuntimeID == b.Binding.RuntimeID &&
		a.Binding.SessionID == b.Binding.SessionID && a.Binding.RepairAssignmentID == b.Binding.RepairAssignmentID &&
		a.Binding.OwnerAgentID == b.Binding.OwnerAgentID
}

func hasRepairPlanReportPacket(packets []workpacket.RepairPlanReportPacket, expected workpacket.RepairPlanReportPacket) bool {
	for _, packet := range packets {
		if packet.ID == expected.ID && sameRepairPlanReportPacket(packet, expected) {
			return true
		}
	}
	return false
}

func acceptedRepairPlanReportReceipt(snapshot loopruntime.Snapshot, packet workpacket.RepairPlanReportPacket, accepted loopruntime.RepairPlanReportReceipt) (Receipt, error) {
	result := emptyReceipt(snapshot, packet.ID)
	if accepted.PacketID != packet.ID || accepted.OwnerAgentID != packet.Binding.OwnerAgentID ||
		accepted.ReportID != packet.ReportID || accepted.WorkComplete {
		return result, errors.New("original S9 PlanReport receipt differs from its issued packet")
	}
	result.SubmissionKey = accepted.AcceptedIdempotencyKey
	result.Accepted = true
	result.ResponsibilityStatus = "accepted"
	result.WorkComplete = false
	result.DomainRefs = []string{accepted.ReportRef}
	result.WaitingOn = []Waiting{}
	result.NextAction = "S9 实施前 PlanReport 已记录；继续当前专业工序，无需另交 generic PLAN 或激活报告。"
	return result, nil
}

func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func submissionRepairPacketContext(contexts ...context.Context) context.Context {
	if len(contexts) != 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}
