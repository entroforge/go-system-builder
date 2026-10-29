package investigation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const caseWorkflowNext = "runtime investigation status --case-id <case>"

// CaseRevisionRequest is the common optimistic-concurrency envelope for an
// InvestigationCase mutation. Mutate receives a private copy of the active
// Case; the source Finding set and identity fields are checked again before
// the immutable next revision is written.
type CaseRevisionRequest struct {
	Context              context.Context `json:"-"`
	ExpectedRevision     int
	ExpectedCaseRevision int
	ExpectedCaseSHA256   string
	CaseID               string
	Operation            string
	OccurredAt           time.Time
	Mutate               func(map[string]any) error
	// validateCurrent is an internal capability used by issued S8 answer
	// consumers. It is run against the original, pinned Case bytes before
	// staging and again inside the original Runtime CAS. It is intentionally
	// unexported: callers cannot supply a self-authored authority callback.
	validateCurrent func(root string, state, document map[string]any, caseBytes []byte) error
}

// HypothesisRequest registers one falsifiable S8 hypothesis inside a Case.
// SourceFindingIDs may be a subset of the Case exact set, but can never add
// or replace a source Finding. EvidenceRefs must carry at least one content
// reference: an evidence-free hypothesis cannot be dispatched or falsified.
type HypothesisRequest struct {
	Context              context.Context `json:"-"`
	ExpectedRevision     int
	ExpectedCaseRevision int
	ExpectedCaseSHA256   string
	CaseID               string
	HypothesisID         string
	AssignmentID         string
	Statement            string
	Invariant            string
	Discriminator        string
	ExpectedOutcomes     map[string]any
	SourceFindingIDs     []string
	EvidenceRefs         []string
	OccurredAt           time.Time
}

// HypothesisResultRequest records read-only evidence for a registered
// hypothesis. It is not a routing or root-cause authority by itself.
type HypothesisResultRequest struct {
	Context              context.Context `json:"-"`
	ExpectedRevision     int
	ExpectedCaseRevision int
	ExpectedCaseSHA256   string
	CaseID               string
	HypothesisID         string
	AssignmentID         string
	Method               string
	EvidenceRefs         []string
	SourceBoundaryRefs   []string
	Observed             string
	Counterfactual       string
	Result               string
	ExplainsFindingIDs   []string
	DoesNotExplain       []string
	NewHypotheses        []map[string]any
	OccurredAt           time.Time
	// typedPacket/typedReceipt are an unexported capability carried only by
	// SubmitIssuedHypothesisResult. Public legacy callers cannot manufacture a
	// packet-backed acceptance row or bypass an issued packet.
	typedPacket     *workpacket.HypothesisResultPacket
	typedReceipt    *runtime.HypothesisResultReceipt
	typedValidation func(root string, state, document map[string]any, caseBytes []byte) error
}

// EvidenceReference is a content-addressed artifact carried into S8 when a
// downstream verifier proves that the approved causal model needs
// reassessment. It deliberately contains only identity and integrity data;
// the referenced artifact remains authoritative in its own domain.
type EvidenceReference struct {
	Path   string
	SHA256 string
}

// RouteRequest records the one Case-level disposition. s9_repair requires a
// closed causal chain; investigate_more is the deterministic fallback while
// any source Finding remains unexplained or a current question is unanswered.
type RouteRequest struct {
	Context                        context.Context `json:"-"`
	ExpectedRevision               int
	ExpectedCaseRevision           int
	ExpectedCaseSHA256             string
	CaseID                         string
	Route                          string
	RouteReason                    string
	PrimaryRootCause               string
	CausalModel                    map[string]any
	BlastRadius                    map[string]any
	DetectionGap                   map[string]any
	CanonicalCaseID                string
	CausalReassessmentEvidenceRefs []EvidenceReference
	// NoCompetingHypothesis, when non-empty, records the S8-4 declaration that
	// no competing hypothesis was credible enough to warrant a discriminator;
	// it substitutes for the refuted-hypothesis result in causal closure.
	NoCompetingHypothesis string
	// CompetingHypothesisAssessment explains how accepted discriminators handled
	// credible alternatives; it must reference current conclusive results.
	CompetingHypothesisAssessment map[string]any
	OccurredAt                    time.Time
}

// UpdateCase commits an arbitrary, package-local Case revision mutation. The
// specialized APIs below should be preferred because they validate their
// domain shape before reaching this lower-level entry point.
func UpdateCase(root, statePath, journalPath string, request CaseRevisionRequest) (runtime.Snapshot, error) {
	if request.Mutate == nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "case mutation is required")
	}
	return updateCaseRevision(root, statePath, journalPath, request)
}

// RegisterHypothesis appends one falsifiable hypothesis as a new immutable
// Case revision and advances the Runtime through CAS.
func RegisterHypothesis(root, statePath, journalPath string, request HypothesisRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(request.HypothesisID) == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "hypothesis_id is required")
	}
	if err := validateAssignmentID(request.AssignmentID, "Hypothesis.assignment_id"); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	if strings.TrimSpace(request.Statement) == "" || strings.TrimSpace(request.Invariant) == "" || strings.TrimSpace(request.Discriminator) == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "hypothesis requires statement, invariant, and discriminator")
	}
	if len(request.ExpectedOutcomes) < 2 || strings.TrimSpace(stringField(request.ExpectedOutcomes["support"])) == "" || strings.TrimSpace(stringField(request.ExpectedOutcomes["refute"])) == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "hypothesis expected_outcomes must name non-empty support and refute outcomes")
	}
	sourceIDs := []string{}
	if len(request.SourceFindingIDs) > 0 {
		var err error
		sourceIDs, err = normalizeSet(request.SourceFindingIDs, "Hypothesis.source_finding_ids")
		if err != nil {
			return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
		}
	}
	evidenceRefs, err := nonEmptyStrings(request.EvidenceRefs, "Hypothesis.evidence_refs")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	snapshot, serr := runtime.NewStore(statePath, journalPath).WithContext(request.Context).Snapshot()
	if serr != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "read Runtime for Hypothesis.evidence_refs: %v", serr)
	}
	return updateCaseRevision(root, statePath, journalPath, CaseRevisionRequest{
		Context:              request.Context,
		ExpectedRevision:     request.ExpectedRevision,
		ExpectedCaseRevision: request.ExpectedCaseRevision,
		ExpectedCaseSHA256:   request.ExpectedCaseSHA256,
		CaseID:               request.CaseID,
		Operation:            "hypothesis_registered",
		OccurredAt:           request.OccurredAt,
		validateCurrent: func(lockedRoot string, state, document map[string]any, _ []byte) error {
			if stringField(document["observation_batch_id"]) == "" {
				return nil
			}
			remaining, err := remainingFindingQuestionRefs(lockedRoot, state, document, sourceIDs, evidenceRefs)
			if err != nil {
				return err
			}
			if len(remaining) == 0 {
				return nil
			}
			return ValidateEvidenceRefs(remaining, EvidenceAttestationOptions{AllowPaths: true, State: state, Root: lockedRoot, RequireSHA: true, RequireReviewRound: currentReviewRound(state)})
		},
		Mutate: func(document map[string]any) error {
			if len(sourceIDs) == 0 && stringField(document["observation_batch_id"]) != "" {
				return errors.New("Hypothesis.source_finding_ids must contain at least one Finding ID for a Batch Case")
			}
			refsToValidate := evidenceRefs
			if stringField(document["observation_batch_id"]) == "" {
				source, err := humanCaseSource(root, snapshot.State, document)
				if err != nil {
					return err
				}
				refsToValidate = withoutHumanSource(evidenceRefs, source)
			} else {
				var err error
				refsToValidate, err = remainingFindingQuestionRefs(root, snapshot.State, document, sourceIDs, evidenceRefs)
				if err != nil {
					return err
				}
			}
			if len(refsToValidate) > 0 {
				if err := ValidateEvidenceRefs(refsToValidate, EvidenceAttestationOptions{
					AllowPaths: true, State: snapshot.State, Root: root, RequireSHA: true, RequireReviewRound: currentReviewRound(snapshot.State),
				}); err != nil {
					return fmt.Errorf("Hypothesis.evidence_refs: %w", err)
				}
			}
			caseIDs, err := stringSlice(document["source_finding_ids"], "InvestigationCase.source_finding_ids")
			if err != nil {
				return err
			}
			if err := subsetOf(sourceIDs, caseIDs, "Hypothesis.source_finding_ids"); err != nil {
				return err
			}
			hypotheses, err := objectArray(document["hypotheses"], "InvestigationCase.hypotheses")
			if err != nil {
				return err
			}
			for _, raw := range hypotheses {
				if stringField(raw["hypothesis_id"]) == request.HypothesisID {
					return fmt.Errorf("hypothesis %q is already registered; keep its original question and result, and add a distinct falsifiable question for further work", request.HypothesisID)
				}
			}
			hypotheses = append(hypotheses, map[string]any{
				"hypothesis_id":      request.HypothesisID,
				"assignment_id":      strings.TrimSpace(request.AssignmentID),
				"statement":          strings.TrimSpace(request.Statement),
				"invariant":          strings.TrimSpace(request.Invariant),
				"discriminator":      strings.TrimSpace(request.Discriminator),
				"expected_outcomes":  cloneMap(request.ExpectedOutcomes),
				"source_finding_ids": stringSliceAny(sourceIDs),
				"evidence_refs":      stringSliceAny(evidenceRefs),
				"status":             "open",
			})
			document["hypotheses"] = hypothesesToAny(hypotheses)
			return nil
		},
	})
}

