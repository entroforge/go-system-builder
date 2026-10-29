package assignment

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestResolveFrozenNativeCheckSourceUsesOnlyFrozenManifestWorktree(t *testing.T) {
	fixture := nativeCheckSourceFixture(t, true)
	source, err := ResolveFrozenNativeCheckSource(fixture.root, fixture.state, fixture.record)
	if err != nil {
		t.Fatal(err)
	}
	if source.Checkout != fixture.worktree || source.Branch != fixture.branch || source.TargetBranch != "main" || source.Head == "" {
		t.Fatalf("native source = %+v", source)
	}
	if source.ManifestRef != fixture.manifestRef || source.ManifestSHA256 != fixture.record.Manifest.SHA256 {
		t.Fatalf("native source lost its frozen manifest binding: %+v", source)
	}
}

func TestResolveFrozenNativeCheckSourceRejectsSidecarOnlyCoordinates(t *testing.T) {
	fixture := nativeCheckSourceFixture(t, false)
	if err := os.MkdirAll(filepath.Join(fixture.root, ".claude/assignments"), 0o755); err != nil {
		t.Fatal(err)
	}
	sidecar, _ := json.Marshal(map[string]any{
		"assignment_id": fixture.record.AssignmentID,
		"worktree_path": fixture.worktree,
		"branch":        fixture.branch,
		"target_branch": "main",
	})
	if err := os.WriteFile(filepath.Join(fixture.root, ".claude/assignments", fixture.record.AssignmentID+".json"), sidecar, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveFrozenNativeCheckSource(fixture.root, fixture.state, fixture.record); err == nil || !strings.Contains(err.Error(), "sidecar-only") {
		t.Fatalf("sidecar-only coordinate was accepted: %v", err)
	}
}

func TestResolveFrozenNativeCheckSourceRejectsManifestDriftAndForeignCheckout(t *testing.T) {
	t.Run("manifest drift", func(t *testing.T) {
		fixture := nativeCheckSourceFixture(t, true)
		path := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
		if err := os.WriteFile(path, append(fixture.manifest, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveFrozenNativeCheckSource(fixture.root, fixture.state, fixture.record); err == nil || !strings.Contains(err.Error(), "bytes changed") {
			t.Fatalf("manifest drift was accepted: %v", err)
		}
	})
	t.Run("foreign checkout", func(t *testing.T) {
		fixture := nativeCheckSourceFixture(t, false)
		foreign := t.TempDir()
		foreignGit(t, foreign)
		updateNativeCheckManifestPath(t, &fixture, foreign)
		if _, err := ResolveFrozenNativeCheckSource(fixture.root, fixture.state, fixture.record); err == nil || !strings.Contains(err.Error(), "another repository") {
			t.Fatalf("foreign checkout was accepted: %v", err)
		}
	})
}

type nativeCheckSourceFixtureData struct {
	root, manifestRef, branch, worktree string
	manifest                            []byte
	state                               map[string]any
	record                              assignmentRecordAlias
}

// Keep the fixture field's concrete type local to this package's canonical
// registration helper while avoiding a test-only alternate Record shape.
type assignmentRecordAlias = assignmentrecord.Record

func nativeCheckSourceFixture(t *testing.T, includeCoordinates bool) nativeCheckSourceFixtureData {
	t.Helper()
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	configureRegistrationClosing(t, &fixture, assignmentrecord.ClosingContractVersion, "post_merge", "main", true)
	branch := "wt/assignment-s6-registration"
	worktree := filepath.Join(fixture.root, ".worktrees", "assignment-s6-registration")
	if includeCoordinates {
		out := nativeCheckSourceFixtureData{root: fixture.root, manifestRef: fixture.manifestRef, branch: branch, worktree: worktree, state: fixture.state}
		updateNativeCheckManifestPath(t, &out, filepath.ToSlash(filepath.Join(".worktrees", "assignment-s6-registration")))
		gitRegistration(t, fixture.root, "worktree", "add", "-b", branch, worktree, "HEAD")
		return out
	}
	records, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task)
	if err != nil {
		t.Fatalf("build current canonical Record: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want one", len(records))
	}
	entities := fixture.state["entities"].(map[string]any)
	entities["assignments"] = []any{records[0]}
	return nativeCheckSourceFixtureData{root: fixture.root, manifestRef: fixture.manifestRef, branch: branch, worktree: worktree, manifest: fixture.manifest, state: fixture.state, record: records[0]}
}

func updateNativeCheckManifestPath(t *testing.T, fixture *nativeCheckSourceFixtureData, path string) {
	t.Helper()
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	row := manifest["assignments"].([]any)[0].(map[string]any)
	branch := fixture.branch
	if branch == "" {
		branch = "wt/assignment-s6-registration"
		fixture.branch = branch
	}
	row["worktree_path"], row["branch"], row["target_branch"] = path, branch, "main"
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRegistration(t, fixture.root, "add", fixture.manifestRef)
	gitRegistration(t, fixture.root, "commit", "-m", "freeze native assignment worktree coordinates")
	records, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, updated, fixture.manifestRef, "docs/dev/tasks/TASK-001-01.md", "TASK-001-01", mustReadNativeTask(t, fixture.root, "docs/dev/tasks/TASK-001-01.md"))
	if err != nil || len(records) != 1 {
		t.Fatalf("rebuild frozen native check Record: records=%d err=%v", len(records), err)
	}
	entities := fixture.state["entities"].(map[string]any)
	entities["assignments"] = []any{records[0]}
	fixture.manifest = updated
	fixture.record = records[0]
}

func mustReadNativeTask(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func foreignGit(t *testing.T, root string) {
	t.Helper()
	commands := [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Native Source Test"},
		{"config", "user.email", "native-source@example.invalid"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "foreign"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
}
