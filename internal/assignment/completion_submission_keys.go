package assignment

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

const resultSubmissionBindingLimit = 1024

// ResultSubmissionBinding records a content-deduplicated key in the original
// Agent row. It never changes the immutable Result or its current pointer.
type ResultSubmissionBinding struct {
	Record            assignmentrecord.RecordReference `json:"record"`
	RecordSHA256      string                           `json:"record_sha256"`
	SubmissionKey     string                           `json:"submission_key"`
	AnswerDigest      string                           `json:"answer_digest"`
	ToolClosureDigest string                           `json:"tool_closure_digest"`
	ResultRef         string                           `json:"result_ref"`
	ResultSHA256      string                           `json:"result_sha256"`
	ResultRevision    int                              `json:"result_revision"`
}

func resultForSubmissionKey(state map[string]any, record assignmentrecord.Record, history []assignmentresult.AcceptedResult, key string, read func(string) ([]byte, error)) (assignmentresult.AcceptedResult, bool, error) {
	agent, found := findCompletionAgent(state, record.OwnerAgentID)
	if !found {
		return assignmentresult.AcceptedResult{}, false, fmt.Errorf("Result submission key owner is not registered")
	}
	bindings, err := readResultSubmissionBindings(agent)
	if err != nil {
		return assignmentresult.AcceptedResult{}, false, err
	}
	recordSHA, err := record.Digest()
	if err != nil {
		return assignmentresult.AcceptedResult{}, false, err
	}
	seen := map[string]bool{}
	byRevision := map[int]assignmentresult.AcceptedResult{}
	var matched assignmentresult.AcceptedResult
	matchedKey := false
	for _, result := range history {
		seen[result.SubmissionKey] = true
		byRevision[result.ResultRevision] = result
		if result.SubmissionKey == key {
			matched, matchedKey = result, true
		}
	}
	closures := map[int]string{}
	receipts := map[int]ResultAcceptanceReceipt{}
	for _, binding := range bindings {
		if binding.Record != record.Reference() {
			continue
		}
		if seen[binding.SubmissionKey] || binding.RecordSHA256 != recordSHA || binding.SubmissionKey == "" || len(binding.SubmissionKey) > 1024 {
			return matched, false, fmt.Errorf("Result submission key index has an invalid or duplicate binding")
		}
		seen[binding.SubmissionKey] = true
		accepted, found := byRevision[binding.ResultRevision]
		if !found || accepted.AnswerDigest != binding.AnswerDigest {
			return matched, false, fmt.Errorf("Result submission key no longer binds its original accepted answer")
		}
		closure, cached := closures[binding.ResultRevision]
		if !cached {
			closure, err = assignmentresult.EffectiveToolClosureDigest(record, accepted)
			if err != nil {
				return matched, false, err
			}
			closures[binding.ResultRevision] = closure
		}
		if closure != binding.ToolClosureDigest {
			return matched, false, fmt.Errorf("Result submission key no longer binds its original tool closure")
		}
		receipt, cached := receipts[binding.ResultRevision]
		if !cached {
			receipt, err = canonicalResultReceipt(state, accepted, read, true)
			if err != nil {
				return matched, false, err
			}
			receipts[binding.ResultRevision] = receipt
		}
		if receipt.ResultRef != binding.ResultRef || receipt.ResultSHA256 != binding.ResultSHA256 {
			return matched, false, fmt.Errorf("Result submission key source binding changed")
		}
		if binding.SubmissionKey == key {
			matched, matchedKey = accepted, true
		}
	}
	return matched, matchedKey, nil
}

func readResultSubmissionBindings(agent map[string]any) ([]ResultSubmissionBinding, error) {
	raw, exists := agent["result_submission_bindings"]
	if !exists {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var bindings []ResultSubmissionBinding
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bindings); err != nil {
		return nil, fmt.Errorf("decode Result submission key index: %w", err)
	}
	if bindings == nil || len(bindings) > resultSubmissionBindingLimit {
		return nil, fmt.Errorf("Result submission key index exceeds its bounded array")
	}
	return bindings, nil
}

