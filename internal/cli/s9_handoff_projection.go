package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// These values exist only during the current request. Index coordinates retain
// known responsibility when cancellation or a source failure prevents a read.
// They grant neither reception nor integration authority.
type s9HandoffProjection struct {
	Plan                 s9ListenerQuestionnaire
	Result               *s9ListenerQuestionnaire
	Native               bool
	ExecutionObservation nativeExecutionReturn
	PlanAccepted         bool
	PlanKnown            bool
	Diagnostic           string
	Attempt              *nativeS9Attempt
	DiagnosticFiles      map[string]string
	ConsumerFailure      *nativeS9ConsumerFailure
	ResultFact           s9ListenerFact
	ResultFactKnown      bool
	ResultFactError      error
	ReadError            error
	RegisteredRef        string
}

func s9HandoffForBinding(ctx context.Context, root string, snap runtime.Snapshot, binding workpacket.RepairPlanReportBinding, preferredResult ...string) (p s9HandoffProjection) {
	p.ExecutionObservation = nativeExecutionInContext(ctx)
	defer func() {
		if p.Result == nil {
			return
		}
		execution := s9ObservedExecution(snap.State, *p.Result)
		if execution["purpose"] == workspace.NativeDeliveryPurpose && execution["agent_id"] == binding.OwnerAgentID && execution["runtime_id"] == binding.RuntimeID && tolerantInt(execution["baseline_generation"]) == binding.BaselineGeneration {
			p.Native = true
		}
		if ctx.Err() != nil {
			return
		}
		p.ResultFact, p.ResultFactError = s9ListenerAcceptanceContext(ctx, root, snap.State, *p.Result)
		p.ResultFactKnown = true
		if p.ResultFactError == nil && !p.ResultFact.unknown && (p.ResultFact.kind == "candidate" || p.ResultFact.kind == "result") {
			// A newer original result controls the next owner. The failed Stop's
			// recovery instruction is no longer a current duty.
			p.Diagnostic, p.Attempt, p.DiagnosticFiles, p.ConsumerFailure = "", nil, nil, nil
		}
	}()
	rows := objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"])
	for _, row := range rows {
		if row["template_id"] != "s9-plan-report-v1" {
			continue
		}
		var b workpacket.RepairPlanReportBinding
		raw, err := json.Marshal(row["binding"])
		if err != nil {
			p.ReadError = err
			continue
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			p.ReadError = err
			continue
		}
		if b != binding {
			continue
		}
		if p.Plan.id != "" {
			p.ReadError = fmt.Errorf("multiple original PlanReport packets for this exact Result binding")
			return p
		}
		p.Plan = s9ListenerQuestionnaire{id: stringValue(row["packet_id"]), view: stringValue(row["view_path"]), answer: stringValue(row["answer_path"]), binding: b}
	}
	// Read Plan acceptance before later Result sources; a spent budget leaves
	// its status unknown but never prevents pure Result index projection.
	if p.Plan.id != "" {
		fact, err := s9ListenerAcceptanceContext(ctx, root, snap.State, p.Plan)
		p.PlanKnown, p.PlanAccepted = err == nil && !fact.unknown, fact.kind == "plan"
		p.ReadError = err
		if ctx.Err() == nil {
			if diagnostic, found := nativeS9ReturnDiagnosticRecordContext(ctx, root, snap, p.Plan.id); found {
				brief, _, _ := nativeS9Paths(workpacket.RepairPlanReportPacket{ID: p.Plan.id, ViewPath: p.Plan.view})
				a := diagnostic.Attempt
				if a == nil || a.Kind == "Builder" && a.Owner == binding.OwnerAgentID && (a.ResponsibilityRef == p.Plan.id || a.ResponsibilityRef == filepath.Join(root, brief)) {
					p.Diagnostic, p.Attempt, p.DiagnosticFiles, p.ConsumerFailure = diagnostic.historicalMessage(), a, diagnostic.Files, diagnostic.ConsumerFailure
					if a != nil {
						p.Native = true
					}
				}
			}
		}
	} else {
		p.ReadError = fmt.Errorf("original PlanReport index is unavailable for this exact Result binding")
	}
	var candidates []s9ListenerQuestionnaire
	for _, row := range rows {
		if row["template_id"] != "s9-repair-result-v1" {
			continue
		}
		var b workpacket.RepairResultBinding
		raw, err := json.Marshal(row["binding"])
		if err != nil {
			p.ReadError = err
			continue
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			p.ReadError = err
			continue
		}
		if b.Sources != binding {
			continue
		}
		original := mapFieldCLI(mapFieldCLI(row, "inputs"), "execution")
		if locator := mapFieldCLI(row, "execution_locator"); len(locator) > 0 {
			original = locator
		}
		result := s9ListenerQuestionnaire{id: stringValue(row["packet_id"]), view: stringValue(row["view_path"]), answer: stringValue(row["answer_path"]), binding: binding, result: &b, originalRoot: stringValue(original["worktree_path"]), executionAssignment: stringValue(original["assignment_id"])}
		e := s9ObservedExecution(snap.State, result)
		exact := len(e) > 0 && e["runtime_id"] == binding.RuntimeID && e["agent_id"] == binding.OwnerAgentID && tolerantInt(e["baseline_generation"]) == binding.BaselineGeneration && tolerantInt(e["execution_generation"]) == tolerantInt(original["execution_generation"]) && stringValue(e["rework_ref"]) == b.ReworkRef && stringValue(e["worktree_path"]) == result.originalRoot
		if len(preferredResult) > 0 && preferredResult[0] != "" {
			if result.id == preferredResult[0] {
				p.Result = &result
				if exact {
					p.RegisteredRef = stringValue(e["delivery_ref"])
				}
				return p
			}
			continue
		}
		if exact {
			candidates = append(candidates, result)
			p.RegisteredRef = stringValue(e["delivery_ref"])
		}
	}
	if len(candidates) > 1 {
		p.ReadError = fmt.Errorf("multiple current Result indexes for this exact original binding")
		p.RegisteredRef = ""
	} else if len(candidates) == 1 {
		p.Result = &candidates[0]
	}
	return p
}

