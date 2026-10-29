package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const repairResultAnswerLimit int64 = 20 << 20

// IssueRepairResults delegates packet creation and PACKET-ISSUE to the
// original Repair Writer. This adapter only publishes the canonical view and
// answer draft into each exact registered Worker checkout.
func (s *Service) IssueRepairResults(ctx context.Context, ownerFilter string) ([]workpacket.RepairResultPacket, error) {
	s = s.WithContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The named original issuer owns recovery of an exact pending PACKET-ISSUE
	// pair. Do not put a read-only service snapshot in front of that recovery.
	packets, err := repair.IssueRepairResultPackets(ctx, s.Root, s.StatePath, s.JournalPath, -1, ownerFilter)
	if err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		latest, err := s.snapshot()
		if err != nil {
			return nil, err
		}
		sourceRoot, execution, err := RepairResultRegisteredSource(s.Root, latest.State, packet, ctx)
		if err != nil {
			return nil, err
		}
		if repair.NativeS9SourceReference(execution).Path != "" {
			if _, err := repair.ReadRepairResultInput(ctx, s.Root, latest.State, packet, execution); err != nil {
				return nil, err
			}
			continue
		}
		if err := publishRepairResultAt(sourceRoot, s.Root, packet); err != nil {
			return nil, err
		}
	}
	return packets, nil
}

// RepairResultSourceForPath resolves only a current packet in its registered
// execution. It is a selector, not proof of the Hook caller's identity.
func (s *Service) RepairResultSourceForPath(path string) (workpacket.RepairResultPacket, string, bool, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return workpacket.RepairResultPacket{}, "", false, err
	}
	return currentRepairResultSourceForPath(s.Root, snapshot.State, path, s.Context())
}

// SaveRepairResultFrom accepts the exact issued answer through the original
// candidate/workspace_updated or blocker/work_blocked Writer. The packet's
// registered Worker source is selected again inside the original CAS. Empty owner
// and sourceRoot hints use the responsibility selected by the exact answer path.
func (s *Service) SaveRepairResultFrom(ctx context.Context, owner, path, sourceRoot string, authorize func(root string, state map[string]any) error) (Receipt, error) {
	receipt, _, err := s.saveRepairResultFrom(ctx, owner, path, sourceRoot, authorize, false)
	return receipt, err
}

// SaveRepairResultFromOrdinaryHook selects the current exact source before
// reading an answer. Only a confirmed native delivery is quiet; receive means
// the original receiver or its rejection feedback applies, not acceptance.
func (s *Service) SaveRepairResultFromOrdinaryHook(ctx context.Context, path string) (Receipt, bool, error) {
	return s.saveRepairResultFrom(ctx, "", path, "", nil, true)
}

