package qualitygate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Status is an evaluator-owned Quality Gate state. Controller-only states
// such as advanced and blocked are intentionally not representable here.
type Status string

const (
	StatusSatisfied Status = "satisfied"
	StatusNotReady  Status = "not_ready"
	StatusUnknown   Status = "unknown"
)

const (
	ErrorGateUnknown     = "LOOP_GATE_UNKNOWN"
	ErrorTriggerConflict = "LOOP_TRIGGER_CONFLICT"
)

// FileView is the evaluator's read-only artifact boundary.
type FileView interface {
	ReadFile(path string) ([]byte, error)
}

// Input contains the immutable facts observed for one gate evaluation.
type Input struct {
	Root          string
	Snapshot      runtime.Snapshot
	TransitionID  string
	GateID        string
	AffectedPaths []string
	Files         FileView
}

// Evaluation is the pure evaluator result consumed by the Controller.
type Evaluation struct {
	Status              Status   `json:"status"`
	GateID              string   `json:"gate_id"`
	CandidateTransition string   `json:"candidate_transition"`
	ObservedRevision    int      `json:"observed_revision"`
	Fingerprint         string   `json:"fingerprint"`
	Missing             []string `json:"missing"`
	EvidenceRefs        []string `json:"evidence_refs"`
	Conflicts           []string `json:"conflicts"`
	ErrorCode           string   `json:"error_code"`
	TransitionCommitted bool     `json:"transition_committed"`
	NextCursor          string   `json:"next_cursor"`
}

// Evaluator is the read-only Quality Gate boundary consumed by Controllers.
type Evaluator interface {
	Evaluate(context.Context, Input) (Evaluation, error)
}

// Engine evaluates gates without mutating Runtime or invoking transitions.
type Engine struct {
	registry *Registry
}

// NewEvaluator constructs a pure evaluator over a validated registry.
func NewEvaluator(registry *Registry) *Engine {
	return &Engine{registry: registry}
}

// RequestedEvents returns qualified transition events from valid evidence at
// the current cursor. The Controller supplies these facts to the catalog
// selector without re-implementing evidence qualification.
func (e *Engine) RequestedEvents(input Input) []string {
	return e.qualifiedRequestedEvents(input, verifiedCurrentDocuments(input))
}

func evidenceKindsEqual(requirementKind, actualKind string) bool {
	if requirementKind == actualKind {
		return true
	}
	switch requirementKind {
	case "document_review_record":
		return actualKind == "document_review"
	case "clean_round_record":
		return actualKind == "clean_round"
	case "bug_batch_record":
		return actualKind == "bug"
	case "targeted_reverification_record":
		return actualKind == "targeted_reverification"
	case "delivery_review_record":
		return actualKind == "delivery_review"
	case "finding_record", "root_cause_record", "repair_record":
		return actualKind == "bug"
	case "team_manifest_record":
		return actualKind == "builder_report" || actualKind == "team_manifest"
	case "completion_report":
		return actualKind == "agent_completion" || actualKind == "completion_report"
	case "builder_report_record":
		return actualKind == "builder_report" || actualKind == "agent_completion"
	case "activation_record":
		return actualKind == "agent_activation"
	case "pause_record":
		// Schema evidence.kind enum uses human_decision; gate requirements
		// still name pause_record (TR-014/024). Accept both spellings.
		return actualKind == "pause" || actualKind == "human_decision"
	}
	return strings.TrimSuffix(requirementKind, "_record") == actualKind
}

