package workpacket

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/schema"
)

const hypothesisResultTemplateID = "s8-hypothesis-result-v1"

type HypothesisResultBinding struct {
	RuntimeID                  string   `json:"runtime_id"`
	BaselineGeneration         int      `json:"baseline_generation"`
	ReviewRound                int      `json:"review_round"`
	CaseID                     string   `json:"case_id"`
	CaseRef                    string   `json:"case_ref"`
	CaseSHA256                 string   `json:"case_sha256"`
	CaseRevision               int      `json:"case_revision"`
	CaseRouteSHA256            string   `json:"case_route_sha256"`
	ObservationBatchID         string   `json:"observation_batch_id,omitempty"`
	ObservationBatchRef        string   `json:"observation_batch_ref,omitempty"`
	ObservationBatchSHA256     string   `json:"observation_batch_sha256,omitempty"`
	BaselineDigest             string   `json:"baseline_digest,omitempty"`
	SourceFindingIDs           []string `json:"source_finding_ids"`
	HypothesisID               string   `json:"hypothesis_id"`
	HypothesisQuestionSHA256   string   `json:"hypothesis_question_sha256"`
	HypothesisSourceFindingIDs []string `json:"hypothesis_source_finding_ids"`
	AssignmentID               string   `json:"assignment_id"`
	AssignmentSHA256           string   `json:"assignment_sha256"`
	TeamID                     string   `json:"team_id"`
	TaskID                     string   `json:"task_id"`
	TaskRef                    string   `json:"task_ref"`
	TaskSHA256                 string   `json:"task_sha256"`
	ManifestRef                string   `json:"manifest_ref"`
	ManifestSHA256             string   `json:"manifest_sha256"`
	OwnerAgentID               string   `json:"owner_agent_id"`
}

// HypothesisResultQuestion is the exact registered Case hypothesis row. Its
// identity/status fields are retained for caller comparison, while the
// question digest covers only the six immutable question/content fields.
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
	SHA256    string          `json:"sha256"`
	Document  json.RawMessage `json:"document"`
}

func (finding *HypothesisResultFinding) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire HypothesisResultFinding
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
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

