package assignmentrecord

// CurrentRuntimeBinding exposes the same strict namespace/commit-sequence
// parsing used by Record construction to original issuance callers.
func CurrentRuntimeBinding(state map[string]any) (string, int, int, error) {
	return currentRuntimeBinding(state)
}

// SupersededOwners identifies canonical historical owners with no current
// charter in this Runtime namespace. It does not stop an Agent or grant a
// successor permission; the original issuance Writer owns those changes.
func SupersededOwners(state map[string]any) (map[string]bool, error) {
	records, err := RecordsFromState(state)
	if err != nil {
		return nil, err
	}
	owners := map[string]bool{}
	if len(records) == 0 {
		return owners, nil
	}
	runtimeID, generation, _, err := currentRuntimeBinding(state)
	if err != nil {
		return nil, err
	}
	groups := map[string][]Record{}
	for _, record := range records {
		owners[record.OwnerAgentID] = true
		if record.RuntimeID == runtimeID && record.BaselineGeneration == generation {
			groups[record.AssignmentID] = append(groups[record.AssignmentID], record)
		}
	}
	for assignment, history := range groups {
		current, found, err := FindRecord(history, runtimeID, generation, assignment)
		if err != nil {
			return nil, err
		}
		if found {
			delete(owners, current.OwnerAgentID)
		}
	}
	return owners, nil
}