// Evaluate reports unknown for unregistered gates. Registered gate semantics
// are supplied incrementally by the registry.
func (e *Engine) Evaluate(ctx context.Context, input Input) (Evaluation, error) {
	result := Evaluation{
		Status:           StatusUnknown,
		GateID:           input.GateID,
		ObservedRevision: input.Snapshot.Revision,
		Missing:          []string{},
		EvidenceRefs:     []string{},
		Conflicts:        []string{},
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = []string{"evaluation:context_canceled"}
			return result, nil
		default:
		}
	}
	spec, ok := e.registry.Lookup(input.GateID)
	if !ok {
		result.ErrorCode = ErrorGateUnknown
		return result, nil
	}
	if input.TransitionID != "" && input.TransitionID != spec.TransitionID {
		result.ErrorCode = ErrorGateUnknown
		result.Conflicts = []string{"transition:" + input.TransitionID + ":gate_mismatch"}
		return result, nil
	}
	cursorState, cursorPhase := currentStatePhase(input.Snapshot.State)
	if cursorState != spec.CursorState || (spec.CursorPhase != "" && cursorPhase != spec.CursorPhase) {
		result.ErrorCode = ErrorGateUnknown
		result.Conflicts = []string{"cursor:" + currentCursor(input.Snapshot.State) + ":gate_mismatch"}
		return result, nil
	}
	result.CandidateTransition = spec.TransitionID
	result.NextCursor = currentCursor(input.Snapshot.State)
	documents := verifiedCurrentDocuments(input)
	if events := e.qualifiedRequestedEvents(input, documents); len(events) > 1 {
		result.Status = StatusUnknown
		result.ErrorCode = ErrorTriggerConflict
		result.Conflicts = events
		result.CandidateTransition = ""
		return result, nil
	}

	if input.GateID == "GATE-PLANNING-DESIGN-COMPLETE" {
		return evaluatePlanningDesign(input, result, spec), nil
	}
	if input.GateID == "GATE-PLANNING-CONTRACTS-COMPLETE" {
		return evaluatePlanningArtifact(input, result, spec, documents, "contract", "locked", "document:contract:locked"), nil
	}
	if input.GateID == "GATE-PLANNING-TASKS-COMPLETE" {
		return evaluatePlanningArtifact(input, result, spec, documents, "task", "complete", "document:task:complete"), nil
	}
	return evaluateRegisteredGate(input, result, spec, documents), nil
}

type documentFact struct {
	Kind          string
	Path          string
	Version       string
	SHA256        string
	Status        string
	Generation    int
	AuthorAgentID string
}

type subjectRef struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type evidenceEnvelope struct {
	SchemaVersion          string       `json:"schema_version"`
	EvidenceID             string       `json:"evidence_id"`
	Kind                   string       `json:"kind"`
	RuntimeID              string       `json:"runtime_id"`
	BaselineGeneration     int          `json:"baseline_generation"`
	ProducerAgentID        string       `json:"producer_agent_id"`
	ProducerResponsibility string       `json:"producer_responsibility"`
	ReviewRound            int          `json:"review_round"`
	SubjectRefs            []subjectRef `json:"subject_refs"`
	Conclusion             string       `json:"conclusion"`
	RequestedEvent         string       `json:"requested_event"`
	InvalidatedBy          string       `json:"invalidated_by"`
	TaskID                 string       `json:"task_id"`
}

func evaluatePlanningDesign(input Input, result Evaluation, spec GateSpec) Evaluation {
	state := input.Snapshot.State
	generation := nestedInt(state, "baseline", "generation")
	documents := currentDocuments(state, generation)

	requiredKinds := []string{"req", "design"}
	relevant := make([]documentFact, 0, len(requiredKinds))
	for _, kind := range requiredKinds {
		document, ok := findCurrentDocument(documents, kind, input.Files)
		if !ok {
			result.Missing = append(result.Missing, "document:"+kind+":locked")
			continue
		}
		relevant = append(relevant, document)
	}
	if len(result.Missing) > 0 {
		sort.Strings(result.Missing)
		result.Status = StatusNotReady
		result.Fingerprint = fingerprint(result.GateID, spec.SemanticVersion, state, generation, relevant, nil)
		return result
	}

	return evaluateRegisteredGate(input, result, spec, relevant)
}

func evaluatePlanningArtifact(
	input Input,
	result Evaluation,
	spec GateSpec,
	documents []documentFact,
	kind string,
	status string,
	missing string,
) Evaluation {
	found := false
	for _, document := range documents {
		if document.Kind == kind && document.Status == status {
			found = true
			break
		}
	}
	if !found {
		result.Status = StatusNotReady
		result.Missing = []string{missing}
		result.Fingerprint = fingerprint(
			result.GateID,
			spec.SemanticVersion,
			input.Snapshot.State,
			nestedInt(input.Snapshot.State, "baseline", "generation"),
			documents,
			nil,
		)
		return result
	}
	return evaluateRegisteredGate(input, result, spec, documents)
}