// HypothesisEvidenceRef preserves the original attestation source tuple.
// Runtime-index metadata is copied from the original evidence entry and must
// still be reverified against the current Runtime and physical bytes by the
// S8 consumer. Execution anchors intentionally carry no fabricated file
// hash or index metadata. Typed Finding sources retain their original Finding
// binding and declared content tuple; observations never gain fabricated bytes.
type HypothesisEvidenceRef struct {
	Ref                string  `json:"ref"`
	Origin             string  `json:"origin"`
	SourceFindingID    string  `json:"source_finding_id,omitempty"`
	Attestation        string  `json:"attestation"`
	Kind               string  `json:"kind,omitempty"`
	Path               string  `json:"path,omitempty"`
	SHA256             string  `json:"sha256,omitempty"`
	Status             string  `json:"status,omitempty"`
	BaselineGeneration *int    `json:"baseline_generation,omitempty"`
	ReviewRound        *int    `json:"review_round,omitempty"`
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
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
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

type HypothesisResultPacket struct {
	ID              string                  `json:"packet_id"`
	TemplateID      string                  `json:"template_id"`
	TemplateSHA     string                  `json:"template_sha256"`
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
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
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

// HypothesisQuestionDigest returns the digest over only the original
// registered question/content projection. Assignment identity and lifecycle
// status are separately bound in the packet and are deliberately excluded.
func HypothesisQuestionDigest(question HypothesisResultQuestion) (string, error) {
	projection := struct {
		Statement        string                     `json:"statement"`
		Invariant        string                     `json:"invariant"`
		Discriminator    string                     `json:"discriminator"`
		ExpectedOutcomes HypothesisExpectedOutcomes `json:"expected_outcomes"`
		SourceFindingIDs []string                   `json:"source_finding_ids"`
		EvidenceRefs     []string                   `json:"evidence_refs"`
	}{
		Statement: question.Statement, Invariant: question.Invariant,
		Discriminator: question.Discriminator, ExpectedOutcomes: question.ExpectedOutcomes,
		SourceFindingIDs: append([]string{}, question.SourceFindingIDs...),
		EvidenceRefs:     append([]string(nil), question.EvidenceRefs...),
	}
	data, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	return Digest(canonical), nil
}

// HypothesisCaseRouteDigest canonicalizes the eligible Case route tuple.
// Eligible cases are actively investigating and have no route, or retain an
// unconsumed investigate_more route. This digest is a freshness binding, not
// a route authorization.
func HypothesisCaseRouteDigest(route HypothesisResultCaseRoute) (string, error) {
	if err := validateHypothesisCaseRoute(route); err != nil {
		return "", err
	}
	data, err := json.Marshal(route)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	return Digest(canonical), nil
}

// BuildHypothesisResult builds a stable questionnaire from a registered
// Hypothesis and its exact Case/ObservationBatch/dispatch source closure.
// Callers must prove the supplied hashes and evidence metadata from original
// authority bytes before issuing this projection.
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
	templateSHA := Digest(templateBytes)
	identityJSON, err := json.Marshal(struct {
		Binding     HypothesisResultBinding `json:"binding"`
		Inputs      HypothesisResultInputs  `json:"inputs"`
		TemplateSHA string                  `json:"template_sha256"`
	}{binding, canonicalInputs, templateSHA})
	if err != nil {
		return HypothesisResultPacket{}, fmt.Errorf("encode S8 packet identity: %w", err)
	}
	identityJSON, err = canonicalJSON(identityJSON)
	if err != nil {
		return HypothesisResultPacket{}, err
	}
	packetHash := Digest(identityJSON)
	packetID := "packet-" + packetHash
	base := fmt.Sprintf("%s/g%d/S8/r%d/%s/%s", binding.RuntimeID, binding.BaselineGeneration, binding.ReviewRound, binding.AssignmentID, packetID)
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
		ID: packetID, TemplateID: hypothesisResultTemplateID, TemplateSHA: templateSHA,
		Binding: binding, Inputs: canonicalInputs,
		ResultID:        "hypothesis-result-" + packetHash,
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

func RebuildHypothesisResult(packet HypothesisResultPacket) (HypothesisResultPacket, error) {
	// Only known embedded templates can restore an issuance. Frozen author
	// questions are never accepted as a new template or refreshed in place.
	for _, name := range []string{"stage-form-s8-template.json", "stage-form-s8-template-before-answer-shape.json", "stage-form-s8-template-before-guidance.json"} {
		data, err := schema.ReadAsset(name)
		if err != nil {
			return HypothesisResultPacket{}, err
		}
		if Digest(data) == packet.TemplateSHA {
			return buildHypothesisResultWithTemplate(packet.Binding, packet.Inputs, data)
		}
	}
	return HypothesisResultPacket{}, fmt.Errorf("unsupported frozen S8 template digest")
}

func HypothesisResultPacketDigest(packet HypothesisResultPacket) (string, error) {
	rebuilt, err := RebuildHypothesisResult(packet)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(packet, rebuilt) {
		return "", fmt.Errorf("S8 HypothesisResult packet differs from its canonical rebuild")
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

// ValidateHypothesisResultAnswer validates a complete authoring file and
// returns only its canonical professional answer. Runtime evidence, live
// Case freshness, dispatch registration, and Finding-boundary semantics are
// checked by the original investigation consumer, not this neutral package.
func ValidateHypothesisResultAnswer(packet HypothesisResultPacket, data []byte) (json.RawMessage, []FieldError) {
	issue := func(pointer, expected, observed string) (json.RawMessage, []FieldError) {
		return nil, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "保留原发行题干，只填写 answer 中的调查判断和现场观察。答卷顶层仅为 title、instruction、read、questions、answer；不要提交含 packet_id、binding、inputs、draft 的 view packet。"}}
	}
	if len(data) > 20<<20 {
		return issue("/", "JSON <= 20 MiB", "oversized")
	}
	if err := ValidateUniqueJSON(data); err != nil {
		return issue("/", "unique JSON keys", err.Error())
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s8.schema.json", data); err != nil {
		return issue("/", "registered closed S8 HypothesisResult answer", err.Error())
	}
	var draft HypothesisResultDraft
	if err := decodeOneStrictJSON(data, &draft); err != nil {
		return issue("/", "one closed S8 HypothesisResult questionnaire", err.Error())
	}
	expected, err := RebuildHypothesisResult(packet)
	if err != nil || !reflect.DeepEqual(packet, expected) {
		return issue("/", "canonical issued S8 packet", "packet identity differs")
	}
	answer := draft.Answer
	draft.Answer = expected.Draft.Answer
	if !reflect.DeepEqual(draft, expected.Draft) {
		return issue("/", "unchanged issued machine fields", "questionnaire or binding was edited")
	}
	canonicalAnswer, err := canonicalJSON(answer)
	if err != nil {
		return issue("/answer", "one JSON answer object", err.Error())
	}
	if pointer, want, observed := validateCompleteHypothesisResultAnswer(packet.Binding, canonicalAnswer); pointer != "" {
		return issue(pointer, want, observed)
	}
	return json.RawMessage(canonicalAnswer), nil
}

func validateCompleteHypothesisResultAnswer(binding HypothesisResultBinding, data []byte) (pointer, expected, observed string) {
	var answer HypothesisResultAnswer
	if err := decodeOneStrictJSON(data, &answer); err != nil {
		return "/answer", "valid professional fields", err.Error()
	}
	for _, field := range []struct{ pointer, value string }{
		{"/answer/method", answer.Method},
		{"/answer/observed", answer.Observed},
		{"/answer/counterfactual_or_discriminator", answer.CounterfactualOrDiscriminator},
	} {
		if strings.TrimSpace(field.value) == "" {
			return field.pointer, "non-empty professional judgment", "empty"
		}
	}
	if answer.Result == nil || !oneOfHypothesisResult(*answer.Result, "supported", "refuted", "inconclusive") {
		return "/answer/result", "supported | refuted | inconclusive", "unanswered or invalid"
	}
	if err := validateAnswerStrings(answer.EvidenceRefs, "/answer/evidence_refs", true); err != nil {
		return err.pointer, "one or more non-empty unique references", err.observed
	}
	if err := validateAnswerStrings(answer.SourceBoundaryRefs, "/answer/source_boundary_refs", true); err != nil {
		return err.pointer, "one or more non-empty unique original source boundaries", err.observed
	}
	caseFindings := stringSet(binding.SourceFindingIDs)
	seen := map[string]string{}
	for i, id := range answer.ExplainsFindingIDs {
		if !caseFindings[id] {
			return fmt.Sprintf("/answer/explains_finding_ids/%d", i), "a Finding in the frozen Case set", id
		}
		if _, ok := seen[id]; ok {
			return fmt.Sprintf("/answer/explains_finding_ids/%d", i), "unique Finding IDs", id
		}
		seen[id] = "explains"
	}
	for i, id := range answer.DoesNotExplain {
		if !caseFindings[id] {
			return fmt.Sprintf("/answer/does_not_explain/%d", i), "a Finding in the frozen Case set", id
		}
		if prior, ok := seen[id]; ok {
			return fmt.Sprintf("/answer/does_not_explain/%d", i), "a Finding not also listed under explains_finding_ids", id + " already " + prior
		}
		seen[id] = "does_not_explain"
	}
	for i, hypothesis := range answer.NewHypotheses {
		for _, field := range []struct{ name, value string }{
			{"statement", hypothesis.Statement}, {"invariant", hypothesis.Invariant}, {"discriminator", hypothesis.Discriminator},
		} {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Sprintf("/answer/new_hypotheses/%d/%s", i, field.name), "non-empty falsifiable hypothesis content", "empty"
			}
		}
		if strings.TrimSpace(hypothesis.ExpectedOutcomes.Support) == "" || strings.TrimSpace(hypothesis.ExpectedOutcomes.Refute) == "" {
			return fmt.Sprintf("/answer/new_hypotheses/%d/expected_outcomes", i), "non-empty support and refute outcomes", "incomplete"
		}
		if err := validateAnswerStrings(hypothesis.SourceFindingIDs, fmt.Sprintf("/answer/new_hypotheses/%d/source_finding_ids", i), len(binding.SourceFindingIDs) > 0); err != nil {
			return err.pointer, "one or more Case Finding IDs", err.observed
		}
		seenSources := map[string]bool{}
		for j, id := range hypothesis.SourceFindingIDs {
			if !caseFindings[id] {
				return fmt.Sprintf("/answer/new_hypotheses/%d/source_finding_ids/%d", i, j), "a Finding in the frozen Case set", id
			}
			if seenSources[id] {
				return fmt.Sprintf("/answer/new_hypotheses/%d/source_finding_ids/%d", i, j), "unique Finding IDs", id
			}
			seenSources[id] = true
		}
		if err := validateAnswerStrings(hypothesis.EvidenceRefs, fmt.Sprintf("/answer/new_hypotheses/%d/evidence_refs", i), true); err != nil {
			return err.pointer, "one or more non-empty evidence references", err.observed
		}
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
	questionDigest, err := HypothesisQuestionDigest(question)
	if err != nil || questionDigest != binding.HypothesisQuestionSHA256 {
		return HypothesisResultInputs{}, fmt.Errorf("S8 registered Hypothesis question digest does not match its binding")
	}
	caseRoute := cloneHypothesisCaseRoute(inputs.CaseRoute)
	routeDigest, err := HypothesisCaseRouteDigest(caseRoute)
	if err != nil || routeDigest != binding.CaseRouteSHA256 {
		return HypothesisResultInputs{}, fmt.Errorf("S8 current InvestigationCase route tuple is invalid or differs from its binding")
	}
	if len(inputs.Findings) != len(binding.SourceFindingIDs) || binding.ObservationBatchID != "" && len(inputs.Findings) == 0 || len(inputs.Findings) > 512 {
		return HypothesisResultInputs{}, fmt.Errorf("S8 packet Finding documents must exactly match the bounded Case source Finding set")
	}
	findings := make([]HypothesisResultFinding, len(inputs.Findings))
	for i, finding := range inputs.Findings {
		if finding.FindingID != binding.SourceFindingIDs[i] || !validFindingID(finding.FindingID) || !safeRelative(finding.Ref) || finding.Ref == "." || !digestPattern.MatchString(finding.SHA256) {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding reference identity is invalid or differs from the frozen exact set at index %d", i)
		}
		if len(finding.Document) == 0 || len(finding.Document) > 4<<20 {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding %q document must be valid JSON <= 4 MiB", finding.FindingID)
		}
		document, err := canonicalJSON(finding.Document)
		if err != nil {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding %q document: %w", finding.FindingID, err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("finding.schema.json", document); err != nil {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding %q does not match the original Finding schema: %w", finding.FindingID, err)
		}
		var stored struct {
			FindingID    string   `json:"finding_id"`
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := decodeOneJSON(document, &stored); err != nil || stored.FindingID != finding.FindingID {
			return HypothesisResultInputs{}, fmt.Errorf("S8 Finding document at index %d has a mismatched finding_id", i)
		}
		findings[i] = HypothesisResultFinding{FindingID: finding.FindingID, Ref: finding.Ref, SHA256: finding.SHA256, Document: json.RawMessage(document)}
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
	type expectedRef struct{ ref, origin, findingID string }
	want := make([]expectedRef, 0, len(question.EvidenceRefs)+len(findings))
	for _, ref := range question.EvidenceRefs {
		want = append(want, expectedRef{ref: ref, origin: "hypothesis"})
	}
	for _, finding := range findings {
		var document struct {
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := decodeOneJSON(finding.Document, &document); err != nil {
			return fmt.Errorf("decode evidence refs from Finding %q: %w", finding.FindingID, err)
		}
		for _, ref := range document.EvidenceRefs {
			if strings.TrimSpace(ref) == "" || ref != strings.TrimSpace(ref) {
				return fmt.Errorf("S8 Finding %q has an empty or padded evidence ref", finding.FindingID)
			}
			want = append(want, expectedRef{ref: ref, origin: "finding", findingID: finding.FindingID})
		}
	}
	if len(want) == 0 || len(refs) != len(want) || len(refs) > 4096 {
		return fmt.Errorf("S8 EvidenceRef tuples must exactly preserve the Hypothesis and Finding source references")
	}
	selectedSources := map[string]bool{}
	for _, finding := range findings {
		chosen := false
		for _, id := range question.SourceFindingIDs {
			if id == finding.FindingID {
				chosen = true
			}
		}
		if !chosen {
			continue
		}
		var doc struct {
			Refs []string `json:"evidence_refs"`
		}
		if err := decodeOneJSON(finding.Document, &doc); err != nil {
			return err
		}
		for _, ref := range doc.Refs {
			selectedSources[ref] = true
		}
	}
	seen := map[string]bool{}
	for i, item := range want {
		ref := refs[i]
		if ref.Ref != item.ref || strings.TrimSpace(ref.Ref) == "" || ref.Ref != strings.TrimSpace(ref.Ref) || ref.Origin != item.origin || ref.SourceFindingID != item.findingID {
			return fmt.Errorf("S8 EvidenceRef tuple %d does not match its original Hypothesis/Finding source", i)
		}
		key := ref.Origin + "\x00" + ref.SourceFindingID + "\x00" + ref.Ref
		if seen[key] {
			return fmt.Errorf("S8 EvidenceRef tuple %d is duplicated", i)
		}
		seen[key] = true
		if ref.Attestation == "file_source" {
			source, typed, err := evidence.ParseFindingReference(ref.Ref)
			if err != nil || !typed || ref.Origin != "hypothesis" || !strings.HasPrefix(ref.Ref, "path:") || ref.Path != source.Path || ref.SHA256 != source.SHA256 || ref.Kind != "" || ref.Status != "" || ref.BaselineGeneration != nil || ref.ReviewRound != nil || ref.InvalidatedBy != nil {
				return fmt.Errorf("invalid S8 frozen local file source")
			}
			continue
		}
		if ref.Attestation == "finding_source" {
			source, typed, err := evidence.ParseFindingReference(ref.Ref)
			if err != nil || !typed || (ref.Origin == "hypothesis" && !selectedSources[ref.Ref]) || (ref.Origin == "finding" && ref.SourceFindingID == "") || ref.Path != source.Path || ref.SHA256 != source.SHA256 || ref.Kind != "" || ref.Status != "" || ref.BaselineGeneration != nil || ref.ReviewRound != nil || ref.InvalidatedBy != nil {
				return fmt.Errorf("S8 Finding source %q must preserve its original type, Finding binding and content tuple without fabricated index metadata", ref.Ref)
			}
			continue
		}
		if _, typed, err := evidence.ParseFindingReference(ref.Ref); ref.Origin == "finding" && (typed || err != nil) {
			return fmt.Errorf("S8 typed Finding source %q requires its original finding_source attestation, not an index alias", ref.Ref)
		}
		if strings.Contains(ref.Ref, "://") {
			if ref.Attestation != "execution_anchor" || ref.Kind != "" || ref.Path != "" || ref.SHA256 != "" || ref.Status != "" || ref.BaselineGeneration != nil || ref.ReviewRound != nil || ref.InvalidatedBy != nil {
				return fmt.Errorf("S8 execution anchor %q has fabricated Runtime index or content-hash metadata", ref.Ref)
			}
			continue
		}
		if ref.Attestation != "runtime_index" || ref.Kind == "" || !safeRelative(ref.Path) || ref.Path == "." || !digestPattern.MatchString(ref.SHA256) || ref.Status != "valid" || ref.BaselineGeneration == nil || *ref.BaselineGeneration != binding.BaselineGeneration || ref.ReviewRound == nil || *ref.ReviewRound != binding.ReviewRound || ref.InvalidatedBy != nil {
			return fmt.Errorf("S8 indexed evidence ref %q must retain its valid current-generation/current-round index tuple", ref.Ref)
		}
	}
	return nil
}

func validateHypothesisResultBinding(binding HypothesisResultBinding) error {
	if !safeComponent(binding.RuntimeID) || binding.BaselineGeneration < 0 || binding.ReviewRound < 1 ||
		!validInvestigationCaseID(binding.CaseID) || !safeRelative(binding.CaseRef) || binding.CaseRef == "." || !digestPattern.MatchString(binding.CaseSHA256) || binding.CaseRevision < 1 || !digestPattern.MatchString(binding.CaseRouteSHA256) ||
		!validHypothesisID(binding.HypothesisID) || !digestPattern.MatchString(binding.HypothesisQuestionSHA256) || !validAssignmentID(binding.AssignmentID) || !digestPattern.MatchString(binding.AssignmentSHA256) ||
		!safeComponent(binding.TeamID) || !safeComponent(binding.TaskID) || !safeRelative(binding.TaskRef) || binding.TaskRef == "." || !digestPattern.MatchString(binding.TaskSHA256) || !safeRelative(binding.ManifestRef) || binding.ManifestRef == "." || !digestPattern.MatchString(binding.ManifestSHA256) ||
		!safeComponent(binding.OwnerAgentID) || len(binding.SourceFindingIDs) > 512 || len(binding.HypothesisSourceFindingIDs) > len(binding.SourceFindingIDs) {
		return fmt.Errorf("S8 HypothesisResult requires a complete, bounded Case, Hypothesis, and registered Assignment binding")
	}
	if binding.ObservationBatchID != "" && (!validObservationBatchID(binding.ObservationBatchID) || !safeRelative(binding.ObservationBatchRef) || binding.ObservationBatchRef == "." || !digestPattern.MatchString(binding.ObservationBatchSHA256) || !digestPattern.MatchString(binding.BaselineDigest) || len(binding.SourceFindingIDs) == 0 || len(binding.HypothesisSourceFindingIDs) == 0) {
		return fmt.Errorf("S8 ObservationBatch binding is incomplete")
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
		strings.TrimSpace(question.ExpectedOutcomes.Support) == "" || strings.TrimSpace(question.ExpectedOutcomes.Refute) == "" || len(question.EvidenceRefs) == 0 {
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
		{binding.ObservationBatchRef, "Read the sealed S7 Finding set and frozen baseline binding."},
		{binding.TaskRef, "Read the exact registered investigator TASK and its scope."},
		{binding.ManifestRef, "Read the registered dispatch manifest bound to this Assignment and owner."},
	}
	for _, finding := range inputs.Findings {
		paths = append(paths, struct{ path, purpose string }{finding.Ref, "Read this immutable Finding, including its encounter and failure boundary."})
	}
	for _, ref := range inputs.EvidenceRefs {
		if ref.Attestation == "runtime_index" || ref.Attestation == "file_source" {
			purpose := "Read the indexed evidence artifact at the exact registered SHA; execution anchors remain external references."
			if ref.Attestation == "file_source" {
				purpose = "Read the original captured investigation file at its fixed SHA; reuse its exact ref without another evidence registration."
			}
			paths = append(paths, struct{ path, purpose string }{ref.Path, purpose})
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
		if refs[i].BaselineGeneration != nil {
			value := *refs[i].BaselineGeneration
			cloned[i].BaselineGeneration = &value
		}
		if refs[i].ReviewRound != nil {
			value := *refs[i].ReviewRound
			cloned[i].ReviewRound = &value
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
func validObservationBatchID(value string) bool  { return observationBatchIDRE.MatchString(value) }
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
	observationBatchIDRE        = regexp.MustCompile(`^observation-batch-[A-Za-z0-9._-]+$`)
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