func (s *Service) saveRepairResultFrom(ctx context.Context, owner, path, sourceRoot string, authorize func(root string, state map[string]any) error, ordinary bool) (Receipt, bool, error) {
	s = s.WithContext(ctx)
	if err := ctx.Err(); err != nil {
		return Receipt{}, true, err
	}
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, true, err
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, "", err), true, err
	}
	selection, found, err := selectCurrentRepairResultSource(s.Root, snapshot.State, path, s.Context())
	if err != nil {
		return rejected(snapshot, "", err), true, err
	}
	if !found {
		err := errors.New("S9 result answer path has no current issuance for this registered Builder")
		return rejected(snapshot, "", err), true, err
	}
	packet, actualRoot := selection.packet, selection.root
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, packet.ID, err), true, err
	}
	if ordinary {
		switch selection.execution.Purpose {
		case workspace.NativeDeliveryPurpose:
			return Receipt{}, false, nil
		case "":
			// The original selector has confirmed an independent registered source.
		default:
			err := fmt.Errorf("S9 ordinary Result save does not support execution purpose %q", selection.execution.Purpose)
			return rejected(snapshot, packet.ID, err), true, err
		}
	}
	owner = packet.Binding.Sources.OwnerAgentID
	if sourceRoot != "" {
		want, err := filepath.Abs(sourceRoot)
		if err != nil {
			return rejected(snapshot, packet.ID, err), true, err
		}
		got, err := filepath.Abs(actualRoot)
		if err != nil || filepath.Clean(want) != filepath.Clean(got) {
			err := errors.New("S9 result answer source root differs from the registered Worker execution")
			return rejected(snapshot, packet.ID, err), true, err
		}
	}
	answerBytes, err := repair.ReadRepairResultInput(ctx, s.Root, snapshot.State, packet, selection.execution)
	if err != nil {
		return rejected(snapshot, packet.ID, err), true, err
	}
	// Decode only the routing field here. The selected receiver validates the
	// professional answer once, immediately before recording it.
	var document struct {
		Answer workpacket.RepairResultProfessionalAnswer `json:"answer"`
	}
	if err := json.Unmarshal(answerBytes, &document); err != nil {
		return rejected(snapshot, packet.ID, err), true, err
	}
	professional := document.Answer
	if professional.Verdict != "completed" {
		receipt, err := s.saveRepairResultBlocker(ctx, packet, owner, actualRoot, professional.Verdict, nil)
		return receipt, true, err
	}
	next, candidate, candidateRef, acceptErr := repair.AcceptIssuedRepairResultCandidate(ctx, s.Root, s.StatePath, s.JournalPath, repair.AcceptIssuedRepairResultCandidateRequest{
		ExpectedRevision: snapshot.Revision,
		PacketID:         packet.ID,
		AnswerPath:       packet.AnswerPath,
		OwnerAgentID:     owner,
		SourceRoot:       actualRoot,
		OccurredAt:       time.Now().UTC(),
	})
	if acceptErr != nil {
		return rejected(next, packet.ID, acceptErr), true, acceptErr
	}
	accepted := &loopruntime.RepairResultCandidateReceipt{
		CandidateRef: candidateRef.Path, CandidateSHA256: candidateRef.SHA256,
		AcceptedIdempotencyKey: "runtime:s9:issued-repair-candidate:" + packet.ID,
	}
	if candidate.Provenance != nil {
		accepted.Provenance = *candidate.Provenance
	}
	receipt, err := acceptedRepairResultCandidateReceipt(s.Root, next, packet, owner, accepted, s.Context())
	return receipt, true, err
}

// RepairResultReceipt is a read-only view of the original blocker, candidate or final receipt.
// Main verification/final Result remains a separate original consumer.
func (s *Service) RepairResultReceipt(owner, ref string) (Receipt, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	packet, found, err := repairResultByReference(s.Root, snapshot.State, ref, s.Context())
	if err != nil {
		return Receipt{}, err
	}
	if !found || owner == "" || packet.Binding.Sources.OwnerAgentID != owner {
		return rejected(snapshot, "", errors.New("S9 packet reference has no matching registered Builder")), nil
	}
	if result, found, err := repairResultBlockerReceipt(s.Root, snapshot, packet, owner); err != nil || found {
		return result, err
	}
	final, finalFound, finalErr := recordedRepairResultReceiptContext(s.Context(), s.Root, snapshot.State, packet.ID, owner)
	if finalErr != nil {
		return Receipt{}, finalErr
	}
	if finalFound {
		result := emptyReceipt(snapshot, packet.ID)
		if final.Provenance.PacketID != packet.ID || final.Provenance.OwnerAgentID != owner || final.ResultID != packet.ResultID || final.ResultRef == "" || final.CandidateRef == "" {
			return Receipt{}, errors.New("S9 original final Result receipt differs from its packet")
		}
		result.Accepted = true
		// The original receipt is immutable historical acceptance. Project
		// current owner completion from the original domain consumer so an
		// accepted Result cannot revive responsibility after the execution was
		// replaced or explicitly reworked.
		workComplete, completionErr := repair.RepairResultWorkCompleteWithContext(s.Context(), s.Root, snapshot.State, packet.ID, owner)
		result.WorkComplete = completionErr == nil && workComplete
		result.SubmissionKey = final.AcceptedIdempotencyKey
		result.ConsumerOwner = "main"
		result.ConsumerPending = true
		result.DomainRefs = []string{final.CandidateRef, final.ResultRef}
		if completionErr != nil {
			result.ResponsibilityStatus = "main_result_accepted_owner_status_unavailable"
			result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "accepted Result is historical; current Worker completion proof could not be resolved", Owner: owner, Ref: packet.ID})
			result.NextAction = "原 Main Result 已接收且保留其历史回执；当前 Builder 完成状态无法核验，先用 runtime repair status 检查执行/返工记录，不要把历史 receipt 当作当前完成。"
		} else if !workComplete {
			result.ResponsibilityStatus = "main_result_accepted_rework_required"
			result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "accepted Result is historical; current Worker attempt no longer matches its candidate", Owner: owner, Ref: packet.ID})
			result.NextAction = "原 Main Result 已接收且保留其历史回执；当前执行代次或返工已变化，按 runtime repair status 给出的原命名返工路径继续，不要把旧 receipt 当作当前完成。"
		} else {
			result.ResponsibilityStatus = "main_result_accepted"
			result.NextAction = "原 Main Result 已接收；继续原 checkpoint ACK/cleanup 与 S9/S7 后续门，不重复提交答卷。"
		}
		return result, nil
	}
	return acceptedRepairResultCandidateReceipt(s.Root, snapshot, packet, owner, nil, s.Context())
}

