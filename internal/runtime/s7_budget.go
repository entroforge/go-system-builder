package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const (
	S7BudgetDecisionIncrease   = "increase_budget"
	S7BudgetDecisionGovernance = "return_to_governance"
)

// S7BudgetDecision is the human-authored decision artifact consumed when the
// configured full-review budget is exhausted. The artifact is deliberately
// small: the Runtime supplies the authoritative revision, runtime id, and
// evidence scope at commit time.
type S7BudgetDecision struct {
	Decision                    string `json:"decision"`
	RuntimeID                   string `json:"runtime_id"`
	ExpectedRevision            int    `json:"expected_revision"`
	ReviewRound                 int    `json:"review_round"`
	PreviousMaxFullReviewRounds int    `json:"previous_max_full_review_rounds"`
	NewMaxFullReviewRounds      int    `json:"new_max_full_review_rounds"`
	Reason                      string `json:"reason"`
	AuthorizedBy                string `json:"authorized_by"`
	CreatedAt                   string `json:"created_at,omitempty"`
	DecisionID                  string `json:"decision_id,omitempty"`
}

// S7BudgetDecisionRequest describes the single CAS operation that records a
// human decision and, for increase_budget, changes the review budget. The
// decision file is also recorded as human_decision evidence in this same
// mutation, so an accepted decision cannot leave an unreferenced artifact.
type S7BudgetDecisionRequest struct {
	ExpectedRevision int
	DecisionPath     string
	Actor            string
	Validator        CandidateValidator
}

// S7BudgetDecisionReceipt is the durable handoff returned to the CLI. A
// governance decision is followed by GTR-006 at Revision; an increase is
// ready for the controller to retry the pending round-opening transition.
type S7BudgetDecisionReceipt struct {
	Snapshot     Snapshot
	Decision     S7BudgetDecision
	EvidenceID   string
	EvidencePath string
	ScopeRef     string
}

// ApplyS7BudgetDecision records a human S7 budget decision through one Runtime
// CAS commit. It accepts a decision only when the current round has reached
// the configured limit, rejects stale or mismatched artifacts, and never lets
// a human lower the limit. The caller owns any subsequent lifecycle transition
// for return_to_governance.
func ApplyS7BudgetDecision(root, statePath, journalPath string, request S7BudgetDecisionRequest) (S7BudgetDecisionReceipt, error) {
	if request.ExpectedRevision < 0 {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("expected revision must be non-negative")
	}
	if strings.TrimSpace(request.Actor) == "" {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision actor is required")
	}
	cleanPath, err := safeEvidencePath(root, request.DecisionPath)
	if err != nil {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(root, cleanPath))
	if err != nil {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("read S7 budget decision: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("s7-budget-decision.schema.json", data); err != nil {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision schema: %w", err)
	}
	var decision S7BudgetDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("decode S7 budget decision: %w", err)
	}
	if err := validateS7BudgetDecision(decision, request.Actor); err != nil {
		return S7BudgetDecisionReceipt{}, err
	}

	store := NewWriter(statePath, journalPath, root, request.Validator)
	snapshot, err := store.Snapshot()
	if err != nil {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("read runtime: %w", err)
	}
	state := snapshot.State
	if snapshot.Revision != request.ExpectedRevision {
		return S7BudgetDecisionReceipt{}, ErrStaleRevision
	}
	if decision.ExpectedRevision != request.ExpectedRevision {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision expected_revision=%d does not match requested revision %d", decision.ExpectedRevision, request.ExpectedRevision)
	}
	runtimeID, _ := state["runtime_id"].(string)
	if decision.RuntimeID != runtimeID {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision runtime_id %q does not match current runtime %q", decision.RuntimeID, runtimeID)
	}
	review, _ := state["review"].(map[string]any)
	currentRound, err := integerField(review, "round")
	if err != nil {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("read S7 review round: %w", err)
	}
	if decision.ReviewRound != currentRound {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision review_round=%d does not match current round %d", decision.ReviewRound, currentRound)
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	lifecycleState, _ := lifecycle["state"].(string)
	lifecyclePhase, _ := lifecycle["phase"].(string)
	if lifecycleState != "acceptance" && !(lifecycleState == "bug_resolution" && lifecyclePhase == "ready_for_full_review") {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision is only legal at acceptance or bug_resolution.ready_for_full_review; current cursor is %s.%s", lifecycleState, lifecyclePhase)
	}
	maxRounds, err := s7MaxFullReviewRounds(state)
	if err != nil {
		return S7BudgetDecisionReceipt{}, err
	}
	if currentRound < maxRounds {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 review budget is not exhausted: round %d of %d; continue the current round before asking for a budget decision", currentRound, maxRounds)
	}
	if decision.PreviousMaxFullReviewRounds != maxRounds {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("S7 budget decision previous_max_full_review_rounds=%d does not match current limit %d", decision.PreviousMaxFullReviewRounds, maxRounds)
	}
	if decision.Decision == S7BudgetDecisionIncrease && decision.NewMaxFullReviewRounds <= maxRounds {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("new_max_full_review_rounds must be greater than current limit %d", maxRounds)
	}
	if decision.Decision == S7BudgetDecisionGovernance && decision.NewMaxFullReviewRounds != 0 {
		return S7BudgetDecisionReceipt{}, fmt.Errorf("return_to_governance must not change new_max_full_review_rounds")
	}

	committedRevision := request.ExpectedRevision + 1
	evidenceID := strings.TrimSpace(decision.DecisionID)
	if evidenceID == "" {
		evidenceID = fmt.Sprintf("ev-s7-budget-decision-r%d", committedRevision)
	}
	scopePrefix := "runtime_budget"
	if decision.Decision == S7BudgetDecisionGovernance {
		scopePrefix = "runtime_governance"
	}
	scopeRef := fmt.Sprintf("%s:%s@%d", scopePrefix, runtimeID, committedRevision)
	baselineGeneration := baselineGeneration(state)
	reviewRound := decision.ReviewRound
	occurredAt := time.Now().UTC()
	if decision.CreatedAt != "" {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, decision.CreatedAt); parseErr == nil {
			occurredAt = parsed.UTC()
		}
	}
	decision.DecisionID = evidenceID

	receipt := S7BudgetDecisionReceipt{
		Decision: decision, EvidenceID: evidenceID, EvidencePath: cleanPath, ScopeRef: scopeRef,
	}
	next, err := store.Update(request.ExpectedRevision, Mutation{
		EventID:        fmt.Sprintf("evt-s7-budget-decision-r%d", committedRevision),
		TransitionID:   "S7-BUDGET-DECISION",
		Event:          "s7_budget_decision_recorded",
		Actor:          request.Actor,
		IdempotencyKey: fmt.Sprintf("runtime:s7-budget-decision:%s:%d", evidenceID, request.ExpectedRevision),
		RuntimeID:      runtimeID,
		EvidenceIDs:    []string{evidenceID},
		Message:        fmt.Sprintf("Recorded human S7 budget decision %s.", decision.Decision),
		OccurredAt:     occurredAt,
		Apply: func(state map[string]any) error {
			items, ok := state["evidence"].([]any)
			if !ok {
				return fmt.Errorf("runtime evidence must be an array")
			}
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item != nil && item["id"] == evidenceID {
					return fmt.Errorf("S7 budget decision evidence %s is already registered", evidenceID)
				}
			}
			items = append(items, map[string]any{
				"id": evidenceID, "kind": "human_decision", "path": cleanPath,
				"sha256": sha256Hex(data), "status": "valid",
				"baseline_generation": baselineGeneration, "review_round": reviewRound,
				"produced_by": []string{request.Actor}, "invalidated_by": nil,
				"invalidation_rule": nil, "invalidation_reason": nil,
				"responsibility_id": nil, "scope_refs": []string{scopeRef},
			})
			state["evidence"] = items
			configuration, ok := state["configuration"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime configuration must be an object")
			}
			repair, ok := configuration["repair"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime configuration.repair must be an object")
			}
			if decision.Decision == S7BudgetDecisionIncrease {
				repair["max_full_review_rounds"] = decision.NewMaxFullReviewRounds
			}
			var newMax any
			if decision.Decision == S7BudgetDecisionIncrease {
				newMax = decision.NewMaxFullReviewRounds
			}
			repair["last_budget_decision"] = map[string]any{
				"decision": decision.Decision, "evidence_id": evidenceID, "evidence_path": cleanPath,
				"review_round": reviewRound, "previous_max_full_review_rounds": maxRounds,
				"new_max_full_review_rounds": newMax, "authorized_by": request.Actor,
				"reason": decision.Reason, "decided_at": occurredAt.Format(time.RFC3339Nano),
				"committed_revision": committedRevision,
			}
			state["updated_at"] = occurredAt.Format(time.RFC3339Nano)
			return nil
		},
	})
	if err != nil {
		return S7BudgetDecisionReceipt{}, err
	}
	receipt.Snapshot = next
	return receipt, nil
}

