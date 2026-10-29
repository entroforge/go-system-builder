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

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func (s *Service) IssueContracts(ctx context.Context) (workpacket.ContractPacket, error) {
	s = s.WithContext(ctx)
	if err := workspace.RequireMain(s.Root); err != nil {
		return workpacket.ContractPacket{}, err
	}
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.ContractPacket{}, err
	}
	binding, _, err := runtime.CurrentContractBinding(snap.State)
	if err != nil {
		return workpacket.ContractPacket{}, err
	}
	var p workpacket.ContractPacket
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		candidate, found, err := runtime.FindContractPacket(snap.State, text(row["packet_id"]))
		if err != nil {
			return p, err
		}
		if !found || candidate.Binding.RuntimeID != binding.RuntimeID || candidate.Binding.Generation != binding.Generation || candidate.Binding.StageEntry != binding.StageEntry {
			continue
		}
		if p.ID != "" {
			return p, fmt.Errorf("S3 current stage has more than one original questionnaire")
		}
		p = candidate
	}
	if p.ID == "" {
		inputs, err := runtime.ResolveContractInputs(s.Root, snap.State, s.Files)
		if err != nil {
			return p, err
		}
		p, err = workpacket.BuildContracts(binding, inputs)
		if err != nil {
			return p, err
		}
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if _, err := runtime.RecordContractPacket(s.Root, s.StatePath, s.JournalPath, p, s.Files, s.Validator); err != nil {
		return p, err
	}
	view, _ := json.MarshalIndent(p, "", "  ")
	view = append(view, '\n')
	actual, err := workpacket.PublishBounded(s.Root, p.ViewPath, view, int64(len(view)))
	if err != nil {
		return p, err
	}
	if !bytes.Equal(actual, view) {
		return p, fmt.Errorf("published contract view differs from original issuance")
	}
	draft, _ := json.MarshalIndent(p.Draft, "", "  ")
	if _, err := workpacket.PublishBounded(s.Root, p.AnswerPath, append(draft, '\n'), workpacket.ContractAnswerReadLimit(p)); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Service) ContractForPath(path string) (workpacket.ContractPacket, bool, error) {
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.ContractPacket{}, false, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		p, found, err := runtime.FindContractPacket(snap.State, text(row["packet_id"]))
		if err != nil {
			return p, false, err
		}
		if found && filepath.Clean(path) == filepath.Join(s.Root, filepath.FromSlash(p.AnswerPath)) {
			return p, true, workpacket.CheckPath(s.Root, p.AnswerPath)
		}
	}
	return workpacket.ContractPacket{}, false, nil
}

