package workpacket

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

var (
	taskPlanningREQIDRE      = regexp.MustCompile(`^REQ-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	taskPlanningREQSuffixRE  = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*$`)
	taskPlanningContractIDRE = regexp.MustCompile(`^(?:FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	taskPlanningIndexIDRE    = regexp.MustCompile(`^CONTRACTS-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
)

// TaskClause is one cell in the locked CONTRACTS index clause universe.
// TASK §3 declarations remain authoring artifacts and are checked by the
// original semantic.TasksCheck consumer.
type TaskClause struct {
	ContractID string `json:"contract_id"`
	Section    int    `json:"section"`
}

// TaskPlanningInputs are reconstructed from the original REQ, registered
// locked contract batch, and the accepted S3 source closure. They are never
// accepted from answer JSON.
type TaskPlanningInputs struct {
	Contracts      []Reference         `json:"contracts"`
	Sources        []Subject           `json:"sources"`
	Directories    map[string][]string `json:"directories"`
	ClauseUniverse []TaskClause        `json:"clause_universe"`
}

// TaskPlanningAnswer contains only the professional self-review and actual
// S4 artifact paths. Dependency and clause declarations live in the TASK and
// index files and are reconciled by the original S4 checker.
type TaskPlanningAnswer struct {
	Conclusion *string  `json:"conclusion"`
	IndexPath  *string  `json:"index_path"`
	TaskPaths  []string `json:"task_paths"`
}

type TaskPlanningDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      TaskPlanningAnswer  `json:"answer"`
}

type TaskPlanningPacket struct {
	ID          string             `json:"packet_id"`
	TemplateID  string             `json:"template_id"`
	TemplateSHA string             `json:"template_sha256"`
	Binding     PlanningBinding    `json:"binding"`
	Inputs      TaskPlanningInputs `json:"inputs"`
	ViewPath    string             `json:"view_path"`
	AnswerPath  string             `json:"answer_path"`
	Draft       TaskPlanningDraft  `json:"draft"`
}

// BuildTaskPlanning creates the S4 Main questionnaire from resolved original
// inputs. The task set itself is left empty until the professional answer is
// submitted; callers must resolve it from the actual REQ-scoped directory.
func BuildTaskPlanning(binding PlanningBinding, inputs TaskPlanningInputs) (TaskPlanningPacket, error) {
	return BuildTaskPlanningWithTemplate(binding, inputs, "")
}

