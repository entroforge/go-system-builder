package workpacket

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/schema"
)

const reviewResultTemplateID = "s7-review-result-v1"

var (
	reviewResultIDRE = regexp.MustCompile(`^review-result-[A-Za-z0-9._-]+$`)
	reviewClaimIDRE  = regexp.MustCompile(`^claim-[A-Za-z0-9._-]+$`)
	reviewPlanIDRE   = regexp.MustCompile(`^review-plan-[A-Za-z0-9._-]+$`)
	reviewAssignIDRE = regexp.MustCompile(`^assignment-[A-Za-z0-9._-]+$`)
)

// ReviewResultBinding is the closed, machine supplied S7 issuance identity.
// The workpacket package deliberately has no dependency on review's Plan or
// Result types; the original review writer derives this value from its pinned
// Plan and registered Assignment.
type ReviewResultBinding struct {
	RuntimeID          string   `json:"runtime_id"`
	BaselineGeneration int      `json:"baseline_generation"`
	ReviewPlanID       string   `json:"review_plan_id"`
	ReviewPlanRef      string   `json:"review_plan_ref"`
	ReviewPlanRevision int      `json:"review_plan_revision"`
	AssignmentID       string   `json:"assignment_id"`
	OwnerAgentID       string   `json:"owner_agent_id"`
	ClaimIDs           []string `json:"claim_ids"`
}

// ReviewResultInputs copies the frozen questions, subject set, and optional
// original method selection. RawMessage retains the complete original source
// JSON without importing the S7 review domain into this neutral package.
type ReviewResultInputs struct {
	Claims         json.RawMessage `json:"claims"`
	FrozenSubjects json.RawMessage `json:"frozen_subjects"`
	// Optional for pre-method issuances: absence preserves their original identity.
	Methods      []ReadItem          `json:"methods,omitempty"`
	MethodSource *ReviewMethodSource `json:"method_source,omitempty"`
}

// ReviewMethodSource pins the original registered selection, not a new approval.
type ReviewMethodSource struct {
	ManifestRef string `json:"manifest_ref"`
}

func (inputs *ReviewResultInputs) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire ReviewResultInputs
	var decoded wire
	if err := decodeOneJSON(data, &decoded); err != nil {
		return err
	}
	claims, err := canonicalJSON(decoded.Claims)
	if err != nil {
		return fmt.Errorf("S7 packet Claims: %w", err)
	}
	subjects, err := canonicalJSON(decoded.FrozenSubjects)
	if err != nil {
		return fmt.Errorf("S7 packet frozen subjects: %w", err)
	}
	decoded.Claims, decoded.FrozenSubjects = claims, subjects
	*inputs = ReviewResultInputs(decoded)
	return nil
}

// ReviewMethodPurpose is fixed method guidance, not an author field.
func ReviewMethodPurpose(name string) string {
	return "应用本责任已选择的专业方法（" + name + "）检查已分配 Claims；这是方法指引，不是被审对象或额外审批。"
}

type ReviewResultDraft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      json.RawMessage     `json:"answer"`
}

func (draft *ReviewResultDraft) UnmarshalJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire ReviewResultDraft
	var decoded wire
	if err := decodeOneStrictJSON(data, &decoded); err != nil {
		return err
	}
	answer, err := canonicalJSON(decoded.Answer)
	if err != nil {
		return fmt.Errorf("S7 packet professional draft answer: %w", err)
	}
	decoded.Answer = answer
	*draft = ReviewResultDraft(decoded)
	return nil
}

// ReviewResultPacket is the original neutral S7 authoring projection.
// ResultID and IssuanceEventID derive from the existing responsibility ID.
type ReviewResultPacket struct {
	ID              string              `json:"packet_id"`
	TemplateID      string              `json:"template_id"`
	Binding         ReviewResultBinding `json:"binding"`
	Inputs          ReviewResultInputs  `json:"inputs"`
	ResultID        string              `json:"result_id"`
	IssuanceEventID string              `json:"issuance_event_id"`
	ViewPath        string              `json:"view_path"`
	AnswerPath      string              `json:"answer_path"`
	Draft           ReviewResultDraft   `json:"draft"`
}

