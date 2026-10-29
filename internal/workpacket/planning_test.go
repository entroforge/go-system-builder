package workpacket

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDesignProjectionFrozenMainAndProfessionalAnswer(t *testing.T) {
	binding := PlanningBinding{RuntimeID: "loop-design", Generation: 1, StageEntry: 2, Stage: "S2", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1.0.0", SHA256: Digest([]byte("frozen requirement"))}}
	p, err := BuildDesign(binding)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildDesign(binding)
	if err != nil || again.ID != p.ID {
		t.Fatal("unstable issuance projection", again, err)
	}
	if !strings.Contains(p.AnswerPath, "/Main/answer.json") || p.Draft.Answer.Conclusion != nil || p.Draft.Answer.ArchitecturePath != nil {
		t.Fatal("projection fabricated a professional pass", p)
	}
	data, _ := json.Marshal(p.Draft)
	if answer, issues := ValidateDesignAnswer(p, data); len(issues) != 0 || answer.Conclusion != nil {
		t.Fatal("draft must remain readable without inventing a passing answer", issues)
	}
	pass, path := "pass", "docs/architecture/ARCHITECTURE-001.md"
	draft := p.Draft
	draft.Answer = DesignAnswer{Conclusion: &pass, ArchitecturePath: &path}
	data, _ = json.Marshal(draft)
	answer, issues := ValidateDesignAnswer(p, data)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	changed := draft
	changed.Title = "tampered"
	bad, _ := json.Marshal(changed)
	if _, issues := ValidateDesignAnswer(p, bad); len(issues) != 0 {
		t.Fatal("presentation metadata became a professional admission requirement", issues)
	}
	if _, issues := ValidateDesignAnswer(p, []byte(`{"answer":{"conclusion":123,"architecture_path":"docs/architecture/ARCHITECTURE-001.md"}}`)); len(issues) == 0 {
		t.Fatal("malformed professional field type was accepted")
	}
	arch := Reference{ID: "ARCHITECTURE-001", Path: path, Version: "v1.0.0", SHA256: Digest([]byte("actual locked architecture"))}
	evidence, err := DesignEvidence(p, "ev-design", arch, answer, time.Now().UTC())
	if err != nil || evidence["producer_agent_id"] != "Main" || evidence["producer_responsibility"] != "Orchestrator" || evidence["kind"] != "planning_design" {
		t.Fatal(evidence, err)
	}
	subjects := evidence["subject_refs"].([]Subject)
	if len(subjects) != 2 || subjects[0].SHA256 != binding.Requirement.SHA256 || subjects[1].SHA256 != arch.SHA256 {
		t.Fatal("evidence has guessed subjects", subjects)
	}
	arch.Path = "docs/architecture/other.md"
	if _, err := DesignEvidence(p, "ev-design", arch, answer, time.Now()); err == nil {
		t.Fatal("different actual architecture accepted")
	}
	binding.Owner = "architect-1"
	if _, err := BuildDesign(binding); err == nil {
		t.Fatal("Main projection fabricated dispatched identity")
	}
}
