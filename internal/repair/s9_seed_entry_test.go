package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS9EntrySeedDoesNotClaimUnregisteredPublication(t *testing.T) {
	root := t.TempDir()
	create := func() (ArtifactRef, error) {
		return createS7ReviewPlanSeedForEntry(root, "review-plan-entry-test", 1, ChangeImpact{}, ContractRef{Path: "contract.json"}, []string{"TASK-1"}, "impact.json", "2026-10-30T00:00:00Z")
	}
	first, err := create()
	if err != nil {
		t.Fatal(err)
	}
	// No Runtime handoff was committed. An existing file is not a receipt.
	second, err := create()
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatal("unregistered file was claimed as a replay")
	}
	for _, ref := range []ArtifactRef{first, second} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil {
			t.Fatal(err)
		}
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		if plan["review_plan_id"] != "review-plan-entry-test" || plan["created_at"] != "2026-10-30T00:00:00Z" {
			t.Fatalf("wrong original seed: %s", data)
		}
		if _, exists := plan["review_round"]; exists {
			t.Fatal("new seed wrote review round")
		}
	}
}

func TestS9EntrySeedConcurrentPublicationsHaveDistinctUnclaimedPaths(t *testing.T) {
	root := t.TempDir()
	type result struct {
		ref ArtifactRef
		err error
	}
	done := make(chan result, 8)
	for i := 0; i < cap(done); i++ {
		go func() {
			ref, err := createS7ReviewPlanSeedForEntry(root, "review-plan-entry-test", 1, ChangeImpact{}, ContractRef{Path: "contract.json"}, []string{"TASK-1"}, "impact.json", "2026-10-30T00:00:00Z")
			done <- result{ref, err}
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < cap(done); i++ {
		got := <-done
		if got.err != nil {
			t.Fatal(got.err)
		}
		if seen[got.ref.Path] {
			t.Fatalf("concurrent attempts claimed the same unpublished file: %s", got.ref.Path)
		}
		seen[got.ref.Path] = true
		if _, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(got.ref.Path))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestS9EntrySeedPreservesOccupiedPathAndUsesAnother(t *testing.T) {
	for _, content := range []string{`{"partial":`, `{"different":"existing"}`} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			rel := responsibilityArtifactPath("s7-seeds", "review-plan-entry-test")
			path := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			ref, err := createS7ReviewPlanSeedForEntry(root, "review-plan-entry-test", 1, ChangeImpact{}, ContractRef{Path: "contract.json"}, []string{"TASK-1"}, "impact.json", "2026-10-30T00:00:00Z")
			if err != nil {
				t.Fatal(err)
			}
			if ref.Path == rel {
				t.Fatal("claimed occupied path")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != content {
				t.Fatalf("existing content changed: %q %v", data, err)
			}
			if !strings.Contains(ref.Path, "s7-seeds-alt-") {
				t.Fatalf("expected alternate path, got %s", ref.Path)
			}
		})
	}
}
