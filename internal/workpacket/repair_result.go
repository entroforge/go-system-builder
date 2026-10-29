package workpacket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const repairResultTemplateID = "s9-repair-result-v1"

var (
	repairResultPacketLimit = 20 << 20
	repairResultSourceLimit = 4 << 20
	repairResultPacketIDRE  = regexp.MustCompile(`^packet-[a-f0-9]{64}$`)
	repairResultIDRE        = regexp.MustCompile(`^repair-result-[a-f0-9]{64}$`)
)

// RepairResultBinding freezes the original PlanReport authority and exact
// registered execution definition for one S9 result questionnaire. The
// neutral workpacket layer does not grant the authority represented here; the
// original repair/runtime consumer must reread and verify each source.
type RepairResultBinding struct {
	Sources          RepairPlanReportBinding `json:"sources"`
	PlanReportRef    string                  `json:"plan_report_ref"`
	PlanReportSHA256 string                  `json:"plan_report_sha256"`
	ExecutionSHA256  string                  `json:"execution_sha256"`
	// ReworkRef and ReworkSHA256 are deliberately present and empty on the
	// first attempt. A later attempt binds the exact immutable rework receipt.
	ReworkRef    string `json:"rework_ref"`
	ReworkSHA256 string `json:"rework_sha256"`
	// Resume binds a new answer attempt to one accepted Main blocker resolution.
	// It is omitted on original packets so their canonical bytes and identities
	// remain unchanged.
	Resume *RepairResultResume `json:"resume,omitempty"`
}

// RepairResultResume is the immutable, source-derived lineage from the prior
// packet's blocker through the accepted Main resolution. It contains facts,
// not permission; runtime readers must reconstruct and verify the original
// packet, evidence and journal event before treating it as current.
type RepairResultResume struct {
	PredecessorPacketID      string `json:"predecessor_packet_id"`
	PredecessorPacketSHA256  string `json:"predecessor_packet_sha256"`
	BlockerRef               string `json:"blocker_ref"`
	BlockerSHA256            string `json:"blocker_sha256"`
	ResolutionMessageRef     string `json:"resolution_message_ref"`
	ResolutionMessageSHA256  string `json:"resolution_message_sha256"`
	ResolutionEventID        string `json:"resolution_event_id"`
	ResolutionIdempotencyKey string `json:"resolution_idempotency_key"`
	ResolutionBeforeRevision int    `json:"resolution_before_revision"`
	ResolutionAfterRevision  int    `json:"resolution_after_revision"`
	ResolutionSequence       int    `json:"resolution_sequence"`
}

// RepairResultInputs retains canonical source documents and the neutral,
// closed projection of the registered workspace.Execution definition.
// Mutable execution progress (status, tool receipts, and delivery pointers)
// is excluded and must be reread by the original consumer at acceptance.
type RepairResultInputs struct {
	Sources       RepairPlanReportInputs `json:"sources"`
	PlanReport    json.RawMessage        `json:"plan_report"`
	Execution     json.RawMessage        `json:"execution"`
	ReworkReceipt json.RawMessage        `json:"rework_receipt"`
}

func (inputs *RepairResultInputs) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairResultInputs
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	for _, source := range []struct {
		name string
		raw  *json.RawMessage
	}{
		{"PlanReport", &decoded.PlanReport},
		{"Execution", &decoded.Execution},
	} {
		canonical, err := canonicalRepairResultObject(*source.raw, source.name)
		if err != nil {
			return err
		}
		*source.raw = canonical
	}
	rework, err := canonicalRepairResultNullableObject(decoded.ReworkReceipt, "ReworkReceipt")
	if err != nil {
		return err
	}
	decoded.ReworkReceipt = rework
	*inputs = RepairResultInputs(decoded)
	return nil
}