// BuildReviewResult constructs the stable S7 questionnaire from a trusted
// current Plan/Assignment projection. It validates the supplied Claim IDs
// against the frozen question JSON and derives all identifiers and paths.
func BuildReviewResult(binding ReviewResultBinding, inputs ReviewResultInputs) (ReviewResultPacket, error) {
	templateBytes, err := schema.ReadAsset("stage-form-s7-template.json")
	if err != nil {
		return ReviewResultPacket{}, err
	}
	return buildReviewResultWithTemplate(binding, inputs, templateBytes)
}

func buildReviewResultWithTemplate(binding ReviewResultBinding, inputs ReviewResultInputs, templateBytes []byte) (ReviewResultPacket, error) {
	binding = cloneReviewResultBinding(binding)
	if err := validateReviewResultBinding(binding); err != nil {
		return ReviewResultPacket{}, err
	}
	canonicalInputs, claimMethods, err := canonicalReviewResultInputs(binding, inputs)
	if err != nil {
		return ReviewResultPacket{}, err
	}
	var template struct {
		SchemaVersion     string              `json:"schema_version"`
		Stage             string              `json:"stage"`
		Adapter           string              `json:"adapter"`
		AuthoringProtocol string              `json:"authoring_protocol"`
		Title             string              `json:"title"`
		Instruction       string              `json:"instruction"`
		Questions         map[string]Question `json:"questions"`
	}
	if err := decodeOneJSON(templateBytes, &template); err != nil || template.SchemaVersion != "1.0.0" || template.Stage != "S7" || template.Adapter != "review_result" || template.Title == "" || template.Instruction == "" || len(template.Questions) < 7 {
		return ReviewResultPacket{}, fmt.Errorf("unregistered S7 ReviewResult template")
	}
	if template.AuthoringProtocol != "" && template.AuthoringProtocol != "shared-checkout-s7-v1" {
		return ReviewResultPacket{}, fmt.Errorf("unsupported original S7 authoring protocol")
	}
	// The original Plan entry and assignment identify this responsibility;
	// neither prompt bytes nor professional content creates an identity.
	key := fmt.Sprintf("%d:%s%d:%s%d:%s%d:%s:%d", len(binding.RuntimeID), binding.RuntimeID, len(binding.ReviewPlanID), binding.ReviewPlanID, len(binding.AssignmentID), binding.AssignmentID, len(binding.OwnerAgentID), binding.OwnerAgentID, binding.ReviewPlanRevision)
	packetKey := base64.RawURLEncoding.EncodeToString([]byte(key))
	packetID := "packet-" + packetKey
	responsibility := binding.ReviewPlanID
	base := fmt.Sprintf("%s/g%d/S7/%s/%s/%s", reviewResultIdentityPath(binding.RuntimeID), binding.BaselineGeneration, reviewResultIdentityPath(responsibility), reviewResultIdentityPath(binding.AssignmentID), reviewResultPacketPath(packetID))
	answer := makeReviewResultDraftAnswer(binding.ClaimIDs, claimMethods)
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		return ReviewResultPacket{}, err
	}
	canonicalAnswer, err := canonicalJSON(answerBytes)
	if err != nil {
		return ReviewResultPacket{}, err
	}
	read := make([]ReadItem, 0)
	read = append(read, ReadItem{Path: binding.ReviewPlanRef, Purpose: "Read the assigned Claims and their actual source references in the original ReviewPlan."})
	var subjects []reviewPacketSubject
	if err := decodeOneJSON(canonicalInputs.FrozenSubjects, &subjects); err != nil {
		return ReviewResultPacket{}, fmt.Errorf("decode S7 frozen subjects: %w", err)
	}
	for _, subject := range subjects {
		read = append(read, ReadItem{Path: subject.Path, Purpose: "Use this source frozen by the current ReviewPlan while answering the assigned Claims."})
	}
	// The issued work packet carries its declared subject paths and kinds.
	// The author's original Plan remains untouched.
	canonicalInputs.FrozenSubjects, err = json.Marshal(subjects)
	if err != nil {
		return ReviewResultPacket{}, err
	}
	read = append(read, canonicalInputs.Methods...)
	packet := ReviewResultPacket{
		ID: packetID, TemplateID: reviewResultTemplateID,
		Binding: binding, Inputs: canonicalInputs,
		ResultID:        "review-result-" + packetKey,
		IssuanceEventID: "evt-review-result-packet-" + packetID,
		ViewPath:        ".claude/work-packets/" + base + ".json",
		AnswerPath:      reviewResultAnswerPath(packetID, binding.OwnerAgentID),
		Draft:           ReviewResultDraft{Title: template.Title, Instruction: template.Instruction, Read: read, Questions: template.Questions, Answer: canonicalAnswer},
	}
	if !reviewResultIDRE.MatchString(packet.ResultID) {
		return ReviewResultPacket{}, fmt.Errorf("derived ReviewResult ID is invalid")
	}
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		return ReviewResultPacket{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-result-work-packet.schema.json", packetBytes); err != nil {
		return ReviewResultPacket{}, fmt.Errorf("generated S7 ReviewResult packet violates its registered schema: %w", err)
	}
	return packet, nil
}