type repairResultSourceSelection struct {
	packet    workpacket.RepairResultPacket
	root      string
	execution workspace.Execution
}

func currentRepairResultSourceForPath(root string, state map[string]any, inputPath string, contexts ...context.Context) (workpacket.RepairResultPacket, string, bool, error) {
	selection, found, err := selectCurrentRepairResultSource(root, state, inputPath, contexts...)
	return selection.packet, selection.root, found, err
}

func selectCurrentRepairResultSource(root string, state map[string]any, inputPath string, contexts ...context.Context) (repairResultSourceSelection, bool, error) {
	packets, err := loopruntime.RepairResultPacketsForPathContext(loopruntime.WithPacketBodyRoot(submissionRepairPacketContext(contexts...), root), state, inputPath)
	if err != nil {
		return repairResultSourceSelection{}, false, err
	}
	return selectRepairResultPathInRoots(root, state, inputPath, packets, contexts...)
}

func repairResultByReference(root string, state map[string]any, ref string, contexts ...context.Context) (workpacket.RepairResultPacket, bool, error) {
	if packet, found, err := loopruntime.FindRepairResultPacketContext(loopruntime.WithPacketBodyRoot(submissionRepairPacketContext(contexts...), root), state, ref); err != nil || found {
		return packet, found, err
	}
	packets, err := loopruntime.RepairResultPacketsForPathContext(loopruntime.WithPacketBodyRoot(submissionRepairPacketContext(contexts...), root), state, ref)
	if err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	packet, _, found, err := repairResultPathInRoots(root, state, ref, packets, contexts...)
	if err != nil || found {
		return packet, found, err
	}
	packet, _, found, err = acceptedRepairResultPathWithSource(root, state, ref, packets)
	return packet, found, err
}

func acceptedRepairResultPathInRoots(root string, state map[string]any, inputPath string, packets []workpacket.RepairResultPacket) (workpacket.RepairResultPacket, bool, error) {
	packet, _, found, err := acceptedRepairResultPathWithSource(root, state, inputPath, packets)
	return packet, found, err
}

