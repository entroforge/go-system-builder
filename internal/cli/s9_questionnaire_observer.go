package cli

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// These are existing question paths and indexed immutable artifacts, not a
// second acceptance ledger. Only actual saves run the original consumers.
func s9ObservableQuestionnaires(root string, state map[string]any, request policy.Input) []observedQuestionnaire {
	var out []observedQuestionnaire
	add := func(path, kind string) {
		out = append(out, observedQuestionnaire{path: path, s9Kind: kind, professionalWorker: kind == "targeted"})
	}
	owner := request.EffectiveAgentID()
	main := mainQuestionnaireViewer(request) && (owner != "" || request.AgentType == "" && request.AgentTranscriptPath == "")
	// A registered Worker checkout locates question paths, not the caller's
	// identity or permission. Never rewrite the actual event's AgentID.
	cwdOwner, ambiguous := "", false
	for _, raw := range mapFieldCLI(mapFieldCLI(state, "workspace"), "executions") {
		execution, _ := raw.(map[string]any)
		if request.CWD == "" || !samePath(request.CWD, stringValue(execution["worktree_path"])) {
			continue
		}
		candidate := stringValue(execution["agent_id"])
		if cwdOwner != "" && cwdOwner != candidate {
			ambiguous = true
		}
		cwdOwner = candidate
	}
	if cwdOwner != "" && !ambiguous {
		owner, main = cwdOwner, false
	} else if !main && !s9ListenerOwnerPresent(state, owner) {
		// Native platform IDs need not be Runtime owner IDs. Without a
		// located checkout, compare only this phase's current professional
		// answers with their existing drafts/submitted content below.
		owner = ""
	}
	pointer := repairPointerCLI(state)
	phase, status := stringValue(mapFieldCLI(state, "lifecycle")["phase"]), stringValue(pointer["status"])
	if main {
		switch status {
		case "planning", "reproducing", "repairing":
			for _, path := range repairDispatchPaths(root, state) {
				add(path, "dispatch")
			}
			if status != "repairing" {
				for _, path := range repairVerifierDispatchPaths(root, state) {
					add(path, "verifier")
				}
			}
		case "impact_reconciliation":
			if path, err := repairLeadQuestionnairePath(root, state, "change-impact.json"); err == nil {
				add(path, "impact")
			}
		case "targeted_reverification":
			for _, path := range repairVerifierDispatchPaths(root, state) {
				add(path, "verifier")
			}
		}
	} else if phase == "targeted_reverification" || status == "targeted_reverification" {
		owners := mapFieldCLI(pointer, "assignment_owners")
		for original := range owners {
			assignment := repair.VerificationAssignmentID(original)
			verifier := stringValue(owners[assignment])
			if verifier == "" || owner != "" && verifier != owner {
				continue
			}
			if path, err := repairTargetedQuestionnairePath(root, state, assignment); err == nil {
				add(path, "targeted")
			}
		}
	}
	result := phase == "fixing"
	if !main && (phase == "planning" || phase == "reproducing" || result) || main && result && status == "blocked" {
		filter := owner
		if main {
			filter = ""
		}
		packets, _ := s9CurrentQuestionnaires(root, state, filter, result, contextForHook(request))
		for _, packet := range packets {
			source := root
			if packet.result != nil {
				source = stringValue(s9ObservedExecution(state, packet)["worktree_path"])
			}
			if !main && source != "" {
				p := packet
				out = append(out, observedQuestionnaire{path: resolveRootPath(source, packet.answer), draft: packet.draft, s9Kind: "packet", s9Packet: &p, professionalWorker: true})
			}
			if main && packet.result != nil && s9ObservedBlockerCurrent(state, packet) {
				p := workpacket.RepairResultPacket{AnswerPath: packet.answer, Binding: *packet.result}
				if path, err := workpacket.RepairResultResolutionAuthoringPath(p); err == nil {
					add(resolveRootPath(root, path), "resolution")
				}
			}
		}
	}
	return out
}

func s9ObservedExecution(state map[string]any, packet s9ListenerQuestionnaire) map[string]any {
	executions := mapFieldCLI(mapFieldCLI(state, "workspace"), "executions")
	if packet.executionAssignment != "" {
		return mapFieldCLI(executions, packet.executionAssignment)
	}
	for _, id := range []string{packet.binding.RepairAssignmentID, packet.binding.ManifestAssignmentID} {
		if execution := mapFieldCLI(executions, id); len(execution) != 0 {
			return execution
		}
	}
	return nil
}

func s9ObservedBlockerCurrent(state map[string]any, packet s9ListenerQuestionnaire) bool {
	for _, row := range objectSliceAny(state["evidence"]) {
		if row["kind"] == "repair_result_blocker" && tolerantInt(row["baseline_generation"]) == packet.binding.BaselineGeneration && slices.Contains(stringSliceAny(row["scope_refs"]), packet.id) {
			return !s9ListenerResolutionRecorded(state, packet)
		}
	}
	return false
}

// A passive equality comparison reads only an already indexed professional
// document. It does not rebuild its receipt/source/hash authorization proof.
func s9ObservedDocument(root, path string) (map[string]any, error) {
	data, err := workpacket.ReadFile(root, path, 20<<20)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	err = json.Unmarshal(data, &document)
	return document, err
}

