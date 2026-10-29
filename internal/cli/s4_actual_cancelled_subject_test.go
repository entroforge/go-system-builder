package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestTaskPlanningSaveKeepsActualCancelledTaskWithoutAuthorListProof(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	cancelled := "docs/dev/tasks/TASK-039-02.md"
	writeContractSource(t, root, cancelled, "# Task\n> Status: cancelled\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n")
	fixtures.CommitCurrentFixture(t, root)
	draft := completedTaskPlanningDraft(p)
	stageJSON(t, root, p.AnswerPath, draft)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("actual cancelled task was made an author-list qualification: %+v %v", receipt, err)
	}
	snap := stageSnapshot(t, root)
	found := false
	for _, raw := range snap.State["evidence"].([]any) {
		ev := raw.(map[string]any)
		if ev["kind"] != "planning_task" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, ev["path"].(string)))
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Subjects []workpacket.Subject          `json:"subject_refs"`
			Answer   workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Answer.TaskPaths) != 1 || envelope.Answer.TaskPaths[0] != draft.Answer.TaskPaths[0] {
			t.Fatalf("professional answer was filled by system: %+v", envelope.Answer)
		}
		for _, subject := range envelope.Subjects {
			if subject.Path == cancelled {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("actual cancelled task fact lost from received record")
	}
	if snap.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("S4 did not reach S5", receipt)
	}
	formal, err := runtime.S5FormalInputs(root, snap.State, fileview.Disk{Root: root})
	if err != nil {
		t.Fatalf("S4 accepted the actual batch but S5 demanded another author list: %v", err)
	}
	activeFound := false
	for _, ref := range formal.References {
		if ref.Path == cancelled {
			t.Fatal("cancelled TASK entered the active S5 subject denominator")
		}
		if ref.Path == draft.Answer.TaskPaths[0] {
			activeFound = true
		}
	}
	if !activeFound {
		t.Fatal("the active TASK disappeared from S5")
	}
	original, err := os.ReadFile(filepath.Join(root, cancelled))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, cancelled), append(original, []byte("changed cancellation source\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.S5FormalInputs(root, snap.State, fileview.Disk{Root: root}); err == nil {
		t.Fatal("cancelled history source drift was silently accepted")
	}
	if err := os.WriteFile(filepath.Join(root, cancelled), original, 0600); err != nil {
		t.Fatal(err)
	}
	packets := registerAcceptedS5Reviewers(t, root, "TASK-039-01", draft.Answer.TaskPaths[0])
	binary := factory.BuildHarness(t)
	for _, packet := range packets {
		stageJSON(t, root, packet.AnswerPath, completedDraft(packet, "pass"))
		if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "actual-native-reviewer", filepath.Join(root, packet.ViewPath), binary); code != 0 {
			t.Fatal(diagnostic)
		} else {
			t.Logf("S5 native reception %s: %s", packet.ID, diagnostic)
		}
		settleOriginalS5Followup(t, root, packet)
	}
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatal("original S5 pair did not advance the active batch to S6")
	}

}