func acceptedRepairResultPathWithSource(root string, state map[string]any, inputPath string, packets []workpacket.RepairResultPacket) (workpacket.RepairResultPacket, string, bool, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return workpacket.RepairResultPacket{}, "", false, err
	}
	if !filepath.IsAbs(inputPath) {
		abs, err = filepath.Abs(filepath.Join(root, filepath.FromSlash(inputPath)))
		if err != nil {
			return workpacket.RepairResultPacket{}, "", false, err
		}
	}
	var selected *workpacket.RepairResultPacket
	selectedRoot := ""
	for _, candidate := range packets {
		accepted, ok, receiptErr := loopruntime.VerifiedRepairResultCandidateReceipt(root, state, candidate.ID, candidate.Binding.Sources.OwnerAgentID)
		if receiptErr != nil {
			return workpacket.RepairResultPacket{}, "", false, receiptErr
		}
		acceptedRoot := ""
		if ok && accepted.Provenance.PacketID == candidate.ID && accepted.Provenance.OwnerAgentID == candidate.Binding.Sources.OwnerAgentID {
			acceptedRoot = accepted.Provenance.ExecutionRoot
		} else {
			blocker, blocked, err := loopruntime.VerifiedRepairResultBlockerReceipt(root, state, candidate.ID, candidate.Binding.Sources.OwnerAgentID)
			if err != nil {
				return workpacket.RepairResultPacket{}, "", false, err
			}
			if !blocked || blocker.Artifact.PacketID != candidate.ID || blocker.Artifact.Binding.Sources.OwnerAgentID != candidate.Binding.Sources.OwnerAgentID {
				continue
			}
			acceptedRoot = blocker.Artifact.ExecutionRoot
		}
		sourceRoot, err := workspace.Canonical(acceptedRoot)
		if err != nil || filepath.Clean(sourceRoot) == filepath.Clean(root) {
			continue
		}
		var frozenExecution struct {
			Path string `json:"worktree_path"`
		}
		if json.Unmarshal(candidate.Inputs.Execution, &frozenExecution) != nil || frozenExecution.Path == "" {
			continue
		}
		frozenRoot, err := workspace.Canonical(frozenExecution.Path)
		if err != nil || filepath.Clean(frozenRoot) != filepath.Clean(sourceRoot) {
			continue
		}
		candidatePath, err := filepath.Abs(filepath.Join(sourceRoot, filepath.FromSlash(candidate.AnswerPath)))
		if err != nil || filepath.Clean(candidatePath) != filepath.Clean(abs) {
			continue
		}
		if err := workpacket.CheckPath(sourceRoot, candidate.AnswerPath); err != nil {
			return workpacket.RepairResultPacket{}, "", false, err
		}
		copyPacket := candidate
		if selected != nil && (selected.ID != copyPacket.ID || filepath.Clean(selectedRoot) != filepath.Clean(sourceRoot)) {
			return workpacket.RepairResultPacket{}, "", false, errors.New("S9 accepted answer path is ambiguous across issued packets")
		}
		selected, selectedRoot = &copyPacket, sourceRoot
	}
	if selected == nil {
		return workpacket.RepairResultPacket{}, "", false, nil
	}
	return *selected, selectedRoot, true, nil
}

func repairResultPathInRoots(root string, state map[string]any, inputPath string, packets []workpacket.RepairResultPacket, contexts ...context.Context) (workpacket.RepairResultPacket, string, bool, error) {
	selection, found, err := selectRepairResultPathInRoots(root, state, inputPath, packets, contexts...)
	return selection.packet, selection.root, found, err
}

