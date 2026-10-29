package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func (s *Service) IssueTaskPlanning(ctx context.Context) (workpacket.TaskPlanningPacket, error) {
	s = s.WithContext(ctx)
	if err := workspace.RequireMain(s.Root); err != nil {
		return workpacket.TaskPlanningPacket{}, err
	}
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.TaskPlanningPacket{}, err
	}
	binding, err := runtime.CurrentTaskPlanningBinding(snap.State)
	if err != nil {
		return workpacket.TaskPlanningPacket{}, err
	}
	p, found, err := currentTaskPlanningPacket(snap.State, binding)
	if err != nil {
		return p, err
	}
	if !found {
		inputs, err := runtime.ResolveTaskPlanningInputs(s.Root, snap.State, s.Files)
		if err != nil {
			return p, err
		}
		p, err = workpacket.BuildTaskPlanning(binding, inputs)
		if err != nil {
			return p, err
		}
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if _, err := runtime.RecordTaskPlanningPacket(s.Root, s.StatePath, s.JournalPath, p, s.Files, s.Validator); err != nil {
		return p, err
	}
	view, _ := json.MarshalIndent(p, "", "  ")
	view = append(view, '\n')
	actual, err := workpacket.PublishBounded(s.Root, p.ViewPath, view, int64(len(view)))
	if err != nil {
		return p, err
	}
	if !bytes.Equal(actual, view) {
		return p, fmt.Errorf("published S4 view differs from original issuance")
	}
	draft, _ := json.MarshalIndent(p.Draft, "", "  ")
	if _, err := workpacket.PublishBounded(s.Root, p.AnswerPath, append(draft, '\n'), workpacket.TaskPlanningAnswerReadLimit(p)); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Service) TaskPlanningForPath(path string) (workpacket.TaskPlanningPacket, bool, error) {
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.TaskPlanningPacket{}, false, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		p, found, err := runtime.FindTaskPlanningPacket(snap.State, text(row["packet_id"]))
		if err != nil {
			return p, false, err
		}
		if found && filepath.Clean(path) == filepath.Join(s.Root, filepath.FromSlash(p.AnswerPath)) {
			return p, true, workpacket.CheckPath(s.Root, p.AnswerPath)
		}
	}
	return workpacket.TaskPlanningPacket{}, false, nil
}

