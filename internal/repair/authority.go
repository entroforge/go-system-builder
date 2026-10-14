package repair

import (
	"errors"
	"fmt"
	"time"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
)

// RestoreAuthorityFingerprint does not recapture or approve a new baseline.
// It restores only the exact digest already pinned by an immutable Session,
// after the normal authority/confirmation gates have checked current bytes.
func RestoreAuthorityFingerprint(root, statePath, journalPath string, req RuntimeRequest) (runtimepkg.Snapshot, error) {
	if req.OperationID != "" {
		return runtimepkg.Snapshot{}, errors.New("authority restore does not accept operation IDs")
	}
	if req.Actor == "" {
		return runtimepkg.Snapshot{}, errors.New("authority restore requires an actor")
	}
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	validate := func(state map[string]any) (RepairSession, error) {
		p := repairPointer(state)
		if p == nil || lifecycleState(state) != "bug_resolution" {
			return RepairSession{}, errors.New("authority restore requires an active S9 Session")
		}
		ref, err := pointerArtifact(p, "path", "sha256", "RepairSession")
		if err != nil {
			return RepairSession{}, err
		}
		session, err := ValidateRepairSession(root, ref)
		if err != nil {
			return RepairSession{}, err
		}
		if session.RuntimeID != stringField(state["runtime_id"]) || session.ReqID != boundReqID(state) || session.BaselineGeneration != baselineGeneration(state) || session.SessionID != stringField(p["session_id"]) || session.ContractRef != stringField(p["contract_ref"]) || session.ContractSHA256 != stringField(p["contract_sha256"]) {
			return RepairSession{}, errors.New("authority restore Session is not bound to the current Runtime and Contract")
		}
		if _, err := ValidateApprovedContractRef(root, ContractRef{Path: session.ContractRef, SHA256: session.ContractSHA256}); err != nil {
			return RepairSession{}, err
		}
		if existing := stringMapField(p["authority_fingerprint"])[session.SessionID]; existing != "" && existing != session.BaselineDigest {
			return RepairSession{}, errors.New("authority restore refuses to replace a different fingerprint")
		}
		// Missing metadata cannot authorize in-flight, unrecorded mutations.
		actual, err := ComputeSessionChangeset(root, session)
		if err != nil {
			return RepairSession{}, err
		}
		var committed []RepairResult
		refs, refsErr := currentRepairResultRefs(p)
		if refsErr == nil {
			for _, ref := range refs {
				value, err := ValidateRepairResult(root, ref)
				if err != nil {
					return RepairSession{}, err
				}
				if value.SessionID != session.SessionID {
					return RepairSession{}, errors.New("authority restore Result belongs to another Session")
				}
				committed = append(committed, value)
			}
		}
		claimed, err := aggregateRepairResultArtifacts(committed)
		if err != nil {
			return RepairSession{}, err
		}
		if err := exactChangedArtifactSet(claimed, actual, "committed repair changes", "current Session changes"); err != nil {
			return RepairSession{}, fmt.Errorf("authority restore refuses unrecorded changes: %w", err)
		}
		clone := map[string]any{}
		for key, value := range p {
			clone[key] = value
		}
		fp := stringMapField(p["authority_fingerprint"])
		fp[session.SessionID] = session.BaselineDigest
		anyFP := map[string]any{}
		for k, v := range fp {
			anyFP[k] = v
		}
		clone["authority_fingerprint"] = anyFP
		if err := checkS9AuthorityFreshness(root, clone, nil); err != nil {
			return RepairSession{}, err
		}
		return session, nil
	}
	session, err := validate(current.State)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if stringMapField(repairPointer(current.State)["authority_fingerprint"])[session.SessionID] == session.BaselineDigest {
		return current, nil
	}
	at := occurred(req.OccurredAt)
	return updateRuntime(writer, current, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-authority-restore-r%d", current.Revision+1), TransitionID: whitelistChecked("S9-AUTHORITY-RESTORE"), Event: "repair_authority_restored", Actor: req.Actor, RuntimeID: session.RuntimeID, IdempotencyKey: fmt.Sprintf("runtime:s9:authority-restore:%s:%d", session.SessionID, current.Revision), From: cursor(current.State), To: cursor(current.State), EvidenceIDs: []string{session.SessionID}, GateID: "S9-AUTHORITY-RESTORE", GateFingerprint: session.BaselineDigest, OccurredAt: at, Apply: func(state map[string]any) error {
		checked, err := validate(state)
		if err != nil {
			return err
		}
		p := stateRepairPointer(state)
		fp := stringMapField(p["authority_fingerprint"])
		fp[checked.SessionID] = checked.BaselineDigest
		anyFP := map[string]any{}
		for k, v := range fp {
			anyFP[k] = v
		}
		p["authority_fingerprint"] = anyFP
		p["updated_at"] = at.Format(time.RFC3339Nano)
		return nil
	}})
}
