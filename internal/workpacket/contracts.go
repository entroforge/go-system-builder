package workpacket

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// ContractInputs must be resolved by the original source adapter. These
// frozen values are not supplied by professional answer JSON.
type ContractInputs struct {
	Designs     []Reference         `json:"designs"`
	Sources     []Subject           `json:"sources"`
	Directories map[string][]string `json:"directories"`
}
type ContractApplicability struct {
	Disposition string `json:"disposition"`
	Rationale   string `json:"rationale"`
}
type ContractAnswer struct {
	Conclusion    *string                          `json:"conclusion"`
	IndexPath     *string                          `json:"index_path"`
	ContractPaths []string                         `json:"contract_paths"`
	Applicability map[string]ContractApplicability `json:"applicability"`
}
type ContractDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      ContractAnswer      `json:"answer"`
}
type ContractPacket struct {
	ID          string          `json:"packet_id"`
	TemplateID  string          `json:"template_id"`
	TemplateSHA string          `json:"template_sha256"`
	Binding     PlanningBinding `json:"binding"`
	Inputs      ContractInputs  `json:"inputs"`
	ViewPath    string          `json:"view_path"`
	AnswerPath  string          `json:"answer_path"`
	Draft       ContractDraft   `json:"draft"`
}

func BuildContracts(binding PlanningBinding, inputs ContractInputs) (ContractPacket, error) {
	probe := binding
	probe.Stage = "S2"
	if _, err := BuildDesign(probe); err != nil || binding.Stage != "S3" {
		return ContractPacket{}, fmt.Errorf("S3 requires actual Main stage and frozen REQ")
	}
	if len(inputs.Designs) == 0 {
		return ContractPacket{}, fmt.Errorf("original registered design/source closure required")
	}
	// Copy and sort source sets; caller mutation and discovery order cannot
	// change the packet or its identity after construction.
	copyInputs := ContractInputs{Designs: append([]Reference(nil), inputs.Designs...), Sources: append([]Subject(nil), inputs.Sources...), Directories: map[string][]string{}}
	seen := map[string]bool{binding.Requirement.Path: true}
	for _, ref := range copyInputs.Designs {
		if ref.ID == "" || ref.Version == "" || !safeRelative(ref.Path) || !digestPattern.MatchString(ref.SHA256) || seen[ref.Path] {
			return ContractPacket{}, fmt.Errorf("design input identity is invalid or duplicated")
		}
		seen[ref.Path] = true
	}
	for _, ref := range copyInputs.Sources {
		if !safeRelative(ref.Path) || !digestPattern.MatchString(ref.SHA256) || seen[ref.Path] {
			return ContractPacket{}, fmt.Errorf("conditional input is invalid or duplicated")
		}
		seen[ref.Path] = true
	}
	for dir, names := range inputs.Directories {
		if !safeRelative(dir) {
			return ContractPacket{}, fmt.Errorf("invalid source directory manifest")
		}
		copyNames := append([]string(nil), names...)
		sort.Strings(copyNames)
		for i, name := range copyNames {
			if name == "." || name == ".." || strings.ContainsAny(name, "/\\") || name == "" || (i > 0 && copyNames[i-1] == name) {
				return ContractPacket{}, fmt.Errorf("invalid source directory member")
			}
		}
		if copyNames == nil {
			copyNames = []string{}
		}
		copyInputs.Directories[dir] = copyNames
	}
	sort.Slice(copyInputs.Designs, func(i, j int) bool { return copyInputs.Designs[i].Path < copyInputs.Designs[j].Path })
	sort.Slice(copyInputs.Sources, func(i, j int) bool { return copyInputs.Sources[i].Path < copyInputs.Sources[j].Path })
	data, err := schema.ReadAsset("stage-form-s3-template.json")
	if err != nil {
		return ContractPacket{}, err
	}
	var template struct {
		Stage, Adapter, Title, Instruction string
		Questions                          map[string]Question
	}
	if err := json.Unmarshal(data, &template); err != nil || template.Stage != "S3" || template.Adapter != "planning_contract" || len(template.Questions) != 4 {
		return ContractPacket{}, fmt.Errorf("unregistered S3 template")
	}
	sha := Digest(data)
	identity, _ := json.Marshal(struct {
		Binding     PlanningBinding
		Inputs      ContractInputs
		TemplateSHA string
	}{binding, copyInputs, sha})
	id := "packet-" + Digest(identity)
	base := fmt.Sprintf("%s/g%d/S3/e%d/%s", binding.RuntimeID, binding.Generation, binding.StageEntry, id)
	read := []ReadItem{{Path: binding.Requirement.Path, Purpose: "Use the frozen locked requirement and its acceptance denominator."}}
	for _, ref := range copyInputs.Designs {
		read = append(read, ReadItem{Path: ref.Path, Purpose: "Use the original registered design; return design gaps through the original process."})
	}
	for _, ref := range copyInputs.Sources {
		read = append(read, ReadItem{Path: ref.Path, Purpose: "Use this frozen conditional scenario, ADR or shared-model source."})
	}
	return ContractPacket{ID: id, TemplateID: "s3-contract-v1", TemplateSHA: sha, Binding: binding, Inputs: copyInputs, ViewPath: ".claude/work-packets/" + base + ".json", AnswerPath: ".claude/submissions/" + base + "/Main/answer.json", Draft: ContractDraft{Title: template.Title, Instruction: template.Instruction, Read: read, Questions: template.Questions, Answer: ContractAnswer{ContractPaths: []string{}, Applicability: map[string]ContractApplicability{}}}}, nil
}

