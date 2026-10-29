package workpacket

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestContractDraftPathsDoNotGrantReception(t *testing.T) {
	binding := PlanningBinding{RuntimeID: "loop-draft", Generation: 1, StageEntry: 3, Stage: "S3", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1", SHA256: Digest([]byte("req"))}}
	p, err := BuildContracts(binding, ContractInputs{Designs: []Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1", SHA256: Digest([]byte("design"))}}})
	if err != nil {
		t.Fatal(err)
	}
	index, contract := "docs/dev/contracts/CONTRACTS-SERVICE-009.md", "docs/dev/contracts/BE-SERVICE-009.md"
	draft := p.Draft
	draft.Answer.IndexPath, draft.Answer.ContractPaths = &index, []string{contract}
	for _, incomplete := range []string{"conclusion", "applicability"} {
		pass := "pass"
		draft.Answer.Conclusion = &pass
		if incomplete == "conclusion" {
			draft.Answer.Conclusion = nil
		}
		data, _ := json.Marshal(draft)
		paths, named := ContractDraftDeliveryPaths(p, data)
		if !named || !reflect.DeepEqual(paths, []string{index, contract}) {
			t.Fatalf("incomplete %s lost paths: %v %v", incomplete, named, paths)
		}
		if _, issues := ValidateContractAnswer(p, data); len(issues) == 0 {
			t.Fatalf("incomplete %s accepted", incomplete)
		}
	}
	pass := "pass"
	draft.Answer.Conclusion = &pass
	draft.Answer.Applicability = map[string]ContractApplicability{"FE": {Disposition: "not_applicable", Rationale: "No client"}, "BE": {Disposition: "required", Rationale: "Service contract"}, "SYNC": {Disposition: "not_applicable", Rationale: "No separate integration"}}
	for _, bad := range []string{"/tmp/BE-SERVICE-009.md", "docs/dev/contracts/../contracts/BE-SERVICE-009.md", "docs/dev/contracts/BE-contract-template.md", contract} {
		draft.Answer.ContractPaths = []string{contract, bad}
		data, _ := json.Marshal(draft)
		paths, named := ContractDraftDeliveryPaths(p, data)
		if !named || !reflect.DeepEqual(paths, []string{index, contract}) {
			t.Fatalf("unsafe/duplicate draft path projected: %v", paths)
		}
		if _, issues := ValidateContractAnswer(p, data); len(issues) == 0 || issues[0].Pointer != "/answer/contract_paths/1" || !strings.Contains(issues[0].Observed, bad) {
			t.Fatalf("invalid path not diagnosed: %+v", issues)
		}
	}
	draft.Answer.ContractPaths = []string{contract}
	data, _ := json.Marshal(draft)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	raw["answer"].(map[string]any)["unknown_contract_scope"] = "invalid"
	data, _ = json.Marshal(raw)
	if _, issues := ValidateContractAnswer(p, data); len(issues) == 0 || issues[0].Pointer != "/answer/unknown_contract_scope" || !strings.Contains(issues[0].Observed, "unknown_contract_scope") || issues[0].Repair == "" {
		t.Fatalf("schema error lost cause: %+v", issues)
	}
	// Reject duplicate JSON keys and preserve the size bound even for display.
	if paths, named := ContractDraftDeliveryPaths(p, []byte(`{"answer":{},"answer":{}}`)); named || len(paths) > 0 {
		t.Fatal("ambiguous JSON projected")
	}
}
