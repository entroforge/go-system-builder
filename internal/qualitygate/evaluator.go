package qualitygate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/evidence"
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

type observedS10View struct {
	FileView
	observations map[string]string
}

func (v observedS10View) ReadFile(path string) ([]byte, error) {
	data, err := v.FileView.ReadFile(path)
	if err != nil {
		v.observations[path] = "unreadable:" + err.Error()
	} else {
		v.observations[path] = sha256Hex(data)
	}
	return data, err
}

// fileDirLister is the optional directory-listing capability a FileView may
// implement so the planning gates can discover disk-declared artifacts
// (documents[] registration is produced by the gated transitions
// themselves, so requiring it up front deadlocks the auto-advance path).
type fileDirLister interface {
	ReadDir(dir string) ([]os.DirEntry, error)
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
	return evidence.DefaultCatalog().Accepts(requirementKind, actualKind)
}

// Evaluate reports unknown for unregistered gates. Registered gate semantics
// are supplied incrementally by the registry.
func (e *Engine) Evaluate(ctx context.Context, input Input) (result Evaluation, err error) {
	observations := map[string]string{}
	isS10 := strings.HasPrefix(input.GateID, "GATE-ACCEPTANCE-") || strings.HasPrefix(input.GateID, "GATE-RELEASE-AUDIT-")
	if isS10 && input.Files != nil {
		input.Files = observedS10View{input.Files, observations}
	}
	defer func() {
		if result.Fingerprint == "" {
			result.Fingerprint = fingerprint(result.GateID, "diagnostic-v1", input.Snapshot.State, nestedInt(input.Snapshot.State, "baseline", "generation"), nil, append(append([]string{}, result.Conflicts...), result.Missing...))
		}
		if isS10 {
			data, _ := json.Marshal(struct {
				Fingerprint        string
				Observations       map[string]string
				Conflicts, Missing []string
			}{result.Fingerprint, observations, result.Conflicts, result.Missing})
			result.Fingerprint = "sha256:" + sha256Hex(data)
		}
	}()
	result = Evaluation{
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
	if input.GateID == "GATE-DOCUMENT-PASS" {
		// Registered-document drift check: every current-
		// generation registered document must still match its disk sha.
		// Without this, exactSubjects compares against the verified subset
		// only and a document the reviewers never saw can be re-registered
		// from disk and locked into building by TR-003's commit.
		if conflicts := registeredDocumentDrift(input); len(conflicts) > 0 {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = conflicts
			return result, nil
		}
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
	ID            string
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
	// Builder-result content (L3-S6 §7.3): the gate consumes the completion
	// facts instead of counting envelopes by task_id alone.
	Checks          []envelopeCheck `json:"checks,omitempty"`
	ChangedPaths    []string        `json:"changed_paths,omitempty"`
	ScopeDeviations []string        `json:"scope_deviations,omitempty"`
	// S10 acceptance/release-audit evidence points at a structured completion
	// ledger. The human-readable ACC/audit Markdown remains a report; this
	// reference makes the finite coverage and counterevidence contract
	// consumable by the gate without parsing prose.
	AuditManifestPath   string `json:"audit_manifest_path,omitempty"`
	AuditManifestSHA256 string `json:"audit_manifest_sha256,omitempty"`
}

// envelopeCheck mirrors the completion-report checkResult shape
// (name/command/result/evidence_ref) the Builder submits.
type envelopeCheck struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Result  string `json:"result"`
}

func evaluatePlanningDesign(input Input, result Evaluation, spec GateSpec) Evaluation {
	state := input.Snapshot.State
	generation := nestedInt(state, "baseline", "generation")
	documents := currentDocuments(state, generation)

	requiredKinds := []string{"req", "design"}
	relevant := make([]documentFact, 0, len(requiredKinds))
	for _, kind := range requiredKinds {
		document, ok := findCurrentDocument(documents, kind, input.Files)
		if ok {
			relevant = append(relevant, document)
			continue
		}
		// Disk fallback (same family as the planning artifact
		// gates): a locked REQ / ARCH document declared on disk satisfies
		// the precondition — the registration into documents[] happens at
		// PTR-PLAN-01's commit.
		if diskFacts, listed := diskDeclaredArtifacts(input, kind, "locked"); listed {
			for _, fact := range diskFacts {
				if fact.Kind == kind {
					relevant = append(relevant, fact)
					ok = true
					break
				}
			}
		}
		if !ok {
			result.Missing = append(result.Missing, "document:"+kind+":locked")
		}
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
		// Disk fallback: the agent's producible fact is the
		// file itself — a contract declaring `Status: locked` / a task
		// declaring `Status: complete` on disk satisfies the gate's
		// precondition; the commit-time registration into documents[]
		// (with journal) remains the transition's job.
		if diskFacts, ok := diskDeclaredArtifacts(input, kind, status); ok {
			documents = append(documents, diskFacts...)
			for _, fact := range diskFacts {
				if fact.Kind == kind && fact.Status == status {
					found = true
					break
				}
			}
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
		if requirement.ProducedByTransition {
			// The transition engine validates the canonical generated token and
			// materializes this evidence while committing the transition. It
			// cannot be required in the pre-transition snapshot without making
			// the automatic gate permanently not_ready.
			continue
		}
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
	if result.GateID == "GATE-BUILDER-BATCH-READY" {
		// Unconditional: the exact-set evaluation runs even when the base
		// requirements are short, so the missing matrix names each
		// unproven TASK (and a lost/empty batch registry) instead of only
		// the aggregate evidence token.
		applyBuilderBatchCompleteness(input, &result)
	}
	if result.GateID == "GATE-VERIFY-CLEAN-ROUND-PASSED" || result.GateID == "GATE-ACCEPTANCE-COMPLETE" || result.GateID == "GATE-RELEASE-AUDIT-APPROVED" {
		// L3-S7 §10: recompute the machine CleanRound over the ReviewPlan's
		// exact Claim set; an evidence-only pass is not sufficient.
		applyCleanRoundGate(input, &result)
	}
	if result.GateID == "GATE-VERIFY-BLOCKING-FINDING" {
		// L3-S7 §3.7: the sealed ObservationBatch must carry the exact
		// current-round Finding set with the drain policy respected.
		applyObservationBatchGate(input, &result)
	}
	if result.GateID == "GATE-ACCEPTANCE-COMPLETE" || result.GateID == "GATE-ACCEPTANCE-REVIEW-REQUIRED" || result.GateID == "GATE-RELEASE-AUDIT-APPROVED" || result.GateID == "GATE-RELEASE-AUDIT-BLOCKED" || result.GateID == "GATE-RELEASE-AUDIT-REVIEW-REQUIRED" {
		// L3-S10 §1.2: a generic PASS/APPROVED envelope is not enough. The
		// finite coverage inventory and counterevidence ledger are the
		// machine-consumed anti-shortcut contract. RC-05 (S10-8): the blocked
		// route re-checks too — a structurally incomplete ledger cannot enter
		// TR-018 just because its conclusion says "blocked"; the blocked
		// manifest itself must still be a complete, evidence-linked record.
		// RC-16: the review_required acceptance route is under the same
		// manifest re-hash gate — without it a tampered review_required
		// manifest sails through the gate unverified.
		applyS10ManifestGate(input, &result)
	}
	result.Fingerprint = fingerprint(result.GateID, spec.SemanticVersion, state, generation, documents, append(append(append([]string{}, result.EvidenceRefs...), result.Conflicts...), result.Missing...))
	return result
}

// latestS10EvidenceID returns the most recently registered qualifying record of
// the required kind. S10 records accumulate (every correction registers a new
// fingerprinted envelope), and evidence rows cannot be retired outside a
// transition commit, so the gate must judge the current claim -- the latest
// registration -- instead of the oldest id that happens to sort first.
func latestS10EvidenceID(input Input, refs []string, kind string) string {
	qualified := make(map[string]struct{}, len(refs))
	for _, id := range refs {
		qualified[id] = struct{}{}
	}
	raw, _ := input.Snapshot.State["evidence"].([]any)
	for i := len(raw) - 1; i >= 0; i-- {
		index, _ := raw[i].(map[string]any)
		if index == nil {
			continue
		}
		id := stringValue(index["id"])
		if _, ok := qualified[id]; !ok {
			continue
		}
		if evidenceKindsEqual(kind, stringValue(index["kind"])) {
			return id
		}
	}
	return ""
}

func applyS10ManifestGate(input Input, result *Evaluation) {
	kinds := []string{"acceptance"}
	if strings.HasPrefix(result.GateID, "GATE-RELEASE-AUDIT-") {
		kinds = append(kinds, "release_audit")
	}
	for _, kind := range kinds {
		id := latestS10EvidenceID(input, result.EvidenceRefs, kind+"_record")
		if id == "" {
			continue
		}
		row, err := acceptance.SelectS10Candidate(input.Snapshot.State, kind)
		if err != nil {
			result.Status = StatusNotReady
			result.Missing = append(result.Missing, err.Error())
			continue
		}
		envelope, _ := s10EnvelopeByID(input, id)
		if envelope.AuditManifestPath == "" || envelope.AuditManifestSHA256 == "" {
			result.Missing = append(result.Missing, "s10:"+kind+"_manifest:"+id)
			result.Status = StatusNotReady
			continue
		}
		candidate, err := acceptance.ValidateS10Candidate(acceptance.CandidateInput{State: input.Snapshot.State, Files: input.Files, Kind: kind, EvidenceID: stringValue(row["id"]), AffectedPaths: input.AffectedPaths})
		if err != nil {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, "s10:"+kind+"_manifest:"+id+":invalid:"+err.Error())
			continue
		}
		retained := []string{}
		for _, id := range result.EvidenceRefs {
			env, ok := s10EnvelopeByID(input, id)
			if !ok || !evidenceKindsEqual(kind+"_record", env.Kind) || id == candidate.EvidenceID {
				retained = append(retained, id)
			}
		}
		result.EvidenceRefs = retained
	}
	result.Missing = sortedUnique(result.Missing)
	result.Conflicts = sortedUnique(result.Conflicts)
}

func s10EnvelopeByID(input Input, id string) (evidenceEnvelope, bool) {
	for _, envelope := range evidenceEnvelopesByID(input, []string{id}) {
		return envelope, true
	}
	return evidenceEnvelope{}, false
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
	kindCurrentRound := func(kind string) bool {
		for requirementKind := range currentRoundKinds {
			if evidenceKindsEqual(requirementKind, kind) {
				return true
			}
		}
		return false
	}
	runtimeID, _ := input.Snapshot.State["runtime_id"].(string)
	raw, _ := input.Snapshot.State["evidence"].([]any)
	authorized := make(map[string]int)
	unauthorized := make(map[string][]string)
	for _, item := range raw {
		index, _ := item.(map[string]any)
		if index == nil {
			continue
		}
		kind := stringValue(index["kind"])
		// Requirements name catalog slots; the persisted kind may be a
		// legacy alias (review_result vs the pre-S7 per-lens kinds
		// delivery_review/qa_review/e2e_review), so the lookup goes through
		// the alias-aware comparison.
		var responsibilities map[string]struct{}
		slot := kind
		relevant := false
		for requirementKind, resp := range allowed {
			if evidenceKindsEqual(requirementKind, kind) {
				responsibilities = resp
				slot = requirementKind
				relevant = true
				break
			}
		}
		if !relevant ||
			stringValue(index["status"]) != "valid" ||
			intValue(index["baseline_generation"]) != generation ||
			index["invalidated_by"] != nil ||
			(kindCurrentRound(kind) && intValue(index["review_round"]) != currentRound) ||
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
			unauthorized[slot] = append(unauthorized[slot], envelope.EvidenceID)
		} else {
			authorized[slot]++
		}
	}
	// Deferred like the naming errors in qualifiedEvidence: an unauthorized
	// producer only explains an unfilled slot when no authorized record for
	// that slot is registered. Reporting it unconditionally would make the
	// gate unsatisfiable -- the operator can register a qualified record but
	// cannot retire the foreign one, so the conflict would never clear.
	var conflicts []string
	for slot, ids := range unauthorized {
		if authorized[slot] > 0 {
			continue
		}
		for _, id := range ids {
			conflicts = append(conflicts, "evidence:"+id+":producer")
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
		if _, err := acceptance.Contract(stringValue(index["kind"])); err == nil {
			selected, err := acceptance.SelectS10Candidate(state, stringValue(index["kind"]))
			if err != nil || selected["id"] != index["id"] {
				continue
			}
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
	if _, err := acceptance.Contract(requirement.Kind); err == nil {
		selected, selectErr := acceptance.SelectS10Candidate(state, requirement.Kind)
		if selectErr != nil {
			return nil, nil
		}
		raw = []any{selected}
	}
	var valid []string
	var conflicts []string
	var mismatched []string
	// Deferred like conclusion mismatches below: evidence rows cannot be
	// retired outside a transition commit, and the documented recovery for a
	// mis-registered envelope is "register new qualified evidence" — which
	// leaves the superseded row pointing at a shared path. An unconditional
	// schema conflict would then make the gate permanently unsatisfiable;
	// the deferred conflicts surface only when too few records qualify.
	var deferredSchema []string
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
			if _, err := acceptance.Contract(requirement.Kind); err == nil {
				conflicts = append(conflicts, "evidence:"+stringValue(index["id"])+":sha256_mismatch")
			}
			continue
		}
		var envelope evidenceEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			deferredSchema = append(deferredSchema, "evidence:"+stringValue(index["id"])+":schema")
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
			deferredSchema = append(deferredSchema, "evidence:"+stringValue(index["id"])+":schema")
			continue
		}
		if requirement.CurrentReviewRound &&
			(envelope.ReviewRound != currentRound || envelope.ReviewRound != intValue(index["review_round"])) {
			continue
		}
		if envelope.InvalidatedBy != "" {
			continue
		}
		// A registered current-generation record whose conclusion or
		// requested_event misses the requirement may be a naming error —
		// but the same kind legitimately serves several requirements with
		// different conclusion vocabularies (bug serves finding_record AND
		// root_cause_record), so the conflict is deferred: it is reported
		// only when nothing ends up qualifying (without the
		// false alarms).
		if !containsString(requirement.Conclusions, envelope.Conclusion) {
			if !requirement.RoutingVerdict {
				mismatched = append(mismatched, "evidence:"+stringValue(index["id"])+":conclusion_mismatch:"+envelope.Conclusion)
			}
			continue
		}
		if requirement.RequestedEvent != "" && envelope.RequestedEvent != requirement.RequestedEvent {
			if !requirement.RoutingVerdict {
				mismatched = append(mismatched, "evidence:"+stringValue(index["id"])+":requested_event_mismatch:"+envelope.RequestedEvent)
			}
			continue
		}
		if !subjectsMatch(envelope.SubjectRefs, documents) {
			deferredSchema = append(deferredSchema, "evidence:"+stringValue(index["id"])+":subject_not_registered_or_drifted")
			continue
		}
		if !containsString(requirement.Responsibilities, envelope.ProducerResponsibility) {
			continue
		}
		valid = append(valid, envelope.EvidenceID)
	}
	if len(valid) == 0 && len(mismatched) > 0 {
		// Nothing qualified and naming errors exist — they are the reason.
		conflicts = append(conflicts, mismatched...)
	}
	if len(valid) < requirement.MinCount && len(deferredSchema) > 0 {
		// Nothing (or not enough) qualified and superseded registrations
		// exist — their schema drift is then the blocking reason.
		conflicts = append(conflicts, deferredSchema...)
	}
	return sortedUnique(valid), sortedUnique(conflicts)
}

func applyDocumentPassIndependence(input Input, result *Evaluation, documents []documentFact) {
	// Reviewer-vs-author is data-driven: it only fires when documents carry
	// a real author_agent_id. On the organic path registrations record
	// hook_controller (the commit actor, not the drafting agent), so this
	// layer is dormant there — independence rests on separation_edges
	// (dispatch) + distinct producers (below) + the reviewer discipline in
	// the document-verifier card (L3-S5 §2, honestly recorded).
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
		if missing := missingSubjects(envelope.SubjectRefs, documents); len(missing) > 0 {
			result.Missing = append(result.Missing, "evidence:exact_document_manifest")
			result.Conflicts = append(result.Conflicts, "exact_subjects_missing:"+strings.Join(missing, ","))
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

// applyBuilderBatchCompleteness evaluates GATE-BUILDER-BATCH-READY over the
// TR-003 exact execution batch — the current-generation task documents
// registered by register_execution_batch — instead of scanning runtime task
// states (L3-S6 §8.2). A task registered straight into `reviewed` (or left
// in `candidate`) is inside the registered batch and therefore cannot slip
// the completeness check. Per TASK the gate proves:
//
//  1. a qualified completion_report envelope bound to that task exists;
//  2. every check recorded in the envelope passed (non-pass results block);
//  3. the envelope declares no scope deviations;
//  4. a durable worktree integration checkpoint with task_id bound to that
//     task reached `verified` or beyond.
//
// An empty registered batch is itself not_ready: TR-003 refuses to lock an
// empty batch, so reaching building without one means the batch registry
// was lost, not that zero work suffices.
func applyBuilderBatchCompleteness(input Input, result *Evaluation) {
	batch := executionBatchTasks(input.Snapshot.State)
	if len(batch) == 0 {
		result.Missing = append(result.Missing, "batch:execution_batch_empty")
		result.Status = StatusNotReady
		return
	}
	if HasDispatchPlan(input.Snapshot.State) {
		progress := PlannedBuilderProgress(input)
		for _, taskID := range batch {
			if progress[taskID].State != "integrated" {
				result.Missing = append(result.Missing, "integration_checkpoint:"+taskID+":"+progress[taskID].Reason)
			}
		}
		if len(result.Missing) > 0 {
			result.Status = StatusNotReady
		}
		return
	}
	completions := make(map[string]evidenceEnvelope)
	for _, envelope := range evidenceEnvelopesByID(input, result.EvidenceRefs) {
		if evidenceKindsEqual("completion_report", envelope.Kind) && envelope.TaskID != "" {
			completions[envelope.TaskID] = envelope
		}
	}
	integrated := verifiedIntegrationTaskIDs(input)
	for _, taskID := range batch {
		envelope, ok := completions[taskID]
		if !ok {
			result.Missing = append(result.Missing, "evidence:completion_report:"+taskID)
		}
		if ok {
			if failing := failingEnvelopeChecks(envelope); len(failing) > 0 {
				result.Missing = append(result.Missing, "checks:"+taskID+":"+strings.Join(failing, ","))
			}
			if len(envelope.ScopeDeviations) > 0 {
				result.Missing = append(result.Missing, "scope_deviations:"+taskID+":"+strings.Join(envelope.ScopeDeviations, ","))
			}
		}
		if !integrated[taskID] {
			result.Missing = append(result.Missing, "integration_checkpoint:"+taskID)
		}
	}
	result.Missing = sortedUnique(result.Missing)
	if len(result.Missing) > 0 {
		result.Status = StatusNotReady
	}
}

// executionBatchTasks returns the TR-003 exact execution batch: the task
// documents registered at the current baseline generation. Order is the
// registration order so the missing matrix is reproducible.
func executionBatchTasks(state map[string]any) []string {
	generation := nestedInt(state, "baseline", "generation")
	raw, _ := state["documents"].([]any)
	var batch []string
	for _, item := range raw {
		document, _ := item.(map[string]any)
		if document == nil || stringValue(document["kind"]) != "task" {
			continue
		}
		if intValue(document["generation"]) != generation {
			continue
		}
		if id := stringValue(document["id"]); id != "" {
			batch = append(batch, id)
		}
	}
	return batch
}

// verifiedIntegrationTaskIDs loads every durable worktree checkpoint for
// the current runtime + generation and returns the task IDs whose state
// reached `verified` or beyond. The checkpoint files are the Integrator's
// authoritative record; a FileView without directory listing makes them
// unobservable, which is surfaced as missing per task by the caller (fail
// closed, not silently skipped). Report-bound checkpoints also pass through
// the merge-receipt guard so this legacy projection cannot release a
// successor from an unreachable target.
func verifiedIntegrationTaskIDs(input Input) map[string]bool {
	integrated := make(map[string]bool)
	lister, ok := input.Files.(fileDirLister)
	if !ok || input.Files == nil {
		return integrated
	}
	runtimeID, _ := input.Snapshot.State["runtime_id"].(string)
	generation := nestedInt(input.Snapshot.State, "baseline", "generation")
	dir := path.Join(".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation), "worktree")
	entries, err := lister.ReadDir(dir)
	if err != nil {
		return integrated
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := input.Files.ReadFile(path.Join(dir, entry.Name(), "checkpoint.json"))
		if err != nil {
			continue
		}
		var checkpoint dispatchCheckpoint
		if json.Unmarshal(data, &checkpoint) != nil || checkpoint.TaskID == "" {
			continue
		}
		switch checkpoint.State {
		case "verified", "acknowledged", "cleanup_pending", "complete":
			if checkpointMergeValid(input, checkpoint) {
				integrated[checkpoint.TaskID] = true
			}
		}
	}
	return integrated
}

// failingEnvelopeChecks names the envelope checks whose result is not pass
// (fail / blocked / not_run all leave the closing contract unproven).
func failingEnvelopeChecks(envelope evidenceEnvelope) []string {
	var failing []string
	for _, check := range envelope.Checks {
		if check.Result != "pass" {
			label := check.Name
			if label == "" {
				label = check.Command
			}
			failing = append(failing, label+"="+check.Result)
		}
	}
	return failing
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
			ID:            stringValue(value["id"]),
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
		// Shared schema/sample subjects must participate in S5, but cannot
		// substitute for the S2 architecture deliverable.
		if kind == "design" && strings.HasPrefix(document.ID, "shared-model:") {
			continue
		}
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

// diskDeclaredArtifacts scans the artifact directory for the given kind and
// returns the on-disk documents whose top-of-file status field matches.
// The bool reports whether listing was possible at all (a FileView without
// directory listing — legacy test doubles — keeps the documents[]-only
// behavior).
func diskDeclaredArtifacts(input Input, kind, status string) ([]documentFact, bool) {
	lister, ok := input.Files.(fileDirLister)
	if !ok || input.Files == nil {
		return nil, false
	}
	dirRel, filePrefix := diskArtifactHome(kind)
	entries, err := lister.ReadDir(dirRel)
	if err != nil {
		return nil, false
	}
	var facts []documentFact
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") ||
			strings.Contains(strings.ToLower(name), "template") ||
			strings.EqualFold(name, "README.md") ||
			(filePrefix != "" && !strings.HasPrefix(strings.TrimSuffix(name, ".md"), filePrefix)) {
			continue
		}
		rel := path.Join(dirRel, name)
		data, err := input.Files.ReadFile(rel)
		if err != nil {
			continue
		}
		declared := parseMarkdownStatusField(string(data))
		if declared != status {
			continue
		}
		facts = append(facts, documentFact{
			Kind: kind, Path: rel,
			Version: parseMarkdownVersionField(string(data)),
			SHA256:  sha256Hex(data),
			Status:  declared,
		})
	}
	return facts, true
}

// parseMarkdownStatusField reads the top blockquote `状态`/`Status` field,
// mirroring the transition package's ParseMarkdownField semantics without
// importing it.
func parseMarkdownStatusField(content string) string {
	return parseTopField(content, "状态", "Status")
}

func parseMarkdownVersionField(content string) string {
	return parseTopField(content, "版本", "Version")
}

func parseTopField(content string, keys ...string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, key := range keys {
			for _, sep := range []string{"：", ":"} {
				prefix := key + sep
				if strings.HasPrefix(line, prefix) {
					value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
					// Trailing parenthetical annotations (e.g. the REQ
					// template's guidance note) are not part of the value.
					if i := strings.IndexAny(value, "（( "); i > 0 {
						value = strings.TrimSpace(value[:i])
					}
					return value
				}
			}
		}
	}
	return ""
}

// diskArtifactHome maps a document kind to the directory and file prefix
// whose on-disk Status declaration is the agent-producible fact for that
// kind's gate precondition.
func diskArtifactHome(kind string) (dir string, prefix string) {
	switch kind {
	case "task":
		return projectlayout.Tasks, "TASK-"
	case "design":
		return projectlayout.Architecture, "ARCHITECTURE-"
	case "req":
		return projectlayout.Requirements, "REQ-"
	default:
		return projectlayout.Contracts, ""
	}
}

// registeredDocumentDrift names every current-generation registered
// document whose on-disk bytes no longer match the registered sha (or
// whose file is unreadable) — one `document_drift:<path>` conflict each.
func registeredDocumentDrift(input Input) []string {
	if input.Files == nil {
		return nil
	}
	documents := currentDocuments(input.Snapshot.State, nestedInt(input.Snapshot.State, "baseline", "generation"))
	var conflicts []string
	for _, document := range documents {
		if document.Path == "" || document.SHA256 == "" {
			// An empty path/sha escapes both this screen and exactSubjects —
			// name it instead of silently shrinking the manifest.
			conflicts = append(conflicts, "document_drift:"+document.Path+"(missing path/sha)")
			continue
		}
		data, err := input.Files.ReadFile(document.Path)
		if err != nil || sha256Hex(data) != document.SHA256 {
			conflicts = append(conflicts, "document_drift:"+document.Path)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

// missingSubjects lists the manifest entries the envelope did not cover.
func missingSubjects(subjects []subjectRef, documents []documentFact) []string {
	have := make(map[string]bool, len(subjects))
	for _, subject := range subjects {
		have[subject.Path] = true
	}
	var missing []string
	for _, document := range documents {
		if !have[document.Path] {
			missing = append(missing, document.Path)
		}
	}
	sort.Strings(missing)
	return missing
}
