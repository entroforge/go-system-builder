package transition_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/transition"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

type tr006BatchBoundaryDisk struct {
	fileview.Disk
	verify func() error
}

func (d tr006BatchBoundaryDisk) Verify() error { return d.verify() }

func TestTR006RejectsWrongActorBeforeWriter(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "building", "", 0)
	req039fixtures.WriteState(t, root, state)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	files := tr006BatchBoundaryDisk{Disk: fileview.Disk{Root: root}, verify: func() error { verified = true; return nil }}
	_, err = transition.Apply(root, statePath, journalPath, transition.Request{Files: files, TransitionID: "TR-006", ExpectedRevision: 0, ExpectedRuntimeID: req039fixtures.RuntimeIDFromState(state), Actor: "not-an-allowed-actor"})
	if err == nil || !strings.Contains(err.Error(), "cannot execute") || verified {
		t.Fatalf("actor check changed: verified=%t err=%v", verified, err)
	}
	afterState, stateErr := os.ReadFile(statePath)
	afterJournal, journalErr := os.ReadFile(journalPath)
	if stateErr != nil || journalErr != nil || !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("rejected actor changed Runtime or journal")
	}
}
