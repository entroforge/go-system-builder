package runtime

import "fmt"

// validateCheckpointPublicationBudget checks the exact prospective pair before
// creating a pending commit. Publication uses these same JSON serializers.
func (s *Store) validateCheckpointPublicationBudget(state map[string]any, event map[string]any, journal journalInspection) error {
	if s.root == "" || s.offlineRecovery || !hasIssuedIndices(state) {
		return nil
	}
	stateBytes, err := jsonDocumentBytes(state)
	if err != nil {
		return err
	}
	if len(stateBytes) > checkpointStateLimit {
		return fmt.Errorf("prospective issued-index checkpoint state exceeds 16 MiB; Runtime mutation was not accepted")
	}
	eventBytes, err := jsonLineBytes(event)
	if err != nil {
		return err
	}
	return s.validateCheckpointPublicationBytesBudget(state, stateBytes, eventBytes, journal)
}

// validateCheckpointPublicationBytesBudget reuses the exact final documents;
// history is still counted one complete event at a time without a second buffer.
func (s *Store) validateCheckpointPublicationBytesBudget(state map[string]any, stateBytes, eventBytes []byte, journal journalInspection) error {
	if s.root == "" || s.offlineRecovery || !hasIssuedIndices(state) {
		return nil
	}
	if len(stateBytes) > checkpointStateLimit {
		return fmt.Errorf("prospective issued-index checkpoint state exceeds 16 MiB; Runtime mutation was not accepted")
	}
	total := len(eventBytes)
	if total > checkpointJournalLimit {
		return fmt.Errorf("prospective issued-index complete journal exceeds 64 MiB; Runtime mutation was not accepted")
	}
	for _, prior := range journal.Events {
		data, err := jsonLineBytes(prior)
		if err != nil {
			return err
		}
		if len(data) > checkpointJournalLimit-total {
			return fmt.Errorf("prospective issued-index complete journal exceeds 64 MiB; Runtime mutation was not accepted")
		}
		total += len(data)
	}
	return nil
}
