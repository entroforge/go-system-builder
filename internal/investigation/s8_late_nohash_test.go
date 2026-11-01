package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestLateS8OriginalAnswerDoesNotOverwriteCurrentCase(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v (%d)", err, len(packets))
	}
	packet := packets[0]
	answer := writeS8ProfessionalAnswer(t, f.root, packet, "Late original observation")
	snapshot := currentS8Snapshot(t, f)
	old := investigationPointer(t, f)
	newCase := readCaseDocument(t, f.root, old["path"].(string))
	newCase["case_id"] = "investigation-case-new"
	newCase["revision"] = 1
	newRef := ".claude/review/investigation/cases/investigation-case-new-r1.json"
	writeInvestigationJSON(t, filepath.Join(f.root, newRef), newCase)
	life := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	writer := loopruntime.NewWriter(f.statePath, f.journalPath, f.root, semantic.RuntimeCandidateValidator{})
	next, err := writer.Update(snapshot.Revision, loopruntime.Mutation{
		EventID: "evt-test-new-s8-case", TransitionID: "TEST-S8-NEW-CASE", Event: "test_new_case", Actor: "orchestrator", IdempotencyKey: "test-new-s8-case", RuntimeID: snapshot.State["runtime_id"].(string), BaselineGeneration: 2, From: cursor, To: cursor, RetainLastTransition: true,
		Apply: func(state map[string]any) error {
			p := state["review"].(map[string]any)["investigation"].(map[string]any)
			p["case_id"], p["revision"], p["path"] = "investigation-case-new", 1, newRef
			delete(p, "sha256")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedCase, _ := json.Marshal(next.State["review"].(map[string]any)["investigation"])
	service := &submission.Service{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath}
	receipt, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, filepath.Join(f.root, answer), "", nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("late original answer: %+v %v", receipt, err)
	}
	got := currentS8Snapshot(t, f)
	gotCase, _ := json.Marshal(got.State["review"].(map[string]any)["investigation"])
	if !bytes.Equal(expectedCase, gotCase) {
		t.Fatalf("late answer changed current Case: %s -> %s", expectedCase, gotCase)
	}
	p, found, err := loopruntime.FindHypothesisResultPacket(got.State, packet.ID)
	if err != nil || !found || len(p.LateResults) != 1 {
		t.Fatalf("late responsibility not retained: %+v %v", p.LateResults, err)
	}
	recorded := p.LateResults[0].Answer
	if bytes.Contains(recorded, []byte("sha256")) {
		t.Fatalf("machine digest in accepted answer: %s", recorded)
	}
	before := got.Revision
	receipt, err = service.HypothesisResultReceipt(owner.ownerID, packet.ID)
	if err != nil || !receipt.Accepted || currentS8Snapshot(t, f).Revision != before {
		t.Fatalf("late replay: %+v %v", receipt, err)
	}
}
