package workpacket

import (
	"testing"
)

func TestPlanningPacketIdentityUsesStageResponsibility(t *testing.T) {
	binding := PlanningBinding{RuntimeID: "loop-nohash", Generation: 1, StageEntry: 3, Stage: "S2", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1"}}
	first, err := BuildDesign(binding)
	if err != nil {
		t.Fatal(err)
	}
	legacy := binding
	second, err := BuildDesign(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("historic digest affected current issuance: %#v %#v", first, second)
	}
	next := binding
	next.StageEntry++
	third, err := BuildDesign(next)
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID {
		t.Fatal("different stage entries share an identity")
	}
	binding.Stage = "S3"
	contracts, err := BuildContracts(binding, ContractInputs{Designs: []Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if contracts.ID == first.ID {
		t.Fatalf("invalid S3 identity: %#v", contracts)
	}
	binding.Stage = "S4"
	task, err := BuildTaskPlanning(binding, TaskPlanningInputs{Contracts: []Reference{{ID: "CONTRACTS-001", Path: "docs/dev/contracts/CONTRACTS-001.md", Version: "v1"}, {ID: "FE-001", Path: "docs/dev/contracts/FE-001.md", Version: "v1"}}, ClauseUniverse: []TaskClause{{ContractID: "FE-001", Section: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == contracts.ID {
		t.Fatalf("invalid S4 identity: %#v", task)
	}
}
