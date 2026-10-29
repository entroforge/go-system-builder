package submission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/controller"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type Waiting struct {
	Reason string `json:"reason"`
	Owner  string `json:"owner"`
	Ref    string `json:"ref"`
}

type Receipt struct {
	SubmissionKey string `json:"submission_key,omitempty"`
	PacketID      string `json:"packet_id"`
	Accepted      bool   `json:"accepted"`
	// AttemptStatus describes the current save attempt separately from durable
	// acceptance. Only S4 rejected attempts currently populate this field.
	AttemptStatus        string                  `json:"attempt_status,omitempty"`
	ResponsibilityStatus string                  `json:"responsibility_status"`
	WorkComplete         bool                    `json:"work_complete"`
	ConsumerOwner        string                  `json:"consumer_owner,omitempty"`
	ConsumerPending      bool                    `json:"consumer_pending,omitempty"`
	CurrentStage         string                  `json:"current_stage"`
	DomainRefs           []string                `json:"domain_refs"`
	WaitingOn            []Waiting               `json:"waiting_on"`
	NextAction           string                  `json:"next_action"`
	FieldErrors          []workpacket.FieldError `json:"field_errors"`
	Error                string                  `json:"error,omitempty"`
	// Set only by the original S9 blocker proof projection, never decoded from
	// authored JSON. Historical acceptance alone does not prove a current block.
	currentRepairResultBlocker bool
}

// Receipt reconstructs completion from the durable domain reception, never
// from the answer file, a cached receipt, or a completion-shaped message.
func (s *Service) Receipt(owner, path string) (Receipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.ReceiptContext(ctx, owner, path)
}

func (s *Service) ReceiptContext(ctx context.Context, owner, path string) (Receipt, error) {
	return s.receiptContext(ctx, owner, path, false)
}

// DiagnoseReceiptContext is the explicit read-only receipt command. Unlike
// ordinary receipt projections, it diagnoses an unreceived answer and its
// original sources. It never calls Save or records completion.
func (s *Service) DiagnoseReceiptContext(ctx context.Context, owner, path string) (Receipt, error) {
	reader := *s
	reader.readOnly = true
	return reader.receiptContext(ctx, owner, path, true)
}

func (s *Service) receiptContext(ctx context.Context, owner, path string, diagnose bool) (Receipt, error) {
	s = s.WithContext(ctx)
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	p, source, err := s.source(ctx, snapshot.State, owner, path)
	if errors.Is(err, runtime.ErrSubmissionSuperseded) {
		return s.ReceiptPacket(owner, p.ID)
	}
	if err != nil {
		if diagnose {
			r := rejected(snapshot, p.ID, err)
			r.NextAction = "Main 按具体错误恢复原责任的准确来源或路径；此只读诊断未接收答案，不改用其他来源。"
			return r, err
		}
		return rejected(snapshot, "", err), err
	}
	r, ok, err := runtime.FindSubmission(snapshot.State, p.ID)
	if err != nil {
		return Receipt{}, err
	}
	if !ok {
		if diagnose {
			data, err := workpacket.ReadFile(source.Root, source.Path, 256<<10)
			if err != nil {
				return rejected(snapshot, p.ID, err), err
			}
			if _, fields := workpacket.ValidateAnswer(p, data); len(fields) > 0 {
				err := &workpacket.AnswerError{Label: "S5 answer", Fields: fields}
				r := rejected(snapshot, p.ID, err)
				r.FieldErrors = fields
				r.NextAction = "仅按实际审查判断修正原答案；真实独立性或来源阻塞交 Main 恢复，不能为退出编造结论。此诊断未接收答案。"
				return r, err
			}
			if err := runtime.ValidateWorkPacket(s.Root, snapshot.State, p, s.Files); err != nil {
				r := rejected(snapshot, p.ID, err)
				r.NextAction = "Main 按具体错误恢复原责任和准确来源，再续跑原审查；此只读诊断未接收答案。"
				return r, err
			}
			if err := validateS5ApprovalMode(s.Root, snapshot.State, owner); err != nil {
				return rejected(snapshot, p.ID, err), err
			}
			r := rejected(snapshot, p.ID, fmt.Errorf("answer has not been accepted"))
			r.NextAction = "当前答案字段及原来源检查未发现缺项；这不是接收或专业质量证明。Main 结合实际原生执行确认原 Stop 是否完成，不凭此诊断代收或重派。"
			return r, nil
		}
		return rejected(snapshot, p.ID, fmt.Errorf("answer has not been accepted")), nil
	}
	return receipt(s.Root, snapshot, p, r)
}

