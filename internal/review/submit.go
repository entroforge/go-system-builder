package review

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// SubmitRequest drives `runtime review-result submit`.
type SubmitRequest struct {
	ExpectedRevision int
	AssignmentID     string
	ResultPath       string
	// CaptureDir optionally points at a captures directory; findings with an
	// empty encounter timeline absorb the buffered steps (L3-S7 §3.6).
	CaptureDir string
	OccurredAt time.Time
}

// SubmitResult is the single entry point for a Canonical ReviewResult
// (L3-S7 §9.1). It wraps submitResult with the first-pass success metric
// (§14.2): stale-revision CAS conflicts are concurrency retries, not submit
// friction, so only real validation/consumption failures count as rejected.
func SubmitResult(
	root, statePath, journalPath string,
	request SubmitRequest,
) (loopruntime.Snapshot, error) {
	snapshot, err := submitResult(root, statePath, journalPath, request)
	var siteLostBlocked *SiteLostBlockedError
	switch {
	case err == nil:
		_ = metrics.RecordS7ResultSubmit(root, "accepted")
	case errors.Is(err, loopruntime.ErrStaleRevision):
	case errors.As(err, &siteLostBlocked):
		_ = metrics.RecordS7ResultSubmit(root, "blocked")
	default:
		_ = metrics.RecordS7ResultSubmit(root, "rejected")
	}
	return snapshot, err
}

