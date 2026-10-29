package transition

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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
	n, err := sharedModelDocuments(state, ctx, true, nil)
	if err != nil || n != 4 {
		t.Fatalf("registered %d: %v", n, err)
	}
	if _, err = sharedModelDocuments(state, ctx, false, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs/architecture/data-model/request.json")
	if err = os.WriteFile(path, []byte(`{"order_id":"changed-but-still-valid"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = sharedModelDocuments(state, ctx, false, nil); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("drift accepted: %v", err)
	}
	// Legitimate replanning re-registers; freezing does not launder the old hash.
	if _, err = sharedModelDocuments(state, ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = sharedModelDocuments(state, ctx, false, nil); err != nil {
		t.Fatal(err)
	}
}
func TestSharedBaselineCannotDisappearAtFreeze(t *testing.T) {
	root, state, ctx := sharedFixture(t)
	if _, err := sharedModelDocuments(state, ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "docs/dev/contracts/CONTRACTS-001.md")
	if err := os.WriteFile(p, []byte("> Shared model policy: none\n> Shared model reason: UI-only\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedModelDocuments(state, ctx, false, nil); err == nil {
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

func TestS3SharedRegistrationUsesAcceptedMembership(t *testing.T) {
	for _, change := range []string{"unchanged", "unaccepted member", "missing member"} {
		t.Run(change, func(t *testing.T) {
			root, state, ctx := sharedFixture(t)
			checked := sharedmodel.Check(root, ctx.Request.Files)
			if len(checked.Problems) != 0 || len(checked.Files) != 4 {
				t.Fatalf("model fixture: %+v", checked)
			}
			accepted := &acceptedS3ContractBatch{subjects: map[string]workpacket.Subject{}, sharedOutputs: map[string]bool{}}
			for path, data := range checked.Files {
				accepted.subjects[path] = workpacket.Subject{Path: path, SHA256: SHA256(data)}
				accepted.sharedOutputs[path] = true
			}
			// A frozen upstream JSON is not automatically a shared model.
			accepted.subjects["docs/architecture/unrelated.json"] = workpacket.Subject{Path: "docs/architecture/unrelated.json", SHA256: SHA256([]byte("{}"))}
			switch change {
			case "unaccepted member":
				delete(accepted.subjects, "docs/architecture/data-model/request.json")
			case "missing member":
				accepted.sharedOutputs["docs/architecture/data-model/removed.json"] = true
			}
			count, err := sharedModelDocuments(state, ctx, true, accepted)
			if change == "unchanged" {
				if err != nil || count != 4 || len(sharedModelPaths(state)) != 4 {
					t.Fatalf("exact accepted models: count=%d err=%v documents=%v", count, err, state["documents"])
				}
			} else if err == nil || len(state["documents"].([]any)) != 0 {
				t.Fatalf("model set drift partially registered: %v %v", err, state["documents"])
			}
		})
	}
}
