package investigation_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// This fixture advances the Runtime cursor and generation through its Writer;
// it deliberately does not represent or assert a contract approval.
func TestLateS8WorkerPathSurvivesStageAndGenerationChange(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	worker := registerS8WorkerExecution(t, f, currentS8Snapshot(t, f), owner)
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original issue: %d %v", len(packets), err)
	}
	packet := packets[0]
	answer := writeS8ProfessionalAnswer(t, worker, packet, "Late worker observation survives stage change")
	privatePath := filepath.Join(worker, "personal-notes.txt")
	if err := os.WriteFile(privatePath, []byte("owned by investigator"), 0600); err != nil {
		t.Fatal(err)
	}
	before := currentS8Snapshot(t, f)
	life := before.State["lifecycle"].(map[string]any)
	from := map[string]any{"state": life["state"], "phase": life["phase"]}
	to := map[string]any{"state": "bug_resolution", "phase": "fixing"}
	writer := runtime.NewWriter(f.statePath, f.journalPath, f.root, semantic.RuntimeCandidateValidator{})
	_, err = writer.Update(before.Revision, runtime.Mutation{EventID: "evt-test-stage-generation", TransitionID: "TEST-STAGE-GENERATION", Event: "test_stage_generation", Actor: "orchestrator", IdempotencyKey: "test:s8-stage-generation", RuntimeID: packet.Binding.RuntimeID, BaselineGeneration: packet.Binding.BaselineGeneration, From: from, To: to, RetainLastTransition: true, Apply: func(state map[string]any) error {
		state["baseline"].(map[string]any)["generation"] = 3
		state["lifecycle"].(map[string]any)["phase"] = "fixing"
		return nil
	}})
	if err != nil {
		t.Fatalf("advance test cursor: %v", err)
	}
	service := &submission.Service{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath}
	receipt, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, filepath.Join(worker, answer), "", nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original worker late result: %+v %v", receipt, err)
	}
	after := currentS8Snapshot(t, f)
	selected, found, err := runtime.FindHypothesisResultPacket(after.State, packet.ID)
	if err != nil || !found || len(selected.LateResults) != 1 || !bytes.Contains(selected.LateResults[0].Answer, []byte("Late worker observation")) {
		t.Fatalf("lost original late result: %+v %v", selected.LateResults, err)
	}
	if string(mustRead(t, privatePath)) != "owned by investigator" {
		t.Fatal("private worker file changed")
	}
	if after.State["lifecycle"].(map[string]any)["phase"] != "fixing" || after.State["baseline"].(map[string]any)["generation"].(float64) != 3 {
		t.Fatal("late receive moved current stage or generation")
	}
	_, err = service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, filepath.Join(worker, answer), "", nil)
	if err != nil || currentS8Snapshot(t, f).Revision != after.Revision {
		t.Fatalf("late worker replay: %v", err)
	}
}
