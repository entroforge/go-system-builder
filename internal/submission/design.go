package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func (s *Service) IssueDesign(ctx context.Context) (workpacket.DesignPacket, error) {
	s = s.WithContext(ctx)
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.DesignPacket{}, err
	}
	p, err := runtime.CurrentDesignPacket(snap.State)
	if err != nil {
		return p, err
	}
	// Recovery uses the original issued template, even after a binary update.
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	found := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s2-design-v1" {
			continue
		}
		original, exists, readErr := runtime.FindDesignPacket(snap.State, text(row["packet_id"]))
		if readErr != nil {
			return p, readErr
		}
		if !exists || runtime.ValidateDesignPacket(s.Root, snap.State, original, nil) != nil {
			continue
		}
		if found {
			return p, fmt.Errorf("multiple design questionnaires belong to the current entry")
		}
		p, found = original, true
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if _, err := runtime.RecordDesignPacket(s.Root, s.StatePath, s.JournalPath, p, s.Files, s.Validator); err != nil {
		return p, err
	}
	view, _ := json.MarshalIndent(p, "", "  ")
	view = append(view, '\n')
	actual, err := workpacket.Publish(s.Root, p.ViewPath, view)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(actual, view) {
		return p, fmt.Errorf("published design view differs from original issuance")
	}
	if _, accepted, err := runtime.FindSubmission(snap.State, p.ID); err != nil {
		return p, err
	} else if accepted {
		if _, err := workpacket.ReadFile(s.Root, p.AnswerPath, 256<<10); err != nil {
			return p, fmt.Errorf("accepted design answer is unavailable at %s: %w; restore its original authored bytes, not a blank draft", filepath.Join(s.Root, p.AnswerPath), err)
		}
		return p, nil
	}
	draft, _ := json.MarshalIndent(p.Draft, "", "  ")
	if _, err := workpacket.Publish(s.Root, p.AnswerPath, append(draft, '\n')); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Service) DesignForPath(path string) (workpacket.DesignPacket, bool, error) {
	snap, err := s.snapshot()
	if err != nil {
		return workpacket.DesignPacket{}, false, err
	}
	return s.designForPath(snap, path)
}

func (s *Service) designForPath(snap runtime.Snapshot, path string) (workpacket.DesignPacket, bool, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s2-design-v1" {
			continue
		}
		if filepath.Clean(path) != filepath.Join(s.Root, filepath.FromSlash(text(row["answer_path"]))) {
			continue
		}
		p, found, err := runtime.FindDesignPacket(snap.State, text(row["packet_id"]))
		return p, found, err
	}
	return workpacket.DesignPacket{}, false, nil
}