func (s *Service) SaveContracts(ctx context.Context, path string) (Receipt, error) {
	s = s.WithContext(ctx)
	if err := workspace.RequireMain(s.Root); err != nil {
		return Receipt{}, err
	}
	p, issued, err := s.ContractForPath(path)
	if err != nil || !issued {
		return Receipt{}, fmt.Errorf("contract path has no original Main issuance: %v", err)
	}
	snap, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	data, err := workpacket.ReadFile(s.Root, p.AnswerPath, workpacket.ContractAnswerReadLimit(p))
	if err != nil {
		return s.unreceivedContractReceipt(snap, p, err), err
	}
	answer, issues := workpacket.ValidateContractAnswer(p, data)
	if len(issues) > 0 {
		err := fmt.Errorf("contract answer %s", planningFieldDiagnostic(issues[0]))
		r := s.unreceivedContractReceipt(snap, p, err)
		r.FieldErrors = issues
		return r, err
	}
	digest, err := workpacket.ContractAnswerDigest(answer)
	if err != nil {
		return Receipt{}, err
	}
	prior, accepted, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil {
		return Receipt{}, err
	}
	id := "ev-submission-" + workpacket.Digest([]byte("submission:"+p.ID))
	envelopePath := ""
	if accepted {
		if prior.ContentHash != digest {
			return rejected(snap, p.ID, runtime.ErrSubmissionConflict), runtime.ErrSubmissionConflict
		}
		if _, followupErr := runtime.SubmissionFollowup(snap.State, prior.FollowupID); errors.Is(followupErr, runtime.ErrSubmissionFollowupOriginChanged) && (prior.ControlStatus == "pending" || prior.ControlStatus == "no_transition") {
			// Replaying a historical answer cannot resume an outlet whose
			// original entry was replaced by professional planning rework.
			return s.contractReceipt(snap, p)
		}
		id = prior.EvidenceID
		found := false
		for _, ev := range evidenceRows(snap.State) {
			if ev["id"] == id {
				envelopePath = text(ev["path"])
				found = true
				break
			}
		}
		if !found {
			return Receipt{}, fmt.Errorf("original planning evidence missing")
		}
	} else {
		arch, err := runtime.ContractArtifacts(s.Root, snap.State, p, answer, s.Files)
		if err != nil {
			return s.unreceivedContractReceipt(snap, p, err), err
		}
		envelope, err := workpacket.ContractEvidence(p, id, arch, answer, time.Now().UTC())
		if err != nil {
			return Receipt{}, err
		}
		encoded, _ := json.MarshalIndent(envelope, "", "  ")
		encoded = append(encoded, '\n')
		envelopePath = ".claude/evidence/submissions/" + p.ID + "/" + workpacket.Digest(encoded) + ".json"
		stored, err := workpacket.PublishBounded(s.Root, envelopePath, encoded, int64(len(encoded)))
		if err != nil {
			return Receipt{}, err
		}
		if !bytes.Equal(stored, encoded) {
			err := fmt.Errorf("S3 published evidence differs from this reception: %s", envelopePath)
			return s.unreceivedContractReceipt(snap, p, err), err
		}
	}
	if err := ctx.Err(); err != nil {
		return rejected(snap, p.ID, err), err
	}
	next, err := runtime.RecordEvidence(s.Root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{Context: ctx, ExpectedRevision: -1, ID: id, Kind: "planning_contract", Path: envelopePath, ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: "Main", AnswerJSON: data, Files: s.Files, Authorize: func(root string, state map[string]any) error {
		if err := s.validateContractChecks(state, p, answer); err != nil {
			return err
		}
		if verified, ok := s.Files.(interface{ Verify() error }); ok {
			return verified.Verify()
		}
		return nil
	}}})
	if err != nil {
		return s.unreceivedContractReceipt(snap, p, err), err
	}
	RecordPlanningDiagnostic(s.Root, snap.State, "reception", "")
	record, found, err := runtime.FindSubmission(next.State, p.ID)
	if err != nil || !found {
		return Receipt{}, fmt.Errorf("contract reception has no durable original record")
	}
	if record.ControlStatus != "pending" {
		if _, err := runtime.SubmissionFollowup(next.State, record.FollowupID); record.ControlStatus != "no_transition" || err != nil {
			return s.contractReceipt(next, p)
		}
	}
	result, controlErr := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, CWD: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: record.FollowupID, Files: s.Files})
	preparation := ""
	var preparationWaiting []Waiting
	if controlErr == nil && result.Error == "" {
		preparation, preparationWaiting = s.preparePlanningOutlet(ctx, &result)
	}
	if controlErr == nil && result.Error == "" {
		next = result.Snapshot
	}
	r, err := s.contractReceipt(next, p)
	if err != nil {
		return Receipt{}, err
	}
	if controlErr != nil || result.Error != "" {
		r.NextAction = "S3 答卷已接收；Main/系统需恢复原阶段接续，无需重新提交合同判断。"
		if controlErr != nil {
			r.NextAction += " 原因：" + controlErr.Error()
		} else {
			r.NextAction += " 原因：" + result.Error
		}
		r.NextAction += " 恢复入口：" + PlanningRecoveryCommand(s.Root, "recover")
		if len(r.WaitingOn) == 0 {
			r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: record.FollowupID})
		}
	} else if !result.QualityGate.TransitionCommitted && len(result.QualityGate.Missing)+len(result.QualityGate.Conflicts) > 0 {
		r.NextAction = fmt.Sprintf("S3 答卷已接收，阶段尚未推进。原门 %s：缺项 %v；冲突 %v。按具体条目修复文档或系统接续，修复后使用 %s 接续原出口；无需重交已接收答案。", result.QualityGate.GateID, result.QualityGate.Missing, result.QualityGate.Conflicts, PlanningRecoveryCommand(s.Root, "recover"))
	}
	if controlErr != nil || result.Error != "" || len(result.QualityGate.Missing)+len(result.QualityGate.Conflicts) > 0 {
		RecordPlanningDiagnostic(s.Root, snap.State, "outlet", r.NextAction)
	} else if result.QualityGate.TransitionCommitted {
		// Clear the completed S3 entry, not the newly entered S4 diagnostic.
		// A no-op attempt retains its previous cause without nesting that cause
		// inside another copy of the receipt's recovery guidance.
		RecordPlanningDiagnostic(s.Root, snap.State, "outlet", "")
	}
	r.NextAction += preparation
	r.WaitingOn = append(r.WaitingOn, preparationWaiting...)
	return r, nil
}

