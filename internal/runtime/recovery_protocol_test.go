package runtime_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestApplyRecoveryRefusesNewProtocolBeforeAnyReplacement(t *testing.T) {
	tests := []struct {
		name, suffix, contents string
	}{
		{"state", "", `{"schema_version":"2.0.0"}`},
		{"future state", "", `{"schema_version":"3.0.0"}`},
		{"BOM and truncated state", "", "\xef\xbb\xbf" + `{"schema_version":"2.0.0","broken":`},
		{"duplicate version", "", `{"schema_version":"2.0.0","schema_version":"1.1.0"}`},
		{"active projection without version", "", `{"active_operations":{}}`},
		{"capabilities without version", "", `{"protocol_capabilities":[]}`},
		{"journal invocation with lost state", "journal", `{"schema_version":"1.0.0","action_results":[{"id":"runtime:invocation-start:v2"}]}`},
		{"new journal format", "journal", `{"schema_version":"2.0.0"}`},
		{"pending candidate", ".commit-pending.json", `{"schema_version":"2.1.0","state":{"schema_version":"2.0.0"}}`},
		{"pending refresh", ".fingerprint-pending.json", `{"schema_version":"1.0.0","state":{"schema_version":"2.0.0"}}`},
		{"pending reset", ".rollover-pending.json", `{"schema_version":"1.0.0","fresh_state":{"schema_version":"2.0.0"}}`},
		{"future pending", ".commit-pending.json", `{"schema_version":"3.0.0"}`},
		{"pending recovery candidate", ".recovery-pending.json", `{"schema_version":"1.0.0","candidate_state_base64":"` + base64.StdEncoding.EncodeToString([]byte(`{"schema_version":"2.0.0"}`)) + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath, journalPath := recoveryPaths(t)
			writeRecoveryFile(t, statePath, []byte("damaged legacy state"))
			writeRecoveryFile(t, journalPath, []byte("damaged legacy journal"))
			path := statePath + tc.suffix
			if tc.suffix == "journal" {
				path = journalPath
			}
			writeRecoveryFile(t, path, []byte(tc.contents))
			beforeState, beforeJournal := mustReadRecoveryFile(t, statePath), mustReadRecoveryFile(t, journalPath)
			candidateState, candidateJournal := recoveryCandidates(t)
			_, err := runtime.ApplyRecovery(recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "protocol-plan", "protocol-sha"))
			if !errors.Is(err, runtime.ErrRecoveryProtocolUnsupported) {
				t.Fatalf("ApplyRecovery = %v, want protocol refusal", err)
			}
			if !bytes.Equal(beforeState, mustReadRecoveryFile(t, statePath)) || !bytes.Equal(beforeJournal, mustReadRecoveryFile(t, journalPath)) || string(mustReadRecoveryFile(t, path)) != tc.contents {
				t.Fatal("refusal changed the active pair or pending source")
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/recovery")); !os.IsNotExist(err) {
				t.Fatalf("refusal created a quarantine or success manifest: %v", err)
			}
		})
	}
}

func TestApplyRecoveryPreservesDamagedLegacyCompatibility(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	// A nested version and a literal description are not Runtime declarations.
	source := []byte(`{"schema_version":"1.1.0","description":"active_operations and schema_version 2.0.0","nested":{"schema_version":"2.0.0"},"broken":`)
	writeRecoveryFile(t, statePath, source)
	writeRecoveryFile(t, journalPath, []byte("damaged journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	result, err := runtime.ApplyRecovery(recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "legacy-plan", "legacy-sha"))
	if err != nil || !result.Applied {
		t.Fatalf("legacy recovery = %+v, %v", result, err)
	}
	if !bytes.Equal(mustReadRecoveryFile(t, result.Manifest.SourceState.QuarantinePath), source) {
		t.Fatal("legacy recovery changed the quarantined original")
	}
}

func TestRecoveryResumeRefusesQuarantinedNewProtocolFromOlderWriter(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged legacy state"))
	originalJournal := []byte("damaged legacy journal")
	writeRecoveryFile(t, journalPath, originalJournal)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "older-writer-plan", "older-writer-sha")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryAfterStateReplace {
			return errors.New("interrupt before journal replacement")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatal(err)
	}
	// Model a coherent partial recovery emitted by the fixed predecessor that
	// did not check source versions: current state is already v1, but its exact
	// quarantined source declares v2. Update all source hashes, not just a label.
	markerPath := statePath + ".recovery-pending.json"
	marker := readRecoveryJSONMap(t, markerPath)
	manifest := marker["manifest"].(map[string]any)
	source := manifest["source_state"].(map[string]any)
	original := []byte(`{"schema_version":"2.0.0","active_operations":{}}`)
	writeRecoveryFile(t, source["quarantine_path"].(string), original)
	source["sha256"] = digest(original)
	source["size"] = len(original)
	quarantinePath := filepath.Join(marker["quarantine_dir"].(string), "manifest.json")
	quarantine := readRecoveryJSONMap(t, quarantinePath)
	quarantine["state"] = source
	writeRecoveryJSONMap(t, quarantinePath, quarantine)
	writeRecoveryJSONMap(t, markerPath, marker)
	beforeState, beforeMarker := mustReadRecoveryFile(t, statePath), mustReadRecoveryFile(t, markerPath)
	_, err := runtime.ApplyRecovery(reqWithoutFault(req))
	if !errors.Is(err, runtime.ErrRecoveryProtocolUnsupported) {
		t.Fatalf("resume = %v, want source protocol refusal", err)
	}
	if !bytes.Equal(beforeState, mustReadRecoveryFile(t, statePath)) || !bytes.Equal(beforeMarker, mustReadRecoveryFile(t, markerPath)) || !bytes.Equal(originalJournal, mustReadRecoveryFile(t, journalPath)) {
		t.Fatal("refused resume changed partial recovery bytes")
	}
}