func selectRepairResultPathInRoots(root string, state map[string]any, inputPath string, packets []workpacket.RepairResultPacket, contexts ...context.Context) (repairResultSourceSelection, bool, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return repairResultSourceSelection{}, false, err
	}
	if !filepath.IsAbs(inputPath) {
		abs, err = filepath.Abs(filepath.Join(root, filepath.FromSlash(inputPath)))
		if err != nil {
			return repairResultSourceSelection{}, false, err
		}
	}
	var selected *workpacket.RepairResultPacket
	selectedRoot := ""
	var selectedExecution workspace.Execution
	for _, packet := range packets {
		if err := submissionRepairPacketContext(contexts...).Err(); err != nil {
			return repairResultSourceSelection{}, false, err
		}
		sourceRoot, execution, err := RepairResultRegisteredSource(root, state, packet, submissionRepairPacketContext(contexts...))
		if err != nil {
			if canceled := submissionRepairPacketContext(contexts...).Err(); canceled != nil {
				return repairResultSourceSelection{}, false, canceled
			}
			// A stale/historical packet is not a source-root grant. It remains
			// readable by packet ID through its original receipt.
			continue
		}
		candidate, err := filepath.Abs(filepath.Join(sourceRoot, filepath.FromSlash(packet.AnswerPath)))
		if err != nil || filepath.Clean(candidate) != filepath.Clean(abs) {
			continue
		}
		if repair.NativeS9SourceReference(execution).Path == "" {
			if err := workpacket.CheckPath(sourceRoot, packet.AnswerPath); err != nil {
				return repairResultSourceSelection{}, false, err
			}
		}
		if selected != nil && (selected.ID != packet.ID || filepath.Clean(selectedRoot) != filepath.Clean(sourceRoot)) {
			return repairResultSourceSelection{}, false, errors.New("S9 answer path is ambiguous across issued packets")
		}
		copyPacket := packet
		selected, selectedRoot, selectedExecution = &copyPacket, sourceRoot, execution
	}
	if selected == nil {
		return repairResultSourceSelection{}, false, nil
	}
	return repairResultSourceSelection{packet: *selected, root: selectedRoot, execution: selectedExecution}, true, nil
}

// RepairResultRegisteredSource reuses the original execution/source selector for
// both the consumer and its questionnaire view; it does not authorize tool calls.
func RepairResultRegisteredSource(root string, state map[string]any, packet workpacket.RepairResultPacket, contexts ...context.Context) (string, workspace.Execution, error) {
	if err := submissionRepairPacketContext(contexts...).Err(); err != nil {
		return "", workspace.Execution{}, err
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return "", workspace.Execution{}, err
	}
	var registry workspace.ExecutionRegistry
	if err := json.Unmarshal(raw, &registry); err != nil || registry.Executions == nil {
		return "", workspace.Execution{}, fmt.Errorf("S9 result packet requires a registered Worker execution: %v", err)
	}
	b := packet.Binding.Sources
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	if b.RuntimeID != workspace.RuntimeID(state) || b.BaselineGeneration != workspace.Generation(state) ||
		pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef {
		return "", workspace.Execution{}, errors.New("S9 packet differs from the current repair responsibility")
	}
	var selected *workspace.Execution
	for _, id := range []string{b.RepairAssignmentID, b.ManifestAssignmentID} {
		if id == "" {
			continue
		}
		e, ok := registry.Execution(id, b.RuntimeID, b.BaselineGeneration)
		if !ok {
			continue
		}
		if e.AssignmentID != id || e.AgentID != b.OwnerAgentID || (e.Status != "ready" && e.Status != "complete") {
			return "", workspace.Execution{}, errors.New("S9 packet execution owner, identity, or status differs")
		}
		if selected != nil && (selected.AssignmentID != e.AssignmentID || filepath.Clean(selected.Path) != filepath.Clean(e.Path)) {
			return "", workspace.Execution{}, errors.New("S9 packet has ambiguous canonical/manifest Worker executions")
		}
		copyExecution := e
		selected = &copyExecution
	}
	if selected == nil {
		return "", workspace.Execution{}, errors.New("S9 packet has no exact registered Worker execution")
	}
	var issuedExecution workspace.Execution
	if err := json.Unmarshal(packet.Inputs.Execution, &issuedExecution); err != nil ||
		issuedExecution.AssignmentID != selected.AssignmentID || issuedExecution.AgentID != selected.AgentID ||
		issuedExecution.RuntimeID != selected.RuntimeID || issuedExecution.BaselineGeneration != selected.BaselineGeneration ||
		issuedExecution.Generation != selected.Generation || packet.Binding.ReworkRef != selected.ReworkRef ||
		filepath.Clean(issuedExecution.Path) != filepath.Clean(selected.Path) {
		return "", workspace.Execution{}, errors.New("S9 packet execution association differs from the registered Worker")
	}
	if retained, err := repair.RetainedNativeS9Source(submissionRepairPacketContext(contexts...), root, *selected, b); err != nil {
		return "", workspace.Execution{}, err
	} else if retained != nil {
		return selected.Path, *selected, nil
	}
	rootPath, err := workspace.Canonical(selected.Path)
	if err != nil || rootPath != selected.Path {
		return "", workspace.Execution{}, errors.New("S9 registered Worker source root is unavailable or noncanonical")
	}
	if filepath.Clean(rootPath) == filepath.Clean(root) {
		return "", workspace.Execution{}, errors.New("S9 typed RepairResult does not support Main-native execution")
	}
	return rootPath, *selected, nil
}

