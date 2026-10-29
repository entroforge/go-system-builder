package assignmentrecord

import (
	"encoding/json"
	"fmt"
)

// AllowsPath uses the same normalized scope semantics as Record construction.
func AllowsPath(record Record, path string) bool {
	child, err := normalizeScope(path)
	if err != nil {
		return false
	}
	for _, parent := range record.EffectiveWritePaths {
		if scopeContains(parent, child) {
			return true
		}
	}
	return false
}

// ValidateProjection binds a transport projection to the current original
// Record. It cannot grant paths absent from that record.
func ValidateProjection(record Record, projection map[string]any) error {
	data, err := json.Marshal(projection["assignment_record"])
	if err != nil {
		return err
	}
	var ref RecordReference
	if err := json.Unmarshal(data, &ref); err != nil {
		return err
	}
	sha, err := record.Digest()
	if err != nil {
		return err
	}
	if ref != record.Reference() || projection["assignment_record_sha256"] != sha {
		return fmt.Errorf("projection differs from the current Assignment identity, revision or digest")
	}
	data, err = json.Marshal(projection["allowed_write_paths"])
	if err != nil {
		return err
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return err
	}
	paths, err = normalizeScopeSet(paths)
	if err != nil || !sameStrings(paths, record.EffectiveWritePaths) {
		return fmt.Errorf("projection write paths differ from the canonical Assignment")
	}
	return nil
}

// RecordsFromState reads the original Runtime registry. Missing assignments
// are valid for legacy dispatches; malformed records never become legacy.
func RecordsFromState(state map[string]any) ([]Record, error) {
	entities, _ := state["entities"].(map[string]any)
	raw, exists := entities["assignments"]
	if !exists {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var records []Record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode Runtime Assignment registry: %w", err)
	}
	if records == nil {
		return nil, fmt.Errorf("Runtime Assignment registry must be an array")
	}
	for _, record := range records {
		if err := ValidateRecord(record); err != nil {
			return nil, fmt.Errorf("Runtime Assignment %s: %w", record.AssignmentID, err)
		}
	}
	return records, nil
}

// ForOwner returns the unique current Assignment owned by the registered
// Agent. Identity, TASK ownership and Team projections must still agree.
// Callers must additionally verify frozen source bytes in their file view.
func ForOwner(state map[string]any, owner string) (Record, bool, error) {
	records, err := RecordsFromState(state)
	if err != nil {
		return Record{}, false, err
	}
	if len(records) == 0 {
		return Record{}, false, nil
	}
	runtimeID, generation, _, err := currentRuntimeBinding(state)
	if err != nil {
		return Record{}, false, err
	}
	ids := map[string]bool{}
	for _, record := range records {
		if record.RuntimeID == runtimeID && record.BaselineGeneration == generation && record.OwnerAgentID == owner {
			ids[record.AssignmentID] = true
		}
	}
	if len(ids) > 1 {
		return Record{}, false, fmt.Errorf("Agent %s owns multiple current Assignments", owner)
	}
	for id := range ids {
		record, found, err := FindRecord(records, runtimeID, generation, id)
		if err != nil || !found {
			return Record{}, found, err
		}
		if record.OwnerAgentID != owner {
			return Record{}, false, fmt.Errorf("Agent %s no longer owns Assignment %s", owner, id)
		}
		if err := verifyRuntimeRegistration(state, record.TeamID, owner, record.RoleFamily, record.AgentDefinition.Ref, record.DispatchMode, record.Task.ID, record.Manifest.Ref); err != nil {
			return Record{}, false, err
		}
		task := registeredTaskReference{recordManifestDocument: recordManifestDocument{ID: record.Task.ID, Path: record.Task.Ref, Version: record.Task.Version, SHA256: record.Task.SHA256}}
		if err := verifyRuntimeTaskEntity(state, task, owner); err != nil {
			return Record{}, false, err
		}
		return record, true, nil
	}
	return Record{}, false, nil
}
