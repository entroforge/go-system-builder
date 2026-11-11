package review

// evidenceReferenceID resolves an existing evidence reference by ID or path.
func evidenceReferenceID(state map[string]any, reference string) string {
	for _, raw := range evidenceEntries(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		if stringField(entry["id"]) == reference || stringField(entry["path"]) == reference {
			return stringField(entry["id"])
		}
	}
	return ""
}