func evaluateRegisteredGate(input Input, result Evaluation, spec GateSpec, documents []documentFact) Evaluation {
	state := input.Snapshot.State
	generation := nestedInt(state, "baseline", "generation")
	runtimeID, _ := state["runtime_id"].(string)
	currentRound := nestedInt(state, "review", "round")
	if conflicts := unauthorizedProducerConflicts(input, spec, documents, generation, currentRound); len(conflicts) > 0 {
		result.Status = StatusUnknown
		result.ErrorCode = ErrorGateUnknown
		result.Conflicts = conflicts
		return result
	}
	var evidenceIDs []string
	for _, requirement := range spec.EvidenceRequirements {
		qualified, conflicts := qualifiedEvidence(
			state,
			input.Files,
			runtimeID,
			generation,
			currentRound,
			requirement,
			documents,
		)
		if len(conflicts) > 0 {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, conflicts...)
			sort.Strings(result.Conflicts)
			return result
		}
		if len(qualified) < requirement.MinCount {
			result.Missing = append(result.Missing, evidenceMissingKey(spec, requirement))
			continue
		}
		evidenceIDs = append(evidenceIDs, qualified...)
	}
	result.EvidenceRefs = sortedUnique(evidenceIDs)
	result.Missing = sortedUnique(result.Missing)
	if len(result.Missing) > 0 {
		result.Status = StatusNotReady
	} else {
		result.Status = StatusSatisfied
	}
	if result.Status == StatusSatisfied && result.GateID == "GATE-DOCUMENT-PASS" {
		applyDocumentPassIndependence(input, &result, documents)
	}
	if result.Status == StatusSatisfied && result.GateID == "GATE-BUILDER-BATCH-READY" {
		applyBuilderBatchCompleteness(input, &result)
	}
	result.Fingerprint = fingerprint(result.GateID, spec.SemanticVersion, state, generation, documents, result.EvidenceRefs)
	return result
}

func evidenceMissingKey(spec GateSpec, requirement EvidenceRequirement) string {
	key := "evidence:" + requirement.Kind
	sameKind := 0
	for _, candidate := range spec.EvidenceRequirements {
		if candidate.Kind == requirement.Kind {
			sameKind++
		}
	}
	if sameKind > 1 && len(requirement.Responsibilities) == 1 {
		key += ":" + requirement.Responsibilities[0]
	}
	return key
}

func unauthorizedProducerConflicts(
	input Input,
	spec GateSpec,
	documents []documentFact,
	generation int,
	currentRound int,
) []string {
	allowed := make(map[string]map[string]struct{})
	currentRoundKinds := make(map[string]bool)
	for _, requirement := range spec.EvidenceRequirements {
		if allowed[requirement.Kind] == nil {
			allowed[requirement.Kind] = make(map[string]struct{})
		}
		for _, responsibility := range requirement.Responsibilities {
			allowed[requirement.Kind][responsibility] = struct{}{}
		}
		if requirement.CurrentReviewRound {
			currentRoundKinds[requirement.Kind] = true
		}
	}
	runtimeID, _ := input.Snapshot.State["runtime_id"].(string)
	raw, _ := input.Snapshot.State["evidence"].([]any)
	var conflicts []string
	for _, item := range raw {
		index, _ := item.(map[string]any)
		kind := stringValue(index["kind"])
		responsibilities, relevant := allowed[kind]
		if index == nil || !relevant ||
			stringValue(index["status"]) != "valid" ||
			intValue(index["baseline_generation"]) != generation ||
			index["invalidated_by"] != nil ||
			(currentRoundKinds[kind] && intValue(index["review_round"]) != currentRound) ||
			input.Files == nil {
			continue
		}
		data, err := input.Files.ReadFile(stringValue(index["path"]))
		if err != nil || sha256Hex(data) != stringValue(index["sha256"]) {
			continue
		}
		var envelope evidenceEnvelope
		if json.Unmarshal(data, &envelope) != nil ||
			envelope.EvidenceID != stringValue(index["id"]) ||
			envelope.Kind != kind ||
			envelope.RuntimeID != runtimeID ||
			envelope.BaselineGeneration != generation ||
			envelope.ProducerResponsibility != stringValue(index["responsibility_id"]) ||
			!subjectsMatch(envelope.SubjectRefs, documents) {
			continue
		}
		if _, ok := responsibilities[envelope.ProducerResponsibility]; !ok {
			conflicts = append(conflicts, "evidence:"+envelope.EvidenceID+":producer")
		}
	}
	return sortedUnique(conflicts)
}