func (s *Service) SaveTaskPlanning(ctx context.Context, path string) (Receipt, error) {
	s = s.WithContext(ctx)
	p, issued, err := s.TaskPlanningForPath(path)
	if err != nil || !issued {
		return Receipt{}, fmt.Errorf("S4 answer path has no original Main issuance: %v", err)
	}
	snap, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	data, err := workpacket.ReadFile(s.Root, p.AnswerPath, workpacket.TaskPlanningAnswerReadLimit(p))
	if err != nil {
		return s.rejectedTaskPlanning(snap, p, err), err
	}
	answer, issues := workpacket.ValidateTaskPlanningAnswer(p, data)
	if len(issues) > 0 {
		err := fmt.Errorf("S4 task-planning answer %s", planningFieldDiagnostic(issues[0]))
		r := s.rejectedTaskPlanning(snap, p, err)
		r.FieldErrors = issues
		return r, err
	}
	digest, err := workpacket.TaskPlanningAnswerDigest(answer)
	if err != nil {
		return Receipt{}, err
	}
	prior, accepted, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil {
		return Receipt{}, err
	}
	id := "ev-submission-" + workpacket.Digest([]byte("submission:"+p.ID))
	envelopePath := ".claude/evidence/submissions/" + p.ID + "/" + digest + ".json"
	if accepted {
		if prior.ContentHash != digest {
			return s.rejectedTaskPlanning(snap, p, runtime.ErrSubmissionConflict), runtime.ErrSubmissionConflict
		}
		RecordPlanningDiagnostic(s.Root, snap.State, "reception", "")
		life, _ := snap.State["lifecycle"].(map[string]any)
		baseline, _ := snap.State["baseline"].(map[string]any)
		if (prior.ControlStatus == "pending" || prior.ControlStatus == "no_transition") && snap.State["runtime_id"] == p.Binding.RuntimeID && number(baseline["generation"]) == p.Binding.Generation && life["state"] == "planning" && life["phase"] == "tasks" && number(life["phase_revision"]) == p.Binding.StageEntry {
			result, controlErr := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, CWD: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: prior.FollowupID, Files: s.Files})
			return s.taskPlanningControlReceipt(snap, p, result, controlErr)
		}
		return s.taskPlanningReceipt(snap, p)
	} else {
		life, _ := snap.State["lifecycle"].(map[string]any)
		baseline, _ := snap.State["baseline"].(map[string]any)
		if snap.State["runtime_id"] != p.Binding.RuntimeID || number(baseline["generation"]) != p.Binding.Generation || life["state"] != "planning" || life["phase"] != "tasks" || number(life["phase_revision"]) != p.Binding.StageEntry {
			err := fmt.Errorf("S4 answer is not for the current task-planning responsibility")
			return s.rejectedTaskPlanning(snap, p, err), err
		}
		if err := runtime.ValidateTaskPlanningReceptionSources(s.Root, snap.State, p, s.Files); err != nil {
			return s.rejectedTaskPlanning(snap, p, err), err
		}
		if err := s.validateTaskPlanningChecks(snap.State, p); err != nil {
			return s.rejectedTaskPlanning(snap, p, err), err
		}
		arch, err := runtime.TaskPlanningArtifacts(s.Root, snap.State, p, answer, s.Files)
		if err != nil {
			return s.rejectedTaskPlanning(snap, p, err), err
		}
		envelope, err := workpacket.TaskPlanningEvidence(p, id, arch, answer, time.Now().UTC())
		if err != nil {
			return Receipt{}, err
		}
		encoded, _ := json.MarshalIndent(envelope, "", "  ")
		_, err = workpacket.PublishBounded(s.Root, envelopePath, append(encoded, '\n'), int64(len(encoded)+1))
		if err != nil {
			return Receipt{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return s.rejectedTaskPlanning(snap, p, err), err
	}
	next, err := runtime.RecordEvidence(s.Root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{Context: ctx, ExpectedRevision: -1, ID: id, Kind: "planning_task", Path: envelopePath, ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: "Main", AnswerJSON: data, Files: s.Files}})
	if err != nil {
		r := s.rejectedTaskPlanning(snap, p, err)
		if ctx.Err() != nil {
			r.ConsumerOwner, r.ConsumerPending = "Main", true
			r.NextAction = "S4 接收事务中断，尚不能确认最终接收状态：" + err.Error() + "。保留原答卷；Main 使用 " + PlanningRecoveryCommand(s.Root, "recover") + " 恢复原事务并核实接收/阶段事实，再决定是否仍需保存。"
			RecordPlanningDiagnostic(s.Root, snap.State, "reception", r.NextAction)
		}
		return r, err
	}
	record, found, err := runtime.FindSubmission(next.State, p.ID)
	if err != nil || !found {
		return Receipt{}, fmt.Errorf("S4 reception has no durable original record")
	}
	result, controlErr := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, CWD: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: record.FollowupID, Files: s.Files})
	return s.taskPlanningControlReceipt(next, p, result, controlErr)

}

func currentTaskPlanningPacket(state map[string]any, binding workpacket.PlanningBinding) (workpacket.TaskPlanningPacket, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var current workpacket.TaskPlanningPacket
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		candidate, found, err := runtime.FindTaskPlanningPacket(state, text(row["packet_id"]))
		if err != nil {
			return current, false, err
		}
		if !found || candidate.Binding.RuntimeID != binding.RuntimeID || candidate.Binding.Generation != binding.Generation || candidate.Binding.StageEntry != binding.StageEntry {
			continue
		}
		if candidate.Binding != binding {
			return candidate, false, fmt.Errorf("S4 current stage has an original packet with a different Main or locked REQ binding")
		}
		if current.ID != "" {
			return current, false, fmt.Errorf("S4 current stage has more than one original questionnaire")
		}
		current = candidate
	}
	return current, current.ID != "", nil
}