// BuildTaskPlanningWithTemplate preserves known issued templates without trusting
// caller-supplied prompt bytes. Empty selects the current template for new work.
func BuildTaskPlanningWithTemplate(binding PlanningBinding, inputs TaskPlanningInputs, templateSHA string) (TaskPlanningPacket, error) {
	if binding.Stage != "S4" || binding.Owner != "Main" || !safeComponent(binding.RuntimeID) || binding.Generation < 1 || binding.StageEntry < 0 || !taskPlanningREQIDRE.MatchString(binding.Requirement.ID) || !validTaskPlanningRequirementPath(binding.Requirement.ID, binding.Requirement.Path) || binding.Requirement.Version == "" || !digestPattern.MatchString(binding.Requirement.SHA256) {
		return TaskPlanningPacket{}, fmt.Errorf("S4 requires the actual Main stage and frozen requirement binding")
	}
	canonical, err := canonicalTaskPlanningInputs(binding, inputs)
	if err != nil {
		return TaskPlanningPacket{}, err
	}
	var data []byte
	for _, asset := range []string{"stage-form-s4-template.json", "stage-form-s4-template-before-guidance.json"} {
		data, err = schema.ReadAsset(asset)
		if err != nil {
			return TaskPlanningPacket{}, err
		}
		if templateSHA == "" || Digest(data) == templateSHA {
			break
		}
	}
	if templateSHA != "" && Digest(data) != templateSHA {
		return TaskPlanningPacket{}, fmt.Errorf("unknown S4 questionnaire template")
	}
	var template struct {
		Stage, Adapter, Title, Instruction string
		Questions                          map[string]Question
	}
	if err := json.Unmarshal(data, &template); err != nil || template.Stage != "S4" || template.Adapter != "task_planning" || len(template.Questions) != 3 {
		return TaskPlanningPacket{}, fmt.Errorf("unregistered S4 task planning template")
	}
	templateSHA = Digest(data)
	identity, err := json.Marshal(struct {
		Binding     PlanningBinding
		Inputs      TaskPlanningInputs
		TemplateSHA string
	}{binding, canonical, templateSHA})
	if err != nil {
		return TaskPlanningPacket{}, err
	}
	id := "packet-" + Digest(identity)
	base := fmt.Sprintf("%s/g%d/S4/e%d/%s", binding.RuntimeID, binding.Generation, binding.StageEntry, id)
	read := []ReadItem{{Path: binding.Requirement.Path, Purpose: "Use the frozen locked REQ and its complete acceptance denominator."}}
	for _, contract := range canonical.Contracts {
		read = append(read, ReadItem{Path: contract.Path, Purpose: "Use the complete current locked contract set and clause universe."})
	}
	for _, source := range canonical.Sources {
		read = append(read, ReadItem{Path: source.Path, Purpose: "Use this source frozen by the original planning and contract registrations."})
	}
	answer := TaskPlanningAnswer{TaskPaths: []string{}}
	return TaskPlanningPacket{
		ID: id, TemplateID: "s4-task-planning-v1", TemplateSHA: templateSHA,
		Binding: binding, Inputs: canonical,
		ViewPath:   ".claude/work-packets/" + base + ".json",
		AnswerPath: ".claude/submissions/" + base + "/Main/answer.json",
		Draft:      TaskPlanningDraft{Title: template.Title, Instruction: template.Instruction, Read: read, Questions: template.Questions, Answer: answer},
	}, nil
}

// validTaskPlanningRequirementPath validates the bound requirement location
// without deriving it from the REQ ID. Existing locked requirements may have
// a descriptive filename suffix; the original bound path remains authoritative
// in the packet and its read list.
func validTaskPlanningRequirementPath(id, path string) bool {
	if !taskPlanningREQIDRE.MatchString(id) || !safeRelative(path) || filepath.ToSlash(filepath.Dir(path)) != "docs/requirements" || !strings.HasSuffix(path, ".md") {
		return false
	}
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	if name == id {
		return true
	}
	if !strings.HasPrefix(name, id+"-") {
		return false
	}
	suffix := strings.TrimPrefix(name, id+"-")
	return taskPlanningREQSuffixRE.MatchString(suffix)
}