func (p s9HandoffProjection) summary(root string) string {
	var parts []string
	if p.Plan.id != "" {
		packet := workpacket.RepairPlanReportPacket{ID: p.Plan.id, ViewPath: p.Plan.view, AnswerPath: p.Plan.answer}
		brief, _, _ := nativeS9Paths(packet)
		if p.Native {
			plan, result, err := nativeS9AuthorPaths(root, packet)
			if err != nil {
				parts = append(parts, fmt.Sprintf("Original S9 owner=%s assignment=%s; exact brief %s; %s.", p.Plan.binding.OwnerAgentID, p.Plan.binding.ManifestAssignmentID, filepath.Join(root, brief), s9NativeAuthorCoordinatesFailure(root, packet, err, true)))
			} else {
				parts = append(parts, fmt.Sprintf("Original S9 owner=%s assignment=%s; exact brief %s; private worktree-relative PlanReport %s and Result %s.", p.Plan.binding.OwnerAgentID, p.Plan.binding.ManifestAssignmentID, filepath.Join(root, brief), plan, result))
			}
		} else {
			parts = append(parts, fmt.Sprintf("Original S9 owner=%s assignment=%s; PlanReport question %s; PlanReport answer %s.", p.Plan.binding.OwnerAgentID, p.Plan.binding.ManifestAssignmentID, filepath.Join(root, p.Plan.view), filepath.Join(root, p.Plan.answer)))
		}
	}
	if p.Result != nil {
		parts = append(parts, "Original issued Result answer "+filepath.Join(p.Result.originalRoot, p.Result.answer)+".")
	}
	if p.RegisteredRef != "" {
		parts = append(parts, "Registered candidate reference: "+resolveRootPath(root, p.RegisteredRef)+"; this index alone does not establish acceptance or completion.")
	}
	if p.PlanAccepted {
		parts = append(parts, "The PlanReport receipt is accepted. Preserve the accepted PlanReport; do not re-submit it.")
	} else if p.PlanKnown {
		parts = append(parts, "No original PlanReport acceptance is recorded.")
	}
	if p.Diagnostic != "" {
		if p.ExecutionObservation == nativeExecutionLaunched || p.ExecutionObservation == nativeExecutionUnknown {
			if p.Attempt != nil {
				parts = append(parts, "Historical attempt context only; this execution observation assigns no recovery action: "+p.Diagnostic)
			} else {
				parts = append(parts, "An earlier original Stop diagnostic remains at "+filepath.Join(root, nativeS9DiagnosticPath(p.Plan.id))+". It is preserved historical context, not a recovery action assigned by this execution observation.")
			}
		} else {
			parts = append(parts, p.Diagnostic)
		}
	}
	if p.ReadError != nil {
		parts = append(parts, "Current reception detail is unavailable; preserve known responsibility and accepted facts: "+p.ReadError.Error()+".")
	}
	return strings.Join(parts, " ")
}