func (s *Service) SaveDesign(ctx context.Context, path string) (Receipt, error) {
	s = s.WithContext(ctx)
	snap, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	p, issued, err := s.designForPath(snap, path)
	if err != nil || !issued {
		return Receipt{}, fmt.Errorf("design path does not locate its registered responsibility: %v", err)
	}
	data, err := workpacket.ReadFile(s.Root, p.AnswerPath, 256<<10)
	if err != nil {
		return rejected(snap, p.ID, err), err
	}
	answer, issues := workpacket.ValidateDesignAnswer(p, data)
	if len(issues) > 0 {
		err := fmt.Errorf("design answer %s: %s", issues[0].Pointer, issues[0].Expected)
		r := rejected(snap, p.ID, err)
		r.FieldErrors = issues
		return r, err
	}
	prior, accepted, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil {
		return Receipt{}, err
	}
	digest, err := workpacket.DesignAnswerDigest(answer)
	if err != nil {
		return Receipt{}, err
	}
	if accepted && prior.ContentHash != digest {
		return rejected(snap, p.ID, runtime.ErrSubmissionConflict), runtime.ErrSubmissionConflict
	}
	next := snap
	if !accepted {
		if err := runtime.ValidateDesignPacket(s.Root, snap.State, p, s.Files); err != nil {
			return rejected(snap, p.ID, err), err
		}
		var missing []string
		if answer.Conclusion == nil || strings.TrimSpace(*answer.Conclusion) == "" {
			missing = append(missing, "complete the actual design self-review; leave conclusion empty while unfinished")
		}
		if answer.ArchitecturePath == nil || *answer.ArchitecturePath == "" {
			missing = append(missing, "provide the actual architecture_path")
		}
		var arch workpacket.Reference
		if answer.ArchitecturePath != nil && *answer.ArchitecturePath != "" {
			arch, err = runtime.DesignArchitecture(s.Root, *answer.ArchitecturePath, nil)
			if err != nil {
				missing = append(missing, err.Error())
			}
		}
		if len(missing) > 0 {
			r := emptyReceipt(snap, p.ID)
			r.NextAction = "Continue the current design answer at " + filepath.Join(s.Root, p.AnswerPath) + ": " + strings.Join(missing, "; ") + ". No design decision was recorded."
			return r, nil
		}
		id := "ev-submission-" + workpacket.Digest([]byte("submission:"+p.ID))
		envelopePath := ".claude/evidence/submissions/" + p.ID + "/" + digest + ".json"
		envelope, err := workpacket.DesignEvidence(p, id, arch, answer, time.Now().UTC())
		if err != nil {
			return Receipt{}, err
		}
		encoded, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return Receipt{}, err
		}
		if _, err := workpacket.Publish(s.Root, envelopePath, append(encoded, '\n')); err != nil {
			return Receipt{}, err
		}
		if err := ctx.Err(); err != nil {
			return rejected(snap, p.ID, err), err
		}
		next, err = runtime.RecordEvidence(s.Root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{Context: ctx, ExpectedRevision: -1, ID: id, Kind: "planning_design", Path: envelopePath, ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: "Main", AnswerJSON: data}})
		if err != nil {
			return rejected(snap, p.ID, err), err
		}
		prior, accepted, err = runtime.FindSubmission(next.State, p.ID)
		if err != nil || !accepted {
			return Receipt{}, fmt.Errorf("design reception has no durable record")
		}
	}
	return s.continueDesignOutlet(ctx, next, p, prior)
}

// S2 reception and explicit recovery use the same disk design authority.
// Only the downstream S3 preparation introduces its committed Git source.
func (s *Service) continueDesignOutlet(ctx context.Context, snap runtime.Snapshot, p workpacket.DesignPacket, prior runtime.SubmissionRecord) (Receipt, error) {
	next := snap
	if (prior.ControlStatus != "pending" && prior.ControlStatus != "no_transition") || runtime.ValidateDesignPacket(s.Root, next.State, p, nil) != nil {
		return s.designReceipt(next, p)
	}
	result, controlErr := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, CWD: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: prior.FollowupID})
	preparation := ""
	var preparationWaiting []Waiting
	if controlErr == nil && result.Error == "" {
		preparation, preparationWaiting = s.preparePlanningOutlet(ctx, &result)
	}
	if controlErr == nil && result.Error == "" {
		next = result.Snapshot
	}
	RecordPlanningDiagnostic(s.Root, snap.State, "outlet", "")
	r, err := s.designReceipt(next, p)
	if err != nil {
		return Receipt{}, err
	}
	if controlErr != nil || result.Error != "" {
		detail := result.Error
		if controlErr != nil {
			detail = controlErr.Error()
		}
		r.NextAction = "Design was recorded; the original stage outlet remains pending: " + detail + ". Restore the named material or use " + PlanningRecoveryCommand(s.Root, "recover") + " for this interrupted outlet; do not repeat the design report."
	}
	if len(result.QualityGate.Missing) > 0 || len(result.QualityGate.Conflicts) > 0 {
		r.NextAction = "Design report was recorded. Complete the actual current design/material gaps before progressing: " + strings.Join(append(result.QualityGate.Missing, result.QualityGate.Conflicts...), "; ") + ". Keep the same answer and its recorded observations; save it to continue the original outlet when ready."
	}
	if r.WorkComplete {
		RecordPlanningDiagnostic(s.Root, snap.State, "outlet", "")
	} else {
		RecordPlanningDiagnostic(s.Root, snap.State, "outlet", r.NextAction)
	}
	r.NextAction += preparation
	r.WaitingOn = append(r.WaitingOn, preparationWaiting...)
	return r, nil
}