// SubmitHypothesisResult appends one evidence-bound result for a registered
// hypothesis. Result Finding references are validated as subsets of the
// immutable Case source set.
func SubmitHypothesisResult(root, statePath, journalPath string, request HypothesisResultRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(request.HypothesisID) == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "hypothesis_id is required")
	}
	if err := validateAssignmentID(request.AssignmentID, "HypothesisResult.assignment_id"); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	if !oneOf(request.Result, "supported", "refuted", "inconclusive") {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "result must be supported, refuted, or inconclusive")
	}
	for field, value := range map[string]string{"method": request.Method, "observed": request.Observed, "counterfactual_or_discriminator": request.Counterfactual} {
		if strings.TrimSpace(value) == "" {
			return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "HypothesisResult.%s is required", field)
		}
	}
	evidenceRefs, err := nonEmptyStrings(request.EvidenceRefs, "HypothesisResult.evidence_refs")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	snapshot, serr := runtime.NewStore(statePath, journalPath).WithContext(request.Context).Snapshot()
	if serr != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "read Runtime for HypothesisResult.evidence_refs attestation: %v", serr)
	}
	if err := validateHypothesisSubmissionBinding(snapshot.State, request); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	refsToValidate, err := remainingS8AnswerEvidenceRefs(root, request.typedPacket, evidenceRefs)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if stringField(s8Map(s8Map(snapshot.State["review"])["investigation"])["observation_batch_id"]) == "" {
		current, err := readCurrentInvestigationCase(root, snapshot.State)
		if err != nil {
			return runtime.Snapshot{}, err
		}
		if stringField(current.document["observation_batch_id"]) == "" {
			source, err := humanCaseSource(root, snapshot.State, current.document)
			if err != nil {
				return runtime.Snapshot{}, err
			}
			refsToValidate = withoutHumanSource(refsToValidate, source)
		}
	}

	if len(refsToValidate) > 0 {
		if err := ValidateEvidenceRefs(refsToValidate, EvidenceAttestationOptions{
			AllowPaths:         true,
			State:              snapshot.State,
			Root:               root,
			RequireSHA:         true,
			RequireReviewRound: currentReviewRound(snapshot.State),
		}); err != nil {
			return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "HypothesisResult.evidence_refs: %v", err)
		}
	}
	boundaryRefs, err := nonEmptyStrings(request.SourceBoundaryRefs, "HypothesisResult.source_boundary_refs")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	explains, err := normalizeOptionalSet(request.ExplainsFindingIDs, "HypothesisResult.explains_finding_ids")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	doesNotExplain, err := normalizeOptionalSet(request.DoesNotExplain, "HypothesisResult.does_not_explain")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	return updateCaseRevision(root, statePath, journalPath, CaseRevisionRequest{
		Context:              request.Context,
		ExpectedRevision:     request.ExpectedRevision,
		ExpectedCaseRevision: request.ExpectedCaseRevision,
		ExpectedCaseSHA256:   request.ExpectedCaseSHA256,
		CaseID:               request.CaseID,
		Operation:            "hypothesis_result_submitted",
		OccurredAt:           request.OccurredAt,
		validateCurrent:      request.typedValidation,
		Mutate: func(document map[string]any) error {
			caseIDs, err := stringSlice(document["source_finding_ids"], "InvestigationCase.source_finding_ids")
			if err != nil {
				return err
			}
			hypotheses, err := objectArray(document["hypotheses"], "InvestigationCase.hypotheses")
			if err != nil {
				return err
			}
			registered := false
			registeredAssignmentID := ""
			for _, hypothesis := range hypotheses {
				if stringField(hypothesis["hypothesis_id"]) == request.HypothesisID {
					registered = true
					registeredAssignmentID = stringField(hypothesis["assignment_id"])
					break
				}
			}
			if !registered {
				return fmt.Errorf("hypothesis %q is not registered in this Case; register the hypothesis before submitting a result; source Finding subset validation is deferred until the hypothesis binding exists", request.HypothesisID)
			}
			if registeredAssignmentID != request.AssignmentID {
				return fmt.Errorf("HypothesisResult assignment_id %q does not match registered Hypothesis assignment_id %q; submit the result from the dispatched Assignment", request.AssignmentID, registeredAssignmentID)
			}
			if err := subsetOf(explains, caseIDs, "explains_finding_ids"); err != nil {
				return err
			}
			if err := subsetOf(doesNotExplain, caseIDs, "does_not_explain"); err != nil {
				return err
			}
			results, err := objectArray(document["hypothesis_results"], "InvestigationCase.hypothesis_results")
			if err != nil {
				return err
			}
			for _, result := range results {
				if stringField(result["hypothesis_id"]) == request.HypothesisID && stringField(result["assignment_id"]) == request.AssignmentID {
					return fmt.Errorf("HypothesisResult for %s/%s already exists; preserve that accepted result and receipt; Main adds a distinct question for further investigation", request.HypothesisID, request.AssignmentID)
				}
			}
			resultRow := map[string]any{
				"hypothesis_id":                   request.HypothesisID,
				"assignment_id":                   strings.TrimSpace(request.AssignmentID),
				"method":                          strings.TrimSpace(request.Method),
				"evidence_refs":                   stringSliceAny(evidenceRefs),
				"source_boundary_refs":            stringSliceAny(boundaryRefs),
				"observed":                        strings.TrimSpace(request.Observed),
				"counterfactual_or_discriminator": strings.TrimSpace(request.Counterfactual),
				"result":                          request.Result,
				"explains_finding_ids":            stringSliceAny(explains),
				"does_not_explain":                stringSliceAny(doesNotExplain),
			}
			if request.typedPacket != nil {
				if request.typedReceipt == nil || request.typedReceipt.PacketID != request.typedPacket.ID ||
					request.typedReceipt.OwnerAgentID != request.typedPacket.Binding.OwnerAgentID ||
					request.typedReceipt.AssignmentID != request.AssignmentID {
					return errors.New("S8 typed result receipt is incomplete or does not match its packet")
				}
				receiptBytes, err := json.Marshal(request.typedReceipt)
				if err != nil {
					return err
				}
				var receipt map[string]any
				if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
					return err
				}
				resultRow["receipt"] = receipt
			}
			results = append(results, resultRow)
			// S8-H1 supplement: keep the falsifiable status machine honest — the
			// hypothesis is no longer open once its result is recorded. The
			// schema accepts "open | supported | refuted | inconclusive"; a
			// supported result is the verified terminal, a refuted or
			// inconclusive result is the closed terminal.
			hypothesisStatus := request.Result
			for _, hypothesis := range hypotheses {
				if stringField(hypothesis["hypothesis_id"]) == request.HypothesisID {
					hypothesis["status"] = hypothesisStatus
					break
				}
			}
			document["hypotheses"] = hypothesesToAny(hypotheses)
			document["hypothesis_results"] = hypothesesToAny(results)
			if len(request.NewHypotheses) > 0 {
				registeredIDs := make(map[string]struct{}, len(hypotheses))
				registeredAssignments := make(map[string]struct{}, len(hypotheses))
				for _, hypothesis := range hypotheses {
					registeredIDs[stringField(hypothesis["hypothesis_id"])] = struct{}{}
					registeredAssignments[stringField(hypothesis["assignment_id"])] = struct{}{}
				}
				for _, hypothesis := range request.NewHypotheses {
					if err := validateNewHypothesis(hypothesis, caseIDs); err != nil {
						return err
					}
					hypothesisID := strings.TrimSpace(stringField(hypothesis["hypothesis_id"]))
					assignmentID := strings.TrimSpace(stringField(hypothesis["assignment_id"]))
					if _, exists := registeredIDs[hypothesisID]; exists {
						return fmt.Errorf("new hypothesis %q is already registered; use a new hypothesis_id", hypothesisID)
					}
					if _, exists := registeredAssignments[assignmentID]; exists {
						return fmt.Errorf("new hypothesis assignment_id %q is already bound; allocate a unique Assignment", assignmentID)
					}
					normalized := cloneMap(hypothesis)
					normalized["hypothesis_id"] = hypothesisID
					normalized["assignment_id"] = assignmentID
					normalized["status"] = nonEmpty(stringField(normalized["status"]), "open")
					hypotheses = append(hypotheses, normalized)
					registeredIDs[hypothesisID] = struct{}{}
					registeredAssignments[assignmentID] = struct{}{}
				}
				document["hypotheses"] = hypothesesToAny(hypotheses)
			}
			if request.typedPacket != nil {
				// The original Case schema keeps the typed receipt and its exact
				// server-assigned follow-up hypotheses as one indivisible row.
				// [] is intentional when the author proposed none.
				for index := range results {
					if stringField(results[index]["hypothesis_id"]) == request.HypothesisID && stringField(results[index]["assignment_id"]) == request.AssignmentID {
						newRows := make([]map[string]any, 0, len(request.NewHypotheses))
						if len(request.NewHypotheses) > 0 {
							for _, hypothesis := range request.NewHypotheses {
								newRows = append(newRows, cloneMap(hypothesis))
							}
						}
						results[index]["new_hypotheses"] = hypothesesToAny(newRows)
						break
					}
				}
				document["hypothesis_results"] = hypothesesToAny(results)
			}
			return nil
		},
	})
}

