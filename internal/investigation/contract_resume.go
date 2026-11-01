package investigation

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"os"
	"path/filepath"
	"reflect"
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
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("approved Case pointer drift")
	}
	rel := stringField(pointer["repair_contract_ref"])
	path, err := repositoryPath(root, rel)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("approved contract pointer drift")
	}
	var caseDoc map[string]any
	if json.Unmarshal(caseBytes, &caseDoc) != nil || stringField(caseDoc["case_id"]) != req.CaseID {
		return runtime.Snapshot{}, fmt.Errorf("approved Case identity is unavailable")
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
	if err != nil && !os.IsNotExist(err) {
		return runtime.Snapshot{}, err
	}
	authority, _ := approved["delegated_authority"].(map[string]any)
	// The accepted immutable Contract and Case already carry the authority.
	// A missing private draft cannot undo that receipt; if bytes are still
	// present, changed professional content remains a conflicting retry.
	if stringField(approved["case_id"]) != req.CaseID || (err == nil && !sameApprovedDraft(approved, draft)) || stringField(approved["approved_by"]) != req.ApprovedBy || stringField(approved["approval_evidence_id"]) != req.ApprovalEvidenceID || stringField(authority["delegation_evidence_id"]) != req.DelegationEvidenceID {
		return runtime.Snapshot{}, fmt.Errorf("Case already has a different approval; consume the current contract or use causal reassessment, do not overwrite it")
	}
	return current, nil
}

func sameApprovedDraft(approved map[string]any, raw []byte) bool {
	var draft map[string]any
	if json.Unmarshal(raw, &draft) != nil {
		return false
	}
	copy := cloneMap(approved)
	for _, field := range []string{"approved_by", "approved_at", "approver_id", "approval_evidence_id", "delegated_authority"} {
		delete(copy, field)
	}
	copy["status"], copy["revision"] = "draft", draft["revision"]
	return reflect.DeepEqual(copy, draft)
}
