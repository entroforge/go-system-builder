package e2erun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// DecodeReceipt validates a persisted observation's shape and recomputes its
// result from raw events. This never constructs an Observation or authenticates
// caller JSON. A consumer must first verify its pipeline-owned Runtime index,
// immutable hash, current inputs and assignment binding.
func DecodeReceipt(data []byte) (Receipt, error) {
	var receipt Receipt
	if err := schema.NewEmbeddedValidator().ValidateBytes("e2e-runner-receipt.schema.json", data); err != nil {
		return receipt, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return receipt, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return receipt, fmt.Errorf("E2E receipt must contain one JSON object")
	}
	if !receipt.Invoked {
		if len(receipt.Problems) == 0 || receipt.StartedAt != "" || receipt.RawEvents != "" || receipt.ElapsedMS != 0 {
			return receipt, fmt.Errorf("preparation receipt claims an invocation or omits its error")
		}
		return receipt, nil
	}
	selected := map[string]bool{}
	for _, id := range receipt.SelectedTestIDs {
		selected[id] = true
	}
	derived := receipt
	derived.Tests, derived.Attempts = []Test{}, []Attempt{}
	derived.Problems = append([]string{}, receipt.Problems...)
	derived.Complete, derived.Outcome = false, "unknown"
	parseEvents(&derived, selected)
	if derived.Complete != receipt.Complete || derived.Outcome != receipt.Outcome || !reflect.DeepEqual(derived.Tests, receipt.Tests) || !reflect.DeepEqual(derived.Attempts, receipt.Attempts) {
		return receipt, fmt.Errorf("E2E receipt summary differs from its actual reporter events")
	}
	return receipt, nil
}
