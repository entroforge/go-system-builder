package review

import (
	"encoding/json"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Only explicitly identified operations replay. Legacy callers retain their
// duplicate-rejection behavior. The source bytes/options are logical inputs;
// generated envelope timestamps belong to the first durable commit.
func prepareReviewOperation(root, statePath, journalPath, id, kind string, state map[string]any, input any) (*loopruntime.Operation, loopruntime.Snapshot, bool, error) {
	if id == "" {
		return nil, loopruntime.Snapshot{}, false, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, loopruntime.Snapshot{}, false, err
	}
	op := &loopruntime.Operation{ID: id, InputSHA256: sha256Of(data), RuntimeID: stringField(state["runtime_id"]), Actor: "orchestrator", Kind: kind}
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, found, err := store.LookupOperation(*op)
	return op, snapshot, found, err
}
