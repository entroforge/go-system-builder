package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ---------------------------------------------------------------------------
// blocked_by_confirmed_finding (L3-S7 §3.5, §5.2, §9.1 step 14)
//
// `blocked` is never a Reviewer professional conclusion: the tool projects it
// only when a confirmed product Finding of the current round objectively
// breaks a build/start/entry/precondition for a required Claim. The
// projection always carries blocking_finding_ids + failed_precondition +
// evidence_refs + after_repair_required=true. Token/time/agent shortage has
// no confirmed Finding to bind, so it can never reach this projection
// (§14.1 anti-abuse row); environment/credential/tooling gaps reuse the
// Assignment BLOCKER and stay in S7 instead.
// ---------------------------------------------------------------------------

// validateBlockedClaims enforces the full §3.5 validation chain for every
// blocked_claims entry. Anything missing rejects the whole submit atomically.
func validateBlockedClaims(root string, state map[string]any, plan *Plan, assignment *PlanAssignment, result *Result) error {
	if len(result.BlockedClaims) == 0 {
		return nil
	}
	claimByID := make(map[string]Claim, len(plan.Claims))
	for _, claim := range plan.Claims {
		claimByID[claim.ClaimID] = claim
	}
	// Confirmed Findings this projection may bind: every Finding consumed in
	// the current round plus the Findings this very result confirms in the
	// same transaction. Prior-round or unknown ids are not evidence.
	confirmed := map[string]bool{}
	confirmedEvidence := map[string]map[string]any{}
	currentFindings := map[string]Finding{}
	for _, row := range RoundFindings(state) {
		findingID := stringField(row["finding_id"])
		if findingID == "" || row["status"] == "invalid" || intField(row["review_round"]) != result.ReviewRound {
			continue
		}
		confirmed[findingID] = true
		confirmedEvidence[findingID] = row
	}
	for _, raw := range evidenceEntries(state) {
		row, _ := raw.(map[string]any)
		if row == nil || row["status"] == "invalid" || intField(row["review_round"]) != result.ReviewRound {
			continue
		}
		if id := stringField(row["id"]); id != "" {
			confirmedEvidence[id] = row
		}
	}
	currentFindingIDs := map[string]bool{}
	for _, finding := range result.Findings {
		confirmed[finding.FindingID] = true
		currentFindingIDs[finding.FindingID] = true
		currentFindings[finding.FindingID] = finding
	}
	answered := map[string]bool{}
	for _, claimResult := range result.ClaimResults {
		answered[claimResult.ClaimID] = true
	}
	seen := map[string]bool{}
	for _, blocked := range result.BlockedClaims {
		if seen[blocked.ClaimID] {
			return fmt.Errorf("blocked_claims contains %s twice", blocked.ClaimID)
		}
		seen[blocked.ClaimID] = true
		claim, ok := claimByID[blocked.ClaimID]
		if !ok {
			return fmt.Errorf("blocked_claims references unknown claim %s", blocked.ClaimID)
		}
		if claim.Applicability == "not_applicable" {
			return fmt.Errorf("claim %s is not_applicable in the plan; N/A is a plan disposition and can never be blocked (L3-S7 §9.3)", blocked.ClaimID)
		}
		if answered[blocked.ClaimID] {
			return fmt.Errorf("claim %s is answered by both claim_results and blocked_claims; a Claim gets exactly one disposition", blocked.ClaimID)
		}
		if len(blocked.BlockingFindingIDs) == 0 {
			return fmt.Errorf("blocked claim %s names no blocking_finding_ids; without a confirmed product Finding a Claim stays dispatched/queued in S7 — token, time or Agent shortage is never blocked_by_confirmed_finding (L3-S7 §14.1)", blocked.ClaimID)
		}
		for _, findingID := range blocked.BlockingFindingIDs {
			if !confirmed[findingID] {
				return fmt.Errorf("blocked claim %s binds finding %s which is not a confirmed Finding of review round %d; blocked_by_confirmed_finding only binds findings confirmed in this round", blocked.ClaimID, findingID, result.ReviewRound)
			}
		}
		switch blocked.FailedPrecondition.Kind {
		case "build", "start", "entry", "precondition":
		default:
			return fmt.Errorf("blocked claim %s has failed_precondition kind %q; must be one of build/start/entry/precondition", blocked.ClaimID, blocked.FailedPrecondition.Kind)
		}
		if strings.TrimSpace(blocked.FailedPrecondition.Detail) == "" {
			return fmt.Errorf("blocked claim %s has an empty failed_precondition detail; describe concretely why the confirmed Finding makes the Claim non-executable", blocked.ClaimID)
		}
		if len(blocked.EvidenceRefs) == 0 {
			return fmt.Errorf("blocked claim %s carries no evidence_refs; the projection must prove the precondition failure (L3-S7 §3.5)", blocked.ClaimID)
		}
		for _, ref := range blocked.EvidenceRefs {
			if strings.TrimSpace(ref) == "" {
				return fmt.Errorf("blocked claim %s contains an empty evidence reference", blocked.ClaimID)
			}
			if currentFindingIDs[ref] {
				finding := currentFindings[ref]
				linked := false
				for _, blockingID := range blocked.BlockingFindingIDs {
					if blockingID == ref {
						linked = true
						break
					}
				}
				if !linked {
					return fmt.Errorf("blocked claim %s uses current finding %s as evidence but it is not one of the blocking_finding_ids", blocked.ClaimID, ref)
				}
				if len(finding.EvidenceRefs) == 0 {
					return fmt.Errorf("blocked claim %s uses current finding %s as evidence, but that Finding has no evidence_refs", blocked.ClaimID, ref)
				}
				continue
			}
			entry, ok := confirmedEvidence[ref]
			if !ok {
				return fmt.Errorf("blocked claim %s references evidence %s which is not a valid current-round evidence entry", blocked.ClaimID, ref)
			}
			path, ok := entry["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				return fmt.Errorf("blocked claim %s evidence %s has no artifact path", blocked.ClaimID, ref)
			}
			sha, ok := entry["sha256"].(string)
			if !ok || len(sha) != 64 {
				return fmt.Errorf("blocked claim %s evidence %s has no artifact digest", blocked.ClaimID, ref)
			}
			artifactPath, err := repositoryContainedPath(root, path)
			if err != nil {
				return fmt.Errorf("blocked claim %s evidence %s path is outside repository: %w", blocked.ClaimID, ref, err)
			}
			data, err := os.ReadFile(artifactPath)
			if err != nil {
				return fmt.Errorf("blocked claim %s evidence %s artifact is unreadable: %w", blocked.ClaimID, ref, err)
			}
			if sha256Of(data) != sha {
				return fmt.Errorf("blocked claim %s evidence %s artifact digest drifted", blocked.ClaimID, ref)
			}
		}
		if !blocked.AfterRepairRequired {
			return fmt.Errorf("blocked claim %s must declare after_repair_required=true; the repaired round owes this Claim a real execution, blocked never satisfies it", blocked.ClaimID)
		}
	}
	return nil
}

