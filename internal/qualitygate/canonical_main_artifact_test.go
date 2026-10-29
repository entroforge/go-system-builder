package qualitygate

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestCanonicalMainCheckArtifactPathIsAssignmentBoundAndSymlinkSafe(t *testing.T) {
	record := assignmentrecord.Record{RuntimeID: "loop-qualitygate-s6", BaselineGeneration: 3, AssignmentID: "assignment-qualitygate-s6", Revision: 1}
	root := t.TempDir()
	mainRel := path.Join(".claude/evidence", record.RuntimeID, "g3", "worktree", record.AssignmentID, "checks", "loop-integration-check-01", "receipt.json")
	currentRef := filepath.Join(root, filepath.FromSlash(mainRel))
	got, err := canonicalMainCheckArtifactRel(root, record, root, nil, currentRef)
	if err != nil || got != mainRel {
		t.Fatalf("current Main artifact mapping = %q, %v; want %q", got, err, mainRel)
	}
	priorRoot := filepath.Join(t.TempDir(), "prior-main")
	priorRef := filepath.Join(priorRoot, filepath.FromSlash(mainRel))
	got, err = canonicalMainCheckArtifactRel(root, record, priorRoot, []string{priorRoot}, priorRef)
	if err != nil || got != mainRel {
		t.Fatalf("authorized PreviousMainRoots mapping = %q, %v; want %q", got, err, mainRel)
	}
	for _, ref := range []string{
		filepath.Join(filepath.Dir(root), "outside", filepath.FromSlash(mainRel)),
		filepath.Join(root, ".claude/evidence", record.RuntimeID, "g4", "worktree", record.AssignmentID, "checks", "loop-integration-check-01", "receipt.json"),
		filepath.Join(root, ".claude/evidence", record.RuntimeID, "g3", "worktree", "assignment-peer", "checks", "loop-integration-check-01", "receipt.json"),
		filepath.Join(root, ".claude/evidence", record.RuntimeID, "g3", "worktree", record.AssignmentID, "checks", "loop-integration-check-01", "nested", "receipt.json"),
	} {
		if _, err := canonicalMainCheckArtifactRel(root, record, root, nil, ref); err == nil {
			t.Errorf("accepted out-of-authority Main check artifact %q", ref)
		}
	}
	external := filepath.Join(t.TempDir(), "outside-receipt.json")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, filepath.FromSlash(mainRel))
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readCanonicalMainCheckArtifact(root, record, root, nil, currentRef); err == nil {
		t.Fatal("Main check artifact symlink escaped the pinned evidence reader")
	}
}
