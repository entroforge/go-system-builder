package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func nativeReviewStage(state map[string]any) string {
	if !runtime.PacketEnabled(state) {
		return ""
	}
	life := mapFieldCLI(state, "lifecycle")
	switch life["state"] {
	case "document_verification":
		return "S5"
	case "verification":
		return "S7"
	case "acceptance":
		if stringValue(mapFieldCLI(mapFieldCLI(state, "review"), "plan")["status"]) == "clean" {
			return "S7" // Original accepted responsibility may still owe S10 materials.
		}
		return "S8" // Original Investigator work may still be in flight.
	case "bug_resolution":
		if life["phase"] == "investigation" || s9ProfessionalPhase(stringValue(life["phase"])) {
			return "S8"
		}
	case "release_audit", "awaiting_human_release", "release_authorized":
		return "S8" // Retain already issued professional responsibilities at S11.
	}
	if stringValue(mapFieldCLI(mapFieldCLI(state, "review"), "plan")["status"]) == "observation_sealed" {
		return "S7"
	}

	return ""
}

// This is only the platform Hook boundary for read-only professional work.
// Original packets and domain consumers own all responsibility and result facts;
// there is no native actor registry, launcher, queue, activation or new Result.
func nativeReviewHook(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	return nativeReviewHookStage(root, in, out, diagnostic, "")
}

