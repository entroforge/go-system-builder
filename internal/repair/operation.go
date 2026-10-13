package repair

import (
	"encoding/json"
	"errors"
	"os"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func prepareRepairOperation(root, statePath, journalPath string, request RuntimeRequest, actor, kind string, input any) (*runtimepkg.Operation, runtimepkg.Snapshot, bool, error) {
	if request.OperationID == "" {
		return nil, runtimepkg.Snapshot{}, false, nil
	}
	// Read only the identity here. Lookup validates the complete state/journal
	// pair and, for this explicit writer retry, completes pending recovery.
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, runtimepkg.Snapshot{}, false, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, runtimepkg.Snapshot{}, false, err
	}
	data, err = canonicalJSON(map[string]any{"input": input, "occurred_at": request.OccurredAt, "actor": actor})
	if err != nil {
		return nil, runtimepkg.Snapshot{}, false, err
	}
	op := &runtimepkg.Operation{ID: request.OperationID, RuntimeID: stringField(state["runtime_id"]), Actor: actor, Kind: kind, InputSHA256: sha256Bytes(data)}
	writer := runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	prior, found, err := writer.LookupOperation(*op)
	return op, prior, found, err
}

// The logical input is hashed before generated timestamps and derived refs are
// added. An original receipt, including concurrent commit replay, owns the
// returned artifact. Reading it does not re-evaluate yesterday's product diff.
func runRepairArtifactOperation[T any](root, statePath, journalPath string, request RuntimeRequest, actor, kind, schemaName string, input any, run func(RuntimeRequest) (runtimepkg.Snapshot, T, ArtifactRef, error)) (runtimepkg.Snapshot, T, ArtifactRef, error) {
	var empty T
	op, prior, found, err := prepareRepairOperation(root, statePath, journalPath, request, actor, kind, input)
	if err != nil {
		return runtimepkg.Snapshot{}, empty, ArtifactRef{}, err
	}
	if op == nil {
		return run(request)
	}
	if !found {
		request.operation = op
		prior, _, _, err = run(request)
		if err != nil {
			// Another invocation may have committed after our initial lookup
			// and before the domain status check. Resolve that race through
			// the authoritative receipt, never by bypassing domain validation.
			writer := runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
			replayed, committed, lookupErr := writer.LookupOperation(*op)
			if lookupErr != nil || !committed {
				return runtimepkg.Snapshot{}, empty, ArtifactRef{}, errors.Join(err, lookupErr)
			}
			prior = replayed
		}
	}
	if prior.Operation == nil || len(prior.Operation.Artifacts) != 1 {
		return runtimepkg.Snapshot{}, empty, ArtifactRef{}, errors.New("repair operation has no unique durable artifact receipt; inspect runtime operation before retrying")
	}
	a := prior.Operation.Artifacts[0]
	ref := ArtifactRef{Path: a.Path, SHA256: a.SHA256}
	var result T
	if err := decodeArtifact(root, ref, schemaName, &result); err != nil {
		return runtimepkg.Snapshot{}, empty, ArtifactRef{}, err
	}
	return prior, result, ref, nil
}

func OpenRepairSession(root, statePath, journalPath string, req OpenSessionRequest) (runtimepkg.Snapshot, RepairSession, ArtifactRef, error) {
	if req.Actor == "" {
		req.Actor = req.CreatedBy
	}
	if req.CreatedBy == "" {
		req.CreatedBy = req.Actor
	}
	input := map[string]any{"session_id": req.SessionID, "created_by": req.CreatedBy, "req_id": req.ReqID}
	// Preserve predecessor implement-operation identity for retries after upgrade.
	if req.Intent != "" && req.Intent != "implement" || len(req.ConfirmationSources) > 0 {
		input["intent"] = req.Intent
		input["confirmation_sources"] = req.ConfirmationSources
	}
	return runRepairArtifactOperation(root, statePath, journalPath, req.RuntimeRequest, req.Actor, "S9-SESSION-OPEN", "repair-session.schema.json", input, func(request RuntimeRequest) (runtimepkg.Snapshot, RepairSession, ArtifactRef, error) {
		req.RuntimeRequest = request
		return openRepairSession(root, statePath, journalPath, req)
	})
}

func CompileRepairPlan(root, statePath, journalPath string, req CompilePlanRequest) (runtimepkg.Snapshot, RepairPlan, ArtifactRef, error) {
	actor := req.Actor
	if actor == "" {
		actor = req.CreatedBy
	}
	input := map[string]any{"plan_id": req.PlanID, "created_by": req.CreatedBy}
	return runRepairArtifactOperation(root, statePath, journalPath, req.RuntimeRequest, actor, "PTR-BUG-09", "repair-plan.schema.json", input, func(request RuntimeRequest) (runtimepkg.Snapshot, RepairPlan, ArtifactRef, error) {
		req.RuntimeRequest = request
		return compileRepairPlan(root, statePath, journalPath, req)
	})
}

func SubmitPlanReportDraftToRuntime(root, statePath, journalPath string, req RuntimeRequest, draft PlanReportRequest) (runtimepkg.Snapshot, PlanReport, ArtifactRef, error) {
	actor := req.Actor
	if actor == "" {
		actor = draft.AgentID
	}
	return runRepairArtifactOperation(root, statePath, journalPath, req, actor, "PTR-BUG-10", "repair-plan-report.schema.json", draft, func(request RuntimeRequest) (runtimepkg.Snapshot, PlanReport, ArtifactRef, error) {
		return submitPlanReportDraftToRuntime(root, statePath, journalPath, request, draft)
	})
}

func SubmitRepairResultToRuntime(root, statePath, journalPath string, req SubmitResultRuntimeRequest) (runtimepkg.Snapshot, RepairResult, ArtifactRef, error) {
	actor := req.Actor
	if actor == "" {
		actor = req.Result.ProducerAgentID
	}
	return runRepairArtifactOperation(root, statePath, journalPath, req.RuntimeRequest, actor, "S9-RESULT-SUBMIT", "repair-result.schema.json", req.Result, func(request RuntimeRequest) (runtimepkg.Snapshot, RepairResult, ArtifactRef, error) {
		req.RuntimeRequest = request
		return submitRepairResultToRuntime(root, statePath, journalPath, req)
	})
}

func CommitRepairHandoff(root, statePath, journalPath string, req CommitHandoffRequest) (runtimepkg.Snapshot, error) {
	actor := req.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	op, prior, found, err := prepareRepairOperation(root, statePath, journalPath, req.RuntimeRequest, actor, "TR-012", req.Handoff)
	if err != nil || found {
		return prior, err
	}
	req.operation = op
	snapshot, err := commitRepairHandoff(root, statePath, journalPath, req)
	if err != nil && op != nil {
		writer := runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
		prior, found, lookupErr := writer.LookupOperation(*op)
		if lookupErr == nil && found {
			return prior, nil
		}
		return runtimepkg.Snapshot{}, errors.Join(err, lookupErr)
	}
	return snapshot, err
}