// ReceiptPacket reads an original historical reception without restoring its source.
func (s *Service) ReceiptPacket(owner, packetID string) (Receipt, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	p, ok, err := runtime.FindWorkPacket(snapshot.State, packetID)
	if err != nil || !ok || p.Binding.Owner != owner {
		return Receipt{}, fmt.Errorf("historical packet has no matching registered owner")
	}
	record, ok, err := runtime.FindSubmission(snapshot.State, p.ID)
	if err != nil {
		return Receipt{}, err
	}
	if !ok {
		return rejected(snapshot, p.ID, fmt.Errorf("answer has not been accepted")), nil
	}
	return receipt(s.Root, snapshot, p, record)
}

// CompletedOwner is the Stop/Idle projection. Pending system work or another
// reviewer never turns a received deliverable back into this owner's task.
func CompletedOwner(root, owner string) bool {
	if owner == "" {
		return false
	}
	s := &Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
	snapshot, err := s.snapshot()
	if err != nil || !runtime.PacketEnabled(snapshot.State) {
		return false
	}
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["owner"] != owner {
			continue
		}
		packetID := text(row["packet_id"])
		if skipKnownNonS5WorkPacket(snapshot.State, packetID) {
			continue
		}
		p, ok, err := runtime.FindWorkPacket(snapshot.State, packetID)
		if err != nil || !ok {
			return false
		}
		if life["state"] == "document_verification" && number(life["phase_revision"]) != p.Binding.StageEntry {
			continue
		}
		record, ok, err := runtime.FindSubmission(snapshot.State, p.ID)
		if err != nil || !ok {
			continue
		}
		if r, err := receipt(root, snapshot, p, record); err == nil && r.WorkComplete {
			return true
		}
	}
	return false
}

func emptyReceipt(snapshot runtime.Snapshot, id string) Receipt {
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	stage, _ := runtime.StageFor(text(life["state"]), text(life["phase"]), "")
	if stage == "" {
		stage = text(life["state"])
	}
	return Receipt{PacketID: id, CurrentStage: stage, DomainRefs: []string{}, WaitingOn: []Waiting{}, FieldErrors: []workpacket.FieldError{}}
}