func canonicalTaskPlanningInputs(binding PlanningBinding, inputs TaskPlanningInputs) (TaskPlanningInputs, error) {
	if len(inputs.Contracts) == 0 || len(inputs.ClauseUniverse) == 0 || len(inputs.ClauseUniverse) > 16384 {
		return TaskPlanningInputs{}, fmt.Errorf("S4 requires locked contracts, frozen sources, directories and a bounded clause universe")
	}
	out := TaskPlanningInputs{
		Contracts:      append([]Reference(nil), inputs.Contracts...),
		Sources:        append([]Subject(nil), inputs.Sources...),
		Directories:    map[string][]string{},
		ClauseUniverse: append([]TaskClause(nil), inputs.ClauseUniverse...),
	}
	seenPath := map[string]bool{binding.Requirement.Path: true}
	contractIDs := map[string]bool{}
	indexCount := 0
	for _, ref := range out.Contracts {
		if !safeRelative(ref.Path) || filepath.ToSlash(filepath.Dir(ref.Path)) != "docs/dev/contracts" || !strings.HasSuffix(ref.Path, ".md") || strings.Contains(strings.ToLower(filepath.Base(ref.Path)), "template") || ref.ID == "" || ref.Version == "" || !digestPattern.MatchString(ref.SHA256) || seenPath[ref.Path] || contractIDs[ref.ID] {
			return TaskPlanningInputs{}, fmt.Errorf("S4 locked contract input has invalid or duplicate identity")
		}
		if filepath.Base(ref.Path) != ref.ID+".md" {
			return TaskPlanningInputs{}, fmt.Errorf("S4 locked contract ID and path differ")
		}
		if taskPlanningIndexIDRE.MatchString(ref.ID) {
			// The index path is validated as the unique index below.
		} else if !taskPlanningContractIDRE.MatchString(ref.ID) {
			return TaskPlanningInputs{}, fmt.Errorf("S4 locked contract has an unsupported identity")
		}
		seenPath[ref.Path] = true
		contractIDs[ref.ID] = true
		if strings.HasPrefix(ref.ID, "CONTRACTS-") {
			indexCount++
		}
	}
	if indexCount != 1 {
		return TaskPlanningInputs{}, fmt.Errorf("S4 requires exactly one registered CONTRACTS index")
	}
	for _, source := range out.Sources {
		if !safeRelative(source.Path) || !digestPattern.MatchString(source.SHA256) || seenPath[source.Path] {
			return TaskPlanningInputs{}, fmt.Errorf("S4 source input has invalid or duplicate identity")
		}
		seenPath[source.Path] = true
	}
	for dir, names := range inputs.Directories {
		if !safeRelative(dir) {
			return TaskPlanningInputs{}, fmt.Errorf("S4 source directory manifest is invalid")
		}
		copyNames := append([]string(nil), names...)
		sort.Strings(copyNames)
		for i, name := range copyNames {
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || (i > 0 && copyNames[i-1] == name) {
				return TaskPlanningInputs{}, fmt.Errorf("S4 source directory member is invalid")
			}
		}
		if copyNames == nil {
			copyNames = []string{}
		}
		out.Directories[dir] = copyNames
	}
	for _, clause := range out.ClauseUniverse {
		if !contractIDs[clause.ContractID] || strings.HasPrefix(clause.ContractID, "CONTRACTS-") || clause.Section < 1 {
			return TaskPlanningInputs{}, fmt.Errorf("S4 clause universe contains an invalid contract/section reference")
		}
	}
	sort.Slice(out.Contracts, func(i, j int) bool { return out.Contracts[i].Path < out.Contracts[j].Path })
	sort.Slice(out.Sources, func(i, j int) bool { return out.Sources[i].Path < out.Sources[j].Path })
	sort.Slice(out.ClauseUniverse, func(i, j int) bool {
		if out.ClauseUniverse[i].ContractID != out.ClauseUniverse[j].ContractID {
			return out.ClauseUniverse[i].ContractID < out.ClauseUniverse[j].ContractID
		}
		return out.ClauseUniverse[i].Section < out.ClauseUniverse[j].Section
	})
	for i := 1; i < len(out.ClauseUniverse); i++ {
		if out.ClauseUniverse[i-1] == out.ClauseUniverse[i] {
			return TaskPlanningInputs{}, fmt.Errorf("S4 clause universe repeats %s §%d", out.ClauseUniverse[i].ContractID, out.ClauseUniverse[i].Section)
		}
	}
	return out, nil
}

