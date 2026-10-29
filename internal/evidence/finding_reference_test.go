package evidence

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindingReferencesPreserveContentAndObservationSemantics(t *testing.T) {
	root := t.TempDir()
	data := []byte("captured output\n")
	if err := os.WriteFile(filepath.Join(root, "observation.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, ref := range []string{
		"path:observation.txt#sha256=" + digest,
		fmt.Sprintf("command_output:observation.txt#sha256=%s (bytes=%d, truncated=false)", digest, len(data)),
		fmt.Sprintf("command_output:observation.txt#sha256=%s (bytes=%d, truncated=true)", digest, len(data)+1),
		"command_output:missing.txt (withheld: redaction)",
		"artifact:missing.txt#sha256=" + digest + " (observed)",
		"artifact:missing.txt (deleted)",
		"env:SECRET (present; value never captured)",
	} {
		if err := ValidateFindingReference(root, ref); err != nil {
			t.Fatalf("original Finding reference %q: %v", ref, err)
		}
	}
	for _, ref := range []string{
		"path:observation.txt#sha256=" + strings.Repeat("0", 64),
		"path:observation.txt",
		"path:../observation.txt#sha256=" + digest,
		"path:" + filepath.Join(root, "observation.txt") + "#sha256=" + digest,
		fmt.Sprintf("command_output:observation.txt#sha256=%s (bytes=0, truncated=false)", digest),
		fmt.Sprintf("command_output:observation.txt#sha256=%s (bytes=0, truncated=true)", digest),
		"env:SECRET", "artifact:missing.txt", "runtime:unregistered", "test://not-a-finding-file",
	} {
		if err := ValidateFindingReference(root, ref); err == nil {
			t.Fatalf("invalid or unrelated Finding reference accepted: %q", ref)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err == nil {
		if err := ValidateFindingReference(root, "path:escape.txt#sha256="+digest); err == nil {
			t.Fatal("accepted a symlink outside the repository")
		}
	}
	if err := os.Symlink("observation.txt", filepath.Join(root, "inside.txt")); err == nil {
		if err := ValidateFindingReference(root, "path:inside.txt#sha256="+digest); err != nil {
			t.Fatalf("in-repository symlink accepted by S7 was lost: %v", err)
		}
	}
}