// UpdateCaseRoute records one deterministic Case route. A concrete disposition
// is terminal for this Case revision; investigate_more is the one intentional
// re-entry point. New evidence may therefore move a Case from
// investigate_more to a concrete route, while an unchanged board is rejected
// with a recovery instruction instead of silently overwriting the decision.
func UpdateCaseRoute(root, statePath, journalPath string, request RouteRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(request.RouteReason) == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "route_reason is required")
	}
	if err := validateEvidenceReferences(root, request.CausalReassessmentEvidenceRefs); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	return updateCaseRevision(root, statePath, journalPath, CaseRevisionRequest{
		Context:              request.Context,
		ExpectedRevision:     request.ExpectedRevision,
		ExpectedCaseRevision: request.ExpectedCaseRevision,
		ExpectedCaseSHA256:   request.ExpectedCaseSHA256,
		CaseID:               request.CaseID,
		Operation:            "case_routed",
		OccurredAt:           request.OccurredAt,
		validateCurrent: func(_ string, state, document map[string]any, _ []byte) error {
			route := strings.TrimSpace(request.Route)
			if route == "" {
				route = deterministicCaseRoute(document)
			}
			if route != "investigate_more" {
				return nil
			}
			budget, err := InvestigationReentryBudget(state, document)
			if err != nil {
				return err
			}
			if budget.Exhausted {
				return fmt.Errorf("investigate_more route-registration budget exhausted: %d of %d (%s); this limits consecutive route registrations, not professional investigation. Keep the same Case, register a genuinely different discriminator where needed and accept actual results through the original consumer; once evidence supports a disposition, submit that route. New evidence remains progress and is not discarded. Do not invent a duplicate, specification change, requirement change or causal conclusion to bypass this limit", budget.Used, budget.Limit, budget.Source)
			}
			return nil
		},
		Mutate: func(document map[string]any) error {
			existing := stringField(document["route"])
			reassessment := existing == "s9_repair" && strings.TrimSpace(stringField(document["status"])) == "contract_approved" && strings.TrimSpace(request.Route) == "investigate_more" && len(request.CausalReassessmentEvidenceRefs) > 0
			if existing != "" && existing != "investigate_more" && !reassessment {
				return fmt.Errorf("deterministic route update rejected: Case already has terminal route %q; Main must inspect the recorded disposition and its original consumer or recovery path. A real non-blocked S9 targeted failure reopens this same Case through the original causal-reassessment evidence entry; preserve the source Finding set and do not create a regrouped Case to bypass the recorded route", existing)
			}
			route := strings.TrimSpace(request.Route)
			if route == "" {
				route = deterministicCaseRoute(document)
			}
			if !oneOf(route, "s9_repair", "s2_spec_rework", "human_req_change", "s7_no_change", "investigate_more", "duplicate") {
				return fmt.Errorf("route %q is not a supported deterministic Case route", route)
			}
			candidate := cloneMap(document)
			if strings.TrimSpace(request.PrimaryRootCause) != "" {
				candidate["primary_root_cause"] = strings.TrimSpace(request.PrimaryRootCause)
			}
			if strings.TrimSpace(request.NoCompetingHypothesis) != "" {
				candidate["no_competing_hypothesis"] = strings.TrimSpace(request.NoCompetingHypothesis)
			}
			if request.CompetingHypothesisAssessment != nil {
				candidate["competing_hypothesis_assessment"] = cloneMap(request.CompetingHypothesisAssessment)
			}
			if len(request.CausalModel) > 0 {
				candidate["causal_model"] = cloneMap(request.CausalModel)
			}
			if len(request.BlastRadius) > 0 {
				candidate["blast_radius"] = cloneMap(request.BlastRadius)
			}
			if len(request.DetectionGap) > 0 {
				candidate["detection_gap"] = cloneMap(request.DetectionGap)
			}
			if existing == "investigate_more" && !routeHasNewEvidence(document) {
				return fmt.Errorf("deterministic route update rejected: Case remains in investigate_more without new hypothesis or result evidence; register a new discriminator or submit its result before routing again")
			}
			if err := validateRoute(route, candidate, request, reassessment); err != nil {
				return err
			}
			if reassessment {
				document["status"] = "investigating"
				document["repair_contract_ref"] = nil
				document["repair_contract_sha256"] = nil
				document["causal_reassessment_refs"] = evidenceReferencesToAny(request.CausalReassessmentEvidenceRefs)
			}
			if route == "duplicate" {
				canonical, err := resolveCanonicalCase(root, request.CaseID, request.CanonicalCaseID)
				if err != nil {
					return err
				}
				document["canonical_case_id"] = canonical.ID
				document["canonical_case_ref"] = canonical.Path
				document["canonical_case_sha256"] = canonical.SHA256
			}
			document["route"] = route
			document["route_reason"] = strings.TrimSpace(request.RouteReason)
			if strings.TrimSpace(request.PrimaryRootCause) != "" {
				document["primary_root_cause"] = strings.TrimSpace(request.PrimaryRootCause)
			}
			if strings.TrimSpace(request.NoCompetingHypothesis) != "" {
				document["no_competing_hypothesis"] = strings.TrimSpace(request.NoCompetingHypothesis)
			}
			if request.CompetingHypothesisAssessment != nil {
				document["competing_hypothesis_assessment"] = cloneMap(request.CompetingHypothesisAssessment)
			}
			if len(request.CausalModel) > 0 {
				document["causal_model"] = cloneMap(request.CausalModel)
			}
			if len(request.BlastRadius) > 0 {
				document["blast_radius"] = cloneMap(request.BlastRadius)
			}
			if len(request.DetectionGap) > 0 {
				document["detection_gap"] = cloneMap(request.DetectionGap)
			}
			if route != "s9_repair" && route != "investigate_more" {
				document["status"] = "routed"
			}
			occurredAt := request.OccurredAt
			if occurredAt.IsZero() {
				occurredAt = time.Now().UTC()
			}
			history, err := objectArrayAllowEmpty(document["route_history"], "InvestigationCase.route_history")
			if err != nil {
				return err
			}
			hypotheses, err := objectArrayAllowEmpty(document["hypotheses"], "InvestigationCase.hypotheses")
			if err != nil {
				return err
			}
			results, err := objectArrayAllowEmpty(document["hypothesis_results"], "InvestigationCase.hypothesis_results")
			if err != nil {
				return err
			}
			var from any
			if existing != "" {
				from = existing
			}
			history = append(history, map[string]any{
				"from":                    from,
				"to":                      route,
				"reason":                  strings.TrimSpace(request.RouteReason),
				"case_revision":           integerValueOrZero(document["revision"]),
				"hypothesis_count":        len(hypotheses),
				"hypothesis_result_count": len(results),
				"evidence_fingerprint":    evidenceFingerprint(document),
				"occurred_at":             occurredAt.UTC().Format(time.RFC3339Nano),
			})
			document["route_history"] = hypothesesToAny(history)
			return nil
		},
	})
}

