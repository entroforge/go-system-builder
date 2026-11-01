package checkexec

import (
	"encoding/json"
)

// SealReceipt serializes the runner's original facts. It does not claim byte
// integrity or ask the caller to provide an additional proof.
func SealReceipt(receipt map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