func ValidateContractAnswer(p ContractPacket, data []byte) (ContractAnswer, []FieldError) {
	issue := func(pointer, expected, observed string) (ContractAnswer, []FieldError) {
		return ContractAnswer{}, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "保留已发行题干，补全本人合同交付与适用性判断。"}}
	}
	if int64(len(data)) > ContractAnswerReadLimit(p) {
		return issue("/", "issued questionnaire plus bounded professional answer", "oversized")
	}
	if err := ValidateUniqueJSON(data); err != nil {
		return issue("/", "unique keys", err.Error())
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s3.schema.json", data); err != nil {
		return ContractAnswer{}, planningSchemaIssues(err, "S3")
	}
	var draft ContractDraft
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&draft); err != nil {
		return issue("/", "closed contract questionnaire", err.Error())
	}
	answer := draft.Answer
	draft.Answer = p.Draft.Answer
	if !reflect.DeepEqual(draft, p.Draft) {
		return issue("/", "unchanged issued questionnaire", "machine fields changed")
	}

	if answer.Conclusion == nil && answer.IndexPath == nil && len(answer.ContractPaths) == 0 {
		return issue("/answer/conclusion", "explicit professional pass after completing the draft", "draft incomplete")
	}

	if answer.IndexPath == nil || !validContractDeliveryPath(*answer.IndexPath, "CONTRACTS-") {
		return issue("/answer/index_path", "repository-relative docs/dev/contracts/CONTRACTS-<ID>.md (no absolute paths, traversal or templates)", fmt.Sprintf("%v", optionalContractPath(answer.IndexPath)))
	}
	if len(answer.ContractPaths) == 0 {
		return issue("/answer/contract_paths", "at least one actual FE/BE/SYNC contract; empty contract sets are an unsupported S3 stage adaptation gap (do not fabricate a BE contract)", "empty")
	}
	seen := map[string]bool{}
	families := map[string]bool{}
	for i, path := range answer.ContractPaths {
		if !validContractDeliveryPath(path, "FE-", "BE-", "SYNC-") || seen[path] {
			return issue(fmt.Sprintf("/answer/contract_paths/%d", i), "unique repository-relative docs/dev/contracts/{FE,BE,SYNC}-<ID>.md (no absolute paths, traversal or templates)", fmt.Sprintf("%q (invalid or duplicate)", path))
		}
		seen[path] = true
		families[strings.SplitN(filepath.Base(path), "-", 2)[0]] = true
	}
	if answer.Conclusion == nil || *answer.Conclusion != "pass" {
		return issue("/answer/conclusion", "explicit professional pass", "incomplete")
	}

	if len(answer.Applicability) != 3 {
		return issue("/answer/applicability", "explicit FE/BE/SYNC decisions", "incomplete")
	}
	for _, family := range []string{"FE", "BE", "SYNC"} {
		decision, ok := answer.Applicability[family]
		if !ok || strings.TrimSpace(decision.Rationale) == "" || (decision.Disposition != "required" && decision.Disposition != "not_applicable") || families[family] != (decision.Disposition == "required") {
			return issue("/answer/applicability/"+family, "decision and rationale matching delivered contracts", "inconsistent")
		}
	}
	sort.Strings(answer.ContractPaths)
	return answer, nil
}