func publishRepairResultAt(root, mainRoot string, packet workpacket.RepairResultPacket) error {
	mainRoot, err := filepath.Abs(mainRoot)
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
	view, err := json.MarshalIndent(display, "", "  ")
	if err != nil {
		return err
	}
	view = append(view, '\n')
	stored, err := workpacket.PublishBounded(root, packet.ViewPath, view, repairResultAnswerLimit)
	if err != nil {
		return err
	}
	if !bytes.Equal(stored, view) {
		return fmt.Errorf("S9 result issued view differs from the professional questionnaire")
	}
	_, err = workpacket.PublishBounded(root, packet.AnswerPath, view, repairResultAnswerLimit)
	return err
}

func acceptedRepairResultCandidateReceipt(root string, snapshot loopruntime.Snapshot, packet workpacket.RepairResultPacket, owner string, recorded *loopruntime.RepairResultCandidateReceipt, contexts ...context.Context) (Receipt, error) {
	ctx := submissionRepairPacketContext(contexts...)
	result := emptyReceipt(snapshot, packet.ID)
	var accepted loopruntime.RepairResultCandidateReceipt
	ok := recorded != nil
	var err error
	if recorded != nil {
		accepted = *recorded
	} else {
		accepted, ok, err = recordedRepairResultCandidateReceiptContext(ctx, root, snapshot.State, packet.ID, owner)
		if err != nil {
			return result, err
		}
	}
	if !ok {
		result = rejected(snapshot, packet.ID, errors.New("S9 candidate has not been accepted by the original workspace_updated Writer"))
		result.NextAction = "候选尚未由原 workspace_updated Writer 接收；保持工作区并重新提交已发行答卷。"
		return result, nil
	}
	if accepted.Provenance.PacketID != packet.ID || accepted.Provenance.OwnerAgentID != owner || accepted.CandidateRef == "" {
		return result, errors.New("original S9 candidate receipt differs from its issued packet")
	}
	result.Accepted = true
	result.SubmissionKey = accepted.AcceptedIdempotencyKey
	result.ResponsibilityStatus = "candidate_accepted"
	result.DomainRefs = []string{accepted.CandidateRef}
	result.WaitingOn = []Waiting{}
	result.WorkComplete, err = repair.RepairResultWorkCompleteWithContext(ctx, root, snapshot.State, packet.ID, owner)
	if err != nil {
		result.WorkComplete = false
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "S9 accepted candidate current attempt could not be read", Owner: owner, Ref: packet.ID})
		result.NextAction = "本人 candidate 已记录；当前 attempt 信息暂不可读，由 Main 检查当前工作安排，无需重复答卷。"
		return result, nil
	}
	if result.WorkComplete {
		result.ConsumerOwner = "main"
		result.ConsumerPending = true
		result.NextAction = fmt.Sprintf("本人 S9 candidate 已记录；Main 下一步执行 %s。Main 按修复目标和实际改动选择必要检查，以 --check-command 参数传入实际命令，多项检查可重复该参数；沿原集成执行并记录真实结果。未选择且无已有检查配置时 Main 检查仍待办，不会登记最终 PASS。无需重复答卷。", repairResultIntegrateCommand(root, accepted.Provenance.ExecutionAssignmentID, owner))
	} else {
		p := accepted.Provenance
		answerPath := filepath.Join(p.ExecutionRoot, filepath.FromSlash(packet.AnswerPath))
		var registry workspace.ExecutionRegistry
		raw, marshalErr := json.Marshal(snapshot.State["workspace"])
		attemptChanged := workspace.RuntimeID(snapshot.State) != p.RuntimeID || workspace.Generation(snapshot.State) != p.BaselineGeneration
		if marshalErr == nil && json.Unmarshal(raw, &registry) == nil {
			if execution, found := registry.Executions[p.ExecutionAssignmentID]; found {
				attemptChanged = attemptChanged || execution.Generation != p.ExecutionGeneration ||
					execution.ReworkRef != p.ReworkRef || filepath.Clean(execution.Path) != filepath.Clean(p.ExecutionRoot) ||
					execution.DeliveryRef != accepted.CandidateRef
			}
		}
		if attemptChanged {
			result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "accepted candidate belongs to a previous Worker attempt", Owner: "Main", Ref: answerPath})
			result.NextAction = "本人 candidate 已记录；当前 Worker attempt 已变化，由 Main 按当前工作安排继续。已接收答卷：" + answerPath + "；无需重交旧内容。"
		} else {
			// Explain recorded professional work, without validating or receiving
			// it again. Unknown projection data is not an attempt-change claim.
			var answer workpacket.RepairResultProfessionalAnswer
			var assigned struct {
				UnitIDs []string `json:"unit_ids"`
			}
			if json.Unmarshal(p.SubmittedAnswer, &answer) == nil {
				if answer.Verdict != "completed" {
					result.WaitingOn = append(result.WaitingOn, Waiting{Reason: fmt.Sprintf("recorded professional verdict is %s; implementation remains unfinished", answer.Verdict), Owner: "Main", Ref: answerPath})
				}
				if json.Unmarshal(packet.Inputs.Sources.RepairAssignment, &assigned) == nil {
					for _, id := range assigned.UnitIDs {
						status := "missing actual result"
						for _, unit := range answer.UnitResults {
							if unit.UnitID == id {
								status = unit.Status
								break
							}
						}
						if status != "pass" {
							result.WaitingOn = append(result.WaitingOn, Waiting{Reason: fmt.Sprintf("repair unit %s: %s", id, status), Owner: "Main", Ref: answerPath})
						}
					}
				}
			}
			if len(result.WaitingOn) == 0 {
				result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "current professional completion information is unavailable", Owner: "Main", Ref: answerPath})
			}
			result.NextAction = "本人 candidate 已记录，实际工作尚未完成；Main 按上述未完项安排继续。当前已接收答卷：" + answerPath + "；无需重复提交已接收内容。"
		}

	}
	return result, nil
}