type canonicalCaseReference struct {
	ID     string
	Path   string
	SHA256 string
}

// routeHasNewEvidence compares the current immutable Case board with the
// last investigate_more route. A new evidence ref or an actual newly accepted
// result can advance that checkpoint. Adding questions or editing the route
// reason alone cannot turn the same evidence into new observed work.
func routeHasNewEvidence(document map[string]any) bool {
	history, _ := objectArrayAllowEmpty(document["route_history"], "InvestigationCase.route_history")
	lastFingerprint := ""
	previousResultCount := 0
	found := false
	for index := len(history) - 1; index >= 0; index-- {
		if stringField(history[index]["to"]) == "investigate_more" {
			lastFingerprint = stringField(history[index]["evidence_fingerprint"])
			previousResultCount = integerValueOrZero(history[index]["hypothesis_result_count"])
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if evidenceFingerprint(document) != lastFingerprint {
		return true
	}
	all, _ := objectArrayAllowEmpty(document["hypothesis_results"], "InvestigationCase.hypothesis_results")
	if previousResultCount < 0 || previousResultCount >= len(all) {
		return false
	}
	current, _ := currentHypothesisResults(document)
	for _, appended := range all[previousResultCount:] {
		for _, result := range current {
			if hypothesisResultIdentity(appended) == hypothesisResultIdentity(result) {
				return true // Original results append once per registered tuple.
			}
		}
	}
	return false
}

// evidenceFingerprint hashes the sorted set of evidence refs carried by the
// Case's hypotheses and hypothesis results. Any new evidence_ref — even
// inside a pre-existing count — changes the fingerprint, while re-routing
// with recycled or phantom evidence does not.
func evidenceFingerprint(document map[string]any) string {
	values := make([]string, 0, 8)
	hypotheses, _ := objectArrayAllowEmpty(document["hypotheses"], "InvestigationCase.hypotheses")
	for _, hypothesis := range hypotheses {
		values = append(values, stringSliceValues(hypothesis["evidence_refs"])...)
	}
	results, _ := objectArrayAllowEmpty(document["hypothesis_results"], "InvestigationCase.hypothesis_results")
	for _, result := range results {
		values = append(values, stringSliceValues(result["evidence_refs"])...)
	}
	sort.Strings(values)
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return sha256Hex([]byte(strings.Join(unique, "\x00")))
}

func resolveCanonicalCase(root, currentCaseID, canonicalCaseID string) (canonicalCaseReference, error) {
	canonicalCaseID = strings.TrimSpace(canonicalCaseID)
	if canonicalCaseID == "" {
		return canonicalCaseReference{}, errors.New("duplicate route requires canonical_case_id")
	}
	if canonicalCaseID == strings.TrimSpace(currentCaseID) {
		return canonicalCaseReference{}, fmt.Errorf("duplicate route cannot point to the active Case %q; choose the existing canonical Case id", currentCaseID)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return canonicalCaseReference{}, fmt.Errorf("resolve repository root for canonical Case: %w", err)
	}
	directory := filepath.Join(absRoot, ".claude", "review", "investigation", "cases")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return canonicalCaseReference{}, fmt.Errorf("read canonical Case directory: %w", err)
	}
	validator := schema.NewEmbeddedValidator()
	var best canonicalCaseReference
	bestRevision := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return canonicalCaseReference{}, fmt.Errorf("read candidate canonical Case %s: %w", entry.Name(), readErr)
		}
		var candidate map[string]any
		if json.Unmarshal(data, &candidate) != nil || stringField(candidate["case_id"]) != canonicalCaseID {
			continue
		}
		if err := validator.ValidateBytes("review-investigation-case.schema.json", data); err != nil {
			return canonicalCaseReference{}, fmt.Errorf("canonical Case %q is invalid: %v", canonicalCaseID, err)
		}
		revision := integerValueOrZero(candidate["revision"])
		if revision < bestRevision {
			continue
		}
		bestRevision = revision
		best = canonicalCaseReference{
			ID:     canonicalCaseID,
			Path:   filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "cases", entry.Name())),
			SHA256: sha256Hex(data),
		}
	}
	if best.ID == "" {
		return canonicalCaseReference{}, fmt.Errorf("canonical Case %q was not found; inspect runtime investigation status --all and provide an existing Case id", canonicalCaseID)
	}
	return best, nil
}

