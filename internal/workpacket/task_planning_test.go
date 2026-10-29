package workpacket

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func taskPlanningFixture(t *testing.T) TaskPlanningPacket {
	t.Helper()
	req := []byte("locked REQ")
	inputs := TaskPlanningInputs{
		Contracts: []Reference{
			{ID: "FE-001", Path: "docs/dev/contracts/FE-001.md", Version: "v1.0.0", SHA256: Digest([]byte("fe"))},
			{ID: "CONTRACTS-001", Path: "docs/dev/contracts/CONTRACTS-001.md", Version: "v1.0.0", SHA256: Digest([]byte("index"))},
		},
		Sources:        []Subject{{Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1.0.0", SHA256: Digest([]byte("design"))}},
		Directories:    map[string][]string{"docs/dev/contracts": {"CONTRACTS-001.md", "FE-001.md"}},
		ClauseUniverse: []TaskClause{{ContractID: "FE-001", Section: 1}},
	}
	p, err := BuildTaskPlanning(PlanningBinding{
		RuntimeID: "runtime-001", Generation: 2, StageEntry: 4, Stage: "S4", Owner: "Main",
		Requirement: Reference{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Version: "v1.0.0", SHA256: Digest(req)},
	}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildTaskPlanningCanonicalizesFrozenDomainInputs(t *testing.T) {
	p := taskPlanningFixture(t)
	if p.TemplateID != "s4-task-planning-v1" || p.Binding.Stage != "S4" || p.Binding.Owner != "Main" {
		t.Fatalf("unexpected S4 packet identity: %#v", p)
	}
	if got, want := p.Inputs.Contracts[0].Path, "docs/dev/contracts/CONTRACTS-001.md"; got != want {
		t.Fatalf("contracts sorted by path: got %s want %s", got, want)
	}
	if got := p.Draft.Answer.TaskPaths; len(got) != 0 {
		t.Fatalf("fresh S4 draft must not imply a task batch: %v", got)
	}
	if len(p.Draft.Questions) != 3 {
		t.Fatalf("S4 asks for conclusion, index and complete task set: %#v", p.Draft.Questions)
	}
	for _, duty := range []string{"§4 scope/output paths", "§6", "§7 executable Closing Contracts", "§8 dependency DAG"} {
		if !strings.Contains(p.Draft.Questions["conclusion"].Question, duty) {
			t.Fatalf("S4 professional self-review omits %s", duty)
		}
	}
}

func TestBuildTaskPlanningPreservesBoundREQPathWithDescriptiveSuffix(t *testing.T) {
	base := taskPlanningFixture(t)
	binding := base.Binding
	binding.Requirement = Reference{
		ID: "REQ-039", Path: "docs/requirements/REQ-039-loop-control-plane.md",
		Version: "v2.1.0", SHA256: Digest([]byte("frozen REQ-039")),
	}
	p, err := BuildTaskPlanning(binding, base.Inputs)
	if err != nil {
		t.Fatalf("actual bound REQ path with descriptive suffix rejected: %v", err)
	}
	if p.Binding.Requirement.Path != binding.Requirement.Path || len(p.Draft.Read) == 0 || p.Draft.Read[0].Path != binding.Requirement.Path {
		t.Fatalf("S4 changed the authoritative bound REQ path: binding=%q read=%#v", p.Binding.Requirement.Path, p.Draft.Read)
	}

	pass, index := "pass", "docs/dev/tasks/index-REQ-039.md"
	draft := p.Draft
	draft.Answer = TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{"docs/dev/tasks/TASK-REQ-039-01.md"}}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, issues := ValidateTaskPlanningAnswer(p, data); len(issues) != 0 {
		t.Fatalf("exact waves-v1 index path rejected for suffixed REQ: %#v", issues)
	}

	index = "docs/dev/tasks/index-REQ-039-loop-control-plane.md"
	draft.Answer.IndexPath = &index
	data, err = json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, issues := ValidateTaskPlanningAnswer(p, data); len(issues) == 0 || issues[0].Pointer != "/answer/index_path" {
		t.Fatalf("index path must retain exact waves-v1 REQ ID: %#v", issues)
	}

	for _, path := range []string{
		"docs/design/REQ-039-loop-control-plane.md",
		"docs/requirements/archive/REQ-039-loop-control-plane.md",
		"docs/requirements/REQ-040-loop-control-plane.md",
		"docs/requirements/REQ-039-..md",
	} {
		invalid := binding
		invalid.Requirement.Path = path
		if _, err := BuildTaskPlanning(invalid, base.Inputs); err == nil {
			t.Errorf("accepted non-bound or unsafe REQ path %q", path)
		}
	}
}

func TestValidateTaskPlanningAnswerRequiresExplicitCompleteAnswer(t *testing.T) {
	p := taskPlanningFixture(t)
	if _, issues := ValidateTaskPlanningAnswer(p, mustTaskPlanningJSON(t, p)); len(issues) == 0 || issues[0].Pointer != "/answer/conclusion" {
		t.Fatalf("blank S4 draft must not pass: %#v", issues)
	}
	pass, index := "pass", "docs/dev/tasks/index-REQ-001.md"
	answer := TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{
		"docs/dev/tasks/TASK-REQ-001-02.md", "docs/dev/tasks/TASK-REQ-001-01.md",
	}}
	draft := p.Draft
	draft.Answer = answer
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	validated, issues := ValidateTaskPlanningAnswer(p, data)
	if len(issues) != 0 {
		t.Fatalf("complete typed S4 answer rejected: %#v", issues)
	}
	if validated.TaskPaths[0] != "docs/dev/tasks/TASK-REQ-001-01.md" || validated.TaskPaths[1] != "docs/dev/tasks/TASK-REQ-001-02.md" {
		t.Fatalf("task paths were not canonicalized: %v", validated.TaskPaths)
	}
	if _, issues := ValidateTaskPlanningAnswer(p, []byte(`{"title":"x","instruction":"x","read":[],"questions":{},"answer":{"conclusion":"pass","index_path":"docs/dev/tasks/index-REQ-002.md","task_paths":["docs/dev/tasks/TASK-REQ-002-01.md"]}}`)); len(issues) == 0 {
		t.Fatal("S4 answer for a different REQ was accepted")
	}
}

func TestTaskPlanningAnswerDigestCanonicalizesTaskSet(t *testing.T) {
	one := TaskPlanningAnswer{Conclusion: stringPointer("pass"), IndexPath: stringPointer("docs/dev/tasks/index-REQ-001.md"), TaskPaths: []string{"docs/dev/tasks/TASK-REQ-001-02.md", "docs/dev/tasks/TASK-REQ-001-01.md"}}
	two := one
	two.TaskPaths = []string{one.TaskPaths[1], one.TaskPaths[0]}
	a, err := TaskPlanningAnswerDigest(one)
	if err != nil {
		t.Fatal(err)
	}
	b, err := TaskPlanningAnswerDigest(two)
	if err != nil || a != b {
		t.Fatalf("answer digest must ignore path discovery order: %s %s %v", a, b, err)
	}
}

func TestTaskPlanningEvidenceProjectsExactCanonicalSubjectsAndAnswer(t *testing.T) {
	p := taskPlanningFixture(t)
	pass, index := "pass", "docs/dev/tasks/index-REQ-001.md"
	answer := TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{
		"docs/dev/tasks/TASK-REQ-001-02.md", "docs/dev/tasks/TASK-REQ-001-01.md",
	}}
	artifacts := []Reference{
		{ID: "TASK-REQ-001-02", Path: "docs/dev/tasks/TASK-REQ-001-02.md", Version: "v1.0.0", SHA256: Digest([]byte("task 2"))},
		{ID: "index-REQ-001", Path: index, Version: "v1.0.0", SHA256: Digest([]byte("index"))},
		{ID: "TASK-REQ-001-01", Path: "docs/dev/tasks/TASK-REQ-001-01.md", Version: "v1.0.0", SHA256: Digest([]byte("task 1"))},
	}
	envelope, err := TaskPlanningEvidence(p, "E-S4", artifacts, answer, time.Date(2026, 10, 14, 1, 2, 3, 0, time.FixedZone("other", 3600)))
	if err != nil {
		t.Fatal(err)
	}
	if envelope["kind"] != "planning_task" || envelope["producer_agent_id"] != "Main" || envelope["producer_responsibility"] != "Orchestrator" || envelope["conclusion"] != "pass" {
		t.Fatalf("unexpected original S4 evidence semantics: %#v", envelope)
	}
	answerValue, ok := envelope["task_planning_answer"].(TaskPlanningAnswer)
	if !ok || answerValue.TaskPaths[0] != "docs/dev/tasks/TASK-REQ-001-01.md" {
		t.Fatalf("evidence did not preserve canonical answer: %#v", envelope["task_planning_answer"])
	}
	subjects, ok := envelope["subject_refs"].([]Subject)
	if !ok || len(subjects) != 7 {
		t.Fatalf("evidence subjects should include REQ, source, locked contracts and outputs: %#v", envelope["subject_refs"])
	}
	if subjects[0].Path != p.Binding.Requirement.Path || subjects[len(subjects)-3].Path != index {
		t.Fatalf("evidence subjects have noncanonical source ordering: %#v", subjects)
	}
	if len(envelope["created_at"].(string)) == 0 || envelope["created_at"].(string)[len(envelope["created_at"].(string))-1] != 'Z' {
		t.Fatalf("evidence timestamp must be canonical UTC: %v", envelope["created_at"])
	}
	if _, err := TaskPlanningEvidence(p, "E-S4", artifacts[:2], answer, time.Now()); err == nil {
		t.Fatal("incomplete artifact set was accepted")
	}
}

func mustTaskPlanningJSON(t *testing.T, p TaskPlanningPacket) []byte {
	t.Helper()
	data, err := json.Marshal(p.Draft)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stringPointer(value string) *string { return &value }

func TestValidateTaskPlanningAnswerDoesNotRebuildSourcePacket(t *testing.T) {
	p := taskPlanningFixture(t)
	pass, index := "pass", "docs/dev/tasks/index-REQ-001.md"
	draft := p.Draft
	draft.Answer = TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{"docs/dev/tasks/TASK-001.md"}}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	// Source/packet qualification belongs neither to professional answer decoding
	// nor to repeated reception. The current questionnaire was selected by path.
	p.Inputs = TaskPlanningInputs{}
	p.Draft.Questions = nil
	answer, issues := ValidateTaskPlanningAnswer(p, data)
	if len(issues) != 0 || answer.Conclusion == nil || *answer.Conclusion != "pass" {
		t.Fatalf("professional answer re-proved source packet: %+v %+v", answer, issues)
	}
}

func TestTaskPlanningEvidenceIncludesActualCancelledTaskWithoutChangingProfessionalAnswer(t *testing.T) {
	p := taskPlanningFixture(t)
	pass, index := "pass", "docs/dev/tasks/index-REQ-001.md"
	answer := TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{"docs/dev/tasks/TASK-001.md"}}
	artifacts := []Reference{
		{Path: index, Version: "v1.0.0", SHA256: Digest([]byte("index"))},
		{Path: "docs/dev/tasks/TASK-001.md", Version: "v1.0.0", SHA256: Digest([]byte("complete"))},
		{Path: "docs/dev/tasks/TASK-002.md", Version: "v1.0.0", SHA256: Digest([]byte("cancelled"))},
	}
	envelope, err := TaskPlanningEvidence(p, "E-S4", artifacts, answer, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	actual := envelope["task_planning_answer"].(TaskPlanningAnswer)
	if len(actual.TaskPaths) != 1 || actual.TaskPaths[0] != answer.TaskPaths[0] {
		t.Fatalf("professional answer was extended: %+v", actual)
	}
	found := false
	for _, subject := range envelope["subject_refs"].([]Subject) {
		if subject.Path == "docs/dev/tasks/TASK-002.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("actual cancelled TASK fact omitted")
	}
}

func TestTaskPlanningInheritsLargeAcceptedContractSet(t *testing.T) {
	p := taskPlanningFixture(t)
	inputs := p.Inputs
	for i := 2; i <= 96; i++ {
		id := fmt.Sprintf("FE-%03d", i)
		inputs.Contracts = append(inputs.Contracts, Reference{ID: id, Path: "docs/dev/contracts/" + id + ".md", Version: "v1.0.0", SHA256: Digest([]byte(id))})
	}
	if got, err := BuildTaskPlanning(p.Binding, inputs); err != nil || len(got.Inputs.Contracts) != 97 {
		t.Fatalf("large accepted S3 set cannot enter S4: size=%d err=%v", len(got.Inputs.Contracts), err)
	}
}
