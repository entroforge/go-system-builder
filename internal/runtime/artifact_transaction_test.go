package runtime_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func artifactMutation(paths ...string) runtime.Mutation {
	artifacts := []runtime.ImmutableArtifact{}
	for _, p := range paths {
		artifacts = append(artifacts, runtime.ImmutableArtifact{Path: p, Data: []byte("immutable " + p + "\n")})
	}
	return runtime.Mutation{
		Artifacts: artifacts, EventID: "evt-artifacts", TransitionID: "ARTIFACT-TEST", Event: "test_artifacts",
		Actor: "orchestrator", RuntimeID: "loop-test", IdempotencyKey: "artifact-test:1",
		Apply: func(state map[string]any) error {
			docs, _ := state["documents"].([]any)
			for i, a := range artifacts {
				docs = append(docs, map[string]any{"id": fmt.Sprintf("artifact-%d", i), "kind": "review", "path": a.Path, "sha256": sha256HexForTest(a.Data), "version": "v1", "status": "valid", "generation": 1})
			}
			state["documents"] = docs
			return nil
		},
	}
}

func TestArtifactBundleRejectsWithoutPublishingOrDeletingHistory(t *testing.T) {
	for _, variant := range []string{"stale", "apply_reject", "unreferenced", "canonical_collision", "symlink", "escape", "duplicate"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
			writeState(t, sp, 1)
			store := testWriter(sp, jp)
			m := artifactMutation(".claude/evidence/a.json", ".claude/evidence/b.json")
			expected := 1
			switch variant {
			case "stale":
				expected = 0
			case "apply_reject":
				m.Apply = func(map[string]any) error { return errors.New("domain rejected") }
			case "unreferenced":
				m.Apply = nil
			case "canonical_collision":
				if err := os.MkdirAll(filepath.Join(root, ".claude/evidence"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".claude/evidence/a.json"), []byte("historical bytes"), 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(root, ".claude/evidence")); err != nil {
					t.Fatal(err)
				}
			case "escape":
				m.Artifacts[0].Path = ".claude/evidence/../../product.txt"
			case "duplicate":
				m.Artifacts = append(m.Artifacts, m.Artifacts[0])
			}
			before := mustRead(t, sp)
			if _, err := store.Update(expected, m); err == nil {
				t.Fatal("invalid bundle accepted")
			}
			if !bytes.Equal(before, mustRead(t, sp)) {
				t.Fatal("rejected proposal changed state")
			}
			if data, err := os.ReadFile(jp); err == nil && len(data) > 0 {
				t.Fatal("rejected proposal appended journal")
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/evidence/b.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published second artifact on rejection: %v", err)
			}
			if variant == "canonical_collision" && string(mustRead(t, filepath.Join(root, ".claude/evidence/a.json"))) != "historical bytes" {
				t.Fatal("removed or replaced canonical history")
			}
			staging, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
			if len(staging) != 0 {
				t.Fatalf("definitively rejected operation retained staging: %v", staging)
			}
		})
	}
}

