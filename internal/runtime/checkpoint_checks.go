package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// Checkpoint restore preserves original receipt registration; it does not
// reconstruct acceptance from files or imply that a historical check is fresh.
func validateCheckpointToolReceipts(state map[string]any, journalBytes []byte) error {
	var parsed struct {
		Workspace struct {
			Executions map[string]toolExecution `json:"executions"`
			History    []toolExecution          `json:"execution_history"`
		} `json:"workspace"`
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	executions := parsed.Workspace.History
	for _, e := range parsed.Workspace.Executions {
		executions = append(executions, e)
	}
	var proofs []checkpointReceiptProof
	for _, e := range executions {
		for _, ref := range e.Receipts {
			b := ref.Binding
			if b.RuntimeID != e.RuntimeID || b.AssignmentID != e.AssignmentID || b.Owner != e.Owner || b.Generation != e.Generation || b.ExecutionGeneration != e.ExecutionGeneration || b.Responsibility != "owned_check" {
				return fmt.Errorf("checkpoint tool receipt differs from original execution")
			}
			profileFound := false
			for _, p := range e.Profiles {
				if p.Digest() == ref.ProfileDigest && p.Command == ref.Command {
					profileFound = true
				}
			}
			if !profileFound {
				return fmt.Errorf("checkpoint tool receipt has no original profile")
			}
			proofs = append(proofs, checkpointReceiptProof{
				TransitionID: "WORKSPACE-CHECK", IdempotencyKey: "tool-check:" + ref.SHA256,
				EventID: "tool-check:" + ref.SHA256, RuntimeID: b.RuntimeID,
				BaselineGeneration: b.Generation, ActorID: b.Owner, Responsibility: "owned_check",
				ActorType: actorType(b.Owner),
			})
		}
		seenOwned := map[string]bool{}
		for _, row := range e.OwnedReceipts {
			b := row.Receipt.Binding
			if row.WorkerCheckIndex < 0 || row.WorkerCheckIndex >= len(e.WorkerChecks) {
				return fmt.Errorf("checkpoint owned receipt index is outside the original WorkerChecks")
			}
			check := e.WorkerChecks[row.WorkerCheckIndex]
			if check.Index != row.WorkerCheckIndex || check.Command == "" || check.Command != row.Receipt.Command || check.Profile.Command != check.Command || check.Profile.Location != "worker" || check.Profile.Digest() != row.Receipt.ProfileDigest {
				return fmt.Errorf("checkpoint owned receipt has no exact original WorkerCheck profile")
			}
			if b.RuntimeID != e.RuntimeID || b.AssignmentID != e.AssignmentID || b.Owner != e.Owner || b.Generation != e.Generation || b.ExecutionGeneration != e.ExecutionGeneration || b.Responsibility != "owned_check" || !filepath.IsAbs(row.Receipt.Checkout) || filepath.Clean(row.Receipt.Checkout) != row.Receipt.Checkout || !filepath.IsAbs(row.Receipt.Path) || filepath.Clean(row.Receipt.Path) != row.Receipt.Path || !checkpointValidSHA256(row.Receipt.SHA256) || !checkpointValidSHA256(row.Receipt.LogSHA256) {
				return fmt.Errorf("checkpoint owned receipt differs from original execution")
			}
			if seenOwned[row.Receipt.SHA256] {
				return fmt.Errorf("checkpoint owned receipt duplicates an original captured receipt")
			}
			seenOwned[row.Receipt.SHA256] = true
			key := fmt.Sprintf("owned-check:%d:%s", row.WorkerCheckIndex, row.Receipt.SHA256)
			proofs = append(proofs, checkpointReceiptProof{
				TransitionID: "WORKSPACE-OWNED-CHECK", IdempotencyKey: key, EventID: key,
				RuntimeID: b.RuntimeID, BaselineGeneration: b.Generation,
				ActorID: b.Owner, Responsibility: "owned_check",
				ActorType: actorType(b.Owner),
			})
		}
	}
	nativeProofs, err := checkpointNativeReceiptProofs(state)
	if err != nil {
		return err
	}
	proofs = append(proofs, nativeProofs...)
	aliasProofs, err := checkpointResultAliasProofs(state)
	if err != nil {
		return err
	}
	proofs = append(proofs, aliasProofs...)
	if len(proofs) == 0 {
		return nil
	}
	journal, err := inspectJournalData(journalBytes)
	if err != nil {
		return err
	}
	for _, proof := range proofs {
		if !checkpointReceiptWasReceived(journal.Events, proof) {
			return fmt.Errorf("checkpoint receipt/index has no original %s reception journal", proof.TransitionID)
		}
	}
	return nil
}

type checkpointReceiptProof struct {
	TransitionID       string
	IdempotencyKey     string
	EventID            string
	RuntimeID          string
	BaselineGeneration int
	ActorID            string
	ActorType          string
	Responsibility     string
}

func checkpointReceiptWasReceived(events []map[string]any, proof checkpointReceiptProof) bool {
	for _, event := range events {
		actor, _ := event["actor"].(map[string]any)
		baseline, baselineErr := integerField(event, "baseline_generation")
		if event["event"] == "transition_committed" && event["outcome"] == "committed" && event["transition_id"] == proof.TransitionID && event["idempotency_key"] == proof.IdempotencyKey && event["event_id"] == proof.EventID && event["runtime_id"] == proof.RuntimeID && baselineErr == nil && baseline == proof.BaselineGeneration && actor["id"] == proof.ActorID && actor["type"] == proof.ActorType && event["producer_responsibility"] == proof.Responsibility {
			return true
		}
	}
	return false
}

func checkpointNativeReceiptProofs(state map[string]any) ([]checkpointReceiptProof, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var proofs []checkpointReceiptProof
	for _, raw := range rows {
		agent, _ := raw.(map[string]any)
		owner, _ := agent["id"].(string)
		value, exists := agent["owned_check_receipts"]
		if !exists {
			continue
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("checkpoint native owned-check index cannot be encoded: %w", err)
		}
		var receipts []checkpointNativeOwnedReceipt
		if err := json.Unmarshal(data, &receipts); err != nil || receipts == nil || len(receipts) > 1024 {
			return nil, fmt.Errorf("checkpoint native owned-check index is invalid or exceeds its budget")
		}
		seen := map[string]bool{}
		for _, row := range receipts {
			ref := row.ToolReceipt
			b := ref.Binding
			if row.CheckIndex < 0 || owner == "" || row.RuntimeID == "" || row.BaselineGeneration < 1 || row.AssignmentID == "" || row.OwnerAgentID != owner || row.ExecutionGeneration != 0 || row.Record.RuntimeID != row.RuntimeID || row.Record.BaselineGeneration != row.BaselineGeneration || row.Record.AssignmentID != row.AssignmentID || row.Record.Revision < 1 || !checkpointValidSHA256(row.RecordSHA256) || row.Command == "" || !checkpointValidSHA256(row.ProfileDigest) || row.NativeContextGeneration < 1 || row.NativeContextRef == "" || !checkpointValidSHA256(row.NativeContextSHA256) || row.ActivationRef == "" || !checkpointValidSHA256(row.ActivationSHA256) || row.Checkout == "" || row.SourceBranch == "" || row.TargetBranch == "" || row.SourceHead == "" || !checkpointValidSHA256(row.SourceSnapshotSHA256) {
				return nil, fmt.Errorf("checkpoint native owned-check row is incomplete or differs from its Assignment")
			}
			if b.RuntimeID != row.RuntimeID || b.Generation != row.BaselineGeneration || b.AssignmentID != row.AssignmentID || b.Owner != owner || b.ExecutionGeneration != 0 || b.Responsibility != "owned_check" || ref.Command != row.Command || ref.ProfileDigest != row.ProfileDigest || ref.Checkout != row.Checkout || !filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path || !checkpointValidSHA256(ref.SHA256) || !checkpointValidSHA256(ref.LogSHA256) {
				return nil, fmt.Errorf("checkpoint native owned-check receipt differs from its original binding")
			}
			if seen[ref.SHA256] {
				return nil, fmt.Errorf("checkpoint native owned-check index duplicates an original receipt")
			}
			seen[ref.SHA256] = true
			key := "native-worker-check:" + ref.SHA256
			proofs = append(proofs, checkpointReceiptProof{
				TransitionID: "NATIVE-WORKER-CHECK", IdempotencyKey: key, EventID: key,
				RuntimeID: row.RuntimeID, BaselineGeneration: row.BaselineGeneration,
				ActorID: owner, Responsibility: "owned_check",
				ActorType: actorType(owner),
			})
		}
	}
	return proofs, nil
}

func checkpointResultAliasProofs(state map[string]any) ([]checkpointReceiptProof, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var proofs []checkpointReceiptProof
	for _, raw := range rows {
		agent, _ := raw.(map[string]any)
		value, exists := agent["result_submission_bindings"]
		if !exists {
			continue
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("checkpoint Result submission key index cannot be encoded: %w", err)
		}
		var bindings []checkpointResultSubmissionBinding
		if err := json.Unmarshal(data, &bindings); err != nil || bindings == nil || len(bindings) > 1024 {
			return nil, fmt.Errorf("checkpoint Result submission key index is invalid or exceeds its budget")
		}
		owner, _ := agent["id"].(string)
		seen := map[string]bool{}
		for _, binding := range bindings {
			if owner == "" || binding.Record.RuntimeID == "" || binding.Record.BaselineGeneration < 1 || binding.Record.AssignmentID == "" || binding.Record.Revision < 1 || !checkpointValidSHA256(binding.RecordSHA256) || binding.SubmissionKey == "" || len(binding.SubmissionKey) > 1024 || !checkpointValidSHA256(binding.AnswerDigest) || binding.ToolClosureDigest != "" && !checkpointValidSHA256(binding.ToolClosureDigest) || binding.ResultRef == "" || !checkpointValidSHA256(binding.ResultSHA256) || binding.ResultRevision < 1 {
				return nil, fmt.Errorf("checkpoint Result submission key index contains an invalid binding")
			}
			partition, err := json.Marshal(struct {
				Record checkpointRecordReference
				Key    string
			}{Record: binding.Record, Key: binding.SubmissionKey})
			if err != nil {
				return nil, err
			}
			keyHash := sha256Hex(partition)
			if seen[keyHash] {
				return nil, fmt.Errorf("checkpoint Result submission key index duplicates a binding")
			}
			seen[keyHash] = true
			proofs = append(proofs, checkpointReceiptProof{
				TransitionID:   "BUILDER-RESULT-KEY",
				IdempotencyKey: fmt.Sprintf("runtime:s6-result-key:%s:g%d:a%d:%s", binding.Record.AssignmentID, binding.Record.BaselineGeneration, binding.Record.Revision, keyHash),
				EventID:        "evt-result-alias-" + keyHash,
				RuntimeID:      binding.Record.RuntimeID, BaselineGeneration: binding.Record.BaselineGeneration,
				ActorID: "orchestrator", Responsibility: "orchestrator",
				ActorType: "orchestrator",
			})
		}
	}
	return proofs, nil
}

func checkpointValidSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

type toolExecution struct {
	RuntimeID           string                  `json:"runtime_id"`
	AssignmentID        string                  `json:"assignment_id"`
	Owner               string                  `json:"agent_id"`
	Generation          int                     `json:"baseline_generation"`
	ExecutionGeneration int                     `json:"execution_generation"`
	Path                string                  `json:"worktree_path"`
	Profiles            []checkexec.Profile     `json:"check_profiles"`
	Receipts            []checkexec.ToolReceipt `json:"tool_check_receipts"`
	WorkerChecks        []toolWorkerCheck       `json:"worker_checks"`
	OwnedReceipts       []toolOwnedCheckReceipt `json:"owned_check_receipts"`
}

type toolWorkerCheck struct {
	Index   int               `json:"index"`
	Command string            `json:"command"`
	Profile checkexec.Profile `json:"profile"`
}

type toolOwnedCheckReceipt struct {
	WorkerCheckIndex int                   `json:"worker_check_index"`
	Receipt          checkexec.ToolReceipt `json:"receipt"`
}

type checkpointRecordReference struct {
	RuntimeID          string `json:"runtime_id"`
	BaselineGeneration int    `json:"baseline_generation"`
	AssignmentID       string `json:"assignment_id"`
	Revision           int    `json:"assignment_revision"`
}

type checkpointNativeOwnedReceipt struct {
	CheckIndex              int                       `json:"check_index"`
	Command                 string                    `json:"command"`
	ProfileDigest           string                    `json:"profile_digest"`
	Record                  checkpointRecordReference `json:"record"`
	RecordSHA256            string                    `json:"record_sha256"`
	RuntimeID               string                    `json:"runtime_id"`
	BaselineGeneration      int                       `json:"baseline_generation"`
	AssignmentID            string                    `json:"assignment_id"`
	OwnerAgentID            string                    `json:"owner_agent_id"`
	ExecutionGeneration     int                       `json:"execution_generation"`
	NativeContextGeneration int                       `json:"native_context_generation"`
	NativeContextRef        string                    `json:"native_context_ref"`
	NativeContextSHA256     string                    `json:"native_context_sha256"`
	ActivationRef           string                    `json:"activation_ref"`
	ActivationSHA256        string                    `json:"activation_sha256"`
	Checkout                string                    `json:"checkout"`
	SourceBranch            string                    `json:"source_branch"`
	TargetBranch            string                    `json:"target_branch"`
	SourceHead              string                    `json:"source_head"`
	SourceSnapshotSHA256    string                    `json:"source_snapshot_sha256"`
	ToolReceipt             checkexec.ToolReceipt     `json:"tool_receipt"`
}

type checkpointResultSubmissionBinding struct {
	Record            checkpointRecordReference `json:"record"`
	RecordSHA256      string                    `json:"record_sha256"`
	SubmissionKey     string                    `json:"submission_key"`
	AnswerDigest      string                    `json:"answer_digest"`
	ToolClosureDigest string                    `json:"tool_closure_digest"`
	ResultRef         string                    `json:"result_ref"`
	ResultSHA256      string                    `json:"result_sha256"`
	ResultRevision    int                       `json:"result_revision"`
}
