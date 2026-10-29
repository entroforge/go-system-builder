package workpacket

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestContractProjectionFreezesInputsAndRequiresExplicitApplicability(t *testing.T) {
	binding := PlanningBinding{RuntimeID: "loop-contract", Generation: 1, StageEntry: 3, Stage: "S3", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1.0.0", SHA256: Digest([]byte("REQ"))}}
	inputs := ContractInputs{Designs: []Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1.0.0", SHA256: Digest([]byte("design"))}}, Sources: []Subject{{Path: "docs/design/prototypes/orders/cases.json", SHA256: Digest([]byte("cases"))}}, Directories: map[string][]string{"docs/design/prototypes/orders": {"stories.md", "cases.json"}}}
	p, err := BuildContracts(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildContracts(binding, inputs)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("unstable projection", err)
	}
	inputs.Directories["docs/design/prototypes/orders"][0] = "changed.md"
	inputs.Designs[0].SHA256 = Digest([]byte("changed"))
	if p.Inputs.Designs[0].SHA256 != Digest([]byte("design")) || p.Inputs.Directories["docs/design/prototypes/orders"][1] != "stories.md" {
		t.Fatal("caller mutated frozen inputs")
	}
	data, _ := json.Marshal(p.Draft)
	if _, issues := ValidateContractAnswer(p, data); len(issues) == 0 {
		t.Fatal("draft automatically passed")
	}
	pass, index := "pass", "docs/dev/contracts/CONTRACTS-001.md"
	draft := p.Draft
	draft.Answer = ContractAnswer{Conclusion: &pass, IndexPath: &index, ContractPaths: []string{"docs/dev/contracts/BE-001.md"}, Applicability: map[string]ContractApplicability{"FE": {Disposition: "not_applicable", Rationale: "The requirement exposes no client surface."}, "BE": {Disposition: "required", Rationale: "The requirement defines the service behavior."}, "SYNC": {Disposition: "not_applicable", Rationale: "No separate client/provider integration is in this requirement."}}}
	data, _ = json.Marshal(draft)
	answer, issues := ValidateContractAnswer(p, data)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	t.Run("large complete delivery and empty adaptation gap", func(t *testing.T) {
		large := p.Draft
		large.Answer = answer
		large.Answer.ContractPaths = nil
		for i := 1; i <= 96; i++ {
			large.Answer.ContractPaths = append(large.Answer.ContractPaths, fmt.Sprintf("docs/dev/contracts/BE-%03d.md", i))
		}
		encoded, _ := json.Marshal(large)
		if _, issues := ValidateContractAnswer(p, encoded); len(issues) != 0 {
			t.Fatalf("legal large delivery rejected: %v", issues)
		}
		large.Answer.ContractPaths = []string{}
		encoded, _ = json.Marshal(large)
		if _, issues := ValidateContractAnswer(p, encoded); len(issues) == 0 || issues[0].Pointer != "/answer/contract_paths" || !strings.Contains(issues[0].Expected, "adaptation gap") {
			t.Fatalf("empty delivery lacks adaptation diagnostic: %v", issues)
		}
	})
	if answer.Applicability["BE"].Disposition != "required" {
		t.Fatal("professional applicability changed")
	}
	for _, kind := range []string{"machine_field", "duplicate_contract", "scope_mismatch", "missing_rationale", "unexpected_family"} {
		t.Run(kind, func(t *testing.T) {
			var changed ContractDraft
			json.Unmarshal(data, &changed)
			switch kind {
			case "machine_field":
				changed.Instruction = "changed"
			case "duplicate_contract":
				changed.Answer.ContractPaths = append(changed.Answer.ContractPaths, changed.Answer.ContractPaths[0])
			case "scope_mismatch":
				changed.Answer.Applicability["FE"] = ContractApplicability{Disposition: "required", Rationale: "missing FE"}
			case "missing_rationale":
				changed.Answer.Applicability["SYNC"] = ContractApplicability{Disposition: "not_applicable"}
			case "unexpected_family":
				changed.Answer.Applicability["OTHER"] = ContractApplicability{Disposition: "not_applicable", Rationale: "extra"}
			}
			bad, _ := json.Marshal(changed)
			if _, issues := ValidateContractAnswer(p, bad); len(issues) == 0 {
				t.Fatal("invalid professional answer accepted")
			}
		})
	}
	changed, err := BuildContracts(binding, inputs)
	if err != nil || changed.ID == p.ID {
		t.Fatal("changed inputs reused old packet", err)
	}
}

func TestContractProjectionCanonicalOrderAndInvalidSourceManifest(t *testing.T) {
	b := PlanningBinding{RuntimeID: "loop-contract", Generation: 1, StageEntry: 0, Stage: "S3", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1", SHA256: Digest([]byte("req"))}}
	a := Reference{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1", SHA256: Digest([]byte("a"))}
	z := a
	z.ID = "ARCHITECTURE-002"
	z.Path = "docs/architecture/ARCHITECTURE-002.md"
	p, err := BuildContracts(b, ContractInputs{Designs: []Reference{a, z}})
	if err != nil {
		t.Fatal(err)
	}
	q, err := BuildContracts(b, ContractInputs{Designs: []Reference{z, a}})
	if err != nil || p.ID != q.ID {
		t.Fatal("source discovery order changed identity", err)
	}
	for _, names := range [][]string{{"../escape"}, {"same", "same"}, {".."}} {
		if _, err := BuildContracts(b, ContractInputs{Designs: []Reference{a}, Directories: map[string][]string{"docs/design": names}}); err == nil {
			t.Fatal("invalid manifest accepted", names)
		}
	}
	b.Owner = "contract-planner-1"
	if _, err := BuildContracts(b, ContractInputs{Designs: []Reference{a}}); err == nil {
		t.Fatal("fabricated professional agent binding")
	}
}

func TestContractQuestionDeclaresApplicabilityObjectShape(t *testing.T) {
	b := PlanningBinding{RuntimeID: "loop-contract", Generation: 1, StageEntry: 0, Stage: "S3", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1", SHA256: Digest([]byte("req"))}}
	p, err := BuildContracts(b, ContractInputs{Designs: []Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1", SHA256: Digest([]byte("design"))}}})
	if err != nil {
		t.Fatal(err)
	}
	question := p.Draft.Questions["applicability"].Question
	for _, field := range []string{"answer.applicability", "FE", "BE", "SYNC", "object", "disposition", "rationale", "required", "not_applicable"} {
		if !strings.Contains(question, field) {
			t.Fatalf("issued question omits %s: %s", field, question)
		}
	}
	if len(p.Draft.Answer.Applicability) != 0 || p.Draft.Answer.Conclusion != nil {
		t.Fatal("shape guidance prefilled professional judgments")
	}
}
