package workpacket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/schema"
)

const repairPlanReportTemplateID = "s9-plan-report-v1"

var (
	repairPlanReportCaseIDRE       = regexp.MustCompile(`^investigation-case-[A-Za-z0-9._-]+$`)
	repairPlanReportContractIDRE   = regexp.MustCompile(`^repair-contract-[A-Za-z0-9._-]+$`)
	repairPlanReportSessionIDRE    = regexp.MustCompile(`^repair-session-[A-Za-z0-9._-]+$`)
	repairPlanReportPlanIDRE       = regexp.MustCompile(`^repair-plan-[A-Za-z0-9._-]+$`)
	repairPlanReportAssignmentRE   = regexp.MustCompile(`^repair-assignment-[A-Za-z0-9._-]+$`)
	repairPlanReportManifestIDRE   = regexp.MustCompile(`^assignment-[A-Za-z0-9._-]+$`)
	repairPlanReportTaskIDRE       = regexp.MustCompile(`^TASK-[A-Za-z0-9._-]+$`)
	repairPlanReportPacketLimit    = 20 << 20
	repairPlanReportSourceDocLimit = 4 << 20
)

// RepairPlanReportBinding is the immutable identity supplied by the original
// S9 dispatcher. The package does not import repair/runtime and cannot grant
// dispatch authority.
type RepairPlanReportBinding struct {
	RuntimeID                string `json:"runtime_id"`
	BaselineGeneration       int    `json:"baseline_generation"`
	ReviewRound              int    `json:"review_round"`
	CaseID                   string `json:"case_id"`
	CaseRef                  string `json:"case_ref"`
	CaseSHA256               string `json:"case_sha256"`
	ContractID               string `json:"contract_id"`
	ContractRef              string `json:"contract_ref"`
	ContractSHA256           string `json:"contract_sha256"`
	SessionID                string `json:"session_id"`
	SessionRef               string `json:"session_ref"`
	SessionSHA256            string `json:"session_sha256"`
	PlanID                   string `json:"plan_id"`
	PlanRef                  string `json:"plan_ref"`
	PlanSHA256               string `json:"plan_sha256"`
	RepairAssignmentID       string `json:"repair_assignment_id"`
	RepairAssignmentSHA256   string `json:"repair_assignment_sha256"`
	ManifestAssignmentID     string `json:"manifest_assignment_id"`
	ManifestAssignmentSHA256 string `json:"manifest_assignment_sha256"`
	TeamID                   string `json:"team_id"`
	TaskID                   string `json:"task_id"`
	TaskRef                  string `json:"task_ref"`
	TaskSHA256               string `json:"task_sha256"`
	ManifestRef              string `json:"manifest_ref"`
	ManifestSHA256           string `json:"manifest_sha256"`
	OwnerAgentID             string `json:"owner_agent_id"`
}

// RepairPlanReportInputs retains canonical source documents and the exact
// selected RepairPlan and manifest Assignment rows. Source bytes/SHA and
// current authorization must still be proved by the original S9 consumer.
type RepairPlanReportInputs struct {
	Case               json.RawMessage `json:"case"`
	Contract           json.RawMessage `json:"contract"`
	Session            json.RawMessage `json:"session"`
	Plan               json.RawMessage `json:"plan"`
	Task               json.RawMessage `json:"task"`
	RepairAssignment   json.RawMessage `json:"repair_assignment"`
	ManifestAssignment json.RawMessage `json:"manifest_assignment"`
	ProspectiveScope   []string        `json:"prospective_scope"`
	ForbiddenScope     []string        `json:"forbidden_scope"`
}

func (inputs *RepairPlanReportInputs) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairPlanReportInputs
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	for _, source := range []*json.RawMessage{
		&decoded.Case, &decoded.Contract, &decoded.Session, &decoded.Plan, &decoded.Task,
		&decoded.RepairAssignment, &decoded.ManifestAssignment,
	} {
		canonical, err := canonicalRepairPlanReportSource(*source)
		if err != nil {
			return err
		}
		*source = canonical
	}
	*inputs = RepairPlanReportInputs(decoded)
	return nil
}

