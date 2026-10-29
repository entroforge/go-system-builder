package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This is an author projection for the existing human or bounded technical
// decision. Machine binding comes from the already presented immutable draft.
type investigationContractDecisionAnswer struct {
	Decision                   string   `json:"decision"`
	Reason                     string   `json:"reason,omitempty"`
	RequirementsUnchanged      bool     `json:"requirements_unchanged,omitempty"`
	BusinessSemanticsUnchanged bool     `json:"business_semantics_unchanged,omitempty"`
	Reversible                 bool     `json:"reversible,omitempty"`
	ScopeComplete              bool     `json:"scope_complete,omitempty"`
	EvidenceRefs               []string `json:"evidence_refs,omitempty"`
	Rationale                  string   `json:"rationale,omitempty"`
}

type investigationContractDecisionTarget struct{ path, authority, actor string }

func investigationContractDecisionTargetFor(root string, state map[string]any, draft []byte) (investigationContractDecisionTarget, error) {
	id, reviewer, err := investigation.ContractReviewAuthority(root, state)
	if err != nil {
		return investigationContractDecisionTarget{}, err
	}
	name := "s8-contract-" + workpacket.Digest(draft)
	if id != "" {
		name = "s8-review-" + workpacket.Digest(draft) + "-" + workpacket.Digest([]byte(id))
	}
	return investigationContractDecisionTarget{filepath.Join(root, ".claude", "decisions", name+".json"), id, reviewer}, nil
}

func investigationLeadContractDecisionPaths(root string, state map[string]any) []string {
	paths := investigationLeadContractPaths(root, state)
	if len(paths) != 1 {
		return nil
	}
	_, document, _, err := investigationLeadSource(root, state)
	if err != nil {
		return nil
	}
	finalPath, draft, err := investigationLeadContractProjection(root, paths[0], document)
	if err != nil {
		return nil
	}
	actual, err := os.ReadFile(finalPath)
	if err != nil || !bytes.Equal(actual, draft) {
		return nil
	}
	target, err := investigationContractDecisionTargetFor(root, state, draft)
	if err != nil {
		return nil
	}
	return []string{target.path}
}

func prepareInvestigationContractDecision(root string, state map[string]any, draft []byte) (string, error) {
	target, err := investigationContractDecisionTargetFor(root, state, draft)
	if err != nil {
		return "", err
	}
	var answer any = map[string]any{"decision": "", "reason": ""}
	if target.authority != "" {
		answer = map[string]any{"decision": "", "requirements_unchanged": false, "business_semantics_unchanged": false, "reversible": false, "scope_complete": false, "evidence_refs": []string{}, "rationale": ""}
	}
	if target.authority == "" {
		_ = stageRepairDelegationApplicationContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	}
	return target.path, writeRepairLeadDraft(target.path, answer)
}

func investigationContractDecisionContext(root string, state map[string]any, draft []byte) string {
	target, err := investigationContractDecisionTargetFor(root, state, draft)
	if err != nil {
		return "Original approval preparation requires Main to resolve existing authority: " + err.Error()
	}
	if target.authority != "" {
		paths := investigationLeadContractPaths(root, state)
		_, document, _, sourceErr := investigationLeadSource(root, state)
		if sourceErr != nil {
			return "Original Driver review source requires Main/system recovery: " + sourceErr.Error()
		}
		if len(paths) != 1 {
			return "Original Driver review requires the current exact Contract questionnaire; Main/system must restore its source before delivering the review."
		}
		finalPath, _, projectionErr := investigationLeadContractProjection(root, paths[0], document)
		if projectionErr != nil {
			return "Original Driver review draft requires Main/system recovery: " + projectionErr.Error()
		}
		responsibility := "Main delivers the exact final review draft at " + finalPath + " and these review fields to designated Driver " + target.actor + ". That Driver returns their own decision, four judgments, evidence_refs and rationale; Main faithfully records that returned judgment in this application and saves it. A Worker save does not consume Main's application."
		if target.actor == "Main" {
			responsibility = "Main is the designated Driver: personally review the exact final draft at " + finalPath + ", fill the actual judgment in this application and save it."
		}
		return "Original Driver technical review application: " + target.path + ". Designated reviewer: " + target.actor + "; existing bounded authority: " + target.authority + ". " + responsibility + " Review fields: decision=approve_bounded_repair (or reject_contract/defer), the four actual professional judgments requirements_unchanged/business_semantics_unchanged/reversible/scope_complete, supporting evidence_refs and rationale. No human consent or technical judgment is generated by saving. System carries existing authority, identity, final draft hash and evidence registration into the original approval Writer; no metadata or command is authored. Empty decision remains a draft. Expiry, scope, budget and semantic review still apply."
	}

	optionalGrant := stageRepairDelegationApplicationContext(root, policy.Input{Event: "PreCompact"}, runtime.Snapshot{State: state})
	return optionalGrant + "\nApproval application: " + target.path + ". Present the exact final review draft to the actual human. Only after that human's explicit choice, Main faithfully fills decision=approve_contract (or reject_contract/defer) and optional reason, then saves this application. Empty decision remains a draft. Identity, approval hash, evidence and scope are supplied internally through the original approval Writer; no command or evidence registration is required. Current responsibility: the actual human decides this final draft; Main presents it and faithfully records that explicit choice. Main must not invent human consent or substitute a separate technical review for that choice."
}