func (s *Service) validateContractChecks(state map[string]any, p workpacket.ContractPacket, answer workpacket.ContractAnswer) error {
	bound, _ := state["bound_req"].(map[string]any)
	if text(bound["id"]) != p.Binding.Requirement.ID {
		return fmt.Errorf("S3 original checker REQ binding differs")
	}
	paths := append([]string{*answer.IndexPath}, answer.ContractPaths...)
	result, err := semantic.ContractsCheckBatch(s.Root, s.Files, p.Binding.Requirement, paths)
	if err != nil {
		return fmt.Errorf("contracts_checked: %w", err)
	}
	if result.Contracts == 0 || len(result.Problems) != 0 {
		return fmt.Errorf("contracts_checked: %d contracts, problems: %v", result.Contracts, result.Problems)
	}
	return nil
}

func (s *Service) contractReceipt(snap runtime.Snapshot, p workpacket.ContractPacket) (Receipt, error) {
	r := emptyReceipt(snap, p.ID)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil {
		return r, err
	}
	if !found {
		return rejected(snap, p.ID, fmt.Errorf("contract answer not received")), nil
	}
	key := workpacket.Digest([]byte("submission:" + p.ID))
	if record.Owner != "Main" || record.Key != key || record.FollowupID != workpacket.Digest([]byte(key+":after-domain")) || record.OriginState != "planning" || record.OriginPhase != "contracts" || record.OriginEntry != p.Binding.StageEntry {
		return r, fmt.Errorf("contract receipt original responsibility differs")
	}
	for _, ev := range evidenceRows(snap.State) {
		if ev["id"] != record.EvidenceID {
			continue
		}
		data, err := workpacket.ReadFile(s.Root, text(ev["path"]), workpacket.ContractEvidenceReadLimit(p))
		if err != nil || workpacket.Digest(data) != ev["sha256"] || ev["kind"] != "planning_contract" {
			return r, fmt.Errorf("original contract evidence no longer matches")
		}
		var value map[string]json.RawMessage
		json.Unmarshal(data, &value)
		var answer workpacket.ContractAnswer
		if err := json.Unmarshal(value["contract_answer"], &answer); err != nil {
			return r, fmt.Errorf("original S3 professional answer malformed")
		}
		if err := runtime.ValidateContractEnvelope(p, record.EvidenceID, answer, data); err != nil {
			return r, err
		}
		digest, err := workpacket.ContractAnswerDigest(answer)
		if err != nil || digest != record.ContentHash {
			return r, fmt.Errorf("contract receipt professional digest differs")
		}
		r.SubmissionKey = record.Key
		r.Accepted = true
		r.WorkComplete = true
		r.ResponsibilityStatus = "accepted"
		r.DomainRefs = []string{record.EvidenceID}
		r.NextAction = "合同交付已被原消费者接收；Main 按原门继续阶段工作。"
		if baseline, ok := snap.State["baseline"].(map[string]any); !ok || snap.State["runtime_id"] != p.Binding.RuntimeID || number(baseline["generation"]) != p.Binding.Generation {
			r.WorkComplete = false
			r.ResponsibilityStatus = "replaced"
		}
		life, _ := snap.State["lifecycle"].(map[string]any)
		if record.ControlStatus == "replaced" || ev["status"] == "invalid" || ((record.ControlStatus == "pending" || record.ControlStatus == "no_transition") && (life["state"] != "planning" || life["phase"] != "contracts" || number(life["phase_revision"]) != p.Binding.StageEntry)) {
			r.WorkComplete = false
			r.ResponsibilityStatus = "replaced"
		}
		if !r.WorkComplete {
			r.NextAction = "这是旧代或已替换的 S3 接收记录；原接收事实保留，按当前责任给出的问卷继续，不重交或恢复此旧卷。"
			return r, nil
		}
		if _, err := runtime.SubmissionFollowup(snap.State, record.FollowupID); err == nil {
			r.NextAction = "S3 答卷已接收，阶段接续尚未完成；Main/系统处理具体缺项，然后使用 " + PlanningRecoveryCommand(s.Root, "recover") + " 接续原出口，无需重交答案。" + PlanningDiagnostic(s.Root, snap.State, "outlet")
			r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: record.FollowupID})
		}
		return r, nil
	}
	return r, fmt.Errorf("contract receipt original domain reference missing")
}

