package review

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestFrozenSubjectsForProductBaselineIncludesTasksAndDeletedPaths(t *testing.T) {
	snapshot := &buildsurface.Snapshot{
		Tasks: []buildsurface.Task{
			{ID: "TASK-B", Path: "tasks/B.md", SHA256: "b"},
			{ID: "TASK-A", Path: "tasks/A.md", SHA256: "a"},
		},
		Paths: []buildsurface.Path{
			{Path: "internal/add.go", Status: "added", SHA256: "c"},
			{Path: "internal/remove.go", Status: "deleted", SHA256: "d", BlobOID: "e", PreimageCommit: "f", DeletedByCommit: "g"},
			{Path: "internal/change.go", Status: "modified", SHA256: "h"},
		},
	}

	got, err := frozenSubjectsForProductBaseline(snapshot)
	if err != nil {
		t.Fatalf("frozenSubjectsForProductBaseline: %v", err)
	}
	want := []FrozenSubject{
		{Path: "internal/add.go", SHA256: "c", Kind: "changed_surface"},
		{Path: "internal/change.go", SHA256: "h", Kind: "changed_surface"},
		{Path: "internal/remove.go", SHA256: "d", Kind: "deleted"},
		{Path: "tasks/A.md", SHA256: "a", Kind: "task"},
		{Path: "tasks/B.md", SHA256: "b", Kind: "task"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frozen subjects = %#v, want %#v", got, want)
	}
}

func TestFrozenSubjectsForProductBaselineRejectsDeletedLockedTask(t *testing.T) {
	for _, sha := range []string{"task", "preimage"} {
		_, err := frozenSubjectsForProductBaseline(&buildsurface.Snapshot{
			Tasks: []buildsurface.Task{{ID: "TASK-A", Path: "tasks/A.md", SHA256: "task"}},
			Paths: []buildsurface.Path{{Path: "tasks/A.md", Status: "deleted", SHA256: sha}},
		})
		if err == nil {
			t.Fatal("deleted locked TASK source was accepted")
		}
	}
}

func TestVerifyProductBaselineDeclarationRequiresCanonicalSnapshot(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-test",
		"baseline":   map[string]any{"generation": 1},
		"entities": map[string]any{
			"assignments": []any{map[string]any{"runtime_id": "loop-test", "baseline_generation": 1}},
		},
	}
	if err := verifyProductBaselineDeclaration("", state, &Plan{}); err == nil {
		t.Fatal("canonical Runtime without product_baseline was accepted")
	}
}

func TestSubjectDigestVersionBindsProductBaselineAndFrozenKind(t *testing.T) {
	snapshot := buildsurface.Snapshot{Contract: buildsurface.Contract, RuntimeID: "loop-test", BaselineGeneration: 1, FinalCommit: "head-1"}
	digest, err := buildsurface.Digest(snapshot)
	if err != nil {
		t.Fatalf("buildsurface.Digest: %v", err)
	}
	snapshot.Digest = digest
	plan := &Plan{ProductBaseline: &snapshot, FrozenSubjects: []FrozenSubject{{Path: "internal/a.go", SHA256: "a", Kind: "changed_surface"}}}
	base := SubjectDigest(plan)

	changedKind := *plan
	changedKind.FrozenSubjects = append([]FrozenSubject(nil), plan.FrozenSubjects...)
	changedKind.FrozenSubjects[0].Kind = "deleted"
	if got := SubjectDigest(&changedKind); got == base {
		t.Fatal("SubjectDigest did not bind frozen subject kind")
	}

	changedSnapshot := snapshot
	changedSnapshot.FinalCommit = "head-2"
	changedSnapshot.Digest, err = buildsurface.Digest(changedSnapshot)
	if err != nil {
		t.Fatalf("build changed product baseline digest: %v", err)
	}
	changedBaseline := *plan
	changedBaseline.ProductBaseline = &changedSnapshot
	if got := SubjectDigest(&changedBaseline); got == base {
		t.Fatal("SubjectDigest did not bind product baseline")
	}
}

func TestSubjectDigestKeepsHistoricalFormatWithoutProductBaseline(t *testing.T) {
	plan := &Plan{FrozenSubjects: []FrozenSubject{{Path: "internal/a.go", SHA256: "abc"}}}
	want := sha256Of([]byte("internal/a.go:abc"))
	if got := SubjectDigest(plan); got != want {
		t.Fatalf("historical SubjectDigest = %s, want %s", got, want)
	}
}

