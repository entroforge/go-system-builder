package investigation

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"os"
	"path/filepath"
)

// A response-loss retry returns the durable approval; it never spends another
// delegation use or asks for a second human receipt. Changed bytes/identity
// remain an error and must use the existing causal-reassessment path.
func resumeApprovedContract(root string, current runtime.Snapshot, pointer map[string]any, req ContractRequest) (runtime.Snapshot, error) {
	casePath, err := repositoryPath(root, stringField(pointer["path"]))
	if err != nil {
		return runtime.Snapshot{}, err
	}
	caseBytes, err := os.ReadFile(casePath)
	if err != nil || sha256Hex(caseBytes) != stringField(pointer["sha256"]) {
		return runtime.Snapshot{}, fmt.Errorf("approved Case pointer drift")
	}
	rel := stringField(pointer["repair_contract_ref"])
	path, err := repositoryPath(root, rel)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil || sha256Hex(data) != stringField(pointer["repair_contract_sha256"]) {
		return runtime.Snapshot{}, fmt.Errorf("approved contract pointer drift")
	}
	var approved map[string]any
	if err := json.Unmarshal(data, &approved); err != nil {
		return runtime.Snapshot{}, err
	}
	draftRel, err := relativeContractPath(root, req.ContractPath)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	draft, err := os.ReadFile(filepath.Join(root, draftRel))
	if err != nil {
		return runtime.Snapshot{}, err
	}
	authority, _ := approved["delegated_authority"].(map[string]any)
	if stringField(approved["case_id"]) != req.CaseID || stringField(approved["approval_hash"]) != req.ApprovalHash || sha256Hex(draft) != req.ApprovalHash || stringField(approved["approved_by"]) != req.ApprovedBy || stringField(approved["approval_evidence_id"]) != req.ApprovalEvidenceID || stringField(authority["delegation_evidence_id"]) != req.DelegationEvidenceID {
		return runtime.Snapshot{}, fmt.Errorf("Case already has a different approval; consume the current contract or use causal reassessment, do not overwrite it")
	}
	return current, nil
}
