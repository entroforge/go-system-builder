package investigation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

const contractNextCommand = "continue the current original S8 Contract/decision application at its supplied path; authors give only actual professional decisions, while Main/system recovers existing binding and evidence internally"

// ContractRequest carries the caller's Runtime CAS revision and the human or
// orchestrator identity that approves a draft RepairContract. Approval is a
// single transaction: the immutable approved Contract and the next immutable
// Case revision are written before the Runtime pointer is advanced.
//
// Approval evidence is a human_boundary gate: ApprovalEvidenceID resolves to
// valid human_decision evidence produced by ApprovedBy and scoped to the
// semantic context "s8_contract_approval:<runtime_id>". Both fields are required;
// an approver name by itself is not an approval receipt. With an explicit
// DelegationEvidenceID, ApprovalEvidenceID instead names a technical review;
// delegation.go enforces the human grant, scope, expiry and atomic use budget.
type ContractRequest struct {
	ExpectedRevision     int
	CaseID               string
	ContractPath         string
	ApprovedBy           string
	ContractDraft        json.RawMessage
	ApprovalEvidenceID   string
	DelegationEvidenceID string
	OccurredAt           time.Time
}

// ApproveContract validates a draft against the active InvestigationCase,
// requires exact Finding coverage, writes immutable approved artifacts, and
// CAS-pins the approved Contract into Runtime. It deliberately does not create
// a BUG or require a legacy BUG acceptance: it advances the lifecycle through
// S8-REPAIR-CONTRACT-APPROVAL so S9 consumes the approved Contract through the
// pointer recorded here.
func ApproveContract(root, statePath, journalPath string, request ContractRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(root) == "" {
		return runtime.Snapshot{}, actionableContractError("repository root is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("resolve repository root: %v", err)
	}
	root = absoluteRoot
	if strings.TrimSpace(request.CaseID) == "" {
		return runtime.Snapshot{}, actionableContractError("case_id is required")
	}
	if strings.TrimSpace(request.ContractPath) == "" {
		return runtime.Snapshot{}, actionableContractError("contract file is required")
	}
	if strings.TrimSpace(request.ApprovedBy) == "" {
		return runtime.Snapshot{}, actionableContractError("approved_by is required; record the approving human or orchestrator identity")
	}
	approvalEvidenceID := strings.TrimSpace(request.ApprovalEvidenceID)

	store := runtime.NewStore(statePath, journalPath)
	current, err := store.Snapshot()
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("read Runtime before RepairContract approval: %w", err)
	}
	if request.ExpectedRevision >= 0 && current.Revision != request.ExpectedRevision {
		return runtime.Snapshot{}, fmt.Errorf("%w: expected %d but Runtime is at %d; next: %s", runtime.ErrStaleRevision, request.ExpectedRevision, current.Revision, contractNextCommand)
	}

	pointer, err := activeCasePointer(current.State, request.CaseID)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if stringField(pointer["status"]) == "contract_approved" {
		return resumeApprovedContract(root, current, pointer, request)
	}
	caseRel := stringField(pointer["path"])
	casePath, err := repositoryPath(root, caseRel)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase path is invalid: %v", err)
	}
	caseBytes, err := os.ReadFile(casePath)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %q is missing or unreadable: %v", caseRel, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseBytes); err != nil {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %q schema is invalid: %v", caseRel, err)
	}
	var caseDocument map[string]any
	if err := json.Unmarshal(caseBytes, &caseDocument); err != nil {
		return runtime.Snapshot{}, actionableContractError("decode InvestigationCase %q: %v", caseRel, err)
	}
	if stringField(caseDocument["case_id"]) != request.CaseID {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase declares case_id %q, requested %q", stringField(caseDocument["case_id"]), request.CaseID)
	}
	if stringField(caseDocument["status"]) != "investigating" {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %s is %q, not investigating; inspect runtime investigation status before approving", request.CaseID, stringField(caseDocument["status"]))
	}
	caseRevision, err := integerValue(caseDocument["revision"])
	if err != nil || caseRevision != integerValueOrZero(pointer["revision"]) {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase revision does not match Runtime pointer; re-read runtime investigation status before retry")
	}
	caseFindingIDs, err := stringSlice(caseDocument["source_finding_ids"], "InvestigationCase.source_finding_ids")
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("%v", err)
	}
	if err := requireRepairReadyCase(caseDocument); err != nil {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %s is not ready for RepairContract approval: %v", request.CaseID, err)
	}

	contractRel, err := relativeContractPath(root, request.ContractPath)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("RepairContract path is invalid: %v", err)
	}
	draftPath, err := repositoryPath(root, contractRel)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("RepairContract path is invalid: %v", err)
	}
	draftBytes, err := os.ReadFile(draftPath)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("RepairContract draft %q is missing or unreadable: %v", contractRel, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-contract.schema.json", draftBytes); err != nil {
		return runtime.Snapshot{}, actionableContractError("RepairContract draft %q schema is invalid: %v", contractRel, err)
	}
	if err := repair.ValidateContractUnitScopes(draftBytes); err != nil {
		return runtime.Snapshot{}, actionableContractError("RepairContract scope is not executable: %v", err)
	}
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		return runtime.Snapshot{}, actionableContractError("decode RepairContract draft %q: %v", contractRel, err)
	}
	if stringField(draft["status"]) != "draft" {
		return runtime.Snapshot{}, actionableContractError("RepairContract %q is %q; only a draft can be approved", contractRel, stringField(draft["status"]))
	}
	if stringField(draft["case_id"]) != request.CaseID {
		return runtime.Snapshot{}, actionableContractError("RepairContract case_id %q does not match active Case %q", stringField(draft["case_id"]), request.CaseID)
	}
	draftRevision, err := integerValue(draft["revision"])
	if err != nil || draftRevision != caseRevision {
		return runtime.Snapshot{}, actionableContractError("RepairContract revision must equal active InvestigationCase revision %d", caseRevision)
	}
	contractFindingIDs, err := stringSlice(draft["source_finding_ids"], "RepairContract.source_finding_ids")
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("%v", err)
	}
	if err := exactFindingSetWithDetails(caseFindingIDs, contractFindingIDs); err != nil {
		return runtime.Snapshot{}, actionableContractError("RepairContract source_finding_ids must be an exact Finding set: %v", err)
	}

	if len(request.ContractDraft) > 0 && !bytes.Equal(request.ContractDraft, draftBytes) {
		return runtime.Snapshot{}, actionableContractError("presented Contract changed; obtain the actual decision for the current draft")
	}
	if approvalEvidenceID == "" {
		return runtime.Snapshot{}, actionableContractError("approval_evidence_id binding is missing; Main/system must recover the original decision receipt; the author gives the actual choice in the supplied application without evidence registration")
	}
	var delegation map[string]any
	if request.DelegationEvidenceID != "" {
		delegation, err = validateDelegatedApproval(root, current.State, draft, request)
	} else {
		err = validateContractApprovalEvidence(root, current.State, strings.TrimSpace(request.ApprovedBy), approvalEvidenceID, current.Revision, request.CaseID, stringField(draft["repair_contract_id"]), draftBytes)
	}
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("%v", err)
	}

	approvedAt := request.OccurredAt
	if approvedAt.IsZero() {
		approvedAt = time.Now().UTC()
	}
	approved := cloneMap(draft)
	approved["revision"] = caseRevision + 1
	approved["status"] = "approved"
	approved["approved_by"] = strings.TrimSpace(request.ApprovedBy)
	approved["approved_at"] = approvedAt.UTC().Format(time.RFC3339Nano)
	// RC-15: approver_id is the audit alias of approved_by recorded at
	// approval time so downstream consumers read one canonical field.
	approved["approver_id"] = strings.TrimSpace(request.ApprovedBy)
	approved["approval_evidence_id"] = approvalEvidenceID
	delete(approved, "delegated_authority")
	if delegation != nil {
		approved["delegated_authority"] = delegation
	}
	approvedBytes, err := json.MarshalIndent(approved, "", "  ")
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("encode approved RepairContract: %w", err)
	}
	approvedBytes = append(approvedBytes, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-contract.schema.json", approvedBytes); err != nil {
		return runtime.Snapshot{}, actionableContractError("approved RepairContract schema is invalid before write: %v", err)
	}
	contractID := stringField(approved["repair_contract_id"])
	approvedRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "contracts", contractID+fmt.Sprintf("-r%d.json", caseRevision+1)))
	approvedPath, err := repositoryPath(root, approvedRel)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("approved RepairContract path is invalid: %v", err)
	}
	if err := writeExclusive(approvedPath, approvedBytes); err != nil {
		return runtime.Snapshot{}, fmt.Errorf("write approved RepairContract %s: %w", approvedRel, err)
	}
	cleanupWriter := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	cleanupContract := func() { _, _ = cleanupApprovalArtifact(cleanupWriter, current, approvedRel, approvedBytes) }

	approvedCase := cloneMap(caseDocument)
	approvedCase["revision"] = caseRevision + 1
	approvedCase["status"] = "contract_approved"
	approvedCase["route"] = "s9_repair"
	approvedCase["route_reason"] = "approved RepairContract transfers root-cause repair to S9"
	approvedCase["repair_contract_ref"] = approvedRel
	approvedCaseBytes, err := json.MarshalIndent(approvedCase, "", "  ")
	if err != nil {
		cleanupContract()
		return runtime.Snapshot{}, fmt.Errorf("encode approved InvestigationCase: %w", err)
	}
	approvedCaseBytes = append(approvedCaseBytes, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", approvedCaseBytes); err != nil {
		cleanupContract()
		return runtime.Snapshot{}, actionableContractError("approved InvestigationCase schema is invalid before write: %v", err)
	}
	approvedCaseRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "cases", request.CaseID+fmt.Sprintf("-r%d.json", caseRevision+1)))
	approvedCasePath, err := repositoryPath(root, approvedCaseRel)
	if err != nil {
		cleanupContract()
		return runtime.Snapshot{}, actionableContractError("approved InvestigationCase path is invalid: %v", err)
	}
	if err := writeExclusive(approvedCasePath, approvedCaseBytes); err != nil {
		cleanupContract()
		return runtime.Snapshot{}, fmt.Errorf("write approved InvestigationCase %s: %w", approvedCaseRel, err)
	}
	cleanup := func() {
		_, _ = cleanupApprovalArtifact(cleanupWriter, current, approvedCaseRel, approvedCaseBytes)
		cleanupContract()
	}

	lifecycle, _ := current.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": stringField(lifecycle["state"]), "phase": lifecycle["phase"]}
	nextCursor := map[string]any{"state": "bug_resolution", "phase": "repair_readback"}
	runtimeID := stringField(current.State["runtime_id"])
	commitRevision := runtimeCommitRevision(request.ExpectedRevision, current.State)
	baseline, _ := baselineGeneration(current.State)
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	evidenceIDs := []string{request.CaseID, contractID, approvalEvidenceID}
	if delegation != nil {
		evidenceIDs = append(evidenceIDs, request.DelegationEvidenceID)
	}
	snapshot, err := updateRuntime(writer, request.ExpectedRevision, runtime.Mutation{
		EventID:                fmt.Sprintf("evt-repair-contract-approved-%s-r%d", contractID, commitRevision+1),
		TransitionID:           "S8-REPAIR-CONTRACT-APPROVAL",
		Event:                  "repair_contract_approved",
		Actor:                  "orchestrator",
		IdempotencyKey:         fmt.Sprintf("runtime:investigation-contract-approve:%s:%d", contractID, commitRevision),
		RuntimeID:              runtimeID,
		From:                   cursor,
		To:                     nextCursor,
		EvidenceIDs:            evidenceIDs,
		RequestID:              "investigation-contract-approve",
		BaselineGeneration:     baseline,
		GateID:                 "S8-REPAIR-CONTRACT-APPROVAL",
		ProducerResponsibility: "S8 Investigation",
		Message:                fmt.Sprintf("repair_contract_approved: %s for %s; next: S9 consume the approved Contract", contractID, request.CaseID),
		OccurredAt:             approvedAt,
		Apply: func(state map[string]any) error {
			// Revalidate reusable authority and budget inside the Writer CAS.
			if delegation != nil {
				if _, err := validateDelegatedApproval(root, state, draft, request); err != nil {
					return err
				}
			}
			review, ok := state["review"].(map[string]any)
			if !ok || review == nil {
				return errors.New("Runtime review section is missing; restore state.review before retry")
			}
			existing, ok := review["investigation"].(map[string]any)
			if !ok || existing == nil {
				return errors.New("active InvestigationCase pointer disappeared; run runtime investigation status and reconcile before retry")
			}
			if stringField(existing["case_id"]) != request.CaseID || integerValueOrZero(existing["revision"]) != caseRevision || stringField(existing["status"]) != "investigating" {
				return fmt.Errorf("active InvestigationCase changed during approval; expected %s at investigating; re-read runtime investigation status and retry", request.CaseID)
			}
			lifecycle, ok := state["lifecycle"].(map[string]any)
			if !ok || lifecycle == nil || stringField(lifecycle["state"]) != "bug_resolution" {
				return errors.New("Runtime is no longer in bug_resolution; inspect the Controller checkpoint before retry")
			}
			// Phase-agnostic within bug_resolution: after an investigate_more
			// re-entry the Case returns to investigating while side effects of
			// earlier plan-report submissions may have advanced the phase past
			// investigation. The authoritative guards are the Case pointer
			// (case_id + sha + investigating status, checked above); approval
			// itself re-pins the phase to repair_readback below.
			// repair_readback is accepted for re-approval after an
			// investigate_more re-entry: the s9_repair re-route restores this
			// phase while the Case returns to investigating, and the designed
			// continuation is a fresh approval of the re-authored contract.
			phaseRevision, err := integerValue(lifecycle["phase_revision"])
			if err != nil {
				return fmt.Errorf("lifecycle.phase_revision is invalid: %w", err)
			}
			lifecycle["phase"] = "repair_readback"
			lifecycle["phase_revision"] = phaseRevision + 1
			existing["path"] = approvedCaseRel
			existing["revision"] = caseRevision + 1
			existing["status"] = "contract_approved"
			existing["repair_contract_ref"] = approvedRel
			existing["updated_at"] = approvedAt.UTC().Format(time.RFC3339Nano)
			if delegation != nil {
				if err := consumeDelegationUse(state, request.DelegationEvidenceID); err != nil {
					return err
				}
			} else if err := runtime.ConsumeHumanDecisionEvidence(state, approvalEvidenceID, "S8-REPAIR-CONTRACT-APPROVAL", approvedAt); err != nil {
				return err
			}
			state["updated_at"] = approvedAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
	if err != nil {
		cleanup()
		return runtime.Snapshot{}, err
	}
	return snapshot, nil
}

// contractApprovalScope is the semantic human_boundary scope prefix for the
// S8→S9 contract approval. Runtime revision is deliberately not part of this
// business scope; the actual presented Contract, Case/Contract identity and
// one-time evidence id preserve the original decision.
const contractApprovalScope = "s8_contract_approval"

// validateContractApprovalEvidence enforces that the cited human_decision
// evidence is valid, produced by the named approver, and scoped to
// "s8_contract_approval:<runtime_id>". A legacy @revision suffix remains
// readable for migration, but new evidence uses the semantic scope.
func validateContractApprovalEvidence(root string, state map[string]any, approvedBy, evidenceID string, revision int, caseID, contractID string, draft []byte) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return errors.New("runtime evidence must be an array")
	}
	runtimeID := stringField(state["runtime_id"])
	semanticScope := fmt.Sprintf("%s:%s", contractApprovalScope, runtimeID)
	legacyScope := fmt.Sprintf("%s@%d", semanticScope, revision)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || stringField(item["id"]) != evidenceID || stringField(item["kind"]) != "human_decision" || stringField(item["status"]) != "valid" {
			continue
		}
		if runtime.HumanDecisionEvidenceConsumed(item) {
			return fmt.Errorf("contract approval evidence %q was already consumed; create a new approval decision", evidenceID)
		}
		if containsStringAny(item["produced_by"], approvedBy) && (containsStringAny(item["scope_refs"], semanticScope) || containsStringAny(item["scope_refs"], legacyScope)) {
			if err := validateContractApprovalArtifact(root, item, state, caseID, contractID, draft); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("contract approval evidence %q must be valid human_decision evidence produced by %q and scoped to %s; continue the original decision application with the actual human choice, and let Main/system recover its original evidence binding without authoring metadata or manual registration", evidenceID, approvedBy, semanticScope)
}

// validateContractApprovalArtifact binds JSON approval records to the Case,
// actual presented draft and evidence ID being approved. Markdown/legacy records
// remain readable during migration; the current S8 CLI contract emits JSON
// records with all fields below.
func validateContractApprovalArtifact(root string, item, state map[string]any, caseID, contractID string, draft []byte) error {
	pathRef := strings.TrimSpace(stringField(item["path"]))
	if filepath.Ext(pathRef) != ".json" {
		return fmt.Errorf("original decision's presented Contract is unavailable")
	}
	clean := filepath.Clean(pathRef)
	if pathRef == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("contract approval evidence %q has unsafe artifact path %q", stringField(item["id"]), pathRef)
	}
	data, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		return fmt.Errorf("read contract approval artifact %q: %w", stringField(item["id"]), err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		return fmt.Errorf("decode contract approval artifact %q: %w", stringField(item["id"]), err)
	}
	want := map[string]string{
		"decision":    "approve_contract",
		"decision_id": stringField(item["id"]),
		"runtime_id":  stringField(state["runtime_id"]),
		"case_id":     caseID,
		"contract_id": contractID,
	}
	var presented any
	if len(draft) == 0 || json.Unmarshal(draft, &presented) != nil || !reflect.DeepEqual(artifact["contract_draft"], presented) {
		return fmt.Errorf("contract approval artifact %q does not record the actual presented Contract", stringField(item["id"]))
	}
	for field, expected := range want {
		if stringField(artifact[field]) != expected {
			return fmt.Errorf("contract approval artifact %q field %s=%q, want %q", stringField(item["id"]), field, stringField(artifact[field]), expected)
		}
	}
	return nil
}