func (s *Service) designReceipt(snap runtime.Snapshot, p workpacket.DesignPacket) (Receipt, error) {
	r := emptyReceipt(snap, p.ID)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil {
		return r, err
	}
	if !found {
		return rejected(snap, p.ID, fmt.Errorf("design answer not received")), nil
	}
	r.SubmissionKey = record.Key
	r.Accepted = true
	r.DomainRefs = []string{record.EvidenceID}
	r.ResponsibilityStatus = "accepted"
	r.WorkComplete = record.ControlStatus != "pending" && record.ControlStatus != "no_transition"
	r.NextAction = "Design report was recorded; follow the current stage's actual professional work. Acceptance is not independent certification of every design task."
	for _, ev := range evidenceRows(snap.State) {
		if ev["id"] == record.EvidenceID && ev["status"] == "invalid" {
			r.WorkComplete = false
			r.ResponsibilityStatus = "replaced"
			r.NextAction = "This design reception remains recorded, but professional rework has replaced its responsibility. Follow the current questionnaire; do not restore or replay the old design report."
			return r, nil
		}
	}
	if (record.ControlStatus == "pending" || record.ControlStatus == "no_transition") && runtime.ValidateDesignPacket(s.Root, snap.State, p, nil) != nil {
		r.ResponsibilityStatus = "replaced"
		r.NextAction = "This design reception remains recorded, but the current stage responsibility has changed. Follow its current questionnaire; do not replay the old stage outlet."
		return r, nil
	}
	if record.ControlStatus == "pending" || record.ControlStatus == "no_transition" {
		r.NextAction = "Design report was recorded; the original stage outlet is pending. Resolve the named material gaps; save the same answer when ready, or use " + PlanningRecoveryCommand(s.Root, "recover") + " for an interrupted outlet. Do not repeat the professional report."
		r.NextAction += PlanningDiagnostic(s.Root, snap.State, "outlet")
		r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: record.FollowupID})
		if _, err := workpacket.ReadFile(s.Root, p.AnswerPath, 256<<10); err != nil {
			r.NextAction += " The original accepted answer is unavailable at " + filepath.Join(s.Root, p.AnswerPath) + ": " + err.Error() + ". Restore its original authored bytes before saving it again; explicit recovery continues from recorded evidence. Do not replace it with a blank draft."
		}
	}
	for _, ev := range evidenceRows(snap.State) {
		if ev["id"] == record.EvidenceID {
			materialPath := filepath.Join(s.Root, text(ev["path"]))
			data, readErr := os.ReadFile(materialPath)
			var material struct {
				Subjects []workpacket.Subject `json:"subject_refs"`
			}
			if readErr == nil {
				readErr = json.Unmarshal(data, &material)
			}
			if readErr != nil {
				r.NextAction = "The accepted design material is unavailable at " + materialPath + ": " + readErr.Error() + ". Restore the recorded material; do not submit a replacement proof."
				return r, nil
			}
			// Report missing actual reading material without re-certifying its
			// historical bytes, subject count, version or producer.
			for _, subject := range material.Subjects {
				if _, err := workpacket.ReadFile(s.Root, subject.Path, 2<<20); err != nil {
					r.NextAction = "Accepted design reading material is unavailable at " + filepath.Join(s.Root, subject.Path) + ": " + err.Error() + ". Main must restore that material; the original reception remains recorded."
					break
				}
			}
			return r, nil
		}
	}
	r.NextAction = "The accepted design record has no material reference; Main must restore that recorded material without repeating the professional answer."
	return r, nil
}

func (s *Service) DesignReceipt(path, packetID string) (Receipt, error) {
	snap, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	var p workpacket.DesignPacket
	var found bool
	if packetID != "" {
		p, found, err = runtime.FindDesignPacket(snap.State, packetID)
	} else {
		p, found, err = s.DesignForPath(path)
	}
	if err != nil || !found {
		return Receipt{}, fmt.Errorf("no original design issuance")
	}
	return s.designReceipt(snap, p)
}

// DesignReceiptForSnapshot projects the observer's already-read authority.
// It cannot recover a pending Writer transaction while rendering guidance.
func (s *Service) DesignReceiptForSnapshot(snap runtime.Snapshot, packetID string) (Receipt, error) {
	p, found, err := runtime.FindDesignPacket(snap.State, packetID)
	if err != nil || !found {
		return Receipt{}, fmt.Errorf("no original design issuance")
	}
	return s.designReceipt(snap, p)
}

