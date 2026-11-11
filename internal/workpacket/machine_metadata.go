package workpacket

import (
	"encoding/json"
	"fmt"
)

// withoutHistoricalMachineFields makes an in-memory projection of an issued
// object. It never rewrites its source and never visits professional documents
// nested inside it. Historical machine metadata is not a receipt condition.
func withoutHistoricalMachineFields(data []byte, fields ...string) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return data, nil
	}
	changed := false
	for _, field := range fields {
		if _, exists := object[field]; exists {
			delete(object, field)
			changed = true
		}
	}
	if !changed {
		return data, nil
	}
	projected, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("project historical packet metadata: %w", err)
	}
	return projected, nil
}

// WithoutHistoricalMachineFields projects only explicitly named top-level
// framework metadata for receipt readers. It does not rewrite the source or
// traverse the professional answer contained in the object.
func WithoutHistoricalMachineFields(data []byte, fields ...string) ([]byte, error) {
	return withoutHistoricalMachineFields(data, fields...)
}
