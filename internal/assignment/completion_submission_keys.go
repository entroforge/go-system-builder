package assignment

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
)

const resultSubmissionBindingLimit = 1024

// ResultSubmissionBinding records a content-deduplicated key in the original
// Agent row. It never changes the immutable Result or its current pointer.
type ResultSubmissionBinding struct {
	Record         assignmentrecord.RecordReference `json:"record"`
	SubmissionKey  string                           `json:"submission_key"`
	ResultRef      string                           `json:"result_ref"`
	ResultRevision int                              `json:"result_revision"`
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
	byRevision := map[int]assignmentresult.AcceptedResult{}
	var matched assignmentresult.AcceptedResult
	matchedKey := false
	for _, result := range history {
		byRevision[result.ResultRevision] = result
		if result.SubmissionKey == key {
			matched, matchedKey = result, true
		}
	}
	for _, binding := range bindings {
		if binding.Record != record.Reference() || binding.SubmissionKey != key {
			continue
		}
		accepted, found := byRevision[binding.ResultRevision]
		if !found {
			return matched, false, fmt.Errorf("Result submission key has no accepted original result")
		}
		receipt, err := canonicalResultReceipt(state, accepted, read, true)
		if err != nil || receipt.ResultRef != binding.ResultRef {
			return matched, false, fmt.Errorf("Result submission key cannot locate its original receipt")
		}
		if matchedKey && matched.ResultRevision != accepted.ResultRevision {
			return matched, false, fmt.Errorf("Result submission key identifies multiple original results")
		}
		matched, matchedKey = accepted, true
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
