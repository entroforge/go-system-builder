package runtime_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// minimalPolicy mirrors the shape of docs/hook-policy.json after the REQ-039
// reduction: an artifact `version` ("v2.0.0") alongside the wire-format
// `schema_version` ("1.2.0"). The two must never be confused — that confusion
// is BUG-039-12.
const minimalPolicy = `{"version":"v2.0.0","schema_version":"1.2.0","policy_id":"hook-policy"}`

func writePolicyRefState(t *testing.T, dir, recordedVersion, recordedSHA string) (statePath, journalPath, policyPath string) {
	t.Helper()
	policyPath = filepath.Join(dir, "docs", "hook-policy.json")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte(minimalPolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   32,
		"hook_control": map[string]any{
			"mode":   "enforce",
			"health": "healthy",
			"policy_ref": map[string]any{
				"path":    "docs/hook-policy.json",
				"version": recordedVersion,
				"sha256":  recordedSHA,
			},
		},
	}
	data, err := json.Marshal(state)
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

func policyDigest() string {
	sum := sha256.Sum256([]byte(minimalPolicy))
	return fmt.Sprintf("%x", sum)
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

// TestStoreRefreshFingerprintsTracksPolicyVersionNotSchemaVersion is the core
// BUG-039-12 regression: the recorded policy version must become the policy
// document's `version` ("v2.0.0"), never its `schema_version` ("1.2.0").
func TestStoreRefreshFingerprintsTracksPolicyVersionNotSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	statePath, journalPath, _ := writePolicyRefState(t, dir, "1.2.0", "stale-digest")

	if _, err := runtime.NewStore(statePath, journalPath).RefreshFingerprints(dir); err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}

	policyRef := readPolicyRef(t, statePath)
	if policyRef["version"] != "v2.0.0" {
		t.Fatalf("policy_ref version must track the policy `version` field: got %v want v2.0.0", policyRef["version"])
	}
	if policyRef["sha256"] != policyDigest() {
		t.Fatalf("policy_ref sha256 was not refreshed: got %v want %s", policyRef["sha256"], policyDigest())
	}
}

// TestStoreRefreshFingerprintsFallsBackToSchemaVersion pins the fallback that
// keeps docs/loop-definition.json working: it declares only `schema_version`,
// so that value remains authoritative when no `version` field exists.
func TestStoreRefreshFingerprintsFallsBackToSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	definition := []byte(`{"schema_version":"1.3.0","definition_id":"test"}`)
	definitionPath := filepath.Join(dir, "loop-definition.json")
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   3,
		"definition": map[string]any{
			"path": "loop-definition.json", "version": "1.2.0", "sha256": "stale",
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "loop-state.json")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	store := runtime.NewStore(statePath, filepath.Join(dir, "loop-events.jsonl"))
	if _, err := store.RefreshFingerprints(dir); err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}

	updated, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if err := json.Unmarshal(updated, &after); err != nil {
		t.Fatal(err)
	}
	definitionState := after["definition"].(map[string]any)
	if definitionState["version"] != "1.3.0" {
		t.Fatalf("definition version must fall back to schema_version: got %v", definitionState["version"])
	}
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

// TestStoreInspectPolicyRefDetectsVersionAndSHADrift covers the doctor detector
// input: both the version and the digest diverge from the on-disk policy.
func TestStoreInspectPolicyRefDetectsVersionAndSHADrift(t *testing.T) {
	dir := t.TempDir()
	statePath, journalPath, _ := writePolicyRefState(t, dir, "1.2.0", "stale-digest")

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
	if !drift.SHADrifted() {
		t.Fatalf("expected sha drift, got recorded=%s on-disk=%s", drift.RecordedSHA256, drift.OnDiskSHA256)
	}
	if drift.OnDiskVersion != "v2.0.0" {
		t.Fatalf("on-disk version must be the policy `version`: got %s", drift.OnDiskVersion)
	}
	if drift.Missing || drift.FileMissing {
		t.Fatalf("policy file exists and is recorded; got %+v", drift)
	}
}

// TestStoreInspectPolicyRefCleanAfterRefresh proves the reconcile path converges:
// RefreshFingerprints is sufficient to clear the drift the detector reports.
func TestStoreInspectPolicyRefCleanAfterRefresh(t *testing.T) {
	dir := t.TempDir()
	statePath, journalPath, _ := writePolicyRefState(t, dir, "1.2.0", "stale-digest")
	store := runtime.NewStore(statePath, journalPath)

	if _, err := store.RefreshFingerprints(dir); err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}
	drift, err := store.InspectPolicyRef(dir)
	if err != nil {
		t.Fatalf("InspectPolicyRef: %v", err)
	}
	if drift.Drifted() {
		t.Fatalf("policy_ref should be consistent after refresh, got %+v", drift)
	}
	if drift.RecordedVersion != "v2.0.0" {
		t.Fatalf("recorded version after refresh: got %s want v2.0.0", drift.RecordedVersion)
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
	statePath, journalPath, policyPath := writePolicyRefState(t, dir, "v2.0.0", policyDigest())
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
