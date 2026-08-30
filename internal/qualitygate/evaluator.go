package qualitygate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	if result.GateID == "GATE-VERIFY-CLEAN-ROUND-PASSED" {
		// L3-S7 §10: recompute the machine CleanRound over the ReviewPlan's
		// exact Claim set; an evidence-only pass is not sufficient.
		applyCleanRoundGate(input, &result)
	}
	if result.GateID == "GATE-VERIFY-BLOCKING-FINDING" {
		// L3-S7 §3.7: the sealed ObservationBatch must carry the exact
		// current-round Finding set with the drain policy respected.
		applyObservationBatchGate(input, &result)
	}
	if result.GateID == "GATE-ACCEPTANCE-COMPLETE" || result.GateID == "GATE-ACCEPTANCE-REVIEW-REQUIRED" || result.GateID == "GATE-RELEASE-AUDIT-APPROVED" || result.GateID == "GATE-RELEASE-AUDIT-BLOCKED" {
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
	result.Fingerprint = fingerprint(result.GateID, spec.SemanticVersion, state, generation, documents, result.EvidenceRefs)
	return result
}

func applyS10ManifestGate(input Input, result *Evaluation) {
	wanted := map[string]string{"acceptance": "acceptance_record"}
	if result.GateID == "GATE-RELEASE-AUDIT-APPROVED" || result.GateID == "GATE-RELEASE-AUDIT-BLOCKED" {
		wanted["release_audit"] = "release_audit_record"
	}
	for manifestType, evidenceKind := range wanted {
		evidenceID := ""
		for _, id := range result.EvidenceRefs {
			envelope, ok := s10EnvelopeByID(input, id)
			if ok && evidenceKindsEqual(evidenceKind, envelope.Kind) {
				evidenceID = id
				break
			}
		}
		if evidenceID == "" {
			// The ordinary evidence requirements already explain a missing
			// acceptance/audit envelope. Do not add a second, confusing
			// manifest error when its parent evidence is absent.
			continue
		}
		envelope, _ := s10EnvelopeByID(input, evidenceID)
		if strings.TrimSpace(envelope.AuditManifestPath) == "" || strings.TrimSpace(envelope.AuditManifestSHA256) == "" {
			result.Missing = append(result.Missing, "s10:"+manifestType+"_manifest:"+evidenceID)
			result.Status = StatusNotReady
			continue
		}
		if input.Files == nil {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, "s10:"+manifestType+"_manifest:"+evidenceID+":unreadable; next: restore the manifest file and register a new fingerprinted envelope")
			continue
		}
		manifestData, err := input.Files.ReadFile(envelope.AuditManifestPath)
		if err != nil {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:unreadable:%s; next: restore the manifest file and register a new fingerprinted envelope", manifestType, evidenceID, err))
			continue
		}
		if sha256Hex(manifestData) != envelope.AuditManifestSHA256 {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:sha256_mismatch; next: do not edit in place, regenerate the manifest and register a new fingerprinted envelope", manifestType, evidenceID))
			continue
		}
		// RC-16: outcome-aware validation. Passing/approved outcomes require a
		// clean ledger; a routed review_required/blocked outcome keeps the
		// unresolved rows that explain the route, but must still be a
		// structurally complete, evidence-linked record. The conclusion/type
		// pairing is fail-closed: allowsUnresolvedOutcome only relaxes the
		// matching route (acceptance+review_required, release_audit+blocked).
		baseline, baselineErr := s10ExternalBaseline(input)
		if baselineErr != nil {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:external_baseline_unverifiable:%s; next: restore the current-generation completion artifacts so the changed-surface denominator can be re-derived, then re-run the gate", manifestType, evidenceID, baselineErr))
			continue
		}
		var summary acceptance.Summary
		if input.Root != "" && acceptance.S10AuthorityAvailable(input.Snapshot.State) {
			authority, authorityErr := acceptance.BuildS10InventoryAuthority(input.Root, input.Snapshot.State, baseline)
			if authorityErr != nil {
				result.Status = StatusUnknown
				result.ErrorCode = ErrorGateUnknown
				result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:authoritative_inventory_unverifiable:%s; next: restore the current bound REQ, contract/TASK registrations, and pinned S7 ReviewPlan before re-running the gate", manifestType, evidenceID, authorityErr))
				continue
			}
			summary, err = acceptance.ValidateForOutcomeWithBaselineAndAuthority(manifestData, manifestType, strings.TrimSpace(envelope.Conclusion), baseline, authority)
		} else {
			summary, err = acceptance.ValidateForOutcomeWithBaseline(manifestData, manifestType, strings.TrimSpace(envelope.Conclusion), baseline)
		}
		if err != nil {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:invalid:%s", manifestType, evidenceID, err))
			continue
		}
		if missing := missingS10EvidenceRefs(input, evidenceID, summary.EvidenceRefs); len(missing) > 0 {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:evidence_ref_missing:%s; next: register the referenced current evidence first, then regenerate and re-register the manifest envelope (ids match runtime evidence verbatim — copy them from `.claude/loop-state.json` evidence[].id)", manifestType, evidenceID, strings.Join(missing, ",")))
			continue
		}
		if summary.ManifestType != manifestType || !s10ManifestBindingMatches(input, envelope, manifestData) {
			result.Status = StatusUnknown
			result.ErrorCode = ErrorGateUnknown
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("s10:%s_manifest:%s:binding_mismatch; next: regenerate against the current runtime, baseline, and S7 round", manifestType, evidenceID))
		}
	}
	result.Missing = sortedUnique(result.Missing)
	result.Conflicts = sortedUnique(result.Conflicts)
	if len(result.Conflicts) == 0 && len(result.Missing) > 0 {
		result.Status = StatusNotReady
	}
}

