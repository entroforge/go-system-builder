package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChangesetIdentityIsScopedToSession(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("same payload"), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := ComputeChangeset(root, ChangesetRequest{SessionID: "session-a", ExplicitPaths: []string{"file.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComputeChangeset(root, ChangesetRequest{SessionID: "session-b", ExplicitPaths: []string{"file.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.ChangesetID == b.ChangesetID || a.Digest != b.Digest {
		t.Fatalf("identity and content digest conflated: a=%+v b=%+v", a, b)
	}
	if _, err := PersistChangeset(root, a); err != nil {
		t.Fatal(err)
	}
	if _, err := PersistChangeset(root, b); err != nil {
		t.Fatal(err)
	}
	if _, err := PersistChangeset(root, a); err == nil {
		t.Fatal("immutable overwrite accepted")
	}
}
