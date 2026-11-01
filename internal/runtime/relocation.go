package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
				if err := normalizeRelocatedExecution(row, prior, oldRoot, newRoot); err != nil {
					return Snapshot{}, err
				}
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
				if err := normalizeRelocatedExecution(row, prior, oldRoot, newRoot); err != nil {
					return Snapshot{}, err
				}
			}
		}
	}
	comparableNew, err := relocationComparableRegistry(normalized)
	if err != nil {
		return Snapshot{}, err
	}
	comparableOld, err := relocationComparableRegistry(old)
	if err != nil {
		return Snapshot{}, err
	}
	if !reflect.DeepEqual(comparableNew, comparableOld) {
		return Snapshot{}, fmt.Errorf("relocation may only change filesystem coordinates")
	}
	// Retain original Runtime metadata verbatim; only the validated coordinates
	// come from the destination projection. Historical machine fields are not
	// required for the move and are not silently rewritten by it.
	retainedRegistry, err := relocationRegistryWithCoordinates(old, registry)
	if err != nil {
		return Snapshot{}, err
	}
	target, err := cloneState(source)
	if err != nil {
		return Snapshot{}, err
	}
	target["bound_req"].(map[string]any)["workspace"].(map[string]any)["project_root"] = newRoot
	target["workspace"] = retainedRegistry
	target["root"] = newRoot
	release, err := s.acquireLock(s.statePath+".lock", 5*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	// A private copy limits the exception to this transaction and exact source.
	tx := *s
	tx.relocationSourceState = source
	// Other pending operations must be recovered under their original authority.
	for _, path := range []string{s.rolloverMarkerPath()} {
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
		if pending.PreviousRevision != before.Revision || pending.IdempotencyKey != id || !reflect.DeepEqual(expected, pending.State) {
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
	if reflect.DeepEqual(current["workspace"], retainedRegistry) {
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
	if !reflect.DeepEqual(current, source) || revision != before.Revision {
		return Snapshot{}, ErrStaleRevision
	}
	return tx.applyMutation(before.Revision, Mutation{EventID: id, IdempotencyKey: id, RuntimeID: stringValue(source["runtime_id"]), TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RetainLastTransition: true, Message: "explicit Main relocation: " + reason, Apply: func(state map[string]any) error {
		state["bound_req"].(map[string]any)["workspace"].(map[string]any)["project_root"] = newRoot
		state["workspace"], state["root"] = retainedRegistry, newRoot
		return nil
	}})
}

// PlanRebind moves only evidence locators. Normalize exactly that declared
// old-root/new-root mapping before comparing current responsibility fields;
// captured checkout, command, profile and binding remain unchanged.
func normalizeRelocatedExecution(row, prior map[string]any, oldRoot, newRoot string) error {
	row["worktree_path"] = prior["worktree_path"]
	for _, field := range []string{"tool_check_receipts", "owned_check_receipts"} {
		current, _ := row[field].([]any)
		previous, _ := prior[field].([]any)
		if len(current) != len(previous) {
			return fmt.Errorf("relocation cannot change %s membership", field)
		}
		for i, raw := range current {
			ref, ok := raw.(map[string]any)
			old, existed := previous[i].(map[string]any)
			if !ok || !existed {
				return fmt.Errorf("relocation %s is malformed", field)
			}
			if field == "owned_check_receipts" {
				ref, ok = ref["receipt"].(map[string]any)
				old, existed = old["receipt"].(map[string]any)
				if !ok || !existed {
					return fmt.Errorf("relocation owned-check receipt is malformed")
				}
			}
			oldPath, _ := old["path"].(string)
			rel, err := filepath.Rel(oldRoot, oldPath)
			if !filepath.IsAbs(oldPath) || err != nil || !filepath.IsLocal(rel) || ref["path"] != filepath.Join(newRoot, rel) {
				return fmt.Errorf("relocation receipt locator differs from its exact original Main mapping")
			}
			ref["path"] = oldPath
		}
	}
	return nil
}

// Compare only the current workspace contract. Unknown historical members stay
// in the original registry and are retained verbatim by the coordinate update.
// This projection adds no parsing or validation requirements to relocation.
func relocationComparableRegistry(registry map[string]any) (map[string]any, error) {
	out := relocationFields(registry, "binding_version", "main_root", "common_dir", "integration_branch")
	for _, key := range []string{"executions", "execution_history"} {
		raw, exists := registry[key]
		if !exists {
			continue
		}
		switch rows := raw.(type) {
		case map[string]any:
			next := make(map[string]any, len(rows))
			for id, row := range rows {
				next[id] = relocationExecutionContent(row)
			}
			out[key] = next
		case []any:
			next := make([]any, len(rows))
			for i, row := range rows {
				next[i] = relocationExecutionContent(row)
			}
			out[key] = next
		default:
			out[key] = raw
		}
	}
	return out, nil
}

func relocationFields(raw any, fields ...string) map[string]any {
	row, _ := raw.(map[string]any)
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		if value, exists := row[field]; exists {
			out[field] = value
		}
	}
	return out
}

func relocationArrayContent(raw any, project func(any) any) any {
	rows, ok := raw.([]any)
	if !ok {
		return raw
	}
	out := make([]any, len(rows))
	for i, row := range rows {
		out[i] = project(row)
	}
	return out
}

func relocationExecutionContent(raw any) any {
	row, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	out := relocationFields(row, "purpose", "work_packet_id", "stage_entry", "check_location", "rework_ref", "platform_session_id", "assignment_id", "runtime_id", "baseline_generation", "execution_generation", "agent_id", "worktree_path", "branch", "target_branch", "status", "write_paths", "required_checks", "delivery_ref")
	if value, exists := row["assignment_record"]; exists {
		if _, ok := value.(map[string]any); ok {
			out["assignment_record"] = relocationFields(value, "runtime_id", "baseline_generation", "assignment_id", "assignment_revision")
		} else {
			out["assignment_record"] = value
		}
	}
	if value, exists := row["inputs"]; exists {
		if inputs, ok := value.(map[string]any); ok {
			paths := make(map[string]any, len(inputs))
			for path := range inputs {
				paths[path] = path
			}
			out["inputs"] = paths
		} else {
			out["inputs"] = value
		}
	}
	for _, key := range []string{"check_profiles", "tool_check_receipts", "owned_check_receipts", "worker_checks"} {
		if value, exists := row[key]; exists {
			out[key] = relocationArrayContent(value, func(item any) any {
				if _, ok := item.(map[string]any); !ok {
					return item
				}
				switch key {
				case "check_profiles":
					return relocationProfileContent(item)
				case "tool_check_receipts":
					return relocationToolReceiptContent(item)
				case "owned_check_receipts":
					result := relocationFields(item, "worker_check_index")
					if value, exists := item.(map[string]any)["receipt"]; exists {
						result["receipt"] = relocationToolReceiptContent(value)
					}
					return result
				default:
					result := relocationFields(item, "index", "command")
					if value, exists := item.(map[string]any)["profile"]; exists {
						result["profile"] = relocationProfileContent(value)
					}
					return result
				}
			})
		}
	}
	return out
}

func relocationToolReceiptContent(raw any) any {
	row, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	out := relocationFields(row, "path", "checkout", "command")
	if value, exists := row["binding"]; exists {
		if _, ok := value.(map[string]any); ok {
			out["binding"] = relocationFields(value, "runtime_id", "baseline_generation", "assignment_id", "owner", "execution_generation", "responsibility")
		} else {
			out["binding"] = value
		}
	}
	return out
}

func relocationProfileContent(raw any) any {
	row, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	out := relocationFields(row, "reuse_mode", "id", "version", "check_index", "command", "location", "timeout_seconds", "environment_policy", "input_closure", "side_effects", "reuse_policy")
	for _, key := range []string{"environment", "dependencies", "fixtures"} {
		if value, exists := row[key]; exists {
			out[key] = relocationArrayContent(value, func(item any) any {
				if _, ok := item.(map[string]any); !ok {
					return item
				}
				if key == "environment" {
					return relocationFields(item, "name", "value")
				}
				return relocationFields(item, "path")
			})
		}
	}
	return out
}

func relocationRegistryWithCoordinates(old, proposed map[string]any) (map[string]any, error) {
	retained, err := cloneState(old)
	if err != nil {
		return nil, err
	}
	retained["main_root"], retained["common_dir"] = proposed["main_root"], proposed["common_dir"]
	move := func(dst, src map[string]any) error {
		dst["worktree_path"] = src["worktree_path"]
		for _, field := range []string{"tool_check_receipts", "owned_check_receipts"} {
			original, _ := dst[field].([]any)
			next, _ := src[field].([]any)
			if len(original) != len(next) {
				return fmt.Errorf("relocation receipt membership changed")
			}
			for i := range original {
				o, ok1 := original[i].(map[string]any)
				n, ok2 := next[i].(map[string]any)
				if !ok1 || !ok2 {
					return fmt.Errorf("relocation receipt malformed")
				}
				if field == "owned_check_receipts" {
					o, ok1 = o["receipt"].(map[string]any)
					n, ok2 = n["receipt"].(map[string]any)
				}
				if !ok1 || !ok2 {
					return fmt.Errorf("relocation owned receipt malformed")
				}
				o["path"] = n["path"]
			}
		}
		return nil
	}
	for _, key := range []string{"executions", "execution_history"} {
		switch rows := retained[key].(type) {
		case map[string]any:
			next, _ := proposed[key].(map[string]any)
			for id, raw := range rows {
				row, ok1 := raw.(map[string]any)
				n, ok2 := next[id].(map[string]any)
				if !ok1 || !ok2 {
					return nil, fmt.Errorf("relocation execution mismatch")
				}
				if err := move(row, n); err != nil {
					return nil, err
				}
			}
		case []any:
			next, _ := proposed[key].([]any)
			if len(rows) != len(next) {
				return nil, fmt.Errorf("relocation history mismatch")
			}
			for i, raw := range rows {
				row, ok1 := raw.(map[string]any)
				n, ok2 := next[i].(map[string]any)
				if !ok1 || !ok2 {
					return nil, fmt.Errorf("relocation history mismatch")
				}
				if err := move(row, n); err != nil {
					return nil, err
				}
			}
		}
	}
	return retained, nil
}