func ContractAnswerDigest(answer ContractAnswer) (string, error) {
	copyAnswer := answer
	copyAnswer.ContractPaths = append([]string(nil), answer.ContractPaths...)
	sort.Strings(copyAnswer.ContractPaths)
	data, err := json.Marshal(copyAnswer)
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// ContractDraftDeliveryPaths only projects safe author-named paths for display.
// It grants no reception eligibility: the original validator still runs in full.
func ContractDraftDeliveryPaths(p ContractPacket, data []byte) ([]string, bool) {
	if int64(len(data)) > ContractAnswerReadLimit(p) || ValidateUniqueJSON(data) != nil {
		return nil, false
	}
	var draft struct {
		Answer struct {
			IndexPath     *string  `json:"index_path"`
			ContractPaths []string `json:"contract_paths"`
		} `json:"answer"`
	}
	if json.Unmarshal(data, &draft) != nil {
		return nil, false
	}
	var paths []string
	named := draft.Answer.IndexPath != nil || len(draft.Answer.ContractPaths) > 0
	if draft.Answer.IndexPath != nil && validContractDeliveryPath(*draft.Answer.IndexPath, "CONTRACTS-") {
		paths = append(paths, *draft.Answer.IndexPath)
	}
	seen := map[string]bool{}
	for _, path := range draft.Answer.ContractPaths {
		if validContractDeliveryPath(path, "FE-", "BE-", "SYNC-") && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}
	return paths, named
}

func validContractDeliveryPath(path string, prefixes ...string) bool {
	if !safeRelative(path) || filepath.ToSlash(filepath.Dir(path)) != "docs/dev/contracts" || !strings.HasSuffix(path, ".md") || strings.Contains(strings.ToLower(path), "template") {
		return false
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(filepath.Base(path), prefix) {
			return true
		}
	}
	return false
}

func optionalContractPath(path *string) any {
	if path == nil {
		return nil
	}
	return *path
}

func planningSchemaIssues(err error, stage string) []FieldError {
	var validation *jsonschema.ValidationError
	if !errors.As(err, &validation) {
		return []FieldError{{Pointer: "/", Expected: "registered " + stage + " answer", Observed: boundedContractDiagnostic(err.Error()), Repair: "Restore the issued questionnaire shape; edit only its answer fields."}}
	}
	var issues []FieldError
	var walk func(*jsonschema.ValidationError)
	walk = func(node *jsonschema.ValidationError) {
		if len(issues) >= 8 {
			return
		}
		if len(node.Causes) > 0 {
			for _, child := range node.Causes {
				walk(child)
			}
			return
		}
		pointer := ""
		for _, part := range node.InstanceLocation {
			pointer += "/" + strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
		}
		var fields []string
		switch detail := node.ErrorKind.(type) {
		case *kind.AdditionalProperties:
			fields = append(fields, detail.Properties...)
		case *kind.Required:
			fields = append(fields, detail.Missing...)
		}
		if len(fields) > 0 {
			sort.Strings(fields)
			pointer += "/" + strings.ReplaceAll(strings.ReplaceAll(fields[0], "~", "~0"), "/", "~1")
		}
		if pointer == "" {
			pointer = "/"
		}
		issues = append(issues, FieldError{Pointer: boundedContractDiagnostic(pointer), Expected: "registered " + stage + " field shape", Observed: boundedContractDiagnostic(node.Error()), Repair: "At this field, remove unknown properties or restore the issued type and required keys; keep professional values in the registered answer fields."})
	}
	walk(validation)
	return issues
}

func boundedContractDiagnostic(message string) string {
	const limit = 2048
	if len(message) > limit {
		return strings.ToValidUTF8(message[:limit], "") + " [truncated]"
	}
	return message
}