// blockedByClaim indexes one result's blocked declarations by claim id.
func blockedByClaim(result *Result) map[string]BlockedClaim {
	out := make(map[string]BlockedClaim, len(result.BlockedClaims))
	for _, blocked := range result.BlockedClaims {
		out[blocked.ClaimID] = blocked
	}
	return out
}

// blockedClaimsOrEmpty keeps the persisted envelope field a stable array.
func blockedClaimsOrEmpty(result *Result) []BlockedClaim {
	if result.BlockedClaims == nil {
		return []BlockedClaim{}
	}
	return result.BlockedClaims
}

// applyBlockedDispositions writes the derived disposition rows. The claim row
// keeps the projection's identity binding (result_id + blocking finding_ids);
// the full source fields (failed_precondition, evidence_refs,
// after_repair_required) are preserved in the persisted result envelope and
// in the sealed ObservationBatch's claim_coverage_summary.blocked_claims.
func applyBlockedDispositions(claims map[string]any, result *Result) error {
	for _, blocked := range result.BlockedClaims {
		claimRow, _ := claims[blocked.ClaimID].(map[string]any)
		if claimRow == nil {
			return fmt.Errorf("claim %s is missing from the runtime projection", blocked.ClaimID)
		}
		if claimRow["applicability"] == "not_applicable" {
			return fmt.Errorf("claim %s is not_applicable in the plan; N/A is a plan disposition, never a blocked projection (L3-S7 §9.3)", blocked.ClaimID)
		}
		claimRow["disposition"] = "blocked"
		claimRow["result_id"] = result.ResultID
		ids := make([]any, 0, len(blocked.BlockingFindingIDs))
		for _, id := range blocked.BlockingFindingIDs {
			ids = append(ids, id)
		}
		claimRow["finding_ids"] = ids
	}
	return nil
}

