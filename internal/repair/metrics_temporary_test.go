package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairSessionMetricsTemporaryKeepsBaselineOwnership(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	boundaryWrite(t, root, "product.go", "package product\n")
	boundaryGit(t, root, "add", "product.go")
	boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base")
	baseline, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	session := RepairSession{BaselineArtifacts: baseline}
	path := ".claude/.loop-metrics-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp"
	boundaryWrite(t, root, path, "partial metrics")
	changes, err := ComputeSessionChangeset(root, session)
	if err != nil || len(changes) != 0 {
		t.Fatalf("untracked metrics staging changed Session diff: %v %v", changes, err)
	}
	neighbor := ".claude/.loop-metrics-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp.go"
	boundaryWrite(t, root, neighbor, "package product\n")
	changes, err = ComputeSessionChangeset(root, session)
	if err != nil || len(changes) != 1 || changes[0].Path != neighbor || changes[0].Status != "added" {
		t.Fatalf("product neighbor lost from Session diff: %v %v", changes, err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(neighbor))); err != nil {
		t.Fatal(err)
	}
	// Historical Session ownership wins even when Git never tracked the name.
	protected := RepairSession{BaselineArtifacts: append(append([]ArtifactRef(nil), baseline...), ArtifactRef{Path: path, SHA256: sha256Bytes([]byte("original"))})}
	changes, err = ComputeSessionChangeset(root, protected)
	if err != nil || len(changes) != 1 || changes[0].Path != path || changes[0].Status != "modified" {
		t.Fatalf("historically protected file hidden: %v %v", changes, err)
	}
	boundaryGit(t, root, "add", "--", path)
	got, _, err := captureRepositoryBaseline(root)
	if err != nil || !hasRepairArtifact(got, path) {
		t.Fatalf("index-owned staging name omitted: %v %v", got, err)
	}
	boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "track name")
	boundaryGit(t, root, "rm", "--", path)
	boundaryWrite(t, root, path, "untracked replacement")
	got, _, err = captureRepositoryBaseline(root)
	if err != nil || !hasRepairArtifact(got, path) {
		t.Fatalf("HEAD-owned staged deletion omitted: %v %v", got, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); err != nil || string(data) != "untracked replacement" {
		t.Fatalf("baseline scan changed unknown file: %q %v", data, err)
	}
}

func hasRepairArtifact(artifacts []ArtifactRef, path string) bool {
	for _, artifact := range artifacts {
		if artifact.Path == path {
			return true
		}
	}
	return false
}