func (s *Service) ContractReceipt(path, packetID string) (Receipt, error) {
	snap, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	var p workpacket.ContractPacket
	var found bool
	if packetID != "" {
		p, found, err = runtime.FindContractPacket(snap.State, packetID)
	} else {
		p, found, err = s.ContractForPath(path)
	}
	if err != nil || !found {
		return Receipt{}, fmt.Errorf("no original contract issuance")
	}
	return s.contractReceipt(snap, p)
}

func (s *Service) MainPlanningReceipt(path, packetID string) (Receipt, error) {
	if packetID != "" {
		snap, err := s.snapshot()
		if err != nil {
			return Receipt{}, err
		}
		if _, found, err := runtime.FindTaskPlanningPacket(snap.State, packetID); err != nil {
			return Receipt{}, err
		} else if found {
			return s.TaskPlanningReceipt(path, packetID)
		}
		if _, found, err := runtime.FindContractPacket(snap.State, packetID); err != nil {
			return Receipt{}, err
		} else if found {
			return s.ContractReceipt(path, packetID)
		}
	} else if _, found, err := s.TaskPlanningForPath(path); err != nil {
		return Receipt{}, err
	} else if found {
		return s.TaskPlanningReceipt(path, "")
	} else if _, found, err := s.ContractForPath(path); err != nil {
		return Receipt{}, err
	} else if found {
		return s.ContractReceipt(path, "")
	}
	return s.DesignReceipt(path, packetID)
}

// unreceivedContractReceipt keeps output/source errors on the current answer.
// Named revision applies only to an already accepted judgment.
func (s *Service) unreceivedContractReceipt(snapshot runtime.Snapshot, p workpacket.ContractPacket, err error) Receipt {
	RecordPlanningDiagnostic(s.Root, snapshot.State, "reception", err.Error())
	r := rejected(snapshot, p.ID, err)
	r.NextAction = "本次 S3 答卷尚未接收。按具体错误修正专业文件或答卷，再保存原路径 " + filepath.Join(s.Root, filepath.FromSlash(p.AnswerPath)) + "；保留本次所需的专业修复。"
	return r
}