// Only Main receives the preparation source as a recovery responsibility. The
// pin's own coordinates come from the issued packet, not the missing pin's
// version or a guessed private Plan/Result path.
func s9NativeAuthorCoordinatesFailure(root string, packet workpacket.RepairPlanReportPacket, err error, main bool) string {
	message := fmt.Sprintf("frozen private author coordinates unavailable: %v", err)
	if main {
		_, pin, _ := nativeS9Paths(packet)
		message += "; Main owns the original preparation source at " + resolveRootPath(root, pin) + " and restores or resolves it when needed for the original continuation; no private author path or version was inferred"
	} else {
		message += "; Main owns the original preparation gap"
	}
	return message
}

func s9UnreceivedAction() string {
	return "Candidate reception is not established; ready execution is not delivery. This observation alone establishes no failed Stop or platform completion. Preserve the exact original responsibility and any accepted PlanReport; Main continues the original Builder only for specific unfinished professional work."
}

func s9CandidateAction(root, assignment, owner string) string {
	return fmt.Sprintf("The Worker candidate is recorded. Main next runs runtime workspace integrate using this exact command: %s runtime workspace integrate --root %s --assignment %s --agent %s. Select necessary checks from the repair objective and actual changes using --check-command with an actual command; repeat that parameter for multiple checks. The original integration runs and records them. Without selected or existing checks, Main checks remain pending and no final PASS is recorded. Do not repeat this answer.", s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")), s7CaptureShellQuote(root), s7CaptureShellQuote(assignment), s7CaptureShellQuote(owner))
}

func s9FinalAction() string {
	return "The original Main Result is recorded as accepted. When the complete Final batch enters impact reconciliation, Main prepares or resumes its original ChangeImpact before independent checkpoint ACK/cleanup. A cleanup pending resource does not cancel the accepted Final or block its unlocked professional successor; Impact and independent verification still require their original consumers. Preserve accepted Final and Main checks; do not merge, check or receive completed work again. Do not repeat this answer."
}

// Select the original typed responsibility before looking at candidate
// provenance. An unreadable or historical typed source still fences legacy.
func typedRepairResultPacketForAssignmentContext(ctx context.Context, root string, state map[string]any, assignmentID, owner string) (workpacket.RepairResultPacket, bool, error) {
	if err := ctx.Err(); err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	pointer := repairPointerCLI(state)
	var matches []string
	historicalTyped := false
	raw, present := mapFieldCLI(state, "entities")["work_packets"]
	if !present {
		return workpacket.RepairResultPacket{}, false, nil
	}
	rows, valid := raw.([]any)
	if !valid {
		return workpacket.RepairResultPacket{}, false, fmt.Errorf("original work_packets index is not an array; cannot determine typed S9 responsibility")
	}
	seen := map[string]bool{}
	for _, value := range rows {
		row, valid := value.(map[string]any)
		id := stringValue(row["packet_id"])
		if !valid || id == "" || stringValue(row["template_id"]) == "" || seen[id] {
			return workpacket.RepairResultPacket{}, false, fmt.Errorf("original work_packets index has an invalid or duplicate identity; cannot determine typed S9 responsibility")
		}
		seen[id] = true
		if row["template_id"] != "s9-repair-result-v1" {
			continue
		}
		var b workpacket.RepairResultBinding
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return workpacket.RepairResultPacket{}, false, err
		}
		if err := json.Unmarshal(data, &b); err != nil {
			return workpacket.RepairResultPacket{}, false, err
		}
		s := b.Sources
		relevant := s.RuntimeID == state["runtime_id"] && (s.ManifestAssignmentID == assignmentID || s.RepairAssignmentID == assignmentID)
		historicalTyped = historicalTyped || relevant
		if s.RuntimeID != state["runtime_id"] || s.BaselineGeneration != workspace.Generation(state) || s.ReviewRound != tolerantInt(mapFieldCLI(state, "review")["round"]) || s.SessionID != pointer["session_id"] || s.PlanRef != pointer["plan_ref"] || s.PlanSHA256 != pointer["plan_sha256"] || (s.ManifestAssignmentID != assignmentID && s.RepairAssignmentID != assignmentID) {
			continue
		}
		// A wrong owner is a typed source error, never absence of typed work.
		if owner != "" && s.OwnerAgentID != owner {
			return workpacket.RepairResultPacket{}, true, fmt.Errorf("typed S9 original owner differs for assignment %s", assignmentID)
		}
		matches = append(matches, stringValue(row["packet_id"]))
	}
	if len(matches) == 0 {
		if historicalTyped {
			return workpacket.RepairResultPacket{}, true, fmt.Errorf("assignment %s has an original typed S9 responsibility outside the current generation/round/Plan; follow its named recovery or rework, no legacy fallback", assignmentID)
		}
		return workpacket.RepairResultPacket{}, false, nil
	}
	ctx = runtime.WithPacketBodyRoot(ctx, root)
	var all, current []workpacket.RepairResultPacket
	for _, id := range matches {
		p, found, err := runtime.FindRepairResultPacketContext(ctx, state, id)
		if err != nil || !found {
			return p, true, fmt.Errorf("typed S9 original packet %s is unavailable: %v", id, err)
		}
		all = append(all, p)
		if _, err := runtime.RepairResultExecution(state, p); err == nil {
			current = append(current, p)
		}
	}
	if len(current) == 1 {
		return current[0], true, nil
	}
	if len(current) > 1 || len(all) > 1 {
		return workpacket.RepairResultPacket{}, true, fmt.Errorf("typed S9 assignment %s has ambiguous original attempts; preserve original sources", assignmentID)
	}
	return all[0], true, fmt.Errorf("typed S9 original packet %s no longer matches its registered execution attempt; Main restores the original source or follows named rework; no legacy fallback", all[0].ID)
}