// Receipt queries project already accepted indexed objects. They do not replay
// journal, issuance, source hashes or packet reconstruction.
func recordedRepairResultReceipt(root string, state map[string]any, packetID, owner string) (loopruntime.RepairResultReceipt, bool, error) {
	return recordedRepairResultReceiptContext(context.Background(), root, state, packetID, owner)
}
func recordedRepairResultReceiptContext(ctx context.Context, root string, state map[string]any, packetID, owner string) (loopruntime.RepairResultReceipt, bool, error) {
	if err := ctx.Err(); err != nil {
		return loopruntime.RepairResultReceipt{}, false, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	encoded, err := json.Marshal(pointer["result_refs"])
	if err != nil {
		return loopruntime.RepairResultReceipt{}, false, err
	}
	var refs []repair.ArtifactRef
	if err := json.Unmarshal(encoded, &refs); err != nil {
		return loopruntime.RepairResultReceipt{}, false, err
	}
	if len(refs) == 0 {
		if path, _ := pointer["result_ref"].(string); path != "" {
			refs = append(refs, repair.ArtifactRef{Path: path})
		}
	}
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return loopruntime.RepairResultReceipt{}, false, err
		}
		data, err := workpacket.ReadFile(root, ref.Path, repairResultAnswerLimit)
		if err != nil {
			return loopruntime.RepairResultReceipt{}, false, err
		}
		var result repair.RepairResult
		if err := json.Unmarshal(data, &result); err != nil {
			return loopruntime.RepairResultReceipt{}, false, err
		}
		if result.Receipt != nil && result.Receipt.Provenance.PacketID == packetID && result.Receipt.Provenance.OwnerAgentID == owner {
			return *result.Receipt, true, nil
		}
	}
	return loopruntime.RepairResultReceipt{}, false, nil
}

