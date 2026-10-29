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

func TestTR006BatchProofRemainsInsideOriginalWriter(t *testing.T) {
	for _, scenario := range []string{"valid", "missing-completion", "completion-drifts-before-commit", "wrong-actor"} {
		t.Run(scenario, func(t *testing.T) {
			root := req039fixtures.FreshRoot(t)
			state := req039fixtures.BaseState(t, root, "building", "", 0)
			req039fixtures.SeedBuilderBatchReady(t, root, state)
			var completionPath string
			for _, raw := range state["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["id"] == "ev-completion-2" {
					completionPath = filepath.Join(root, row["path"].(string))
				}
			}
			if completionPath == "" {
				t.Fatal("fixture has no second TASK completion artifact")
			}
			if scenario == "missing-completion" {
				if err := os.Remove(completionPath); err != nil {
					t.Fatal(err)
				}
			}
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
			files := tr006BatchBoundaryDisk{Disk: fileview.Disk{Root: root}, verify: func() error {
				verified = true // Apply calls Verify under the original Writer lock.
				if scenario == "completion-drifts-before-commit" {
					return os.WriteFile(completionPath, []byte("changed completion bytes"), 0600)
				}
				return nil
			}}
			actor := "orchestrator"
			if scenario == "wrong-actor" {
				actor = "not-an-allowed-actor"
			}
			snapshot, applyErr := transition.Apply(root, statePath, journalPath, transition.Request{
				Files: files, TransitionID: "TR-006", ExpectedRevision: 0,
				ExpectedRuntimeID: req039fixtures.RuntimeIDFromState(state), Actor: actor,
				Evidence: map[string]string{"builder_report_record": "ev-team"},
			})
			if scenario == "valid" {
				if applyErr != nil || !verified || snapshot.State["lifecycle"].(map[string]any)["state"] != "verification" {
					t.Fatalf("valid batch did not pass the original transaction: verified=%t err=%v", verified, applyErr)
				}
				return
			}
			if scenario == "wrong-actor" {
				if applyErr == nil || !strings.Contains(applyErr.Error(), "cannot execute") || verified {
					t.Fatalf("preflight actor check changed: verified=%t err=%v", verified, applyErr)
				}
			} else if scenario == "missing-completion" {
				if applyErr == nil || !verified || !strings.Contains(applyErr.Error(), "evidence:ev-completion-2:unreadable") {
					t.Fatalf("missing completion escaped locked evidence proof: verified=%t err=%v", verified, applyErr)
				}
			} else if applyErr == nil || !verified || !strings.Contains(applyErr.Error(), "TASK-039-02") {
				t.Fatalf("drifted batch escaped locked proof: verified=%t err=%v", verified, applyErr)
			}
			afterState, stateErr := os.ReadFile(statePath)
			afterJournal, journalErr := os.ReadFile(journalPath)
			if stateErr != nil || journalErr != nil || !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
				t.Fatal("rejected TR-006 changed Runtime or journal")
			}
		})
	}
}
