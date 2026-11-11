package runtime_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The original Runtime Writer must not turn an unknown pre-existing
// checkpoint directory into a commit gate, or claim it by equal bytes.
func TestRuntimeWriterPreservesUnknownCheckpointAndContinues(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeState(t, statePath, 1)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	req := state["bound_req"].(map[string]any)
	p, err := workpacket.BuildDesign(workpacket.PlanningBinding{RuntimeID: "loop-test", Generation: 1, StageEntry: 1, Stage: "S2", Owner: "Main", Requirement: workpacket.Reference{ID: req["id"].(string), Path: req["path"].(string), Version: req["version"].(string)}})
	if err != nil {
		t.Fatal(err)
	}
	packetJSON, _ := json.Marshal(p)
	var row map[string]any
	if err := json.Unmarshal(packetJSON, &row); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["work_packets"] = []any{row}
	data, _ = json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte("loop-test"))
	unknownDir := filepath.Join(root, ".claude", "runtime-checkpoints", "cp", encoded, "r2-s1")
	if err := os.MkdirAll(unknownDir, 0700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(unknownDir, "private.txt")
	if err := os.WriteFile(unknown, []byte("do not claim"), 0600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(statePath, journalPath)
	result, err := writer.Update(1, runtime.Mutation{EventID: "evt-checkpoint-collision", TransitionID: "TR-TEST", Event: "test_transition", Actor: "orchestrator", IdempotencyKey: "checkpoint:collision", Apply: func(candidate map[string]any) error {
		candidate["milestone"].(map[string]any)["instruction"] = "preserve unknown checkpoint"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 2 {
		t.Fatalf("revision = %d", result.Revision)
	}
	if got, err := os.ReadFile(unknown); err != nil || string(got) != "do not claim" {
		t.Fatalf("unknown changed: %q %v", got, err)
	}
	latest, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil || !strings.Contains(latest, "/alternate-1/") {
		t.Fatalf("indexed checkpoint = %q, %v", latest, err)
	}
	if _, err := runtime.ReadRuntimeCheckpoint(root, latest, testCandidateValidator()); err != nil {
		t.Fatalf("read actual indexed checkpoint: %v", err)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(latest))
	originalManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		present bool
		value   any
	}{
		{name: "absent"},
		{name: "null", present: true, value: nil},
		{name: "nonstandard", present: true, value: "historical-not-a-digest"},
		{name: "number", present: true, value: 123},
		{name: "object", present: true, value: map[string]any{"legacy": true}},
	} {
		t.Run("legacy_sha_"+tc.name, func(t *testing.T) {
			var manifest map[string]any
			if err := json.Unmarshal(originalManifest, &manifest); err != nil {
				t.Fatal(err)
			}
			if tc.present {
				manifest["state_sha256"], manifest["journal_sha256"] = tc.value, tc.value
			}
			input, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, input, 0600); err != nil {
				t.Fatal(err)
			}
			pair, err := runtime.ReadRuntimeCheckpoint(root, latest, testCandidateValidator())
			if err != nil || pair.Manifest.RuntimeID != "loop-test" || pair.Manifest.Revision != 2 {
				t.Fatalf("read historical manifest: %#v %v", pair.Manifest, err)
			}
			if got, err := os.ReadFile(manifestPath); err != nil || string(got) != string(input) {
				t.Fatalf("original manifest was modified: %q %v", got, err)
			}
		})
	}
	var forged map[string]any
	if err := json.Unmarshal(originalManifest, &forged); err != nil {
		t.Fatal(err)
	}
	forged["runtime_id"] = "another-owner"
	forged["state_sha256"] = map[string]any{"legacy": true}
	input, _ := json.Marshal(forged)
	if err := os.WriteFile(manifestPath, input, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ReadRuntimeCheckpoint(root, latest, testCandidateValidator()); err == nil {
		t.Fatal("different Runtime identity was accepted")
	}
	if err := json.Unmarshal(originalManifest, &forged); err != nil {
		t.Fatal(err)
	}
	forged["unknown_professional_field"] = map[string]any{"sha256": "professional text remains untouched"}
	input, _ = json.Marshal(forged)
	if err := os.WriteFile(manifestPath, input, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ReadRuntimeCheckpoint(root, latest, testCandidateValidator()); err != nil {
		t.Fatalf("unrelated manifest field rejected: %v", err)
	}
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != string(input) {
		t.Fatalf("original manifest was modified: %q %v", got, err)
	}
}
