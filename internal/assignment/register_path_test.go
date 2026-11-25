package assignment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisteredAbsoluteArtifactBecomesRepositoryRelative(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{root, relativeRoot} {
		ref := repositoryPath(spelling, filepath.Join(root, "artifacts", "manifest.json"))
		if ref != "artifacts/manifest.json" {
			t.Fatalf("root %q produced nonportable reference %q", spelling, ref)
		}
		if err := ensureRepositoryRef("manifest", ref); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "manifest.json")
	if err := ensureRepositoryRef("manifest", repositoryPath(relativeRoot, outside)); err == nil {
		t.Fatal("accepted a host-absolute artifact outside the repository")
	}
}