// All S7 authoring uses the single current answer path.
func reviewResultAnswerPath(packetID, owner string) string {
	return ".loop-submissions/s7/" + reviewResultPacketPath(packetID) + "/" + reviewResultIdentityPath(owner) + "/answer.json"
}

// Original responsibility names are unrestricted in length. Split only
// long names into reversible fixed-size components, preserving prior short paths.
func reviewResultIdentityPath(id string) string {
	if len(id) <= 120 {
		return id
	}
	parts := []string{"i"}
	for len(id) > 120 {
		parts = append(parts, id[:120])
		id = id[120:]
	}
	return strings.Join(append(parts, id), "/")
}

// Split the reversible ID into bounded filesystem components; IDs themselves
// retain the whole original responsibility, never a truncated identifier.
func reviewResultPacketPath(id string) string {
	parts := []string{"p"}
	for len(id) > 120 {
		parts = append(parts, id[:120])
		id = id[120:]
	}
	return strings.Join(append(parts, id), "/")
}

// ReviewResultUsesNormalAuthoring identifies only a canonical current issuance.
func ReviewResultUsesNormalAuthoring(packet ReviewResultPacket) bool {
	return packet.AnswerPath == reviewResultAnswerPath(packet.ID, packet.Binding.OwnerAgentID)
}

// ValidateReviewResultAnswer validates a complete authoring file, preserving
// the immutable machine questionnaire and returning only its canonical
// professional answer object. Domain-specific evidence and Finding checks
// remain with review.SubmitResult.
func ValidateReviewResultAnswer(packet ReviewResultPacket, data []byte) (json.RawMessage, []FieldError) {
	issue := func(pointer, expected, observed string) (json.RawMessage, []FieldError) {
		return nil, []FieldError{{Pointer: pointer, Expected: expected, Observed: observed, Repair: "保留已发行题干，只填写 answer 中的专业判断与观察。答卷顶层仅为 title、instruction、read、questions、answer；不要提交含 packet_id、binding、inputs、draft 的 view packet。"}}
	}
	if len(data) > 20<<20 {
		return issue("/", "JSON <= 20 MiB", "oversized")
	}
	if err := ValidateUniqueJSON(data); err != nil {
		return issue("/", "unique JSON keys", err.Error())
	}
	var draft ReviewResultDraft
	if err := decodeOneStrictJSON(data, &draft); err != nil {
		return issue("/", "one closed S7 ReviewResult questionnaire", err.Error())
	}
	answer := draft.Answer
	canonicalAnswer, err := canonicalJSON(answer)
	if err != nil {
		return issue("/answer", "one JSON answer object", err.Error())
	}
	canonicalAnswer, err = bindReviewFindingMetadata(packet, canonicalAnswer)
	if err != nil {
		return issue("/answer/findings", "Findings for the original assigned Claims", err.Error())
	}
	// Validate the same original closed persisted shape after binding only
	// omitted machine fields. The author file and frozen questions stay intact.
	validated := draft
	validated.Answer = canonicalAnswer
	validatedBytes, err := json.Marshal(validated)
	if err != nil {
		return issue("/answer", "valid professional JSON", err.Error())
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s7.schema.json", validatedBytes); err != nil {
		return issue("/", "registered closed S7 ReviewResult answer", err.Error())
	}
	if pointer, expected, observed := validateCompleteReviewResultAnswer(packet.Binding, canonicalAnswer); pointer != "" {
		return issue(pointer, expected, observed)
	}
	return json.RawMessage(canonicalAnswer), nil
}