// preparePlanningOutlet completes only the next responsibility of an actual
// committed planning transition. Old-answer replay never calls this outlet.
func (s *Service) preparePlanningOutlet(ctx context.Context, result *controller.ControlResult) (string, []Waiting) {
	s = s.WithContext(ctx)
	if !result.QualityGate.TransitionCommitted || !runtime.PacketEnabled(result.Snapshot.State) {
		return "", nil
	}
	id := result.QualityGate.CandidateTransition
	if id != "PTR-PLAN-01" && id != "PTR-PLAN-02" {
		return "", nil
	}
	preparationFailed := func(err error) (string, []Waiting) {
		RecordPlanningDiagnostic(s.Root, result.Snapshot.State, "preparation", err.Error())
		return " The preceding report and stage transition are recorded; the original next-stage questionnaire preparation failed: " + err.Error() + ". Main/system must repair the named source, then resume original questionnaire preparation with " + PlanningRecoveryCommand(s.Root, "issue") + ". Do not resubmit the completed report, invent a questionnaire path, or fabricate inapplicable materials.", []Waiting{{Reason: "system_followup", Owner: "Main", Ref: id}}
	}
	service := *s
	if service.Files == nil {
		catalog, err := transition.LoadCatalog(s.Root)
		if err != nil {
			return preparationFailed(err)
		}
		ref, err := fileview.DevelopmentRef(result.Snapshot.State)
		if err != nil {
			return preparationFailed(err)
		}
		service.Files, err = fileview.New(s.Root, "refs/heads/"+strings.TrimPrefix(ref, "refs/heads/"), catalog.Definition.FileSources)
		if err != nil {
			return preparationFailed(err)
		}
	}
	var view, answer, delivery string
	var err error
	if id == "PTR-PLAN-01" {
		var packet workpacket.ContractPacket
		packet, err = service.IssueContracts(ctx)
		view, answer = packet.ViewPath, packet.AnswerPath
		if err == nil {
			delivery = ContractDeliveryGuidance(s.Root, packet, service.Files, result.Snapshot.State)
		}
	} else {
		var packet workpacket.TaskPlanningPacket
		packet, err = service.IssueTaskPlanning(ctx)
		view, answer = packet.ViewPath, packet.AnswerPath
		if err == nil {
			delivery = TaskPlanningDeliveryGuidance(s.Root, packet, service.Files, result.Snapshot.State)
		}
	}
	if err != nil {
		return preparationFailed(err)
	}
	RecordPlanningDiagnostic(s.Root, result.Snapshot.State, "preparation", "")
	current, err := s.snapshot()
	if err == nil {
		result.Snapshot = current
	}
	message := fmt.Sprintf(" Current next-stage questionnaire: read %s; answer %s. Fill its professional answers and save normally.", filepath.Join(s.Root, view), filepath.Join(s.Root, answer)) + delivery
	if err != nil {
		message += " Preparation recorded, but current Runtime could not be read: " + err.Error()
	}
	return message, nil
}