func s9HandoffForExecution(ctx context.Context, root string, snap runtime.Snapshot, e workspace.Execution) s9HandoffProjection {
	var selected *workpacket.RepairPlanReportBinding
	pointer := repairPointerCLI(snap.State)
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if row["template_id"] != "s9-plan-report-v1" {
			continue
		}
		var b workpacket.RepairPlanReportBinding
		raw, err := json.Marshal(row["binding"])
		if err != nil {
			return s9HandoffProjection{ReadError: err}
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return s9HandoffProjection{ReadError: err}
		}
		if b.RuntimeID != e.RuntimeID || b.BaselineGeneration != e.BaselineGeneration || b.OwnerAgentID != e.AgentID || (b.ManifestAssignmentID != e.AssignmentID && b.RepairAssignmentID != e.AssignmentID) || b.SessionID != pointer["session_id"] || b.PlanRef != pointer["plan_ref"] {
			continue
		}
		if selected != nil {
			return s9HandoffProjection{ReadError: fmt.Errorf("ambiguous original PlanReport responsibility for execution %s", e.AssignmentID)}
		}
		copy := b
		selected = &copy
	}
	if selected == nil {
		return s9HandoffProjection{ReadError: fmt.Errorf("original S9 PlanReport index is unavailable for owner=%s assignment=%s", e.AgentID, e.AssignmentID)}
	}
	return s9HandoffForBinding(ctx, root, snap, *selected)
}

// The ordinary reminder inspects already recorded professional fields only.
// It does not rebuild receipt, Git or Main-proof validation.
type s9CandidateProgressState string

const (
	s9CandidateComplete   s9CandidateProgressState = "complete"
	s9CandidateUnfinished s9CandidateProgressState = "unfinished"
	s9CandidateUnknown    s9CandidateProgressState = "unknown"
)

func s9ObservedCandidateProgress(document map[string]any) (s9CandidateProgressState, string) {
	answer := mapFieldCLI(mapFieldCLI(document, "provenance"), "submitted_answer")
	if len(answer) == 0 {
		return s9CandidateUnknown, "current professional completion information is unavailable"
	}
	if answer["verdict"] != "completed" {
		return s9CandidateUnfinished, fmt.Sprintf("recorded professional verdict is %v", answer["verdict"])
	}
	units := objectSliceAny(answer["unit_results"])
	if len(units) == 0 {
		return s9CandidateUnknown, "recorded repair unit completion information is unavailable"
	}
	for _, unit := range units {
		if unit["status"] != "pass" {
			return s9CandidateUnfinished, fmt.Sprintf("repair unit %v: %v", unit["unit_id"], unit["status"])
		}
	}
	if result := mapFieldCLI(document, "proposal")["result"]; result != "pass" {
		return s9CandidateUnfinished, fmt.Sprintf("recorded professional candidate result is %v", result)
	}
	return s9CandidateComplete, ""
}