// The original packet supplies machine defaults for Findings. A Finding can
// describe another responsibility's Claim; that observation remains the
// author's and must not be rejected merely because the Claim is not assigned
// to this packet.
func bindReviewFindingMetadata(packet ReviewResultPacket, answer []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(answer, &raw); err != nil {
		return nil, err
	}
	if _, exists := raw["findings"]; !exists {
		return answer, nil
	}
	var findings []map[string]json.RawMessage
	if err := json.Unmarshal(raw["findings"], &findings); err != nil {
		return nil, err
	}
	if len(findings) == 0 {
		return answer, nil
	}
	var claims []struct {
		ClaimID string `json:"claim_id"`
		Lens    string `json:"lens"`
	}
	if err := json.Unmarshal(packet.Inputs.Claims, &claims); err != nil {
		return nil, err
	}
	lenses := map[string]string{}
	defaultLens := ""
	for _, claim := range claims {
		lenses[claim.ClaimID] = claim.Lens
		if defaultLens == "" {
			defaultLens = claim.Lens
		}
	}
	for i, finding := range findings {
		var claimID string
		_ = json.Unmarshal(finding["claim_id"], &claimID)
		lens := lenses[claimID]
		if lens == "" {
			lens = defaultLens
		}
		defaults := map[string]string{
			"schema_version": "1.0.0",
			"finding_id":     fmt.Sprintf("finding-%s-%d", packet.ID, i),
			"lens":           lens,
		}
		for key, value := range defaults {
			if _, exists := finding[key]; !exists {
				finding[key], _ = json.Marshal(value)
			}
		}
	}
	var err error
	raw["findings"], err = json.Marshal(findings)
	if err != nil {
		return nil, err
	}
	return json.Marshal(raw)
}

// ValidateReviewResultCompleteness shares the issued answer's Claim closure
// contract with durable receipt validation. It does not establish evidence or
// Finding validity; those remain the original domain consumer's responsibility.
func ValidateReviewResultCompleteness(binding ReviewResultBinding, data []byte) error {
	if pointer, expected, observed := validateCompleteReviewResultAnswer(binding, data); pointer != "" {
		return fmt.Errorf("%s: %s (%s)", pointer, expected, observed)
	}
	return nil
}