// submitResult consumes one Canonical ReviewResult in one CAS transaction:
//
//  1. validates the result against review-plan coordinates, the Assignment's
//     exact Claim set, producer identity and Builder/Reviewer independence;
//  2. persists the result envelope plus one immutable Finding per fail Claim;
//  3. advances the reviewer Agent (working -> reported) and marks the
//     Assignment consumed;
//  4. updates the claim disposition projection; a finding flips the round to
//     cannot_clean (critical P0 seals immediately; ordinary findings drain);
//  5. when the final required Claim disposition lands, the round consumer
//     runs in the same transaction: findings -> sealed ObservationBatch,
//     no findings -> machine CleanRound;
//  6. pause verdicts (req_change_required / release_blocked) create the one
//     authoritative pause checkpoint here; TR-010/TR-011 only move the cursor.
func submitResult(
	root, statePath, journalPath string,
	request SubmitRequest,
) (loopruntime.Snapshot, error) {
	if request.AssignmentID == "" || request.ResultPath == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("--assignment-id and --result are required")
	}
	data, err := os.ReadFile(request.ResultPath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read ReviewResult: %w", err)
	}
	// verdict=fail is a common authoring mistake (Reviewers try to record a
	// per-Claim "fail" verdict on the result envelope). The schema's enum
	// rejection is accurate but uninformative; surface the actionable
	// verdict=finding path before the schema validator buries the error
	// under "value must be one of …" (L3-S7 §3.5).
	if hint, ok := verdictHint(data); ok {
		return loopruntime.Snapshot{}, hint
	}
	if err := schema.NewValidator(root).ValidateBytes("review-result.schema.json", data); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult schema: %w", err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode ReviewResult: %w", err)
	}
	if result.AssignmentID != request.AssignmentID {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult assignment_id %s does not match --assignment-id %s", result.AssignmentID, request.AssignmentID)
	}
	// Capture-buffer merge: findings whose encounter timeline is empty absorb
	// the buffered steps; reviewer-written timelines are never rewritten. A
	// multi-Finding result must correlate each buffered step so S8 never gets an
	// assignment-wide timeline attached to the wrong Finding.
	if request.CaptureDir != "" {
		steps, captureErr := LoadCaptureStepsStrict(request.CaptureDir)
		if captureErr != nil {
			return loopruntime.Snapshot{}, s7GateError(
				"S7_CAPTURE_INVALID",
				"capture buffer cannot be consumed",
				[]string{captureErr.Error()},
				[]string{"repair the malformed capture line or provide a fresh correlated capture buffer"},
				"runtime review-result submit --assignment-id "+request.AssignmentID+" --result "+request.ResultPath,
			)
		}
		if err := mergeCapturedTimelineChecked(result.Findings, steps); err != nil {
			return loopruntime.Snapshot{}, s7GateError(
				"S7_CAPTURE_CORRELATION",
				"capture timeline cannot be assigned unambiguously",
				[]string{err.Error()},
				[]string{"add finding_id or claim_id to each capture step, or submit reviewer-authored timelines inline"},
				"runtime review-result submit --assignment-id "+request.AssignmentID+" --result "+request.ResultPath,
			)
		}
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	// Reject a stale caller before producing any review artifacts. The CAS
	// remains authoritative for races, but this early check prevents the
	// common stale-submit path from leaving orphan evidence on disk.
	if request.ExpectedRevision >= 0 && intField(current["revision"]) != request.ExpectedRevision {
		return loopruntime.Snapshot{}, loopruntime.ErrStaleRevision
	}
	commitRevision := currentCommitRevision(request.ExpectedRevision, current)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	if state, _ := lifecycle["state"].(string); state != "verification" {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResults can only be submitted in the verification stage (current state: %s)", lifecycle["state"])
	}
	plan, ptr, err := LoadPlan(root, current)
	if err != nil {
		if PlanPointerFromState(current) != nil {
			return staleReviewPlanAfterDrift(root, statePath, journalPath, current, err)
		}
		return loopruntime.Snapshot{}, err
	}
	// A regression_available plan reuses an existing CASE/PATH asset only
	// while the declared path and fingerprint still match disk. Registration
	// checks this once, but submit must check again because a test asset can
	// drift while reviewers are working. This is verification-artifact drift,
	// not a product-baseline drift: keep the runtime unchanged and return the
	// repair path so the Planner can refresh the asset or choose cold_start.
	if plan.E2ECoverageState == "regression_available" {
		if err := verifyRegressionAssetFingerprints(root, plan); err != nil {
			return loopruntime.Snapshot{}, err
		}
	}
	if err := verifyFrozenSubjects(root, plan); err != nil {
		return staleReviewPlanAfterDrift(root, statePath, journalPath, current, fmt.Errorf("ReviewPlan frozen subject baseline: %w", err))
	}
	round := currentReviewRound(current)
	generation := baselineGeneration(current)
	switch ptr.Status {
	case "running", "cannot_clean", "discovery_draining":
	default:
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan %s is %s; results are only accepted while the round is running or draining", ptr.PlanID, ptr.Status)
	}
	if result.ReviewPlanID != plan.ReviewPlanID {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult binds plan %s but the registered plan is %s", result.ReviewPlanID, plan.ReviewPlanID)
	}
	if result.ReviewRound != round {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult declares review_round %d but the runtime is at round %d", result.ReviewRound, round)
	}
	if result.BaselineGeneration != generation {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult declares baseline_generation %d but the runtime is at generation %d", result.BaselineGeneration, generation)
	}
	if result.AssignmentRevision != ptr.Revision {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult declares assignment_revision %d but the registered ReviewPlan is at assignment_revision %d; re-read the current plan before submitting", result.AssignmentRevision, ptr.Revision)
	}
	if digest := SubjectDigest(plan); result.SubjectDigest != digest {
		return loopruntime.Snapshot{}, fmt.Errorf("subject_digest mismatch: the result binds %s but the frozen baseline digests to %s — copy the expected value from `loop-harness s7 status` (subject_digest line); a mismatch after copying means the frozen baseline drifted and the round is stale, not submittable", result.SubjectDigest, digest)
	}

	assignment := findPlanAssignment(plan, result.AssignmentID)
	if assignment == nil {
		return loopruntime.Snapshot{}, fmt.Errorf("assignment %s is not part of ReviewPlan %s (known: %s)", result.AssignmentID, plan.ReviewPlanID, strings.Join(planAssignmentIDs(plan), ", "))
	}
	if err := validateClaimResultSet(assignment, &result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateClaimEvidenceRequirements(plan, assignment, &result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateResultEvidenceReferences(root, current, &result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateVerdictConsistency(&result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateFindings(plan, assignment, &result); err != nil {
		// Site-lost path (L3-S7 §9.1 step 12): an ordinary Finding whose
		// encounter cannot be completed AND is declared unrecoverable records
		// an Assignment BLOCKER that stays in S7 instead of a bare rejection.
		if readiness := asReadinessError(err); readiness != nil && len(result.SiteLost) > 0 {
			return submitSiteLostBlocker(root, statePath, journalPath, request, current, assignment, &result, readiness)
		}
		return loopruntime.Snapshot{}, err
	}
	if err := validateSiteLostDeclarations(&result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateBlockedClaims(root, current, plan, assignment, &result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateProducerIndependence(current, &result); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := verifyResultArtifactDigest(root, plan, ptr, &result, assignment.Lens); err != nil {
		// A digest mismatch is a recoverable authoring state, not round
		// drift: the workspace itself is intact and the seal-time
		// verification (verifySealedArtifactDigests) independently guards
		// post-consumption workspace drift. Marking the plan stale here
		// would deadlock an otherwise viable round behind one stale digest
		// (verified live in the S7 round-4 sandbox review), so this is a
		// plain rejection with the recovery path. Only frozen-subject
		// drift stales the plan.
		return loopruntime.Snapshot{}, fmt.Errorf("%w; recompute with `loop-harness s7 workspace-digest`, re-run the flows if the spec/fixture changed, then resubmit", err)
	}

	runtimeID, _ := current["runtime_id"].(string)
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	lens := assignment.Lens
	responsibility := LensToResponsibility(lens)

	// Persist artifacts before the CAS (same pattern as the S6 Builder
	// Result): bytes on disk are what the evidence index fingerprints.
	artifactRels := []string{}
	casAttempted := false
	cleanupArtifacts := func() {
		for _, rel := range artifactRels {
			if path, err := repositoryContainedPath(root, rel); err == nil {
				_ = os.Remove(path)
			}
		}
	}
	// Validation/build failures happen before the state CAS and must not leave
	// evidence that the runtime never indexed. A stale CAS is the one race we
	// can identify safely after attempting the write, so it receives the same
	// cleanup. For an unknown CAS error keep the immutable bytes: the writer
	// may have committed state before returning the error and deleting them
	// would make the evidence index unverifiable.
	defer func() {
		if !casAttempted {
			cleanupArtifacts()
		}
	}()
	resultRel := filepath.ToSlash(filepath.Join(
		".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
		"reviews", result.ProducerAgentID, result.ResultID+".json"))
	resultEnvelope := map[string]any{
		"schema_version":               "1.0.0",
		"evidence_id":                  result.ResultID,
		"kind":                         "review_result",
		"runtime_id":                   runtimeID,
		"baseline_generation":          generation,
		"review_round":                 round,
		"producer_agent_id":            result.ProducerAgentID,
		"producer_responsibility":      responsibility,
		"subject_refs":                 []any{},
		"conclusion":                   result.Verdict,
		"review_plan_id":               plan.ReviewPlanID,
		"assignment_id":                result.AssignmentID,
		"assignment_revision":          result.AssignmentRevision,
		"subject_digest":               result.SubjectDigest,
		"verification_artifact_digest": result.VerificationArtifactDigest,
		"claim_results":                result.ClaimResults,
		"blocked_claims":               blockedClaimsOrEmpty(&result),
		"checks":                       result.Checks,
		"deviations":                   result.Deviations,
		"verdict":                      result.Verdict,
		"created_at":                   occurredAt.UTC().Format(time.RFC3339Nano),
	}
	resultBytes, err := marshalArtifact(resultEnvelope)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("encode review result envelope: %w", err)
	}
	if err := writeArtifact(root, resultRel, resultBytes); err != nil {
		return loopruntime.Snapshot{}, err
	}
	artifactRels = append(artifactRels, resultRel)
	resultSHA := sha256Of(resultBytes)

	findingArtifacts := make([]findingArtifact, 0, len(result.Findings))
	for _, finding := range result.Findings {
		rel := filepath.ToSlash(filepath.Join(
			".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
			"findings", finding.FindingID+".json"))
		bytes, err := marshalArtifact(finding)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("encode finding %s: %w", finding.FindingID, err)
		}
		if err := writeArtifact(root, rel, bytes); err != nil {
			return loopruntime.Snapshot{}, err
		}
		artifactRels = append(artifactRels, rel)
		findingArtifacts = append(findingArtifacts, findingArtifact{finding: finding, rel: rel, sha: sha256Of(bytes)})
	}

	// The round consumer runs in the same transaction: project the final
	// dispositions with this result applied, then decide seal / clean.
	projected := projectDispositions(current, assignment, &result)
	complete := roundCompleteWith(projected)
	var batchRel, batchSHA string
	var batchID string
	var cleanRel, cleanSHA string
	sealNow := complete && len(RoundFindings(current))+len(result.Findings) > 0
	if !sealNow {
		for _, f := range result.Findings {
			if f.Severity == "P0" {
				// Critical findings stop the line: seal immediately with the
				// unobserved Claims made explicit (L3-S7 §3.7, §5.2).
				sealNow = true
				break
			}
		}
	}
	cleanNow := complete && !sealNow && len(RoundFindings(current)) == 0 && len(result.Findings) == 0

	if (sealNow || cleanNow) && ptr.VerificationArtifactWorkspace != "" {
		if err := verifySealedArtifactDigests(root, ptr, projectedAssignments(current, assignment, &result)); err != nil {
			return staleReviewPlanAfterDrift(root, statePath, journalPath, current, err)
		}
	}
	if sealNow {
		batchID = fmt.Sprintf("observation-batch-r%d", round)
		batch, err := buildObservationBatch(state_view{state: current, root: root}, plan, ptr, projected, &result, complete, occurredAt)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		batchRel = filepath.ToSlash(filepath.Join(
			".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
			"review", batchID+".json"))
		batchBytes, err := marshalArtifact(batch)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("encode ObservationBatch: %w", err)
		}
		if err := schema.NewValidator(root).ValidateBytes("observation-batch.schema.json", batchBytes); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("ObservationBatch schema: %w", err)
		}
		if err := writeArtifact(root, batchRel, batchBytes); err != nil {
			return loopruntime.Snapshot{}, err
		}
		artifactRels = append(artifactRels, batchRel)
		batchSHA = sha256Of(batchBytes)
	}
	if cleanNow {
		cleanRel = filepath.ToSlash(filepath.Join(
			".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
			"review", fmt.Sprintf("clean-round-r%d.json", round)))
		snapshot := buildCleanRoundSnapshot(current, plan, resultEnvelope, resultSHA, resultRel, occurredAt)
		cleanBytes, err := marshalArtifact(snapshot)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("encode CleanRound: %w", err)
		}
		if err := writeArtifact(root, cleanRel, cleanBytes); err != nil {
			return loopruntime.Snapshot{}, err
		}
		artifactRels = append(artifactRels, cleanRel)
		cleanSHA = sha256Of(cleanBytes)
	}

	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	resultRepoPath := repositoryPath(root, request.ResultPath)

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	casAttempted = true
	snapshot, err := updateRuntime(store, request.ExpectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-review-result-%s-r%d", result.ResultID, commitRevision+1),
		TransitionID:   "REVIEW-RESULT",
		Event:          "review_result_submitted",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:review-result:%s:%d", result.ResultID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		EvidenceIDs:    append([]string{result.ResultID}, findingIDs(result.Findings)...),
		Message: fmt.Sprintf("Consumed Canonical ReviewResult %s for assignment %s (verdict %s)",
			result.ResultID, result.AssignmentID, result.Verdict),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			// RC-10 observability: per-phase durations of the submit CAS
			// transaction (loop_s7_submit_phase_ms{phase}). Best-effort — the
			// `_ =` discard mirrors recordRoundMetrics and never fails the
			// verb. phaseTotal timers cover the whole closure; seal/clean are
			// recorded by recordRoundMetrics consumers below, not here.
			applyStart := time.Now()
			if err := applyResultConsumption(state, plan, assignment, &result, resultRel, resultSHA, responsibility, generation, round, occurredAt); err != nil {
				return err
			}
			_ = metrics.RecordS7SubmitPhase(root, "result_consumption", time.Since(applyStart).Milliseconds())
			findingsStart := time.Now()
			if err := applyFindings(state, &result, findingArtifacts, round, occurredAt); err != nil {
				return err
			}
			_ = metrics.RecordS7SubmitPhase(root, "findings", time.Since(findingsStart).Milliseconds())
			// A stop verdict and both terminal round transitions close the
			// admission gate before this CAS returns. Promoting a queued
			// Assignment here would dispatch new work after P0/pause/seal.
			advanceStart := time.Now()
			if !sealNow && !cleanNow && result.Verdict != "req_change_required" && result.Verdict != "release_blocked" {
				if err := releaseQueuedReviewAssignments(state); err != nil {
					return err
				}
			}
			if err := advanceReviewerAgent(state, &result, resultRepoPath, occurredAt); err != nil {
				return err
			}
			_ = metrics.RecordS7SubmitPhase(root, "advance", time.Since(advanceStart).Milliseconds())
			reviewMap := state["review"].(map[string]any)
			lifecycleMap := state["lifecycle"].(map[string]any)
			switch result.Verdict {
			case "req_change_required", "release_blocked":
				// One authoritative checkpoint in the verdict transaction;
				// TR-010/TR-011 then carry only the result evidence
				// (L3-S7 §9.2, §13.1 dual-carrier removal).
				pauseStart := time.Now()
				if err := capturePauseCheckpoint(state, "S7 review verdict: "+result.Verdict, occurredAt); err != nil {
					return err
				}
				_ = metrics.RecordS7SubmitPhase(root, "pause", time.Since(pauseStart).Milliseconds())
				setPlanStatus(reviewMap, lifecycleMap, "paused")
				return nil
			}
			if len(result.Findings) > 0 {
				if status, _ := reviewMap["plan"].(map[string]any)["status"].(string); status == "running" {
					setPlanStatus(reviewMap, lifecycleMap, "cannot_clean")
				} else if status == "cannot_clean" && !sealNow {
					setPlanStatus(reviewMap, lifecycleMap, "discovery_draining")
				}
			}
			if sealNow {
				sealStart := time.Now()
				if err := applyObservationBatch(state, batchID, batchRel, batchSHA, result.Findings, round, occurredAt); err != nil {
					return err
				}
				_ = metrics.RecordS7SubmitPhase(root, "seal", time.Since(sealStart).Milliseconds())
				setPlanStatus(reviewMap, lifecycleMap, "observation_sealed")
				return nil
			}
			if cleanNow {
				cleanStart := time.Now()
				if err := applyCleanRound(state, cleanRel, cleanSHA, round, occurredAt); err != nil {
					return err
				}
				_ = metrics.RecordS7SubmitPhase(root, "clean", time.Since(cleanStart).Milliseconds())
				setPlanStatus(reviewMap, lifecycleMap, "clean")
				reviewMap["clean_round"] = round
				return nil
			}
			return nil
		},
	})
	if err != nil {
		if errors.Is(err, loopruntime.ErrStaleRevision) {
			cleanupArtifacts()
			return snapshot, err
		}
		// An Apply-time rejection (e.g. reviewer Agent not in working state)
		// fails the CAS without committing. When the persisted revision is
		// still the caller's expected revision the transaction definitively
		// did not land, so the staged artifacts are orphans that would block
		// the corrected resubmit with "file exists" — remove them. Only an
		// advanced or unreadable revision keeps the bytes (the commit may
		// have landed before the error surfaced).
		if after, readErr := os.ReadFile(statePath); readErr == nil {
			var post map[string]any
			if json.Unmarshal(after, &post) == nil && intField(post["revision"]) == commitRevision {
				cleanupArtifacts()
			}
		}
		return snapshot, err
	}
	recordRoundMetrics(root, current, plan, ptr, &result, round, occurredAt, sealNow, cleanNow)
	return snapshot, nil
}