// S10ExternalBaseline is the shared RC-05/RC-16 external-denominator builder
// for the S10 manifest consumers: the Quality Gate (applyS10ManifestGate) and
// `loop-harness s10 status` (inspectS10Artifact) both call it so the two can
// never diverge on what the denominator is (RC-16 status/gate single source).
// It unions three sources the manifest author does not control:
//
//  1. the immutable current-generation completion artifacts, re-derived
//     through review.ChangedPathsForRootDetailed when a repository root is
//     available (the same exact-set surface S7 froze);
//  2. change_impact evidence artifacts of the current generation — the
//     change ledger a repair round authorized;
//  3. the affected paths of the triggering request, with the explicit "all"
//     token marking a full-surface declaration (waives the exact-set check).
//
// RC-16 fail-closed rule: when the completion-artifact projection is
// unverifiable (review diagnostics present — e.g. a registered completion
// artifact missing from disk), the returned error names the diagnostics and
// the caller must surface `s10:external_baseline_unverifiable` instead of
// silently waiving the exact-set check. Only a genuinely empty projection
// (no diagnostics, no paths) returns a Baseline with no ChangedPaths, which
// leaves the self-declared denominator untouched.
func S10ExternalBaseline(root string, state map[string]any, affectedPaths []string) (acceptance.Baseline, error) {
	return acceptance.BuildS10ExternalBaseline(root, state, affectedPaths)
}

// s10ExternalBaseline is the gate-side wrapper over S10ExternalBaseline. The
// gate reads the change_impact ledger through the evaluator's FileView (tests
// use in-memory file views), so rooted evaluation calls the shared builder for
// the completion projection and then unions the FileView-based ledger entries.
// A rootless evaluation keeps the pre-RC-16 behavior: no completion projection
// to verify, so only the ledger and affected paths contribute.
func s10ExternalBaseline(input Input) (acceptance.Baseline, error) {
	if input.Root == "" {
		baseline := acceptance.Baseline{}
		if strings.TrimSpace(strings.Join(input.AffectedPaths, ",")) == "all" || (len(input.AffectedPaths) == 1 && input.AffectedPaths[0] == "all") {
			baseline.AffectedPathsAll = true
			return baseline, nil
		}
		seen := map[string]struct{}{}
		add := func(paths []string) {
			for _, p := range paths {
				p = strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")), "./")
				if p == "" || strings.Contains(p, ":") {
					continue
				}
				if _, ok := seen[p]; ok {
					continue
				}
				seen[p] = struct{}{}
				baseline.ChangedPaths = append(baseline.ChangedPaths, p)
			}
		}
		add(changeImpactChangedPaths(input))
		for _, p := range input.AffectedPaths {
			add([]string{p})
		}
		sort.Strings(baseline.ChangedPaths)
		return baseline, nil
	}
	baseline, err := S10ExternalBaseline(input.Root, input.Snapshot.State, input.AffectedPaths)
	if err != nil {
		return acceptance.Baseline{}, err
	}
	extra := changeImpactChangedPaths(input)
	if len(extra) > 0 {
		seen := map[string]struct{}{}
		for _, p := range baseline.ChangedPaths {
			seen[p] = struct{}{}
		}
		for _, p := range extra {
			p = strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")), "./")
			if p == "" || strings.Contains(p, ":") {
				continue
			}
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				baseline.ChangedPaths = append(baseline.ChangedPaths, p)
			}
		}
		sort.Strings(baseline.ChangedPaths)
	}
	return baseline, nil
}

// changeImpactChangedPaths reads changed_artifacts from every current-
// generation change_impact evidence artifact in the runtime index. A drifted
// or unreadable artifact contributes nothing (its registration gate already
// proves it separately); a readable one is an authoritative ledger entry.
func changeImpactChangedPaths(input Input) []string {
	if input.Files == nil {
		return nil
	}
	return changeImpactChangedPathsRead(input.Snapshot.State, input.Files.ReadFile)
}