func nativeReviewHookStage(root string, in policy.Input, out, diagnostic io.Writer, issuedStage string) (bool, int) {
	snap, err := nativeRoutingSnapshot(root, in)
	if err != nil {
		return false, 0
	}
	stage := nativeReviewStage(snap.State)
	if issuedStage != "" {
		stage = issuedStage
	}
	if stage == "" {
		return false, 0
	}
	ordinaryReview := (stage == "S7" || stage == "S8") && (in.Event == "PreToolUse" || in.Event == "PostToolUse")
	if in.AgentID == "Main" || in.AgentID == "" {
		if in.Event == "PostToolUse" && (in.ToolName == "Agent" || in.ToolName == "Task") {
			life := mapFieldCLI(snap.State, "lifecycle")
			if stage != "S5" && life["state"] != "verification" && (life["state"] != "bug_resolution" || life["phase"] != "investigation") {
				return false, 0
			}
			return true, 0
		}
		return false, 0
	}
	// S5 retains its document-review routing. S7/S8 always identify an
	// original responsibility by the same exact task and answer references.
	if stage == "S5" {
		if prior := contextAgent(snap.State, in.AgentID); stringValue(prior["activation_ref"]) != "" {
			return false, 0
		}
		for _, raw := range mapFieldCLI(mapFieldCLI(snap.State, "workspace"), "executions") {
			e, _ := raw.(map[string]any)
			if e["agent_id"] == in.AgentID {
				return false, 0
			}
		}
	}
	if ordinaryReview && in.Event == "PostToolUse" {
		failed, _ := in.ToolResponse["is_error"].(bool)
		if in.Error != "" || failed {
			life := mapFieldCLI(snap.State, "lifecycle")
			if stage == "S8" && (life["state"] != "bug_resolution" || life["phase"] != "investigation") {
				return false, 0
			}
			return true, 0
		}
	}
	if ordinaryReview {
		// Real ordinary payloads point transcript_path at Main's session. Only
		// this actor's verified sidechain transcript can identify its task.
		child, ok := nativeChildTranscript(in)
		if !ok {
			// Without this Worker's task, this receiver cannot claim an event
			// that may belong to another issued professional responsibility.
			return false, 0
		}
		in.AgentTranscriptPath = child
	}
	if in.Event != "SubagentStart" && in.Event != "SubagentStop" && !ordinaryReview {
		// S7/S8 do not consume other platform events: the original S9 and
		// general Hook routes still own their own feedback and lifecycle.
		if stage != "S5" {
			return false, 0
		}
		return true, 0
	}
	if engine, err := policy.Load(filepath.Join(root, "docs/control/hook-policy.json")); err == nil {
		observed := policy.Decision{Decision: "audit", RuleID: "native_review_lifecycle_observed", Reason: stage + " native professional lifecycle observed", Retry: "not_applicable"}
		auditStarted := time.Now()
		defer func() {
			observed.ElapsedMS = time.Since(auditStarted).Milliseconds()
			if err := appendHookAudit(contextForHook(in), root, engine.Envelope(in, observed, time.Now())); err != nil {
				fmt.Fprintln(diagnostic, err)
			}
		}()
	}
	if in.Event == "SubagentStart" && stage == "S5" {
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": stage + " native professional work: follow the exact original questionnaire view and answer references in Main's task prompt. Read its actual frozen subjects, perform your independent review/investigation and save the professional answer at the supplied path. Return that exact reference; SubagentStop calls the original consumer. Ordinary tools do not activate or receive work. Claude Code owns execution/resume; no Team, platform ID lookup, workspace launcher or generic report is required. Real domain approval and independence remain. Unrelated native work follows its own task; do not infer a formal responsibility from your role."}})
		return true, 0
	}
	var candidates []nativeReviewCandidate
	var sourceErr error
	selectAnswer := func(id, assignment, view, path string, generation int, runtimeID string, aliases ...string) {
		actual, err := questionnaireSourceRoot(root, snap.State, assignment, id, runtimeID, generation)
		refs := append([]string{filepath.Join(root, view), filepath.Join(root, path)}, aliases...)
		if err == nil {
			refs = append(refs, filepath.Join(actual, view), filepath.Join(actual, path))
		}
		candidates = append(candidates, nativeReviewCandidate{view: filepath.Join(actual, view), stage: stage, owner: id, answer: filepath.Join(actual, path), source: actual, refs: refs, err: err})
	}
	selectReviewAnswer := func(p runtime.ReviewResultPacketIndex) {
		packet := workpacket.ReviewResultPacket{ID: p.ID, Binding: p.Binding, AnswerPath: p.AnswerPath, ViewPath: p.ViewPath}
		source, err := review.IssuedResultSourceRoot(root, snap.State, packet, p.Reception)
		refs := []string{p.ViewPath, p.AnswerPath, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath)}
		if source != "" {
			refs = append(refs, filepath.Join(source, p.ViewPath), filepath.Join(source, p.AnswerPath))
		}
		candidates = append(candidates, nativeReviewCandidate{owner: p.Binding.OwnerAgentID, answer: filepath.Join(source, p.AnswerPath), source: source, packet: p.ID, view: filepath.Join(source, p.ViewPath), stage: "S7", assignment: p.Binding.AssignmentID, claims: p.Binding.ClaimIDs, refs: refs, err: err})
	}
	switch stage {
	case "S5":
		for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
			if row["template_id"] != "s5-document-review-v1" {
				continue
			}
			p, found, err := runtime.FindWorkPacket(snap.State, stringValue(row["packet_id"]))
			if err != nil {
				sourceErr = err
				continue
			}
			if !found {
				continue
			}
			if p.Binding.RuntimeID != snap.State["runtime_id"] || p.Binding.Generation != workspace.Generation(snap.State) || p.Binding.StageEntry != tolerantInt(mapFieldCLI(snap.State, "lifecycle")["phase_revision"]) {
				continue
			}
			if record, accepted, err := runtime.FindSubmission(snap.State, p.ID); err != nil {
				sourceErr = err
				continue
			} else if accepted && (record.RevisionPacketID != "" || record.SupersededBy != "") {
				continue
			}
			var aliases []string
			frozenAnswer, frozenSource := "", ""
			if registry, err := workspace.Decode(snap.State); err == nil && registry != nil {
				if execution, declared, err := workspace.ReviewExecution(snap.State, p); err == nil && declared && execution.Path != "" {
					if paths, err := registry.WorkerSubmissionPaths(execution); err == nil {
						frozenSource, frozenAnswer = execution.Path, filepath.Join(execution.Path, paths.Answer)
						aliases = []string{frozenAnswer, filepath.Join(execution.Path, workpacket.WorkerViewPath)}
					}
				}
			}
			selectAnswer(p.Binding.Owner, p.Binding.AssignmentID, p.ViewPath, p.AnswerPath, p.Binding.Generation, p.Binding.RuntimeID, aliases...)
			candidates[len(candidates)-1].packet = p.ID
			if frozenAnswer != "" {
				candidates[len(candidates)-1].answer, candidates[len(candidates)-1].source = frozenAnswer, frozenSource
			}
		}
	case "S7", "S8":
		// Already issued Reviewer and Investigator responsibilities can overlap
		// across a stage transition. Neither candidate reads another answer.
		packets, err := runtime.ProjectIssuedReviewResultPacketIndex(snap.State, "")
		if err != nil && stage == "S7" {
			message := "S7 current responsibility index is unavailable; Main must restore the original Runtime index before answer observation: " + nativeFailureDetail(err)
			fmt.Fprintln(diagnostic, message)
			_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
			return true, 0
		}
		if err == nil {
			for _, p := range packets {
				if in.Event == "SubagentStart" && stage == "S7" {
					packet := workpacket.ReviewResultPacket{ID: p.ID, Binding: p.Binding}
					if p.Reception["receipt"] != nil && p.Reception["work_complete"] == true || review.IssuedResultWorkComplete(snap.State, packet) {
						continue
					}
				}
				selectReviewAnswer(p)
			}
		}

		current, currentErr := runtime.CurrentHypothesisResultPackets(root, snap.State, "")
		currentIDs := map[string]bool{}
		for _, p := range current {
			currentIDs[p.ID] = true
		}
		// A damaged Case or changed question must not erase the original
		// responsibility from the return. Registry references identify only
		// the diagnostic owner here; only a current packet can be received.
		hypothesisPackets, err := runtime.HypothesisResultPackets(snap.State)
		if err != nil {
			sourceErr = err
		}
		for _, p := range hypothesisPackets {
			if p.Binding.RuntimeID != snap.State["runtime_id"] {
				continue
			}
			selectAnswer(p.Binding.OwnerAgentID, p.Binding.AssignmentID, p.ViewPath, p.AnswerPath, p.Binding.BaselineGeneration, p.Binding.RuntimeID)
			candidates[len(candidates)-1].packet = p.ID
			candidates[len(candidates)-1].stage = "S8"
			candidates[len(candidates)-1].refs = append(candidates[len(candidates)-1].refs, p.ViewPath, p.AnswerPath)
			candidates[len(candidates)-1].hypothesis = p.Binding.HypothesisID
			candidates[len(candidates)-1].diagnosticOnly = currentErr != nil || !currentIDs[p.ID]
			if candidates[len(candidates)-1].err == nil {
				if currentErr != nil {
					candidates[len(candidates)-1].err = currentErr
				} else if !currentIDs[p.ID] {
					candidates[len(candidates)-1].err = fmt.Errorf("S8 questionnaire %s is no longer a current responsibility; Main must inspect the original Case question, source and route before resuming; no answer was received", p.ID)
				}
			}
			if candidates[len(candidates)-1].source == "" {
				// Keep the issued reference actionable when its original source
				// is unavailable. This does not permit reception from Main.
				candidates[len(candidates)-1].answer = filepath.Join(root, p.AnswerPath)
			}
		}
	}
	var refs []string
	for _, c := range candidates {
		refs = append(refs, c.refs...)
		if c.stage == "S8" {
			refs = append(refs, c.owner, c.hypothesis)
			for _, ref := range c.refs {
				if rel, err := filepath.Rel(root, ref); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					refs = append(refs, rel)
				}
			}
		}
	}
	taskInput := in
	if ordinaryReview {
		// Tool payloads and assistant output never nominate a professional owner.
		taskInput.LastAssistantMessage = ""
	}
	var text string
	if stage == "S5" {
		text, _ = nativeTaskTextDiagnostic(taskInput, refs, true)
	} else {
		if taskInput.AgentTranscriptPath == "" && in.Event == "SubagentStart" {
			child, ok := nativeChildTranscript(in)
			if !ok {
				return false, 0
			}
			taskInput.AgentTranscriptPath = child
		}
		text = nativeReviewTaskText(taskInput, refs)
	}
	var selected []nativeReviewCandidate
	unknown := false
	if stage == "S5" {
		for _, c := range candidates {
			matches := false
			for _, ref := range c.refs {
				matches = matches || nativeS6ExactReference(text, ref)
			}
			if matches {
				selected = append(selected, c)
			}
		}
		unknown = len(selected) > 1
		if unknown {
			selected = nil
		}
	} else {
		selected, unknown = nativeReviewSelect(nativeReviewFacts(text), text, root, candidates)
	}
	if len(selected) == 0 && sourceErr != nil {
		fmt.Fprintln(diagnostic, "Original responsibility index could not be read:", sourceErr)
		unknown = true
	}
	if len(selected) == 0 {
		if !unknown && (stage == "S7" || stage == "S8") {
			return false, 0
		}
		if unknown {
			message := stage + " lifecycle handoff contains an unresolved original responsibility; Main must inspect the original dispatch"
			fmt.Fprintln(diagnostic, message)
			if ordinaryReview {
				_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
			}
		}
		return true, 0
	}
	handle := func(c nativeReviewCandidate, out io.Writer) (bool, int) {
		stage, owner, answer, source, packet, sourceErr := c.stage, c.owner, c.answer, c.source, c.packet, c.err

		if in.Event == "SubagentStart" {
			view := ""
			for _, c := range candidates {
				if c.packet == packet {
					view = c.view
				}
			}

			message := "S7 current professional responsibility for " + owner + ": packet " + packet + "; view " + view + "; answer " + answer + ". Read that exact original questionnaire, review only its assigned Claims against the frozen subjects, save the professional answer at that exact original path. Ordinary PreToolUse and successful PostToolUse automatically observe that answer through the original consumer; SubagentStop retains the final fallback. No submit, done or return-reference ceremony is required. Ordinary tools are freely available; no activation or platform ID lookup is required."
			if stage == "S8" {
				message = "S8 current professional responsibility for " + owner + ": packet " + packet + "; view " + view + "; answer " + answer + ". Read that exact original question, perform its actual independent discriminator without product writes, and save your professional HypothesisResult at the supplied original path. Ordinary PreToolUse and successful PostToolUse observe that answer through the original Case consumer; SubagentStop is the final fallback. No submit, done or return-reference action is required. Ordinary tools remain available. An inconclusive result is an honest conclusion, not proof that you have not done your work; Main owns unknowns and next responsibility. Genuine unfinished work may continue on the original responsibility."
			}
			if sourceErr != nil {
				message += " Main must restore the original source: " + nativeFailureDetail(sourceErr)
			}
			_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
			return true, 0
		}
		// Only a proven same-repository different checkout rejects a shared
		// original; missing/non-Git cwd does not invent a new source binding.

		var received submission.Receipt
		if sourceErr == nil {
			service, err := submission.NewContext(contextForHook(in), root, true)
			if err != nil {
				sourceErr = err
			} else {
				var receipt submission.Receipt
				if stage == "S7" {
					receipt, sourceErr = service.ReviewResultReceipt(owner, packet)
				}
				if stage == "S8" {
					receipt, sourceErr = service.ObserveHypothesisResultFrom(contextForHook(in), owner, answer, source, nil, ordinaryReview)
				}
				if stage != "S8" && sourceErr == nil && receipt.Accepted && receipt.WorkComplete {
					if stage != "S7" || !s7SuccessorPending(root, snap.State) {
						return true, 0
					}
					applies, compareErr := reviewPacketAppliesToCurrent(root, snap.State, packet, contextForHook(in))
					if compareErr != nil {
						message := "S7 original answer is already received; its relationship to the current responsibility is unknown: " + compareErr.Error() + ". Preserve the accepted answer; Main restores the original plan materials before continuing the outlet."
						fmt.Fprintln(diagnostic, message)
						_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
						return true, 0
					}
					if !applies {
						return true, 0
					}
					received = receipt
				} else {
					switch stage {
					case "S5":
						receipt, sourceErr = service.Save(contextForHook(in), owner, answer)
					case "S7":
						receipt, sourceErr = service.SaveReviewResultFrom(contextForHook(in), owner, answer, source, nil)
					case "S8":
						// The original consumer above already returned this attempt's
						// outcome; never query or receive it again in this event.
					}
				}
				if ordinaryReview && len(receipt.FieldErrors) > 0 && (stage != "S7" || !s7ResultHasProfessionalExpression(answer)) {
					return true, 0
				}
				var committedGap *submission.ReviewResultReceiptError
				if len(receipt.FieldErrors) != 0 && !errors.As(sourceErr, &committedGap) {
					sourceErr = &workpacket.AnswerError{Label: stage + " answer", Fields: receipt.FieldErrors}
				}
				if sourceErr == nil && !receipt.Accepted {
					sourceErr = fmt.Errorf("original professional consumer did not accept this answer: %s", receipt.Error)
				}
				if sourceErr == nil {
					received = receipt
					fmt.Fprintln(diagnostic, stage+" native professional answer received by the original consumer. "+receipt.NextAction)
				}
			}
		}
		if sourceErr != nil {
			if ordinaryReview && (os.IsNotExist(sourceErr) || len(received.FieldErrors) > 0) {
				return true, 0
			}
			if stage == "S5" {
				submission.RecordPlanningDiagnostic(root, snap.State, nativeS5DiagnosticKind(packet), fmt.Sprintf("owner %s; answer %s: %s", owner, answer, nativeFailureDetail(sourceErr)))
				return true, nativeS5HandoffFailure(owner, answer, sourceErr, diagnostic)
			}
			if stage == "S7" {
				var receiptGap *submission.ReviewResultReceiptError
				if errors.As(sourceErr, &receiptGap) {
					return true, nativeS7SubmittedReceiptFailure(root, in, snap.State, receiptGap, out, diagnostic)
				}
				if ordinaryReview {
					message := fmt.Sprintf("S7 automatic original-answer observation for owner %s, packet %s, answer %s could not verify reception: %s. Preserve the original answer and any accepted receipt. Main must restore the original packet/source/receiver and inspect the original receipt. Ordinary tools remain available; no submit or done action is required.", owner, packet, answer, nativeFailureDetail(sourceErr))
					var fields *workpacket.AnswerError
					if errors.As(sourceErr, &fields) {
						message = fmt.Sprintf("S7 automatic original-answer observation for owner %s, packet %s, answer %s remains unreceived: %s. Correct the specified professional fields in that same original answer. Ordinary tools remain available; no submit or done action is required.", owner, packet, answer, nativeFailureDetail(sourceErr))
					}
					fmt.Fprintln(diagnostic, message)
					_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
					return true, 0
				}
				submission.RecordPlanningDiagnostic(root, snap.State, nativeS7DiagnosticKind(packet), fmt.Sprintf("owner %s; packet %s; answer %s: %s", owner, packet, answer, nativeFailureDetail(sourceErr)))
				return true, nativeS7HandoffFailure(owner, answer, sourceErr, diagnostic)
			}
			if stage == "S8" {
				if ordinaryReview {
					message := fmt.Sprintf("S8 original-answer observation for %s at %s could not receive a new delivery: %s. Preserve any accepted receipt and prior observations. Main owns source or preparation recovery; genuine unfinished work may continue on its original responsibility. Ordinary tools remain available; no return-reference, resubmit or repeated Stop action is required.", owner, answer, nativeFailureDetail(sourceErr))
					fmt.Fprintln(diagnostic, message)
					_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
					return true, 0
				}
				submission.RecordPlanningDiagnostic(root, snap.State, "S8 native Stop "+packet, fmt.Sprintf("owner %s; answer %s: %s", owner, answer, nativeFailureDetail(sourceErr)))
				return true, nativeS8HandoffFailure(owner, answer, sourceErr, diagnostic)
			}
			return true, nativeStopFailure(stage+" native professional handoff", in.StopHookActive, sourceErr, diagnostic)
		}
		if stage == "S5" {
			submission.RecordPlanningDiagnostic(root, snap.State, nativeS5DiagnosticKind(packet), "")
		}
		if stage == "S7" {
			submission.RecordPlanningDiagnostic(root, snap.State, nativeS7DiagnosticKind(packet), "")
			// Reception is already durable. Continue only the original clean-round
			// exit; a continuation failure never turns it into an unreceived answer.
			statePath := filepath.Join(root, ".claude/loop-state.json")
			journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
			completionContext, cancelCompletion := hookCompletionContext(contextForHook(in))
			defer cancelCompletion()
			current, err := runtime.NewStore(statePath, journalPath).WithContext(completionContext).Snapshot()
			handoff := ""
			if err != nil {
				handoff = "S7 ReviewResult is received; Main restores current Runtime access before continuing the original clean-round exit: " + err.Error() + ". Recovery: " + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime review-continue --root " + s7CaptureShellQuote(root)
				fmt.Fprintln(diagnostic, handoff)
			} else {
				applies, compareErr := reviewPacketAppliesToCurrent(root, current.State, packet, completionContext)
				if compareErr != nil {
					handoff = "Original S7 answer is accepted; its relationship to the current responsibility is unknown: " + compareErr.Error() + ". Main restores the original plan materials and continues the original outlet; the Reviewer need not resubmit."
					fmt.Fprintln(diagnostic, handoff)
				} else if !applies {
					handoff = "原 S7 晚到答卷已收；Main 查阅原 Result 与发现后决定补充调查或后续处理。"
				} else {
					var continuationErr error
					current, handoff, continuationErr = continueS7AcceptanceResult(completionContext, root, statePath, journalPath, current)
					if handoff != "" {
						fmt.Fprintln(diagnostic, handoff)
					}
					if continuationErr == nil {
						handoff = ""
					}
				}
			}
			if ordinaryReview {
				// A failed snapshot cannot supply a current stage. Reception remains
				// durable and its concrete recovery is carried to this actual Worker.
				nativeS7NaturalReceipt(root, in, current, owner, packet, answer, received, handoff, out)
			}
		}
		if stage == "S8" {
			submission.RecordPlanningDiagnostic(root, snap.State, "S8 native Stop "+packet, "")
		}
		return true, 0
	}
	if len(selected) == 1 && !unknown {
		return handle(selected[0], out)
	}
	var messages []string
	code := 0
	for i, c := range selected {
		if i > 0 {
			in.Context = context.WithValue(contextForHook(in), nativeRoutingKey{}, &nativeRoutingRead{root: root})
			var err error
			snap, err = nativeRoutingSnapshot(root, in)
			if err != nil {
				fmt.Fprintln(diagnostic, "Remaining original responsibilities are unknown because Runtime could not be read:", err)
				unknown = true
				break
			}
		}
		var output bytes.Buffer
		_, itemCode := handle(c, &output)
		if itemCode != 0 {
			code = itemCode
		}
		messages = append(messages, nativeReviewOutputMessages(output.Bytes())...)
	}
	if unknown {
		message := "Some original responsibilities remain unknown; Main inspects the original dispatch and preserves any accepted results."
		fmt.Fprintln(diagnostic, message)
		if ordinaryReview || in.Event == "SubagentStart" {
			messages = append(messages, message)
		}
	}
	if len(messages) != 0 {
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": strings.Join(messages, "\n")}})
	}
	return true, code
}