func updateCaseRevision(root, statePath, journalPath string, request CaseRevisionRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(root) == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "repository root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "resolve repository root: %v", err)
	}
	current, err := runtime.NewStore(statePath, journalPath).WithContext(request.Context).Snapshot()
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("read Runtime before Case revision: %w", err)
	}
	if request.ExpectedRevision >= 0 && current.Revision != request.ExpectedRevision {
		return runtime.Snapshot{}, fmt.Errorf("%w: expected Runtime revision %d but it is %d; next: %s", runtime.ErrStaleRevision, request.ExpectedRevision, current.Revision, caseWorkflowNext)
	}
	pointer, err := mutableCasePointer(current.State, request.CaseID, request.Operation)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	expectedCaseRevision := request.ExpectedCaseRevision
	if expectedCaseRevision < 1 {
		expectedCaseRevision = integerValueOrZero(pointer["revision"])
	}
	expectedCaseSHA256 := strings.TrimSpace(request.ExpectedCaseSHA256)
	if expectedCaseSHA256 == "" {
		expectedCaseSHA256 = stringField(pointer["sha256"])
	}
	if integerValueOrZero(pointer["revision"]) != expectedCaseRevision {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "stale Case revision: expected %d but Runtime points at %d; re-read the Case pointer", expectedCaseRevision, integerValueOrZero(pointer["revision"]))
	}
	if stringField(pointer["sha256"]) != expectedCaseSHA256 {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "stale Case sha256/hash: expected %s but Runtime pins %s; re-read runtime investigation status before retry", expectedCaseSHA256, stringField(pointer["sha256"]))
	}
	caseRel := stringField(pointer["path"])
	casePath, err := repositoryPath(root, caseRel)
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case path is invalid: %v", err)
	}
	caseBytes, err := os.ReadFile(casePath)
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case %q is missing or unreadable: %v", caseRel, err)
	}
	actualSHA := sha256Hex(caseBytes)
	if actualSHA != expectedCaseSHA256 {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case %q sha256 drifted: expected %s but disk is %s; inspect runtime investigation status, then restore the pinned artifact or reconcile", caseRel, expectedCaseSHA256, actualSHA)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseBytes); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case %q schema is invalid: %v", caseRel, err)
	}
	var currentDocument map[string]any
	if err := json.Unmarshal(caseBytes, &currentDocument); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "decode Case %q: %v", caseRel, err)
	}
	if stringField(currentDocument["case_id"]) != request.CaseID || integerValueOrZero(currentDocument["revision"]) != expectedCaseRevision {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case identity or revision does not match the Runtime pointer; re-read status before retry")
	}
	if request.validateCurrent != nil {
		if err := request.validateCurrent(root, current.State, currentDocument, caseBytes); err != nil {
			return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "current Case authorization: %v", err)
		}
	}
	caseIDs, err := stringSlice(currentDocument["source_finding_ids"], "InvestigationCase.source_finding_ids")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	nextDocument := cloneMap(currentDocument)
	if err := request.Mutate(nextDocument); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	if request.Operation == "case_routed" && stringField(nextDocument["route"]) == "s9_repair" {
		if err := validateCausalClosureEvidence(root, current.State, nextDocument); err != nil {
			return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "causal closure evidence: %v", err)
		}
	}
	nextIDs, err := stringSlice(nextDocument["source_finding_ids"], "InvestigationCase.source_finding_ids")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	if len(caseIDs) == 0 {
		if len(nextIDs) != 0 {
			return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "source Finding set is immutable")
		}
	} else if err := exactFindingSetWithDetails(caseIDs, nextIDs); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "source Finding exact set is immutable: %v", err)
	}
	// This existing field is a projection of the current questions/results,
	// never a second authority that can retain historical support after a
	// legitimate Case revision changes the current responsibility.
	nextDocument["unexplained_finding_ids"] = stringSliceAny(difference(caseIDs, supportedExplainedFindingIDs(nextDocument)))
	// RC-18 S8-H3: the Case pins the digest of the S7 frozen baseline it was
	// ingested from. Recompute that digest on every revision so a subject
	// tampering between intake and investigation becomes observable instead of
	// silently inheriting the old pin. The pinned value is never rewritten —
	// changing it would bless the drift — so the mutation message carries the
	// warning into the journal and the status board surfaces it to the caller.
	driftWarning := ""
	if plan, _, planErr := review.LoadPlan(root, current.State); planErr == nil && stringField(nextDocument["baseline_digest"]) != "" {
		if digest := review.SubjectDigest(plan); digest != stringField(nextDocument["baseline_digest"]) {
			driftWarning = fmt.Sprintf("baseline_digest drift: Case pins %s but the frozen ReviewPlan subjects now digest to %s; the investigation baseline is stale — re-verify findings against the current baseline before causal closure",
				stringField(nextDocument["baseline_digest"]), digest)
		}
	}
	nextDocument["case_id"] = request.CaseID
	nextDocument["revision"] = expectedCaseRevision + 1
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	history, err := objectArrayAllowEmpty(currentDocument["revision_history"], "InvestigationCase.revision_history")
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "%v", err)
	}
	history = append(history, map[string]any{
		"revision":    expectedCaseRevision,
		"path":        caseRel,
		"sha256":      expectedCaseSHA256,
		"operation":   nonEmpty(request.Operation, "case_revision_updated"),
		"occurred_at": occurredAt.UTC().Format(time.RFC3339Nano),
	})
	nextDocument["revision_history"] = hypothesesToAny(history)
	nextBytes, err := json.MarshalIndent(nextDocument, "", "  ")
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("encode Case revision: %w", err)
	}
	nextBytes = append(nextBytes, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", nextBytes); err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "next Case revision schema is invalid: %v", err)
	}
	nextRevision := expectedCaseRevision + 1
	nextRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "cases", request.CaseID+fmt.Sprintf("-r%d.json", nextRevision)))
	nextPath, err := repositoryPath(root, nextRel)
	if err != nil {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "next Case path is invalid: %v", err)
	}
	if err := writeExclusive(nextPath, nextBytes); err != nil {
		return runtime.Snapshot{}, fmt.Errorf("write immutable Case revision %s: %w", nextRel, err)
	}
	nextSHA := sha256Hex(nextBytes)
	cleanup := func() {
		// The same lock/pending protocol as the original Runtime writer must
		// guard cleanup. If an interrupted CAS may still publish this artifact,
		// retain it for recovery instead of racing a raw unlink against state.
		_, _ = runtime.NewStore(statePath, journalPath).WithContext(request.Context).RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
			ExpectedRevision: current.Revision,
			ArtifactPath:     nextRel,
			ArtifactSHA256:   nextSHA,
			ReferencedPaths:  stateStringReferences(current.State, nextRel),
		})
	}
	lifecycle, _ := current.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": stringField(lifecycle["state"]), "phase": lifecycle["phase"]}
	runtimeID := stringField(current.State["runtime_id"])
	commitRevision := runtimeCommitRevision(request.ExpectedRevision, current.State)
	baseline, _ := baselineGeneration(current.State)
	status := stringField(nextDocument["status"])
	if status == "" {
		status = "investigating"
	}
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	snapshot, err := updateRuntime(writer, request.ExpectedRevision, runtime.Mutation{
		EventID:                fmt.Sprintf("evt-investigation-case-%s-r%d", request.CaseID, commitRevision+1),
		TransitionID:           "INVESTIGATION-CASE-REVISION-UPDATED",
		Event:                  "investigation_case_revision_updated",
		Actor:                  "orchestrator",
		IdempotencyKey:         fmt.Sprintf("runtime:investigation-case:%s:%d", request.CaseID, commitRevision),
		RuntimeID:              runtimeID,
		From:                   cursor,
		To:                     cursor,
		EvidenceIDs:            []string{request.CaseID},
		RequestID:              "investigation-case-revision",
		BaselineGeneration:     baseline,
		GateID:                 "S8-INVESTIGATION-CASE-REVISION",
		GateFingerprint:        "sha256:investigation-case-revision-v1",
		ProducerResponsibility: "S8 Investigation",
		Message:                driftJournalMessage(nonEmpty(request.Operation, "case_revision_updated"), request.CaseID, nextRevision, driftWarning),
		OccurredAt:             occurredAt,
		Apply: func(state map[string]any) error {
			review, ok := state["review"].(map[string]any)
			if !ok || review == nil {
				return errors.New("Runtime review section is missing; restore state.review before retry")
			}
			existing, ok := review["investigation"].(map[string]any)
			if !ok || existing == nil {
				return fmt.Errorf("active InvestigationCase disappeared; run %s and reconcile", caseWorkflowNext)
			}
			if stringField(existing["case_id"]) != request.CaseID || integerValueOrZero(existing["revision"]) != expectedCaseRevision || stringField(existing["sha256"]) != expectedCaseSHA256 {
				return fmt.Errorf("active InvestigationCase changed during update; re-read %s and retry", caseWorkflowNext)
			}
			if request.validateCurrent != nil {
				// Re-read the source under the Runtime writer lock. Checking only the
				// staged candidate would allow Case bytes/route/source registration
				// to drift after preflight but before the original CAS commits.
				currentBytes, readErr := os.ReadFile(casePath)
				if readErr != nil {
					return fmt.Errorf("re-read pinned InvestigationCase inside CAS: %w", readErr)
				}
				if sha256Hex(currentBytes) != expectedCaseSHA256 || !bytes.Equal(currentBytes, caseBytes) {
					return errors.New("pinned InvestigationCase bytes changed before original CAS")
				}
				var lockedDocument map[string]any
				if err := json.Unmarshal(currentBytes, &lockedDocument); err != nil {
					return fmt.Errorf("decode pinned InvestigationCase inside CAS: %w", err)
				}
				if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", currentBytes); err != nil {
					return fmt.Errorf("pinned InvestigationCase schema changed before CAS: %w", err)
				}
				if err := request.validateCurrent(root, state, lockedDocument, currentBytes); err != nil {
					return fmt.Errorf("revalidate current Case authorization inside original CAS: %w", err)
				}
			}
			if existing["status"] == "contract_approved" && nextDocument["status"] == "investigating" {
				if err := retireSupersededRepairPointer(state, request.CaseID); err != nil {
					return err
				}
			}
			existing["path"] = nextRel
			existing["sha256"] = nextSHA
			existing["revision"] = nextRevision
			existing["status"] = status
			existing["source_finding_ids"] = stringSliceAny(caseIDs)
			existing["route"] = nextDocument["route"]
			existing["route_reason"] = nextDocument["route_reason"]
			existing["canonical_case_id"] = nextDocument["canonical_case_id"]
			existing["canonical_case_ref"] = nextDocument["canonical_case_ref"]
			existing["canonical_case_sha256"] = nextDocument["canonical_case_sha256"]
			syncOptionalPointerField(existing, nextDocument, "repair_contract_ref")
			syncOptionalPointerField(existing, nextDocument, "repair_contract_sha256")
			existing["route_consumed_at"] = nextDocument["route_consumed_at"]
			existing["route_consumer"] = nextDocument["route_consumer"]
			existing["next_action"] = nextDocument["next_action"]
			// RC-18 S8-H3 status-board outlet: keep the latest drift warning
			// (or clear it) so `runtime investigation status` shows it.
			review["investigation_baseline_drift"] = driftWarningPointer(driftWarning)
			existing["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
	if err != nil {
		cleanup()
		return runtime.Snapshot{}, err
	}
	return snapshot, nil
}

// driftJournalMessage appends the RC-18 baseline-drift warning (S8-H3) to the
// journal message when present. Empty warnings leave the message unchanged.
func driftJournalMessage(operation, caseID string, revision int, driftWarning string) string {
	message := fmt.Sprintf("%s: %s revision %d", operation, caseID, revision)
	if driftWarning != "" {
		message += "; WARNING " + driftWarning
	}
	return message
}

// driftWarningPointer maps the drift warning into the state value the status
// board reads: the warning string when drifted, nil when the baseline matches.
func driftWarningPointer(driftWarning string) any {
	if driftWarning == "" {
		return nil
	}
	return driftWarning
}

func deterministicCaseRoute(document map[string]any) string {
	if requireCurrentHypothesisResults(document) != nil {
		return "investigate_more"
	}
	caseIDs, _ := stringSlice(document["source_finding_ids"], "source_finding_ids")
	explained := supportedExplainedFindingIDs(document)
	if len(difference(caseIDs, explained)) != 0 || !nonEmptyObject(document["causal_model"]) || strings.TrimSpace(stringField(document["primary_root_cause"])) == "" {
		return "investigate_more"
	}
	if err := validateCausalClosure(document); err != nil {
		return "investigate_more"
	}
	return "s9_repair"
}

func validateRoute(route string, document map[string]any, request RouteRequest, reassessment bool) error {
	if route == "s9_repair" {
		if deterministicCaseRoute(document) != "s9_repair" {
			// Surface the exact structural reason instead of the generic
			// determinism error so the Investigator can close the specific gap.
			if err := causalClosureDefect(document); err != nil {
				return err
			}
			return errors.New("route is not deterministic: every source Finding must be explained by supported results and causal closure must be complete")
		}
		if strings.TrimSpace(request.PrimaryRootCause) == "" && strings.TrimSpace(stringField(document["primary_root_cause"])) == "" {
			return errors.New("s9_repair requires primary_root_cause")
		}
		if len(request.CausalModel) == 0 && !nonEmptyObject(document["causal_model"]) {
			return errors.New("s9_repair requires causal_model")
		}
	}
	if route == "investigate_more" && !reassessment && deterministicCaseRoute(document) != "investigate_more" {
		return errors.New("route is not deterministic: the Case is already causally closed; choose a concrete disposition")
	}
	if route == "duplicate" && strings.TrimSpace(request.CanonicalCaseID) == "" {
		return errors.New("duplicate route requires canonical_case_id")
	}
	return nil
}

// causalClosureDefect reports the first missing S8 causal-material element.
// It returns nil when the Case document satisfies the closure contract.
func causalClosureDefect(document map[string]any) error {
	if err := requireCurrentHypothesisResults(document); err != nil {
		return err
	}
	caseIDs, _ := stringSlice(document["source_finding_ids"], "source_finding_ids")
	explained := supportedExplainedFindingIDs(document)
	if len(difference(caseIDs, explained)) != 0 {
		return fmt.Errorf("route is not deterministic: source Findings remain unexplained: %v", difference(caseIDs, explained))
	}
	if !nonEmptyObject(document["causal_model"]) {
		return errors.New("route is not deterministic: causal_model is missing or empty")
	}
	if strings.TrimSpace(stringField(document["primary_root_cause"])) == "" {
		return errors.New("route is not deterministic: primary_root_cause is missing")
	}
	return validateCausalClosure(document)
}

func validateAssignmentID(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required; allocate an Assignment id with the assignment- prefix and choose a self-describing token (it binds the hypothesis to the question its Investigator will answer; `runtime investigation dispatch` attaches the Investigator later)", field)
	}
	if !strings.HasPrefix(value, "assignment-") {
		return fmt.Errorf("%s %q must use the assignment- prefix so Runtime can bind it", field, value)
	}
	if strings.TrimPrefix(value, "assignment-") == "" {
		return fmt.Errorf("%s %q must include a unique token after the assignment- prefix", field, value)
	}
	return nil
}

func validateEvidenceReferences(root string, refs []EvidenceReference) error {
	seen := make(map[string]struct{}, len(refs))
	for index, ref := range refs {
		path := strings.TrimSpace(ref.Path)
		sha := strings.TrimSpace(ref.SHA256)
		if path == "" || sha == "" {
			return fmt.Errorf("causal_reassessment_evidence_refs[%d] requires path and sha256", index)
		}
		if _, ok := seen[path]; ok {
			return fmt.Errorf("causal_reassessment_evidence_refs contains duplicate path %q", path)
		}
		seen[path] = struct{}{}
		absolute, err := repositoryPath(root, path)
		if err != nil {
			return fmt.Errorf("causal reassessment evidence %q is invalid: %v", path, err)
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return fmt.Errorf("causal reassessment evidence %q is missing or unreadable: %v", path, err)
		}
		if actual := sha256Hex(data); actual != sha {
			return fmt.Errorf("causal reassessment evidence %q sha256 drifted: expected %s but disk is %s; re-read the targeted artifact", path, sha, actual)
		}
	}
	return nil
}

func evidenceReferencesToAny(refs []EvidenceReference) []any {
	result := make([]any, 0, len(refs))
	for _, ref := range refs {
		result = append(result, map[string]any{"path": strings.TrimSpace(ref.Path), "sha256": strings.TrimSpace(ref.SHA256)})
	}
	return result
}

func syncOptionalPointerField(pointer, document map[string]any, field string) {
	value, ok := document[field]
	if !ok || value == nil || strings.TrimSpace(stringField(value)) == "" {
		delete(pointer, field)
		return
	}
	pointer[field] = value
}

func mutableCasePointer(state map[string]any, caseID, operation string) (map[string]any, error) {
	review, ok := state["review"].(map[string]any)
	if !ok || review == nil {
		return nil, caseWorkflowError(caseID, "state.review is missing; run runtime investigation ingest before continuing")
	}
	pointer, ok := review["investigation"].(map[string]any)
	if !ok || pointer == nil {
		return nil, caseWorkflowError(caseID, "state.review.investigation is missing; run runtime investigation ingest before continuing")
	}
	if stringField(pointer["case_id"]) != caseID {
		return nil, caseWorkflowError(caseID, "active InvestigationCase is %q, not %q; inspect runtime investigation status before retry", stringField(pointer["case_id"]), caseID)
	}
	status := stringField(pointer["status"])
	if status != "investigating" && !(status == "contract_approved" && operation == "case_routed") {
		return nil, caseWorkflowError(caseID, "InvestigationCase %s is %q; re-open it with a causal reassessment route before recording new S8 evidence", caseID, status)
	}
	return pointer, nil
}

// retireSupersededRepairPointer removes the active S9 pointer when a targeted
// failure is deliberately handed back to S8. The failed TargetedReverification
// remains content-addressed in the Case's causal_reassessment_refs; keeping the
// old review.repair pointer active would make the next S9 session look live and
// reject a new approved Contract as a duplicate session.
func retireSupersededRepairPointer(state map[string]any, caseID string) error {
	review, ok := state["review"].(map[string]any)
	if !ok || review == nil {
		return errors.New("state.review is missing while retiring the superseded S9 pointer")
	}
	value, exists := review["repair"]
	if !exists || value == nil {
		return nil
	}
	pointer, ok := value.(map[string]any)
	if !ok {
		return errors.New("state.review.repair is malformed while retiring the superseded S9 pointer")
	}
	if pointerCaseID := strings.TrimSpace(stringField(pointer["case_id"])); pointerCaseID != "" && pointerCaseID != caseID {
		return fmt.Errorf("active S9 RepairSession belongs to Case %q, not %q; reconcile the active repair before causal reassessment", pointerCaseID, caseID)
	}
	review["repair"] = nil
	return nil
}

func supportedExplainedFindingIDs(document map[string]any) []string {
	results, _ := currentHypothesisResults(document)
	var ids []string
	for _, result := range results {
		if stringField(result["result"]) == "supported" {
			values, _ := stringSlice(result["explains_finding_ids"], "explains_finding_ids")
			ids = append(ids, values...)
		}
	}
	// Union semantics: a Finding explained by MULTIPLE supported hypotheses is
	// normal investigation reality (a primary mechanism plus narrower process
	// hypotheses). normalizeOptionalSet errors on duplicates and this caller
	// discarded that error, which silently marked every Finding unexplained.
	// Deduplicate instead — each result's explains set is already validated
	// against the source Finding set at submission time.
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return sortedStrings(out)
}

type hypothesisResultKey struct{ hypothesis, assignment string }

func hypothesisResultIdentity(row map[string]any) hypothesisResultKey {
	return hypothesisResultKey{stringField(row["hypothesis_id"]), stringField(row["assignment_id"])}
}

// Match only the unique result for each exact current question/Assignment.
// Historical rows remain readable, but cannot answer a different responsibility.
// An inconclusive row is received work; it supplies neither support nor refutation.
func currentHypothesisResults(document map[string]any) (effective []map[string]any, pending []string) {
	hypotheses, _ := objectArrayAllowEmpty(document["hypotheses"], "InvestigationCase.hypotheses")
	results, _ := objectArrayAllowEmpty(document["hypothesis_results"], "InvestigationCase.hypothesis_results")
	byQuestion := make(map[hypothesisResultKey][]map[string]any, len(results))
	for _, result := range results {
		byQuestion[hypothesisResultIdentity(result)] = append(byQuestion[hypothesisResultIdentity(result)], result)
	}
	identities := make(map[string]int, len(hypotheses))
	for _, hypothesis := range hypotheses {
		identities[stringField(hypothesis["hypothesis_id"])]++
	}
	for _, hypothesis := range hypotheses {
		key := hypothesisResultIdentity(hypothesis)
		matched := byQuestion[key]
		if key.hypothesis == "" || key.assignment == "" || identities[key.hypothesis] != 1 || len(matched) != 1 {
			pending = append(pending, key.hypothesis)
			continue
		}
		effective = append(effective, matched[0])
	}
	return effective, sortedStrings(pending)
}

func requireCurrentHypothesisResults(document map[string]any) error {
	_, pending := currentHypothesisResults(document)
	if len(pending) != 0 {
		return fmt.Errorf("current hypotheses have no unique accepted result: %v; Main must finish those exact questions before repair, or choose the original non-repair disposition", pending)
	}
	return nil
}

func validateNewHypothesis(hypothesis map[string]any, caseIDs []string) error {
	if strings.TrimSpace(stringField(hypothesis["hypothesis_id"])) == "" || strings.TrimSpace(stringField(hypothesis["statement"])) == "" || strings.TrimSpace(stringField(hypothesis["invariant"])) == "" || strings.TrimSpace(stringField(hypothesis["discriminator"])) == "" {
		return errors.New("new hypothesis requires hypothesis_id, assignment_id, statement, invariant, and discriminator")
	}
	if err := validateAssignmentID(stringField(hypothesis["assignment_id"]), "new hypothesis.assignment_id"); err != nil {
		return err
	}
	if _, err := nonEmptyStrings(stringSliceValues(hypothesis["evidence_refs"]), "new hypothesis.evidence_refs"); err != nil {
		return err
	}
	expected, ok := hypothesis["expected_outcomes"].(map[string]any)
	if !ok || strings.TrimSpace(stringField(expected["support"])) == "" || strings.TrimSpace(stringField(expected["refute"])) == "" {
		return errors.New("new hypothesis expected_outcomes must name non-empty support and refute outcomes")
	}
	ids, err := stringSlice(hypothesis["source_finding_ids"], "new hypothesis source_finding_ids")
	if err != nil {
		return err
	}
	return subsetOf(ids, caseIDs, "new hypothesis source_finding_ids")
}

// stringSliceValues reads a raw JSON string array without failing the caller;
// validation of individual entries is left to nonEmptyStrings.
func stringSliceValues(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, _ := item.(string)
		result = append(result, text)
	}
	return result
}