func s9ObservedQuestionnaireUnchanged(root string, state map[string]any, q observedQuestionnaire, data []byte) bool {
	if workpacket.ValidateUniqueJSON(data) != nil {
		return false // Malformed/changed author bytes retain original error feedback.
	}
	switch q.s9Kind {
	case "dispatch":
		return !repairDispatchHasProfessionalContent(q.path, data) || s9ObservedDispatchRegistered(root, state, data, false)
	case "verifier":
		return !repairVerifierDispatchHasProfessionalContent(q.path, data) || s9ObservedDispatchRegistered(root, state, data, true)
	case "packet":
		return s9ObservedPacketAccepted(root, state, q, data)
	}
	var answer map[string]any
	if json.Unmarshal(data, &answer) != nil {
		return false
	}
	if q.s9Kind == "resolution" {
		resolution := mapFieldCLI(answer, "s9_resolution")
		return strings.TrimSpace(stringValue(answer["body"])) == "" && strings.TrimSpace(stringValue(resolution["rationale"])) == "" && len(stringSliceAny(resolution["evidence_refs"])) == 0 && (resolution["decision"] == nil || resolution["decision"] == "retry")
	}
	pointer := repairPointerCLI(state)
	if q.s9Kind == "impact" {
		ref, err := repairPointerRef(pointer, "impact_ref", "impact_sha256", "registered ChangeImpact")
		if err != nil {
			return len(stringSliceAny(answer["change_types"])) == 0 && len(objectSliceAny(answer["decisions"])) == 0
		}
		registered, err := s9ObservedDocument(root, ref.Path)
		return err == nil && s9ObservedLeadProfessionalSame(answer, registered, false)
	}
	if q.s9Kind == "targeted" {
		for _, ref := range repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "") {
			registered, err := s9ObservedDocument(root, ref.Path)
			path, pathErr := repairTargetedQuestionnairePath(root, state, stringValue(registered["performing_assignment_id"]))
			if err == nil && pathErr == nil && samePath(path, q.path) {
				return s9ObservedLeadProfessionalSame(answer, registered, true)
			}
		}
		return stringValue(answer["result"]) == ""
	}
	return false
}

func s9ObservedDispatchRegistered(root string, state map[string]any, data []byte, verifier bool) bool {
	var rows []repairDispatchChoice
	if verifier {
		var choices []repairVerifierDispatchChoice
		if json.Unmarshal(data, &choices) != nil {
			return false
		}
		for _, row := range choices {
			if row.IndependentVerification {
				choice, owner := repairVerifierDispatchSelection(state, row)
				choice.AgentID = owner
				rows = append(rows, choice)
			}
		}
	} else if json.Unmarshal(data, &rows) != nil {
		return false
	}
	selected, seen := false, map[string]bool{}
	for _, row := range rows {
		if seen[row.AssignmentID] {
			return false
		}
		seen[row.AssignmentID] = true
		if row.RoleFamily == "" && row.CheckLocation != "" {
			row.RoleFamily = stringValue(contextAgent(state, repairDispatchOwner(state, row))["role"])
		}
		if row.RoleFamily == "" {
			continue
		}
		selected = true
		owner := repairDispatchOwner(state, row)
		if repairDispatchSameRegistered(state, row, owner) != nil {
			return false
		}
		if !verifier && (row.CheckLocation != "" && row.CheckLocation != "main" && row.CheckLocation != "worker" || repairExecutionCheckLocationConflict(state, row) != nil) {
			return false
		}

	}
	return selected
}

func s9ObservedLeadProfessionalSame(answer, registered map[string]any, targeted bool) bool {
	project := func(doc map[string]any) any {
		if targeted {
			return []any{stringValue(doc["continuity_reason"]), doc["assertion_results"], stringValue(doc["scope_compliance"]), stringValue(doc["result"]), stringValue(doc["failure_class"])}
		}
		escalation := stringValue(doc["escalation_level"])
		if escalation == "" {
			escalation = "assignment"
		}
		return []any{repairLeadStringSet(stringSliceAny(doc["change_types"])), doc["decisions"], escalation, repairLeadStringSet(stringSliceAny(doc["invalidated_evidence_ids"])), repairLeadStringSet(stringSliceAny(doc["superseded_evidence_ids"])), repairLeadStringSet(stringSliceAny(doc["retained_evidence_ids"]))}
	}
	return repairLeadCompareProfessional(project(answer), project(registered)) == nil
}

func s9ObservedPacketAccepted(root string, state map[string]any, q observedQuestionnaire, data []byte) bool {
	if q.s9Packet == nil {
		return false
	}
	packet := *q.s9Packet
	var envelope map[string]any
	if json.Unmarshal(data, &envelope) != nil || envelope["answer"] == nil {
		return false
	}
	pointer := repairPointerCLI(state)
	refs := repairArtifactRefs(pointer, "plan_report_refs", "plan_report_ref", "plan_report_sha256")
	if packet.result != nil {
		refs = repairArtifactRefs(pointer, "result_refs", "result_ref", "result_sha256")
		execution := s9ObservedExecution(state, packet)
		refs = append(refs, repair.ArtifactRef{Path: stringValue(execution["delivery_ref"])})
		for _, row := range objectSliceAny(state["evidence"]) {
			if row["kind"] == "repair_result_blocker" && slices.Contains(stringSliceAny(row["scope_refs"]), packet.id) {
				refs = append(refs, repair.ArtifactRef{Path: stringValue(row["path"])})
			}
		}
	}
	for _, ref := range refs {
		doc, err := s9ObservedDocument(root, ref.Path)
		if err != nil {
			continue
		}
		for _, professional := range []map[string]any{doc, mapFieldCLI(doc, "receipt"), mapFieldCLI(doc, "provenance"), mapFieldCLI(mapFieldCLI(doc, "receipt"), "provenance")} {
			if professional["packet_id"] == packet.id && professional["owner_agent_id"] == packet.binding.OwnerAgentID {
				return repairLeadCompareProfessional(envelope["answer"], professional["submitted_answer"]) == nil
			}
		}
	}
	draft, _ := q.draft.(map[string]any)
	return draft != nil && repairLeadCompareProfessional(envelope["answer"], draft["answer"]) == nil
}
