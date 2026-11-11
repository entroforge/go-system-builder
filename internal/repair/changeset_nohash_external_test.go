package repair_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestChangesetOriginalGitDiffAndExplicitPathsWithoutMachineDigests(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init")
	git("config", "user.name", "simulation")
	git("config", "user.email", "simulation@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deleted.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	git("tag", "baseline")
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "change")
	eventTime := time.Date(2026, 11, 1, 12, 0, 0, 1, time.UTC)
	c, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "session-1", BaseRef: "baseline", HeadRef: "HEAD", OccurredAt: eventTime})
	if err != nil {
		t.Fatal(err)
	}
	want := []repair.ArtifactRef{{ID: "changed-deleted-txt", Path: "deleted.txt", Status: "deleted"}, {ID: "changed-kept-txt", Path: "kept.txt", Status: "modified"}}
	if !reflect.DeepEqual(c.Artifacts, want) {
		t.Fatalf("changeset = %#v", c)
	}
	ref, err := repair.PersistChangeset(root, c)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := repair.PersistChangeset(root, c); err != nil || replay.Path != ref.Path {
		t.Fatalf("same-event replay = %#v, %v", replay, err)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"digest", "base_ref", "head_ref"} {
		if _, ok := stored[key]; ok {
			t.Fatalf("new changeset contains %s: %s", key, body)
		}
	}
	for _, item := range stored["artifacts"].([]any) {
		if _, ok := item.(map[string]any)["sha256"]; ok {
			t.Fatalf("new changeset contains artifact SHA: %s", body)
		}
	}
	explicit, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "session-1", ExplicitPaths: []string{"kept.txt"}})
	if err != nil || len(explicit.Artifacts) != 1 {
		t.Fatalf("explicit changeset = %#v, %v", explicit, err)
	}
}

func TestChangesetPreservesUnrelatedExistingEventFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "product.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "s", ExplicitPaths: []string{"product.txt"}, OccurredAt: time.Date(2026, 11, 1, 12, 0, 0, 1, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, filepath.FromSlash(repair.ArtifactStoragePath("changesets", c.ChangesetID)))
	if err := os.MkdirAll(filepath.Dir(original), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("private material"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := repair.PersistChangeset(root, c)
	if err != nil || ref.ID == "" {
		t.Fatalf("new event = %#v, %v", ref, err)
	}
	if again, err := repair.PersistChangeset(root, c); err != nil || again != ref {
		t.Fatalf("same event replay = %#v, %v", again, err)
	}
	if body, err := os.ReadFile(original); err != nil || string(body) != "private material" {
		t.Fatalf("unrelated file changed: %q, %v", body, err)
	}
	stored, err := repair.ValidateChangeset(root, ref)
	if err != nil || stored.ChangesetID != ref.ID || stored.Artifacts[0].Path != "product.txt" {
		t.Fatalf("new stored event = %#v, %v", stored, err)
	}
}

func TestSessionChangesetProjectsOriginalContributionsOrReportsUnknown(t *testing.T) {
	root := t.TempDir()
	session := repair.RepairSession{SessionID: "repair-session-1"}
	// A repository file by itself is not evidence that it changed during this
	// Session. Missing old baseline facts must not invent a whole-tree diff.
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changes, err := repair.ComputeSessionChangesetRecord(root, session); err == nil || !strings.Contains(err.Error(), "unknown") || len(changes.Artifacts) != 0 {
		t.Fatalf("missing source = %#v, %v", changes, err)
	}
	if changes, err := repair.ComputeSessionChangeset(root, session); err == nil || !strings.Contains(err.Error(), "unknown") || len(changes) != 0 {
		t.Fatalf("legacy session changes must remain unknown: %#v, %v", changes, err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"review":{"repair":{"session_id":"repair-session-1","plan_ref":"plan.json","result_refs":[{"path":"result.json","sha256":"wrong-old-value"}]}}}`)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), []byte(`{"contract_id":"contract-1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result := []byte(`{"assignment_id":"assignment-1","changed_artifacts":[{"path":"product.txt","status":"modified","sha256":"historical-value"}]}`)
	if err := os.WriteFile(filepath.Join(root, "result.json"), result, 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := repair.ComputeSessionChangesetRecord(root, session)
	if err != nil || changes.Source != "accepted_results" || len(changes.Artifacts) != 1 || changes.Artifacts[0].Path != "product.txt" {
		t.Fatalf("original contribution projection = %#v, %v", changes, err)
	}
	legacy, err := repair.ComputeSessionChangeset(root, session)
	if err != nil || len(legacy) != 1 || legacy[0].Path != "product.txt" || legacy[0].Status != "modified" {
		t.Fatalf("legacy session entry did not preserve original contribution: %#v, %v", legacy, err)
	}
	if _, err := repair.ComputeSessionChangesetRecord(root, repair.RepairSession{SessionID: "another-session"}); err == nil || !strings.Contains(err.Error(), "different Session") {
		t.Fatalf("different session matched: %v", err)
	}
}
