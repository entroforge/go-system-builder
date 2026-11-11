package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func canonicalRuntimeJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty JSON")
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
