package req039_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Runtime maintenance must preserve the actual user-owned TASK document;
// maintenance is not a new means to replace or freeze its source contents.
func TestBranchAuditMaintenancePreservesTaskDocument(t *testing.T) {
	for _, command := range []string{"reconcile-policy-ref"} {
		t.Run(command, func(t *testing.T) {
			root := nativeMultiBuilderRoot(t)
			path := filepath.Join(root, "docs/dev/tasks/TASK-042-02.md")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			changed := append(original, []byte("\nUser-maintained clarification.\n")...)
			if err := os.WriteFile(path, changed, 0644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := runCLI(t, []string{"runtime", command, "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
				t.Fatalf("maintenance %s: %d %s %s", command, code, stdout.String(), stderr.String())
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, changed) {
				t.Fatalf("maintenance overwrote TASK: %v", err)
			}
		})
	}
}
