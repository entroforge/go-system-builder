package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkerDraftIgnoreKeepsUnexpectedFilesVisible(t *testing.T) {
	f := coreBuilderFixture(t)
	e := prepareCoreBuilder(t, f, "backend-core")
	for _, name := range []string{"plan.json", "stage-answer.json", "completion.json", "commit.json", "repair-result.json"} {
		if _, err := workspace.Git(context.Background(), e.Path, "check-ignore", "--quiet", ".loop-submissions/"+name); err != nil {
			t.Fatalf("fixed control draft must be ignored for non-forced cleanup: %s: %v", name, err)
		}
	}
	unexpected := ".loop-submissions/unexpected.json"
	if err := os.WriteFile(filepath.Join(e.Path, unexpected), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := workspace.Git(context.Background(), e.Path, "status", "--porcelain", "--untracked-files=all")
	if err != nil || !strings.Contains(status, unexpected) {
		t.Fatalf("unexpected file was hidden from the original dirty-worktree guard: %s: %v", status, err)
	}
}