func changeImpactChangedPathsRead(state map[string]any, readFile func(string) ([]byte, error)) []string {
	generation := nestedInt(state, "baseline", "generation")
	rawEvidence, _ := state["evidence"].([]any)
	var paths []string
	for _, raw := range rawEvidence {
		entry, _ := raw.(map[string]any)
		if entry == nil || !evidenceKindsEqual("change_impact_record", stringValue(entry["kind"])) ||
			stringValue(entry["status"]) != "valid" || entry["invalidated_by"] != nil ||
			intValue(entry["baseline_generation"]) != generation {
			continue
		}
		data, err := readFile(stringValue(entry["path"]))
		if err != nil || sha256Hex(data) != stringValue(entry["sha256"]) {
			continue
		}
		var impact struct {
			ChangedArtifacts []struct {
				Path string `json:"path"`
			} `json:"changed_artifacts"`
		}
		if json.Unmarshal(data, &impact) != nil {
			continue
		}
		for _, artifact := range impact.ChangedArtifacts {
			if artifact.Path != "" {
				paths = append(paths, artifact.Path)
			}
		}
	}
	return paths
}

func missingS10EvidenceRefs(input Input, selfID string, refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	currentGeneration := nestedInt(input.Snapshot.State, "baseline", "generation")
	currentRound := nestedInt(input.Snapshot.State, "review", "round")
	available := make(map[string]struct{})
	rawEvidence, _ := input.Snapshot.State["evidence"].([]any)
	for _, raw := range rawEvidence {
		entry, _ := raw.(map[string]any)
		if entry == nil || stringValue(entry["status"]) != "valid" || intValue(entry["baseline_generation"]) != currentGeneration {
			continue
		}
		// RC-14: invalidated_by empty string is treated as nil (not invalidated); only non-empty invalidates.
		if v := entry["invalidated_by"]; v != nil {
			if str, ok := v.(string); ok {
				if stringValue(str) != "" {
					continue
				}
			} else {
				continue
			}
		}
		id := stringValue(entry["id"])
		if id == "" || id == selfID {
			continue
		}
		// RC-14: when entry carries a review_round, it must match currentRound; round-less evidence is not round-bound and remains available.
		if currentRound > 0 {
			if round := intValue(entry["review_round"]); round != 0 && round != currentRound {
				continue
			}
		}
		// RC-14: kind must be a registered evidence kind (phantom kinds rejected).
		kind := stringValue(entry["kind"])
		if kind != "" && !evidence.DefaultCatalog().IsRegisteredKind(kind) {
			continue
		}
		path := stringValue(entry["path"])
		if input.Files == nil || path == "" {
			continue
		}
		data, err := input.Files.ReadFile(path)
		if err != nil || sha256Hex(data) != stringValue(entry["sha256"]) {
			continue
		}
		available[id] = struct{}{}
	}
	missing := make([]string, 0)
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			missing = append(missing, ref)
			continue
		}
		// RC-14: execution anchors (://) are not runtime evidence ids; they cannot satisfy S10 manifest refs.
		if strings.Contains(ref, "://") {
			missing = append(missing, ref)
			continue
		}
		if _, ok := available[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	return sortedUnique(missing)
}

func s10EnvelopeByID(input Input, id string) (evidenceEnvelope, bool) {
	for _, envelope := range evidenceEnvelopesByID(input, []string{id}) {
		return envelope, true
	}
	return evidenceEnvelope{}, false
}

func s10ManifestBindingMatches(input Input, envelope evidenceEnvelope, data []byte) bool {
	var manifest struct {
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
		ReviewRound        int    `json:"review_round"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return false
	}
	return manifest.RuntimeID == envelope.RuntimeID &&
		manifest.BaselineGeneration == envelope.BaselineGeneration &&
		manifest.ReviewRound == envelope.ReviewRound &&
		manifest.RuntimeID == stringValue(input.Snapshot.State["runtime_id"]) &&
		manifest.BaselineGeneration == nestedInt(input.Snapshot.State, "baseline", "generation") &&
		manifest.ReviewRound == nestedInt(input.Snapshot.State, "review", "round")
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
	var conflicts []string
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
		relevant := false
		for requirementKind, resp := range allowed {
			if evidenceKindsEqual(requirementKind, kind) {
				responsibilities = resp
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
	var mismatched []string
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
// closed, not silently skipped).
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
		var checkpoint struct {
			TaskID string `json:"task_id"`
			State  string `json:"state"`
		}
		if json.Unmarshal(data, &checkpoint) != nil || checkpoint.TaskID == "" {
			continue
		}
		switch checkpoint.State {
		case "verified", "acknowledged", "cleanup_pending", "complete":
			integrated[checkpoint.TaskID] = true
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
		return "docs/tasks", "TASK-"
	case "design":
		return "docs/design/architecture", "ARCHITECTURE-"
	case "req":
		return "docs/requirements", "REQ-"
	default:
		return "docs/contracts", ""
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
