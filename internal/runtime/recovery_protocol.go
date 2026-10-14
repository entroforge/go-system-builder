package runtime

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrRecoveryProtocolUnsupported distinguishes an unsupported protocol from
// damaged legacy bytes. Reconstruction cannot safely discard newer authority.
var ErrRecoveryProtocolUnsupported = errors.New("recovery source protocol is unsupported")

// CheckLegacyRecoverySources is a read-only preflight for the legacy artifact
// reconstruction algorithm. ApplyRecovery repeats it under the Runtime lock.
// It recognizes format declarations, including those before a truncated JSON
// tail, without requiring damaged legacy inputs to pass their original schema.
// Absence of a readable declaration is not proof of historical compatibility;
// operators must still use the matching release and a complete recovery set.
func CheckLegacyRecoverySources(statePath, journalPath string) error {
	if err := checkRecoveryProtocolFile(statePath, false); err != nil {
		return err
	}
	if err := checkRecoveryProtocolFile(journalPath, true); err != nil {
		return err
	}
	paths := append(knownRecoverySourcePendingPaths(statePath), statePath+".recovery-pending.json")
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := checkLegacyRecoveryPending(data, path == statePath+".commit-pending.json"); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func checkLegacyRecoveryPending(data []byte, commit bool) error {
	return probeRecoveryObject(data, func(key string, value json.RawMessage) error {
		switch key {
		case "schema_version":
			allowed := []string{"1.0.0"}
			if commit {
				allowed = append(allowed, "2.0.0", "2.1.0", "2.2.0")
			}
			return recoveryVersion(value, allowed...)
		case "state", "fresh_state":
			return checkLegacyRecoveryState(value)
		case "candidate_state_base64", "candidate_journal_base64":
			var encoded string
			if json.Unmarshal(value, &encoded) != nil {
				return nil // Existing pending validation owns malformed encodings.
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil
			}
			if key == "candidate_state_base64" {
				return checkLegacyRecoveryState(decoded)
			}
			return checkLegacyRecoveryJournal(decoded)
		}
		return nil
	})
}

func checkRecoveryProtocolFile(path string, journal bool) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if journal {
		err = checkLegacyRecoveryJournal(data)
	} else {
		err = checkLegacyRecoveryState(data)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func checkLegacyRecoveryState(data []byte) error {
	return probeRecoveryObject(data, func(key string, value json.RawMessage) error {
		switch key {
		case "schema_version":
			return recoveryVersion(value, "1.0.0", "1.1.0")
		case "active_operations", "protocol_capabilities":
			return fmt.Errorf("%w: %s requires a matching protocol recovery implementation; preserve the complete recovery set and isolate incompatible writers", ErrRecoveryProtocolUnsupported, key)
		}
		return nil
	})
}

func checkLegacyRecoveryJournal(data []byte) error {
	reader := bufio.NewReader(bytes.NewReader(data))
	for {
		line, readErr := reader.ReadBytes('\n')
		if err := probeRecoveryObject(line, func(key string, value json.RawMessage) error {
			if key == "schema_version" {
				return recoveryVersion(value, "1.0.0")
			}
			if key == "action_results" {
				var actions []struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(value, &actions) == nil {
					for _, action := range actions {
						if action.ID == "runtime:invocation-start:v2" {
							return fmt.Errorf("%w: journal contains durable invocations; legacy reconstruction cannot preserve them", ErrRecoveryProtocolUnsupported)
						}
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func recoveryVersion(raw json.RawMessage, allowed ...string) error {
	if len(raw) == 0 {
		return nil // A truncated value contains no observable version declaration.
	}
	var version string
	if json.Unmarshal(raw, &version) == nil {
		for _, supported := range allowed {
			if version == supported {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: schema_version %q; use a matching recovery implementation, preserve inputs and isolate incompatible writers", ErrRecoveryProtocolUnsupported, version)
}

// Observe each top-level field before advancing. Duplicate keys cannot hide an
// earlier new-protocol declaration; strings mentioning field names are ignored.
func probeRecoveryObject(data []byte, observe func(string, json.RawMessage) error) error {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte{0xef, 0xbb, 0xbf})
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil
		}
		name, ok := key.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		decodeErr := decoder.Decode(&value)
		if err := observe(name, value); err != nil {
			return err
		}
		if decodeErr != nil {
			return nil
		}
	}
	return nil
}
