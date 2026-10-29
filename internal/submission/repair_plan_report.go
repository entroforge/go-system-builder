package submission

import (
	"bytes"
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
	s = s.WithContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The original packet Issuer owns pending-marker recovery and its own CAS.
	// Do not pre-read through the read-only service snapshot here: a pending
	// original PACKET-ISSUE must be recoverable by this named mutation path.
	packets, err := repair.IssueRepairPlanReportPacketsContext(ctx, s.Root, s.StatePath, s.JournalPath, -1, ownerFilter)
	if err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := publishRepairPlanReportView(s.Root, packet); err != nil {
			return nil, err
		}
		draft, err := jsonMarshalIndent(packet.Draft)
		if err != nil {
			return nil, err
		}
		if _, err := workpacket.PublishBounded(s.Root, packet.AnswerPath, append(draft, '\n'), repairPlanReportAnswerLimit); err != nil {
			return nil, err
		}
	}
	return packets, nil
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
	s = s.WithContext(ctx)
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, "", err), err
	}
	packet, actualRoot, found, err := currentRepairPlanReportSourceForPath(s.Root, snapshot.State, path, s.Context())
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
		ExpectedRevision: snapshot.Revision,
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
	receipt.NextAction = "本人 S9 PlanReport 已记录；原生 Builder 继续原专业修复、自检并返回实际工作树中的 Result，由 Stop 接收。Main 单独完成原集成与必要检查，无需重复提交 Plan 或分工卷；已冻结旧执行保留原恢复路径。"
	// Only an actual existing frozen execution keeps workspace recovery.
	// A missing native brief is a preparation gap, never legacy authority.
	workspaceState, _ := next.State["workspace"].(map[string]any)
	executions, _ := workspaceState["executions"].(map[string]any)
	execution, _ := executions[packet.Binding.ManifestAssignmentID].(map[string]any)
	brief := filepath.Join(s.Root, filepath.Dir(packet.ViewPath), packet.ID, "native", "brief.md")
	if len(execution) > 0 && execution["purpose"] != "native_delivery" {
		receipt.NextAction = fmt.Sprintf("本人 S9 PlanReport 已记录；Main 保留已登记旧执行的原恢复位置：runtime workspace prepare --root %q --assignment %q --agent %q --check-location %s。准备完成后提供本人的准确 RepairResult 卷，无需重复提交 Plan 或分工卷。", s.Root, packet.Binding.ManifestAssignmentID, owner, execution["check_location"])
	} else if _, err := os.Stat(brief); err != nil {
		receipt.NextAction += fmt.Sprintf(" 原生简报 %s 暂不可读，由 Main/系统恢复该原准备资料；不转为旧 workspace startup，不重填已接收 Plan。", brief)
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
	packets, err := loopruntime.CurrentRepairPlanReportPacketsContext(submissionRepairPacketContext(contexts...), root, state, "")
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, "", false, err
	}
	return repairPlanReportPathInPackets(root, inputPath, packets)
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

func publishRepairPlanReportView(root string, packet workpacket.RepairPlanReportPacket) error {
	mainRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	display := packet.Draft
	display.Read = append([]workpacket.ReadItem(nil), packet.Draft.Read...)
	for i := range display.Read {
		if !filepath.IsAbs(display.Read[i].Path) {
			display.Read[i].Path = filepath.Join(mainRoot, filepath.FromSlash(display.Read[i].Path))
		}
	}
	data, err := jsonMarshalIndent(display)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	stored, err := workpacket.PublishBounded(root, packet.ViewPath, data, repairPlanReportAnswerLimit)
	if err != nil {
		return err
	}
	if !bytes.Equal(stored, data) {
		return fmt.Errorf("S9 issued view differs from the professional questionnaire: %s", packet.ViewPath)
	}
	return nil
}

func sameRepairPlanReportPacket(a, b workpacket.RepairPlanReportPacket) bool {
	left, err := workpacket.RepairPlanReportPacketDigest(a)
	if err != nil {
		return false
	}
	right, err := workpacket.RepairPlanReportPacketDigest(b)
	return err == nil && left == right
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
		accepted.ReportID != packet.ReportID || accepted.AnswerSHA256 == "" || accepted.WorkComplete {
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