func verifiedCurrentDocuments(input Input) []documentFact {
	generation := nestedInt(input.Snapshot.State, "baseline", "generation")
	documents := currentDocuments(input.Snapshot.State, generation)
	verified := make([]documentFact, 0, len(documents))
	for _, document := range documents {
		if input.Files == nil || document.Path == "" || document.SHA256 == "" {
			continue
		}
		data, err := input.Files.ReadFile(document.Path)
		if err == nil && sha256Hex(data) == document.SHA256 {
			verified = append(verified, document)
		}
	}
	return verified
}

func (e *Engine) qualifiedRequestedEvents(input Input, documents []documentFact) []string {
	state := input.Snapshot.State
	runtimeID, _ := state["runtime_id"].(string)
	generation := nestedInt(state, "baseline", "generation")
	currentRound := nestedInt(state, "review", "round")
	cursorState, cursorPhase := currentStatePhase(state)
	candidates := e.registry.specsForCursor(cursorState, cursorPhase)
	if len(candidates) < 2 {
		return nil
	}

	raw, _ := state["evidence"].([]any)
	var events []string
	for _, item := range raw {
		index, _ := item.(map[string]any)
		if index == nil ||
			stringValue(index["status"]) != "valid" ||
			intValue(index["baseline_generation"]) != generation ||
			index["invalidated_by"] != nil {
			continue
		}
		path := stringValue(index["path"])
		if input.Files == nil || path == "" {
			continue
		}
		data, err := input.Files.ReadFile(path)
		if err != nil || sha256Hex(data) != stringValue(index["sha256"]) {
			continue
		}
		var envelope evidenceEnvelope
		if json.Unmarshal(data, &envelope) != nil ||
			envelope.RequestedEvent == "" ||
			envelope.EvidenceID != stringValue(index["id"]) ||
			envelope.Kind != stringValue(index["kind"]) ||
			envelope.RuntimeID != runtimeID ||
			envelope.BaselineGeneration != generation ||
			envelope.ProducerAgentID == "" ||
			envelope.ProducerResponsibility != stringValue(index["responsibility_id"]) ||
			!containsAny(index["produced_by"], envelope.ProducerAgentID) ||
			!subjectsMatch(envelope.SubjectRefs, documents) {
			continue
		}
		for _, candidate := range candidates {
			if candidate.TransitionEvent != envelope.RequestedEvent {
				continue
			}
			if envelopeMatchesAnyRequirement(envelope, index, candidate.EvidenceRequirements, currentRound) {
				events = append(events, envelope.RequestedEvent)
			}
		}
	}
	return sortedUnique(events)
}

func envelopeMatchesAnyRequirement(envelope evidenceEnvelope, index map[string]any, requirements []EvidenceRequirement, currentRound int) bool {
	for _, requirement := range requirements {
		if !evidenceKindsEqual(requirement.Kind, envelope.Kind) ||
			!containsString(requirement.Responsibilities, envelope.ProducerResponsibility) ||
			!containsString(requirement.Conclusions, envelope.Conclusion) {
			continue
		}
		if requirement.CurrentReviewRound &&
			(envelope.ReviewRound != currentRound || intValue(index["review_round"]) != currentRound) {
			continue
		}
		return true
	}
	return false
}

