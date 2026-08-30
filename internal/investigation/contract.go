package investigation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

const contractNextCommand = "runtime investigation contract approve --root . --case-id <case> --file <draft> --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id>"

// ContractRequest carries the caller's Runtime CAS revision and the human or
// orchestrator identity that approves a draft RepairContract. Approval is a
// single transaction: the immutable approved Contract and the next immutable
// Case revision are written before the Runtime pointer is advanced.
//
// RC-15 (S9-H5/H6) approval authority: ApprovalHash pins the exact draft
// bytes the human reviewed (sha256 of the on-disk draft; the server
// recomputes and compares, so a mid-approval swap is rejected). Approval
// evidence is a human_boundary gate: ApprovalEvidenceID must resolve to
// valid human_decision evidence produced by ApprovedBy and scoped to
// "s8_contract_approval:<runtime_id>@<revision>". Both fields are required;
// an approver name by itself is not an approval receipt.
type ContractRequest struct {
	ExpectedRevision   int
	CaseID             string
	ContractPath       string
	ApprovedBy         string
	ApprovalHash       string
	ApprovalEvidenceID string
	OccurredAt         time.Time
}

// ApproveContract validates a draft against the active InvestigationCase,
// requires exact Finding coverage, writes immutable approved artifacts, and
// CAS-pins the approved Contract into Runtime. It deliberately does not create
// a BUG or require a legacy BUG acceptance: it advances the lifecycle through
// S8-REPAIR-CONTRACT-APPROVAL so S9 consumes the approved Contract through the
// pointer recorded here. The old PTR-BUG-08 catalog entry (deprecated: legacy
// compatibility) remains only for legacy BUG projections and is not used by
// the Case/Contract authority path.
func ApproveContract(root, statePath, journalPath string, request ContractRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(root) == "" {
		return runtime.Snapshot{}, actionableContractError("repository root is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return runtime.Snapshot{}, actionableContractError("resolve repository root: %v", err)
	}
	root = absoluteRoot
	if request.ExpectedRevision < 0 {
		return runtime.Snapshot{}, actionableContractError("expected Runtime revision must be non-negative")
	}
	if strings.TrimSpace(request.CaseID) == "" {
		return runtime.Snapshot{}, actionableContractError("case_id is required")
	}
	if strings.TrimSpace(request.ContractPath) == "" {
		return runtime.Snapshot{}, actionableContractError("contract file is required")
	}
	if strings.TrimSpace(request.ApprovedBy) == "" {
		return runtime.Snapshot{}, actionableContractError("approved_by is required; record the approving human or orchestrator identity")
	}
	approvalHash := strings.TrimSpace(request.ApprovalHash)
	approvalEvidenceID := strings.TrimSpace(request.ApprovalEvidenceID)

	store := runtime.NewStore(statePath, journalPath)
	current, err := store.Snapshot()
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("read Runtime before RepairContract approval: %w", err)
	}
	if current.Revision != request.ExpectedRevision {
		return runtime.Snapshot{}, fmt.Errorf("%w: expected %d but Runtime is at %d; next: %s", runtime.ErrStaleRevision, request.ExpectedRevision, current.Revision, contractNextCommand)
	}

	pointer, err := activeCasePointer(current.State, request.CaseID)
	if err != nil {
		return runtime.Snapshot{}, err
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
	caseSHA := sha256Hex(caseBytes)
	if caseSHA != stringField(pointer["sha256"]) {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %q sha256 drifted: state pins %s but disk is %s", caseRel, stringField(pointer["sha256"]), caseSHA)
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
	if err := validateCausalClosureEvidence(root, current.State, caseDocument); err != nil {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %s causal closure evidence is not current: %v", request.CaseID, err)
	}
	if err := validateContractBaseline(root, current.State, caseDocument); err != nil {
		return runtime.Snapshot{}, actionableContractError("InvestigationCase %s baseline is not current: %v", request.CaseID, err)
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

	// RC-15 (S9-H5/H6): ordering — exact-set and causal-closure are
	// reported first so callers fix the Case/draft before pinning the
	// approval receipt. Then validate the human-boundary receipt.
	if approvalHash == "" {
		return runtime.Snapshot{}, actionableContractError("approval_hash is required; pin the exact draft bytes reviewed by the approver")
	}
	if approvalHash != sha256Hex(draftBytes) {
		return runtime.Snapshot{}, actionableContractError("approval_hash does not match the draft on disk: pinned %s but draft is %s; re-read the draft and record the decision against the current bytes", approvalHash, sha256Hex(draftBytes))
	}
	if approvalEvidenceID == "" {
		return runtime.Snapshot{}, actionableContractError("approval_evidence_id is required; cite valid human_decision evidence for this S8 approval")
	}
	if err := validateContractApprovalEvidence(current.State, strings.TrimSpace(request.ApprovedBy), approvalEvidenceID, current.Revision); err != nil {
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
	approved["approval_hash"] = sha256Hex(draftBytes)
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
	contractSHA := sha256Hex(approvedBytes)

	approvedCase := cloneMap(caseDocument)
	approvedCase["revision"] = caseRevision + 1
	approvedCase["status"] = "contract_approved"
	approvedCase["route"] = "s9_repair"
	approvedCase["route_reason"] = "approved RepairContract transfers root-cause repair to S9"
	approvedCase["repair_contract_ref"] = approvedRel
	approvedCase["repair_contract_sha256"] = contractSHA
	approvedCaseBytes, err := json.MarshalIndent(approvedCase, "", "  ")
	if err != nil {
		_ = os.Remove(approvedPath)
		return runtime.Snapshot{}, fmt.Errorf("encode approved InvestigationCase: %w", err)
	}
	approvedCaseBytes = append(approvedCaseBytes, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", approvedCaseBytes); err != nil {
		_ = os.Remove(approvedPath)
		return runtime.Snapshot{}, actionableContractError("approved InvestigationCase schema is invalid before write: %v", err)
	}
	approvedCaseRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "cases", request.CaseID+fmt.Sprintf("-r%d.json", caseRevision+1)))
	approvedCasePath, err := repositoryPath(root, approvedCaseRel)
	if err != nil {
		_ = os.Remove(approvedPath)
		return runtime.Snapshot{}, actionableContractError("approved InvestigationCase path is invalid: %v", err)
	}
	if err := writeExclusive(approvedCasePath, approvedCaseBytes); err != nil {
		_ = os.Remove(approvedPath)
		return runtime.Snapshot{}, fmt.Errorf("write approved InvestigationCase %s: %w", approvedCaseRel, err)
	}
	approvedCaseSHA := sha256Hex(approvedCaseBytes)
	cleanup := func() {
		if !runtimeReferencesApproval(statePath, approvedCaseRel, approvedCaseSHA, approvedRel, contractSHA) {
			_ = os.Remove(approvedCasePath)
			_ = os.Remove(approvedPath)
		}
	}

	lifecycle, _ := current.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": stringField(lifecycle["state"]), "phase": lifecycle["phase"]}
	nextCursor := map[string]any{"state": "bug_resolution", "phase": "repair_readback"}
	runtimeID := stringField(current.State["runtime_id"])
	baseline, _ := baselineGeneration(current.State)
	snapshot, err := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).Update(request.ExpectedRevision, runtime.Mutation{
		EventID:                fmt.Sprintf("evt-repair-contract-approved-%s-r%d", contractID, request.ExpectedRevision+1),
		TransitionID:           "S8-REPAIR-CONTRACT-APPROVAL",
		Event:                  "repair_contract_approved",
		Actor:                  "orchestrator",
		IdempotencyKey:         fmt.Sprintf("runtime:investigation-contract-approve:%s:%d", contractID, request.ExpectedRevision),
		RuntimeID:              runtimeID,
		From:                   cursor,
		To:                     nextCursor,
		EvidenceIDs:            []string{request.CaseID, contractID, approvalEvidenceID},
		RequestID:              "investigation-contract-approve",
		BaselineGeneration:     baseline,
		GateID:                 "S8-REPAIR-CONTRACT-APPROVAL",
		GateFingerprint:        "sha256:repair-contract-approval-v1",
		ProducerResponsibility: "S8 Investigation",
		Message:                fmt.Sprintf("repair_contract_approved: %s for %s; next: S9 consume the approved Contract", contractID, request.CaseID),
		OccurredAt:             approvedAt,
		Apply: func(state map[string]any) error {
			review, ok := state["review"].(map[string]any)
			if !ok || review == nil {
				return errors.New("Runtime review section is missing; restore state.review before retry")
			}
			existing, ok := review["investigation"].(map[string]any)
			if !ok || existing == nil {
				return errors.New("active InvestigationCase pointer disappeared; run runtime investigation status and reconcile before retry")
			}
			if stringField(existing["case_id"]) != request.CaseID || stringField(existing["sha256"]) != caseSHA || stringField(existing["status"]) != "investigating" {
				return fmt.Errorf("active InvestigationCase changed during approval; expected %s at investigating; re-read runtime investigation status and retry", request.CaseID)
			}
			lifecycle, ok := state["lifecycle"].(map[string]any)
			if !ok || lifecycle == nil || stringField(lifecycle["state"]) != "bug_resolution" || stringField(lifecycle["phase"]) != "investigation" {
				return errors.New("Runtime is no longer in bug_resolution.investigation; inspect the Controller checkpoint before retry")
			}
			phaseRevision, err := integerValue(lifecycle["phase_revision"])
			if err != nil {
				return fmt.Errorf("lifecycle.phase_revision is invalid: %w", err)
			}
			lifecycle["phase"] = "repair_readback"
			lifecycle["phase_revision"] = phaseRevision + 1
			existing["path"] = approvedCaseRel
			existing["sha256"] = approvedCaseSHA
			existing["revision"] = caseRevision + 1
			existing["status"] = "contract_approved"
			existing["repair_contract_ref"] = approvedRel
			existing["repair_contract_sha256"] = contractSHA
			existing["updated_at"] = approvedAt.UTC().Format(time.RFC3339Nano)
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

// validateContractBaseline rechecks the S7 subject digest at the S8 authority
// boundary. Case revisions surface ReviewPlan drift as a warning, but an
// approval may happen without another Case mutation; therefore Contract
// approval must independently re-read the sealed ObservationBatch and any
// current ReviewPlan pointer before it can transfer authority to S9.
func validateContractBaseline(root string, state, caseDocument map[string]any) error {
	pinnedDigest := strings.TrimSpace(stringField(caseDocument["baseline_digest"]))
	if pinnedDigest == "" {
		return errors.New("Case baseline_digest is missing; re-ingest from a sealed ObservationBatch")
	}
	pinnedGeneration, err := integerValue(caseDocument["baseline_generation"])
	if err != nil {
		return fmt.Errorf("Case baseline_generation is invalid: %w", err)
	}
	runtimeGeneration, err := baselineGeneration(state)
	if err != nil {
		return fmt.Errorf("Runtime baseline.generation is invalid: %w", err)
	}
	if pinnedGeneration != runtimeGeneration {
		return fmt.Errorf("Case baseline_generation %d does not match Runtime baseline.generation %d; re-ingest after the current baseline is sealed", pinnedGeneration, runtimeGeneration)
	}
	reviewState, _ := state["review"].(map[string]any)
	if warning := strings.TrimSpace(stringField(reviewState["investigation_baseline_drift"])); warning != "" {
		return fmt.Errorf("%s; re-verify the Case against the current S7 baseline before approval", warning)
	}

	batchPointer, err := observationBatchPointer(state)
	if err != nil {
		return fmt.Errorf("sealed ObservationBatch is unavailable: %w", err)
	}
	batchPath, err := repositoryPath(root, batchPointer.Path)
	if err != nil {
		return fmt.Errorf("ObservationBatch path is invalid: %w", err)
	}
	batchBytes, err := os.ReadFile(batchPath)
	if err != nil {
		return fmt.Errorf("read sealed ObservationBatch %q: %w", batchPointer.Path, err)
	}
	actualBatchSHA := sha256Hex(batchBytes)
	if actualBatchSHA != batchPointer.SHA256 {
		return fmt.Errorf("sealed ObservationBatch %q sha256 drifted: state pins %s but disk is %s", batchPointer.Path, batchPointer.SHA256, actualBatchSHA)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("observation-batch.schema.json", batchBytes); err != nil {
		return fmt.Errorf("sealed ObservationBatch %q schema is invalid: %w", batchPointer.Path, err)
	}
	var batch observationBatch
	if err := json.Unmarshal(batchBytes, &batch); err != nil {
		return fmt.Errorf("decode sealed ObservationBatch %q: %w", batchPointer.Path, err)
	}
	if batch.RuntimeID != stringField(state["runtime_id"]) {
		return fmt.Errorf("ObservationBatch runtime_id %q does not match Runtime %q", batch.RuntimeID, stringField(state["runtime_id"]))
	}
	if batch.BaselineGeneration != pinnedGeneration {
		return fmt.Errorf("ObservationBatch baseline_generation %d does not match Case baseline_generation %d", batch.BaselineGeneration, pinnedGeneration)
	}
	if batch.SubjectDigest != pinnedDigest {
		return fmt.Errorf("baseline_digest drift: Case pins %s but sealed ObservationBatch now declares %s", pinnedDigest, batch.SubjectDigest)
	}

	if review.PlanPointerFromState(state) != nil {
		plan, _, err := review.LoadPlan(root, state)
		if err != nil {
			return fmt.Errorf("current ReviewPlan cannot be revalidated: %w", err)
		}
		currentDigest := review.SubjectDigest(plan)
		if currentDigest != pinnedDigest {
			return fmt.Errorf("baseline_digest drift: Case pins %s but current ReviewPlan subjects digest to %s", pinnedDigest, currentDigest)
		}
	}
	return nil
}

// contractApprovalScope is the human_boundary scope prefix for the
// S8→S9 contract approval. It mirrors the runtime_rollover / runtime_governance
// prefixes used by store.go validateLifecycleApproval so one human_decision
// receipt authorizes exactly one verb at one revision.
const contractApprovalScope = "s8_contract_approval"

// validateContractApprovalEvidence enforces that the cited human_decision
// evidence is valid, produced by the named approver, and scoped to
// "s8_contract_approval:<runtime_id>@<revision>". A decision from another
// verb, another identity, or another revision is rejected.
func validateContractApprovalEvidence(state map[string]any, approvedBy, evidenceID string, revision int) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return errors.New("runtime evidence must be an array")
	}
	runtimeID := stringField(state["runtime_id"])
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || stringField(item["id"]) != evidenceID || stringField(item["kind"]) != "human_decision" || stringField(item["status"]) != "valid" {
			continue
		}
		if containsStringAny(item["produced_by"], approvedBy) && containsStringAny(item["scope_refs"], fmt.Sprintf("%s:%s@%d", contractApprovalScope, runtimeID, revision)) {
			return nil
		}
	}
	return fmt.Errorf("contract approval evidence %q must be valid human_decision evidence produced by %q and scoped to %s:%s@%d; register the decision artifact with `runtime evidence add --kind human_decision --scope-ref %s:%s@%d` before approving the Contract", evidenceID, approvedBy, contractApprovalScope, runtimeID, revision, contractApprovalScope, runtimeID, revision)
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
	if stringField(pointer["status"]) != "investigating" {
		return nil, actionableContractError("InvestigationCase %s is %q; only investigating Cases can receive first Contract approval", caseID, stringField(pointer["status"]))
	}
	return pointer, nil
}

func exactFindingSetWithDetails(caseIDs, contractIDs []string) error {
	left, err := normalizeSet(caseIDs, "InvestigationCase.source_finding_ids")
	if err != nil {
		return err
	}
	right, err := normalizeSet(contractIDs, "RepairContract.source_finding_ids")
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
	unexplained, err := stringSlice(document["unexplained_finding_ids"], "InvestigationCase.unexplained_finding_ids")
	if err != nil {
		return err
	}
	if len(unexplained) > 0 {
		return fmt.Errorf("unexplained Finding IDs remain: %v", unexplained)
	}
	if !nonEmptyObject(document["causal_model"]) {
		return errors.New("causal_model is missing or empty")
	}
	if strings.TrimSpace(stringField(document["primary_root_cause"])) == "" {
		return errors.New("primary_root_cause is missing")
	}
	if err := validateCausalClosure(document); err != nil {
		return err
	}
	if stringField(document["route"]) != "s9_repair" {
		return fmt.Errorf("route is %q, want s9_repair", stringField(document["route"]))
	}
	if strings.TrimSpace(stringField(document["route_reason"])) == "" {
		return errors.New("route_reason is missing")
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

func runtimeReferencesApproval(statePath, caseRel, caseSHA, contractRel, contractSHA string) bool {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	var state map[string]any
	if json.Unmarshal(data, &state) != nil {
		return false
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	return pointer != nil && stringField(pointer["path"]) == caseRel && stringField(pointer["sha256"]) == caseSHA && stringField(pointer["repair_contract_ref"]) == contractRel && stringField(pointer["repair_contract_sha256"]) == contractSHA
}

func actionableContractError(format string, args ...any) error {
	return fmt.Errorf(format+"; next: %s", append(args, contractNextCommand)...)
}