// Shared by the ordinary Main dispatch entry and questionnaire reminder.
// No receipt/source closure or Git work runs in this observation.
func s9ObservedHandoffAction(ctx context.Context, root string, snap runtime.Snapshot, handoff s9HandoffProjection, execution map[string]any) string {
	if execution["status"] == "complete" {
		return "Original S9 execution is already integrated and cleaned; preserve its original receipts and continue remaining responsibilities."
	}
	if err := ctx.Err(); err != nil {
		return "Current Result reception is unknown; Main preserves known responsibility and accepted work: " + err.Error()
	}
	if handoff.Result == nil {
		if stringValue(execution["delivery_ref"]) != "" {
			return "Original registered candidate/Result association is unavailable; Main restores the exact original source and binding without legacy fallback or repeated accepted work."
		}
		return s9UnreceivedActionContext(ctx, root, handoff)
	}
	fact, err := handoff.ResultFact, handoff.ResultFactError
	if !handoff.ResultFactKnown {
		fact, err = s9ListenerAcceptanceContext(ctx, root, snap.State, *handoff.Result)
	}
	if fact.unknown {
		return "Original reception detail was not refreshed within this request; acceptance and professional completion are unknown. Preserve exact question paths and any independently accepted work; no repeated submission is inferred."
	}
	if err != nil {
		return "Original Result reception detail is unavailable; Main preserves known accepted work and restores the exact source: " + err.Error()
	}
	switch fact.kind {
	case "result":
		return s9FinalAction()
	case "candidate":
		if err := ctx.Err(); err != nil {
			return "Candidate reception is recorded; current professional completion is unknown: " + err.Error()
		}
		document, err := s9ObservedDocument(root, fact.ref)
		if err != nil {
			return "Candidate reception is recorded; Main restores its original source: " + err.Error()
		}
		state, reason := s9ObservedCandidateProgress(document)
		if state == s9CandidateUnknown {
			return "Candidate reception is recorded; current professional completion is unknown: " + reason
		}
		if state == s9CandidateUnfinished {
			return "Candidate reception is recorded; actual professional work remains unfinished: " + reason + ". Main follows these original omissions without repeating accepted content."
		}
		return s9CandidateAction(root, handoff.Result.executionAssignment, handoff.Result.binding.OwnerAgentID)
	case "blocker", "contract_conflict":
		return "Original blocker is recorded; Main follows its original recovery decision or S8 Contract revision; no completion is inferred."
	default:
		if stringValue(execution["delivery_ref"]) != "" || stringValue(execution["delivery_sha256"]) != "" {
			return "Original candidate binding or provenance is incomplete; Main restores the exact registered source without legacy fallback or replacement candidate."
		}
		return s9UnreceivedActionContext(ctx, root, handoff)
	}
}

func s9KnownMainHandoffsContext(ctx context.Context, root string, snap runtime.Snapshot) string {
	var lines []string
	pointer := repairPointerCLI(snap.State)
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if row["template_id"] != "s9-plan-report-v1" {
			continue
		}
		var binding workpacket.RepairPlanReportBinding
		raw, err := json.Marshal(row["binding"])
		if err != nil {
			continue
		}
		if json.Unmarshal(raw, &binding) != nil {
			continue
		}
		if binding.RuntimeID != snap.State["runtime_id"] || binding.BaselineGeneration != workspace.Generation(snap.State) || binding.SessionID != pointer["session_id"] || binding.PlanRef != pointer["plan_ref"] {
			continue
		}
		_, execution := repairExecutionForChoice(snap.State, repairDispatchChoice{AssignmentID: binding.RepairAssignmentID, AgentID: binding.OwnerAgentID})
		if execution["status"] == "complete" || len(execution) > 0 && execution["purpose"] != workspace.NativeDeliveryPurpose {
			continue
		}
		handoff := s9HandoffForBinding(ctx, root, snap, binding)
		if len(execution) == 0 && !handoff.PlanAccepted && handoff.Diagnostic == "" {
			continue
		}
		lines = append(lines, handoff.summary(root)+" "+s9ObservedHandoffAction(ctx, root, snap, handoff, execution))
	}
	return strings.Join(lines, "\n")
}
