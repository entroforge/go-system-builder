package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Only actual human choices from the original finite grant. Binding identities
// belong to its original evidence projection.
type repairDelegationApplicationAnswer struct {
	Decision       string   `json:"decision"`
	Reviewer       string   `json:"reviewer"`
	AllowedPaths   []string `json:"allowed_paths"`
	ForbiddenPaths []string `json:"forbidden_paths"`
	MaxContracts   int      `json:"max_contracts"`
	ExpiresAt      string   `json:"expires_at"`
}

func repairDelegationFromApplication(root string, state map[string]any, answer repairDelegationApplicationAnswer) investigation.RepairDelegation {
	bound := mapFieldCLI(state, "bound_req")
	grant := investigation.RepairDelegation{Decision: strings.TrimSpace(answer.Decision), RuntimeID: stringValue(state["runtime_id"]), BaselineGeneration: integerValue(nestedStateValue(state, "baseline", "generation")), ApprovedBy: stringValue(bound["approved_by"]), Reviewer: answer.Reviewer, AllowedPaths: answer.AllowedPaths, ForbiddenPaths: answer.ForbiddenPaths, MaxContracts: answer.MaxContracts, ExpiresAt: answer.ExpiresAt}
	for _, row := range objectSliceAny(state["evidence"]) {
		if row["kind"] != "human_decision" || row["status"] != "valid" {
			continue
		}
		data, err := workpacket.ReadFile(root, stringValue(row["path"]), 20<<20)
		if err != nil {
			continue
		}
		var prior investigation.RepairDelegation
		if json.Unmarshal(data, &prior) != nil {
			continue
		}
		id := prior.DecisionID
		prior.DecisionID = ""
		if reflect.DeepEqual(prior, grant) {
			grant.DecisionID = id
			return grant
		}
	}
	runtimeID := base64.RawURLEncoding.EncodeToString([]byte(grant.RuntimeID))
	grant.DecisionID = fmt.Sprintf("grant-s8-%s-g%d-r%d", runtimeID, grant.BaselineGeneration, integerValue(state["revision"]))
	return grant
}

// Ordinary tools do not resubmit a decision whose exact content already has
// its original receipt. This reads the existing authority, not a second cache.
func repairDelegationApplicationAlreadyRecorded(root string, state map[string]any, path string, data []byte) bool {
	paths := repairDelegationApplicationPaths(root, state)
	if len(paths) != 1 || !samePath(path, paths[0]) {
		return false
	}
	var answer repairDelegationApplicationAnswer
	if investigationLeadDecode(data, &answer) != nil || strings.TrimSpace(answer.Decision) != "delegate_bounded_repair" {
		return false
	}
	grant := repairDelegationFromApplication(root, state, answer)
	row := evidenceEntryByID(state, grant.DecisionID)
	if row == nil || row["status"] != "valid" || row["kind"] != "human_decision" || !s8ContractContains(row["produced_by"], grant.ApprovedBy) || !s8ContractContains(row["scope_refs"], "s8_repair_delegation:"+grant.RuntimeID) {
		return false
	}
	return stringValue(row["id"]) == grant.DecisionID
}

func repairDelegationApplicationPaths(root string, state map[string]any) []string {
	bound := mapFieldCLI(state, "bound_req")
	if bound["status"] != "locked" || stringValue(bound["approved_by"]) == "" || stringValue(state["runtime_id"]) == "" || integerValue(nestedStateValue(state, "baseline", "generation")) < 1 {
		return nil
	}
	dir := s8OriginalSourceDir(root, stringValue(state["runtime_id"]), integerValue(nestedStateValue(state, "baseline", "generation")), stringValue(bound["path"]))
	return []string{filepath.Join(dir, "s8-repair-delegation.json")}
}

func stageRepairDelegationApplicationContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) {
		return ""
	}
	paths := repairDelegationApplicationPaths(root, snapshot.State)
	if len(paths) != 1 {
		return ""
	}
	path := paths[0]
	if repairLeadPreparationEvent(request.Event) {
		if err := writeRepairLeadDraft(path, repairDelegationApplicationAnswer{AllowedPaths: []string{}, ForbiddenPaths: []string{}}); err != nil {
			return "Original optional repair delegation application preparation failed: " + err.Error()
		}
	}
	return "Optional finite repair delegation application: " + path + ". Use only if the actual bound REQ approver chooses this reusable bounded authority; ordinary per-contract human approval remains available and no second grant is required when an approved policy or valid grant already exists. Main faithfully records the human's decision=delegate_bounded_repair, designated reviewer (different from the human authorizer), literal allowed_paths/forbidden_paths, finite max_contracts and explicit RFC3339 expires_at. Empty decision creates no grant. The system supplies the current locked REQ/runtime/generation, authority identity, scope and evidence registration; no machine fields or commands are authored. A grant alone does not approve any Contract, technical review, requirement change or release."
}

func runSavedRepairDelegationApplication(root string, request policy.Input, stdout io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved || filepath.Base(path) != "s8-repair-delegation.json" {
		return false, 0
	}
	feedback := func(message string) (bool, int) { return true, repairLeadFeedback(stdout, path, message) }
	if !mainQuestionnaireViewer(request) {
		return feedback("Only Main may faithfully record the actual human's finite grant; a Worker cannot invent that authority.")
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return feedback("Original binding recovery is required: " + err.Error())
	}
	paths := repairDelegationApplicationPaths(root, current.State)
	if len(paths) != 1 || !samePath(path, paths[0]) {
		return feedback("The locked REQ/runtime/generation changed. This application remains bound to its original human authority; use the current original application without copying or guessing machine metadata.")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return feedback("Original grant application cannot be read: " + err.Error())
	}
	var answer repairDelegationApplicationAnswer
	if err := investigationLeadDecode(data, &answer); err != nil {
		return feedback("Fill only the actual decision, reviewer, scope, finite budget and expiry; the system supplies the original grant binding: " + err.Error())
	}
	answer.Decision = strings.TrimSpace(answer.Decision)
	if answer.Decision == "" {
		return feedback("Draft only: no actual human delegation decision was supplied. No grant, evidence or Runtime mutation was recorded.")
	}
	if answer.Decision != "delegate_bounded_repair" {
		return feedback("No finite repair grant was recorded. The original grant requires the human's explicit delegate_bounded_repair choice; ordinary per-contract approval remains available.")
	}
	grant := repairDelegationFromApplication(root, current.State, answer)
	if row := evidenceEntryByID(current.State, grant.DecisionID); row != nil {
		return feedback("The original finite grant is already recorded; repeated save creates no new authority or budget.")
	}
	if err := investigation.ValidateRepairDelegation(root, current.State, grant); err != nil {
		return feedback("Original finite grant validation rejected this answer; no authority was created: " + err.Error())
	}
	record, _ := json.MarshalIndent(grant, "", "  ")
	record = append(record, '\n')
	recordPath := filepath.Join(s8OriginalSourceDir(root, grant.RuntimeID, grant.BaselineGeneration, grant.DecisionID), "grant.json")
	relPath, relErr := filepath.Rel(root, recordPath)
	if relErr != nil {
		return feedback("Original grant path is unavailable: " + relErr.Error())
	}
	rel, err := workpacket.PublishS9Questionnaire(contextForHook(request), root, filepath.ToSlash(relPath), record, 20<<20)
	if err != nil {
		return feedback("Original grant evidence could not be published: " + err.Error())
	}
	next, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: grant.DecisionID, Kind: "human_decision", Path: rel, ProducedBy: []string{grant.ApprovedBy}, ScopeRefs: []string{"s8_repair_delegation:" + grant.RuntimeID}, Validator: semantic.RuntimeCandidateValidator{}})
	if err != nil {
		// A concurrent original Writer may already have recorded this same human
		// choice. Reuse only that registered authority, never an unregistered
		// file occupying a guessed publication path.
		if latest, snapshotErr := runtime.NewStore(statePath, journalPath).Snapshot(); snapshotErr == nil && repairDelegationApplicationAlreadyRecorded(root, latest.State, path, data) {
			return feedback("The original finite grant is already recorded; repeated save creates no new authority or budget.")
		}
		return feedback("Original grant evidence Writer did not confirm reception: " + err.Error())
	}
	return feedback("The actual finite grant was recorded through the existing evidence Writer. No Contract is approved by the grant alone. " + stageInvestigationLeadContractContext(root, policy.Input{Event: "PreToolUse"}, next))
}