// projectBlockedDispositions mirrors applyBlockedDispositions onto the
// pre-transaction projection the round consumer reasons over.
func projectBlockedDispositions(dispositions map[string]ClaimDisposition, result *Result) {
	for _, blocked := range result.BlockedClaims {
		disp := dispositions[blocked.ClaimID]
		disp.Disposition = "blocked"
		disp.ResultID = result.ResultID
		disp.FindingIDs = append([]string{}, blocked.BlockingFindingIDs...)
		dispositions[blocked.ClaimID] = disp
	}
}

// batchBlockedClaims lists every blocked Claim with its finding binding for
// the sealed ObservationBatch (L3-S7 §3.7 claim_coverage_summary). Details
// for claims blocked by earlier results are recovered from those results'
// persisted envelopes — the projection is a derived fact, never re-typed.
func batchBlockedClaims(view state_view, projected map[string]ClaimDisposition, result *Result) ([]any, error) {
	current := blockedByClaim(result)
	claimIDs := make([]string, 0, len(projected))
	for claimID, disp := range projected {
		if disp.Applicability != "not_applicable" && disp.Disposition == "blocked" {
			claimIDs = append(claimIDs, claimID)
		}
	}
	sort.Strings(claimIDs)
	out := make([]any, 0, len(claimIDs))
	for _, claimID := range claimIDs {
		disp := projected[claimID]
		blocked, ok := current[claimID]
		if !ok {
			loaded, err := loadBlockedClaimFromEnvelope(view.root, view.state, disp.ResultID, claimID)
			if err != nil {
				return nil, err
			}
			blocked = loaded
		}
		findingIDs := make([]any, 0, len(blocked.BlockingFindingIDs))
		for _, id := range blocked.BlockingFindingIDs {
			findingIDs = append(findingIDs, id)
		}
		evidenceRefs := make([]any, 0, len(blocked.EvidenceRefs))
		for _, ref := range blocked.EvidenceRefs {
			evidenceRefs = append(evidenceRefs, ref)
		}
		out = append(out, map[string]any{
			"claim_id":             claimID,
			"blocking_finding_ids": findingIDs,
			"failed_precondition": map[string]any{
				"kind":   blocked.FailedPrecondition.Kind,
				"detail": blocked.FailedPrecondition.Detail,
			},
			"evidence_refs":         evidenceRefs,
			"after_repair_required": true,
			"result_id":             disp.ResultID,
		})
	}
	return out, nil
}