// An untouched questionnaire is not a failed professional answer. A stated
// conclusion (even an invalid one) is an actual expression and must receive
// the original consumer's field feedback on the same natural event.
func s7ResultHasProfessionalExpression(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var draft struct {
		Answer map[string]json.RawMessage `json:"answer"`
	}
	if json.Unmarshal(data, &draft) != nil {
		return false
	}
	expressed := func(raw json.RawMessage) bool {
		text := strings.TrimSpace(string(raw))
		return text != "" && text != "null" && text != `""`
	}
	if expressed(draft.Answer["verdict"]) {
		return true
	}
	var claims []map[string]json.RawMessage
	if json.Unmarshal(draft.Answer["claim_results"], &claims) == nil {
		for _, claim := range claims {
			if expressed(claim["conclusion"]) || expressed(claim["observed"]) {
				return true
			}
		}
	}
	return false
}

// The original platform task selects the responsibility. Later coordinator
// messages and the Worker's final text are not a new dispatch.
func nativeReviewTaskText(in policy.Input, refs []string) string {
	defer metrics.Span(contextForHook(in), "hook/native_routes/task_text")()
	file, err := os.Open(in.AgentTranscriptPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	reader := &nativeTaskTranscript{ctx: contextForHook(in), reader: file, size: info.Size()}
	text, _, _ := reader.earliestUsing("", func(line []byte, final string) (string, bool, bool) {
		return nativeReviewOriginalTask(line, in.AgentID, final)
	})
	return text
}

// Project durable personal acceptance and the original Main-owned outlet only.
// Called only on first natural-event receipt; accepted replays stay silent and
// never rerun Gate checks or S10 material preparation.
func nativeS7NaturalReceipt(root string, in policy.Input, snapshot runtime.Snapshot, owner, packet, answer string, receipt submission.Receipt, continuation string, out io.Writer) {
	message := fmt.Sprintf("S7 original ReviewResult receipt: packet %s; owner %s; answer %s; accepted=%t; work_complete=%t; responsibility_status=%s. Your assigned Reviewer answer is received; preserve it and any observations. No further save, submit or done action is required for this accepted answer.", packet, owner, answer, receipt.Accepted, receipt.WorkComplete, receipt.ResponsibilityStatus)
	message += " " + receipt.NextAction
	if snapshot.State == nil {
		message += " Current Runtime stage is unavailable. Main owns recovery of the original continuation. " + continuation
	} else {
		stage := lifecycleState(snapshot.State)
		phase := stringValue(mapFieldCLI(snapshot.State, "lifecycle")["phase"])
		message += fmt.Sprintf(" Current Runtime state=%s phase=%s. Main owns round-wide and subsequent-stage responsibility; the Reviewer does not inherit that work.", stage, phase)
		if stage == "verification" && mapFieldCLI(mapFieldCLI(snapshot.State, "review"), "plan")["status"] == "clean" {
			message += " The original ReviewPlan is recorded as clean; the S7 exit remains pending. Main restores its recorded cause and resumes the original exit with: " + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime review-continue --root " + s7CaptureShellQuote(root)
		} else if stage == "acceptance" {
			message += " Original TR-009 has entered S10 acceptance; Main follows its original audit questionnaire or material recovery."
		}
		// Existing continuation diagnostics retain the exact failed Gate or
		// preparation cause; their projection is read-only and does not retry it.
		if detail := submission.PlanningDiagnosticContext(contextForHook(in), root, snapshot.State, "S7 acceptance continuation"); detail != "" {
			message += " Main's recorded continuation diagnostic: " + detail
		} else if continuation != "" {
			message += " Main's original continuation remains pending: " + continuation
		}
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
}

// Submit succeeded, but the following receipt check did not. Preserve both
// facts without asking the professional to edit or submit the answer again.
func nativeS7SubmittedReceiptFailure(root string, in policy.Input, state map[string]any, gap *submission.ReviewResultReceiptError, out, diagnostic io.Writer) int {
	message := fmt.Sprintf("S7 original submission succeeded; receipt verification remains incomplete: packet %s; owner %s; answer %s; source %s; Runtime revision %d. The receipt has not been verified and personal or round completion is not asserted. Main must inspect the original receipt and restore the original receiver before continuing the original exit with %s runtime review-continue --root %s. Preserve the original answer; the Reviewer has no new authoring action. Cause: %v", gap.PacketID, gap.OwnerAgentID, gap.AnswerPath, gap.SourceRoot, gap.Revision, s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")), s7CaptureShellQuote(root), gap.Cause)
	// Reuse the original display diagnostic, without making it receipt authority.
	// Its best-effort writer cannot guarantee delivery under storage failure.
	submission.RecordPlanningDiagnostic(root, state, nativeS7DiagnosticKind(gap.PacketID), message)
	message += " Main must independently verify the original authoritative receipt; this display diagnostic is best effort and may not have reached Main."
	fmt.Fprintln(diagnostic, message)
	_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
	return 0
}

func reviewPacketAppliesToCurrent(root string, state map[string]any, packetID string, ctx context.Context) (bool, error) {
	index, err := runtime.ProjectIssuedReviewResultPacketIndex(state, "")
	if err != nil {
		return false, err
	}
	for _, p := range index {
		if p.ID == packetID {
			return review.IssuedResultAppliesToCurrent(root, state, workpacket.ReviewResultPacket{ID: p.ID, Binding: p.Binding}, ctx)
		}
	}
	return false, fmt.Errorf("original Reviewer packet %s is missing from the responsibility index", packetID)
}
