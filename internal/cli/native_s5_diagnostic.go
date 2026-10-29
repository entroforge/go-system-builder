package cli

import (
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The existing diagnostic cache is display-only. A packet-scoped key prevents
// another reviewer or a revised attempt from inheriting the last Stop failure.
func nativeS5DiagnosticKind(packet string) string { return "S5 native Stop " + packet }

func nativeS5ReturnDiagnostic(root string, state map[string]any, p workpacket.Packet) (string, string) {
	const nextAction = "S5 尚无接收事实。Main 根据本次原诊断与实际原生返回处理：真实独立性或来源阻塞由 Main 恢复来源或安排独立审查者；仅有专业答案缺项则续跑原责任修正具体字段。不能为退出选择虚假结论；后台启动返回不代表执行已结束。"
	life := mapFieldCLI(state, "lifecycle")
	if p.Binding.RuntimeID != state["runtime_id"] || p.Binding.Generation != workspace.Generation(state) || life["state"] != "document_verification" || p.Binding.StageEntry != tolerantInt(life["phase_revision"]) {
		return "This S5 reference is historical. ", "Main 保留历史返回并继续当前责任；不恢复这个旧 attempt，不以新责任的结果补认旧接收。"
	}
	if diagnosis := submission.PlanningDiagnostic(root, state, nativeS5DiagnosticKind(p.ID)); diagnosis != "" {
		return diagnosis + ". This is the last attempted reception, not a fresh validation of edited files. ", nextAction
	}
	answer := filepath.Join(root, p.AnswerPath)
	if execution, declared, err := workspace.ReviewExecution(state, p); err != nil {
		return fmt.Sprintf("Original S5 source for %s is unavailable: %v. Main restores that registered source; do not use a shared answer instead. ", p.ID, err), nextAction
	} else if declared {
		registry, err := workspace.Decode(state)
		if err != nil || registry == nil {
			return fmt.Sprintf("Original S5 execution registry for %s is unavailable: %v. Main restores the original source binding. ", p.ID, err), nextAction
		}
		paths, err := registry.WorkerSubmissionPaths(execution)
		if err != nil {
			return fmt.Sprintf("Original S5 answer source for %s is unavailable: %v. Main restores that registered source. ", p.ID, err), nextAction
		}
		answer = filepath.Join(execution.Path, paths.Answer)
	}
	command := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime stage-answer receipt --root " + s7CaptureShellQuote(root) + " --agent " + s7CaptureShellQuote(p.Binding.Owner) + " --path " + s7CaptureShellQuote(answer)
	return "No readable original Stop diagnostic is available for " + p.ID + ". If the actual native execution has ended, Main can inspect the current exact answer and sources without receiving it: `" + command + "`. This explicit read-only diagnostic does not recover the lost historical error or establish completion; a background launch alone does not call for diagnosis. ", nextAction
}
