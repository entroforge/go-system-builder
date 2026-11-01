package workpacket

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/schema"
)

const hypothesisResultTemplateID = "s8-hypothesis-result-v1"

type HypothesisResultBinding struct {
	RuntimeID                  string   `json:"runtime_id"`
	BaselineGeneration         int      `json:"baseline_generation"`
	CaseID                     string   `json:"case_id"`
	CaseRef                    string   `json:"case_ref"`
	CaseRevision               int      `json:"case_revision"`
	SourceFindingIDs           []string `json:"source_finding_ids"`
	HypothesisID               string   `json:"hypothesis_id"`
	HypothesisSourceFindingIDs []string `json:"hypothesis_source_finding_ids"`
	AssignmentID               string   `json:"assignment_id"`
	TeamID                     string   `json:"team_id"`
	TaskID                     string   `json:"task_id"`
	TaskRef                    string   `json:"task_ref"`
	ManifestRef                string   `json:"manifest_ref"`
	OwnerAgentID               string   `json:"owner_agent_id"`
}

// HypothesisResultQuestion is the registered Case hypothesis row. Its
// identity and professional content retain their original meaning.
type HypothesisResultQuestion struct {
	HypothesisID     string                     `json:"hypothesis_id"`
	AssignmentID     string                     `json:"assignment_id"`
	Statement        string                     `json:"statement"`
	Invariant        string                     `json:"invariant"`
	Discriminator    string                     `json:"discriminator"`
	ExpectedOutcomes HypothesisExpectedOutcomes `json:"expected_outcomes"`
	SourceFindingIDs []string                   `json:"source_finding_ids"`
	EvidenceRefs     []string                   `json:"evidence_refs"`
	Status           string                     `json:"status"`
}

type HypothesisExpectedOutcomes struct {
	Support string `json:"support"`
	Refute  string `json:"refute"`
}

// HypothesisResultCaseRoute freezes the small current routing tuple that
// makes a Case eligible for another investigation result. Nil optional
// values are encoded as JSON null, including fields absent in older Case
// artifacts.
type HypothesisResultCaseRoute struct {
	Status          string  `json:"status"`
	Route           *string `json:"route"`
	RouteReason     *string `json:"route_reason"`
	RouteConsumedAt *string `json:"route_consumed_at"`
	RouteConsumer   *string `json:"route_consumer"`
}

type HypothesisResultFinding struct {
	FindingID string          `json:"finding_id"`
	Ref       string          `json:"ref"`
	Document  json.RawMessage `json:"document"`
}

func (finding *HypothesisResultFinding) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire HypothesisResultFinding
	var decoded wire
	if err := decodeOneJSON(data, &decoded); err != nil {
		return err
	}
	document, err := canonicalJSON(decoded.Document)
	if err != nil {
		return fmt.Errorf("S8 frozen Finding document: %w", err)
	}
	decoded.Document = document
	*finding = HypothesisResultFinding(decoded)
	return nil
}

// HypothesisEvidenceRef preserves an original evidence reference and its
// available responsibility context. References do not authenticate bytes or
// certify the investigation's professional conclusion.
type HypothesisEvidenceRef struct {
	Ref                string  `json:"ref"`
	Origin             string  `json:"origin"`
	SourceFindingID    string  `json:"source_finding_id,omitempty"`
	Attestation        string  `json:"attestation"`
	Kind               string  `json:"kind,omitempty"`
	Path               string  `json:"path,omitempty"`
	Status             string  `json:"status,omitempty"`
	BaselineGeneration *int    `json:"baseline_generation,omitempty"`
	InvalidatedBy      *string `json:"invalidated_by"`
}

type HypothesisResultInputs struct {
	Hypothesis   HypothesisResultQuestion  `json:"hypothesis"`
	CaseRoute    HypothesisResultCaseRoute `json:"case_route"`
	Findings     []HypothesisResultFinding `json:"findings"`
	EvidenceRefs []HypothesisEvidenceRef   `json:"evidence_refs"`
}

func (inputs *HypothesisResultInputs) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire HypothesisResultInputs
	var decoded wire
	if err := decodeOneJSON(data, &decoded); err != nil {
		return err
	}
	for i := range decoded.Findings {
		doc, err := canonicalJSON(decoded.Findings[i].Document)
		if err != nil {
			return fmt.Errorf("S8 Finding %d document: %w", i, err)
		}
		decoded.Findings[i].Document = doc
	}
	*inputs = HypothesisResultInputs(decoded)
	return nil
}

type HypothesisResultDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      json.RawMessage     `json:"answer"`
}

func (draft *HypothesisResultDraft) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire HypothesisResultDraft
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	answer, err := canonicalJSON(decoded.Answer)
	if err != nil {
		return fmt.Errorf("S8 professional draft answer: %w", err)
	}
	decoded.Answer = answer
	*draft = HypothesisResultDraft(decoded)
	return nil
}

// HypothesisLateResult is the original responsibility's accepted answer when
// another Case has become current. The current Case is never rewritten.
type HypothesisLateResult struct {
	Answer                       json.RawMessage `json:"answer"`
	ExecutionRoot                string          `json:"execution_root"`
	AcceptedEventID              string          `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string          `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int             `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int             `json:"accepted_sequence"`
}

type HypothesisResultPacket struct {
	LateResults     []HypothesisLateResult  `json:"late_results,omitempty"`
	ID              string                  `json:"packet_id"`
	TemplateID      string                  `json:"template_id"`
	Binding         HypothesisResultBinding `json:"binding"`
	Inputs          HypothesisResultInputs  `json:"inputs"`
	ResultID        string                  `json:"result_id"`
	IssuanceEventID string                  `json:"issuance_event_id"`
	ViewPath        string                  `json:"view_path"`
	AnswerPath      string                  `json:"answer_path"`
	Draft           HypothesisResultDraft   `json:"draft"`
}

func (packet *HypothesisResultPacket) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire HypothesisResultPacket
	var decoded wire
	if err := decodeOneJSON(data, &decoded); err != nil {
		return err
	}
	*packet = HypothesisResultPacket(decoded)
	return nil
}

type HypothesisNewQuestion struct {
	Statement        string                     `json:"statement"`
	Invariant        string                     `json:"invariant"`
	Discriminator    string                     `json:"discriminator"`
	ExpectedOutcomes HypothesisExpectedOutcomes `json:"expected_outcomes"`
	SourceFindingIDs []string                   `json:"source_finding_ids"`
	EvidenceRefs     []string                   `json:"evidence_refs"`
}

// HypothesisResultAnswer contains only original HypothesisResultRequest
// professional fields plus authored content for additional hypotheses. IDs,
// status, routing, causal model, repair contract, and approvals are omitted.
type HypothesisResultAnswer struct {
	Method                        string                  `json:"method"`
	EvidenceRefs                  []string                `json:"evidence_refs"`
	SourceBoundaryRefs            []string                `json:"source_boundary_refs"`
	Observed                      string                  `json:"observed"`
	CounterfactualOrDiscriminator string                  `json:"counterfactual_or_discriminator"`
	Result                        *string                 `json:"result"`
	ExplainsFindingIDs            []string                `json:"explains_finding_ids"`
	DoesNotExplain                []string                `json:"does_not_explain"`
	NewHypotheses                 []HypothesisNewQuestion `json:"new_hypotheses"`
}

// BuildHypothesisResult builds a stable questionnaire from a registered
// Hypothesis and its exact Case/Finding/dispatch source closure.
// The original accepted sources supply the professional question.
func BuildHypothesisResult(binding HypothesisResultBinding, inputs HypothesisResultInputs) (HypothesisResultPacket, error) {
	templateBytes, err := schema.ReadAsset("stage-form-s8-template.json")
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	return buildHypothesisResultWithTemplate(binding, inputs, templateBytes)
}

func buildHypothesisResultWithTemplate(binding HypothesisResultBinding, inputs HypothesisResultInputs, templateBytes []byte) (HypothesisResultPacket, error) {
	binding = cloneHypothesisResultBinding(binding)
	if err := validateHypothesisResultBinding(binding); err != nil {
		return HypothesisResultPacket{}, err
	}
	canonicalInputs, err := canonicalHypothesisResultInputs(binding, inputs)
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	var template struct {
		SchemaVersion string              `json:"schema_version"`
		Stage         string              `json:"stage"`
		Adapter       string              `json:"adapter"`
		Title         string              `json:"title"`
		Instruction   string              `json:"instruction"`
		Questions     map[string]Question `json:"questions"`
	}
	if err := decodeOneStrictJSON(templateBytes, &template); err != nil || template.SchemaVersion != "1.0.0" || template.Stage != "S8" || template.Adapter != "hypothesis_result" || strings.TrimSpace(template.Title) == "" || strings.TrimSpace(template.Instruction) == "" || len(template.Questions) != 9 {
		return HypothesisResultPacket{}, fmt.Errorf("unregistered S8 HypothesisResult template")
	}
	// The issued responsibility, not professional content, provides the identity.
	identityBytes, err := json.Marshal([]any{binding.RuntimeID, binding.BaselineGeneration, binding.CaseID, binding.CaseRevision, binding.HypothesisID, binding.AssignmentID, binding.OwnerAgentID})
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	packetID := "packet-s8-" + base64.RawURLEncoding.EncodeToString(identityBytes)
	parts := []string{}
	for rest := packetID; rest != ""; {
		n := len(rest)
		if n > 120 {
			n = 120
		}
		parts = append(parts, rest[:n])
		rest = rest[n:]
	}
	base := filepath.ToSlash(filepath.Join(parts...))
	answerBytes, err := json.Marshal(makeHypothesisResultDraftAnswer())
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	answer, err := canonicalJSON(answerBytes)
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	read := makeHypothesisResultRead(binding, canonicalInputs)
	packet := HypothesisResultPacket{
		ID: packetID, TemplateID: hypothesisResultTemplateID,
		Binding: binding, Inputs: canonicalInputs,
		ResultID:        "hypothesis-result-" + packetID,
		IssuanceEventID: "evt-hypothesis-result-packet-" + packetID,
		ViewPath:        ".claude/work-packets/" + base + ".json",
		AnswerPath:      ".claude/submissions/" + base + "/" + binding.OwnerAgentID + "/answer.json",
		Draft:           HypothesisResultDraft{Title: template.Title, Instruction: template.Instruction, Read: read, Questions: template.Questions, Answer: answer},
	}
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("hypothesis-result-work-packet.schema.json", packetBytes); err != nil {
		return HypothesisResultPacket{}, fmt.Errorf("generated S8 HypothesisResult packet violates its registered schema: %w", err)
	}
	return packet, nil
}

// ValidateHypothesisResultAnswer validates a complete authoring file and
// returns only its canonical professional answer. Runtime evidence, live
// Case freshness, dispatch registration, and Finding-boundary semantics are
// checked by the original investigation consumer, not this neutral package.
func ValidateHypothesisResultAnswer(packet HypothesisResultPacket, data []byte) (json.RawMessage, []FieldError) {
	_, raw, issues := ParseHypothesisResultAnswer(packet, data)
	return raw, issues
}

// ParseHypothesisResultAnswer parses the current original questionnaire once.
// The registered packet supplies its existing question; no source or packet
// rebuild is a precondition for receiving the professional answer.
func ParseHypothesisResultAnswer(packet HypothesisResultPacket, data []byte) (HypothesisResultAnswer, json.RawMessage, []FieldError) {
	issue := func(pointer, expected, observed string) (HypothesisResultAnswer, json.RawMessage, []FieldError) {
		return HypothesisResultAnswer{}, nil, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "保留原发行题干，只填写 answer 中的调查判断和现场观察。"}}
	}
	if len(data) > 20<<20 {
		return issue("/", "JSON <= 20 MiB", "oversized")
	}
	var draft struct {
		Title       string                 `json:"title"`
		Instruction string                 `json:"instruction"`
		Read        []ReadItem             `json:"read"`
		Questions   map[string]Question    `json:"questions"`
		Answer      HypothesisResultAnswer `json:"answer"`
	}
	if err := decodeOneStrictJSON(data, &draft); err != nil {
		return issue("/", "one original S8 questionnaire", err.Error())
	}
	if draft.Title != packet.Draft.Title || draft.Instruction != packet.Draft.Instruction || !reflect.DeepEqual(draft.Read, packet.Draft.Read) || !reflect.DeepEqual(draft.Questions, packet.Draft.Questions) {
		return issue("/", "unchanged issued question", "questionnaire was edited")
	}
	if pointer, expected, observed := validateHypothesisResultFields(packet.Binding, draft.Answer); pointer != "" {
		return issue(pointer, expected, observed)
	}
	// Canonical empty collections prevent a repeated incomplete observation from
	// becoming a new attempt merely because an omitted field decoded as nil.
	draft.Answer.EvidenceRefs = append([]string{}, draft.Answer.EvidenceRefs...)
	draft.Answer.SourceBoundaryRefs = append([]string{}, draft.Answer.SourceBoundaryRefs...)
	draft.Answer.ExplainsFindingIDs = append([]string{}, draft.Answer.ExplainsFindingIDs...)
	draft.Answer.DoesNotExplain = append([]string{}, draft.Answer.DoesNotExplain...)
	draft.Answer.NewHypotheses = append([]HypothesisNewQuestion{}, draft.Answer.NewHypotheses...)
	raw, err := json.Marshal(draft.Answer)
	if err != nil {
		return issue("/answer", "professional answer", err.Error())
	}
	return draft.Answer, raw, nil
}

func validateCompleteHypothesisResultAnswer(binding HypothesisResultBinding, data []byte) (pointer, expected, observed string) {
	var answer HypothesisResultAnswer
	if err := decodeOneStrictJSON(data, &answer); err != nil {
		return "/answer", "valid professional fields", err.Error()
	}
	return validateHypothesisResultFields(binding, answer)
}

func validateHypothesisResultFields(binding HypothesisResultBinding, answer HypothesisResultAnswer) (pointer, expected, observed string) {
	if answer.Result == nil || !oneOfHypothesisResult(*answer.Result, "supported", "refuted", "inconclusive") {
		return "/answer/result", "supported | refuted | inconclusive", "unanswered or invalid"
	}
	return "", "", ""
}

func canonicalHypothesisResultInputs(binding HypothesisResultBinding, inputs HypothesisResultInputs) (HypothesisResultInputs, error) {
	question := cloneHypothesisResultQuestion(inputs.Hypothesis)
	if err := validateHypothesisQuestion(question); err != nil {
		return HypothesisResultInputs{}, err
	}
	if question.HypothesisID != binding.HypothesisID || question.AssignmentID != binding.AssignmentID || question.Status != "open" || !reflect.DeepEqual(question.SourceFindingIDs, binding.HypothesisSourceFindingIDs) {
		return HypothesisResultInputs{}, fmt.Errorf("S8 registered Hypothesis identity, status, or source subset differs from its binding")
	}
	caseRoute := cloneHypothesisCaseRoute(inputs.CaseRoute)
	if err := validateHypothesisCaseRoute(caseRoute); err != nil {
		return HypothesisResultInputs{}, err
	}
	if len(inputs.Findings) != len(binding.SourceFindingIDs) || len(inputs.Findings) > 512 {
		return HypothesisResultInputs{}, fmt.Errorf("S8 packet Finding documents must exactly match the bounded Case source Finding set")
	}
	findings := make([]HypothesisResultFinding, len(inputs.Findings))
	for i, finding := range inputs.Findings {
		if finding.FindingID != binding.SourceFindingIDs[i] || !validFindingID(finding.FindingID) || !safeRelative(finding.Ref) || finding.Ref == "." {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding reference identity is invalid or differs from the frozen exact set at index %d", i)
		}
		if len(finding.Document) == 0 || len(finding.Document) > 4<<20 {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding %q document must be valid JSON <= 4 MiB", finding.FindingID)
		}
		document, err := canonicalJSON(finding.Document)
		if err != nil {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding %q document: %w", finding.FindingID, err)
		}
		var stored struct {
			FindingID    string   `json:"finding_id"`
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := decodeOneJSON(document, &stored); err != nil || stored.FindingID != finding.FindingID {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding document at index %d has a mismatched finding_id", i)
		}
		findings[i] = HypothesisResultFinding{FindingID: finding.FindingID, Ref: finding.Ref, Document: json.RawMessage(document)}
	}
	if err := validateHypothesisEvidenceRefs(binding, question, findings, inputs.EvidenceRefs); err != nil {
		return HypothesisResultInputs{}, err
	}
	canonicalInputs := HypothesisResultInputs{
		Hypothesis: question, CaseRoute: caseRoute, Findings: findings,
		EvidenceRefs: cloneHypothesisEvidenceRefs(inputs.EvidenceRefs),
	}
	encoded, err := json.Marshal(canonicalInputs)
	if err != nil {
		return HypothesisResultInputs{}, err
	}
	if len(encoded) > 20<<20 {
		return HypothesisResultInputs{}, fmt.Errorf("S8 frozen inputs exceed 20 MiB")
	}
	return canonicalInputs, nil
}

func validateHypothesisEvidenceRefs(binding HypothesisResultBinding, question HypothesisResultQuestion, findings []HypothesisResultFinding, refs []HypothesisEvidenceRef) error {
	// References are reading material, not a machine certificate of the investigation.
	return nil
}

func validateHypothesisResultBinding(binding HypothesisResultBinding) error {
	if !safeComponent(binding.RuntimeID) || binding.BaselineGeneration < 0 ||
		!validInvestigationCaseID(binding.CaseID) || !safeRelative(binding.CaseRef) || binding.CaseRef == "." || binding.CaseRevision < 1 ||
		!validHypothesisID(binding.HypothesisID) || !validAssignmentID(binding.AssignmentID) ||
		!safeComponent(binding.TeamID) || !safeComponent(binding.TaskID) || !safeRelative(binding.TaskRef) || binding.TaskRef == "." || !safeRelative(binding.ManifestRef) || binding.ManifestRef == "." ||
		!safeComponent(binding.OwnerAgentID) || len(binding.SourceFindingIDs) > 512 || len(binding.HypothesisSourceFindingIDs) > len(binding.SourceFindingIDs) {
		return fmt.Errorf("S8 HypothesisResult requires a complete, bounded Case, Hypothesis, and registered Assignment binding")
	}
	if err := identity.ValidateAgentID(binding.OwnerAgentID); err != nil {
		return fmt.Errorf("S8 HypothesisResult owner: %w", err)
	}
	caseSet := map[string]bool{}
	for _, id := range binding.SourceFindingIDs {
		if !validFindingID(id) || caseSet[id] {
			return fmt.Errorf("S8 Case Finding IDs must be valid, unique, and ordered")
		}
		caseSet[id] = true
	}
	if len(binding.SourceFindingIDs) > 0 {
		if err := validateOrderedSubset(binding.HypothesisSourceFindingIDs, binding.SourceFindingIDs, "Hypothesis source Finding IDs"); err != nil {
			return err
		}
	}
	return nil
}

func validateHypothesisQuestion(question HypothesisResultQuestion) error {
	if !validHypothesisID(question.HypothesisID) || !validAssignmentID(question.AssignmentID) || question.Status != "open" ||
		strings.TrimSpace(question.Statement) == "" || strings.TrimSpace(question.Invariant) == "" || strings.TrimSpace(question.Discriminator) == "" ||
		strings.TrimSpace(question.ExpectedOutcomes.Support) == "" || strings.TrimSpace(question.ExpectedOutcomes.Refute) == "" {
		return fmt.Errorf("S8 packet requires the exact open, falsifiable registered Hypothesis question")
	}
	for _, ref := range question.EvidenceRefs {
		if strings.TrimSpace(ref) == "" || ref != strings.TrimSpace(ref) {
			return fmt.Errorf("S8 Hypothesis evidence refs must be non-empty exact strings")
		}
	}
	return nil
}

func validateHypothesisCaseRoute(route HypothesisResultCaseRoute) error {
	if route.Status != "investigating" {
		return fmt.Errorf("S8 result requires an actively investigating Case")
	}
	if route.Route == nil {
		if route.RouteReason != nil || route.RouteConsumedAt != nil || route.RouteConsumer != nil {
			return fmt.Errorf("S8 un-routed Case cannot carry a route reason or route-consumption tuple")
		}
		return nil
	}
	if *route.Route != "investigate_more" || route.RouteReason == nil || strings.TrimSpace(*route.RouteReason) == "" || route.RouteConsumedAt != nil || route.RouteConsumer != nil {
		return fmt.Errorf("S8 result accepts only an unconsumed investigate_more route")
	}
	return nil
}

func makeHypothesisResultDraftAnswer() HypothesisResultAnswer {
	return HypothesisResultAnswer{
		EvidenceRefs: []string{}, SourceBoundaryRefs: []string{}, Result: nil,
		ExplainsFindingIDs: []string{}, DoesNotExplain: []string{}, NewHypotheses: []HypothesisNewQuestion{},
	}
}

func makeHypothesisResultRead(binding HypothesisResultBinding, inputs HypothesisResultInputs) []ReadItem {
	paths := []struct{ path, purpose string }{
		{binding.CaseRef, "Read the current InvestigationCase and the exact open Hypothesis being investigated."},
		{binding.TaskRef, "Read the exact registered investigator TASK and its scope."},
		{binding.ManifestRef, "Read the registered dispatch manifest bound to this Assignment and owner."},
	}
	for _, finding := range inputs.Findings {
		paths = append(paths, struct{ path, purpose string }{finding.Ref, "Read this immutable Finding, including its encounter and failure boundary."})
	}
	for _, ref := range inputs.EvidenceRefs {
		if ref.Attestation == "reference" {
			paths = append(paths, struct{ path, purpose string }{ref.Ref, "Read the actual cited material as needed for this discriminator; references need no new registration."})

		}
	}
	seen := map[string]bool{}
	read := make([]ReadItem, 0, len(paths))
	for _, item := range paths {
		if item.path != "" && !seen[item.path] {
			seen[item.path] = true
			read = append(read, ReadItem{Path: item.path, Purpose: item.purpose})
		}
	}
	return read
}

func cloneHypothesisResultBinding(binding HypothesisResultBinding) HypothesisResultBinding {
	binding.SourceFindingIDs = append([]string{}, binding.SourceFindingIDs...)
	binding.HypothesisSourceFindingIDs = append([]string{}, binding.HypothesisSourceFindingIDs...)
	return binding
}

func cloneHypothesisResultQuestion(question HypothesisResultQuestion) HypothesisResultQuestion {
	question.SourceFindingIDs = append([]string{}, question.SourceFindingIDs...)
	question.EvidenceRefs = append([]string(nil), question.EvidenceRefs...)
	return question
}

func cloneHypothesisCaseRoute(route HypothesisResultCaseRoute) HypothesisResultCaseRoute {
	copyString := func(value *string) *string {
		if value == nil {
			return nil
		}
		copyValue := *value
		return &copyValue
	}
	route.Route = copyString(route.Route)
	route.RouteReason = copyString(route.RouteReason)
	route.RouteConsumedAt = copyString(route.RouteConsumedAt)
	route.RouteConsumer = copyString(route.RouteConsumer)
	return route
}

func cloneHypothesisEvidenceRefs(refs []HypothesisEvidenceRef) []HypothesisEvidenceRef {
	cloned := append([]HypothesisEvidenceRef(nil), refs...)
	for i := range cloned {
		// Old machine metadata is neither a new packet fact nor a condition
		// for retaining the original evidence reference.
		if refs[i].BaselineGeneration != nil {
			value := *refs[i].BaselineGeneration
			cloned[i].BaselineGeneration = &value
		}
		if refs[i].InvalidatedBy != nil {
			value := *refs[i].InvalidatedBy
			cloned[i].InvalidatedBy = &value
		}
	}
	return cloned
}

func validateOrderedSubset(subset, ordered []string, what string) error {
	if len(subset) == 0 {
		return fmt.Errorf("S8 %s must be non-empty", what)
	}
	wanted := map[string]bool{}
	for _, id := range subset {
		if wanted[id] {
			return fmt.Errorf("S8 %s contains duplicate %q", what, id)
		}
		wanted[id] = true
	}
	got := make([]string, 0, len(subset))
	for _, id := range ordered {
		if wanted[id] {
			got = append(got, id)
			delete(wanted, id)
		}
	}
	if len(wanted) != 0 || !reflect.DeepEqual(got, subset) {
		return fmt.Errorf("S8 %s must be an ordered subset of the Case exact Finding set", what)
	}
	return nil
}

func validInvestigationCaseID(value string) bool { return investigationCaseIDRE.MatchString(value) }
func validFindingID(value string) bool           { return findingIDRE.MatchString(value) }
func validHypothesisID(value string) bool        { return safeComponent(value) && len(value) <= 256 }
func validAssignmentID(value string) bool        { return investigationAssignmentIDRE.MatchString(value) }
func oneOfHypothesisResult(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

var (
	investigationCaseIDRE       = regexp.MustCompile(`^investigation-case-[A-Za-z0-9._-]+$`)
	findingIDRE                 = regexp.MustCompile(`^finding-[A-Za-z0-9._-]+$`)
	investigationAssignmentIDRE = regexp.MustCompile(`^assignment-[A-Za-z0-9._-]+$`)
)

type answerStringError struct {
	pointer  string
	observed string
}

func validateAnswerStrings(values []string, pointer string, requireOne bool) *answerStringError {
	if requireOne && len(values) == 0 {
		return &answerStringError{pointer: pointer, observed: "empty"}
	}
	seen := map[string]bool{}
	for i, value := range values {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return &answerStringError{pointer: fmt.Sprintf("%s/%d", pointer, i), observed: "empty or padded"}
		}
		if seen[value] {
			return &answerStringError{pointer: fmt.Sprintf("%s/%d", pointer, i), observed: "duplicate " + value}
		}
		seen[value] = true
	}
	return nil
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