func rejected(snapshot runtime.Snapshot, id string, err error) Receipt {
	r := emptyReceipt(snapshot, id)
	r.ResponsibilityStatus = "not_accepted"
	r.NextAction = "按本次错误修正原答卷；已接收的内容变更须由 Main 走具名修订或原返工路由。"
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

func receipt(root string, snapshot runtime.Snapshot, p workpacket.Packet, r runtime.SubmissionRecord) (Receipt, error) {
	current, ok, err := runtime.FindSubmission(snapshot.State, p.ID)
	if err != nil || !ok {
		return Receipt{}, fmt.Errorf("cannot reconstruct durable receipt: %v", err)
	}
	r = current
	result := emptyReceipt(snapshot, p.ID)
	result.SubmissionKey = r.Key
	if r.Owner != p.Binding.Owner {
		return result, fmt.Errorf("reception owner differs from issued packet")
	}
	var envelope workpacket.DocumentReviewEnvelope
	found := false
	for _, ev := range evidenceRows(snapshot.State) {
		if ev["id"] != r.EvidenceID {
			continue
		}
		data, err := workpacket.ReadFile(root, text(ev["path"]), 1<<20)
		if err != nil {
			return result, err
		}
		if workpacket.Digest(data) != ev["sha256"] || ev["kind"] != "document_review" {
			return result, fmt.Errorf("accepted domain evidence no longer matches its registered artifact")
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return result, err
		}
		answer := workpacket.Answer{Conclusion: &envelope.Conclusion, Findings: envelope.Findings}
		if err := workpacket.ValidateDocumentReview(p, r.EvidenceID, answer, data); err != nil {
			return result, err
		}
		hash, err := workpacket.AnswerDigest(answer)
		if err != nil || hash != r.ContentHash {
			return result, fmt.Errorf("accepted domain evidence differs from received answer")
		}
		result.DomainRefs = append(result.DomainRefs, r.EvidenceID)
		result.Accepted = true
		// Original gates may consume or invalidate a FIX during routing.
		// That affects its qualification, not the author's received work.
		result.WorkComplete = true
		found = true
		break
	}
	if !found {
		return result, fmt.Errorf("accepted reception has no domain evidence")
	}
	result.ResponsibilityStatus = "accepted"
	result.NextAction = "本人交付已接收，可以结束本责任；后续由指定 owner 处理。"
	if r.RevisionPacketID != "" {
		result.ResponsibilityStatus = "replaced"
		result.WorkComplete = false
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "named_revision", Owner: p.Binding.Owner, Ref: r.RevisionPacketID})
		result.NextAction = "旧交付已留存；完成 Main 发行的当前修订答卷。"
		return result, nil
	}
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	if snapshot.State["runtime_id"] != p.Binding.RuntimeID || number(baseline["generation"]) != p.Binding.Generation || !result.WorkComplete {
		result.ResponsibilityStatus = "replaced"
		result.WorkComplete = false
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "rework_or_generation_changed", Owner: "Main", Ref: r.Key})
		result.NextAction = "由 Main 按当前基线和原返工路由重新派发；旧回执不授权新工作。"
		return result, nil
	}
	if r.ControlStatus == "pending" {
		result.ConsumerOwner, result.ConsumerPending = "main", true
		result.NextAction = "本人审查交付已接收，可以结束本责任；阶段出口接续尚未完成，Main 检查原 system_followup " + r.FollowupID + " 的具体缺项，沿原接续恢复；技术中断可用已有 runtime stage-answer recover，无需重新提交审查答案。"
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: r.FollowupID})
	}
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	if life["state"] != "document_verification" || number(life["phase_revision"]) != p.Binding.StageEntry {
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "next_stage_dispatch", Owner: "Main", Ref: result.CurrentStage})
		return result, nil
	}
	if envelope.Conclusion == "req_change_required" {
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "explicit_requirement_decision", Owner: "human", Ref: r.EvidenceID})
	}
	packets, err := runtime.S5Packets(root, snapshot.State, "")
	if err != nil {
		result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "dispatch_projection_unavailable", Owner: "Main", Ref: "current_dispatch"})
		return result, nil
	}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		assigned := false
		for _, other := range packets {
			if other.Binding.Responsibility != responsibility {
				continue
			}
			assigned = true
			if otherRecord, accepted, err := runtime.FindSubmission(snapshot.State, other.ID); err != nil {
				return result, err
			} else if !accepted {
				result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "responsibility_not_received", Owner: other.Binding.Owner, Ref: other.ID})
			} else if other.ID != p.ID {
				for _, ev := range evidenceRows(snapshot.State) {
					if ev["id"] != otherRecord.EvidenceID {
						continue
					}
					data, err := workpacket.ReadFile(root, text(ev["path"]), 1<<20)
					if err != nil || workpacket.Digest(data) != ev["sha256"] {
						result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "peer_evidence_unavailable", Owner: "Main", Ref: otherRecord.EvidenceID})
						break
					}
					var peer workpacket.DocumentReviewEnvelope
					if json.Unmarshal(data, &peer) == nil && peer.Conclusion == "req_change_required" {
						result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "explicit_requirement_decision", Owner: "human", Ref: otherRecord.EvidenceID})
					}
					break
				}
			}
		}
		if !assigned {
			result.WaitingOn = append(result.WaitingOn, Waiting{Reason: "responsibility_not_dispatched", Owner: "Main", Ref: responsibility})
		}
	}
	return result, nil
}