// RepairResultBeforeFixResult records an author's direct professional
// observation for one exact frozen Contract assertion. It is not synthesized
// from PlanReport command output.
type RepairResultBeforeFixResult struct {
	AssertionID  string   `json:"assertion_id"`
	ObservedRed  string   `json:"observed_red"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// RepairResultUnitResult records the professional implementation conclusion
// for one exact assigned unit. Machine-derived changes and checks are not part
// of this type.
type RepairResultUnitResult struct {
	UnitID                string   `json:"unit_id"`
	Status                string   `json:"status"`
	EvidenceRefs          []string `json:"evidence_refs"`
	ImplementationSummary string   `json:"implementation_summary"`
	RestoredInvariantRefs []string `json:"restored_invariant_refs"`
}

// RepairResultProfessionalAnswer contains only author-owned S9 professional
// conclusions and references. The original domain writer maps verdicts and
// performs candidate/Main acceptance; this neutral type preserves all four
// verdict values without implying receipt or completion.
type RepairResultProfessionalAnswer struct {
	Verdict               string                        `json:"verdict"`
	BeforeFixResults      []RepairResultBeforeFixResult `json:"before_fix_results"`
	UnitResults           []RepairResultUnitResult      `json:"unit_results"`
	DetectionGapArtifacts []string                      `json:"detection_gap_artifacts"`
	EvidenceRefs          []string                      `json:"evidence_refs"`
	ScopeDeviations       []string                      `json:"scope_deviations"`
	ResidualRisks         []string                      `json:"residual_risks"`
	MigrationRef          string                        `json:"migration_ref"`
	RollbackRef           string                        `json:"rollback_ref"`
}

type RepairResultDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      json.RawMessage     `json:"answer"`
}

func (draft *RepairResultDraft) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairResultDraft
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	answer, err := canonicalJSON(decoded.Answer)
	if err != nil {
		return fmt.Errorf("S9 RepairResult draft answer: %w", err)
	}
	decoded.Answer = answer
	*draft = RepairResultDraft(decoded)
	return nil
}

// RepairResultPacket is the immutable neutral S9 result questionnaire. The
// stable ResultID and original issuance event/path identities are derived
// from the packet's full source binding.
type RepairResultPacket struct {
	ID              string              `json:"packet_id"`
	TemplateID      string              `json:"template_id"`
	TemplateSHA     string              `json:"template_sha256"`
	Binding         RepairResultBinding `json:"binding"`
	Inputs          RepairResultInputs  `json:"inputs"`
	ResultID        string              `json:"result_id"`
	IssuanceEventID string              `json:"issuance_event_id"`
	ViewPath        string              `json:"view_path"`
	AnswerPath      string              `json:"answer_path"`
	Draft           RepairResultDraft   `json:"draft"`
}

func (packet *RepairResultPacket) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairResultPacket
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	*packet = RepairResultPacket(decoded)
	return nil
}

// BuildRepairResult constructs a deterministic S9 authoring packet from the
// current, trusted PlanReport/Execution projection. It does not verify live
// source authority; the original S9 issuer and consumer do that.
func BuildRepairResult(binding RepairResultBinding, inputs RepairResultInputs) (RepairResultPacket, error) {
	templateBytes, err := schema.ReadAsset("stage-form-s9-result-template.json")
	if err != nil {
		return RepairResultPacket{}, err
	}
	return buildRepairResultWithTemplate(binding, inputs, templateBytes)
}

func buildRepairResultWithTemplate(binding RepairResultBinding, inputs RepairResultInputs, templateBytes []byte) (RepairResultPacket, error) {
	if err := validateRepairResultBinding(binding); err != nil {
		return RepairResultPacket{}, err
	}
	canonicalInputs, err := canonicalRepairResultInputs(binding, inputs)
	if err != nil {
		return RepairResultPacket{}, err
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
		template.SchemaVersion != "1.0.0" || template.Stage != "S9" || template.Adapter != "repair_result" ||
		strings.TrimSpace(template.Title) == "" || strings.TrimSpace(template.Instruction) == "" ||
		!repairResultTemplateQuestions(template.Questions) {
		return RepairResultPacket{}, fmt.Errorf("unregistered S9 RepairResult template")
	}
	templateSHA := Digest(templateBytes)
	identityData, err := json.Marshal(struct {
		Binding     RepairResultBinding `json:"binding"`
		Inputs      RepairResultInputs  `json:"inputs"`
		TemplateSHA string              `json:"template_sha256"`
	}{binding, canonicalInputs, templateSHA})
	if err != nil {
		return RepairResultPacket{}, fmt.Errorf("encode S9 RepairResult packet identity: %w", err)
	}
	canonicalIdentity, err := canonicalJSON(identityData)
	if err != nil {
		return RepairResultPacket{}, err
	}
	packetHash := Digest(canonicalIdentity)
	packetID := "packet-" + packetHash
	resultID := "repair-result-" + packetHash
	if !repairResultPacketIDRE.MatchString(packetID) || !repairResultIDRE.MatchString(resultID) {
		return RepairResultPacket{}, fmt.Errorf("derived S9 RepairResult packet/result identity is invalid")
	}
	base := fmt.Sprintf("%s/g%d/S9-result/r%d/%s/%s", binding.Sources.RuntimeID, binding.Sources.BaselineGeneration, binding.Sources.ReviewRound, binding.Sources.RepairAssignmentID, packetID)
	answer, err := canonicalJSON([]byte(`{"verdict":null,"before_fix_results":null,"unit_results":null,"detection_gap_artifacts":null,"evidence_refs":null,"scope_deviations":null,"residual_risks":null,"migration_ref":null,"rollback_ref":null}`))
	if err != nil {
		return RepairResultPacket{}, err
	}
	read := makeRepairResultRead(binding, canonicalInputs)
	packet := RepairResultPacket{
		ID: packetID, TemplateID: repairResultTemplateID, TemplateSHA: templateSHA,
		Binding: binding, Inputs: canonicalInputs,
		ResultID:        resultID,
		IssuanceEventID: "evt-repair-result-packet-" + packetID,
		ViewPath:        ".claude/work-packets/" + base + ".json",
		AnswerPath:      ".claude/submissions/" + base + "/" + binding.Sources.OwnerAgentID + "/answer.json",
		Draft: RepairResultDraft{
			Title: template.Title, Instruction: template.Instruction,
			Read: read, Questions: template.Questions, Answer: answer,
		},
	}
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		return RepairResultPacket{}, err
	}
	if len(packetBytes) > repairResultPacketLimit {
		return RepairResultPacket{}, fmt.Errorf("S9 RepairResult packet exceeds 20 MiB")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-work-packet.schema.json", packetBytes); err != nil {
		return RepairResultPacket{}, fmt.Errorf("generated S9 RepairResult packet violates its registered schema: %w", err)
	}
	return packet, nil
}

// RebuildRepairResult recomputes every machine-controlled field from the
// original embedded template and frozen source documents. New work uses the
// current template; unknown historical digests cannot manufacture a template.
func RebuildRepairResult(packet RepairResultPacket) (RepairResultPacket, error) {
	for _, name := range []string{"stage-form-s9-result-template.json", "stage-form-s9-result-template-before-natural-observation.json"} {
		data, err := schema.ReadAsset(name)
		if err != nil {
			return RepairResultPacket{}, err
		}
		if Digest(data) == packet.TemplateSHA {
			return buildRepairResultWithTemplate(packet.Binding, packet.Inputs, data)
		}
	}
	return RepairResultPacket{}, fmt.Errorf("unsupported frozen S9 RepairResult template digest")
}

// RepairResultPacketDigest returns the canonical digest of a complete packet
// only when it exactly matches its deterministic rebuild.
func RepairResultPacketDigest(packet RepairResultPacket) (string, error) {
	rebuilt, err := RebuildRepairResult(packet)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(packet, rebuilt) {
		return "", fmt.Errorf("S9 RepairResult packet differs from its canonical rebuild")
	}
	return RepairResultPacketContentDigest(rebuilt)
}

// RepairResultPacketContentDigest hashes the canonical serialized packet without
// rebuilding its inputs. Callers must already have verified canonical packet
// identity through RepairResultPackets or RebuildRepairResult.
func RepairResultPacketContentDigest(packet RepairResultPacket) (string, error) {
	data, err := json.Marshal(packet)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	return Digest(canonical), nil
}

// ValidateRepairResultAnswer returns the professional answer from the
// authoring file. Display metadata does not affect answer acceptance.
func ValidateRepairResultAnswer(packet RepairResultPacket, data []byte) (json.RawMessage, []FieldError) {
	issue := func(pointer, expected, observed string) (json.RawMessage, []FieldError) {
		return nil, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "填写 answer 中的专业观测、实施结论与资料引用。"}}
	}
	if len(data) > repairResultPacketLimit {
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
		return issue("/answer", "one JSON professional answer object", err.Error())
	}
	if pointer, want, observed := validateCompleteRepairResultAnswer(packet.Inputs.Sources, canonicalAnswer); pointer != "" {
		return issue(pointer, want, observed)
	}
	return json.RawMessage(canonicalAnswer), nil
}

func validateCompleteRepairResultAnswer(sources RepairPlanReportInputs, data []byte) (pointer, expected, observed string) {
	var answer RepairResultProfessionalAnswer
	if err := decodeOneStrictJSON(data, &answer); err != nil {
		return "/answer", "valid S9 professional fields", err.Error()
	}
	var answerFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &answerFields); err != nil {
		return "/answer", "valid S9 professional object", err.Error()
	}
	for _, name := range []string{"verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs", "scope_deviations", "residual_risks", "migration_ref", "rollback_ref"} {
		if len(answerFields[name]) == 0 {
			return "/answer/" + name, "explicit professional field", "missing"
		}
		if bytes.Equal(bytes.TrimSpace(answerFields[name]), []byte("null")) {
			return "/answer/" + name, "explicit completed answer value, not null", "null"
		}
	}
	if answer.Verdict != "completed" && answer.Verdict != "blocked" && answer.Verdict != "contract_conflict" && answer.Verdict != "stale" {
		return "/answer/verdict", "completed | blocked | contract_conflict | stale", answer.Verdict
	}
	if answer.BeforeFixResults == nil || answer.UnitResults == nil || answer.DetectionGapArtifacts == nil || answer.EvidenceRefs == nil || answer.ScopeDeviations == nil || answer.ResidualRisks == nil {
		return "/answer", "all professional arrays explicitly supplied", "one or more arrays are null"
	}
	assignment, err := repairPlanReportObject(sources.RepairAssignment, "RepairAssignment")
	if err != nil {
		return "/inputs/sources/repair_assignment", "canonical exact RepairAssignment", err.Error()
	}
	assertions, ok := repairPlanReportStringSlice(assignment["assertion_ids"])
	if !ok || len(assertions) == 0 {
		return "/inputs/sources/repair_assignment/assertion_ids", "explicit exact assertion slots", "missing or empty"
	}
	if containsString(assertions, "all") {
		return "/inputs/sources/repair_assignment/assertion_ids", "concrete assertion IDs", "wildcard all is not an answer slot"
	}
	if len(answer.BeforeFixResults) != len(assertions) {
		return "/answer/before_fix_results", "one result for every exact Assignment assertion", fmt.Sprintf("got %d want %d", len(answer.BeforeFixResults), len(assertions))
	}
	for i, result := range answer.BeforeFixResults {
		prefix := fmt.Sprintf("/answer/before_fix_results/%d", i)
		if result.AssertionID != assertions[i] {
			return prefix + "/assertion_id", "exact frozen assertion_id in source order", result.AssertionID
		}
		if strings.TrimSpace(result.ObservedRed) == "" {
			return prefix + "/observed_red", "non-empty actual before-fix observation or unfinished-work reason", "empty"
		}
		if err := validateRepairResultRefs(result.EvidenceRefs, prefix+"/evidence_refs", true); err != nil {
			return err.pointer, "unique non-empty evidence references", err.observed
		}
	}
	unitIDs, ok := repairPlanReportStringSlice(assignment["unit_ids"])
	if !ok || len(unitIDs) == 0 {
		return "/inputs/sources/repair_assignment/unit_ids", "explicit exact assigned unit slots", "missing or empty"
	}
	if len(answer.UnitResults) != len(unitIDs) {
		return "/answer/unit_results", "one result for every exact assigned unit", fmt.Sprintf("got %d want %d", len(answer.UnitResults), len(unitIDs))
	}
	for i, result := range answer.UnitResults {
		prefix := fmt.Sprintf("/answer/unit_results/%d", i)
		if result.UnitID != unitIDs[i] {
			return prefix + "/unit_id", "exact frozen unit_id in source order", result.UnitID
		}
		if result.Status != "pass" && result.Status != "fail" && result.Status != "blocked" {
			return prefix + "/status", "pass | fail | blocked", result.Status
		}
		if strings.TrimSpace(result.ImplementationSummary) == "" {
			return prefix + "/implementation_summary", "non-empty professional implementation summary", "empty"
		}
		if err := validateRepairResultRefs(result.EvidenceRefs, prefix+"/evidence_refs", true); err != nil {
			return err.pointer, "unique non-empty evidence references", err.observed
		}
		if err := validateRepairResultRefs(result.RestoredInvariantRefs, prefix+"/restored_invariant_refs", false); err != nil {
			return err.pointer, "unique non-empty invariant references", err.observed
		}
	}
	for _, field := range []struct {
		name string
		refs []string
	}{
		{"detection_gap_artifacts", answer.DetectionGapArtifacts},
		{"evidence_refs", answer.EvidenceRefs},
		{"scope_deviations", answer.ScopeDeviations},
		{"residual_risks", answer.ResidualRisks},
	} {
		if err := validateRepairResultRefs(field.refs, "/answer/"+field.name, false); err != nil {
			return err.pointer, "unique non-empty strings", err.observed
		}
	}
	return "", "", ""
}

type repairResultValidationError struct {
	pointer  string
	observed string
}

func (e *repairResultValidationError) Error() string { return e.pointer + ": " + e.observed }

func validateRepairResultRefs(values []string, pointer string, requireOne bool) *repairResultValidationError {
	if requireOne && len(values) == 0 {
		return &repairResultValidationError{pointer: pointer, observed: "empty"}
	}
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || seen[value] {
			return &repairResultValidationError{pointer: pointer, observed: value}
		}
		seen[value] = true
	}
	return nil
}

func canonicalRepairResultInputs(binding RepairResultBinding, inputs RepairResultInputs) (RepairResultInputs, error) {
	// These documents are already accepted facts used to populate the draft.
	// Canonicalize their readable JSON without replaying their source proofs.
	canonicalSources := inputs.Sources
	canonicalSources.ProspectiveScope = append([]string{}, inputs.Sources.ProspectiveScope...)
	canonicalSources.ForbiddenScope = append([]string{}, inputs.Sources.ForbiddenScope...)
	for _, source := range []struct {
		name string
		raw  *json.RawMessage
	}{
		{"Case", &canonicalSources.Case}, {"Contract", &canonicalSources.Contract},
		{"Session", &canonicalSources.Session}, {"Plan", &canonicalSources.Plan},
		{"Task", &canonicalSources.Task}, {"RepairAssignment", &canonicalSources.RepairAssignment},
		{"ManifestAssignment", &canonicalSources.ManifestAssignment},
	} {
		value, err := canonicalRepairResultObject(*source.raw, source.name)
		if err != nil {
			return RepairResultInputs{}, err
		}
		*source.raw = value
	}
	canonical := RepairResultInputs{Sources: canonicalSources}
	var err error
	canonical.PlanReport, err = canonicalRepairResultObject(inputs.PlanReport, "PlanReport")
	if err != nil {
		return RepairResultInputs{}, err
	}
	canonical.Execution, err = canonicalRepairResultObject(inputs.Execution, "Execution")
	if err != nil {
		return RepairResultInputs{}, err
	}
	canonical.ReworkReceipt, err = canonicalRepairResultNullableObject(inputs.ReworkReceipt, "ReworkReceipt")
	if err != nil {
		return RepairResultInputs{}, err
	}
	if len(canonical.PlanReport)+len(canonical.Execution)+len(canonical.ReworkReceipt) > repairResultPacketLimit {
		return RepairResultInputs{}, fmt.Errorf("S9 RepairResult frozen input closure exceeds 20 MiB")
	}
	if err := validateRepairResultExecution(binding, canonical.Execution); err != nil {
		return RepairResultInputs{}, err
	}
	if err := validateRepairResultPlanReport(binding, canonical.PlanReport); err != nil {
		return RepairResultInputs{}, err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return RepairResultInputs{}, err
	}
	if len(encoded) > repairResultPacketLimit {
		return RepairResultInputs{}, fmt.Errorf("S9 RepairResult frozen inputs exceed 20 MiB")
	}
	return canonical, nil
}

func validateRepairResultBinding(binding RepairResultBinding) error {
	if err := validateRepairPlanReportBinding(binding.Sources); err != nil {
		return fmt.Errorf("S9 RepairResult original source binding: %w", err)
	}
	if !safeRepairPlanReportRef(binding.PlanReportRef) || !digestPattern.MatchString(binding.PlanReportSHA256) || !digestPattern.MatchString(binding.ExecutionSHA256) {
		return fmt.Errorf("S9 RepairResult requires exact PlanReport and Execution source digests")
	}
	if (binding.ReworkRef == "") != (binding.ReworkSHA256 == "") {
		return fmt.Errorf("S9 ReworkRef and ReworkSHA256 must be both empty or both present")
	}
	if binding.ReworkRef != "" && (!safeRepairPlanReportRef(binding.ReworkRef) || !digestPattern.MatchString(binding.ReworkSHA256)) {
		return fmt.Errorf("S9 rework attempt source reference or digest is invalid")
	}
	if err := ValidateRepairResultResume(binding.Sources.OwnerAgentID, binding.Resume); err != nil {
		return err
	}
	return nil
}

// ValidateRepairResultResume checks the current recorded recovery association.
// Historical packet, evidence and journal proofs are not acceptance conditions.
func ValidateRepairResultResume(owner string, resume *RepairResultResume) error {
	if resume == nil {
		return nil
	}
	if owner == "" || resume.PredecessorPacketID == "" || !safeRepairPlanReportRef(resume.BlockerRef) || !safeRepairPlanReportRef(resume.ResolutionMessageRef) {
		return fmt.Errorf("S9 Resume requires its predecessor, blocker and recorded resolution")
	}
	return nil
}

// RepairResultResumeSHA256 returns the canonical digest used by candidate and
// final provenance. An absent Resume is represented by an empty digest.
func RepairResultResumeSHA256(resume *RepairResultResume) (string, error) {
	if resume == nil {
		return "", nil
	}
	data, err := json.Marshal(resume)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	return Digest(canonical), nil
}

func validateRepairResultPlanReport(binding RepairResultBinding, raw json.RawMessage) error {
	report, err := repairPlanReportObject(raw, "accepted PlanReport")
	if err != nil {
		return err
	}
	if report["session_id"] != binding.Sources.SessionID ||
		report["plan_id"] != binding.Sources.PlanID || report["assignment_id"] != binding.Sources.RepairAssignmentID ||
		report["agent_id"] != binding.Sources.OwnerAgentID {
		return fmt.Errorf("S9 PlanReport differs from the current repair responsibility")
	}
	return nil
}

func validateRepairResultExecution(binding RepairResultBinding, raw json.RawMessage) error {
	var execution struct {
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
		AssignmentID       string `json:"assignment_id"`
		AgentID            string `json:"agent_id"`
	}
	if err := json.Unmarshal(raw, &execution); err != nil {
		return fmt.Errorf("decode S9 Execution definition: %w", err)
	}
	if execution.RuntimeID != binding.Sources.RuntimeID || execution.BaselineGeneration != binding.Sources.BaselineGeneration ||
		execution.AssignmentID != binding.Sources.ManifestAssignmentID || execution.AgentID != binding.Sources.OwnerAgentID {
		return fmt.Errorf("S9 Execution differs from the current repair responsibility")
	}
	return nil
}

func canonicalRepairResultObject(raw json.RawMessage, name string) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > repairResultSourceLimit {
		return nil, fmt.Errorf("S9 %s source must be non-empty and no larger than 4 MiB", name)
	}
	if err := ValidateUniqueJSON(raw); err != nil {
		return nil, fmt.Errorf("S9 %s source: %w", name, err)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("S9 %s source: %w", name, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
		return nil, fmt.Errorf("S9 %s source must be a JSON object", name)
	}
	return json.RawMessage(canonical), nil
}

func canonicalRepairResultNullableObject(raw json.RawMessage, name string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("S9 %s source must be explicit (object or null)", name)
	}
	if err := ValidateUniqueJSON(raw); err != nil {
		return nil, fmt.Errorf("S9 %s source: %w", name, err)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("S9 %s source: %w", name, err)
	}
	if bytes.Equal(canonical, []byte("null")) {
		return json.RawMessage("null"), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
		return nil, fmt.Errorf("S9 %s source must be an object or null", name)
	}
	return json.RawMessage(canonical), nil
}

func repairResultTemplateQuestions(questions map[string]Question) bool {
	fields := []string{"verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs", "scope_deviations", "residual_risks", "migration_ref", "rollback_ref"}
	if len(questions) != len(fields) {
		return false
	}
	for _, field := range fields {
		question, ok := questions[field]
		if !ok || strings.TrimSpace(question.Question) == "" {
			return false
		}
	}
	return true
}

func makeRepairResultRead(binding RepairResultBinding, inputs RepairResultInputs) []ReadItem {
	read := makeRepairPlanReportRead(binding.Sources)
	read = append(read, ReadItem{Path: binding.PlanReportRef, Purpose: "Read the exact PlanReport accepted by the original S9 writer."})
	var execution struct {
		Path string `json:"worktree_path"`
	}
	_ = decodeOneStrictJSON(inputs.Execution, &execution)
	if execution.Path != "" {
		read = append(read, ReadItem{Path: execution.Path, Purpose: "Work only in the originally registered Worker execution; current HEAD and changes are checked at candidate receipt."})
	}
	if binding.ReworkRef != "" {
		read = append(read, ReadItem{Path: binding.ReworkRef, Purpose: "Read the exact accepted integration rework receipt for this attempt."})
	}
	return read
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