func validateS7BudgetDecision(decision S7BudgetDecision, actor string) error {
	switch decision.Decision {
	case S7BudgetDecisionIncrease, S7BudgetDecisionGovernance:
	default:
		return fmt.Errorf("unsupported S7 budget decision %q; choose increase_budget or return_to_governance", decision.Decision)
	}
	if strings.TrimSpace(decision.RuntimeID) == "" {
		return fmt.Errorf("S7 budget decision runtime_id is required")
	}
	if decision.ExpectedRevision < 0 {
		return fmt.Errorf("S7 budget decision expected_revision must be non-negative")
	}
	if decision.ReviewRound < 1 {
		return fmt.Errorf("S7 budget decision review_round must be at least 1")
	}
	if decision.PreviousMaxFullReviewRounds < 1 {
		return fmt.Errorf("S7 budget decision previous_max_full_review_rounds must be at least 1")
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return fmt.Errorf("S7 budget decision reason is required")
	}
	if strings.TrimSpace(decision.AuthorizedBy) == "" || strings.TrimSpace(decision.AuthorizedBy) != strings.TrimSpace(actor) {
		return fmt.Errorf("S7 budget decision authorized_by must match --actor")
	}
	if decision.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, decision.CreatedAt); err != nil {
			return fmt.Errorf("S7 budget decision created_at must be RFC3339: %w", err)
		}
	}
	return nil
}

func s7MaxFullReviewRounds(state map[string]any) (int, error) {
	configuration, ok := state["configuration"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("runtime configuration must be an object")
	}
	repair, ok := configuration["repair"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("runtime configuration.repair must be an object")
	}
	max, err := integerField(repair, "max_full_review_rounds")
	if err != nil {
		return 0, fmt.Errorf("read max_full_review_rounds: %w", err)
	}
	if max < 1 {
		return 0, fmt.Errorf("max_full_review_rounds must be at least 1")
	}
	return max, nil
}