type RepairPlanReportCheck struct {
	Name         string   `json:"name"`
	Command      string   `json:"command"`
	Result       string   `json:"result"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// RepairPlanReportProfessionalAnswer holds only author-owned planning
// judgment. IDs, assertions, approval, status, timestamps, and artifact SHA
// remain derived from the original dispatcher and receipt writer.
type RepairPlanReportProfessionalAnswer struct {
	Plan          string                  `json:"plan"`
	RedChecks     []RepairPlanReportCheck `json:"red_checks"`
	ProposedPaths []string                `json:"proposed_paths"`
}

type RepairPlanReportDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      json.RawMessage     `json:"answer"`
}

func (draft *RepairPlanReportDraft) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairPlanReportDraft
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	answer, err := canonicalJSON(decoded.Answer)
	if err != nil {
		return fmt.Errorf("S9 PlanReport draft answer: %w", err)
	}
	decoded.Answer = answer
	*draft = RepairPlanReportDraft(decoded)
	return nil
}

type RepairPlanReportPacket struct {
	ID              string                  `json:"packet_id"`
	TemplateID      string                  `json:"template_id"`
	TemplateSHA     string                  `json:"template_sha256"`
	Binding         RepairPlanReportBinding `json:"binding"`
	Inputs          RepairPlanReportInputs  `json:"inputs"`
	ReportID        string                  `json:"report_id"`
	IssuanceEventID string                  `json:"issuance_event_id"`
	ViewPath        string                  `json:"view_path"`
	AnswerPath      string                  `json:"answer_path"`
	Draft           RepairPlanReportDraft   `json:"draft"`
}

func (packet *RepairPlanReportPacket) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairPlanReportPacket
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	*packet = RepairPlanReportPacket(decoded)
	return nil
}

// BuildRepairPlanReport constructs the deterministic S9 pre-execution
// questionnaire from the trusted current dispatch/source projection.
func BuildRepairPlanReport(binding RepairPlanReportBinding, inputs RepairPlanReportInputs) (RepairPlanReportPacket, error) {
	templateBytes, err := schema.ReadAsset("stage-form-s9-plan-template.json")
	if err != nil {
		return RepairPlanReportPacket{}, err
	}
	return buildRepairPlanReportWithTemplate(binding, inputs, templateBytes)
}

func buildRepairPlanReportWithTemplate(binding RepairPlanReportBinding, inputs RepairPlanReportInputs, templateBytes []byte) (RepairPlanReportPacket, error) {
	if err := validateRepairPlanReportBinding(binding); err != nil {
		return RepairPlanReportPacket{}, err
	}
	canonicalInputs, err := canonicalRepairPlanReportInputs(binding, inputs)
	if err != nil {
		return RepairPlanReportPacket{}, err
	}
	var template struct {
		SchemaVersion string              `json:"schema_version"`
		Stage         string              `json:"stage"`
		Adapter       string              `json:"adapter"`
		Title         string              `json:"title"`
		Instruction   string              `json:"instruction"`
		Questions     map[string]Question `json:"questions"`
	}
	if err := decodeOneStrictJSON(templateBytes, &template); err != nil ||
		template.SchemaVersion != "1.0.0" || template.Stage != "S9" || template.Adapter != "repair_plan_report" ||
		strings.TrimSpace(template.Title) == "" || strings.TrimSpace(template.Instruction) == "" ||
		!repairPlanReportTemplateQuestions(template.Questions) {
		return RepairPlanReportPacket{}, fmt.Errorf("unregistered S9 PlanReport template")
	}
	templateSHA := Digest(templateBytes)
	identityBytes, err := json.Marshal(struct {
		Binding     RepairPlanReportBinding `json:"binding"`
		Inputs      RepairPlanReportInputs  `json:"inputs"`
		TemplateSHA string                  `json:"template_sha256"`
	}{binding, canonicalInputs, templateSHA})
	if err != nil {
		return RepairPlanReportPacket{}, fmt.Errorf("encode S9 PlanReport packet identity: %w", err)
	}
	identityBytes, err = canonicalJSON(identityBytes)
	if err != nil {
		return RepairPlanReportPacket{}, err
	}
	packetHash := Digest(identityBytes)
	packetID := "packet-" + packetHash
	base := fmt.Sprintf("%s/g%d/S9-plan/r%d/%s/%s", binding.RuntimeID, binding.BaselineGeneration, binding.ReviewRound, binding.RepairAssignmentID, packetID)
	draftAnswer, err := canonicalJSON([]byte(`{"plan":null,"red_checks":null,"proposed_paths":null}`))
	if err != nil {
		return RepairPlanReportPacket{}, err
	}
	packet := RepairPlanReportPacket{
		ID: packetID, TemplateID: repairPlanReportTemplateID, TemplateSHA: templateSHA,
		Binding: binding, Inputs: canonicalInputs,
		ReportID:        "repair-plan-report-" + packetHash,
		IssuanceEventID: "evt-repair-plan-report-packet-" + packetID,
		ViewPath:        ".claude/work-packets/" + base + ".json",
		AnswerPath:      ".claude/submissions/" + base + "/" + binding.OwnerAgentID + "/answer.json",
		Draft: RepairPlanReportDraft{
			Title: template.Title, Instruction: template.Instruction,
			Read: makeRepairPlanReportRead(binding), Questions: template.Questions, Answer: draftAnswer,
		},
	}
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		return RepairPlanReportPacket{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-plan-report-work-packet.schema.json", packetBytes); err != nil {
		return RepairPlanReportPacket{}, fmt.Errorf("generated S9 PlanReport packet violates its registered schema: %w", err)
	}
	return packet, nil
}

func RebuildRepairPlanReport(packet RepairPlanReportPacket) (RepairPlanReportPacket, error) {
	// An issued packet fixes the complete template bytes, not just the fields
	// visible in its mutable draft. New work always uses the current template.
	for _, name := range []string{"stage-form-s9-plan-template.json", "stage-form-s9-plan-template-before-natural-observation.json"} {
		data, err := schema.ReadAsset(name)
		if err != nil {
			return RepairPlanReportPacket{}, err
		}
		if Digest(data) == packet.TemplateSHA {
			return buildRepairPlanReportWithTemplate(packet.Binding, packet.Inputs, data)
		}
	}
	return RepairPlanReportPacket{}, fmt.Errorf("unsupported frozen S9 PlanReport template digest")
}

func RepairPlanReportPacketDigest(packet RepairPlanReportPacket) (string, error) {
	rebuilt, err := RebuildRepairPlanReport(packet)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(packet, rebuilt) {
		return "", fmt.Errorf("S9 PlanReport packet differs from its canonical rebuild")
	}
	data, err := json.Marshal(rebuilt)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	return Digest(canonical), nil
}

// ValidateRepairPlanReportAnswer returns the professional answer from the
// authoring file. Display metadata does not affect answer acceptance.
func ValidateRepairPlanReportAnswer(packet RepairPlanReportPacket, data []byte) (json.RawMessage, []FieldError) {
	issue := func(pointer, expected, observed string) (json.RawMessage, []FieldError) {
		return nil, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "填写 answer 中的 plan、red_checks 和 proposed_paths，记录本人实际计划与前检。"}}
	}
	if len(data) > repairPlanReportPacketLimit {
		return issue("/", "JSON <= 20 MiB", "oversized")
	}
	if err := ValidateUniqueJSON(data); err != nil {
		return issue("/", "unique JSON keys", err.Error())
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil || document == nil {
		return issue("/", "one JSON object containing answer", "invalid JSON object")
	}
	answer := document["answer"]
	canonicalAnswer, err := canonicalJSON(answer)
	if err != nil {
		return issue("/answer", "one JSON answer object", err.Error())
	}
	if pointer, want, observed := validateCompleteRepairPlanReportAnswer(packet.Inputs, canonicalAnswer); pointer != "" {
		return issue(pointer, want, observed)
	}
	return json.RawMessage(canonicalAnswer), nil
}

func validateCompleteRepairPlanReportAnswer(inputs RepairPlanReportInputs, data []byte) (pointer, expected, observed string) {
	var answer RepairPlanReportProfessionalAnswer
	if err := decodeOneStrictJSON(data, &answer); err != nil {
		return "/answer", "valid S9 professional fields", err.Error()
	}
	if strings.TrimSpace(answer.Plan) == "" {
		return "/answer/plan", "concrete pre-execution plan", "empty"
	}
	if len(answer.RedChecks) == 0 || len(answer.RedChecks) > 128 {
		return "/answer/red_checks", "one or more bounded red checks", "empty or oversized"
	}
	for i, check := range answer.RedChecks {
		prefix := fmt.Sprintf("/answer/red_checks/%d", i)
		if strings.TrimSpace(check.Name) == "" || strings.TrimSpace(check.Command) == "" {
			return prefix, "check name and command", "empty"
		}
		if check.Result != "fail" && check.Result != "pass" && check.Result != "blocked" {
			return prefix + "/result", "fail | pass | blocked", check.Result
		}
		if len(check.EvidenceRefs) == 0 {
			return prefix + "/evidence_refs", "at least one evidence reference", "empty"
		}
		seenRefs := map[string]bool{}
		for _, ref := range check.EvidenceRefs {
			if strings.TrimSpace(ref) == "" || ref != strings.TrimSpace(ref) || seenRefs[ref] {
				return prefix + "/evidence_refs", "unique non-empty evidence references", ref
			}
			seenRefs[ref] = true
		}
	}
	if answer.ProposedPaths == nil || len(answer.ProposedPaths) > 4096 {
		return "/answer/proposed_paths", "explicit path array; [] for no product changes", "missing, null or oversized"
	}
	seenPaths := map[string]bool{}
	for i, value := range answer.ProposedPaths {
		path := strings.TrimSpace(value)
		pointer := fmt.Sprintf("/answer/proposed_paths/%d", i)
		if path == "" || value != path || !safeRelative(path) || seenPaths[path] {
			return pointer, "unique safe relative proposed path", value
		}
		seenPaths[path] = true

	}
	return "", "", ""
}

func validateRepairPlanReportBinding(binding RepairPlanReportBinding) error {
	if !safeComponent(binding.RuntimeID) || binding.BaselineGeneration < 0 || binding.ReviewRound < 1 ||
		!repairPlanReportCaseIDRE.MatchString(binding.CaseID) || !safeRepairPlanReportRef(binding.CaseRef) || !digestPattern.MatchString(binding.CaseSHA256) ||
		!repairPlanReportContractIDRE.MatchString(binding.ContractID) || !safeRepairPlanReportRef(binding.ContractRef) || !digestPattern.MatchString(binding.ContractSHA256) ||
		!repairPlanReportSessionIDRE.MatchString(binding.SessionID) || !safeRepairPlanReportRef(binding.SessionRef) || !digestPattern.MatchString(binding.SessionSHA256) ||
		!repairPlanReportPlanIDRE.MatchString(binding.PlanID) || !safeRepairPlanReportRef(binding.PlanRef) || !digestPattern.MatchString(binding.PlanSHA256) ||
		!repairPlanReportAssignmentRE.MatchString(binding.RepairAssignmentID) || !digestPattern.MatchString(binding.RepairAssignmentSHA256) ||
		!repairPlanReportManifestIDRE.MatchString(binding.ManifestAssignmentID) || !digestPattern.MatchString(binding.ManifestAssignmentSHA256) ||
		!safeComponent(binding.TeamID) || !repairPlanReportTaskIDRE.MatchString(binding.TaskID) || !safeRepairPlanReportRef(binding.TaskRef) || !digestPattern.MatchString(binding.TaskSHA256) ||
		!safeRepairPlanReportRef(binding.ManifestRef) || !digestPattern.MatchString(binding.ManifestSHA256) || !safeComponent(binding.OwnerAgentID) {
		return fmt.Errorf("S9 PlanReport requires a complete pinned Case, Contract, Session, Plan, Assignment, TASK, manifest, and owner binding")
	}
	if err := identity.ValidateAgentID(binding.OwnerAgentID); err != nil {
		return fmt.Errorf("S9 PlanReport owner: %w", err)
	}
	return nil
}

func canonicalRepairPlanReportInputs(binding RepairPlanReportBinding, inputs RepairPlanReportInputs) (RepairPlanReportInputs, error) {
	canonical := inputs
	if canonical.ProspectiveScope == nil || canonical.ForbiddenScope == nil {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 prospective_scope and forbidden_scope must be explicit JSON arrays")
	}
	canonical.ProspectiveScope = append([]string{}, inputs.ProspectiveScope...)
	canonical.ForbiddenScope = append([]string{}, inputs.ForbiddenScope...)
	for _, source := range []struct {
		name string
		data *json.RawMessage
	}{
		{"Case", &canonical.Case}, {"Contract", &canonical.Contract}, {"Session", &canonical.Session},
		{"Plan", &canonical.Plan}, {"TASK", &canonical.Task}, {"RepairAssignment", &canonical.RepairAssignment},
		{"manifest Assignment", &canonical.ManifestAssignment},
	} {
		value, err := canonicalRepairPlanReportSource(*source.data)
		if err != nil {
			return RepairPlanReportInputs{}, fmt.Errorf("S9 %s source: %w", source.name, err)
		}
		*source.data = value
	}
	if len(canonical.Case)+len(canonical.Contract)+len(canonical.Session)+len(canonical.Plan)+len(canonical.Task)+len(canonical.RepairAssignment)+len(canonical.ManifestAssignment) > repairPlanReportPacketLimit {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 source closure exceeds 20 MiB")
	}
	for _, source := range []struct {
		name   string
		data   json.RawMessage
		schema string
	}{
		{"Case", canonical.Case, "review-investigation-case.schema.json"},
		{"Contract", canonical.Contract, "repair-contract.schema.json"},
		{"Session", canonical.Session, "repair-session.schema.json"},
		{"RepairPlan", canonical.Plan, "repair-plan.schema.json"},
	} {
		if err := schema.NewEmbeddedValidator().ValidateBytes(source.schema, source.data); err != nil {
			return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen %s schema: %w", source.name, err)
		}
	}
	if err := canonicalRepairPlanReportScope(canonical.ProspectiveScope, false); err != nil {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 prospective scope: %w", err)
	}
	if err := canonicalRepairPlanReportScope(canonical.ForbiddenScope, true); err != nil {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 forbidden scope: %w", err)
	}
	caseDoc, err := repairPlanReportObject(canonical.Case, "Case")
	if err != nil || caseDoc["case_id"] != binding.CaseID || caseDoc["status"] != "contract_approved" || caseDoc["repair_contract_ref"] != binding.ContractRef || caseDoc["repair_contract_sha256"] != binding.ContractSHA256 {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen Case is not the approved binding source")
	}
	contract, err := repairPlanReportObject(canonical.Contract, "Contract")
	if err != nil || contract["repair_contract_id"] != binding.ContractID || contract["case_id"] != binding.CaseID || contract["status"] != "approved" {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen Contract identity/approval differs from binding")
	}
	session, err := repairPlanReportObject(canonical.Session, "Session")
	if err != nil || session["session_id"] != binding.SessionID || session["contract_ref"] != binding.ContractRef || session["contract_sha256"] != binding.ContractSHA256 || session["runtime_id"] != binding.RuntimeID || !repairPlanReportJSONEqual(session["baseline_generation"], binding.BaselineGeneration) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen Session identity/source tuple differs from binding")
	}
	plan, err := repairPlanReportObject(canonical.Plan, "Plan")
	if err != nil || plan["plan_id"] != binding.PlanID || plan["session_id"] != binding.SessionID || plan["contract_id"] != binding.ContractID || plan["contract_ref"] != binding.ContractRef || plan["contract_sha256"] != binding.ContractSHA256 {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen RepairPlan identity/source tuple differs from binding")
	}
	planProspective, ok := repairPlanReportStringSlice(plan["prospective_scope"])
	if !ok || !reflect.DeepEqual(planProspective, canonical.ProspectiveScope) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 prospective scope differs from frozen RepairPlan")
	}
	planForbidden, ok := repairPlanReportStringSlice(plan["forbidden_scope"])
	if !ok || !reflect.DeepEqual(planForbidden, canonical.ForbiddenScope) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 forbidden scope differs from frozen RepairPlan")
	}
	contractProspective, ok := repairPlanReportStringSlice(contract["prospective_scope"])
	if !ok || !reflect.DeepEqual(contractProspective, canonical.ProspectiveScope) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 prospective scope differs from approved Contract")
	}
	contractForbidden, ok := repairPlanReportStringSlice(contract["forbidden_scope"])
	if !ok || !reflect.DeepEqual(contractForbidden, canonical.ForbiddenScope) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 forbidden scope differs from approved Contract")
	}
	repairAssignment, err := repairPlanReportObject(canonical.RepairAssignment, "RepairAssignment")
	if err != nil || repairAssignment["assignment_id"] != binding.RepairAssignmentID || repairAssignment["contract_ref"] != binding.ContractRef || Digest(canonical.RepairAssignment) != binding.RepairAssignmentSHA256 {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 exact RepairAssignment identity/digest differs from binding")
	}
	task, err := repairPlanReportObject(canonical.Task, "TASK")
	if err != nil || task["task_id"] != binding.TaskID || task["session_id"] != binding.SessionID || task["plan_id"] != binding.PlanID || task["assignment_id"] != binding.RepairAssignmentID {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen TASK identity/source tuple differs from binding")
	}
	if !repairPlanReportJSONEqual(task["unit_ids"], repairAssignment["unit_ids"]) || !repairPlanReportJSONEqual(task["scope"], repairAssignment["scope"]) || !repairPlanReportOptionalStringsEqual(task["depends_on"], repairAssignment["depends_on"]) || !repairPlanReportOptionalStringsEqual(task["resource_locks"], repairAssignment["resource_locks"]) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen TASK unit/scope/dependency/lock projection differs from its RepairAssignment")
	}
	manifestAssignment, err := repairPlanReportObject(canonical.ManifestAssignment, "manifest Assignment")
	if err != nil || manifestAssignment["assignment_id"] != binding.ManifestAssignmentID || manifestAssignment["agent_id"] != binding.OwnerAgentID || Digest(canonical.ManifestAssignment) != binding.ManifestAssignmentSHA256 || !repairPlanReportJSONEqual(manifestAssignment["scope"], repairAssignment["scope"]) {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 exact manifest Assignment identity/digest differs from binding")
	}
	assignments, ok := plan["assignments"].([]any)
	if !ok {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 RepairPlan assignments are missing")
	}
	matched := 0
	for _, raw := range assignments {
		rowBytes, err := json.Marshal(raw)
		if err != nil {
			return RepairPlanReportInputs{}, err
		}
		rowCanonical, err := canonicalJSON(rowBytes)
		if err != nil {
			return RepairPlanReportInputs{}, err
		}
		var row map[string]any
		if err := json.Unmarshal(rowCanonical, &row); err != nil {
			return RepairPlanReportInputs{}, err
		}
		if row["assignment_id"] == binding.RepairAssignmentID {
			if !bytes.Equal(rowCanonical, canonical.RepairAssignment) {
				return RepairPlanReportInputs{}, fmt.Errorf("S9 frozen RepairAssignment differs from its exact RepairPlan row")
			}
			matched++
		}
	}
	if matched != 1 {
		return RepairPlanReportInputs{}, fmt.Errorf("S9 RepairPlan does not contain exactly one bound RepairAssignment")
	}
	return canonical, nil
}

func canonicalRepairPlanReportSource(data json.RawMessage) (json.RawMessage, error) {
	if len(data) == 0 || len(data) > repairPlanReportSourceDocLimit {
		return nil, fmt.Errorf("source must be non-empty and no larger than 4 MiB")
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
		return nil, fmt.Errorf("source must be a JSON object")
	}
	return json.RawMessage(canonical), nil
}

func repairPlanReportObject(raw json.RawMessage, label string) (map[string]any, error) {
	if err := ValidateUniqueJSON(raw); err != nil {
		return nil, err
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("%s must be one JSON object", label)
	}
	return value, nil
}

func repairPlanReportStringSlice(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		result := make([]string, len(values))
		for i, raw := range values {
			text, ok := raw.(string)
			if !ok {
				return nil, false
			}
			result[i] = text
		}
		return result, true
	default:
		return nil, false
	}
}

func repairPlanReportOptionalStringsEqual(left, right any) bool {
	leftStrings, leftOK := repairPlanReportOptionalStringSlice(left)
	rightStrings, rightOK := repairPlanReportOptionalStringSlice(right)
	return leftOK && rightOK && reflect.DeepEqual(leftStrings, rightStrings)
}

func repairPlanReportOptionalStringSlice(value any) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	return repairPlanReportStringSlice(value)
}

func repairPlanReportJSONEqual(left, right any) bool {
	data, err := json.Marshal(left)
	if err != nil {
		return false
	}
	leftJSON, err := canonicalJSON(data)
	if err != nil {
		return false
	}
	data, err = json.Marshal(right)
	if err != nil {
		return false
	}
	rightJSON, err := canonicalJSON(data)
	return err == nil && bytes.Equal(leftJSON, rightJSON)
}

func canonicalRepairPlanReportScope(values []string, allowEmpty bool) error {
	if (!allowEmpty && len(values) == 0) || len(values) > 4096 {
		return fmt.Errorf("scope must be non-empty and bounded")
	}
	seen := map[string]bool{}
	for _, value := range values {
		// S8 scope matching already treats one terminal directory slash as
		// the same boundary. Preserve the approved bytes, but validate and
		// deduplicate that boundary here instead of stranding its S9 handoff.
		key := strings.TrimSuffix(value, "/")
		if strings.TrimSpace(value) != value || key == "" || value != key && (key == "all" || key == ".") || key != "all" && !safeRelative(key) || seen[key] {
			return fmt.Errorf("scope contains an invalid or duplicate path %q", value)
		}
		seen[key] = true
	}
	return nil
}

func repairPlanReportPathInScope(path string, rules []string) bool {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(path))))
	for _, rule := range rules {
		rule = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(rule))))
		if rule == "all" || path == rule || strings.HasPrefix(path, strings.TrimSuffix(rule, "/")+"/") {
			return true
		}
	}
	return false
}

func safeRepairPlanReportRef(value string) bool { return safeRelative(value) && value != "." }

func repairPlanReportTemplateQuestions(questions map[string]Question) bool {
	if len(questions) != 3 {
		return false
	}
	for _, field := range []string{"plan", "red_checks", "proposed_paths"} {
		question, exists := questions[field]
		if !exists || strings.TrimSpace(question.Question) == "" {
			return false
		}
	}
	return true
}

func makeRepairPlanReportRead(binding RepairPlanReportBinding) []ReadItem {
	refs := []struct{ path, purpose string }{
		{binding.CaseRef, "Read the exact approved Case that authorized this repair."},
		{binding.ContractRef, "Read the exact approved RepairContract and its assertions."},
		{binding.SessionRef, "Read the current RepairSession and its execution boundary."},
		{binding.PlanRef, "Read the exact RepairPlan and your assigned unit."},
		{binding.TaskRef, "Read the registered TASK bound to the original dispatch."},
		{binding.ManifestRef, "Read the registered Team manifest that dispatched this owner."},
	}
	read := make([]ReadItem, 0, len(refs))
	for _, ref := range refs {
		read = append(read, ReadItem{Path: ref.path, Purpose: ref.purpose})
	}
	return read
}
