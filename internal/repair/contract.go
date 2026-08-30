package repair

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func ValidateApprovedContractRef(root string, ref ContractRef) (ApprovedContract, error) {
	data, err := readArtifact(root, ArtifactRef{Path: ref.Path, SHA256: ref.SHA256}, "repair-contract.schema.json")
	if err != nil {
		return ApprovedContract{}, fmt.Errorf("validate approved RepairContract reference: %w", err)
	}
	var document struct {
		ContractID             string       `json:"repair_contract_id"`
		CaseID                 string       `json:"case_id"`
		Revision               int          `json:"revision"`
		Status                 string       `json:"status"`
		SourceFindingIDs       []string     `json:"source_finding_ids"`
		Units                  []RepairUnit `json:"repair_units"`
		ProspectiveScope       []string     `json:"prospective_scope"`
		ForbiddenScope         []string     `json:"forbidden_scope"`
		CompatibilityMigration string       `json:"compatibility_migration"`
		ApprovedBy             string       `json:"approved_by"`
		ApprovedAt             string       `json:"approved_at"`
		ApprovalHash           string       `json:"approval_hash"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return ApprovedContract{}, fmt.Errorf("decode approved RepairContract: %w", err)
	}
	if document.Status != "approved" {
		return ApprovedContract{}, fmt.Errorf("RepairContract %q is %q; S9 requires status=approved", ref.Path, document.Status)
	}
	if strings.TrimSpace(document.ApprovedBy) == "" || strings.TrimSpace(document.ApprovedAt) == "" || strings.TrimSpace(document.ApprovalHash) == "" {
		return ApprovedContract{}, fmt.Errorf("approved RepairContract %q lacks approval provenance; require approved_by, approved_at, and approval_hash", ref.Path)
	}
	if len(document.Units) == 0 || len(document.SourceFindingIDs) == 0 {
		return ApprovedContract{}, fmt.Errorf("approved RepairContract %q has no repair units or source Findings", ref.Path)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-contract.schema.json", data); err != nil {
		return ApprovedContract{}, err
	}
	relative := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref.Path)))
	if filepath.IsAbs(ref.Path) {
		var err error
		relative, err = relativePath(root, ref.Path)
		if err != nil {
			return ApprovedContract{}, err
		}
	}
	return ApprovedContract{
		ContractID: document.ContractID, CaseID: document.CaseID, Revision: document.Revision, Status: document.Status,
		SourceFindingIDs: append([]string(nil), document.SourceFindingIDs...), Units: append([]RepairUnit(nil), document.Units...),
		ProspectiveScope: append([]string(nil), document.ProspectiveScope...), ForbiddenScope: append([]string(nil), document.ForbiddenScope...), CompatibilityMigration: document.CompatibilityMigration,
		Ref: ContractRef{Path: relative, SHA256: ref.SHA256},
	}, nil
}

func ValidateApprovedContract(root string, ref ArtifactReference) (ApprovedContract, error) {
	return ValidateApprovedContractRef(root, ContractRef{Path: ref.Path, SHA256: ref.SHA256})
}

// contractAssertionIDs turns the three Contract assertion arrays into the
// stable slot IDs consumed by PlanReport/TargetedReverification. The Contract
// remains the only source of truth; the plan stores the derived map so a
// Worker can see the required coverage without rereading free-form prose.
func contractAssertionIDs(root string, ref ContractRef) ([]string, error) {
	data, err := readArtifact(root, ArtifactRef{Path: ref.Path, SHA256: ref.SHA256}, "repair-contract.schema.json")
	if err != nil {
		return nil, err
	}
	var contract map[string]any
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil, fmt.Errorf("decode approved RepairContract assertions: %w", err)
	}
	ids := []string{}
	for _, slot := range []struct{ field, prefix string }{{"symptom_assertions", "symptom"}, {"root_invariant_assertions", "root"}, {"detection_gap_assertions", "gap"}} {
		field, prefix := slot.field, slot.prefix
		values, ok := contract[field].([]any)
		if !ok {
			continue
		}
		for index := range values {
			ids = append(ids, fmt.Sprintf("%s-%d", prefix, index+1))
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("approved RepairContract has no assertion slots")
	}
	return ids, nil
}