// containsStringAny reports whether the decoded string list contains value.
func containsStringAny(raw any, value string) bool {
	switch values := raw.(type) {
	case []any:
		for _, item := range values {
			if text, _ := item.(string); text == value {
				return true
			}
		}
	case []string:
		for _, text := range values {
			if text == value {
				return true
			}
		}
	case string:
		return values == value
	}
	return false
}

func activeCasePointer(state map[string]any, caseID string) (map[string]any, error) {
	review, ok := state["review"].(map[string]any)
	if !ok || review == nil {
		return nil, actionableContractError("state.review is missing; run runtime investigation ingest before approving a Contract")
	}
	pointer, ok := review["investigation"].(map[string]any)
	if !ok || pointer == nil {
		return nil, actionableContractError("state.review.investigation is missing; run runtime investigation ingest before approving a Contract")
	}
	if stringField(pointer["case_id"]) != caseID {
		return nil, actionableContractError("active InvestigationCase is %q, not %q; inspect runtime investigation status before retry", stringField(pointer["case_id"]), caseID)
	}
	if stringField(pointer["status"]) != "investigating" && stringField(pointer["status"]) != "contract_approved" {
		return nil, actionableContractError("InvestigationCase %s is %q; only investigating Cases can receive first Contract approval", caseID, stringField(pointer["status"]))
	}
	return pointer, nil
}