// Recover advances at most limit pending domain receptions. It does not
// consume drafts or infer authorization, and never asks the author to retry
// work that already has a durable reception.
func (s *Service) Recover(ctx context.Context, owner string, limit int) ([]Receipt, error) {
	s = s.WithContext(ctx)
	if limit < 1 || limit > 2 {
		return nil, fmt.Errorf("stage-answer recovery limit must be 1 or 2")
	}
	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	if !runtime.PacketEnabled(snapshot.State) {
		return nil, nil
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	results := []Receipt{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if owner != "" && row["owner"] != owner {
			continue
		}
		if row["origin_state"] == "planning" {
			if _, followupErr := runtime.SubmissionFollowup(snapshot.State, text(row["followup_event_id"])); errors.Is(followupErr, runtime.ErrSubmissionFollowupOriginChanged) || errors.Is(followupErr, runtime.ErrSubmissionFollowupProcessed) {
				// A new planning entry preserves old receptions as history.
				// They must neither block recovery of current work nor consume
				// the bounded recovery budget with an obsolete outlet.
				continue
			}
		}
		if row["control_status"] != "pending" {
			// Compatibility for S2/S3/S4 answers received before not-ready gates
			// retained their followup. Never revive a historical stage entry.
			if row["control_status"] != "no_transition" {
				continue
			}
			if _, found, err := runtime.FindContractPacket(snapshot.State, text(row["packet_id"])); err != nil {
				return results, err
			} else if !found {
				if _, design, err := runtime.FindDesignPacket(snapshot.State, text(row["packet_id"])); err != nil {
					return results, err
				} else if !design {
					if _, tasks, err := runtime.FindTaskPlanningPacket(snapshot.State, text(row["packet_id"])); err != nil {
						return results, err
					} else if !tasks {
						continue
					}
				}
			}
			if _, err := runtime.SubmissionFollowup(snapshot.State, text(row["followup_event_id"])); err != nil {
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if taskPlanning, found, err := runtime.FindTaskPlanningPacket(snapshot.State, text(row["packet_id"])); err != nil {
			return results, err
		} else if found {
			if owner != "" && owner != "Main" {
				continue
			}
			result, controlErr := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, CWD: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: text(row["followup_event_id"]), Files: s.Files})
			r, err := s.taskPlanningControlReceipt(snapshot, taskPlanning, result, controlErr)
			if err != nil {
				return results, err
			}

			results = append(results, r)
			if len(results) == limit {
				break
			}
			continue
		}
		if contract, found, err := runtime.FindContractPacket(snapshot.State, text(row["packet_id"])); err != nil {
			return results, err
		} else if found {
			if owner != "" && owner != "Main" {
				continue
			}
			result, err := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, CWD: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: text(row["followup_event_id"]), Files: s.Files})
			if err != nil {
				RecordPlanningDiagnostic(s.Root, snapshot.State, "outlet", err.Error())
				return results, err
			}
			preparation, preparationWaiting := s.preparePlanningOutlet(ctx, &result)
			latest, err := s.snapshot()
			if err != nil {
				return results, err
			}
			r, err := s.contractReceipt(latest, contract)
			if err != nil {
				return results, err
			}
			if result.Error != "" {
				r.NextAction = "Main 恢复原 contract followup；无需重新提交专业判断。原因：" + result.Error + "；修复后使用 " + PlanningRecoveryCommand(s.Root, "recover")
			}
			if result.Error == "" && !result.QualityGate.TransitionCommitted && len(result.QualityGate.Missing)+len(result.QualityGate.Conflicts) > 0 {
				r.NextAction = fmt.Sprintf("S3 答卷已接收，阶段尚未推进。原门 %s：缺项 %v；冲突 %v。修复后使用 %s 接续原出口；无需重交答案。", result.QualityGate.GateID, result.QualityGate.Missing, result.QualityGate.Conflicts, PlanningRecoveryCommand(s.Root, "recover"))
			}
			if result.Error != "" || len(result.QualityGate.Missing)+len(result.QualityGate.Conflicts) > 0 {
				RecordPlanningDiagnostic(s.Root, snapshot.State, "outlet", r.NextAction)
			} else if result.QualityGate.TransitionCommitted {
				RecordPlanningDiagnostic(s.Root, snapshot.State, "outlet", "")
			}
			r.NextAction += preparation
			r.WaitingOn = append(r.WaitingOn, preparationWaiting...)
			results = append(results, r)
			if len(results) == limit {
				break
			}
			continue
		}
		if design, found, err := runtime.FindDesignPacket(snapshot.State, text(row["packet_id"])); err != nil {
			return results, err
		} else if found {
			if owner != "" && owner != "Main" {
				continue
			}
			record, _, err := runtime.FindSubmission(snapshot.State, design.ID)
			if err != nil {
				return results, err
			}
			r, err := s.continueDesignOutlet(ctx, snapshot, design, record)
			if err != nil {
				return results, err
			}
			results = append(results, r)
			if len(results) == limit {
				break
			}
			continue
		}
		if skipKnownNonS5WorkPacket(snapshot.State, text(row["packet_id"])) {
			continue
		}
		p, ok, err := runtime.FindWorkPacket(snapshot.State, text(row["packet_id"]))
		if err != nil || !ok {
			return results, fmt.Errorf("pending reception has no packet: %v", err)
		}
		result, err := s.control(ctx, text(row["followup_event_id"]), p.Binding.Owner)
		if err != nil {
			return results, err
		}
		latest, err := s.snapshot()
		if err != nil {
			return results, err
		}
		record, _, err := runtime.FindSubmission(latest.State, p.ID)
		if err != nil {
			return results, err
		}
		r, err := receipt(s.Root, latest, p, record)
		if err != nil {
			return results, err
		}
		if result.Error == "" && result.QualityGate.TransitionCommitted && result.QualityGate.CandidateTransition == "TR-004" {
			if design, err := runtime.CurrentDesignPacket(latest.State); err == nil {
				r.NextAction = "完成本轮实际设计后保存当前S2答卷：" + filepath.Join(s.Root, design.AnswerPath)
			}
		}
		if result.Error != "" {
			r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: record.FollowupID})
			if result.QualityGate.TransitionCommitted && result.QualityGate.CandidateTransition == "TR-004" {
				r.NextAction = result.Error
			}
		}
		results = append(results, r)
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

func number(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return -1
}