// validateBlastRadius enforces the S8-3 repair contract: blast_radius must
// name the concrete surface set the mechanism can pollute. An empty object,
// an empty path set, or a paths entry without content fails closed before a
// hollow causal dossier can reach S9.
func validateBlastRadius(blastRadius map[string]any) error {
	if blastRadius == nil {
		return errors.New("blast_radius is missing or empty")
	}
	paths := stringSliceValues(blastRadius["paths"])
	if len(paths) == 0 {
		return errors.New("blast_radius.paths must name at least one surface, endpoint, artifact, or data set the mechanism can pollute")
	}
	seen := make(map[string]struct{}, len(paths))
	for index, path := range paths {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("blast_radius.paths[%d] must be a non-empty path or surface id", index)
		}
		if _, exists := seen[path]; exists {
			return fmt.Errorf("blast_radius.paths contains duplicate entry %q", path)
		}
		seen[path] = struct{}{}
	}
	return nil
}

// validateDetectionGap enforces the S8-3 repair contract: detection_gap must
// declare its type and bind at least one evidence reference so the repair
// stage can assert the closing detection. A free-text-only gap is rejected.
func validateDetectionGap(detectionGap map[string]any) error {
	if detectionGap == nil {
		return errors.New("detection_gap is missing or empty")
	}
	if strings.TrimSpace(stringField(detectionGap["gap_type"])) == "" {
		return errors.New("detection_gap.gap_type is required (test | contract | type | monitoring | process)")
	}
	if !oneOf(strings.TrimSpace(stringField(detectionGap["gap_type"])), "test", "contract", "type", "monitoring", "process") {
		return fmt.Errorf("detection_gap.gap_type %q is not a supported detection layer (test | contract | type | monitoring | process)", stringField(detectionGap["gap_type"]))
	}
	refs := stringSliceValues(detectionGap["evidence_refs"])
	if len(refs) == 0 {
		return errors.New("detection_gap.evidence_refs must bind at least one evidence reference showing why the gap exists")
	}
	for index, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("detection_gap.evidence_refs[%d] must be a non-empty evidence reference", index)
		}
	}
	return nil
}

