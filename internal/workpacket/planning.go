package workpacket

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// PlanningBinding identifies the actual Main stage responsibility. It never
// fabricates a dispatched Assignment or an independent professional agent.
type PlanningBinding struct {
	RuntimeID   string    `json:"runtime_id"`
	Generation  int       `json:"generation"`
	StageEntry  int       `json:"stage_entry"`
	Stage       string    `json:"stage"`
	Owner       string    `json:"owner"`
	Requirement Reference `json:"requirement"`
}
type DesignAnswer struct {
	Conclusion       *string `json:"conclusion"`
	ArchitecturePath *string `json:"architecture_path"`
}
type DesignDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      DesignAnswer        `json:"answer"`
}
type DesignPacket struct {
	ID          string          `json:"packet_id"`
	TemplateID  string          `json:"template_id"`
	TemplateSHA string          `json:"template_sha256"`
	Binding     PlanningBinding `json:"binding"`
	ViewPath    string          `json:"view_path"`
	AnswerPath  string          `json:"answer_path"`
	Draft       DesignDraft     `json:"draft"`
}

func BuildDesign(binding PlanningBinding) (DesignPacket, error) {
	req := binding.Requirement
	if binding.Stage != "S2" || binding.Owner != "Main" || !safeComponent(binding.RuntimeID) || binding.Generation < 1 || binding.StageEntry < 0 || req.ID == "" || req.Version == "" || !safeRelative(req.Path) || !digestPattern.MatchString(req.SHA256) {
		return DesignPacket{}, fmt.Errorf("S2 requires the actual Main stage and frozen requirement binding")
	}
	data, err := schema.ReadAsset("stage-form-s2-template.json")
	if err != nil {
		return DesignPacket{}, err
	}
	var template struct {
		Stage, Adapter, Title, Instruction string
		Questions                          map[string]Question
	}
	if err := json.Unmarshal(data, &template); err != nil {
		return DesignPacket{}, err
	}
	if template.Stage != "S2" || template.Adapter != "planning_design" || len(template.Questions) != 2 {
		return DesignPacket{}, fmt.Errorf("unregistered S2 design template")
	}
	templateSHA := Digest(data)
	identity, _ := json.Marshal(struct {
		Binding     PlanningBinding
		TemplateSHA string
	}{binding, templateSHA})
	id := "packet-" + Digest(identity)
	base := fmt.Sprintf("%s/g%d/S2/e%d/%s", binding.RuntimeID, binding.Generation, binding.StageEntry, id)
	p := DesignPacket{ID: id, TemplateID: "s2-design-v1", TemplateSHA: templateSHA, Binding: binding, ViewPath: ".claude/work-packets/" + base + ".json", AnswerPath: ".claude/submissions/" + base + "/Main/answer.json", Draft: DesignDraft{Title: template.Title, Instruction: template.Instruction, Read: []ReadItem{{Path: req.Path, Purpose: "Current locked requirement: identify its actual commitments, UI impact and existing materials."}, {Path: "docs/architecture/ARCHITECTURE-template.md", Purpose: "Architecture output template: define actual boundaries, data/state/error/recovery and contract decisions."}, {Path: "docs/design/decisions/ADR-template.md", Purpose: "Record decisions, alternatives, risks and the actual human direction judgment when needed; do not infer approval from a label."}, {Path: "docs/design/prototypes/README.md", Purpose: "Find existing affected module truth. UI changed: update applicable UI/scenario package; no UI but service behavior affected: update applicable service behavior package; do not manufacture unrelated deliverables."}, {Path: "docs/rules/scenario-model.md", Purpose: "Use actual AC/FR/behavior relations, observations and oracles; complete applicable scenario checks as design work, not a second receipt."}, {Path: "docs/design/derivation/DERIVATION-template.md", Purpose: "For actual UI changes, derive the current module surface from the project design foundation and existing module materials."}, {Path: "docs/rules/design-foundation.md", Purpose: "Conditional UI design guidance; absent project-specific foundation is a concrete material gap, not permission to invent its contents."}}, Questions: template.Questions}}
	return p, nil
}

func ValidateDesignAnswer(p DesignPacket, data []byte) (DesignAnswer, []FieldError) {
	issue := func(pointer, expected, observed, repair string) (DesignAnswer, []FieldError) {
		return DesignAnswer{}, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: repair}}
	}
	var draft struct {
		Answer DesignAnswer `json:"answer"`
	}
	if err := json.Unmarshal(data, &draft); err != nil {
		return issue("/answer", "readable original design answer fields", err.Error(), "填写现有 conclusion 和 architecture_path 字段。")
	}
	answer := draft.Answer
	if answer.Conclusion != nil && *answer.Conclusion != "" && *answer.Conclusion != "pass" {
		return issue("/answer/conclusion", "pass after actual design self-review, or empty while unfinished", *answer.Conclusion, "未完成时保留空结论，不要为了接收而填写 pass。")
	}
	if answer.ArchitecturePath != nil && *answer.ArchitecturePath != "" && !safeRelative(*answer.ArchitecturePath) {
		return issue("/answer/architecture_path", "current project architecture path", *answer.ArchitecturePath, "填写实际设计文档路径。")
	}
	return answer, nil
}

func DesignAnswerDigest(answer DesignAnswer) (string, error) {
	data, err := json.Marshal(answer)
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// ValidateUniqueJSON shares the authoring duplicate-key boundary with adapters.
func ValidateUniqueJSON(data []byte) error { return rejectDuplicateKeys(data) }

// DesignEvidence is the original planning_design envelope, not a transition
// or a quality certificate. The adapter must validate the actual artifact first.
func DesignEvidence(p DesignPacket, id string, architecture Reference, answer DesignAnswer, at time.Time) (map[string]any, error) {
	if answer.Conclusion == nil || *answer.Conclusion != "pass" || answer.ArchitecturePath == nil || architecture.Path != *answer.ArchitecturePath {
		return nil, fmt.Errorf("planning evidence differs from professional answer or actual architecture")
	}
	req := p.Binding.Requirement
	return map[string]any{"schema_version": "1.0.0", "evidence_id": id, "kind": "planning_design", "runtime_id": p.Binding.RuntimeID, "baseline_generation": p.Binding.Generation, "producer_agent_id": p.Binding.Owner, "producer_responsibility": "Orchestrator", "subject_refs": []Subject{{Path: req.Path, Version: req.Version, SHA256: req.SHA256}, {Path: architecture.Path, Version: architecture.Version, SHA256: architecture.SHA256}}, "conclusion": *answer.Conclusion, "created_at": at.UTC().Format(time.RFC3339Nano)}, nil
}