func runSavedInvestigationContractDecision(root string, request policy.Input, stdout io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	name := filepath.Base(path)
	if !saved || (!strings.HasPrefix(name, "s8-contract-") && !strings.HasPrefix(name, "s8-review-")) || filepath.Ext(path) != ".json" || filepath.Dir(path) != filepath.Join(root, ".claude", "decisions") {
		return false, 0
	}
	feedback := func(message string) (bool, int) { return true, repairLeadFeedback(stdout, path, message) }
	if !mainQuestionnaireViewer(request) {
		return feedback("This is Main's original human/Driver application. A designated non-Main Driver returns their own judgment to Main; Main faithfully records it and saves this application. A Worker save does not consume it.")
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return feedback("Original Runtime recovery is required: " + err.Error())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return feedback("Approval application cannot be read: " + err.Error())
	}
	var answer investigationContractDecisionAnswer
	if err := investigationLeadDecode(data, &answer); err != nil {
		return feedback("Fill only the supplied professional decision fields; machine approval fields are supplied internally: " + err.Error())
	}
	answer.Decision, answer.Reason, answer.Rationale = strings.TrimSpace(answer.Decision), strings.TrimSpace(answer.Reason), strings.TrimSpace(answer.Rationale)
	if pointer := investigationPointer(current.State); pointer != nil && pointer["status"] == "contract_approved" {
		return investigationContractDecisionReplay(root, path, current.State, answer, feedback)
	}
	paths := investigationLeadContractPaths(root, current.State)
	if len(paths) != 1 {
		return feedback("This is no longer the current S8 approval responsibility; preserve its historical choice and use current original stage materials.")
	}
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil {
		return feedback("Original Case recovery is required: " + err.Error())
	}
	finalPath, draft, err := investigationLeadContractProjection(root, paths[0], document)
	if err != nil {
		return feedback("Complete the current professional Contract before approval: " + err.Error())
	}
	target, err := investigationContractDecisionTargetFor(root, current.State, draft)
	if err != nil {
		return feedback("Original authority preparation requires recovery: " + err.Error())
	}
	if !samePath(path, target.path) {
		return feedback("The Contract, Case or effective authority changed after this application was prepared. This choice remains bound to the earlier presented draft; present the current fixed final draft and obtain its actual decision, without copying or recalculating approval metadata.")
	}
	actual, err := os.ReadFile(finalPath)
	if err != nil || !bytes.Equal(actual, draft) {
		return feedback("The exact presented final draft is missing or changed; recover original bytes before recording approval.")
	}
	switch answer.Decision {
	case "":
		return feedback("Draft only: no actual decision was supplied. No evidence, approval, Runtime or journal decision was recorded.")
	case "reject_contract", "defer":
		return feedback("The actual choice remains in this application. No approval was granted; revise the professional Contract or await the responsible person's next decision.")
	}
	actor := strings.TrimSpace(request.AgentID)
	if actor == "" {
		actor = "Main"
	}
	kind, scope := "human_decision", "s8_contract_approval:"+stringValue(current.State["runtime_id"])
	var contract map[string]any
	if err := json.Unmarshal(draft, &contract); err != nil {
		return feedback("Original final draft cannot be decoded: " + err.Error())
	}
	artifact := map[string]any{"decision": answer.Decision, "reason": answer.Reason, "runtime_id": current.State["runtime_id"], "case_id": document["case_id"], "contract_id": contract["repair_contract_id"], "approval_hash": workpacket.Digest(draft), "approved_by": actor}
	if target.authority != "" {
		if answer.Decision != "approve_bounded_repair" {
			return feedback("The designated Driver must give the actual approve_bounded_repair, reject_contract or defer choice; no technical approval was granted.")
		}
		if !answer.RequirementsUnchanged || !answer.BusinessSemanticsUnchanged || !answer.Reversible || !answer.ScopeComplete || answer.Rationale == "" || len(answer.EvidenceRefs) == 0 {
			return feedback("Complete the four actual technical judgments, supporting evidence_refs and rationale. Missing or false judgments do not authorize bounded repair.")
		}
		actor, kind, scope = target.actor, "repair_contract_review", "s8_contract_review:"+stringValue(document["case_id"])
		review := investigation.RepairTechnicalReview{Decision: answer.Decision, RuntimeID: stringValue(current.State["runtime_id"]), CaseID: stringValue(document["case_id"]), ContractID: stringValue(contract["repair_contract_id"]), ApprovalHash: workpacket.Digest(draft), ReviewedBy: actor, DelegationEvidenceID: target.authority, RequirementsUnchanged: answer.RequirementsUnchanged, BusinessSemanticsUnchanged: answer.BusinessSemanticsUnchanged, Reversible: answer.Reversible, ScopeComplete: answer.ScopeComplete, EvidenceRefs: answer.EvidenceRefs, Rationale: answer.Rationale}
		encoded, _ := json.Marshal(review)
		_ = json.Unmarshal(encoded, &artifact)
		// Do not retain human-only fields in the strict original technical DTO.
		delete(artifact, "reason")
		delete(artifact, "approved_by")
	} else if answer.Decision != "approve_contract" {
		return feedback("Decision must be approve_contract, reject_contract or defer, reflecting the actual human's choice; no approval was granted.")
	}
	identity, _ := json.Marshal(artifact)
	id := "approval-s8-" + workpacket.Digest(identity)
	if kind == "human_decision" {
		artifact["decision_id"] = id
	}
	record, _ := json.MarshalIndent(artifact, "", "  ")
	record = append(record, '\n')
	rel := filepath.ToSlash(filepath.Join(".claude", "decisions", id+".json"))
	recordPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := prepareQuestionnaireText(recordPath, string(record)); err != nil {
		return feedback("Original decision evidence could not be fixed: " + err.Error())
	}
	actual, err = os.ReadFile(recordPath)
	if err != nil || !bytes.Equal(actual, record) {
		return feedback("Original immutable decision evidence conflicts; no historical decision was overwritten.")
	}
	if row := evidenceEntryByID(current.State, id); row != nil {
		if row["kind"] != kind || row["status"] != "valid" || row["path"] != rel || row["sha256"] != workpacket.Digest(record) || !s8ContractContains(row["produced_by"], actor) || !s8ContractContains(row["scope_refs"], scope) {
			return feedback("Original decision registration conflicts; no approval was inferred.")
		}
	} else {
		current, err = runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{ExpectedRevision: current.Revision, ID: id, Kind: kind, Path: rel, ProducedBy: []string{actor}, ScopeRefs: []string{scope}, Validator: semantic.RuntimeCandidateValidator{}})
		if err != nil {
			return feedback("Original decision evidence Writer did not confirm reception; preserve the choice and inspect its actual receipt: " + err.Error())
		}
	}
	next, err := investigation.ApproveContract(root, statePath, journalPath, investigation.ContractRequest{ExpectedRevision: current.Revision, CaseID: stringValue(document["case_id"]), ContractPath: finalPath, ApprovedBy: actor, ApprovalHash: workpacket.Digest(draft), ApprovalEvidenceID: id, DelegationEvidenceID: target.authority})
	if err != nil {
		return feedback("The actual decision was recorded; original Contract approval remains outstanding: " + err.Error())
	}
	prepared, err := prepareRepairEntry(root, statePath, journalPath, next.State)
	if err != nil {
		return feedback("Original Contract approval is recorded; S9 entry preparation requires recovery: " + err.Error())
	}
	return feedback(fmt.Sprintf("Original Contract approval recorded through the existing Writer; S9 entry is prepared. %s", repairLeadExistingQuestionnaires(root, prepared)))
}