// requireCompetingHypothesisClosure keeps absence distinct from a professional
// assessment of credible alternatives handled by current actual discriminators.
func requireCompetingHypothesisClosure(document map[string]any) error {
	results, _ := currentHypothesisResults(document)
	declaredNone := strings.TrimSpace(stringField(document["no_competing_hypothesis"])) != ""
	raw, assessed := document["competing_hypothesis_assessment"]
	if assessed {
		if declaredNone {
			return errors.New("competing_hypothesis_assessment and no_competing_hypothesis are mutually exclusive")
		}
		assessment, ok := raw.(map[string]any)
		if !ok || len(assessment) != 2 || strings.TrimSpace(stringField(assessment["conclusion"])) == "" {
			return errors.New("competing_hypothesis_assessment requires only hypothesis_ids and a non-empty conclusion explaining how credible alternatives were discriminated")
		}
		ids, err := stringSlice(assessment["hypothesis_ids"], "competing_hypothesis_assessment.hypothesis_ids")
		if err != nil {
			return err
		}
		ids, err = normalizeSet(ids, "competing_hypothesis_assessment.hypothesis_ids")
		if err != nil {
			return err
		}
		current := map[string]string{}
		for _, result := range results {
			current[stringField(result["hypothesis_id"])] = stringField(result["result"])
		}
		for _, id := range ids {
			if !oneOf(current[id], "supported", "refuted") {
				return fmt.Errorf("competing_hypothesis_assessment hypothesis %q requires a current accepted supported or refuted result; unknown, unanswered and inconclusive questions cannot close alternatives", id)
			}
		}
		return nil
	}
	for _, result := range results {
		if stringField(result["result"]) == "refuted" {
			return nil
		}
	}
	if declaredNone {
		return nil
	}
	return errors.New("causal closure requires a current refuted hypothesis result, competing_hypothesis_assessment of current conclusive discriminators, or no_competing_hypothesis with the reason no alternative mechanism was credible")
}