// loadBlockedClaimFromEnvelope recovers one blocked projection from the
// persisted Canonical ReviewResult envelope that produced it.
func loadBlockedClaimFromEnvelope(root string, state map[string]any, resultID, claimID string) (BlockedClaim, error) {
	if resultID == "" {
		return BlockedClaim{}, fmt.Errorf("blocked claim %s has no producing result reference; the projection is out of sync — run `runtime reconcile`", claimID)
	}
	data, err := loadIndexedEvidenceArtifact(root, state, resultID, "review_result")
	if err != nil {
		return BlockedClaim{}, fmt.Errorf("blocked claim %s: %w", claimID, err)
	}
	var envelope struct {
		ReviewRound   int            `json:"review_round"`
		BlockedClaims []BlockedClaim `json:"blocked_claims"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return BlockedClaim{}, fmt.Errorf("blocked claim %s: decode producing result %s: %w", claimID, resultID, err)
	}
	if envelope.ReviewRound != currentReviewRound(state) {
		return BlockedClaim{}, fmt.Errorf("blocked claim %s: producing result %s belongs to review round %d, not current round %d", claimID, resultID, envelope.ReviewRound, currentReviewRound(state))
	}
	for _, blocked := range envelope.BlockedClaims {
		if blocked.ClaimID == claimID {
			return blocked, nil
		}
	}
	return BlockedClaim{}, fmt.Errorf("blocked claim %s: producing result %s carries no blocked projection for it; the projection is out of sync — run `runtime reconcile`", claimID, resultID)
}

// loadIndexedEvidenceArtifact is the single read path for persisted evidence
// references used by derived projections. The state index supplies the
// canonical relative path and digest; callers never join an untrusted path
// directly and never trust the index without re-hashing the bytes.
func loadIndexedEvidenceArtifact(root string, state map[string]any, evidenceID, expectedKind string) ([]byte, error) {
	if evidenceID == "" {
		return nil, fmt.Errorf("evidence id is empty")
	}
	for _, raw := range evidenceEntries(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || stringField(entry["id"]) != evidenceID {
			continue
		}
		if expectedKind != "" && stringField(entry["kind"]) != expectedKind {
			return nil, fmt.Errorf("evidence %s has kind %q, want %q", evidenceID, entry["kind"], expectedKind)
		}
		if stringField(entry["status"]) != "valid" {
			return nil, fmt.Errorf("evidence %s is not valid", evidenceID)
		}
		path := stringField(entry["path"])
		sha := stringField(entry["sha256"])
		if path == "" || len(sha) != 64 {
			return nil, fmt.Errorf("evidence %s has incomplete path/digest metadata", evidenceID)
		}
		artifactPath, err := repositoryContainedPath(root, path)
		if err != nil {
			return nil, fmt.Errorf("evidence %s path is outside repository: %w", evidenceID, err)
		}
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			return nil, fmt.Errorf("evidence %s artifact is unreadable: %w", evidenceID, err)
		}
		if sha256Of(data) != sha {
			return nil, fmt.Errorf("evidence %s artifact digest drifted", evidenceID)
		}
		return data, nil
	}
	return nil, fmt.Errorf("evidence %s is not in the evidence index", evidenceID)
}

// ---------------------------------------------------------------------------
// Finding site-lost BLOCKER reuse (L3-S7 §9.1 steps 12/14)
//
// An ordinary Finding whose encounter is not investigation-ready is first
// asked to complete the scene in place (the plain readiness rejection). When
// the Reviewer explicitly declares the scene unrecoverable (site_lost in the
// result), submit does not seal, does not fake ready and does not push the
// reproduction debt to S8: it records the existing Assignment BLOCKER
// semantics (reviewer Agent working -> blocked, work_blocked_ref bound to the
// declaring result) and keeps the round in S7. Recovery: fix the capture
// conditions, resolve the blocker, and the same finder resubmits a fresh
// ReviewResult. P0/security/data-destructive findings keep the existing
// capture_gaps + immediate-seal path and never use site_lost.
// ---------------------------------------------------------------------------

// ReadinessError marks investigation-readiness failures so the site-lost
// declaration path can recognize them.
type ReadinessError struct {
	FindingID string
	Severity  string
	Err       error
}

// SiteLostBlockedError reports a committed control-plane blocker whose
// ReviewResult was deliberately not consumed. It remains an error at the CLI
// boundary so the Agent must read and follow the recovery path, but callers
// such as metrics must distinguish it from a rejected submission.
type SiteLostBlockedError struct {
	Message string
}

func (e *SiteLostBlockedError) Error() string { return e.Message }

func (e *ReadinessError) Error() string { return e.Err.Error() }
func (e *ReadinessError) Unwrap() error { return e.Err }

// validateSiteLostDeclarations rejects spurious declarations: a declared
// finding must be part of this result, must be ordinary (P0 seals with
// capture_gaps), and must actually fail investigation readiness — declaring
// site_lost over a complete encounter is a contradiction.
func validateSiteLostDeclarations(result *Result) error {
	if len(result.SiteLost) == 0 {
		return nil
	}
	for _, finding := range result.Findings {
		if finding.Severity == "P0" {
			return fmt.Errorf("result contains P0 finding %s; a mixed result cannot use site_lost because safety-stop findings must take the immediate-seal path", finding.FindingID)
		}
	}
	findingByID := make(map[string]Finding, len(result.Findings))
	for _, finding := range result.Findings {
		findingByID[finding.FindingID] = finding
	}
	seen := map[string]bool{}
	for _, declaration := range result.SiteLost {
		if declaration.FindingID == "" || strings.TrimSpace(declaration.Reason) == "" {
			return fmt.Errorf("site_lost entries require a finding_id and a concrete reason")
		}
		if seen[declaration.FindingID] {
			return fmt.Errorf("site_lost declares finding %s twice", declaration.FindingID)
		}
		seen[declaration.FindingID] = true
		finding, ok := findingByID[declaration.FindingID]
		if !ok {
			return fmt.Errorf("site_lost declares finding %s which is not part of this result", declaration.FindingID)
		}
		if finding.Severity == "P0" {
			return fmt.Errorf("finding %s is P0; safety-stop findings seal immediately with explicit capture_gaps and never take the site_lost path (L3-S7 §9.1 step 12)", declaration.FindingID)
		}
		if err := validateInvestigationReadiness(finding); err == nil {
			return fmt.Errorf("finding %s is declared site_lost but its encounter is already investigation-ready; remove the declaration and submit normally", declaration.FindingID)
		}
	}
	return nil
}

// submitSiteLostBlocker runs when an ordinary Finding fails investigation
// readiness and the Reviewer declared its scene unrecoverable. It commits one
// CAS that records the Assignment BLOCKER (no result consumption, no finding
// registration, no seal) and returns the rejection naming the recovery
// action.
func submitSiteLostBlocker(
	root, statePath, journalPath string,
	request SubmitRequest,
	current map[string]any,
	assignment *PlanAssignment,
	result *Result,
	readiness *ReadinessError,
) (loopruntime.Snapshot, error) {
	if err := validateSiteLostDeclarations(result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	declared := map[string]string{}
	for _, declaration := range result.SiteLost {
		declared[declaration.FindingID] = declaration.Reason
	}
	reason, ok := declared[readiness.FindingID]
	if !ok {
		// No declaration for the failing finding: keep the current plain
		// rejection demanding in-place scene completion.
		return loopruntime.Snapshot{}, readiness.Err
	}

	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	generation := baselineGeneration(current)
	blockerID := fmt.Sprintf("review-blocker-%s", result.ResultID)
	blockerRel := filepath.ToSlash(filepath.Join(
		".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
		"review-blockers", result.ResultID+"-site-lost.json"))
	blockerEnvelope := map[string]any{
		"schema_version":      "1.0.0",
		"evidence_id":         blockerID,
		"kind":                "review_blocker",
		"runtime_id":          runtimeID,
		"baseline_generation": generation,
		"review_round":        result.ReviewRound,
		"assignment_id":       result.AssignmentID,
		"producer_agent_id":   result.ProducerAgentID,
		"finding_id":          readiness.FindingID,
		"reason":              reason,
		"created_at":          occurredAt.UTC().Format(time.RFC3339Nano),
	}
	blockerBytes, err := marshalArtifact(blockerEnvelope)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("encode site-lost blocker: %w", err)
	}
	if err := writeArtifact(root, blockerRel, blockerBytes); err != nil {
		return loopruntime.Snapshot{}, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// If the CAS error was returned after a commit became visible, keep
		// the immutable bytes so the evidence index cannot be orphaned. Only
		// clean an artifact when the caller's revision is still persisted.
		stateBytes, readErr := os.ReadFile(statePath)
		if readErr != nil {
			return
		}
		var persisted map[string]any
		if json.Unmarshal(stateBytes, &persisted) != nil || intField(persisted["revision"]) != currentCommitRevision(request.ExpectedRevision, current) {
			return
		}
		if path, pathErr := repositoryContainedPath(root, blockerRel); pathErr == nil {
			_ = os.Remove(path)
		}
	}()
	blockerSHA := sha256Of(blockerBytes)

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	commitRevision := currentCommitRevision(request.ExpectedRevision, current)
	snapshot, err := updateRuntime(store, request.ExpectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-review-site-lost-%s-r%d", result.ResultID, commitRevision+1),
		TransitionID:   "REVIEW-RESULT",
		Event:          "review_assignment_blocked",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:review-site-lost:%s:%d", result.ResultID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		Message: fmt.Sprintf("Assignment %s blocked in S7: finding %s scene unrecoverable (%s); the result is not consumed and the round is not sealed",
			result.AssignmentID, readiness.FindingID, reason),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			reviewMap, ok := state["review"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime review section must be an object")
			}
			assignments, _ := reviewMap["assignments"].(map[string]any)
			row, _ := assignments[assignment.AssignmentID].(map[string]any)
			if row == nil {
				return fmt.Errorf("assignment %s is not registered in the runtime projection; dispatch it via `runtime register-workgroup` first", assignment.AssignmentID)
			}
			if status, _ := row["status"].(string); status == "result_submitted" || status == "consumed" {
				return fmt.Errorf("assignment %s already has a consumed ReviewResult; one Assignment submits exactly one Result (L3-S7 §3.5)", assignment.AssignmentID)
			}
			agentID, _ := row["agent_id"].(string)
			if agentID == "" {
				return fmt.Errorf("assignment %s has no dispatched Agent; dispatch it via `runtime register-workgroup` before declaring site_lost", assignment.AssignmentID)
			}
			if agentID != result.ProducerAgentID {
				return fmt.Errorf("ReviewResult producer %s does not match the dispatched Agent %s for assignment %s", result.ProducerAgentID, agentID, assignment.AssignmentID)
			}
			row["status"] = "blocked"
			row["queue_reason"] = "site_lost:" + readiness.FindingID
			row["blocker_ref"] = blockerRel
			row["blocked_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			entities, _ := state["entities"].(map[string]any)
			agents, _ := entities["agents"].([]any)
			for _, raw := range agents {
				agent, _ := raw.(map[string]any)
				if agent["id"] != agentID {
					continue
				}
				if currentState, _ := agent["state"].(string); currentState != "working" {
					return fmt.Errorf("reviewer Agent %s is %s; a site_lost BLOCKER requires working state", agentID, currentState)
				}
				agent["state"] = "blocked"
				agent["work_blocked_ref"] = blockerRel
				agent["blocker_resolved_ref"] = nil
				agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				if err := appendEvidence(state, map[string]any{
					"id": blockerID, "kind": "review_blocker", "path": blockerRel,
					"sha256": blockerSHA, "status": "valid", "baseline_generation": generation,
					"review_round": result.ReviewRound, "produced_by": []any{result.ProducerAgentID},
					"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
					"responsibility_id": LensToResponsibility(assignment.Lens), "scope_refs": []any{assignment.AssignmentID},
				}); err != nil {
					return err
				}
				return nil
			}
			return fmt.Errorf("Agent %s is not registered", agentID)
		},
	})
	if err != nil {
		return snapshot, err
	}
	committed = true
	return snapshot, &SiteLostBlockedError{Message: fmt.Sprintf(
		"finding %s is not investigation-ready (%v) and the reviewer declared the scene unrecoverable: %s. "+
			"Assignment %s is now blocked and stays in S7 — the result was NOT consumed, no Finding was registered and nothing was sealed (L3-S7 §9.1). "+
			"Recovery: fix the capture conditions (capture buffer, read-only state re-capture, existing logs), record blocker_resolved for Agent %s, and have the same finder resubmit a fresh ReviewResult; an authorized human decides whether a safe rebuild is possible. The reproduction debt is never handed to S8",
		readiness.FindingID, readiness.Err, reason, result.AssignmentID, result.ProducerAgentID)}
}

// asReadinessError unwraps a validation failure into a ReadinessError.
func asReadinessError(err error) *ReadinessError {
	var readiness *ReadinessError
	if errors.As(err, &readiness) {
		return readiness
	}
	return nil
}
