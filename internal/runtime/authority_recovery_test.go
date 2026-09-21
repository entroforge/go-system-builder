package runtime_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// TestPendingCandidateCannotSwitchAuthorityWithCurrentState covers the
// pending-write path, rather than only the current state path. A valid active
// Runtime must not accept a candidate that points its bound workspace at a
// different project root.
func TestPendingCandidateCannotSwitchAuthorityWithCurrentState(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeState(t, statePath, 1)
	current := readJSONMapRuntimeTest(t, statePath)
	bindAuthorityWorkspaceTest(t, current, root)
	writeJSONMapRuntimeTest(t, statePath, current)

	otherRoot := t.TempDir()
	candidate := readJSONMapRuntimeTest(t, statePath)
	bindAuthorityWorkspaceTest(t, candidate, otherRoot)
	marker := map[string]any{
		"schema_version":        "1.0.0",
		"previous_state_sha256": sha256HexForTest(mustRead(t, statePath)),
		"previous_revision":     1,
		"state_sha256":          sha256HexForTest(mustJSON(t, candidate)),
		"state":                 candidate,
	}
	markerPath := statePath + ".fingerprint-pending.json"
	writeJSONMapRuntimeTest(t, markerPath, marker)
	markerBefore := mustRead(t, markerPath)

	_, err := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{}).Snapshot()
	if !errors.Is(err, runtime.ErrAuthorityMismatch) {
		t.Fatalf("pending candidate authority error = %v, want ErrAuthorityMismatch", err)
	}
	if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
		t.Fatal("authority rejection changed the pending marker")
	}
}

// TestPendingCandidateCannotSwitchAuthorityWithoutCurrentState ensures that
// a missing/corrupt current pair cannot turn the pending candidate into a new
// authority. Candidate validation must run before any recovery replacement.
func TestPendingCandidateCannotSwitchAuthorityWithoutCurrentState(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeState(t, statePath, 1)
	candidate := readJSONMapRuntimeTest(t, statePath)
	bindAuthorityWorkspaceTest(t, candidate, t.TempDir())
	stateBytes := mustJSON(t, candidate)
	marker := map[string]any{
		"schema_version":        "1.0.0",
		"previous_state_sha256": "",
		"previous_revision":     0,
		"state_sha256":          sha256HexForTest(stateBytes),
		"state":                 candidate,
	}
	markerPath := statePath + ".fingerprint-pending.json"
	writeJSONMapRuntimeTest(t, markerPath, marker)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}

	_, err := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{}).Snapshot()
	if !errors.Is(err, runtime.ErrAuthorityMismatch) {
		t.Fatalf("pending candidate without current authority error = %v, want ErrAuthorityMismatch", err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("pending marker should remain after rejection: %v", err)
	}
}

// TestOfflineRecoveryWriterRequiresExplicitCapability keeps the staging
// exception explicit and local to the recovery constructor. A zero capability
// does not silently enable path-based bypass, while the issued capability can
// read a bound state from a root-contained staging pair.
func TestOfflineRecoveryWriterRequiresExplicitCapability(t *testing.T) {
	root := t.TempDir()
	activeStatePath := filepath.Join(root, ".claude", "loop-state.json")
	if err := os.MkdirAll(filepath.Dir(activeStatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeState(t, activeStatePath, 1)
	active := readJSONMapRuntimeTest(t, activeStatePath)
	bindAuthorityWorkspaceTest(t, active, root)
	writeJSONMapRuntimeTest(t, activeStatePath, active)

	stagingDir := filepath.Join(root, ".claude", "recovery", "candidate")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stagingDir, "candidate-state.json")
	journalPath := filepath.Join(stagingDir, "candidate-events.jsonl")
	writeJSONMapRuntimeTest(t, statePath, active)
	if err := os.WriteFile(journalPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	withoutCapability := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{})
	if _, err := withoutCapability.Snapshot(); !errors.Is(err, runtime.ErrAuthorityMismatch) {
		t.Fatalf("ordinary staging writer error = %v, want ErrAuthorityMismatch", err)
	}

	withCapability := runtime.NewOfflineRecoveryWriter(statePath, journalPath, root, integrityTestValidator{}, runtime.NewOfflineRecoveryCapability())
	if _, err := withCapability.Snapshot(); err != nil {
		t.Fatalf("explicit recovery capability should allow root-contained staging pair: %v", err)
	}
	alias := filepath.Join(stagingDir, "state-alias.json")
	if err := os.Symlink(statePath, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, state, journal string }{
		{"active-state", activeStatePath, journalPath},
		{"active-journal", statePath, filepath.Join(root, ".claude", "loop-events.jsonl")},
		{"swapped-active-file", statePath, activeStatePath},
		{"same-file", statePath, statePath},
		{"state-alias", alias, journalPath},
		{"outside-root", statePath, filepath.Join(t.TempDir(), "journal.jsonl")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := runtime.NewOfflineRecoveryWriter(tc.state, tc.journal, root, integrityTestValidator{}, runtime.NewOfflineRecoveryCapability())
			if _, err := writer.Snapshot(); !errors.Is(err, runtime.ErrAuthorityMismatch) {
				t.Fatalf("unsafe offline pair error = %v, want ErrAuthorityMismatch", err)
			}
		})
	}
}

func bindAuthorityWorkspaceTest(t *testing.T, state map[string]any, root string) {
	t.Helper()
	bound, ok := state["bound_req"].(map[string]any)
	if !ok {
		t.Fatalf("fixture bound_req = %#v", state["bound_req"])
	}
	bound["workspace"] = map[string]any{
		"project_root":     root,
		"dev_branch":       "test-development",
		"release_upstream": "origin/release",
		"bound_commit":     "fixture",
	}
}

func TestPendingCandidateRejectsRetiredRequirementPath(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}
	writeState(t, statePath, 1)
	candidate := readJSONMapRuntimeTest(t, statePath)
	candidate["bound_req"] = map[string]any{"path": "docs/product/requirements/REQ-001.md"}
	stateBytes := mustJSON(t, candidate)
	markerPath := statePath + ".fingerprint-pending.json"
	writeJSONMapRuntimeTest(t, markerPath, map[string]any{
		"schema_version": "1.0.0", "previous_state_sha256": "", "previous_revision": 0,
		"state_sha256": sha256HexForTest(stateBytes), "state": candidate,
	})
	before := mustRead(t, markerPath)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(err.Error(), "layout migration required") {
		t.Fatalf("old pending candidate accepted: %v", err)
	}
	if string(mustRead(t, markerPath)) != string(before) {
		t.Fatal("rejected marker changed")
	}
	for _, p := range []string{statePath, journalPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("rejected candidate published %s: %v", p, err)
		}
	}
}