func exactFindingSetWithDetails(caseIDs, contractIDs []string) error {
	left, err := normalizeOptionalSet(caseIDs, "InvestigationCase.source_finding_ids")
	if err != nil {
		return err
	}
	right, err := normalizeOptionalSet(contractIDs, "RepairContract.source_finding_ids")
	if err != nil {
		return err
	}
	missing := difference(left, right)
	extra := difference(right, left)
	if len(missing) != 0 || len(extra) != 0 {
		return fmt.Errorf("missing=%v extra=%v", missing, extra)
	}
	return nil
}

func requireRepairReadyCase(document map[string]any) error {
	if !nonEmptyObject(document["causal_model"]) {
		return errors.New("causal_model is missing or empty")
	}
	if strings.TrimSpace(stringField(document["primary_root_cause"])) == "" {
		return errors.New("primary_root_cause is missing")
	}
	return nil
}

func nonEmptyObject(value any) bool {
	object, ok := value.(map[string]any)
	return ok && len(object) > 0
}

func difference(left, right []string) []string {
	known := make(map[string]struct{}, len(right))
	for _, value := range right {
		known[value] = struct{}{}
	}
	var result []string
	for _, value := range left {
		if _, ok := known[value]; !ok {
			result = append(result, value)
		}
	}
	return result
}

func relativeContractPath(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		path = rel
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("must be a repository-relative path or an absolute path under repository root")
	}
	return filepath.ToSlash(clean), nil
}

func cloneMap(input map[string]any) map[string]any {
	data, _ := json.Marshal(input)
	var output map[string]any
	_ = json.Unmarshal(data, &output)
	return output
}

func integerValue(value any) (int, error) {
	switch number := value.(type) {
	case float64:
		return int(number), nil
	case int:
		return number, nil
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func integerValueOrZero(value any) int {
	result, _ := integerValue(value)
	return result
}

func cleanupApprovalArtifact(writer *runtime.Store, current runtime.Snapshot, path string, data []byte) (bool, error) {
	return writer.RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{ExpectedRevision: current.Revision, ArtifactPath: path, ArtifactBytes: data, ReferencedPaths: stateStringReferences(current.State, path)})
}

func actionableContractError(format string, args ...any) error {
	return fmt.Errorf(format+"; next: %s", append(args, contractNextCommand)...)
}
