package assignment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestCompleteTaskWorkerClosureUnavailableIsDurableAndAliasReplays(t *testing.T) {
	f, statePath, journalPath, record, _ := registerAgentBeginConfiguredFixture(t, true, func(f *recordRegistrationFixture) {
		configureRegistrationClosing(t, f, assignmentrecord.ClosingContractVersion, "post_merge", "main", true)
	})
	plan := writeThinS6Plan(t, f, record, "msg-worker-closure-plan", "corr-worker-closure-plan")
	if _, _, err := AgentBegin(f.root, statePath, journalPath, AgentBeginRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: plan}); err != nil {
		t.Fatal(err)
	}
	source := "src/pkg/result-proof.txt"
	writeRegistrationFile(t, f.root, source, []byte("professional implementation proof\n"))
	message := canonicalResultMessage(t, f, record, "msg-worker-closure-first", "Implemented the assigned behavior", []string{source})
	conditions := make([]any, len(record.DoneWhen))
	for i := range conditions {
		conditions[i] = map[string]any{"done_when_index": i, "judgment": "satisfied", "plan_assertion_indexes": []int{0}, "rationale": "The professional evidence supports this condition."}
	}
	message["closing_assessment"] = map[string]any{"disposition": "complete", "conditions": conditions}
	firstPath := writeRegistrationMessage(t, f.root, "worker-closure-first.json", message)
	first, receipt, err := CompleteTaskWithReceipt(f.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: firstPath})
	if err != nil {
		t.Fatal(err)
	}
	read := func(ref string) ([]byte, error) { return workpacket.ReadFile(f.root, ref, canonicalResultAssetLimit) }
	accepted, err := assignmentresult.LoadAccepted(first.State, record, receipt.ResultRef, read)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted.WorkerCheckFacts) != 1 || accepted.WorkerCheckFacts[0].Status != "unavailable" || accepted.ToolClosureDigest == "" || assignmentresult.WorkerClosureComplete(record, accepted) {
		t.Fatalf("unavailable closure must remain unproven: %+v", accepted)
	}
	message["message_id"] = "msg-worker-closure-alias"
	aliasPath := writeRegistrationMessage(t, f.root, "worker-closure-alias.json", message)
	aliased, aliasReceipt, err := CompleteTaskWithReceipt(f.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath})
	if err != nil {
		t.Fatal(err)
	}
	if aliased.Revision != first.Revision+1 || aliasReceipt.ResultRef != receipt.ResultRef || !aliasReceipt.Replayed {
		t.Fatalf("composite alias = %+v at %d", aliasReceipt, aliased.Revision)
	}
	beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
	// Registered keys use immutable facts even after the live professional
	// evidence disappears, and after reopening the original Runtime Writer.
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(source))); err != nil {
		t.Fatal(err)
	}
	replayed, replayReceipt, err := CompleteTaskWithReceipt(f.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != aliased.Revision || replayReceipt.ResultRef != receipt.ResultRef || !replayReceipt.Replayed {
		t.Fatalf("alias replay = %+v", replayReceipt)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
	data, err := read(receipt.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	var persisted assignmentresult.AcceptedResult
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.ToolClosureDigest != accepted.ToolClosureDigest {
		t.Fatalf("durable closure decode: %v", err)
	}
}