func TestCaptureSubjectDigestsDistinguishProvenDeletedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "present.txt"), []byte("present"), 0600); err != nil {
		t.Fatal(err)
	}
	captured := captureSubjectDigests(root, []FrozenSubject{
		{Path: "present.txt", SHA256: sha256Of([]byte("present")), Kind: "changed_surface"},
		{Path: "deleted.txt", SHA256: "preimage-sha", Kind: "deleted"},
	})
	if len(captured) != 2 {
		t.Fatalf("captured subjects = %#v", captured)
	}
	if captured[0].Status != "present" || captured[0].SHA256 != sha256Of([]byte("present")) {
		t.Fatalf("present subject capture = %#v", captured[0])
	}
	if captured[1].Status != "missing" || captured[1].SHA256 != "" {
		t.Fatalf("deleted subject capture = %#v", captured[1])
	}
}

func TestUndeclaredProductDriftPreservesSpacesInGitPaths(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	path := "internal/new product file.go"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte("untracked product"), 0o600); err != nil {
		t.Fatal(err)
	}
	contextPath := workspace.DispatchContextSource(workspace.Execution{
		RuntimeID: "loop-context-drift", BaselineGeneration: 1, AssignmentID: "assignment-build-TASK001", Generation: 1,
	})
	nearbyProduct := filepath.ToSlash(filepath.Join(filepath.Dir(contextPath), "product.json"))
	for _, rel := range []string{contextPath, nearbyProduct} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	drift, err := detectUndeclaredProductDrift(root, &Plan{})
	if err != nil {
		t.Fatalf("detectUndeclaredProductDrift: %v", err)
	}
	if !reflect.DeepEqual(drift, []string{nearbyProduct, path}) {
		t.Fatalf("drift = %#v, want neighboring and source product files while excluding generated context %s", drift, contextPath)
	}
}

func TestReviewPlanSchemaValidatesProductBaselineShapeStrictly(t *testing.T) {
	root := t.TempDir()
	plan := map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      "review-plan-canonical",
		"review_round":        1,
		"baseline_generation": 1,
		"product_baseline": map[string]any{
			"contract": "s7-product-baseline-v1", "runtime_id": "loop-canonical", "baseline_generation": 1,
			"start_receipt_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"start_commit":         "1111111111111111111111111111111111111111",
			"final_commit":         "2222222222222222222222222222222222222222",
			"project_root":         "/repo", "development_branch": "development",
			"tasks": []any{map[string]any{"id": "TASK-1", "path": "tasks/1.md", "version": "v1", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
			"contributions": []any{map[string]any{
				"assignment_record":        map[string]any{"runtime_id": "loop-canonical", "baseline_generation": 1, "assignment_id": "assignment-a", "assignment_revision": 1},
				"assignment_record_sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"result_ref":               "results/result.json", "result_sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"checkpoint_ref": "checkpoints/main.json", "checkpoint_sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				"merge_commit": "3333333333333333333333333333333333333333", "tested_head": "4444444444444444444444444444444444444444",
				"main_facts_sha256": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			}},
			"paths": []any{map[string]any{
				"path": "internal/removed.go", "status": "deleted",
				"sha256":            "abababababababababababababababababababababababababababababababab",
				"blob_oid":          "5555555555555555555555555555555555555555",
				"preimage_commit":   "6666666666666666666666666666666666666666",
				"deleted_by_commit": "7777777777777777777777777777777777777777",
			}},
			"digest": "8888888888888888888888888888888888888888888888888888888888888888",
		},
		"frozen_subjects": []any{map[string]any{"path": "internal/removed.go", "sha256": "abababababababababababababababababababababababababababababababab", "kind": "deleted"}},
		"claims": []any{map[string]any{
			"claim_id": "claim-qa-one", "lens": "qa", "target": "internal/removed.go", "assertion": "the deletion preserves the contract",
			"oracle": "inspect the caller contract", "method": "static code review", "applicability": "required", "source_refs": []any{"internal/removed.go"},
		}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-qa-one", "lens": "qa", "claim_ids": []any{"claim-qa-one"},
			"non_overlap_boundary": "owns the deletion check", "execution_wave": "static",
		}},
		"e2e_coverage_state": "not_applicable", "dispatch_capacity_policy": "coverage_complete",
		"created_by": "orchestrator", "created_at": "2026-10-14T00:00:00Z",
	}
	validate := func(value map[string]any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return schema.NewValidator(root).ValidateBytes("review-plan.schema.json", data)
	}
	if err := validate(plan); err != nil {
		t.Fatalf("valid canonical ReviewPlan schema: %v", err)
	}
	productBaseline := plan["product_baseline"].(map[string]any)
	productBaseline["worker_supplied_authority"] = true
	if err := validate(plan); err == nil {
		t.Fatal("product_baseline with an unknown authority field was accepted")
	}
	delete(productBaseline, "worker_supplied_authority")
	productBaseline["start_commit"] = strings.Repeat("1", 41)
	if err := validate(plan); err == nil {
		t.Fatal("product_baseline with a non-Git commit length was accepted")
	}
}
