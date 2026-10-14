package runtime

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"
)

// Operation binds a caller-stable identity to canonical logical inputs. The
// domain producer computes InputSHA256 before adding generated timestamps;
// actors, authority references and command options belong in those inputs.
type Operation struct {
	ID          string `json:"operation_id"`
	InputSHA256 string `json:"input_sha256"`
	RuntimeID   string `json:"runtime_id"`
	Actor       string `json:"actor"`
	Kind        string `json:"kind"`
}

type OperationArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// OperationReceipt is recorded inside the authoritative journal, never in a
// second ledger. Revision and OccurredAt describe the original commit even
// when the accompanying Snapshot has since advanced.
type OperationReceipt struct {
	Operation
	EventID    string              `json:"event_id"`
	Effect     string              `json:"effect"`
	Message    string              `json:"message"`
	Revision   int                 `json:"committed_revision"`
	OccurredAt string              `json:"occurred_at"`
	Artifacts  []OperationArtifact `json:"artifacts"`
}

const operationAction = "runtime:operation-receipt:v1"

var operationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,179}$`)
var ErrOperationConflict = errors.New("operation ID was already committed with different inputs or authority")

func validateOperation(o Operation) error {
	hash, err := hex.DecodeString(o.InputSHA256)
	if !operationIDPattern.MatchString(o.ID) || err != nil || len(hash) != 32 || o.RuntimeID == "" || o.Actor == "" || o.Kind == "" {
		return errors.New("operation requires a stable ID, canonical input sha256, Runtime, actor and kind")
	}
	return nil
}

func validateMutationOperation(m Mutation) error {
	if err := validateOperation(*m.Operation); err != nil {
		return err
	}
	if m.Operation.RuntimeID != m.RuntimeID || m.Operation.Actor != m.Actor || m.Operation.Kind != m.TransitionID || m.BoundaryReset {
		return errors.New("operation identity differs from its mutation authority")
	}
	return nil
}

func bindOperationReceipt(event map[string]any, m Mutation, revision int, at time.Time) (*OperationReceipt, error) {
	data, _ := json.Marshal(event["action_results"])
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r["id"] == operationAction {
			return nil, errors.New("operation receipt is writer-owned")
		}
	}
	if m.Operation == nil {
		return nil, nil
	}
	receipt := &OperationReceipt{Operation: *m.Operation, EventID: m.EventID, Effect: m.Event, Message: m.Message, Revision: revision, OccurredAt: at.UTC().Format(time.RFC3339Nano), Artifacts: []OperationArtifact{}}
	if m.artifactBatch != nil {
		for _, a := range m.artifactBatch.entries {
			receipt.Artifacts = append(receipt.Artifacts, OperationArtifact{Path: a.Path, SHA256: a.SHA256})
		}
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	rows = append(rows, map[string]any{"id": operationAction, "result": "committed", "detail": string(encoded)})
	event["action_results"] = rows
	return receipt, nil
}

func findOperation(events []map[string]any, operation Operation) (*OperationReceipt, error) {
	if err := validateOperation(operation); err != nil {
		return nil, err
	}
	var found *OperationReceipt
	for _, event := range events {
		data, _ := json.Marshal(event["action_results"])
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row["id"] != operationAction {
				continue
			}
			var receipt OperationReceipt
			if err := json.Unmarshal([]byte(stringValue(row["detail"])), &receipt); err != nil {
				return nil, fmt.Errorf("invalid operation journal receipt: %w", err)
			}
			if receipt.RuntimeID != operation.RuntimeID || receipt.ID != operation.ID {
				continue
			}
			if receipt.Operation != operation {
				return nil, ErrOperationConflict
			}
			actor, _ := event["actor"].(map[string]any)
			revision, err := integerField(event, "after_revision")
			if err != nil || row["result"] != "committed" || event["runtime_id"] != receipt.RuntimeID || event["event_id"] != receipt.EventID || event["transition_id"] != receipt.Kind || actor["id"] != receipt.Actor || receipt.Revision != revision || event["occurred_at"] != receipt.OccurredAt {
				return nil, errors.New("operation receipt disagrees with its journal event")
			}
			if found != nil {
				return nil, errors.New("duplicate operation receipts in journal")
			}
			found = &receipt
		}
	}
	return found, nil
}

func (s *Store) verifyOperationArtifacts(receipt *OperationReceipt) error {
	if len(receipt.Artifacts) == 0 {
		return nil
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, a := range receipt.Artifacts {
		if !artifactOutputPath(a.Path) {
			return fmt.Errorf("invalid operation artifact path %q", a.Path)
		}
		if err := checkTransactionPath(root, a.Path); err != nil {
			return err
		}
		data, err := root.ReadFile(a.Path)
		if err != nil || sha256Hex(data) != a.SHA256 {
			return fmt.Errorf("committed operation artifact %s is missing or drifted; inspect recovery before retrying", a.Path)
		}
	}
	return nil
}

// LookupOperation returns a prior durable receipt with the current snapshot.
// A writer retry completes pending recovery first; a read-only Store reports
// pending without changing anything. Archived Runtime IDs are never conflated.
func (s *Store) LookupOperation(operation Operation) (Snapshot, bool, error) {
	if err := validateOperation(operation); err != nil {
		return Snapshot{}, false, err
	}
	release, err := s.lock()
	if err != nil {
		return Snapshot{}, false, err
	}
	defer release()
	if s.mutationCapable {
		err = s.recoverPendingWritesLocked()
	} else {
		err = s.reportPendingOperationLocked()
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	state, err := s.read()
	if err != nil {
		return Snapshot{}, false, err
	}
	if state["runtime_id"] != operation.RuntimeID {
		return Snapshot{}, false, ErrStaleRuntimeIdentity
	}
	journal, err := s.inspectJournal()
	if err != nil {
		return Snapshot{}, false, err
	}
	if err := validateStateJournalPair(state, journal); err != nil {
		return Snapshot{}, false, err
	}
	receipt, err := findOperation(journal.Events, operation)
	if err != nil {
		return Snapshot{}, false, err
	}
	if receipt == nil {
		return Snapshot{}, false, nil
	}
	if err := s.verifyOperationArtifacts(receipt); err != nil {
		return Snapshot{}, false, err
	}
	revision, err := integerField(state, "revision")
	return Snapshot{Revision: revision, State: state, Operation: receipt, OperationReplayed: true}, true, err
}

type OperationInspection struct {
	OperationID string            `json:"operation_id"`
	Status      string            `json:"status"`
	Receipt     *OperationReceipt `json:"receipt,omitempty"`
}

// InspectOperation does not recover or write state/journal. A pending pair is
// explicitly UNKNOWN until a writer validates and completes its recovery.
func (s *Store) InspectOperation(root, id string) (OperationInspection, error) {
	out := OperationInspection{OperationID: id, Status: "not_found"}
	if !operationIDPattern.MatchString(id) {
		return out, errors.New("invalid operation ID")
	}
	release, err := s.lock()
	if err != nil {
		return out, err
	}
	defer release()
	if err := s.reportPendingOperationLocked(); err != nil {
		if errors.Is(err, ErrPendingRuntimeOperation) {
			out.Status = "recovery_required"
			return out, nil
		}
		return out, err
	}
	state, err := s.read()
	if err != nil {
		return out, err
	}
	journal, err := s.inspectJournal()
	if err != nil {
		return out, err
	}
	if err := validateStateJournalPair(state, journal); err != nil {
		return out, err
	}
	for _, event := range journal.Events {
		data, _ := json.Marshal(event["action_results"])
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			return out, err
		}
		for _, row := range rows {
			if row["id"] != operationAction {
				continue
			}
			var candidate OperationReceipt
			if err := json.Unmarshal([]byte(stringValue(row["detail"])), &candidate); err != nil {
				return out, err
			}
			if candidate.ID != id || candidate.RuntimeID != state["runtime_id"] {
				continue
			}
			receipt, err := findOperation(journal.Events, candidate.Operation)
			if err != nil {
				return out, err
			}
			reader := *s
			reader.root = root
			if err := reader.verifyOperationArtifacts(receipt); err != nil {
				return out, err
			}
			out.Status, out.Receipt = "committed", receipt
			return out, nil
		}
	}
	return out, nil
}