// recordRoundMetrics captures the L3-S7 §14.2 machine-collectible facts of
// one consumed result: round shape gauges, per-Claim planned -> dispositioned
// lead time, finding count, first-finding -> seal duration and clean rounds.
// Metrics are best-effort observability and never fail the verb.
func recordRoundMetrics(root string, state map[string]any, plan *Plan, ptr *PlanPointer, result *Result, round int, occurredAt time.Time, sealed, clean bool) {
	_ = metrics.RecordS7RoundShape(root, round, len(plan.Assignments), len(plan.Claims), ptr.Revision)
	if plannedAt, err := time.Parse(time.RFC3339Nano, ptr.SubmittedAt); err == nil {
		for _, claimResult := range result.ClaimResults {
			_ = metrics.RecordS7ClaimLeadTime(root, round, claimResult.ClaimID, occurredAt.Sub(plannedAt).Milliseconds())
		}
	}
	_ = metrics.RecordS7Findings(root, round, len(result.Findings))
	if sealed {
		_ = metrics.RecordS7FirstFindingToSeal(root, round, occurredAt.Sub(firstFindingAt(state, occurredAt)).Milliseconds())
	}
	if clean {
		_ = metrics.RecordS7CleanRound(root, round)
	}
}

// firstFindingAt returns the earliest finding creation time in the round.
// Findings from the just-consumed result are created at occurredAt, which
// bounds the scan from above.
func firstFindingAt(state map[string]any, occurredAt time.Time) time.Time {
	earliest := occurredAt
	for _, row := range RoundFindings(state) {
		created, err := time.Parse(time.RFC3339Nano, stringField(row["created_at"]))
		if err == nil && created.Before(earliest) {
			earliest = created
		}
	}
	return earliest
}

// ---------------------------------------------------------------------------
// validation helpers
// ---------------------------------------------------------------------------

func findPlanAssignment(plan *Plan, assignmentID string) *PlanAssignment {
	for i := range plan.Assignments {
		if plan.Assignments[i].AssignmentID == assignmentID {
			return &plan.Assignments[i]
		}
	}
	return nil
}

func planAssignmentIDs(plan *Plan) []string {
	ids := make([]string, 0, len(plan.Assignments))
	for _, assignment := range plan.Assignments {
		ids = append(ids, assignment.AssignmentID)
	}
	sort.Strings(ids)
	return ids
}

// validateClaimResultSet proves claim_results + blocked_claims == the
// Assignment's exact Claim set: every Claim is answered exactly once, either
// by a pass/fail conclusion or by a blocked_by_confirmed_finding declaration;
// no missing, no extras, no duplicates (L3-S7 §3.5).
func validateClaimResultSet(assignment *PlanAssignment, result *Result) error {
	want := map[string]bool{}
	for _, claimID := range assignment.ClaimIDs {
		want[claimID] = true
	}
	seen := map[string]bool{}
	for _, claimResult := range result.ClaimResults {
		if !want[claimResult.ClaimID] {
			return fmt.Errorf("claim_results contains %s which is not part of assignment %s; a Reviewer never adds Claims (L3-S7 §3.5)", claimResult.ClaimID, assignment.AssignmentID)
		}
		if seen[claimResult.ClaimID] {
			return fmt.Errorf("claim_results contains %s twice", claimResult.ClaimID)
		}
		seen[claimResult.ClaimID] = true
	}
	for _, blocked := range result.BlockedClaims {
		if !want[blocked.ClaimID] {
			return fmt.Errorf("blocked_claims contains %s which is not part of assignment %s; a Reviewer never adds Claims (L3-S7 §3.5)", blocked.ClaimID, assignment.AssignmentID)
		}
		if seen[blocked.ClaimID] {
			return fmt.Errorf("claim %s is answered by both claim_results and blocked_claims; a Claim gets exactly one disposition", blocked.ClaimID)
		}
		seen[blocked.ClaimID] = true
	}
	for claimID := range want {
		if !seen[claimID] {
			return fmt.Errorf("claim_results is missing %s; the Assignment's Claim set must be answered exactly (pass/fail or a blocked_by_confirmed_finding declaration, L3-S7 §3.5)", claimID)
		}
	}
	return nil
}

// validateClaimEvidenceRequirements turns a Claim's declared minimum
// evidence into a submit-time gate. The Claim owns the minimum; the
// ReviewResult owns the concrete evidence refs. Every known requirement is
// matched by its explicit `<kind>:<id>` prefix so a path or arbitrary symbol
// cannot satisfy a console/network/timeline requirement by accident.
func validateClaimEvidenceRequirements(plan *Plan, assignment *PlanAssignment, result *Result) error {
	claims := make(map[string]Claim, len(plan.Claims))
	for _, claim := range plan.Claims {
		claims[claim.ClaimID] = claim
	}
	for _, claimResult := range result.ClaimResults {
		claim := claims[claimResult.ClaimID]
		if len(claim.RequiredEvidence) == 0 {
			continue
		}
		if len(claimResult.EvidenceRefs) == 0 {
			return s7GateError(
				"S7_RESULT_EVIDENCE_MISSING",
				fmt.Sprintf("claim %s in assignment %s supplied no evidence_refs", claimResult.ClaimID, assignment.AssignmentID),
				[]string{"required evidence: " + strings.Join(claim.RequiredEvidence, ", ")},
				[]string{"add one typed evidence reference for every required kind to the ClaimResult"},
				"runtime review-result submit --assignment-id "+assignment.AssignmentID+" --result <result.json>",
			)
		}
		for _, requirement := range claim.RequiredEvidence {
			matched := false
			for _, ref := range claimResult.EvidenceRefs {
				if evidenceRefMatchesRequirement(ref, requirement) {
					matched = true
					break
				}
			}
			if !matched {
				return s7GateError(
					"S7_RESULT_EVIDENCE_TYPE",
					fmt.Sprintf("claim %s in assignment %s is missing typed %s evidence", claimResult.ClaimID, assignment.AssignmentID, requirement),
					[]string{fmt.Sprintf("required evidence kind: %s", requirement), "received: " + strings.Join(claimResult.EvidenceRefs, ", ")},
					[]string{fmt.Sprintf("add a %s:<id> reference to this ClaimResult; use the capture wrapper or registered Runtime evidence", requirement)},
					"runtime review-result submit --assignment-id "+assignment.AssignmentID+" --result <result.json>",
				)
			}
		}
	}
	return nil
}

// validateResultEvidenceReferences validates references whose syntax declares
// a local immutable artifact (`path:<repo-relative-path>#sha256=<64 hex>`) or an indexed runtime
// Evidence row. Bare refs remain symbolic/external refs for compatibility
// with browser traces and platform-provided evidence IDs; the explicit path
// prefix is the low-complexity contract that makes local evidence auditable.
func validateResultEvidenceReferences(root string, state map[string]any, result *Result) error {
	for _, claimResult := range result.ClaimResults {
		if err := validateEvidenceRefs(root, state, claimResult.EvidenceRefs, fmt.Sprintf("claim %s", claimResult.ClaimID)); err != nil {
			return err
		}
	}
	for _, check := range result.Checks {
		if err := validateEvidenceRefs(root, state, check.EvidenceRefs, fmt.Sprintf("check %s", check.Name)); err != nil {
			return err
		}
	}
	for _, finding := range result.Findings {
		if err := validateEvidenceRefs(root, state, finding.EvidenceRefs, fmt.Sprintf("finding %s", finding.FindingID)); err != nil {
			return err
		}
		for _, step := range finding.Encounter.Timeline {
			if err := validateEvidenceRefs(root, state, step.EvidenceRefs, fmt.Sprintf("finding %s timeline step %d", finding.FindingID, step.Sequence)); err != nil {
				return err
			}
		}
	}
	for _, blocked := range result.BlockedClaims {
		if err := validateEvidenceRefs(root, state, blocked.EvidenceRefs, fmt.Sprintf("blocked claim %s", blocked.ClaimID)); err != nil {
			return err
		}
	}
	return nil
}

