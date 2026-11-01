package runtime

import "context"

// ValidateCheckpointIndices verifies the saved Runtime and journal form a
// coherent pair. Already received responsibilities remain original facts;
// recovery does not authenticate their sources or recompute their decisions.
func ValidateCheckpointIndices(root string, state map[string]any, journalBytes []byte) error {
	return ValidateCheckpointIndicesContext(context.Background(), root, state, journalBytes)
}

func ValidateCheckpointIndicesContext(ctx context.Context, root string, state map[string]any, journalBytes []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return validateRecoveryPair(state, journalBytes)
}