func (s *Service) rejectedTaskPlanning(snapshot runtime.Snapshot, p workpacket.TaskPlanningPacket, err error) Receipt {
	RecordPlanningDiagnostic(s.Root, snapshot.State, "reception", err.Error())
	// Rejection belongs to this save attempt. An edited or malformed answer
	// cannot erase the original reception, its references, or its current status.
	if prior, projectionErr := s.taskPlanningReceipt(snapshot, p); projectionErr == nil && prior.Accepted {
		prior.AttemptStatus = "rejected"
		prior.Error = err.Error()
		prior.NextAction = "本次 S4 保存尝试被拒绝，新内容未接收；原接收记录保留。具体原因：" + err.Error() + "。" + s.taskPlanningRejectedChangeGuidance(snapshot, p, prior)
		return prior
	}
	r := rejected(snapshot, p.ID, err)
	r.AttemptStatus = "rejected"
	answerPath := filepath.Join(s.Root, filepath.FromSlash(p.AnswerPath))
	r.NextAction = fmt.Sprintf("按具体错误修复 S4 交付或来源，再保存原答案 %s。冻结上游变化须按原阶段流程处理。", answerPath)
	var outputErr *runtime.TaskPlanningOutputSourceError
	if errors.As(err, &outputErr) {
		r.NextAction = outputErr.NextAction()
	}
	return r
}

func (s *Service) taskPlanningRejectedChangeGuidance(snapshot runtime.Snapshot, p workpacket.TaskPlanningPacket, prior Receipt) string {
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	if prior.ResponsibilityStatus == "replaced" {
		return "原 S4 接收已被替换；按当前监听给出的责任与准确问卷继续，不恢复或重交此旧卷。"
	}
	if life["state"] == "planning" && life["phase"] == "tasks" && number(life["phase_revision"]) == p.Binding.StageEntry {
		guidance := "仍在当前 S4：若仅修正 TASK 或派发索引且 REQ/S2/S3 不变，Main 使用 " + PlanningRecoveryCommand(s.Root, "rework") + " --target tasks --reason '修正已接收的 TASK 或派发索引' 开启新入口，并将 reason 改为具体规划修正。冻结上游变化回其所属责任。若只是误改答卷，无需重交或另补答案，也无需先恢复答卷字节。"
		if prior.ConsumerPending {
			// This is the original durable projection, including its concrete
			// recover command and outlet cause, never a prior rejected attempt.
			guidance += "继续已持久的原接续：" + prior.NextAction
		}
		return guidance
	}
	if life["state"] == "document_verification" {
		return "当前已进入 S5；沿原两路独立审查继续，需改材料时由真实发现进入原文档返工责任，不重交旧 S4 答卷。"
	}
	return "按当前监听给出的责任与准确问卷继续；后续材料变化沿当前阶段的原返工责任处理，不恢复或重交此旧卷。"
}

func (s *Service) validateTaskPlanningChecks(state map[string]any, p workpacket.TaskPlanningPacket) error {
	bound, _ := state["bound_req"].(map[string]any)
	if text(bound["id"]) != p.Binding.Requirement.ID {
		return fmt.Errorf("S4 original checker REQ binding differs")
	}
	result, err := semantic.TasksCheckWithFiles(s.Root, s.Files, p.Binding.Requirement.ID)
	if err != nil {
		return fmt.Errorf("tasks_checked: %w", err)
	}
	if result.Tasks == 0 || len(result.Problems) != 0 {
		return fmt.Errorf("tasks_checked: %d TASKs, problems: %v", result.Tasks, result.Problems)
	}
	return nil
}