func ContractDeliveryGuidance(root string, packet workpacket.ContractPacket, files fileview.Reader, state map[string]any) string {
	id := strings.TrimPrefix(packet.Binding.Requirement.ID, "REQ-")
	paths := []string{"docs/dev/contracts/CONTRACTS-" + id + ".md", "docs/dev/contracts/FE-" + id + ".md", "docs/dev/contracts/BE-" + id + ".md", "docs/dev/contracts/SYNC-" + id + ".md"}
	// Once the author has named the actual delivery, show that exact set.
	// Before then these are conventional starting paths, not an applicability
	// decision or a requirement to produce all three contract families.
	label := "Conventional output candidates (choose contract IDs and FE/BE/SYNC applicability from the frozen scope)"
	fieldDiagnostic := ""
	if data, err := workpacket.ReadFile(root, packet.AnswerPath, workpacket.ContractAnswerReadLimit(packet)); err == nil {
		if namedPaths, named := workpacket.ContractDraftDeliveryPaths(packet, data); named {
			paths = namedPaths
			label = "Your named contract delivery (safe draft paths; reception still requires the complete answer)"
		}
		if _, issues := workpacket.ValidateContractAnswer(packet, data); len(issues) > 0 {
			fieldDiagnostic = " Current answer field: " + planningFieldDiagnostic(issues[0]) + "."
		}
	}
	sourceHint := planningDeclaredSourceGuidance(root, paths, state, "S3", false)
	if view, ok := files.(*fileview.View); ok {
		var committed []string
		for _, path := range paths {
			if source, err := view.Source(path); err == nil && source == "git_tree" {
				committed = append(committed, filepath.Join(root, filepath.FromSlash(path)))
			}
		}
		if len(committed) > 0 {
			sourceHint = fmt.Sprintf("\nS3 outputs can change in this entry. Before saving this answer, commit the applicable completed Git-sourced professional files to bound development branch %s: %s; include their Git-sourced model/example dependencies. REQ/S2 inputs stay frozen. The answer and Runtime use their supplied disk paths.", strings.TrimPrefix(view.Ref, "refs/heads/"), strings.Join(committed, ", "))
		}
	}
	for index, path := range paths {
		paths[index] = filepath.Join(root, filepath.FromSlash(path))
	}
	answerHint := " Tool file paths above are absolute. In answer.index_path and answer.contract_paths, copy repository-relative values without the root prefix; for example index_path=\"docs/dev/contracts/CONTRACTS-SERVICE-009.md\", contract_paths=[\"docs/dev/contracts/BE-SERVICE-009.md\"]. Choose actual IDs from the frozen scope; these examples do not mandate IDs or applicability."
	if strings.HasPrefix(label, "Your named") {
		answerHint += planningAnswerPathValues(root, paths, "contract_paths", "CONTRACTS-")
	}
	templates := []string{"CONTRACTS-template.md", "FE-contract-template.md", "BE-contract-template.md", "SYNC-contract-template.md"}
	for index, name := range templates {
		templates[index] = filepath.Join(root, "docs/dev/contracts", name)
	}
	return fmt.Sprintf("\n%s: %s. Templates: %s. Determine FE/BE/SYNC and shared-data applicability; align shared models and SYNC only where applicable, and complete the applicable contracts and index; deliver at least one real applicable contract; when FE/BE/SYNC are all not_applicable, report the stage adaptation gap instead of inventing a BE contract. Read %s for professional method, then save the professional answer once. The original consumer runs contracts check and scenario bridge; manual check commands support self-review or diagnostics, not additional submissions.%s", label, strings.Join(paths, ", "), strings.Join(templates, ", "), filepath.Join(root, ".claude/skills/specification-planning/SKILL.md"), sourceHint+answerHint+fieldDiagnostic+" Human decisions are required only for REQ semantics or decisions outside the existing authorization; Main owns contract decomposition, contract IDs and applicability within the frozen scope."+PlanningProfessionalReworkGuidance(root))
}