func investigationContractDecisionReplay(root, path string, state map[string]any, answer investigationContractDecisionAnswer, feedback func(string) (bool, int)) (bool, int) {
	pointer := investigationPointer(state)
	approved, err := investigationLeadReadObject(root, stringValue(pointer["repair_contract_ref"]), stringValue(pointer["repair_contract_sha256"]))
	if err != nil {
		return feedback("Original approved Contract recovery is required: " + err.Error())
	}
	authority := stringValue(mapFieldCLI(approved, "delegated_authority")["delegation_evidence_id"])
	name := "s8-contract-" + stringValue(approved["approval_hash"]) + ".json"
	if authority != "" {
		name = "s8-review-" + stringValue(approved["approval_hash"]) + "-" + workpacket.Digest([]byte(authority)) + ".json"
	}
	if filepath.Base(path) != name {
		return feedback("This approval application belongs to another final draft or authority; no approved fact was changed.")
	}
	row := evidenceEntryByID(state, stringValue(approved["approval_evidence_id"]))
	if row == nil {
		return feedback("Original approval evidence recovery is required; no new consent was inferred.")
	}
	artifact, err := investigationLeadReadObject(root, stringValue(row["path"]), stringValue(row["sha256"]))
	if err != nil {
		return feedback("Original approval evidence recovery is required: " + err.Error())
	}
	var original investigationContractDecisionAnswer
	encoded, _ := json.Marshal(artifact)
	_ = json.Unmarshal(encoded, &original)
	a, _ := json.Marshal(answer)
	b, _ := json.Marshal(original)
	if !bytes.Equal(a, b) {
		return feedback("This answer conflicts with the original recorded decision; the approved Contract and intent were preserved.")
	}
	return feedback("Original Contract approval is already recorded. Repeated save is read-only; continue the original S9 handoff.")
}
