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
	"github.com/entroforge/go-system-builder/internal/review"
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
	conflict      bool
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
	selectSubjects := func(text string, s9PromptAliases bool) ([]nativeReturnSubject, bool) {
		var selected []nativeReturnSubject
		var reviewCandidates []nativeReviewCandidate
		for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
			kind := stringValue(row["template_id"])
			switch kind {
			case "s5-document-review-v1", "s7-review-result-v1", "s8-hypothesis-result-v1", "s9-plan-report-v1", "s9-repair-result-v1":
			default:
				continue
			}
			id, view, answer := stringValue(row["packet_id"]), stringValue(row["view_path"]), stringValue(row["answer_path"])
			refs := []string{view, answer}
			binding, _ := row["binding"].(map[string]any)
			if (kind == "s7-review-result-v1" || kind == "s8-hypothesis-result-v1") && s9PromptAliases {
				stage := "S7"
				if kind == "s8-hypothesis-result-v1" {
					stage = "S8"
				}
				paths := []string{view, answer, filepath.Join(root, view), filepath.Join(root, answer)}
				reception := mapFieldCLI(row, "review_result_reception")
				source := stringValue(reception["execution_root"])
				if source == "" {
					source = stringValue(mapFieldCLI(reception, "receipt")["execution_root"])
				}
				if source != "" {
					paths = append(paths, filepath.Join(source, view), filepath.Join(source, answer))
				}
				reviewCandidates = append(reviewCandidates, nativeReviewCandidate{stage: stage, packet: id, owner: stringValue(binding["owner_agent_id"]), hypothesis: stringValue(binding["hypothesis_id"]), assignment: stringValue(binding["assignment_id"]), claims: stringSliceAny(binding["claim_ids"]), answer: filepath.Join(root, answer), refs: paths})
				continue
			}
			conflict := false
			var s9Brief string
			if kind == "s9-plan-report-v1" {
				s9Brief = stringValue(row["brief_path"])
				if s9Brief != "" {
					refs = append(refs, s9Brief)
				}
			}
			for _, ref := range refs {
				matches := ref != "" && nativeS6ExactReference(text, filepath.Join(root, ref))
				if kind == "s8-hypothesis-result-v1" {
					matches = nativeS8TaskPath(text, root, filepath.Join(root, ref))
				}
				if s9PromptAliases && s9Brief != "" && ref == s9Brief {
					matches = nativeS9ResponsibilityMatch(text, root, ref)
				}
				if matches {
					selected = append(selected, nativeReturnSubject{kind: kind, id: id, ref: filepath.Join(root, ref), conflict: conflict})
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
					matches := err == nil && nativeS6ExactReference(text, ref)
					if err == nil && s9PromptAliases {
						matches = nativeS9RegisteredResponsibilityMatch(text, root, ref)
					}
					if matches {
						selection := selection
						selected = append(selected, nativeReturnSubject{kind: "Targeted", id: selection.performingAssignmentID, ref: ref, targeted: &selection})
					}
				}
			}
		}
		unknown := false
		if s9PromptAliases {
			var chosen []nativeReviewCandidate
			chosen, unknown = nativeReviewSelect(nativeReviewFacts(text), text, root, reviewCandidates)
			for _, c := range chosen {
				kind := "s7-review-result-v1"
				if c.stage == "S8" {
					kind = "s8-hypothesis-result-v1"
				}
				selected = append(selected, nativeReturnSubject{kind: kind, id: c.packet, ref: c.answer})
			}
		}
		return selected, unknown
	}
	prompt, _ := in.ToolInput["prompt"].(string)
	selected, unknown := selectSubjects(prompt, true)
	issuedSelection := len(selected) > 0
	if len(selected) == 0 && !unknown {
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
		selected, _ = selectSubjects(text, false)
		// A Worker's returned text can quote another professional's path; it
		// does not create a new S7/S8 platform dispatch or transfer ownership.
		filtered := selected[:0]
		for _, subject := range selected {
			if subject.kind != "s7-review-result-v1" && subject.kind != "s8-hypothesis-result-v1" {
				filtered = append(filtered, subject)
			}
		}
		selected = filtered
	}
	if len(selected) == 0 && !unknown {
		return false, 0
	}
	if len(selected) > 1 {
		for _, subject := range selected {
			if subject.kind != "s7-review-result-v1" && subject.kind != "s8-hypothesis-result-v1" {
				selected, unknown = nil, true
				break
			}
		}
	}
	var messages []string
	for _, subject := range selected {
		execution := nativeExecutionFromResponse(in.ToolResponse)
		ctx := context.WithValue(contextForHook(in), nativeExecutionReturnKey{}, execution)
		continuation := ""
		if issuedSelection {
			var attempted bool
			var continueErr error
			if subject.kind == "s7-review-result-v1" || subject.kind == "s8-hypothesis-result-v1" {
				snap, attempted, continueErr = continueNativeReviewMainReturn(ctx, root, in, snap, subject)
			} else if subject.targeted != nil {
				snap, attempted, continueErr = continueNativeTargetedMainReturn(ctx, root, in, snap, subject)
			} else {
				snap, attempted, continueErr = continueNativeOriginalS9MainReturn(ctx, root, in, snap, subject)
			}
			if attempted {
				continuation = "System continued the original professional consumer. "
				if continueErr != nil {
					continuation = "Original professional consumer continuation remains unconfirmed: " + continueErr.Error() + ". Preserve original professional input; no additional author save or Stop is requested. "
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
		messages = append(messages, observation+subject.ref+". "+continuation+summary+" This reception summary does not establish product acceptance. Do not re-dispatch received work merely because Main or the next stage still has work.")
	}
	if unknown {
		messages = append(messages, "Some original responsibilities remain unknown; Main inspects the original dispatch and preserves any accepted results.")
	}
	if err := json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": strings.Join(messages, "\n")}}); err != nil {
		return true, 1
	}
	return true, 0
}

func continueNativeTargetedMainReturn(ctx context.Context, root string, in policy.Input, snap runtime.Snapshot, subject nativeReturnSubject) (runtime.Snapshot, bool, error) {
	if ctx.Err() != nil || subject.targeted == nil || nativeExecutionFromResponse(in.ToolResponse) != nativeExecutionCompleted || in.Error != "" || in.ToolResponse["is_error"] == true {
		return snap, false, nil
	}
	if err := receiveNativeS9Targeted(ctx, root, snap, subject.ref, io.Discard); err != nil {
		return snap, true, err
	}
	if cached, ok := ctx.Value(nativeRoutingKey{}).(*nativeRoutingRead); ok {
		*cached = nativeRoutingRead{root: root}
	}
	current, err := nativeRoutingSnapshot(root, in)
	return current, true, err
}

// A real completed Main return may observe the same issued S7/S8 original
// answer if the Worker's own natural events have not yet received it. The
// prompt selected the exact issued packet; no platform actor or extra author
// return reference is needed, and asynchronous launch is never a delivery.
func continueNativeReviewMainReturn(ctx context.Context, root string, in policy.Input, snap runtime.Snapshot, subject nativeReturnSubject) (runtime.Snapshot, bool, error) {
	if ctx.Err() != nil || nativeExecutionFromResponse(in.ToolResponse) != nativeExecutionCompleted || in.Error != "" || in.ToolResponse["is_error"] == true {
		return snap, false, nil
	}
	service, err := submission.NewContext(ctx, root, true)
	if err != nil {
		return snap, true, err
	}
	switch subject.kind {
	case "s7-review-result-v1":
		packet, found, err := runtime.FindReviewResultPacketContext(runtime.WithPacketBodyRoot(ctx, root), snap.State, subject.id)
		if err != nil || !found {
			if err == nil {
				err = fmt.Errorf("original S7 packet %s is unavailable", subject.id)
			}
			return snap, true, err
		}
		receipt, err := service.ReviewResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
		if err != nil {
			return snap, true, err
		}
		if !receipt.Accepted || !receipt.WorkComplete {
			source, err := review.IssuedResultSourceRoot(root, snap.State, packet)
			if err != nil {
				return snap, true, err
			}
			if _, err := service.SaveReviewResultFrom(ctx, packet.Binding.OwnerAgentID, filepath.Join(source, packet.AnswerPath), source, nil); err != nil {
				return snap, true, err
			}
		}
	case "s8-hypothesis-result-v1":
		packet, found, err := runtime.FindHypothesisResultPacket(snap.State, subject.id)
		if err != nil || !found {
			if err == nil {
				err = fmt.Errorf("original S8 packet %s is unavailable", subject.id)
			}
			return snap, true, err
		}
		receipt, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
		if err != nil {
			return snap, true, err
		}
		if !receipt.Accepted || !receipt.WorkComplete {
			source, err := questionnaireSourceRoot(root, snap.State, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
			if err != nil {
				return snap, true, err
			}
			if _, err := service.ObserveHypothesisResultFrom(ctx, packet.Binding.OwnerAgentID, filepath.Join(source, packet.AnswerPath), source, nil, true); err != nil {
				return snap, true, err
			}
		}
	default:
		return snap, false, nil
	}
	if cached, ok := ctx.Value(nativeRoutingKey{}).(*nativeRoutingRead); ok {
		*cached = nativeRoutingRead{root: root}
	}
	current, err := nativeRoutingSnapshot(root, in)
	if err != nil {
		return snap, true, err
	}
	if subject.kind == "s7-review-result-v1" && s7SuccessorPending(root, current.State) {
		applies, compareErr := reviewPacketAppliesToCurrent(root, current.State, subject.id, ctx)
		if compareErr != nil {
			return current, true, fmt.Errorf("original S7 answer is retained; its relationship to the current responsibility is unknown: %w", compareErr)
		}
		if applies {
			current, _, err = continueS7AcceptanceResult(ctx, root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), current)
		}
	}
	return current, true, err
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
		facts, err := repair.LoadTargetedReceiptFacts(ctx, root, repairPointerCLI(snap.State), subject.id, false)
		if err != nil {
			return "", err
		}
		for _, fact := range facts.Rows {
			records = append(records, filepath.Join(root, fact.Ref.Path)+" result="+fact.Result)
		}
		if len(records) == 0 && len(facts.Unknown) > 0 {
			return "Accepted historical Targeted references exist, but the unavailable original report leaves their responsibility and conclusion unknown. The current responsibility " + subject.ref + " is not established as either received or unreceived. Main preserves those facts and handles the actual uncertainty; no additional author save or Stop is requested.", nil
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
		message := completion + " Original Targeted records: " + strings.Join(records, "; ") + ". These record their original conclusions and do not by themselves establish coverage of later changes. "
		if len(facts.Unknown) > 0 {
			message += "Other accepted historical references have an unknown association or conclusion; Main preserves them and handles the uncertainty. "
		}
		return message + repairHandoffProjectionContext(root, snap.State), nil
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
			current, currentErr := runtime.ProjectIssuedReviewResultPacketIndex(snap.State, p.Binding.OwnerAgentID)
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
		if err == nil && receipt.Accepted && len(p.LateResults) != 0 {
			receipt.NextAction += " 原 Investigator 晚到结果已收；本次接收未自动改变原 Case、既有批准或当前阶段。Main 查阅本人原结果并判断其影响，无需作者复送。"
		}
		if err == nil && !receipt.Accepted {
			if diagnostic := submission.PlanningDiagnostic(root, snap.State, "S8 native Stop "+p.ID); diagnostic != "" {
				prefix = diagnostic + ". This describes the last attempted reception, not a new validation or an accepted result. "
			} else if nativeExecutionInContext(ctx) == nativeExecutionCompleted {
				prefix = nativeS8CurrentAnswerDiagnostic(ctx, root, snap.State, p)
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

// A returned but unreceived original responsibility may be inspected without
// invoking its Writer. The result describes the current answer, not a lost
// historical Stop failure, and never changes the receipt or author work.
func nativeS8CurrentAnswerDiagnostic(ctx context.Context, root string, state map[string]any, packet workpacket.HypothesisResultPacket) string {
	if ctx.Err() != nil {
		return ""
	}
	owner := packet.Binding.OwnerAgentID
	source, err := questionnaireSourceRoot(root, state, packet.Binding.AssignmentID, owner, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
	if err != nil {
		return fmt.Sprintf("S8 current answer observation for owner %s: original source is unknown: %v. ", owner, err)
	}
	answer := filepath.Join(source, packet.AnswerPath)
	data, err := workpacket.ReadFile(source, packet.AnswerPath, 20<<20)
	if err != nil {
		return fmt.Sprintf("S8 current answer observation for owner %s, answer %s: current file is unavailable: %v. This is not a historical Stop diagnosis. ", owner, answer, err)
	}
	_, _, issues := workpacket.ParseHypothesisResultAnswer(packet, data)
	if len(issues) != 0 {
		return fmt.Sprintf("S8 current answer observation for owner %s, answer %s: %s. This is not a historical Stop diagnosis. ", owner, answer, (&workpacket.AnswerError{Label: "S8 answer", Fields: issues}).Error())
	}
	return fmt.Sprintf("S8 current answer observation for owner %s, answer %s: its current fields parse; the cause of the earlier unreceived return is unknown. This is not a historical Stop diagnosis. ", owner, answer)
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