func (s *Service) taskPlanningReceipt(snap runtime.Snapshot, p workpacket.TaskPlanningPacket) (Receipt, error) {
	r := emptyReceipt(snap, p.ID)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil {
		return r, err
	}
	if !found {
		r := rejected(snap, p.ID, fmt.Errorf("S4 answer not received"))
		r.NextAction = "按具体原因修复后保存原答卷 " + filepath.Join(s.Root, p.AnswerPath) + "。" + PlanningDiagnostic(s.Root, snap.State, "reception")
		return r, nil
	}
	r.SubmissionKey = record.Key
	r.Accepted = true
	r.WorkComplete = true
	r.ResponsibilityStatus = "accepted"
	r.DomainRefs = []string{record.EvidenceID}
	r.NextAction = "S4 本人任务拆分交付已接收；原 TR-002 门由系统继续处理。接收不代表 S5 审查通过。"
	if baseline, ok := snap.State["baseline"].(map[string]any); !ok || snap.State["runtime_id"] != p.Binding.RuntimeID || number(baseline["generation"]) != p.Binding.Generation {
		r.WorkComplete = false
		r.ResponsibilityStatus = "replaced"
	}
	life, _ := snap.State["lifecycle"].(map[string]any)
	if life["state"] == "planning" && (life["phase"] != "tasks" || number(life["phase_revision"]) != p.Binding.StageEntry) {
		r.WorkComplete = false
		r.ResponsibilityStatus = "replaced"
	}
	if record.ControlStatus == "replaced" || ((record.ControlStatus == "pending" || record.ControlStatus == "no_transition") && (life["state"] != "planning" || life["phase"] != "tasks" || number(life["phase_revision"]) != p.Binding.StageEntry)) {
		r.WorkComplete = false
		r.ResponsibilityStatus = "replaced"
	}
	for _, ev := range evidenceRows(snap.State) {
		if ev["id"] == record.EvidenceID && ev["status"] == "invalid" {
			r.WorkComplete = false
			r.ResponsibilityStatus = "replaced"
			break
		}
	}
	if !r.WorkComplete {
		r.NextAction = "这是旧代或已替换的 S4 接收记录；按当前监听给出的问卷继续，不重交此旧卷。"
	} else if r.WorkComplete && (record.ControlStatus == "pending" || record.ControlStatus == "no_transition") && life["state"] == "planning" && life["phase"] == "tasks" && number(life["phase_revision"]) == p.Binding.StageEntry {
		r.ConsumerOwner, r.ConsumerPending = "main", true
		r.NextAction = "S4 本人交付已接收，可以结束本责任；阶段出口接续尚未完成，Main 检查原 TR-002 门与 system_followup " + record.FollowupID + " 的具体缺项。真实材料修复后使用 " + PlanningRecoveryCommand(s.Root, "recover") + " 接续原出口，不重交已接收答案，也不制造工具事件等待推进。"
		r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: record.FollowupID})
	} else if record.ControlStatus == "advanced" {
		r.NextAction = "S4 本人交付已接收，TR-002 已推进至 S5；后续由 S5 独立审查。"
	}
	if r.ConsumerPending {
		r.NextAction += PlanningDiagnostic(s.Root, snap.State, "outlet")
	}
	return r, nil
}

func (s *Service) TaskPlanningReceipt(path, packetID string) (Receipt, error) {
	snap, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	var p workpacket.TaskPlanningPacket
	var found bool
	if packetID != "" {
		p, found, err = runtime.FindTaskPlanningPacket(snap.State, packetID)
	} else {
		p, found, err = s.TaskPlanningForPath(path)
	}
	if err != nil || !found {
		return Receipt{}, fmt.Errorf("no original S4 issuance")
	}
	return s.taskPlanningReceipt(snap, p)
}

// Keep reception, transition and its diagnostic distinct. Prefer the original
// Controller's snapshot; if a later read fails, retain the last known facts and
// name the uncertainty. Observation never receives a second answer.
func (s *Service) taskPlanningControlReceipt(origin runtime.Snapshot, p workpacket.TaskPlanningPacket, result controller.ControlResult, controlErr error) (Receipt, error) {
	RecordPlanningDiagnostic(s.Root, origin.State, "reception", "")
	latest := origin
	var snapshotErr error
	if result.Snapshot.State != nil {
		latest = result.Snapshot
	} else {
		var observed runtime.Snapshot
		observed, snapshotErr = s.snapshot()
		if snapshotErr == nil {
			latest = observed
		}
	}
	r, err := s.taskPlanningReceipt(latest, p)
	if err != nil {
		return r, err
	}
	message := ""
	if controlErr != nil {
		message = controlErr.Error()
	}
	if result.Error != "" {
		message += "；" + result.Error
	}
	if !result.QualityGate.TransitionCommitted && len(result.QualityGate.Missing)+len(result.QualityGate.Conflicts) > 0 {
		message += fmt.Sprintf("；原门 %s：缺项 %v；冲突 %v", result.QualityGate.GateID, result.QualityGate.Missing, result.QualityGate.Conflicts)
	}
	if snapshotErr != nil {
		message += "；最终 Runtime 读取未完成：" + snapshotErr.Error() + "；当前阶段仅投影最后已知事实，原事务状态需具名恢复核实"
	}
	if message != "" {
		r.NextAction = "S4 答卷已接收；原 TR-002 系统接续需检查。具体原因：" + message + "。修复后使用 " + PlanningRecoveryCommand(s.Root, "recover") + "；保留已接收答案，无需重交。"
		RecordPlanningDiagnostic(s.Root, origin.State, "outlet", r.NextAction)
	} else if result.QualityGate.TransitionCommitted {
		RecordPlanningDiagnostic(s.Root, origin.State, "outlet", "")
	}
	return r, nil
}
