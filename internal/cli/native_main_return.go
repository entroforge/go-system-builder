package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Main projects original receipts and may continue an already retained S9 source.
// S7/S9 natural observations and Stop fallback use the original receiver;
// named Main recovery reuses it. Other stages keep their own event boundaries.
// This projection adds no actor mapping or ACK.
type nativeReturnSubject struct {
	kind, id, ref string
	record        *assignmentrecord.Record
	targeted      *repairTargetedSelection
}

func nativeMainReturnHook(root string, in policy.Input, out io.Writer) (bool, int) {
	if in.Event != "PostToolUse" || (in.ToolName != "Agent" && in.ToolName != "Task") || !mainQuestionnaireViewer(in) {
		return false, 0
	}
	if cwd, err := workspace.Canonical(in.CWD); err != nil || cwd != root {
		return false, 0
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); err != nil {
		return false, 0
	}
	snap, err := nativeRoutingSnapshot(root, in)
	if err != nil {
		return false, 0
	}
	selectSubjects := func(text string) []nativeReturnSubject {
		var selected []nativeReturnSubject
		for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
			kind := stringValue(row["template_id"])
			switch kind {
			case "s5-document-review-v1", "s7-review-result-v1", "s8-hypothesis-result-v1", "s9-plan-report-v1", "s9-repair-result-v1":
			default:
				continue
			}
			id, view, answer := stringValue(row["packet_id"]), stringValue(row["view_path"]), stringValue(row["answer_path"])
			refs := []string{view, answer}
			if kind == "s9-plan-report-v1" {
				brief, _, _ := nativeS9Paths(workpacket.RepairPlanReportPacket{ID: id, ViewPath: view})
				refs = append(refs, brief)
			}
			for _, ref := range refs {
				if ref != "" && nativeS6ExactReference(text, filepath.Join(root, ref)) {
					selected = append(selected, nativeReturnSubject{kind: kind, id: id, ref: filepath.Join(root, ref)})
					break
				}
			}
		}
		// Do not filter by the new lifecycle: the returned work may just have
		// advanced that lifecycle, while its original reception remains readable.
		if records, err := assignmentrecord.RecordsFromState(snap.State); err == nil {
			for _, r := range records {
				if r.Contract != assignmentrecord.NativeDeliveryContract {
					continue
				}
				brief, _, _ := assignmentrecord.NativePaths(r)
				if nativeS6ExactReference(text, filepath.Join(root, brief)) {
					r := r
					selected = append(selected, nativeReturnSubject{kind: "S6", id: r.AssignmentID, ref: filepath.Join(root, brief), record: &r})
				}
			}
		}
		if contextForHook(in).Err() == nil && stringValue(repairPointerCLI(snap.State)["impact_ref"]) != "" {
			if selections, err := repairTargetedSelections(root, snap.State); err == nil {
				for _, selection := range selections {
					ref, err := repairTargetedQuestionnairePath(root, snap.State, selection.performingAssignmentID)
					if err == nil && nativeS6ExactReference(text, ref) {
						selection := selection
						selected = append(selected, nativeReturnSubject{kind: "Targeted", id: selection.performingAssignmentID, ref: ref, targeted: &selection})
					}
				}
			}
		}
		return selected
	}
	prompt, _ := in.ToolInput["prompt"].(string)
	selected := selectSubjects(prompt)
	issuedSelection := len(selected) == 1
	if len(selected) == 0 {
		text, _ := in.ToolResponse["text"].(string)
		switch content := in.ToolResponse["content"].(type) {
		case string:
			text += "\n" + content
		case []any:
			for _, raw := range content {
				part, _ := raw.(map[string]any)
				if part["type"] == "text" {
					value, _ := part["text"].(string)
					text += "\n" + value
				}
			}
		}
		selected = selectSubjects(text)
	}
	if len(selected) == 0 {
		return false, 0
	}
	message := "Native return has multiple exact responsibility references; Main must resolve the handoff. No answer was received by this observation."
	if len(selected) == 1 {
		subject := selected[0]
		execution := nativeExecutionFromResponse(in.ToolResponse)
		ctx := context.WithValue(contextForHook(in), nativeExecutionReturnKey{}, execution)
		continuation := ""
		if issuedSelection {
			var attempted bool
			var continueErr error
			snap, attempted, continueErr = continueNativeS9MainReturn(ctx, root, in, snap, subject)
			if attempted {
				continuation = "System continued the original S9 consumer. "
				if continueErr != nil {
					continuation = "Original S9 consumer continuation remains unconfirmed: " + continueErr.Error() + ". Preserve original professional input; no additional author save or Stop is requested. "
				}
			}
		}
		summary, err := nativeReturnSummaryContext(ctx, root, snap, subject)
		if err != nil {
			summary = "Reception summary could not be refreshed; this observation establishes no new reception fact. Main preserves this exact return and uses the original receipt/Stop diagnostic when the request is available: " + err.Error()
		}
		observation := "Native return for "
		if execution != nativeExecutionCompleted {
			observation = "Native execution observation for "
			summary = nativeExecutionPendingAction(execution) + " Original reception facts: " + summary
		}
		message = observation + subject.ref + ". " + continuation + summary + " This reception summary does not establish product acceptance. Do not re-dispatch received work merely because Main or the next stage still has work."
	}
	if err := json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": message}}); err != nil {
		return true, 1
	}
	return true, 0
}