// Source declarations and the bound branch suffice for delivery hints. Ordinary
// observation never resolves a Git tree or revalidates the frozen source batch.
func planningDeclaredSourceGuidance(root string, paths []string, state map[string]any, stage string, accepted bool) string {
	if state == nil {
		return ""
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return " Source declaration unavailable: " + err.Error()
	}
	ref, err := fileview.DevelopmentRef(state)
	if err != nil {
		return " Bound delivery branch unavailable: " + err.Error()
	}
	var gitPaths, diskPaths []string
	for _, path := range paths {
		source, sourceErr := fileview.SourceForRules(root, path, catalog.Definition.FileSources)
		if sourceErr != nil {
			return " Source declaration unavailable: " + sourceErr.Error()
		}

		switch source {
		case "git_tree":
			gitPaths = append(gitPaths, filepath.Join(root, path))
		case "disk":
			diskPaths = append(diskPaths, filepath.Join(root, path))
		default:
			return " No declared formal source for " + path + "; Main must repair this source declaration."
		}
	}
	hint := ""
	if accepted {
		if len(gitPaths) > 0 {
			hint = fmt.Sprintf(" Recorded Git sources on bound development branch %s: %s.", strings.TrimPrefix(ref, "refs/heads/"), strings.Join(gitPaths, ", "))
		}
		if len(diskPaths) > 0 {
			hint += " Recorded disk sources: " + strings.Join(diskPaths, ", ") + "."
		}
		return hint + " Preserve or restore the original accepted content. Genuine professional changes use the formal stage rework/revision path; system continuation uses the original recover command."
	}
	if len(gitPaths) > 0 {
		upstream := "REQ/S2"
		if stage == "S4" {
			upstream = "REQ/S3 and other upstream"
		}
		hint = fmt.Sprintf("\n%s outputs can change in this entry. Before saving this answer, commit applicable completed Git-sourced files to bound development branch %s: %s; include their Git-sourced model/example dependencies. %s inputs stay frozen. The answer and Runtime use their supplied disk paths.", stage, strings.TrimPrefix(ref, "refs/heads/"), strings.Join(gitPaths, ", "), upstream)
	}
	if len(diskPaths) > 0 {
		hint += " Declared disk outputs: " + strings.Join(diskPaths, ", ") + "."
	}

	return hint
}

// Accepted deliveries are read from the recorded evidence, never reconstructed
// from a possibly edited author answer or from the complete source closure.
func AcceptedPlanningDeliveryGuidance(root string, state map[string]any, record runtime.SubmissionRecord, stage string) string {
	var limit int64
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		if text(row["packet_id"]) != record.PacketID {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return " Original accepted questionnaire cannot be read: " + err.Error()
		}
		if stage == "S3" {
			var packet workpacket.ContractPacket
			if err := json.Unmarshal(data, &packet); err != nil {
				return " Original accepted questionnaire cannot be read: " + err.Error()
			}
			limit = workpacket.ContractEvidenceReadLimit(packet)
		} else {
			var packet workpacket.TaskPlanningPacket
			if err := json.Unmarshal(data, &packet); err != nil {
				return " Original accepted questionnaire cannot be read: " + err.Error()
			}
			limit = workpacket.TaskPlanningEvidenceReadLimit(packet)
		}
		break
	}
	if limit == 0 {
		return " Original accepted questionnaire is unavailable; Main must restore its registered record."
	}
	for _, ev := range evidenceRows(state) {
		if text(ev["id"]) != record.EvidenceID {
			continue
		}
		path := text(ev["path"])
		data, err := workpacket.ReadFile(root, path, limit)
		if err != nil {
			return " Accepted delivery record unavailable at " + filepath.Join(root, path) + ": " + err.Error()
		}
		var envelope struct {
			Contracts workpacket.ContractAnswer     `json:"contract_answer"`
			Tasks     workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return " Accepted delivery record unreadable at " + filepath.Join(root, path) + ": " + err.Error()
		}
		var paths, display []string
		if stage == "S3" && envelope.Contracts.IndexPath != nil {
			paths = append([]string{*envelope.Contracts.IndexPath}, envelope.Contracts.ContractPaths...)
		}
		if stage == "S4" && envelope.Tasks.IndexPath != nil {
			paths = append([]string{*envelope.Tasks.IndexPath}, envelope.Tasks.TaskPaths...)
		}
		if len(paths) == 0 {
			return " Accepted delivery paths are unavailable in original record " + filepath.Join(root, path) + "; Main must inspect that record without resubmitting the answer."
		}
		for _, output := range paths {
			display = append(display, filepath.Join(root, output))
		}

		hint := planningDeclaredSourceGuidance(root, paths, state, stage, true)
		return " Recorded accepted delivery: " + strings.Join(display, ", ") + ". Full accepted source references remain in " + filepath.Join(root, path) + "." + hint + PlanningProfessionalReworkGuidance(root)
	}
	return " Accepted delivery reference is missing for " + record.EvidenceID + "; Main must restore the original record."
}