// ValidateTaskPlanningAnswer validates the closed S4 questionnaire and returns
// its canonical task path order. A blank draft cannot produce a pass.
func ValidateTaskPlanningAnswer(p TaskPlanningPacket, data []byte) (TaskPlanningAnswer, []FieldError) {
	issue := func(pointer, expected, observed string) (TaskPlanningAnswer, []FieldError) {
		return TaskPlanningAnswer{}, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "保留已发行题干，填写本人自审结论和实际 TASK/索引产物。"}}
	}
	if int64(len(data)) > TaskPlanningAnswerReadLimit(p) {
		return issue("/", "issued questionnaire plus bounded professional answer", "oversized")
	}
	if err := ValidateUniqueJSON(data); err != nil {
		return issue("/", "unique keys", err.Error())
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s4.schema.json", data); err != nil {
		return TaskPlanningAnswer{}, planningSchemaIssues(err, "S4")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var draft TaskPlanningDraft
	if err := decoder.Decode(&draft); err != nil {
		return issue("/", "closed task planning questionnaire", err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return issue("/", "one JSON object", "trailing data")
	}
	answer := draft.Answer
	if answer.Conclusion == nil && answer.IndexPath == nil && len(answer.TaskPaths) == 0 {
		return issue("/answer/conclusion", "explicit professional task planning pass", "incomplete")
	}

	reqID := p.Binding.Requirement.ID
	wantIndex := "docs/dev/tasks/index-" + reqID + ".md"
	if answer.IndexPath == nil || *answer.IndexPath != wantIndex {
		return issue("/answer/index_path", wantIndex+" (repository-relative; no root prefix)", fmt.Sprintf("%v", optionalContractPath(answer.IndexPath)))
	}
	if len(answer.TaskPaths) == 0 {
		return issue("/answer/task_paths", "actual planned REQ task paths within the answer byte budget", "empty")
	}
	seen := map[string]bool{}
	for i, path := range answer.TaskPaths {
		base := filepath.Base(path)
		if !safeRelative(path) || filepath.ToSlash(filepath.Dir(path)) != "docs/dev/tasks" || !strings.HasPrefix(base, "TASK-") || !strings.HasSuffix(base, ".md") || strings.Contains(strings.ToLower(base), "template") || seen[path] {
			return issue(fmt.Sprintf("/answer/task_paths/%d", i), "unique actual repository-relative docs/dev/tasks/TASK-*.md paths (no root prefix)", fmt.Sprintf("%q (invalid or duplicate)", path))
		}
		seen[path] = true
	}
	if answer.Conclusion == nil || *answer.Conclusion != "pass" {
		return issue("/answer/conclusion", "explicit professional task planning pass", "incomplete")
	}
	answer.TaskPaths = append([]string(nil), answer.TaskPaths...)
	sort.Strings(answer.TaskPaths)
	return answer, nil
}

func TaskPlanningAnswerDigest(answer TaskPlanningAnswer) (string, error) {
	copyAnswer := answer
	copyAnswer.TaskPaths = append([]string(nil), answer.TaskPaths...)
	sort.Strings(copyAnswer.TaskPaths)
	data, err := json.Marshal(copyAnswer)
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// TaskPlanningEvidence projects validated S4 authoring into the original
// planning_task evidence shape. It never asserts that TasksCheck passed; the
// trusted composition callback runs that original checker before this is used.
func TaskPlanningEvidence(p TaskPlanningPacket, id string, artifacts []Reference, answer TaskPlanningAnswer, at time.Time) (map[string]any, error) {
	if id == "" || p.ID == "" || p.TemplateID != "s4-task-planning-v1" || p.Binding.Stage != "S4" || p.Binding.Owner != "Main" || p.Binding.RuntimeID == "" || p.Binding.Generation < 1 {
		return nil, fmt.Errorf("planning_task evidence requires the original Main S4 packet")
	}
	answer, err := validateTaskPlanningEvidenceAnswer(p, answer)
	if err != nil {
		return nil, err
	}
	ordered, err := canonicalTaskPlanningArtifacts(answer, artifacts)
	if err != nil {
		return nil, err
	}
	subjects := make([]Subject, 0, 1+len(p.Inputs.Sources)+len(p.Inputs.Contracts)+len(ordered))
	req := p.Binding.Requirement
	if req.Path == "" || req.Version == "" || !digestPattern.MatchString(req.SHA256) {
		return nil, fmt.Errorf("planning_task evidence has an invalid frozen REQ subject")
	}
	subjects = append(subjects, Subject{Path: req.Path, Version: req.Version, SHA256: req.SHA256})
	sources := append([]Subject(nil), p.Inputs.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	for _, source := range sources {
		if !safeRelative(source.Path) || !digestPattern.MatchString(source.SHA256) {
			return nil, fmt.Errorf("planning_task evidence has an invalid frozen source subject")
		}
		subjects = append(subjects, source)
	}
	contracts := append([]Reference(nil), p.Inputs.Contracts...)
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].Path < contracts[j].Path })
	for _, contract := range contracts {
		if !safeRelative(contract.Path) || contract.Version == "" || !digestPattern.MatchString(contract.SHA256) {
			return nil, fmt.Errorf("planning_task evidence has an invalid actual contract subject")
		}
		subjects = append(subjects, Subject{Path: contract.Path, Version: contract.Version, SHA256: contract.SHA256})
	}
	for _, artifact := range ordered {
		subjects = append(subjects, Subject{Path: artifact.Path, Version: artifact.Version, SHA256: artifact.SHA256})
	}
	seen := map[string]bool{}
	for _, subject := range subjects {
		if seen[subject.Path] {
			return nil, fmt.Errorf("planning_task evidence repeats subject path %s", subject.Path)
		}
		seen[subject.Path] = true
	}
	return map[string]any{
		"schema_version": "1.0.0", "evidence_id": id, "kind": "planning_task",
		"runtime_id": p.Binding.RuntimeID, "baseline_generation": p.Binding.Generation,
		"producer_agent_id": p.Binding.Owner, "producer_responsibility": "Orchestrator",
		"subject_refs": subjects, "conclusion": *answer.Conclusion,
		"created_at": at.UTC().Format(time.RFC3339Nano), "task_planning_answer": answer,
	}, nil
}