func bindResultSubmissionAlias(root string, store *loopruntime.Store, snapshot loopruntime.Snapshot, request CompletionRequest, message canonicalCompletionMessage, original assignmentresult.AcceptedResult, read func(string) ([]byte, error)) (loopruntime.Snapshot, ResultAcceptanceReceipt, error) {
	var none ResultAcceptanceReceipt
	record, found, err := assignmentrecord.ForOwner(snapshot.State, request.AgentID)
	if err != nil || !found {
		return snapshot, none, fmt.Errorf("resolve Result alias Assignment: %v", err)
	}
	if len(message.MessageID) > 1024 {
		return snapshot, none, fmt.Errorf("canonical Result message_id exceeds the key budget")
	}
	receipt, err := canonicalResultReceipt(snapshot.State, original, read, true)
	if err != nil {
		return snapshot, none, err
	}
	closure, err := assignmentresult.EffectiveToolClosureDigest(record, original)
	if err != nil {
		return snapshot, none, err
	}
	recordSHA, _ := record.Digest()
	binding := ResultSubmissionBinding{Record: record.Reference(), RecordSHA256: recordSHA, SubmissionKey: message.MessageID, AnswerDigest: original.AnswerDigest, ToolClosureDigest: closure, ResultRef: receipt.ResultRef, ResultSHA256: receipt.ResultSHA256, ResultRevision: original.ResultRevision}
	partition, _ := json.Marshal(struct {
		Record assignmentrecord.RecordReference
		Key    string
	}{record.Reference(), message.MessageID})
	keyHash := sha256Of(partition)
	when := request.OccurredAt
	if when.IsZero() {
		when = time.Now().UTC()
	}
	currentCursor, _ := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": currentCursor["state"], "phase": currentCursor["phase"]}
	committed, err := store.Update(snapshot.Revision, loopruntime.Mutation{
		EventID:      "evt-result-alias-" + keyHash,
		TransitionID: "BUILDER-RESULT-KEY", Event: "completion_key_bound", Actor: "orchestrator",
		RuntimeID: record.RuntimeID, IdempotencyKey: fmt.Sprintf("runtime:s6-result-key:%s:g%d:a%d:%s", record.AssignmentID, record.BaselineGeneration, record.Revision, keyHash),
		From: cursor, To: cursor, RetainLastTransition: true, EvidenceIDs: []string{original.EvidenceID},
		OccurredAt: when, Message: "Bind a deduplicated Result submission key to its original receipt without changing the Result or current pointer.",
		Apply: func(state map[string]any) error {
			currentSnapshot := loopruntime.Snapshot{Revision: snapshot.Revision, State: state}
			current, err := resolveCanonicalResultBinding(store.Context(), root, currentSnapshot, request.AgentID, message, request.NativePlanPath)
			if err != nil {
				return err
			}
			if current.Record.Reference() != binding.Record || current.RecordSHA != binding.RecordSHA256 {
				return fmt.Errorf("Result alias Assignment changed")
			}
			history, err := assignmentresult.AcceptedHistory(state, current.Record, read)
			if err != nil {
				return err
			}
			prior, exists, err := resultForSubmissionKey(state, current.Record, history, message.MessageID, read)
			if err != nil {
				return err
			}
			if exists {
				if prior.AnswerDigest != binding.AnswerDigest {
					return fmt.Errorf("completion message_id was already accepted with a different AnswerDigest")
				}
				replayed, err := canonicalResultReceipt(state, prior, read, true)
				if err != nil {
					return err
				}
				return &canonicalResultReplay{Snapshot: currentSnapshot, Receipt: replayed}
			}
			if current.Record.ClosingContract != nil && len(current.Record.ClosingContract.WorkerChecks) > 0 {
				facts, artifacts, err := CollectWorkerCheckFacts(store.Context(), root, state, current.Record)
				if err != nil {
					return err
				}
				if err := prepareWorkerCheckArtifactRefs(current.Record, "alias-preflight", facts, artifacts); err != nil {
					return err
				}
				digest, err := assignmentresult.WorkerClosureDigest(current.Record, facts)
				if err != nil {
					return err
				}
				if digest != binding.ToolClosureDigest {
					return fmt.Errorf("Worker closure changed during Result key acceptance; retry the same submission key")
				}
			}
			validOriginal := false
			for _, candidate := range history {
				if candidate.ResultRevision == original.ResultRevision && candidate.EvidenceID == original.EvidenceID && candidate.AnswerDigest == original.AnswerDigest {
					validOriginal = true
				}
			}
			if !validOriginal {
				return fmt.Errorf("Result alias original receipt is no longer registered")
			}
			agent, found := findCompletionAgent(state, request.AgentID)
			if !found {
				return fmt.Errorf("Result alias owner is no longer registered")
			}
			bindings, err := readResultSubmissionBindings(agent)
			if err != nil {
				return err
			}
			if len(bindings) >= resultSubmissionBindingLimit {
				return fmt.Errorf("Result submission key index is full; preserve its existing identities")
			}
			bindings = append(bindings, binding)
			data, err := json.Marshal(bindings)
			if err != nil {
				return err
			}
			var rows []any
			if err := json.Unmarshal(data, &rows); err != nil {
				return err
			}
			agent["result_submission_bindings"] = rows
			return nil
		},
	})
	if err != nil {
		if replay, ok := err.(*canonicalResultReplay); ok {
			return replay.Snapshot, replay.Receipt, nil
		}
		return snapshot, none, err
	}
	receipt, err = canonicalResultReceipt(committed.State, original, read, true)
	return committed, receipt, err
}