func recordedRepairResultCandidateReceipt(root string, state map[string]any, packetID, owner string) (loopruntime.RepairResultCandidateReceipt, bool, error) {
	return recordedRepairResultCandidateReceiptContext(context.Background(), root, state, packetID, owner)
}
func recordedRepairResultCandidateReceiptContext(ctx context.Context, root string, state map[string]any, packetID, owner string) (loopruntime.RepairResultCandidateReceipt, bool, error) {
	if err := ctx.Err(); err != nil {
		return loopruntime.RepairResultCandidateReceipt{}, false, err
	}
	packet, found, err := loopruntime.FindRepairResultPacketContext(loopruntime.WithPacketBodyRoot(ctx, root), state, packetID)
	if err != nil || !found {
		return loopruntime.RepairResultCandidateReceipt{}, false, fmt.Errorf("original S9 candidate packet is unavailable: %v", err)
	}
	encoded, err := json.Marshal(state["workspace"])
	if err != nil {
		return loopruntime.RepairResultCandidateReceipt{}, false, err
	}
	var registry workspace.ExecutionRegistry
	if err := json.Unmarshal(encoded, &registry); err != nil {
		return loopruntime.RepairResultCandidateReceipt{}, false, err
	}
	executions, err := repair.RepairResultCandidateExecutions(packet, registry, owner, true)
	if err != nil {
		return loopruntime.RepairResultCandidateReceipt{}, false, err
	}
	for _, execution := range executions {
		if execution.DeliveryRef == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return loopruntime.RepairResultCandidateReceipt{}, false, err
		}
		data, err := workpacket.ReadFile(root, execution.DeliveryRef, repairResultAnswerLimit)
		if err != nil {
			return loopruntime.RepairResultCandidateReceipt{}, false, err
		}
		var candidate repair.WorkspaceCandidate
		if err := json.Unmarshal(data, &candidate); err != nil {
			return loopruntime.RepairResultCandidateReceipt{}, false, err
		}
		if candidate.Provenance == nil || candidate.Provenance.PacketID != packetID || candidate.Provenance.OwnerAgentID != owner {
			continue
		}
		return loopruntime.RepairResultCandidateReceipt{
			Provenance: *candidate.Provenance, CandidateRef: execution.DeliveryRef, CandidateSHA256: execution.DeliverySHA256,
			AcceptedIdempotencyKey: "runtime:s9:issued-repair-candidate:" + packetID,
		}, true, nil
	}
	return loopruntime.RepairResultCandidateReceipt{}, false, nil
}

func sameRepairResultPacket(a, b workpacket.RepairResultPacket) bool {
	left, err := workpacket.RepairResultPacketDigest(a)
	if err != nil {
		return false
	}
	right, err := workpacket.RepairResultPacketDigest(b)
	return err == nil && left == right
}

func hasRepairResultPacket(values []workpacket.RepairResultPacket, expected workpacket.RepairResultPacket) bool {
	for _, packet := range values {
		if packet.ID == expected.ID && sameRepairResultPacket(packet, expected) {
			return true
		}
	}
	return false
}

// These original receipt coordinates are shell arguments, including paths and
// engineering labels with quotes, substitutions or newlines. Go %q is not a
// shell literal and must not reinterpret them when Main copies this command.
func repairResultIntegrateCommand(root, assignment, owner string) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'" }
	return quote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime workspace integrate --root " + quote(root) + " --assignment " + quote(assignment) + " --agent " + quote(owner)
}
