// Package jsoncompat supports narrowly scoped, in-memory compatibility reads.
package jsoncompat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// MaskMachineFields replaces the value of each explicitly named historical
// machine field with null. Each path starts at the first JSON value; "*"
// matches one object member or one array element. No unspecified member is
// traversed. Keys, including duplicate keys, and all other bytes are kept.
// Trailing input is preserved for the caller's original decoder to handle.
//
// Use this only for decoding known machine fields into a zero-valued DTO. It
// preserves the field itself, so it must not be used to change map presence
// semantics. The input slice and the corresponding source file are untouched.
func MaskMachineFields(data []byte, paths ...[]string) ([]byte, error) {
	if len(paths) == 0 {
		return data, nil
	}
	for _, path := range paths {
		if len(path) == 0 {
			return nil, errors.New("machine metadata path must not be empty")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	base := int(decoder.InputOffset()) - len(value)
	var spans []span
	if err := collect(value, base, paths, &spans); err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return data, nil
	}
	var out bytes.Buffer
	last := 0
	for _, part := range spans {
		if part.start < last || part.end < part.start || part.end > len(data) {
			return nil, errors.New("invalid machine metadata projection span")
		}
		out.Write(data[last:part.start])
		out.WriteString("null")
		last = part.end
	}
	out.Write(data[last:])
	return out.Bytes(), nil
}

type span struct{ start, end int }

func collect(data []byte, base int, paths [][]string, spans *[]span) error {
	if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	delim, err := decoder.Token()
	if err != nil {
		return err
	}
	object := delim == json.Delim('{')
	for decoder.More() {
		var key string
		if object {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			var ok bool
			key, ok = token.(string)
			if !ok {
				return errors.New("invalid machine metadata object key")
			}
		}
		var child json.RawMessage
		if err := decoder.Decode(&child); err != nil {
			return err
		}
		end := base + int(decoder.InputOffset())
		start := end - len(child)
		var next [][]string
		match := false
		for _, path := range paths {
			if (!object && path[0] != "*") || (object && path[0] != "*" && path[0] != key) {
				continue
			}
			if len(path) == 1 {
				match = true
			} else {
				next = append(next, path[1:])
			}
		}
		if match {
			*spans = append(*spans, span{start, end})
		} else if len(next) > 0 {
			if err := collect(child, start, next, spans); err != nil {
				return fmt.Errorf("project machine metadata: %w", err)
			}
		}
	}
	_, err = decoder.Token()
	return err
}