// validateCausalClosure composes the structural S8 causal-material checks
// shared by the deterministic s9_repair route and Contract approval.
func validateCausalClosure(document map[string]any) error {
	if err := requireCurrentHypothesisResults(document); err != nil {
		return err
	}
	if err := requireCompetingHypothesisClosure(document); err != nil {
		return err
	}
	blastRadius, _ := document["blast_radius"].(map[string]any)
	if err := validateBlastRadius(blastRadius); err != nil {
		return err
	}
	detectionGap, _ := document["detection_gap"].(map[string]any)
	return validateDetectionGap(detectionGap)
}

// validateCausalClosureEvidence is the Runtime-backed half of the S8 causal
// closure gate. Structural validation alone is not enough for detection-gap
// evidence: every ref that can authorize the S8→S9 route must resolve through
// the same current-generation/SHA evidence validator used by hypotheses and
// hypothesis results.
func validateCausalClosureEvidence(root string, state, document map[string]any) error {
	if err := validateCausalClosure(document); err != nil {
		return err
	}
	detectionGap, _ := document["detection_gap"].(map[string]any)
	refs := stringSliceValues(detectionGap["evidence_refs"])
	// Reuse exact original Finding sources through their original sealed Batch,
	// rather than demanding that S8 recapture or register an alias for S7 facts.
	if stringField(document["observation_batch_ref"]) != "" {
		var err error
		refs, err = remainingFindingQuestionRefs(root, state, document, stringSliceAnyToStrings(document["source_finding_ids"]), refs)
		if err != nil {
			return fmt.Errorf("detection_gap.evidence_refs: %w", err)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	if err := ValidateEvidenceRefs(refs, EvidenceAttestationOptions{
		AllowPaths:         true,
		State:              state,
		Root:               root,
		RequireSHA:         true,
		RequireReviewRound: currentReviewRound(state),
	}); err != nil {
		return fmt.Errorf("detection_gap.evidence_refs: %w", err)
	}
	return nil
}

func objectArray(value any, field string) ([]map[string]any, error) {
	return objectArrayAllowEmpty(value, field)
}

func objectArrayAllowEmpty(value any, field string) ([]map[string]any, error) {
	if value == nil {
		return []map[string]any{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of objects", field)
	}
	result := make([]map[string]any, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", field, index)
		}
		result = append(result, object)
	}
	return result, nil
}

func hypothesesToAny(values []map[string]any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func normalizeOptionalSet(values []string, field string) ([]string, error) {
	if len(values) == 0 {
		return []string{}, nil
	}
	return normalizeSet(values, field)
}

func subsetOf(values, allowed []string, field string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowedSet[value]; !ok {
			return fmt.Errorf("%s contains %q outside the Case source Finding set", field, value)
		}
	}
	return nil
}

func nonEmptyStrings(values []string, field string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s must contain at least one reference", field)
	}
	result := make([]string, 0, len(values))
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s[%d] must be non-empty", field, index)
		}
		result = append(result, strings.TrimSpace(value))
	}
	return result, nil
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func currentReviewRound(state map[string]any) int {
	review, _ := state["review"].(map[string]any)
	if review == nil {
		return 0
	}
	switch v := review["round"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

// stateStringReferences returns the canonical repository-relative path only
// when the current Runtime state contains that exact reference. Cleanup then
// rechecks the same Runtime revision under its original lock before unlinking.
func stateStringReferences(value any, target string) []string {
	target = filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	found := false
	var visit func(any)
	visit = func(node any) {
		if found {
			return
		}
		switch typed := node.(type) {
		case string:
			if typed == target || filepath.ToSlash(filepath.Clean(filepath.FromSlash(typed))) == target {
				found = true
			}
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(value)
	if found {
		return []string{target}
	}
	return nil
}

func caseWorkflowError(caseID, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if strings.TrimSpace(caseID) == "" {
		return fmt.Errorf("%s; next: %s", message, caseWorkflowNext)
	}
	return fmt.Errorf("%s; next: runtime investigation status --case-id %s", message, caseID)
}