func qualifiedEvidence(
	state map[string]any,
	files FileView,
	runtimeID string,
	generation int,
	currentRound int,
	requirement EvidenceRequirement,
	documents []documentFact,
) ([]string, []string) {
	raw, _ := state["evidence"].([]any)
	var valid []string
	var conflicts []string
	for _, item := range raw {
		index, _ := item.(map[string]any)
		if index == nil || !evidenceKindsEqual(requirement.Kind, stringValue(index["kind"])) {
			continue
		}
		if stringValue(index["status"]) != "valid" ||
			intValue(index["baseline_generation"]) != generation ||
			index["invalidated_by"] != nil {
			continue
		}
		if requirement.CurrentReviewRound && intValue(index["review_round"]) != currentRound {
			continue
		}
		path := stringValue(index["path"])
		if files == nil || path == "" {
			conflicts = append(conflicts, "evidence:"+stringValue(index["id"])+":unreadable")
			continue
		}
		data, err := files.ReadFile(path)
		if err != nil {
			conflicts = append(conflicts, "evidence:"+stringValue(index["id"])+":unreadable")
			continue
		}
		if sha256Hex(data) != stringValue(index["sha256"]) {
			continue
		}
		var envelope evidenceEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			conflicts = append(conflicts, "evidence:"+stringValue(index["id"])+":schema")
			continue
		}
		if envelope.SchemaVersion == "" ||
			envelope.EvidenceID != stringValue(index["id"]) ||
			!evidenceKindsEqual(requirement.Kind, envelope.Kind) ||
			envelope.RuntimeID != runtimeID ||
			envelope.BaselineGeneration != generation ||
			envelope.ProducerAgentID == "" ||
			envelope.ProducerResponsibility != stringValue(index["responsibility_id"]) ||
			!containsAny(index["produced_by"], envelope.ProducerAgentID) {
			conflicts = append(conflicts, "evidence:"+stringValue(index["id"])+":schema")
			continue
		}
		if requirement.CurrentReviewRound &&
			(envelope.ReviewRound != currentRound || envelope.ReviewRound != intValue(index["review_round"])) {
			continue
		}
		if envelope.InvalidatedBy != "" ||
			!containsString(requirement.Conclusions, envelope.Conclusion) ||
			(requirement.RequestedEvent != "" && envelope.RequestedEvent != requirement.RequestedEvent) ||
			!subjectsMatch(envelope.SubjectRefs, documents) {
			continue
		}
		if !containsString(requirement.Responsibilities, envelope.ProducerResponsibility) {
			continue
		}
		valid = append(valid, envelope.EvidenceID)
	}
	return sortedUnique(valid), sortedUnique(conflicts)
}

func applyDocumentPassIndependence(input Input, result *Evaluation, documents []documentFact) {
	envelopes := evidenceEnvelopesByID(input, result.EvidenceRefs)
	producers := make(map[string]struct{}, len(envelopes))
	authors := make(map[string]struct{})
	for _, document := range documents {
		if document.AuthorAgentID != "" {
			authors[document.AuthorAgentID] = struct{}{}
		}
	}
	for _, envelope := range envelopes {
		producers[envelope.ProducerAgentID] = struct{}{}
		if _, isAuthor := authors[envelope.ProducerAgentID]; isAuthor {
			result.Missing = append(result.Missing, "evidence:reviewer_not_candidate_author")
		}
		if !exactSubjects(envelope.SubjectRefs, documents) {
			result.Missing = append(result.Missing, "evidence:exact_document_manifest")
		}
	}
	if len(envelopes) != len(result.EvidenceRefs) {
		result.Missing = append(result.Missing, "evidence:exact_document_manifest")
	}
	if len(producers) != len(envelopes) {
		result.Missing = append(result.Missing, "evidence:independent_document_reviewers")
	}
	result.Missing = sortedUnique(result.Missing)
	if len(result.Missing) > 0 {
		result.Status = StatusNotReady
	}
}

func exactSubjects(subjects []subjectRef, documents []documentFact) bool {
	if len(subjects) != len(documents) {
		return false
	}
	wanted := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		wanted[document.Path+"|"+document.Version+"|"+document.SHA256] = struct{}{}
	}
	for _, subject := range subjects {
		key := subject.Path + "|" + subject.Version + "|" + subject.SHA256
		if _, ok := wanted[key]; !ok {
			return false
		}
		delete(wanted, key)
	}
	return len(wanted) == 0
}

