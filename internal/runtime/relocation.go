package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"
)

// RelocateWorkspace changes only Main/Worker coordinates. It cannot execute an
// arbitrary mutation and does not grant callers a general authority exemption.
// The composition root holds the Git/launch leases and validates native Git links.
// The immutable source snapshot comes from the relocation intent, not a rebuilt
// baseline. Existing commit markers provide the only journal recovery protocol.
func (s *Store) RelocateWorkspace(before Snapshot, registry map[string]any, id, reason string) (Snapshot, error) {
	if err := s.requireCandidateValidator(); err != nil {
		return Snapshot{}, err
	}
	if id == "" || reason == "" {
		return Snapshot{}, fmt.Errorf("relocation identity and reason required")
	}
	source, err := cloneState(before.State)
	if err != nil {
		return Snapshot{}, err
	}
	old, _ := source["workspace"].(map[string]any)
	bound, _ := source["bound_req"].(map[string]any)
	authority, _ := bound["workspace"].(map[string]any)
	if old == nil || authority == nil || old["main_root"] != authority["project_root"] || old["integration_branch"] != authority["dev_branch"] {
		return Snapshot{}, fmt.Errorf("relocation source authority mismatch")
	}
	oldRoot, _ := old["main_root"].(string)
	newRoot, err := authorityPath(s.root)
	if err != nil {
		return Snapshot{}, err
	}
	if newRoot != s.root || oldRoot == newRoot || registry["main_root"] != newRoot {
		return Snapshot{}, fmt.Errorf("relocation requires distinct canonical old/new roots")
	}
	if _, err := os.Stat(oldRoot); !os.IsNotExist(err) {
		return Snapshot{}, fmt.Errorf("old Main must be absent; preserve copied or inaccessible authority")
	}
	// Reject every registry change except explicitly mapped filesystem coordinates.
	normalized, err := cloneState(registry)
	if err != nil {
		return Snapshot{}, err
	}
	normalized["main_root"], normalized["common_dir"] = old["main_root"], old["common_dir"]
	for _, key := range []string{"executions", "execution_history"} {
		switch rows := normalized[key].(type) {
		case map[string]any:
			previous, _ := old[key].(map[string]any)
			for k, raw := range rows {
				row, ok := raw.(map[string]any)
				prior, exists := previous[k].(map[string]any)
				if !ok || !exists {
					return Snapshot{}, fmt.Errorf("relocation execution mismatch")
				}
				row["worktree_path"] = prior["worktree_path"]
			}
		case []any:
			previous, _ := old[key].([]any)
			if len(previous) != len(rows) {
				return Snapshot{}, fmt.Errorf("relocation history mismatch")
			}
			for i, raw := range rows {
				row, ok := raw.(map[string]any)
				prior, exists := previous[i].(map[string]any)
				if !ok || !exists {
					return Snapshot{}, fmt.Errorf("relocation history malformed")
				}
				row["worktree_path"] = prior["worktree_path"]
			}
		}
	}
	if !reflect.DeepEqual(normalized, old) {
		return Snapshot{}, fmt.Errorf("relocation may only change filesystem coordinates")
	}
	target, err := cloneState(source)
	if err != nil {
		return Snapshot{}, err
	}
	target["bound_req"].(map[string]any)["workspace"].(map[string]any)["project_root"] = newRoot
	target["workspace"] = registry
	target["root"] = newRoot
	sourceHash, err := hashState(source)
	if err != nil {
		return Snapshot{}, err
	}
	release, err := acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	// A private copy limits the exception to this transaction and exact source.
	tx := *s
	tx.relocationSourceHash = sourceHash
	// Other pending operations must be recovered under their original authority.
	for _, path := range []string{s.rolloverMarkerPath(), s.fingerprintMarkerPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return Snapshot{}, fmt.Errorf("unrelated pending operation at %s; restore original location to recover it", path)
		}
	}
	if data, err := os.ReadFile(s.commitMarkerPath()); err == nil {
		var pending commitPending
		if err = json.Unmarshal(data, &pending); err != nil {
			return Snapshot{}, err
		}
		expected, _ := cloneState(target)
		expected["revision"], expected["journal"] = pending.State["revision"], pending.State["journal"]
		if pending.PreviousStateSHA256 != sourceHash || pending.PreviousRevision != before.Revision || pending.IdempotencyKey != id || !reflect.DeepEqual(expected, pending.State) {
			return Snapshot{}, fmt.Errorf("pending commit is not this relocation")
		}
		// Commit may have reached its target and then stopped during rotation.
		// Resolve the duplicated archive/active prefix before commit recovery
		// inspects the journal, only under the validated new authority.
		if _, rotationErr := os.Stat(s.journalRotationMarkerPath()); rotationErr == nil {
			if err := s.validateCandidate(pending.State); err != nil {
				return Snapshot{}, err
			}
			if err := validatePendingCommitCoherence(pending); err != nil {
				return Snapshot{}, err
			}
			current, err := s.read()
			if err != nil {
				return Snapshot{}, err
			}
			if !reflect.DeepEqual(current, pending.State) {
				return Snapshot{}, fmt.Errorf("rotation does not belong to committed relocation")
			}
			if err := s.recoverPendingJournalRotationLocked(); err != nil {
				return Snapshot{}, err
			}
		} else if !os.IsNotExist(rotationErr) {
			return Snapshot{}, rotationErr
		}
		if err = tx.recoverPendingCommitLocked(); err != nil {
			return Snapshot{}, err
		}
	} else if !os.IsNotExist(err) {
		return Snapshot{}, err
	}
	current, err := tx.read()
	if err != nil {
		return Snapshot{}, err
	}
	revision, err := integerField(current, "revision")
	if err != nil {
		return Snapshot{}, err
	}
	// Idempotent pointer recovery may run after later ordinary Runtime commits.
	if reflect.DeepEqual(current["workspace"], registry) {
		currentBound, _ := current["bound_req"].(map[string]any)
		currentAuthority, _ := currentBound["workspace"].(map[string]any)
		if current["runtime_id"] != source["runtime_id"] || currentAuthority["project_root"] != newRoot {
			return Snapshot{}, fmt.Errorf("relocation identity changed")
		}
		// A rotation interrupted after this relocation committed now belongs
		// to the ordinary new authority. Recover it without the old-state exception.
		if err := s.recoverPendingWritesLocked(); err != nil {
			return Snapshot{}, err
		}
		return Snapshot{Revision: revision, State: current}, nil
	}
	if err := tx.reportPendingOperationLocked(); err != nil {
		return Snapshot{}, err
	}
	hash, err := hashState(current)
	if err != nil {
		return Snapshot{}, err
	}
	if hash != sourceHash || revision != before.Revision {
		return Snapshot{}, ErrStaleRevision
	}
	return tx.applyMutation(before.Revision, Mutation{EventID: id, IdempotencyKey: id, RuntimeID: stringValue(source["runtime_id"]), TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RetainLastTransition: true, Message: "explicit Main relocation: " + reason, Apply: func(state map[string]any) error {
		state["bound_req"].(map[string]any)["workspace"].(map[string]any)["project_root"] = newRoot
		state["workspace"], state["root"] = registry, newRoot
		return nil
	}})
}
