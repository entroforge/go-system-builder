package assignmentresult

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkerCheckClosureUsesOriginalResponsibilityWithoutDigest(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact := WorkerCheckFact{Index: 0, Command: "true", Status: "pass", ExecutionKind: "workspace", Record: record.Reference(), RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID, ExecutionGeneration: 1}
	result := AcceptedResult{WorkerCheckFacts: []WorkerCheckFact{fact}}
	if err := ValidateWorkerClosure(record, result, nil); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "digest") || strings.Contains(string(data), "sha256") {
		t.Fatalf("machine digest projected: %s", data)
	}
	wrong := result
	wrong.WorkerCheckFacts = append([]WorkerCheckFact(nil), result.WorkerCheckFacts...)
	wrong.WorkerCheckFacts[0].OwnerAgentID = "other"
	if err := ValidateWorkerClosure(record, wrong, nil); err == nil {
		t.Fatal("wrong original owner accepted")
	}
	wrong.WorkerCheckFacts[0] = fact
	wrong.WorkerCheckFacts[0].Command = "false"
	if err := ValidateWorkerClosure(record, wrong, nil); err == nil {
		t.Fatal("different original check accepted")
	}
	result.WorkerCheckFacts = nil
	if err := ValidateWorkerClosure(record, result, nil); err != nil {
		t.Fatalf("unknown check facts became an answer gate: %v", err)
	}
}
