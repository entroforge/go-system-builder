package workpacket

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestContractEvidencePreservesAnswerAndExactSubjectClosure(t *testing.T) {
	pass := "pass"
	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	fePath := "docs/dev/contracts/FE-001.md"
	bePath := "docs/dev/contracts/BE-001.md"
	p, answer := contractEvidenceFixture(t, &pass, indexPath, []string{fePath, bePath})
	artifacts := []Reference{
		{Path: bePath, Version: "v1.0.0", SHA256: Digest([]byte("be"))},
		{Path: indexPath, Version: "v1.0.0", SHA256: Digest([]byte("index"))},
		{Path: fePath, Version: "v2.0.0", SHA256: Digest([]byte("fe"))},
	}
	at := time.Date(2026, 10, 14, 8, 9, 10, 123, time.FixedZone("local", 3600))
	evidence, err := ContractEvidence(p, "evidence-1", artifacts, answer, at)
	if err != nil {
		t.Fatal(err)
	}
	if evidence["kind"] != "planning_contract" || evidence["producer_agent_id"] != "Main" || evidence["producer_responsibility"] != "Orchestrator" || evidence["conclusion"] != "pass" {
		t.Fatalf("envelope identity/judgment = %#v", evidence)
	}
	if evidence["created_at"] != at.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("created_at = %v", evidence["created_at"])
	}
	canonicalAnswer, ok := evidence["contract_answer"].(ContractAnswer)
	if !ok || canonicalAnswer.IndexPath == nil || *canonicalAnswer.IndexPath != indexPath || !reflect.DeepEqual(canonicalAnswer.ContractPaths, []string{bePath, fePath}) {
		t.Fatalf("canonical stored answer = %#v", evidence["contract_answer"])
	}
	if canonicalAnswer.Applicability["FE"].Rationale != answer.Applicability["FE"].Rationale || canonicalAnswer.Applicability["SYNC"].Disposition != "not_applicable" {
		t.Fatalf("professional applicability lost: %#v", canonicalAnswer.Applicability)
	}
	subjects, ok := evidence["subject_refs"].([]Subject)
	if !ok {
		t.Fatalf("subject_refs type = %T", evidence["subject_refs"])
	}
	want := []Subject{
		{Path: "docs/requirements/REQ-001.md", Version: "v1.0.0", SHA256: p.Binding.Requirement.SHA256},
		{Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1.0.0", SHA256: p.Inputs.Designs[0].SHA256},
		{Path: "docs/design/prototypes/orders/scenario-model.json", Version: "v2", SHA256: p.Inputs.Sources[0].SHA256},
		{Path: indexPath, Version: "v1.0.0", SHA256: artifacts[1].SHA256},
		{Path: bePath, Version: "v1.0.0", SHA256: artifacts[0].SHA256},
		{Path: fePath, Version: "v2.0.0", SHA256: artifacts[2].SHA256},
	}
	if !reflect.DeepEqual(subjects, want) {
		t.Fatalf("subject_refs = %#v, want %#v", subjects, want)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ContractAnswer ContractAnswer `json:"contract_answer"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.ContractAnswer, canonicalAnswer) {
		t.Fatalf("serialized contract_answer changed: %#v", roundTrip["contract_answer"])
	}
}

func TestContractEvidenceRejectsUnansweredOrIncompleteArtifactSet(t *testing.T) {
	pass := "pass"
	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	bePath := "docs/dev/contracts/BE-001.md"
	p, answer := contractEvidenceFixture(t, &pass, indexPath, []string{bePath})
	if _, err := ContractEvidence(p, "evidence-1", []Reference{{Path: indexPath, Version: "v1.0.0", SHA256: Digest([]byte("i"))}}, answer, time.Now()); err == nil {
		t.Fatal("incomplete artifact set accepted")
	}
	noPass := "draft"
	answer.Conclusion = &noPass
	if _, err := ContractEvidence(p, "evidence-1", []Reference{
		{Path: indexPath, Version: "v1.0.0", SHA256: Digest([]byte("i"))},
		{Path: bePath, Version: "v1.0.0", SHA256: Digest([]byte("b"))},
	}, answer, time.Now()); err == nil {
		t.Fatal("non-pass professional answer accepted")
	}
}

func TestContractEvidenceIncludesSHAOnlySharedModelClosure(t *testing.T) {
	pass := "pass"
	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	bePath := "docs/dev/contracts/BE-001.md"
	p, answer := contractEvidenceFixture(t, &pass, indexPath, []string{bePath})
	modelPaths := []string{
		"docs/architecture/data-model/order.schema.json",
		"docs/architecture/data-model/order-valid.json",
		"docs/architecture/data-model/order-invalid.json",
	}
	artifacts := []Reference{
		{Path: indexPath, Version: "v1.0.0", SHA256: Digest([]byte("index"))},
		{Path: bePath, Version: "v1.0.0", SHA256: Digest([]byte("contract"))},
	}
	for _, path := range modelPaths {
		artifacts = append(artifacts, Reference{Path: path, SHA256: Digest([]byte(path))})
	}
	evidence, err := ContractEvidence(p, "evidence-shared-model", artifacts, answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	subjects := evidence["subject_refs"].([]Subject)
	for _, path := range modelPaths {
		found := false
		for _, subject := range subjects {
			if subject.Path == path && subject.Version == "" && subject.SHA256 == Digest([]byte(path)) {
				found = true
			}
		}
		if !found {
			t.Errorf("SHA-only model subject missing: %s", path)
		}
	}
	if _, err := ContractEvidence(p, "evidence-bad-model-ref", append(append([]Reference(nil), artifacts...), Reference{Path: ".claude/hidden.json", SHA256: Digest([]byte("hidden"))}), answer, time.Now().UTC()); err == nil {
		t.Fatal("private runtime file accepted as shared-model evidence")
	}
	badVersion := append([]Reference(nil), artifacts...)
	badVersion[len(badVersion)-1].Version = "invented-version"
	if _, err := ContractEvidence(p, "evidence-bad-model-version", badVersion, answer, time.Now().UTC()); err == nil {
		t.Fatal("fabricated model version accepted")
	}
}

func contractEvidenceFixture(t *testing.T, conclusion *string, indexPath string, contractPaths []string) (ContractPacket, ContractAnswer) {
	t.Helper()
	binding := PlanningBinding{
		RuntimeID: "runtime-contract", Generation: 1, StageEntry: 3, Stage: "S3", Owner: "Main",
		Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1.0.0", SHA256: Digest([]byte("req"))},
	}
	inputs := ContractInputs{
		Designs: []Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1.0.0", SHA256: Digest([]byte("arch"))}},
		Sources: []Subject{{Path: "docs/design/prototypes/orders/scenario-model.json", Version: "v2", SHA256: Digest([]byte("scenario"))}},
	}
	p, err := BuildContracts(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	applicability := map[string]ContractApplicability{
		"FE":   {Disposition: "not_applicable", Rationale: "No user facing client behavior exists."},
		"BE":   {Disposition: "required", Rationale: "The service owns the changed behavior."},
		"SYNC": {Disposition: "not_applicable", Rationale: "No cross-client protocol change exists."},
	}
	if hasFamily(contractPaths, "FE") {
		applicability["FE"] = ContractApplicability{Disposition: "required", Rationale: "Client behavior changes."}
	}
	if hasFamily(contractPaths, "SYNC") {
		applicability["SYNC"] = ContractApplicability{Disposition: "required", Rationale: "Client and service protocol changes."}
	}
	answer := ContractAnswer{Conclusion: conclusion, IndexPath: &indexPath, ContractPaths: append([]string(nil), contractPaths...), Applicability: applicability}
	return p, answer
}

func hasFamily(paths []string, family string) bool {
	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), family+"-") {
			return true
		}
	}
	return false
}
