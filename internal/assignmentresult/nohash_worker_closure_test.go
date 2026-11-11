package assignmentresult

import "testing"

func TestWorkerClosureUsesActualResponsibilityAndConclusionsWithoutDigests(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact := WorkerCheckFact{Index: 0, Command: "true", Status: "pass", Record: record.Reference(), RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID}
	result := AcceptedResult{WorkerCheckFacts: []WorkerCheckFact{fact}}
	if !WorkerClosureComplete(record, result) {
		t.Fatal("actual passing responsibility was rejected for missing digest")
	}
	result.WorkerCheckFacts[0].OwnerAgentID = "other-agent"
	if WorkerClosureComplete(record, result) {
		t.Fatal("another worker's result was accepted")
	}
	result.WorkerCheckFacts[0].OwnerAgentID = record.OwnerAgentID
	result.WorkerCheckFacts[0].Status = "fail"
	if WorkerClosureComplete(record, result) {
		t.Fatal("actual failed check was treated as passing")
	}
}
