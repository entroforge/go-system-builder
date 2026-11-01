package workspace

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
)

func TestDispatchUsesOriginalPathsWithoutRecordOrSourceDigest(t *testing.T) {
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
	e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatal(err)
	}
	for path, locator := range e.Inputs {
		if path != locator {
			t.Fatalf("input %s carries digest instead of original location", path)
		}
	}
	got, err := f.binding.ReadDispatchContext(e)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dispatch.AssignmentRecord == nil || *got.Dispatch.AssignmentRecord != f.record.Reference() {
		t.Fatal("original responsibility lost")
	}
}
