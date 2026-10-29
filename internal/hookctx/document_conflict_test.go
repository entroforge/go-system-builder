package hookctx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestLoaderProjectsDocumentConflictFromOriginalReadbackRef(t *testing.T) {
	for _, tc := range []struct {
		name, event, ref string
		blocked          bool
	}{
		{"original conflict", "document_conflict_reported", "evidence/conflict.json", true},
		{"missing original reference", "document_conflict_reported", "", false},
		{"old readback is not work blocker", "work_blocked", "evidence/old-readback.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			state := map[string]any{"schema_version": "1.3.0", "runtime_id": "loop-test", "revision": 1,
				"entities": map[string]any{"agents": []any{map[string]any{"id": "owner", "state": "blocked", "last_lifecycle_event": tc.event, "readback_ref": tc.ref}}, "tasks": []any{}, "teams": []any{}, "bugs": []any{}}}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			context, err := hookctx.Load(root, "owner")
			if err != nil {
				t.Fatal(err)
			}
			facts := hook.ProjectResponsibility(context.Agent, nil, false)
			if (facts.Status == workstate.Blocked) != tc.blocked || facts.ExecutionMayEnd != tc.blocked {
				t.Fatalf("original blocker projection=%+v; blocked=%t", facts, tc.blocked)
			}
		})
	}
}
