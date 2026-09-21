package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPR8TrackedTempProductIsCaptured(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	boundaryWrite(t, root, "src/temp/business.go", "package business\n")
	boundaryGit(t, root, "add", ".")
	boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base")
	before, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	captured := false
	for _, a := range before {
		if a.Path == "src/temp/business.go" {
			captured = true
		}
	}
	boundaryWrite(t, root, "src/temp/business.go", "package business\n// modified\n")
	changes, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("captured=%v changes=%v", captured, changes)
	if !captured || len(changes) == 0 {
		t.Fatal("tracked product file lost from baseline/changeset")
	}
}

func TestTrackedCacheNamesKeepDeletionAndIgnoreOnlyOutputs(t *testing.T) {
	for _, dir := range []string{"temp", "tmp", "dist", "coverage"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			boundaryGit(t, root, "init")
			rel := "src/" + dir + "/business.go"
			boundaryWrite(t, root, ".gitignore", dir+"/\n")
			boundaryWrite(t, root, rel, "package business\n")
			boundaryGit(t, root, "add", "-f", rel, ".gitignore")
			boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base")
			before, _, err := captureRepositoryBaseline(root)
			if err != nil {
				t.Fatal(err)
			}
			boundaryGit(t, root, "rm", rel)
			after, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, a := range after {
				if a.Path == rel && a.Status == "deleted" {
					found = true
				}
			}
			if !found {
				t.Fatalf("staged deletion lost: %v", after)
			}
			if excludedBaselinePaths(root, []string{rel})[rel] {
				t.Fatal("freshness classifier exempts tracked deletion")
			}
			boundaryWrite(t, root, "generated/"+dir+"/output.bin", "generated")
			captured, _, err := captureRepositoryBaseline(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range captured {
				if a.Path == "generated/"+dir+"/output.bin" {
					t.Fatal("ignored output captured")
				}
			}
		})
	}
}
func TestCacheNameWithoutGitDoesNotHideProduct(t *testing.T) {
	root := t.TempDir()
	rel := "temp/product.go"
	boundaryWrite(t, root, rel, "package p\n")
	got, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != rel {
		t.Fatalf("non-Git input lost: %v", got)
	}
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
}
