package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// minimalPolicy mirrors the shape of docs/control/hook-policy.json after the REQ-039
// reduction: an artifact `version` ("v2.0.0") alongside the wire-format
// `schema_version` ("1.2.0"). The two must never be confused — that confusion
// is BUG-039-12.
const minimalPolicy = `{"version":"v2.0.0","schema_version":"1.2.0","policy_id":"hook-policy"}`

func writePolicyRefState(t *testing.T, dir, recordedVersion string) (statePath, journalPath, policyPath string) {
	t.Helper()
	policyPath = filepath.Join(dir, "docs", "control", "hook-policy.json")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte(minimalPolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-test"
	state["revision"] = 32
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	hookControl, ok := state["hook_control"].(map[string]any)
	if !ok {
		t.Fatal("loop-state example must contain hook_control")
	}
	hookControl["policy_ref"] = map[string]any{
		"path":    "docs/control/hook-policy.json",
		"version": recordedVersion,
	}
	state["hook_control"] = hookControl
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath = filepath.Join(dir, "loop-state.json")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath = filepath.Join(dir, "loop-events.jsonl")
	return statePath, journalPath, policyPath
}

func readPolicyRef(t *testing.T, statePath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	hookControl, ok := state["hook_control"].(map[string]any)
	if !ok {
		t.Fatalf("state has no hook_control object: %v", state)
	}
	policyRef, ok := hookControl["policy_ref"].(map[string]any)
	if !ok {
		t.Fatalf("state has no hook_control.policy_ref object: %v", hookControl)
	}
	return policyRef
}

func TestDocumentMetadataVersionPrefersVersion(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"version wins over schema_version", minimalPolicy, "v2.0.0"},
		{"schema_version is the fallback", `{"schema_version":"1.3.0"}`, "1.3.0"},
		{"no version fields", `{"policy_id":"x"}`, ""},
		{"not json", `not json`, ""},
		{"empty version is not preferred", `{"version":"","schema_version":"1.1.0"}`, "1.1.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtime.DocumentMetadataVersion([]byte(tc.doc)); got != tc.want {
				t.Fatalf("DocumentMetadataVersion(%s) = %q want %q", tc.doc, got, tc.want)
			}
		})
	}
}

// TestStoreInspectPolicyRefDetectsVersionDrift covers the actual policy version.
func TestStoreInspectPolicyRefDetectsVersionDrift(t *testing.T) {
	dir := t.TempDir()
	statePath, journalPath, _ := writePolicyRefState(t, dir, "1.2.0")

	drift, err := runtime.NewStore(statePath, journalPath).InspectPolicyRef(dir)
	if err != nil {
		t.Fatalf("InspectPolicyRef: %v", err)
	}
	if !drift.Drifted() {
		t.Fatalf("expected drift, got %+v", drift)
	}
	if !drift.VersionDrifted() {
		t.Fatalf("expected version drift, got recorded=%s on-disk=%s", drift.RecordedVersion, drift.OnDiskVersion)
	}
	if drift.OnDiskVersion != "v2.0.0" {
		t.Fatalf("on-disk version must be the policy `version`: got %s", drift.OnDiskVersion)
	}
	if drift.Missing || drift.FileMissing {
		t.Fatalf("policy file exists and is recorded; got %+v", drift)
	}
}

func TestStoreInspectPolicyRefReportsMissingReference(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	if err := os.WriteFile(statePath, []byte(`{"runtime_id":"loop-test","revision":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := runtime.NewStore(statePath, filepath.Join(dir, "loop-events.jsonl")).InspectPolicyRef(dir)
	if err != nil {
		t.Fatalf("InspectPolicyRef: %v", err)
	}
	if !drift.Missing || !drift.Drifted() {
		t.Fatalf("expected missing policy_ref to be reported as drift, got %+v", drift)
	}
}

func TestStoreInspectPolicyRefReportsMissingPolicyFile(t *testing.T) {
	dir := t.TempDir()
	statePath, journalPath, policyPath := writePolicyRefState(t, dir, "v2.0.0")
	if err := os.Remove(policyPath); err != nil {
		t.Fatal(err)
	}

	drift, err := runtime.NewStore(statePath, journalPath).InspectPolicyRef(dir)
	if err != nil {
		t.Fatalf("InspectPolicyRef: %v", err)
	}
	if !drift.FileMissing || !drift.Drifted() {
		t.Fatalf("expected missing policy file to be reported as drift, got %+v", drift)
	}
}