func nativeReturnSummary(root string, snap runtime.Snapshot, subject nativeReturnSubject) (string, error) {
	return nativeReturnSummaryContext(context.Background(), root, snap, subject)
}

func nativeReturnSummaryContext(ctx context.Context, root string, snap runtime.Snapshot, subject nativeReturnSubject) (string, error) {
	if strings.HasPrefix(subject.kind, "s9-") {
		return nativeS9MainReturnSummary(ctx, root, snap, subject, nil)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ctx = runtime.WithPacketBodyRoot(runtime.WithReviewResultReadCache(ctx), root)
	if subject.record != nil {
		r := *subject.record
		agent := contextAgent(snap.State, r.OwnerAgentID)
		current, found, err := assignmentrecord.ForOwner(snap.State, r.OwnerAgentID)
		if err != nil || !found || current.Reference() != r.Reference() || r.RuntimeID != snap.State["runtime_id"] || r.BaselineGeneration != workspace.Generation(snap.State) {
			return "This exact TASK reference is historical or replaced; Main preserves its history and follows the current responsibility without resuming this old revision.", nil
		}
		if workstate.AgentActivity(agent) == "cancelled" || workstate.AgentActivity(agent) == "stopped" {
			return "Original TASK is closed; Main preserves its history without re-dispatching it.", nil
		}
		ref := stringValue(agent["completion_reported_ref"])
		if ref == "" {
			if !nativeExecutionHasReturn(ctx) {
				return "No original TASK delivery reference is recorded. Preserved prior attempt context: " + submission.PlanningDiagnosticContext(ctx, root, snap.State, nativeS6DiagnosticKind(r)) + ". " + nativeExecutionPendingAction(nativeExecutionInContext(ctx)), nil
			}
			return nativeS6ReturnDiagnostic(root, snap.State, r), nil
		}

		return nativeS6RecordGuidance(root, snap.State, r) + " This projects the recorded reference; original task-receipt/integration checks remain authoritative.", nil
	}
	if subject.targeted != nil {
		var records []string
		for _, ref := range repairArtifactRefs(repairPointerCLI(snap.State), "targeted_reverification_artifacts", "", "") {
			var value repair.TargetedReverification
			if err := repairLeadIndexedSource(root, ref, &value); err != nil {
				return "", err
			}
			if value.ImpactID == subject.targeted.impact.ImpactID && value.OriginalAssignmentID == subject.targeted.originalAssignmentID && value.PerformingAssignmentID == subject.id {
				records = append(records, filepath.Join(root, ref.Path)+" result="+value.Result)
			}
		}
		if len(records) == 0 {
			diagnostic, _ := nativeS9ReturnDiagnosticRecordContext(ctx, root, snap, subject.ref)
			if !nativeExecutionHasReturn(ctx) {
				return "No original Targeted reception is recorded. Preserved historical attempt context: " + diagnostic.historicalMessage() + ". " + nativeExecutionPendingAction(nativeExecutionInContext(ctx)), nil
			}
			if available, reason := s9OriginalInputAvailable(ctx, subject.ref); !available {
				return diagnostic.historicalMessage() + " No original Targeted reception is recorded. Main owns the missing original input/source at " + subject.ref + ": " + reason + ". Preserve the original independent responsibility and accepted facts; no supported saved input is established for replay.", nil
			}
			consumerAttempt := diagnostic.Attempt
			if diagnostic.ConsumerFailure != nil {
				consumerAttempt = diagnostic.ConsumerFailure.Attempt
			}
			if consumerAttempt != nil && consumerAttempt.FailureKind == "author" {
				return diagnostic.historicalMessage() + " Main continues the original independent verifier only to correct the specific professional omission in " + subject.ref + ".", nil
			}
			return diagnostic.historicalMessage() + " No original Targeted reception recorded. Main restores the concrete original prerequisite, then continues the original independent consumer: " + s9OriginalConsumeCommand(root, subject.ref) + ". No repeated verifier Stop or observation is required for system recovery.", nil
		}
		diagnostic, found := nativeS9ReturnDiagnosticRecordContext(ctx, root, snap, subject.ref)
		completion := ""
		if found && diagnostic.Attempt != nil && diagnostic.Attempt.Stage == "targeted_received" {
			completion = diagnostic.historicalMessage()
		}
		return completion + " Original Targeted records: " + strings.Join(records, "; ") + ". " + repairHandoffProjectionContext(root, snap.State), nil
	}
	s7Diagnostic := ""
	if subject.kind == "s7-review-result-v1" {
		s7Diagnostic = submission.PlanningDiagnosticContext(ctx, root, snap.State, nativeS7DiagnosticKind(subject.id))
	}
	s, err := submission.NewContext(ctx, root, true)
	if err != nil {
		if subject.kind == "s7-review-result-v1" {
			return nativeS7MainReceiptGap(ctx, root, snap, subject, s7Diagnostic, err), nil
		}
		return "", err
	}
	var receipt submission.Receipt
	prefix := ""
	switch subject.kind {
	case "s5-document-review-v1":
		p, _, e := runtime.FindWorkPacket(snap.State, subject.id)
		if e != nil {
			return "", e
		}
		receipt, err = s.ReceiptPacket(p.Binding.Owner, p.ID)
		if err == nil && !receipt.Accepted {
			if nativeExecutionHasReturn(ctx) {
				prefix, receipt.NextAction = nativeS5ReturnDiagnostic(root, snap.State, p)
			} else {
				prefix = submission.PlanningDiagnosticContext(ctx, root, snap.State, nativeS5DiagnosticKind(p.ID))
			}
		}
	case "s7-review-result-v1":
		// Preserve the original attempt diagnostic even if the independent
		// receipt read fails. A failed query proves neither receipt state.
		prefix = s7Diagnostic
		p, _, e := runtime.FindReviewResultPacketContext(ctx, snap.State, subject.id)
		if e != nil {
			return nativeS7MainReceiptGap(ctx, root, snap, subject, prefix, e), nil
		}
		receipt, err = s.ReviewResultReceipt(p.Binding.OwnerAgentID, p.ID)
		if err != nil {
			return nativeS7MainReceiptGap(ctx, root, snap, subject, prefix, err) + " Original receipt query: `" + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime stage-answer receipt --root " + s7CaptureShellQuote(root) + " --agent " + s7CaptureShellQuote(p.Binding.OwnerAgentID) + " --packet " + s7CaptureShellQuote(p.ID) + "`.", nil
		}
		if err == nil && receipt.Accepted {
			receipt.NextAction += nativeS7AcceptedOutletForRequest(ctx, root, snap, p)
		}
		if err == nil && !receipt.Accepted {
			current, currentErr := runtime.ProjectCurrentReviewResultPacketIndex(snap.State, p.Binding.OwnerAgentID)
			if currentErr != nil {
				return prefix + " The original receipt query did not establish acceptance, but current responsibility membership could not be verified: " + currentErr.Error() + ". Main restores the original responsibility index; this does not establish that the exact questionnaire is historical or replaced.", nil
			}
			isCurrent := false
			for _, candidate := range current {
				isCurrent = isCurrent || candidate.ID == p.ID
			}
			if !isCurrent {
				return "This exact S7 questionnaire is historical or replaced; preserve its history and follow the current responsibility without resuming this old revision.", nil
			}
			if e := ctx.Err(); e != nil {
				return "", e
			}
			if nativeExecutionHasReturn(ctx) {
				prefix = nativeS7IndexReturnDiagnosticContext(ctx, root, snap.State, runtime.ReviewResultPacketIndex{ID: p.ID, Binding: p.Binding, ViewPath: p.ViewPath, AnswerPath: p.AnswerPath})
			} else {
				prefix = submission.PlanningDiagnosticContext(ctx, root, snap.State, nativeS7DiagnosticKind(p.ID))
			}
		}

	case "s8-hypothesis-result-v1":
		p, _, e := runtime.FindHypothesisResultPacket(snap.State, subject.id)
		if e != nil {
			return "", e
		}
		receipt, err = s.HypothesisResultReceipt(p.Binding.OwnerAgentID, p.ID)
		if err == nil && !receipt.Accepted {
			if diagnostic := submission.PlanningDiagnostic(root, snap.State, "S8 native Stop "+p.ID); diagnostic != "" {
				prefix = diagnostic + ". This describes the last attempted reception, not a new validation or an accepted result. "
			}
		}
	}
	if err != nil {
		return "", err
	}
	if !receipt.Accepted && subject.kind == "s8-hypothesis-result-v1" {
		receipt.NextAction = "后台派发即时返回不代表执行结束；先核对实际原生执行。 " + receipt.NextAction
	} else if !receipt.Accepted && subject.kind != "s5-document-review-v1" {
		receipt.NextAction = "尚无原接收事实；后台派发即时返回不代表执行结束。Main 继续可开展的工作，核对实际原生执行与 Stop 诊断，不凭此次返回猜测重派或要求重填。"
		if subject.kind == "s7-review-result-v1" {
			receipt.NextAction += " S7 实际未完成返回仍属于原责任；Main 阅读实际返回和 Stop 的具名作者、原答案路径及具体字段/来源诊断，恢复来源或环境前提，或续跑原 Reviewer 修正专业缺项。已记录 site_lost 时先用原 blocker_resolved 入口解除真实阻塞，再由同一 Reviewer 恢复。不能把已结束但未接收的责任当作仍在后台运行，也不凭 prose 推定验收、重派或编造 verdict/Finding。"
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !receipt.Accepted && !nativeExecutionHasReturn(ctx) {
		if subject.kind == "s5-document-review-v1" {
			prefix += " S5责任边界：独立性或来源阻塞属于Main；专业答案缺项仍属于原专业责任。本次执行观察不分配新的恢复、重派或续跑动作。 "
		}
		receipt.NextAction = nativeExecutionPendingAction(nativeExecutionInContext(ctx))
	}
	data, err := json.Marshal(receipt)
	return prefix + string(data), err
}

// This is a read failure, never a submission outcome. Keep any independently
// captured submission diagnostic alongside the unknown current receipt.
func nativeS7MainReceiptGap(ctx context.Context, root string, snap runtime.Snapshot, subject nativeReturnSubject, diagnostic string, cause error) string {
	if diagnostic == "" {
		diagnostic = submission.PlanningDiagnosticContext(ctx, root, snap.State, nativeS7DiagnosticKind(subject.id))
	}
	return diagnostic + " S7 current receipt is unknown for packet " + subject.id + " at " + subject.ref + ": " + cause.Error() + ". This independent read failure establishes neither acceptance nor non-acceptance and does not replace the original submission diagnostic. Main restores the exact original packet/source and verifies its original ReviewResult receipt; preserve any already recorded submission. If that receipt is verified, Main resumes only the pending original outlet with runtime review-continue, or runtime audit-prepare when TR-009 has already entered S10. No author correction, additional save, Stop or re-dispatch is requested for this system recovery."
}

// Project both original receipts, so a Result-source failure cannot erase an
// already accepted Plan. This is still a query, never an answer receiver.
func nativeS9MainReturnSummary(ctx context.Context, root string, snap runtime.Snapshot, subject nativeReturnSubject, service *submission.Service) (string, error) {
	var binding workpacket.RepairPlanReportBinding
	var resultID string
	matched := false
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if row["packet_id"] != subject.id || row["template_id"] != subject.kind {
			continue
		}
		if matched {
			return "", fmt.Errorf("multiple exact S9 responsibility indexes")
		}
		matched = true
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return "", err
		}
		if subject.kind == "s9-repair-result-v1" {
			var result workpacket.RepairResultBinding
			if err := json.Unmarshal(data, &result); err != nil {
				return "", err
			}
			binding = result.Sources
			resultID = subject.id
		} else if err := json.Unmarshal(data, &binding); err != nil {
			return "", err
		}
	}
	if !matched {
		return "Original S9 responsibility index is unavailable; Main preserves exact return " + subject.ref + " and restores the original source.", nil
	}
	handoff := s9HandoffForBinding(ctx, root, snap, binding, resultID)
	prefix := handoff.summary(root) + " "
	if err := ctx.Err(); err != nil {
		return prefix + "Current receipt is unknown: " + err.Error(), nil
	}
	if service == nil {
		var err error
		service, err = submission.NewContext(ctx, root, true)
		if err != nil {
			return prefix + "Original receipt is unavailable; Main preserves known responsibility and restores its original source: " + err.Error(), nil
		}
	}
	var plan submission.Receipt
	var planErr error
	if handoff.Plan.id != "" {
		plan, planErr = service.RepairPlanReportReceipt(binding.OwnerAgentID, handoff.Plan.id)
	} else {
		planErr = fmt.Errorf("original PlanReport index is unavailable")
	}
	if planErr != nil {
		prefix += "Original PlanReport receipt is unavailable; preserve all independently recorded Result facts: " + planErr.Error() + ". "
	}
	if resultID == "" && handoff.Result != nil {
		resultID = handoff.Result.id
	}
	if resultID == "" {
		if plan.Accepted {
			prefix += "Only the PlanReport receipt is accepted, not the Builder Result. "
		} else if planErr == nil {
			prefix += "Neither PlanReport nor Builder Result is accepted. "
		}
		return prefix + s9UnreceivedActionContext(ctx, root, handoff), nil
	}
	if err := ctx.Err(); err != nil {
		return prefix + "Current Result receipt is unknown: " + err.Error(), nil
	}
	receipt, err := service.RepairResultReceipt(binding.OwnerAgentID, resultID)
	if err != nil {
		return prefix + "Original Result receipt is unavailable; Main preserves accepted PlanReport and restores the exact original source: " + err.Error(), nil
	}
	if !receipt.Accepted {
		if plan.Accepted {
			prefix += "Only the PlanReport receipt is accepted, not the Builder Result. "
		} else if planErr == nil {
			prefix += "Neither PlanReport nor Builder Result is accepted. "
		}
		receipt.NextAction = s9UnreceivedActionContext(ctx, root, handoff)
	} else if receipt.ResponsibilityStatus == "candidate_accepted" && receipt.WorkComplete {
		assignment := binding.ManifestAssignmentID
		if handoff.Result != nil && handoff.Result.executionAssignment != "" {
			assignment = handoff.Result.executionAssignment
		}
		receipt.NextAction = s9CandidateAction(root, assignment, binding.OwnerAgentID)
	} else if receipt.ResponsibilityStatus == "main_result_accepted" {
		receipt.NextAction = s9FinalAction()
	}
	if err := ctx.Err(); err != nil {
		return prefix + "Current receipt projection interrupted: " + err.Error(), nil
	}
	data, err := json.Marshal(receipt)
	return prefix + string(data), err
}