func validateEvidenceRefs(root string, state map[string]any, refs []string, owner string) error {
	indexed := map[string]map[string]any{}
	if evidence, ok := state["evidence"].([]any); ok {
		for _, raw := range evidence {
			row, _ := raw.(map[string]any)
			if row != nil && stringField(row["id"]) != "" {
				indexed[stringField(row["id"])] = row
			}
		}
	}
	for _, rawRef := range refs {
		ref := strings.TrimSpace(rawRef)
		if ref == "" {
			return fmt.Errorf("%s contains an empty evidence reference", owner)
		}
		if strings.HasPrefix(ref, "path:") {
			rel, wantDigest, err := parsePathEvidenceRef(ref)
			if err != nil {
				return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
			}
			if wantDigest == "" {
				return s7GateError(
					"S7_RESULT_EVIDENCE_REF",
					fmt.Sprintf("%s evidence reference %q has no sha256 digest", owner, ref),
					[]string{"a local path is mutable and cannot identify immutable evidence without a content digest"},
					[]string{"append #sha256=<64 hex> or register the artifact as Runtime evidence and use runtime:<evidence-id>"},
					"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
				)
			}
			if rel == "" {
				return fmt.Errorf("%s contains an empty evidence reference path", owner)
			}
			path, err := repositoryContainedPath(root, rel)
			if err != nil {
				return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("%s evidence reference %q does not exist: %w", owner, ref, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%s evidence reference %q is not a regular file", owner, ref)
			}
			if wantDigest != "" {
				data, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("%s evidence reference %q cannot be read: %w", owner, ref, err)
				}
				if got := sha256Of(data); got != wantDigest {
					return s7GateError(
						"S7_RESULT_EVIDENCE_REF",
						fmt.Sprintf("%s evidence reference %q has a digest mismatch", owner, ref),
						[]string{fmt.Sprintf("got %s, want %s", got, wantDigest)},
						[]string{"refresh the #sha256 suffix or register the artifact as Runtime evidence and use runtime:<evidence-id>"},
						"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
					)
				}
			}
			continue
		}
		if strings.HasPrefix(ref, "runtime:") {
			evidenceID := strings.TrimPrefix(ref, "runtime:")
			if evidenceID == "" {
				return s7GateError(
					"S7_RESULT_EVIDENCE_REF",
					fmt.Sprintf("%s contains an empty runtime evidence reference", owner),
					[]string{"runtime:<evidence-id> is required"},
					[]string{"use the id of a registered Runtime evidence row or use path:<repo-relative-path> for a local artifact"},
					"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
				)
			}
			row, ok := indexed[evidenceID]
			if !ok {
				return s7GateError(
					"S7_RESULT_EVIDENCE_REF",
					fmt.Sprintf("%s references runtime evidence %q, but that id is not registered", owner, evidenceID),
					[]string{"runtime:" + evidenceID},
					[]string{"register the artifact as Runtime evidence first, then use runtime:" + evidenceID + " in the result"},
					"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
				)
			}
			if err := validateIndexedEvidence(root, row, ref, owner); err != nil {
				return err
			}
			continue
		}
		if row, ok := indexed[ref]; ok {
			if err := validateIndexedEvidence(root, row, ref, owner); err != nil {
				return err
			}
			continue
		}
		// S7-11 (RC-07): a bare reference that resolves to neither a typed
		// prefix, a repository path, nor an indexed evidence row is a ghost
		// reference. It used to pass silently and survive as an
		// accepted-pending-verdict artifact; now it is unsatisfied evidence
		// and rejects the whole submit.
		return s7GateError(
			"S7_RESULT_EVIDENCE_REF",
			fmt.Sprintf("%s evidence reference %q is not a valid typed ref and does not resolve to a registered Runtime evidence row", owner, ref),
			[]string{"bare references are only accepted when they name a registered evidence id; this one matches no indexed row and carries no path:/runtime: prefix"},
			[]string{"use path:<repo-relative-path>#sha256=<64 hex> for a local artifact, runtime:<evidence-id> for a registered Runtime evidence row, or register the artifact first with `runtime evidence add`"},
			"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
		)
	}
	return nil
}

