package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS7WorkspaceDigestUsesRegisteredWorkerSourceReadOnly(t *testing.T) {
	fix, execution := s7WorkspaceDigestWorkerFixture(t)
	mainSource := filepath.Join(fix.root, "e2e-workspace", "spec.txt")
	workerSource := filepath.Join(execution.Path, "e2e-workspace", "spec.txt")
	writeDigestSource(t, mainSource, "main version\n")
	writeDigestSource(t, workerSource, "worker version\n")

	want, err := review.WorkspaceDigest(execution.Path, "e2e-workspace")
	if err != nil {
		t.Fatal(err)
	}
	mainDigest, err := review.WorkspaceDigest(fix.root, "e2e-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if want == mainDigest {
		t.Fatal("fixture must distinguish the Main and Worker verification sources")
	}

	stateBefore := readDigestFile(t, fix.statePath)
	journalBefore := readOptionalDigestFile(t, fix.journalPath)
	workerBefore := readDigestFile(t, workerSource)
	var out, stderr bytes.Buffer
	code := runS7Command([]string{"workspace-digest", "--root", execution.Path}, &out, &stderr)
	if code != 0 {
		t.Fatalf("Worker workspace-digest exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if !strings.HasPrefix(out.String(), want+"  e2e-workspace\n") {
		t.Fatalf("digest used wrong root: got=%q want prefix=%q", out.String(), want+"  e2e-workspace\n")
	}
	if !bytes.Equal(stateBefore, readDigestFile(t, fix.statePath)) ||
		!bytes.Equal(journalBefore, readOptionalDigestFile(t, fix.journalPath)) ||
		!bytes.Equal(workerBefore, readDigestFile(t, workerSource)) {
		t.Fatal("read-only Worker digest command changed the Runtime, journal, or source")
	}
}

func TestS7WorkspaceDigestRejectsWrongWorkerPointerWithoutRepair(t *testing.T) {
	fix, execution := s7WorkspaceDigestWorkerFixture(t)
	workerSource := filepath.Join(execution.Path, "e2e-workspace", "spec.txt")
	writeDigestSource(t, workerSource, "worker version\n")
	pointerPath := filepath.Join(execution.Path, ".claude", "loop-workspace.json")
	pointerBytes := readDigestFile(t, pointerPath)
	var pointer map[string]any
	if err := json.Unmarshal(pointerBytes, &pointer); err != nil {
		t.Fatal(err)
	}
	pointer["assignment_id"] = "wrong-assignment"
	tampered, err := json.Marshal(pointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pointerPath, tampered, 0600); err != nil {
		t.Fatal(err)
	}

	stateBefore := readDigestFile(t, fix.statePath)
	journalBefore := readOptionalDigestFile(t, fix.journalPath)
	sourceBefore := readDigestFile(t, workerSource)
	var out, stderr bytes.Buffer
	code := runS7Command([]string{"workspace-digest", "--root", execution.Path}, &out, &stderr)
	if code == 0 || !strings.Contains(out.String()+stderr.String(), "stale or unregistered worker pointer") {
		t.Fatalf("wrong Worker pointer was accepted: exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if !bytes.Equal(stateBefore, readDigestFile(t, fix.statePath)) ||
		!bytes.Equal(journalBefore, readOptionalDigestFile(t, fix.journalPath)) ||
		!bytes.Equal(sourceBefore, readDigestFile(t, workerSource)) ||
		!bytes.Equal(tampered, readDigestFile(t, pointerPath)) {
		t.Fatal("wrong-pointer rejection repaired or changed a Runtime, pointer, or source")
	}
}

func TestS7WorkspaceDigestKeepsMainAndLegacyNoWorkspaceBehavior(t *testing.T) {
	t.Run("main", func(t *testing.T) {
		fix, _ := s7WorkspaceDigestWorkerFixture(t)
		mainSource := filepath.Join(fix.root, "e2e-workspace", "spec.txt")
		writeDigestSource(t, mainSource, "main version\n")
		want, err := review.WorkspaceDigest(fix.root, "e2e-workspace")
		if err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		code := runS7Command([]string{"workspace-digest", "--root", fix.root}, &out, &stderr)
		if code != 0 || !strings.HasPrefix(out.String(), want+"  e2e-workspace\n") {
			t.Fatalf("Main workspace digest changed: exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
		}
	})

	for _, hasLegacyPlan := range []bool{false, true} {
		name := "no registered plan"
		if hasLegacyPlan {
			name = "legacy plan without pinned workspace"
		}
		t.Run(name, func(t *testing.T) {
			fix := newRuntimeFixture(t)
			if hasLegacyPlan {
				reviewState, _ := fix.state["review"].(map[string]any)
				reviewState["plan"] = map[string]any{
					"plan_id":            "legacy-review-plan",
					"path":               ".claude/review/plans/legacy-review-plan.json",
					"sha256":             strings.Repeat("a", 64),
					"revision":           1,
					"review_round":       1,
					"status":             "running",
					"e2e_coverage_state": "regression_available",
				}
			}
			fix.persist(t)
			var out, stderr bytes.Buffer
			code := runS7Command([]string{"workspace-digest", "--root", fix.root}, &out, &stderr)
			if code != 0 || !strings.Contains(out.String(), "no verification artifact workspace is pinned") {
				t.Fatalf("legacy no-workspace behavior changed: exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
		})
	}
}

func s7WorkspaceDigestWorkerFixture(t *testing.T) (*runtimeFixture, workspace.Execution) {
	t.Helper()
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{
		{"init", "-b", "customer-development"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.invalid"},
		{"add", "docs/control/loop-definition.json"},
		{"commit", "-m", "base"},
	} {
		if out, err := runGit(t, fix.root, args...); err != nil {
			t.Fatalf("git %v: %v", out, err)
		}
	}
	bindFixtureWorkspace(t, fix)
	binding, err := workspace.New(context.Background(), fix.root)
	if err != nil {
		t.Fatal(err)
	}
	e, err := binding.Plan(context.Background(), fix.state, "review-assignment", "reviewer-spec", []string{"e2e-workspace/"}, []string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding.Executions[e.AssignmentID] = e
	if err := binding.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	binding.Executions[e.AssignmentID] = e
	fix.state["workspace"] = workspace.Encode(binding)
	reviewState, _ := fix.state["review"].(map[string]any)
	reviewState["plan"] = map[string]any{
		"plan_id":                         "review-plan-workspace-digest",
		"path":                            ".claude/review/plans/review-plan-workspace-digest.json",
		"sha256":                          strings.Repeat("a", 64),
		"revision":                        1,
		"review_round":                    1,
		"status":                          "running",
		"e2e_coverage_state":              "cold_start",
		"verification_artifact_workspace": "e2e-workspace",
	}
	fix.persist(t)
	return fix, e
}

func writeDigestSource(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func readDigestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readOptionalDigestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}