// Recreate each durable boundary using the state/event produced by a real
// Store.Update, then recover through the public writer. This tests crash-state
// replay deterministically, without timing a kill between two filesystem calls.
func artifactRecoveryFixture(t *testing.T, boundary string, repairBundle ...bool) (string, string, string, map[string]any, []runtime.ImmutableArtifact) {
	t.Helper()
	root := t.TempDir()
	sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, sp, 1)
	before := mustRead(t, sp)
	m := artifactMutation(".claude/evidence/a.json", ".claude/review/plans/b.json")
	version := "2.0.0"
	if len(repairBundle) > 0 && repairBundle[0] {
		m = artifactMutation(".claude/review/repair/sessions/a.json", ".claude/review/repair/plans/b.json")
		version = "2.1.0"
	}
	store := testWriter(sp, jp)
	if _, err := store.Update(1, m); err != nil {
		t.Fatal(err)
	}
	var previous, desired, event map[string]any
	if err := json.Unmarshal(before, &previous); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mustRead(t, sp), &desired); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bytes.TrimSpace(mustRead(t, jp)), &event); err != nil {
		t.Fatal(err)
	}
	entries := []map[string]any{}
	for i, a := range m.Artifacts {
		staged := fmt.Sprintf(".claude/operations/staging/%s/%d.data", strings.Repeat("a", 32), i)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, staged)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, staged), a.Data, 0644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, map[string]any{"path": a.Path, "staging_path": staged, "sha256": sha256HexForTest(a.Data)})
		if boundary == "marker" || (boundary == "partial_publish" && i == 1) {
			if err := os.Remove(filepath.Join(root, a.Path)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Struct field order is part of the producer's canonical manifest encoding.
	type manifestEntry struct {
		Path        string `json:"path"`
		StagingPath string `json:"staging_path"`
		SHA256      string `json:"sha256"`
	}
	canonical := []manifestEntry{}
	for _, e := range entries {
		canonical = append(canonical, manifestEntry{e["path"].(string), e["staging_path"].(string), e["sha256"].(string)})
	}
	manifestBytes, _ := json.Marshal(canonical)
	for _, raw := range event["action_results"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == "runtime:immutable-artifacts:v2" {
			row["detail"] = sha256HexForTest(manifestBytes)
		}
	}
	eventBytes, _ := json.Marshal(event)
	eventBytes = append(eventBytes, '\n')
	desiredBytes := mustJSON(t, desired)
	previousBytes := mustJSON(t, previous)
	pending := map[string]any{
		"schema_version": version, "previous_state_sha256": sha256HexForTest(previousBytes), "previous_revision": 1,
		"state_sha256": sha256HexForTest(desiredBytes), "journal_event_sha256": sha256HexForTest(eventBytes),
		"request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": false,
		"state": desired, "journal_event": event, "artifacts": entries,
	}
	if boundary != "state" && boundary != "journal" {
		if err := os.WriteFile(sp, before, 0644); err != nil {
			t.Fatal(err)
		}
	}
	journal := []byte{}
	if boundary == "journal" {
		journal = eventBytes
	}
	if err := os.WriteFile(jp, journal, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sp+".commit-pending.json", mustJSON(t, pending), 0644); err != nil {
		t.Fatal(err)
	}
	return root, sp, jp, pending, m.Artifacts
}

func TestArtifactBundleRecoveryAtEveryDurableBoundary(t *testing.T) {
	for _, repairBundle := range []bool{false, true} {
		t.Run(fmt.Sprintf("repair_%v", repairBundle), func(t *testing.T) {
			for _, boundary := range []string{"marker", "partial_publish", "published", "state", "journal"} {
				t.Run(boundary, func(t *testing.T) {
					root, sp, jp, _, artifacts := artifactRecoveryFixture(t, boundary, repairBundle)
					before := mustRead(t, sp)
					if _, err := runtime.NewStore(sp, jp).Snapshot(); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
						t.Fatalf("read-only inspection: %v", err)
					}
					if !bytes.Equal(before, mustRead(t, sp)) {
						t.Fatal("reader performed recovery")
					}
					store := testWriter(sp, jp)
					if recovered, err := store.RecoverPendingOperations(); err != nil || !recovered {
						t.Fatalf("recovery: %v, %v", recovered, err)
					}
					for _, a := range artifacts {
						if !bytes.Equal(a.Data, mustRead(t, filepath.Join(root, a.Path))) {
							t.Fatal("artifact bytes changed")
						}
					}
					snap, err := runtime.NewStore(sp, jp).Snapshot()
					if err != nil || snap.Revision != 2 {
						t.Fatalf("recovered snapshot: %+v %v", snap, err)
					}
					journal := mustRead(t, jp)
					if bytes.Count(journal, []byte("\n")) != 1 {
						t.Fatal("commit duplicated or missing")
					}
					if recovered, err := store.RecoverPendingOperations(); err != nil || recovered {
						t.Fatalf("repeat recovery: %v %v", recovered, err)
					}
					if !bytes.Equal(journal, mustRead(t, jp)) {
						t.Fatal("repeat recovery changed journal")
					}
					staging, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
					if len(staging) != 0 {
						t.Fatal("committed staging not cleaned")
					}
				})
			}
		})
	}
}

func TestArtifactBundleBadRecoveryPreservesAllInputs(t *testing.T) {
	for _, repairBundle := range []bool{false, true} {
		t.Run(fmt.Sprintf("repair_%v", repairBundle), func(t *testing.T) {
			for _, variant := range []string{"staging_missing", "staging_changed", "canonical_changed", "manifest_changed", "downgrade", "unknown_state"} {
				t.Run(variant, func(t *testing.T) {
					root, sp, jp, pending, artifacts := artifactRecoveryFixture(t, "partial_publish", repairBundle)
					entries := pending["artifacts"].([]map[string]any)
					switch variant {
					case "staging_missing":
						if err := os.Remove(filepath.Join(root, entries[1]["staging_path"].(string))); err != nil {
							t.Fatal(err)
						}
					case "staging_changed":
						if err := os.WriteFile(filepath.Join(root, entries[1]["staging_path"].(string)), []byte("drift"), 0644); err != nil {
							t.Fatal(err)
						}
					case "canonical_changed":
						if err := os.WriteFile(filepath.Join(root, entries[0]["path"].(string)), []byte("drift"), 0644); err != nil {
							t.Fatal(err)
						}
					case "manifest_changed":
						entries[1]["path"] = ".claude/evidence/unauthorized.json"
					case "downgrade":
						pending["schema_version"] = "1.0.0"
						delete(pending, "artifacts")
					case "unknown_state":
						var state map[string]any
						if err := json.Unmarshal(mustRead(t, sp), &state); err != nil {
							t.Fatal(err)
						}
						state["updated_at"] = "2026-10-04T00:00:00Z"
						if err := os.WriteFile(sp, mustJSON(t, state), 0644); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(sp+".commit-pending.json", mustJSON(t, pending), 0644); err != nil {
						t.Fatal(err)
					}
					beforeState, beforeJournal, beforeMarker := mustRead(t, sp), mustRead(t, jp), mustRead(t, sp+".commit-pending.json")
					if _, err := testWriter(sp, jp).RecoverPendingOperations(); err == nil {
						t.Fatal("bad pending bundle accepted")
					}
					if !bytes.Equal(beforeState, mustRead(t, sp)) || !bytes.Equal(beforeJournal, mustRead(t, jp)) || !bytes.Equal(beforeMarker, mustRead(t, sp+".commit-pending.json")) {
						t.Fatal("failed recovery changed durable inputs")
					}
					if _, err := os.Stat(filepath.Join(root, artifacts[1].Path)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("recovery published before validating complete bundle")
					}
				})
			}
		})
	}
}
