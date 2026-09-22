package transition

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func sharedFixture(t *testing.T) (string, map[string]any, *ActionContext) {
	t.Helper()
	root := t.TempDir()
	src := "../../docs/examples/shared-model/project"
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(out, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "documents": []any{}}
	ctx := &ActionContext{Root: root, OccurredAt: time.Now(), Request: &Request{Actor: "planner", Files: fileview.Disk{Root: root}}}
	return root, state, ctx
}
func TestSharedDesignRegistrationAndFreeze(t *testing.T) {
	root, state, ctx := sharedFixture(t)
	n, err := sharedModelDocuments(state, ctx, true)
	if err != nil || n != 4 {
		t.Fatalf("registered %d: %v", n, err)
	}
	if _, err = sharedModelDocuments(state, ctx, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs/architecture/data-model/request.json")
	if err = os.WriteFile(path, []byte(`{"order_id":"changed-but-still-valid"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = sharedModelDocuments(state, ctx, false); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("drift accepted: %v", err)
	}
	// Legitimate replanning re-registers; freezing does not launder the old hash.
	if _, err = sharedModelDocuments(state, ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err = sharedModelDocuments(state, ctx, false); err != nil {
		t.Fatal(err)
	}
}
func TestSharedBaselineCannotDisappearAtFreeze(t *testing.T) {
	root, state, ctx := sharedFixture(t)
	if _, err := sharedModelDocuments(state, ctx, true); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "docs/dev/contracts/CONTRACTS-001.md")
	if err := os.WriteFile(p, []byte("> Shared model policy: none\n> Shared model reason: UI-only\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedModelDocuments(state, ctx, false); err == nil {
		t.Fatal("removed baseline accepted")
	}
}

func TestS3NaturalGuardRejectsSharedModelDrift(t *testing.T) {
	root, state, ctx := sharedFixture(t)
	state["root"] = root
	state["_file_view"] = ctx.Request.Files
	if err := guardContractsCheckedFn(state, nil); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "docs/dev/contracts/FE-001.md")
	if err := os.WriteFile(p, []byte("# FE\n## Shared model inputs\n[wrong](../../architecture/data-model/different.json)\n§1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := guardContractsCheckedFn(state, nil); err == nil || !strings.Contains(err.Error(), "must reference") {
		t.Fatalf("natural guard accepted drift: %v", err)
	}
}