func validateTaskPlanningEvidenceAnswer(p TaskPlanningPacket, answer TaskPlanningAnswer) (TaskPlanningAnswer, error) {
	draft := p.Draft
	draft.Answer = answer
	data, err := json.Marshal(draft)
	if err != nil {
		return TaskPlanningAnswer{}, err
	}
	validated, issues := ValidateTaskPlanningAnswer(p, data)
	if len(issues) != 0 {
		return TaskPlanningAnswer{}, fmt.Errorf("planning_task answer %s: %s", issues[0].Pointer, issues[0].Expected)
	}
	return validated, nil
}

func canonicalTaskPlanningArtifacts(answer TaskPlanningAnswer, artifacts []Reference) ([]Reference, error) {
	if answer.IndexPath == nil {
		return nil, fmt.Errorf("planning_task evidence artifact set differs from professional answer")
	}
	want := map[string]bool{*answer.IndexPath: true}
	for _, path := range answer.TaskPaths {
		if want[path] {
			return nil, fmt.Errorf("planning_task answer repeats the index as a TASK")
		}
		want[path] = true
	}
	ordered := append([]Reference(nil), artifacts...)
	seenPath, seenID := map[string]bool{}, map[string]bool{}
	for _, artifact := range ordered {
		if !safeRelative(artifact.Path) || filepath.ToSlash(filepath.Dir(artifact.Path)) != "docs/dev/tasks" || !strings.HasSuffix(artifact.Path, ".md") || strings.Contains(strings.ToLower(filepath.Base(artifact.Path)), "template") || artifact.Version == "" || !digestPattern.MatchString(artifact.SHA256) || (artifact.Path != *answer.IndexPath && !strings.HasPrefix(filepath.Base(artifact.Path), "TASK-")) {
			return nil, fmt.Errorf("planning_task evidence has an invalid or unrequested actual artifact")
		}
		name := strings.TrimSuffix(filepath.Base(artifact.Path), ".md")
		if seenPath[artifact.Path] || seenID[name] {
			return nil, fmt.Errorf("planning_task evidence repeats artifact identity %s", name)
		}
		seenPath[artifact.Path], seenID[name] = true, true
	}
	for path := range want {
		if !seenPath[path] {
			return nil, fmt.Errorf("planning_task evidence omits the actual TASK/index artifact %s", path)
		}
	}
	index := *answer.IndexPath
	sort.Slice(ordered, func(i, j int) bool {
		iIndex, jIndex := ordered[i].Path == index, ordered[j].Path == index
		if iIndex != jIndex {
			return iIndex
		}
		return ordered[i].Path < ordered[j].Path
	})
	return ordered, nil
}
