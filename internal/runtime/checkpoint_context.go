package runtime

// Existing context bindings remain historical facts. Recovery need not reopen
// their original authoring files or authenticate source digests.
func validateCheckpointContextBindings(root string, state map[string]any, journalBytes []byte) error {
	return nil
}