func TaskPlanningDeliveryGuidance(root string, packet workpacket.TaskPlanningPacket, files fileview.Reader, state map[string]any) string {
	index := "docs/dev/tasks/index-" + packet.Binding.Requirement.ID + ".md"
	paths := []string{index}
	label := "Required dispatch index"
	candidates := ""
	fieldDiagnostic := ""
	if data, err := workpacket.ReadFile(root, packet.AnswerPath, workpacket.TaskPlanningAnswerReadLimit(packet)); err == nil {
		if namedPaths, named := taskPlanningDraftDeliveryPaths(packet, data); named {
			paths = namedPaths
			label = "Your named TASK delivery (safe draft paths; reception still requires the complete answer)"
		}
		if _, issues := workpacket.ValidateTaskPlanningAnswer(packet, data); len(issues) > 0 {
			fieldDiagnostic = " Current answer field: " + planningFieldDiagnostic(issues[0]) + "."
		}
	}
	if label == "Required dispatch index" && files != nil {
		if entries, err := files.ReadDir(filepath.Join(root, "docs/dev/tasks")); err == nil {
			var existing []string
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasPrefix(entry.Name(), "TASK-") && strings.HasSuffix(entry.Name(), ".md") && !strings.Contains(strings.ToLower(entry.Name()), "template") {
					existing = append(existing, filepath.Join(root, "docs/dev/tasks", entry.Name()))
				}
			}
			if len(existing) > 0 {
				candidates = " Existing TASK candidates (verify current REQ membership): " + strings.Join(existing, ", ") + "."
			}
		}
	}
	sourceHint := planningDeclaredSourceGuidance(root, paths, state, "S4", false)
	if view, ok := files.(*fileview.View); ok {
		var committed []string
		for _, path := range paths {
			if source, err := view.Source(path); err == nil && source == "git_tree" {
				committed = append(committed, filepath.Join(root, filepath.FromSlash(path)))
			}
		}
		if len(committed) > 0 {
			sourceHint = fmt.Sprintf("\nS4 TASK outputs and the index can change in this entry. Before saving this answer, commit the completed Git-sourced TASK files in your current REQ delivery and its index to bound development branch %s (current named Git paths: %s). REQ/S3 and other upstream inputs stay frozen. The answer and Runtime use their supplied disk paths.", strings.TrimPrefix(view.Ref, "refs/heads/"), strings.Join(committed, ", "))
		}
	}
	pathValues := ""
	if strings.HasPrefix(label, "Your named") {
		pathValues = planningAnswerPathValues("", paths, "task_paths", "index-")
	}
	for i, path := range paths {
		paths[i] = filepath.Join(root, filepath.FromSlash(path))
	}
	// This clarification also accompanies known historical questionnaires;
	// their original template identity and professional answer stay intact.
	sourceHint += " Scope/output coverage concerns planned product files, tests and professional evidence. Runtime Plan/Result paths and their answer format come from the later actual responsibility; describe that handoff without inventing a future assignment, report path or schema. Check/operation examples are not a tool allowlist. Cancelled TASKs stay in the full delivery batch while leaving active dispatch and clause coverage."
	return fmt.Sprintf("\n%s: %s.%s TASK output directory: %s; choose TASK IDs and professional scope from the frozen contracts, and include the full current REQ batch in task_paths. Templates: %s, %s. Dispatch rules: %s. Complete task granularity, Closing Contracts, clause coverage, dependencies and waves; save the professional answer once. Ordinary planning within the locked REQ needs no additional human approval; any inherited questionnaire reference to human gates applies only to actual requirement changes or decisions outside existing authorization. The original consumer runs tasks check, records evidence and resumes TR-002. Manual check commands support self-review or diagnostics, not additional submissions.%s", label, strings.Join(paths, ", "), candidates, filepath.Join(root, "docs/dev/tasks"), filepath.Join(root, "docs/dev/tasks/TASK-template.md"), filepath.Join(root, "docs/dev/tasks/index-template.md"), filepath.Join(root, "docs/rules/dispatch-plan.md"), sourceHint+" Tool file paths above are absolute. In answer.index_path and answer.task_paths, use repository-relative values without the root prefix; for example index_path=\""+index+"\", task_paths=[\"docs/dev/tasks/TASK-009-01.md\"]. Use canonical numeric TASK IDs such as TASK-009 or TASK-009-01; Source REQ refs binds each task to the actual REQ."+pathValues+fieldDiagnostic+PlanningProfessionalReworkGuidance(root))
}

// Display safe draft paths without requiring a professional pass or claiming acceptance.
func taskPlanningDraftDeliveryPaths(packet workpacket.TaskPlanningPacket, data []byte) ([]string, bool) {
	if int64(len(data)) > workpacket.TaskPlanningAnswerReadLimit(packet) || workpacket.ValidateUniqueJSON(data) != nil {
		return nil, false
	}
	var draft struct {
		Answer struct {
			IndexPath *string  `json:"index_path"`
			TaskPaths []string `json:"task_paths"`
		} `json:"answer"`
	}
	if json.Unmarshal(data, &draft) != nil {
		return nil, false
	}
	answer := draft.Answer
	named := answer.IndexPath != nil || len(answer.TaskPaths) > 0
	paths := []string{}
	if answer.IndexPath != nil && *answer.IndexPath == "docs/dev/tasks/index-"+packet.Binding.Requirement.ID+".md" {
		paths = append(paths, *answer.IndexPath)
	}
	seen := map[string]bool{}
	for _, path := range answer.TaskPaths {
		if filepath.IsAbs(path) || strings.Contains(path, "\\") || filepath.ToSlash(filepath.Clean(path)) != path || filepath.ToSlash(filepath.Dir(path)) != "docs/dev/tasks" || !strings.HasPrefix(filepath.Base(path), "TASK-") || !strings.HasSuffix(path, ".md") || strings.Contains(strings.ToLower(path), "template") || seen[path] {
			continue
		}
		paths = append(paths, path)
		seen[path] = true
	}
	return paths, named
}
