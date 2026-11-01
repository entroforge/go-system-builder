package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
)

func TestFreezeDispatchContextPreservesOccupiedPathAndReplaysRecordedSource(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "parent-file"} {
		t.Run(kind, func(t *testing.T) {
			f := newAssignmentRecordWorkspaceFixture(t)
			e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
			if err != nil {
				t.Fatal(err)
			}
			e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
			if err != nil {
				t.Fatal(err)
			}
			dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, f.record)
			if err != nil {
				t.Fatal(err)
			}
			original := DispatchContextSource(e)
			occupied := filepath.Join(f.binding.MainRoot, filepath.FromSlash(original))
			if kind == "parent-file" {
				occupied = filepath.Dir(occupied)
			}
			if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file", "parent-file":
				if err := os.WriteFile(occupied, []byte("unknown"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(occupied, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("unrelated", occupied); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(occupied)
			if err != nil {
				t.Fatal(err)
			}
			e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
			if err != nil {
				t.Fatal(err)
			}
			actual := DispatchContextSource(e)
			if actual == original || e.Inputs[actual] != actual {
				t.Fatalf("actual context not recorded: %s", actual)
			}
			if _, err := f.binding.ReadDispatchContext(e); err != nil {
				t.Fatal(err)
			}
			replay, err := f.binding.FreezeDispatchContext(e, dispatch, nil)
			if err != nil {
				t.Fatal(err)
			}
			if DispatchContextSource(replay) != actual {
				t.Fatal("replay lost original context")
			}
			after, err := os.Lstat(occupied)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("unknown entry changed: %v", err)
			}
		})
	}
}
