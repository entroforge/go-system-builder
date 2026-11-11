package repair

import (
	"os"
	"path/filepath"
	"testing"
)

// Historical paths continue to resolve without revalidating their old hash.
// New references carry the path and do not register a digest.
func TestRepairArtifactUsesActualPathWithoutDigestProof(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "accepted.json")
	want := []byte(`{"result":"pass"}`)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []ArtifactRef{{Path: "accepted.json"}, {Path: "accepted.json"}} {
		got, err := readArtifact(root, ref, "")
		if err != nil || string(got) != string(want) {
			t.Fatalf("read original path %q: got %q, err %v", ref.Path, got, err)
		}
	}
	if got := fileRef("accepted.json", want); got.Path != "accepted.json" {
		t.Fatalf("new artifact reference must not register a digest: %+v", got)
	}
}
