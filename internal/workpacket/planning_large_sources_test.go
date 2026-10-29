package workpacket

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestPlanningPacketsCarryLargeInheritedSourceSets(t *testing.T) {
	binding := PlanningBinding{RuntimeID: "loop-contract", Generation: 1, StageEntry: 3, Stage: "S3", Owner: "Main", Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1", SHA256: Digest([]byte("REQ"))}}
	inputs := ContractInputs{Designs: []Reference{{ID: "ARCHITECTURE-001", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1", SHA256: Digest([]byte("design"))}}, Directories: map[string][]string{}}
	for i := 0; i < 5000; i++ {
		name := fmt.Sprintf("%s-%04d.md", strings.Repeat("source", 20), i)
		inputs.Sources = append(inputs.Sources, Subject{Path: "docs/design/prototypes/existing/" + name, SHA256: Digest([]byte(name))})
		inputs.Directories["docs/design/prototypes/existing"] = append(inputs.Directories["docs/design/prototypes/existing"], name)
	}
	for i := 0; i < 70; i++ {
		inputs.Directories[fmt.Sprintf("docs/design/prototypes/module-%03d", i)] = []string{}
	}
	p, err := BuildContracts(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := json.MarshalIndent(p, "", "  ")
	if len(view) <= 1<<20 {
		t.Fatal("fixture must exercise a machine packet beyond the old publication limit")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("contract-work-packet.schema.json", view); err != nil {
		t.Fatal(err)
	}
	assertLargePlanningPacketFitsRuntimeSchemas(t, p)
	root := t.TempDir()
	if _, err := PublishBounded(root, p.ViewPath, view, int64(len(view))); err != nil {
		t.Fatal(err)
	}
	pass, index := "pass", "docs/dev/contracts/CONTRACTS-001.md"
	draft := p.Draft
	draft.Answer = ContractAnswer{Conclusion: &pass, IndexPath: &index, ContractPaths: []string{"docs/dev/contracts/BE-001.md"}, Applicability: map[string]ContractApplicability{
		"FE": {Disposition: "not_applicable", Rationale: "No client."}, "BE": {Disposition: "required", Rationale: "Service."}, "SYNC": {Disposition: "not_applicable", Rationale: "No shared protocol."},
	}}
	answer, _ := json.MarshalIndent(draft, "", "  ")
	if len(answer) <= 256<<10 {
		t.Fatal("fixture must exercise a questionnaire beyond the old answer limit")
	}
	if _, err := PublishBounded(root, p.AnswerPath, answer, ContractAnswerReadLimit(p)); err != nil {
		t.Fatal(err)
	}
	read, err := ReadFile(root, p.AnswerPath, ContractAnswerReadLimit(p))
	if err != nil {
		t.Fatal(err)
	}
	if _, issues := ValidateContractAnswer(p, read); len(issues) != 0 {
		t.Fatalf("machine source inventory consumed the author answer budget: %v", issues)
	}
	evidence, err := ContractEvidence(p, "ev-large", []Reference{
		{Path: index, Version: "v1", SHA256: Digest([]byte("index"))},
		{Path: draft.Answer.ContractPaths[0], Version: "v1", SHA256: Digest([]byte("contract"))},
	}, draft.Answer, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	evidenceBytes, _ := json.MarshalIndent(evidence, "", "  ")
	if len(evidenceBytes) <= 1<<20 {
		t.Fatal("fixture must exercise inherited evidence beyond the old read limit")
	}
	if _, err := PublishBounded(root, ".claude/evidence/large.json", evidenceBytes, int64(len(evidenceBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(root, ".claude/evidence/large.json", ContractEvidenceReadLimit(p)); err != nil {
		t.Fatal(err)
	}
	draft.Read = append([]ReadItem(nil), draft.Read...)
	draft.Read[len(draft.Read)-1].Path = "docs/design/forged.md"
	changed, _ := json.Marshal(draft)
	if _, issues := ValidateContractAnswer(p, changed); len(issues) == 0 {
		t.Fatal("large answer allowed a changed frozen source")
	}

	// S4 inherits this collection: it cannot reintroduce an input-count gate
	// after S3 has accepted the exact same sources.
	s4 := taskPlanningFixture(t)
	s4.Inputs.Sources = append(s4.Inputs.Sources, p.Inputs.Sources...)
	s4.Inputs.Directories = p.Inputs.Directories
	q, err := BuildTaskPlanning(s4.Binding, s4.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(q)
	if err := schema.NewEmbeddedValidator().ValidateBytes("task-planning-work-packet.schema.json", encoded); err != nil {
		t.Fatal(err)
	}
	assertLargePlanningPacketFitsRuntimeSchemas(t, q)
	qDraft := q.Draft
	taskIndex := "docs/dev/tasks/index-REQ-001.md"
	qDraft.Answer = TaskPlanningAnswer{Conclusion: &pass, IndexPath: &taskIndex, TaskPaths: []string{"docs/dev/tasks/TASK-001.md"}}
	encoded, _ = json.MarshalIndent(qDraft, "", "  ")
	if _, issues := ValidateTaskPlanningAnswer(q, encoded); len(issues) != 0 {
		t.Fatalf("inherited S3 source inventory made the S4 answer unusable: %v", issues)
	}
}

func assertLargePlanningPacketFitsRuntimeSchemas(t *testing.T, packet any) {
	t.Helper()
	example, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.2.0", "1.3.0"} {
		var state map[string]any
		if err := json.Unmarshal(example, &state); err != nil {
			t.Fatal(err)
		}
		state["schema_version"] = version
		state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
		state["entities"].(map[string]any)["work_packets"] = []any{packet}
		state["entities"].(map[string]any)["submissions"] = []any{}
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", data); err != nil {
			t.Fatalf("Runtime %s cannot persist its valid planning packet: %v", version, err)
		}
	}
}