func validateCompleteReviewResultAnswer(binding ReviewResultBinding, data []byte) (pointer, expected, observed string) {
	var answer struct {
		ClaimResults []struct {
			ClaimID    string  `json:"claim_id"`
			Conclusion *string `json:"conclusion"`
			Observed   string  `json:"observed"`
		} `json:"claim_results"`
		BlockedClaims []struct {
			ClaimID string `json:"claim_id"`
		} `json:"blocked_claims"`
		Findings []struct {
			ClaimID  string `json:"claim_id"`
			Severity string `json:"severity"`
		} `json:"findings"`
		Verdict *string `json:"verdict"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return "/answer", "valid professional fields", err.Error()
	}
	if answer.Verdict == nil {
		return "/answer/verdict", "explicit professional Result verdict", "unanswered"
	}

	return "", "", ""
}

func makeReviewResultDraftAnswer(claimIDs []string, methods map[string]string) map[string]any {
	claimResults := make([]any, 0, len(claimIDs))
	for _, id := range claimIDs {
		claimResults = append(claimResults, map[string]any{
			"claim_id": id, "conclusion": nil, "method": methods[id], "observed": "", "evidence_refs": []string{},
		})
	}
	return map[string]any{
		"claim_results":  claimResults,
		"blocked_claims": []any{},
		"checks":         []any{},
		"findings":       []any{},
		"deviations":     []string{},
		"site_lost":      []any{},
		"verdict":        nil,
	}
}

type reviewPacketClaim struct {
	ClaimID string `json:"claim_id"`
	Method  string `json:"method"`
}

type reviewPacketSubject struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
}

func validateReviewResultBinding(binding ReviewResultBinding) error {
	if !safeComponent(binding.RuntimeID) || binding.BaselineGeneration < 0 ||
		!validReviewPlanID(binding.ReviewPlanID) || !safeRelative(binding.ReviewPlanRef) || binding.ReviewPlanRef == "." ||
		binding.ReviewPlanRevision < 1 ||
		!validReviewAssignmentID(binding.AssignmentID) ||
		!safeComponent(binding.OwnerAgentID) || len(binding.ClaimIDs) == 0 || len(binding.ClaimIDs) > 256 {
		return fmt.Errorf("S7 ReviewResult requires a complete pinned Plan and Assignment binding")
	}
	if err := identity.ValidateAgentID(binding.OwnerAgentID); err != nil {
		return fmt.Errorf("S7 ReviewResult owner: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range binding.ClaimIDs {
		if !reviewClaimIDRE.MatchString(id) || seen[id] {
			return fmt.Errorf("S7 ReviewResult Claim IDs must be valid, unique, and ordered from the Assignment")
		}
		seen[id] = true
	}
	return nil
}

func canonicalReviewResultInputs(binding ReviewResultBinding, inputs ReviewResultInputs) (ReviewResultInputs, map[string]string, error) {
	claimsJSON, err := canonicalJSON(inputs.Claims)
	if err != nil || len(claimsJSON) > 4<<20 {
		return ReviewResultInputs{}, nil, fmt.Errorf("S7 Claim input must be valid JSON no larger than 4 MiB")
	}
	subjectsJSON, err := canonicalJSON(inputs.FrozenSubjects)
	if err != nil || len(subjectsJSON) > 8<<20 {
		return ReviewResultInputs{}, nil, fmt.Errorf("S7 frozen subjects must be valid JSON no larger than 8 MiB")
	}
	var claims []reviewPacketClaim
	if err := decodeOneJSON(claimsJSON, &claims); err != nil || len(claims) != len(binding.ClaimIDs) {
		return ReviewResultInputs{}, nil, fmt.Errorf("S7 frozen Claims must exactly match the Assignment Claim set")
	}
	methods := make(map[string]string, len(claims))
	for i, claim := range claims {
		if claim.ClaimID != binding.ClaimIDs[i] || !reviewClaimIDRE.MatchString(claim.ClaimID) {
			return ReviewResultInputs{}, nil, fmt.Errorf("S7 frozen Claim order/identity differs from the Assignment at index %d", i)
		}
		methods[claim.ClaimID] = claim.Method
	}
	var subjects []reviewPacketSubject
	if err := decodeOneJSON(subjectsJSON, &subjects); err != nil || len(subjects) > 16384 {
		return ReviewResultInputs{}, nil, fmt.Errorf("S7 frozen subjects must be a bounded array")
	}
	seen := map[string]bool{}
	for _, subject := range subjects {
		key := subject.Path + "\x00" + subject.Kind
		if !safeRelative(subject.Path) || subject.Path == "." || seen[key] {
			return ReviewResultInputs{}, nil, fmt.Errorf("S7 frozen subject identity is invalid or duplicated")
		}
		seen[key] = true
	}
	selected := append([]ReadItem(nil), inputs.Methods...)
	var source *ReviewMethodSource
	if inputs.MethodSource != nil {
		copySource := *inputs.MethodSource
		source = &copySource
		if !safeRelative(source.ManifestRef) || source.ManifestRef == "." {
			return ReviewResultInputs{}, nil, fmt.Errorf("S7 selected methods require a valid frozen manifest source")
		}
	}
	if len(selected) > 256 || len(subjects)+len(selected) > 16384 || (len(selected) > 0 && source == nil) {
		return ReviewResultInputs{}, nil, fmt.Errorf("S7 selected methods require a bounded registered source")
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
	for i, method := range selected {
		parts := strings.Split(strings.TrimPrefix(method.Path, ".claude/"), "/")
		if !safeRelative(method.Path) || len(parts) != 3 || parts[0] != "skills" || !safeComponent(parts[1]) || parts[2] != "SKILL.md" || strings.TrimSpace(method.Purpose) == "" || len(method.Purpose) > 4096 || (i > 0 && selected[i-1].Path == method.Path) {
			return ReviewResultInputs{}, nil, fmt.Errorf("invalid or duplicate S7 selected method reference")
		}
	}
	return ReviewResultInputs{Claims: json.RawMessage(claimsJSON), FrozenSubjects: json.RawMessage(subjectsJSON), Methods: selected, MethodSource: source}, methods, nil
}

func cloneReviewResultBinding(binding ReviewResultBinding) ReviewResultBinding {
	binding.ClaimIDs = append([]string(nil), binding.ClaimIDs...)
	return binding
}

func validReviewPlanID(value string) bool {
	return reviewPlanIDRE.MatchString(value)
}

func validReviewAssignmentID(value string) bool {
	return reviewAssignIDRE.MatchString(value)
}

func canonicalJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty JSON")
	}
	if err := ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func decodeOneJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func decodeOneStrictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
