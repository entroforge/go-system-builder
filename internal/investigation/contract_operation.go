package investigation

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// A retry pins the original draft hash and approval identity. It may recover a
// pending commit before reading the current Case or consumed approval evidence.
// ExpectedRevision controls CAS and is deliberately not a logical input.
func prepareContractOperation(ctx context.Context, root, statePath, journalPath string, request ContractRequest) (*runtime.Operation, runtime.Snapshot, bool, error) {
	if request.OperationID == "" {
		return nil, runtime.Snapshot{}, false, nil
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, runtime.Snapshot{}, false, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, runtime.Snapshot{}, false, err
	}
	contract, err := relativeContractPath(root, request.ContractPath)
	if err != nil {
		return nil, runtime.Snapshot{}, false, err
	}
	data, err = json.Marshal(map[string]any{
		"case_id": request.CaseID, "contract_path": contract,
		"approved_by": strings.TrimSpace(request.ApprovedBy), "approval_hash": strings.TrimSpace(request.ApprovalHash),
		"approval_evidence_id": strings.TrimSpace(request.ApprovalEvidenceID), "delegation_evidence_id": strings.TrimSpace(request.DelegationEvidenceID),
		"occurred_at": request.OccurredAt,
	})
	if err != nil {
		return nil, runtime.Snapshot{}, false, err
	}
	op := &runtime.Operation{ID: request.OperationID, InputSHA256: sha256Hex(data), RuntimeID: stringField(state["runtime_id"]), Actor: "orchestrator", Kind: "S8-REPAIR-CONTRACT-APPROVAL"}
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	prior, replayed, err := store.LookupOperation(*op)
	return op, prior, replayed, err
}