func evidenceEnvelopesByID(input Input, ids []string) []evidenceEnvelope {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	raw, _ := input.Snapshot.State["evidence"].([]any)
	envelopes := make([]evidenceEnvelope, 0, len(ids))
	for _, item := range raw {
		index, _ := item.(map[string]any)
		if index == nil {
			continue
		}
		if _, ok := wanted[stringValue(index["id"])]; !ok || input.Files == nil {
			continue
		}
		data, err := input.Files.ReadFile(stringValue(index["path"]))
		if err != nil || sha256Hex(data) != stringValue(index["sha256"]) {
			continue
		}
		var envelope evidenceEnvelope
		if json.Unmarshal(data, &envelope) == nil {
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes
}

func applyBuilderBatchCompleteness(input Input, result *Evaluation) {
	completed := make(map[string]struct{})
	for _, envelope := range evidenceEnvelopesByID(input, result.EvidenceRefs) {
		if evidenceKindsEqual("completion_report", envelope.Kind) && envelope.TaskID != "" {
			completed[envelope.TaskID] = struct{}{}
		}
	}
	entities, _ := input.Snapshot.State["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, item := range tasks {
		task, _ := item.(map[string]any)
		if task == nil || !activatedTaskState(stringValue(task["state"])) {
			continue
		}
		taskID := stringValue(task["id"])
		if _, ok := completed[taskID]; !ok {
			result.Missing = append(result.Missing, "evidence:completion_report:"+taskID)
		}
	}
	result.Missing = sortedUnique(result.Missing)
	if len(result.Missing) > 0 {
		result.Status = StatusNotReady
	}
}

func activatedTaskState(state string) bool {
	switch state {
	case "in_progress", "review", "done":
		return true
	default:
		return false
	}
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func currentDocuments(state map[string]any, generation int) []documentFact {
	raw, _ := state["documents"].([]any)
	documents := make([]documentFact, 0, len(raw))
	for _, item := range raw {
		value, _ := item.(map[string]any)
		if value == nil {
			continue
		}
		document := documentFact{
			Kind:          stringValue(value["kind"]),
			Path:          stringValue(value["path"]),
			Version:       stringValue(value["version"]),
			SHA256:        stringValue(value["sha256"]),
			Status:        stringValue(value["status"]),
			Generation:    intValue(value["generation"]),
			AuthorAgentID: stringValue(value["author_agent_id"]),
		}
		if document.Generation == generation {
			documents = append(documents, document)
		}
	}
	return documents
}

func findCurrentDocument(documents []documentFact, kind string, files FileView) (documentFact, bool) {
	for _, document := range documents {
		if document.Kind != kind || document.Status != "locked" || document.Path == "" || document.SHA256 == "" || files == nil {
			continue
		}
		data, err := files.ReadFile(document.Path)
		if err == nil && sha256Hex(data) == document.SHA256 {
			return document, true
		}
	}
	return documentFact{}, false
}

func subjectsMatch(subjects []subjectRef, documents []documentFact) bool {
	// Empty subject_refs means the evidence carries no document constraint
	// (clean_round / bug_batch / release_audit style records). Non-empty
	// refs must still fingerprint-match current documents.
	if len(subjects) == 0 {
		return true
	}
	for _, subject := range subjects {
		matched := false
		for _, document := range documents {
			if subject.Path == document.Path && subject.Version == document.Version && subject.SHA256 == document.SHA256 {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func fingerprint(gateID, semanticVersion string, state map[string]any, generation int, documents []documentFact, evidenceIDs []string) string {
	facts := []string{
		"gate=" + gateID,
		"semantic_version=" + semanticVersion,
		"cursor=" + currentCursor(state),
		fmt.Sprintf("generation=%d", generation),
	}
	for _, document := range documents {
		facts = append(facts, "artifact="+document.Path+"|"+document.Version+"|"+document.SHA256)
	}
	for _, evidenceID := range evidenceIDs {
		facts = append(facts, "evidence="+evidenceID)
	}
	sort.Strings(facts)
	return "sha256:" + sha256Hex([]byte(strings.Join(facts, "\n")))
}

func currentCursor(state map[string]any) string {
	current, phase := currentStatePhase(state)
	if phase != "" {
		current += "." + phase
	}
	return current
}

func currentStatePhase(state map[string]any) (string, string) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return stringValue(lifecycle["state"]), stringValue(lifecycle["phase"])
}

func nestedInt(state map[string]any, object, field string) int {
	value, _ := state[object].(map[string]any)
	return intValue(value[field])
}

func containsAny(value any, want string) bool {
	items, _ := value.([]any)
	for _, item := range items {
		if stringValue(item) == want {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