func parsePathEvidenceRef(ref string) (string, string, error) {
	rel := strings.TrimPrefix(ref, "path:")
	marker := "#sha256="
	index := strings.Index(rel, marker)
	if index < 0 {
		return rel, "", nil
	}
	want := rel[index+len(marker):]
	rel = rel[:index]
	if len(want) != 64 {
		return "", "", fmt.Errorf("#sha256 must contain exactly 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(want); err != nil {
		return "", "", fmt.Errorf("#sha256 is not hexadecimal: %w", err)
	}
	return rel, want, nil
}

func validateIndexedEvidence(root string, row map[string]any, ref, owner string) error {
	rel := stringField(row["path"])
	want := stringField(row["sha256"])
	path, err := repositoryContainedPath(root, rel)
	if err != nil {
		return s7GateError(
			"S7_RESULT_EVIDENCE_REF",
			fmt.Sprintf("%s indexed evidence %q has an invalid path", owner, ref),
			[]string{err.Error()},
			[]string{"register evidence with a repository-contained regular file and its current sha256"},
			"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s7GateError(
			"S7_RESULT_EVIDENCE_REF",
			fmt.Sprintf("%s indexed evidence %q cannot be read", owner, ref),
			[]string{err.Error()},
			[]string{"restore the registered artifact or register a fresh evidence row"},
			"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
		)
	}
	if got := sha256Of(data); got != want {
		return s7GateError(
			"S7_RESULT_EVIDENCE_REF",
			fmt.Sprintf("%s indexed evidence %q has a digest mismatch", owner, ref),
			[]string{fmt.Sprintf("got %s, want %s", sha256Of(data), want)},
			[]string{"refresh the evidence registration or reference the current immutable artifact"},
			"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
		)
	}
	return nil
}

// validateVerdictConsistency keeps the verdict from contradicting the local
// claim results (L3-S7 §3.5).
func validateVerdictConsistency(result *Result) error {
	failures := 0
	for _, claimResult := range result.ClaimResults {
		if claimResult.Conclusion == "fail" {
			failures++
		}
	}
	failedChecks := 0
	for _, check := range result.Checks {
		if check.Result == "fail" {
			failedChecks++
		}
	}
	switch result.Verdict {
	case "pass":
		if failures > 0 || len(result.Findings) > 0 || failedChecks > 0 {
			return fmt.Errorf("verdict=pass contradicts %d fail claim(s), %d finding(s), %d failed check(s)", failures, len(result.Findings), failedChecks)
		}
		if len(result.BlockedClaims) > 0 {
			return fmt.Errorf("verdict=pass contradicts %d blocked claim(s); blocked_by_confirmed_finding is not a pass (L3-S7 §3.5)", len(result.BlockedClaims))
		}
		if len(result.Deviations) > 0 {
			return fmt.Errorf("verdict=pass contradicts %d recorded deviation(s)", len(result.Deviations))
		}
	case "finding":
		if failures == 0 && len(result.BlockedClaims) == 0 {
			return fmt.Errorf("verdict=finding requires at least one fail claim and one Finding with a real encounter (or a blocked_by_confirmed_finding projection bound to a confirmed Finding of this round)")
		}
		if failures > 0 && len(result.Findings) == 0 {
			return fmt.Errorf("verdict=finding with %d fail claim(s) requires at least one Finding with a real encounter", failures)
		}
	case "req_change_required", "release_blocked":
		// Pause verdicts route to the human gateway; claim results still
		// cover the exact set so the resumed round can rely on them.
	default:
		return fmt.Errorf("unknown verdict %q", result.Verdict)
	}
	return nil
}

// verdictHint inspects a raw ReviewResult payload for the verdict value and
// returns an actionable error when the verdict is a string the schema will
// reject — the most common authoring mistake is "verdict=fail", which is not
// a result-level verdict in this model (per-Claim failures live in
// claim_results[].conclusion; the result-level verdict for blocked Claims is
// "finding" with findings[] populated). The function only returns a hint
// when it can confidently recognize the offending value; other schema errors
// fall through to the regular validator. The bool reports whether a hint was
// returned so the caller can short-circuit before schema validation.
func verdictHint(data []byte) (error, bool) {
	var probe struct {
		Verdict any `json:"verdict"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, false
	}
	raw, ok := probe.Verdict.(string)
	if !ok || raw == "" {
		return nil, false
	}
	switch raw {
	case "pass", "finding", "req_change_required", "release_blocked":
		return nil, false
	case "fail":
		return fmt.Errorf("ReviewResult verdict=%q is not a valid result-level verdict; per-Claim failures belong in claim_results[].conclusion, and the result-level verdict for blocked Claims is \"finding\" with findings[] populated (one Finding per fail Claim, each with a real encounter). Valid values: pass, finding, req_change_required, release_blocked (L3-S7 §3.5)", raw), true
	}
	return nil, false
}

// validateFindings binds every fail Claim to exactly one Finding, validates
// each Finding against the schema, and enforces investigation readiness for
// ordinary findings (L3-S7 §3.6/§3.7).
func validateFindings(plan *Plan, assignment *PlanAssignment, result *Result) error {
	validator := schema.NewEmbeddedValidator()
	failByClaim := map[string]bool{}
	for _, claimResult := range result.ClaimResults {
		if claimResult.Conclusion == "fail" {
			failByClaim[claimResult.ClaimID] = true
		}
	}
	covered := map[string]int{}
	claimLens := map[string]string{}
	for _, claim := range plan.Claims {
		claimLens[claim.ClaimID] = claim.Lens
	}
	for _, finding := range result.Findings {
		data, err := json.Marshal(finding)
		if err != nil {
			return fmt.Errorf("encode finding %s: %w", finding.FindingID, err)
		}
		if err := validator.ValidateBytes("finding.schema.json", data); err != nil {
			return fmt.Errorf("finding %s schema: %w", finding.FindingID, err)
		}
		if finding.Blocking != nil && finding.Severity == "P0" && !*finding.Blocking {
			return fmt.Errorf("finding %s is P0 with blocking=false; P0 is implicitly business-blocking (L3-S7 §10.1) — either drop the field or fix the severity", finding.FindingID)
		}
		if !failByClaim[finding.ClaimID] {
			return fmt.Errorf("finding %s references claim %s which has no fail conclusion in this result", finding.FindingID, finding.ClaimID)
		}
		covered[finding.ClaimID]++
		if finding.Lens != assignment.Lens || claimLens[finding.ClaimID] != assignment.Lens {
			return fmt.Errorf("finding %s lens %q contradicts the assignment/claim lens %q", finding.FindingID, finding.Lens, assignment.Lens)
		}
	}
	// Validate every P0 before ordinary readiness failures are allowed to
	// route into site_lost. A mixed result must not hide a safety-stop Finding
	// behind the capture blocker path, regardless of Finding array order.
	for _, finding := range result.Findings {
		if finding.Severity == "P0" {
			if err := validateInvestigationReadiness(finding); err != nil {
				return err
			}
		}
	}
	for _, finding := range result.Findings {
		if finding.Severity != "P0" {
			if err := validateInvestigationReadiness(finding); err != nil {
				return &ReadinessError{
					FindingID: finding.FindingID,
					Severity:  finding.Severity,
					Err:       fmt.Errorf("%w; complete the scene in place from the capture buffer / read-only state, or — if it is unrecoverable — declare site_lost for this finding to record an Assignment BLOCKER that stays in S7 (L3-S7 §9.1)", err),
				}
			}
		}
	}
	for claimID := range failByClaim {
		if covered[claimID] == 0 {
			return fmt.Errorf("fail claim %s has no Finding; every fail Claim must reference one (L3-S7 §3.5)", claimID)
		}
		if covered[claimID] > 1 {
			return fmt.Errorf("fail claim %s has %d Findings; keep exactly one immutable Finding per fail Claim", claimID, covered[claimID])
		}
	}
	return nil
}

// validateInvestigationReadiness: ordinary findings need a complete failure
// boundary and step-bound evidence; P0 findings may stop dangerous capture
// but must say so explicitly (L3-S7 §3.7, §14.1).
func validateInvestigationReadiness(finding Finding) error {
	if finding.Severity == "P0" {
		if finding.Encounter.LastGoodCheckpoint == "" && len(finding.Encounter.CaptureGaps) == 0 {
			return fmt.Errorf("finding %s is P0 with no last_good_checkpoint; safety-stop findings must record explicit capture_gaps", finding.FindingID)
		}
		return nil
	}
	if finding.Encounter.LastGoodCheckpoint == "" {
		return fmt.Errorf("finding %s misses last_good_checkpoint; S8 investigates from last-good -> wall -> first-bad, not from a bare symptom (L3-S7 §3.6)", finding.FindingID)
	}
	if finding.Encounter.TerminalState == "" {
		return fmt.Errorf("finding %s misses terminal_state", finding.FindingID)
	}
	for _, step := range finding.Encounter.Timeline {
		if len(step.EvidenceRefs) == 0 {
			return fmt.Errorf("finding %s timeline step %d has no evidence_refs; ordinary findings are not investigation-ready without step-bound evidence", finding.FindingID, step.Sequence)
		}
	}
	return nil
}

// builderDeliveryEvidenceKinds is the whitelist of evidence kinds that carry
// an S6 Builder delivery this generation (S7-8/RC-07). completion_report is
// the canonical `runtime task-complete` envelope, but builder_report and
// agent_completion are persisted Runtime kinds from the evidence catalog that
// record the same delivery fact through the legacy `runtime evidence add`
// path. A producer is independent only if none of these carriers name it —
// checking a single kind would let a Builder self-certify by switching
// carriers.
var builderDeliveryEvidenceKinds = map[string]bool{
	"completion_report": true,
	"builder_report":    true,
	"agent_completion":  true,
}

// validateProducerIndependence enforces the Builder/Reviewer separation
// edge (L3-S7 §1.4): the producer must not be an Agent that delivered S6
// product results this generation, on any builder delivery evidence carrier.
func validateProducerIndependence(state map[string]any, result *Result) error {
	if result.ProducerAgentID == "" {
		return fmt.Errorf("producer_agent_id is required")
	}
	evidence, _ := state["evidence"].([]any)
	generation := baselineGeneration(state)
	for _, raw := range evidence {
		item, _ := raw.(map[string]any)
		if item == nil || !builderDeliveryEvidenceKinds[stringField(item["kind"])] {
			continue
		}
		if intField(item["baseline_generation"]) != generation {
			continue
		}
		producers, _ := item["produced_by"].([]any)
		for _, producer := range producers {
			if id, _ := producer.(string); id == result.ProducerAgentID {
				return fmt.Errorf("producer %s delivered an S6 Builder Result this generation (evidence kind %q) and cannot review it (L3-S7 §1.4 role independence)", result.ProducerAgentID, stringField(item["kind"]))
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// CAS Apply helpers
// ---------------------------------------------------------------------------

// applyResultConsumption registers the result evidence and updates the
// claim/assignment projections.
func applyResultConsumption(
	state map[string]any,
	plan *Plan,
	assignment *PlanAssignment,
	result *Result,
	resultRel, resultSHA, responsibility string,
	generation, round int,
	occurredAt time.Time,
) error {
	reviewMap, ok := state["review"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime review section must be an object")
	}
	assignments, _ := reviewMap["assignments"].(map[string]any)
	row, _ := assignments[assignment.AssignmentID].(map[string]any)
	if row == nil {
		return fmt.Errorf("assignment %s is not registered in the runtime projection; dispatch it via `runtime register-workgroup` first", assignment.AssignmentID)
	}
	status, _ := row["status"].(string)
	if status == "result_submitted" || status == "consumed" {
		return fmt.Errorf("assignment %s already has a consumed ReviewResult; one Assignment submits exactly one Result (L3-S7 §3.5)", assignment.AssignmentID)
	}
	agentID, _ := row["agent_id"].(string)
	if agentID == "" {
		return fmt.Errorf("assignment %s has no dispatched Agent; dispatch it via `runtime register-workgroup` before submitting", assignment.AssignmentID)
	}
	if agentID != result.ProducerAgentID {
		return fmt.Errorf("ReviewResult producer %s does not match the dispatched Agent %s for assignment %s", result.ProducerAgentID, agentID, assignment.AssignmentID)
	}
	if status == "blocked" {
		resolved := false
		entities, _ := state["entities"].(map[string]any)
		agents, _ := entities["agents"].([]any)
		for _, raw := range agents {
			agent, _ := raw.(map[string]any)
			if stringField(agent["id"]) == result.ProducerAgentID && stringField(agent["blocker_resolved_ref"]) != "" {
				resolved = true
				break
			}
		}
		if !resolved {
			return fmt.Errorf("assignment %s is blocked and cannot accept a ReviewResult until the canonical blocker_resolved Agent event records blocker_resolved_ref", assignment.AssignmentID)
		}
		row["blocker_ref"] = nil
		row["blocked_at"] = nil
	}

	claims, _ := reviewMap["claims"].(map[string]any)
	failFindings := map[string][]string{}
	for _, finding := range result.Findings {
		failFindings[finding.ClaimID] = append(failFindings[finding.ClaimID], finding.FindingID)
	}
	for _, claimResult := range result.ClaimResults {
		claimRow, _ := claims[claimResult.ClaimID].(map[string]any)
		if claimRow == nil {
			return fmt.Errorf("claim %s is missing from the runtime projection", claimResult.ClaimID)
		}
		if claimRow["applicability"] == "not_applicable" {
			return fmt.Errorf("claim %s is not_applicable in the plan; N/A is a plan disposition, never a Reviewer conclusion (L3-S7 §9.3)", claimResult.ClaimID)
		}
		if claimResult.Conclusion == "pass" {
			claimRow["disposition"] = "pass"
		} else {
			claimRow["disposition"] = "finding"
		}
		claimRow["result_id"] = result.ResultID
		ids := make([]any, 0, len(failFindings[claimResult.ClaimID]))
		for _, id := range failFindings[claimResult.ClaimID] {
			ids = append(ids, id)
		}
		claimRow["finding_ids"] = ids
	}
	if err := applyBlockedDispositions(claims, result); err != nil {
		return err
	}
	row["status"] = "consumed"
	row["result_ref"] = resultRel
	if result.VerificationArtifactDigest != nil && *result.VerificationArtifactDigest != "" {
		row["artifact_digest"] = *result.VerificationArtifactDigest
	}

	return appendEvidence(state, map[string]any{
		"id":                  result.ResultID,
		"kind":                "review_result",
		"path":                resultRel,
		"sha256":              resultSHA,
		"status":              "valid",
		"baseline_generation": generation,
		"review_round":        round,
		"produced_by":         []any{result.ProducerAgentID},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   responsibility,
		"scope_refs":          []any{},
	})
}

// findingArtifact pairs one Finding with its persisted evidence coordinates.
type findingArtifact struct {
	finding Finding
	rel     string
	sha     string
}

// applyFindings appends immutable finding rows and their evidence index
// entries. Finding identity is global: an id seen in any round collides.
func applyFindings(
	state map[string]any,
	result *Result,
	artifacts []findingArtifact,
	round int,
	occurredAt time.Time,
) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime entities must be an object")
	}
	if len(artifacts) == 0 {
		return nil
	}
	findings, _ := entities["findings"].([]any)
	for _, artifact := range artifacts {
		for _, raw := range findings {
			row, _ := raw.(map[string]any)
			if row != nil && row["finding_id"] == artifact.finding.FindingID {
				return fmt.Errorf("finding %s already exists; Findings are immutable — add a FindingSupplement instead of reusing the id", artifact.finding.FindingID)
			}
		}
		newRow := map[string]any{
			"finding_id":       artifact.finding.FindingID,
			"path":             artifact.rel,
			"sha256":           artifact.sha,
			"claim_id":         artifact.finding.ClaimID,
			"assignment_id":    result.AssignmentID,
			"lens":             artifact.finding.Lens,
			"severity":         artifact.finding.Severity,
			"observation_mode": artifact.finding.ObservationMode,
			"original_finder":  result.ProducerAgentID,
			"review_round":     round,
			"created_at":       occurredAt.UTC().Format(time.RFC3339Nano),
		}
		// RC-02: carry the explicit business-blocking marker into the entity
		// row so the clean-round evaluator sees the same blocking judgment as
		// the Reviewer. P0 rows persist the implicit blocking=true; a non-P0
		// blocking Finding persists blocking=true; an ordinary non-blocking
		// Finding carries no field (backward-compatible shape).
		if artifact.finding.Blocking != nil {
			newRow["blocking"] = *artifact.finding.Blocking
		} else if artifact.finding.Severity == "P0" {
			newRow["blocking"] = true
		}
		findings = append(findings, newRow)
		if err := appendEvidence(state, map[string]any{
			"id":                  artifact.finding.FindingID,
			"kind":                "finding",
			"path":                artifact.rel,
			"sha256":              artifact.sha,
			"status":              "valid",
			"baseline_generation": baselineGeneration(state),
			"review_round":        round,
			"produced_by":         []any{result.ProducerAgentID},
			"invalidated_by":      nil,
			"invalidation_rule":   nil,
			"invalidation_reason": nil,
			"responsibility_id":   LensToResponsibility(artifact.finding.Lens),
			"scope_refs":          []any{},
		}); err != nil {
			return err
		}
	}
	entities["findings"] = findings
	return nil
}

// advanceReviewerAgent moves the reviewer working -> reported, mirroring the
// completion_reported lifecycle event (entity_lifecycles.agent). Kept local
// so review stays a leaf package (transition -> verification -> review must
// not cycle).
func advanceReviewerAgent(state map[string]any, result *Result, resultRepoPath string, occurredAt time.Time) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent["id"] != result.ProducerAgentID {
			continue
		}
		currentState, _ := agent["state"].(string)
		if currentState == "reported" || currentState == "done" {
			return nil
		}
		if currentState != "working" {
			return fmt.Errorf("reviewer Agent %s is %s; a ReviewResult requires working state. On the plan_checkpoint path the PostToolUse(SendMessage) auto-chain advances reading -> understanding_submitted -> activated -> working automatically when PLAN_REPORT carries plan_ref pointing at the plan file; if the auto-chain did not fire (Worker omitted plan_ref, hook failed, or this is a plan_approval_required assignment), recover with `runtime agent-begin --agent-id %s --plan <plan-report.json>` and resubmit", result.ProducerAgentID, currentState, result.ProducerAgentID)
		}
		agent["state"] = "reported"
		agent["blocker_resolved_ref"] = nil
		if resultRepoPath != "" {
			agent["completion_reported_ref"] = resultRepoPath
		}
		agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
		return nil
	}
	return fmt.Errorf("Agent %s is not registered", result.ProducerAgentID)
}

// applyObservationBatch registers the sealed batch pointer and evidence.
func applyObservationBatch(
	state map[string]any,
	batchID, batchRel, batchSHA string,
	newFindings []Finding,
	round int,
	occurredAt time.Time,
) error {
	reviewMap := state["review"].(map[string]any)
	findingIDs := []any{}
	for _, row := range RoundFindings(state) {
		findingIDs = append(findingIDs, row["finding_id"])
	}
	reviewMap["observation_batch"] = map[string]any{
		"batch_id":     batchID,
		"path":         batchRel,
		"sha256":       batchSHA,
		"finding_ids":  findingIDs,
		"drain_policy": drainPolicyOf(newFindings),
		"sealed_at":    occurredAt.UTC().Format(time.RFC3339Nano),
	}
	return appendEvidence(state, map[string]any{
		"id":                  batchID,
		"kind":                "observation_batch",
		"path":                batchRel,
		"sha256":              batchSHA,
		"status":              "valid",
		"baseline_generation": baselineGeneration(state),
		"review_round":        round,
		"produced_by":         []any{"round-consumer"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "Orchestrator",
		"scope_refs":          []any{},
	})
}

// applyCleanRound registers the machine CleanRound evidence (L3-S7 §10.2).
func applyCleanRound(state map[string]any, cleanRel, cleanSHA string, round int, occurredAt time.Time) error {
	return appendEvidence(state, map[string]any{
		"id":                  fmt.Sprintf("clean-round-r%d", round),
		"kind":                "clean_round",
		"path":                cleanRel,
		"sha256":              cleanSHA,
		"status":              "valid",
		"baseline_generation": baselineGeneration(state),
		"review_round":        round,
		"produced_by":         []any{"round-consumer"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "Clean Round Evaluator",
		"scope_refs":          []any{},
	})
}

// appendEvidence registers one evidence index row, rejecting id collisions.
func appendEvidence(state map[string]any, entry map[string]any) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item != nil && item["id"] == entry["id"] {
			return fmt.Errorf("evidence %s is already registered", entry["id"])
		}
	}
	state["evidence"] = append(items, entry)
	state["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	return nil
}

// releaseQueuedReviewAssignments is the small queue consumer that was
// missing from the original resource-lock design. A conflicting workgroup is
// registered with its Agent already present, but its Assignment remains
// planned until the holder's Result is consumed. The consumer runs inside
// that same CAS, so lock release and queued dispatch cannot interleave.
func releaseQueuedReviewAssignments(state map[string]any) error {
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	claims, _ := reviewMap["claims"].(map[string]any)
	if assignments == nil || claims == nil {
		return fmt.Errorf("runtime review projection is missing assignments or claims")
	}
	ids := make([]string, 0, len(assignments))
	for id := range assignments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		row, _ := assignments[id].(map[string]any)
		if row == nil || row["status"] != "planned" || stringField(row["queue_reason"]) == "" {
			continue
		}
		agentID := stringField(row["queued_agent_id"])
		if agentID == "" || reviewAssignmentLockConflict(id, row, assignments) {
			continue
		}
		row["status"] = "dispatched"
		row["agent_id"] = agentID
		row["queued_agent_id"] = nil
		row["queue_reason"] = nil
		for _, rawClaimID := range stringSliceValue(row["claim_ids"]) {
			claimRow, _ := claims[rawClaimID].(map[string]any)
			if claimRow == nil {
				return fmt.Errorf("queued assignment %s references missing claim %s", id, rawClaimID)
			}
			if claimRow["disposition"] == "planned" {
				claimRow["disposition"] = "running"
			}
		}
		wakeQueuedReviewAgent(state, agentID)
	}
	return nil
}

func wakeQueuedReviewAgent(state map[string]any, agentID string) {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent != nil && agent["id"] == agentID && agent["state"] == "queued" {
			agent["state"] = "reading"
			return
		}
	}
}

func reviewAssignmentLockConflict(candidateID string, candidate map[string]any, assignments map[string]any) bool {
	want := reviewAssignmentLocks(candidate["resource_locks"])
	if len(want) == 0 {
		return false
	}
	for id, raw := range assignments {
		if id == candidateID {
			continue
		}
		row, _ := raw.(map[string]any)
		if row == nil || !reviewAssignmentLockHeld(row) {
			continue
		}
		for lock := range reviewAssignmentLocks(row["resource_locks"]) {
			if want[lock] {
				return true
			}
		}
	}
	return false
}

func reviewAssignmentLockHeld(row map[string]any) bool {
	switch stringField(row["status"]) {
	case "dispatched":
		return row["result_ref"] == nil
	case "result_submitted":
		return true
	default:
		return false
	}
}

func reviewAssignmentLocks(raw any) map[string]bool {
	locks := map[string]bool{}
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if lock, ok := value.(string); ok && strings.TrimSpace(lock) != "" {
				locks[strings.TrimSpace(lock)] = true
			}
		}
	case []string:
		for _, lock := range values {
			if strings.TrimSpace(lock) != "" {
				locks[strings.TrimSpace(lock)] = true
			}
		}
	}
	return locks
}

func stringSliceValue(raw any) []string {
	var out []string
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if id, ok := value.(string); ok && id != "" {
				out = append(out, id)
			}
		}
	case []string:
		out = append(out, values...)
	}
	return out
}

// setPlanStatus moves the ReviewPlan status and — when the status is also a
// verification phase — the lifecycle phase projection (L3-S7 §11.1). paused
// and stale are plan-only statuses: the cursor moves to the paused STATE via
// TR-010/TR-011, never to a verification phase.
func setPlanStatus(reviewMap, lifecycleMap map[string]any, status string) {
	if plan, ok := reviewMap["plan"].(map[string]any); ok {
		plan["status"] = status
	}
	switch status {
	case "running", "cannot_clean", "discovery_draining", "observation_sealed", "clean":
		if lifecycleMap != nil {
			lifecycleMap["phase"] = status
			lifecycleMap["phase_revision"] = intField(lifecycleMap["phase_revision"]) + 1
		}
	}
}

// staleReviewPlanAfterDrift records baseline/workspace drift before returning
// the validation error. A bare error leaves the runtime advertising a
// runnable round, so the next Agent keeps retrying against bytes that no
// longer belong to the pinned review. The mutation is plan-only: lifecycle
// phase remains the last valid verification phase and the scheduler's plan
// status is the admission gate.
func staleReviewPlanAfterDrift(
	root, statePath, journalPath string,
	current map[string]any,
	driftErr error,
) (loopruntime.Snapshot, error) {
	plan := PlanPointerFromState(current)
	if plan == nil {
		return loopruntime.Snapshot{}, driftErr
	}
	if plan.Status == "stale" {
		return loopruntime.Snapshot{}, driftErr
	}
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{}
	if lifecycle != nil {
		cursor = map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	}
	runtimeID, _ := current["runtime_id"].(string)
	commitRevision := currentCommitRevision(-1, current)
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, err := updateRuntime(store, -1, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-review-plan-stale-%s-r%d", plan.PlanID, commitRevision+1),
		TransitionID:   "REVIEW-PLAN-STALE",
		Event:          "review_plan_stale",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:review-plan-stale:%s:%d", plan.PlanID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		Message:        fmt.Sprintf("ReviewPlan %s marked stale: %v", plan.PlanID, driftErr),
		OccurredAt:     time.Now().UTC(),
		Apply: func(state map[string]any) error {
			reviewMap, _ := state["review"].(map[string]any)
			planMap, _ := reviewMap["plan"].(map[string]any)
			if planMap == nil {
				return fmt.Errorf("review plan pointer missing while marking stale")
			}
			status, _ := planMap["status"].(string)
			if status != "running" && status != "cannot_clean" && status != "discovery_draining" && status != "stale" {
				return fmt.Errorf("ReviewPlan %s is %s; cannot mark this round stale", plan.PlanID, status)
			}
			planMap["status"] = "stale"
			return nil
		},
	})
	if err != nil {
		return snapshot, fmt.Errorf("%w; failed to persist ReviewPlan stale status: %v", driftErr, err)
	}
	return snapshot, fmt.Errorf("%w; ReviewPlan %s was marked stale and must be re-planned", driftErr, plan.PlanID)
}

// ---------------------------------------------------------------------------
// round consumer projections
// ---------------------------------------------------------------------------

// projectDispositions returns the claim disposition map as it will look
// after this result is consumed.
func projectDispositions(state map[string]any, assignment *PlanAssignment, result *Result) map[string]ClaimDisposition {
	dispositions := Dispositions(state)
	failByClaim := map[string]bool{}
	for _, finding := range result.Findings {
		failByClaim[finding.ClaimID] = true
	}
	for _, claimResult := range result.ClaimResults {
		disp := dispositions[claimResult.ClaimID]
		disp.ResultID = result.ResultID
		if failByClaim[claimResult.ClaimID] {
			disp.Disposition = "finding"
		} else {
			disp.Disposition = "pass"
		}
		dispositions[claimResult.ClaimID] = disp
	}
	projectBlockedDispositions(dispositions, result)
	return dispositions
}

func roundCompleteWith(dispositions map[string]ClaimDisposition) bool {
	for _, disp := range dispositions {
		if disp.Applicability == "not_applicable" {
			continue
		}
		switch disp.Disposition {
		case "pass", "finding", "blocked":
		default:
			return false
		}
	}
	return true
}

func drainPolicyOf(newFindings []Finding) string {
	for _, finding := range newFindings {
		if finding.Severity == "P0" {
			return "immediate_stop"
		}
	}
	return "complete_required_claims"
}

func findingIDs(findings []Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.FindingID)
	}
	return ids
}

// state_view exposes the pre-transaction state to the batch builder; root
// lets it recover blocked-projection details from persisted result envelopes.
type state_view struct {
	state map[string]any
	root  string
}

// buildObservationBatch assembles the sealed handoff document (L3-S7 §3.7).
func buildObservationBatch(
	view state_view,
	plan *Plan, ptr *PlanPointer,
	projected map[string]ClaimDisposition,
	result *Result,
	complete bool,
	occurredAt time.Time,
) (map[string]any, error) {
	state := view.state
	round := currentReviewRound(state)
	batchFindingIDs := []string{}
	readiness := []any{}
	routes := []any{}
	for _, row := range RoundFindings(state) {
		batchFindingIDs = append(batchFindingIDs, stringField(row["finding_id"]))
		// S7-5 (RC-07): readiness is recomputed from the persisted Finding
		// bytes for the full round, never copied from a previous batch or
		// hard-coded to ready. A prior-round blocker without a location
		// anchor must not flow into S8 as ready.
		readiness = append(readiness, readinessForFindingID(view, row, nil, false))
		routes = append(routes, map[string]any{
			"finding_id":    row["finding_id"],
			"agent_id":      row["original_finder"],
			"assignment_id": row["assignment_id"],
		})
	}
	for _, finding := range result.Findings {
		batchFindingIDs = append(batchFindingIDs, finding.FindingID)
		readiness = append(readiness, readinessForFindingID(view, nil, &finding, true))
		routes = append(routes, map[string]any{
			"finding_id":    finding.FindingID,
			"agent_id":      result.ProducerAgentID,
			"assignment_id": result.AssignmentID,
		})
	}
	sort.Strings(batchFindingIDs)

	summary := map[string]any{"pass": 0, "finding": 0, "not_applicable": 0, "blocked": 0}
	total := 0
	for _, disp := range projected {
		if disp.Applicability == "not_applicable" {
			summary["not_applicable"] = summary["not_applicable"].(int) + 1
			continue
		}
		total++
		switch disp.Disposition {
		case "pass":
			summary["pass"] = summary["pass"].(int) + 1
		case "finding":
			summary["finding"] = summary["finding"].(int) + 1
		case "blocked":
			summary["blocked"] = summary["blocked"].(int) + 1
		}
	}
	summary["total_required"] = total
	summary["plan_revision"] = ptr.Revision
	// blocked_by_confirmed_finding bindings ride the sealed batch so S8 sees
	// exactly which Claims were objectively non-executable, by which confirmed
	// Findings, and that the repaired round owes them (L3-S7 §3.7).
	blockedClaims, err := batchBlockedClaims(view, projected, result)
	if err != nil {
		return nil, err
	}
	summary["blocked_claims"] = blockedClaims

	drainPolicy := drainPolicyOf(result.Findings)
	unobserved := []string{}
	stopReason := ""
	if !complete {
		// immediate_stop only: ordinary batches seal with an empty unobserved
		// set (L3-S7 §3.7 seal condition).
		for _, claimID := range UndispositionedRequired(state) {
			if disp, ok := projected[claimID]; ok {
				switch disp.Disposition {
				case "pass", "finding", "blocked":
					continue
				}
			}
			unobserved = append(unobserved, claimID)
		}
		sort.Strings(unobserved)
		stopReason = "P0/security/data-destructive finding: stop-the-line with explicit safety gaps"
	}

	runtimeID, _ := state["runtime_id"].(string)
	return map[string]any{
		"schema_version":                         "1.0.0",
		"observation_batch_id":                   fmt.Sprintf("observation-batch-r%d", round),
		"conclusion":                             "sealed",
		"evidence_id":                            fmt.Sprintf("observation-batch-r%d", round),
		"kind":                                   "observation_batch",
		"runtime_id":                             runtimeID,
		"producer_agent_id":                      "round-consumer",
		"producer_responsibility":                "Orchestrator",
		"review_plan_id":                         plan.ReviewPlanID,
		"review_round":                           round,
		"baseline_generation":                    baselineGeneration(state),
		"subject_digest":                         SubjectDigest(plan),
		"finding_ids":                            batchFindingIDs,
		"drained_assignment_ids":                 drainedAssignments(state, result.AssignmentID),
		"drain_policy":                           drainPolicy,
		"claim_coverage_summary":                 summary,
		"cancelled_or_non_gating_assignment_ids": []any{},
		"unobserved_claim_ids":                   unobserved,
		"original_finder_routes":                 routes,
		"investigation_readiness":                readiness,
		"severity_summary":                       severitySummary(state, result.Findings),
		"stop_reason":                            stopReason,
		"sealed_at":                              occurredAt.UTC().Format(time.RFC3339Nano),
		"sealed_by":                              "round-consumer",
		"revision":                               1,
	}, nil
}

// readinessForFindingID recomputes one finding's investigation readiness from
// its own recorded encounter (S7-5/RC-07). Row findings (already persisted)
// are re-read from their immutable artifact when the entity row is a pointer
// only; in-memory findings are evaluated directly. A finding is ready only
// when its investigation anchors survived: last_good_checkpoint, first_bad
// wall, terminal state, and step-bound evidence (matching
// validateInvestigationReadiness). Anything less is reported as
// ready_with_safety_gaps with the concrete gap list so S8 never inherits a
// readiness claim the finding bytes cannot support.
func readinessForFindingID(view state_view, row map[string]any, finding *Finding, fromResult bool) map[string]any {
	findingID := ""
	var encounter Encounter
	if finding != nil {
		findingID = finding.FindingID
		encounter = finding.Encounter
	} else if row != nil {
		findingID = stringField(row["finding_id"])
		encounter = loadPersistedEncounter(view, row)
	}
	gaps := append([]string(nil), encounter.CaptureGaps...)
	if encounter.LastGoodCheckpoint == "" {
		gaps = append(gaps, "missing last_good_checkpoint: S8 cannot walk last-good -> wall -> first-bad from a bare symptom")
	}
	if finding != nil || fromResult {
		// In-result findings were already schema-validated; the P0 branch may
		// legally stop dangerous capture. Persisted rows keep their original
		// readiness, recomputed from the artifact below.
		if encounter.TerminalState == "" {
			gaps = append(gaps, "missing terminal_state: the observation has no recorded end state")
		}
	}
	for _, step := range encounter.Timeline {
		if len(step.EvidenceRefs) == 0 {
			gaps = append(gaps, fmt.Sprintf("timeline step %d has no evidence_refs", step.Sequence))
		}
	}
	// The batch schema admits ready and ready_with_safety_gaps; any recomputed
	// gap downgrades the row so S8 sees the real capture state, never a
	// hard-coded ready. (A finding that cannot support its own readiness
	// claim is exactly what S7-5 forbids from flowing into S8.)
	status := "ready"
	if len(gaps) > 0 {
		status = "ready_with_safety_gaps"
	}
	gapValues := make([]any, 0, len(gaps))
	for _, gap := range gaps {
		gapValues = append(gapValues, gap)
	}
	return map[string]any{
		"finding_id":   findingID,
		"status":       status,
		"capture_gaps": gapValues,
	}
}

// loadPersistedEncounter re-reads a persisted Finding artifact so readiness is
// recomputed from the immutable bytes, not from a projection that may lag the
// evidence on disk.
func loadPersistedEncounter(view state_view, row map[string]any) Encounter {
	var encounter Encounter
	rel := stringField(row["path"])
	if rel == "" {
		return encounter
	}
	path, err := repositoryContainedPath(view.root, rel)
	if err != nil {
		return encounter
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return encounter
	}
	var document struct {
		Encounter Encounter `json:"encounter"`
	}
	if json.Unmarshal(data, &document) == nil {
		return document.Encounter
	}
	return encounter
}

func drainedAssignments(state map[string]any, currentAssignmentID string) []any {
	out := []any{}
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	ids := make([]string, 0, len(assignments))
	for id := range assignments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		row, _ := assignments[id].(map[string]any)
		if row != nil && row["status"] == "consumed" {
			out = append(out, id)
		}
	}
	if currentAssignmentID != "" {
		found := false
		for _, value := range out {
			if value == currentAssignmentID {
				found = true
				break
			}
		}
		if !found {
			out = append(out, currentAssignmentID)
			sort.Slice(out, func(i, j int) bool { return out[i].(string) < out[j].(string) })
		}
	}
	return out
}

func severitySummary(state map[string]any, newFindings []Finding) string {
	counts := map[string]int{}
	for _, row := range RoundFindings(state) {
		counts[stringField(row["severity"])]++
	}
	for _, finding := range newFindings {
		counts[finding.Severity]++
	}
	parts := []string{}
	for _, severity := range []string{"P0", "P1", "P2", "P3"} {
		if counts[severity] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", severity, counts[severity]))
		}
	}
	return strings.Join(parts, ", ")
}

// buildCleanRoundSnapshot assembles the immutable CleanRound document
// (L3-S7 §10.2): minimal references and digests for recomputation, not a
// copy of the raw evidence.
func buildCleanRoundSnapshot(
	state map[string]any,
	plan *Plan,
	resultEnvelope map[string]any,
	resultSHA, resultRel string,
	occurredAt time.Time,
) map[string]any {
	round := currentReviewRound(state)
	resultRefs := []any{}
	seen := map[string]bool{}
	evidence, _ := state["evidence"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range evidence {
		item, _ := raw.(map[string]any)
		if item != nil {
			byID[stringField(item["id"])] = item
		}
	}
	for _, disp := range Dispositions(state) {
		if disp.ResultID == "" || seen[disp.ResultID] {
			continue
		}
		seen[disp.ResultID] = true
		if item, ok := byID[disp.ResultID]; ok {
			resultRefs = append(resultRefs, map[string]any{
				"result_id": disp.ResultID,
				"path":      item["path"],
				"sha256":    item["sha256"],
			})
		}
	}
	resultRefs = append(resultRefs, map[string]any{
		"result_id": resultEnvelope["evidence_id"],
		"path":      resultRel,
		"sha256":    resultSHA,
	})
	runtimeID, _ := state["runtime_id"].(string)
	return map[string]any{
		"schema_version": "1.0.0",
		"clean_round_id": fmt.Sprintf("clean-round-r%d", round),
		// envelope identity fields: the gate verifies the persisted document
		// against the evidence index row (same contract as the batch).
		"evidence_id":             fmt.Sprintf("clean-round-r%d", round),
		"kind":                    "clean_round",
		"runtime_id":              runtimeID,
		"producer_agent_id":       "round-consumer",
		"producer_responsibility": "Clean Round Evaluator",
		"review_plan_id":          plan.ReviewPlanID,
		"review_round":            round,
		"baseline_generation":     baselineGeneration(state),
		"subject_digest":          SubjectDigest(plan),
		"result_refs":             resultRefs,
		"conclusion":              "pass",
		"evaluated_at":            occurredAt.UTC().Format(time.RFC3339Nano),
		"evaluated_by":            "round-consumer",
		"evaluator_version":       "review.SubmitResult/1",
	}
}

// capturePauseCheckpoint mirrors transition.CapturePauseCheckpoint's field
// shape (engine.go documentFingerprints + pauseRequiredAction) so the S7
// verdict transaction creates the one authoritative checkpoint without
// importing transition (transition -> verification -> review must stay
// acyclic).
func capturePauseCheckpoint(state map[string]any, reason string, occurredAt time.Time) error {
	if existing, ok := state["pause"]; ok && existing != nil {
		return fmt.Errorf("pause checkpoint already exists; would overwrite")
	}
	lifecycle, ok := state["lifecycle"].(map[string]any)
	if !ok {
		return fmt.Errorf("lifecycle missing")
	}
	documents := []any{}
	for _, raw := range stateDocuments(state) {
		doc, _ := raw.(map[string]any)
		if doc == nil || doc["path"] == nil {
			continue
		}
		documents = append(documents, map[string]any{
			"path":    doc["path"],
			"version": doc["version"],
			"sha256":  doc["sha256"],
		})
	}
	state["pause"] = map[string]any{
		"from_state":            lifecycle["state"],
		"from_phase":            lifecycle["phase"],
		"phase_revision":        intField(lifecycle["phase_revision"]),
		"baseline_generation":   baselineGeneration(state),
		"review_round":          currentReviewRound(state),
		"reason":                reason,
		"required_human_action": "review the blocking finding or REQ change, then resume or re-lock the REQ",
		"document_fingerprints": documents,
		"paused_at":             occurredAt.UTC().Format(time.RFC3339Nano),
	}
	return nil
}

func stateDocuments(state map[string]any) []any {
	documents, _ := state["documents"].([]any)
	return documents
}

// ---------------------------------------------------------------------------
// artifact IO
// ---------------------------------------------------------------------------

func marshalArtifact(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeArtifact(root, rel string, data []byte) error {
	abs, err := repositoryContainedPath(root, rel)
	if err != nil {
		return fmt.Errorf("artifact path %s: %w", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("create evidence dir: %w", err)
	}
	file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("evidence artifact %s already exists but was never indexed by the runtime (a previous failed attempt staged it); delete the stale file or change the artifact id (result_id / finding_id / review_plan_id), then retry", rel)
		}
		return fmt.Errorf("write %s without overwrite: %w", rel, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(abs)
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(abs)
		return fmt.Errorf("close %s: %w", rel, err)
	}
	return nil
}

func repositoryPath(root, path string) string {
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !filepath.IsAbs(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}
