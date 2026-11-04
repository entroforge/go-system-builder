package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// SubmitRequest carries the original reviewer answer to its consumer.
type SubmitRequest struct {
	Context          context.Context `json:"-"`
	ExpectedRevision int
	AssignmentID     string
	ResultPath       string
	// CaptureDir optionally points at a captures directory; findings with an
	// empty encounter timeline absorb the buffered steps (L3-S7 §3.6).
	CaptureDir string
	OccurredAt time.Time
	// Issued adapters supply frozen projected bytes and the exact registered
	// packet owner/source through private fields; Result JSON cannot manufacture them.
	resultData       []byte
	parsedResult     *Result
	issuedPacket     *workpacket.ReviewResultPacket
	packetOwner      string
	issuedSourceRoot string
	preparation      *resultPreparation
}

// Only immutable answer work and this attempt's progress survive a bounded
// continuation. Runtime qualification and mutable evidence are always rechecked.
type resultPreparation struct {
	schemaValidated bool
	answerContent   string
	capturedData    []byte
}

// Keep the existing fixed completion allowance. A single deadline tail may
// continue the same frozen original answer; it neither adopts unregistered
// candidates nor starts another recovery budget after a late return.
const resultRecoveryBudget = 900 * time.Millisecond
const resultCleanupBudget = 500 * time.Millisecond

// Concurrent peers use only the original request's remaining work budget.
// A local cap also bounds explicit callers that supply no deadline.
const resultConcurrencyBudget = 5 * time.Second

// SubmitResult is the single entry point for a Canonical ReviewResult
// (L3-S7 §9.1). It wraps submitResult with the first-pass success metric
// (§14.2): stale-revision CAS conflicts are concurrency retries, not submit
// friction, so only real validation/consumption failures count as rejected.
func SubmitResult(
	root, statePath, journalPath string,
	request SubmitRequest,
) (loopruntime.Snapshot, error) {
	request.Context = loopruntime.WithPacketBodyRoot(loopruntime.WithReviewResultReadCache(buildsurface.WithReadCache(request.Context)), root)
	defer metrics.Span(request.Context, "reception/s7")()
	if request.AssignmentID == "" || request.ResultPath == "" {
		_ = metrics.RecordS7ResultSubmit(root, "rejected")
		return loopruntime.Snapshot{}, fmt.Errorf("--assignment-id and --result are required")
	}
	entryError := request.Context.Err()
	if entryError != nil && (request.resultData == nil || !errors.Is(entryError, context.DeadlineExceeded)) {
		_ = metrics.RecordS7ResultSubmit(root, "interrupted")
		return loopruntime.Snapshot{}, entryError
	}
	// Freeze the answer once for both the original transaction and its bounded
	// deadline recovery. Recovery must never reread an author-editable file.
	if request.resultData == nil {
		data, err := readReviewResultInput(request.ResultPath)
		if err != nil {
			outcome := "rejected"
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				outcome = "interrupted"
			}
			_ = metrics.RecordS7ResultSubmit(root, outcome)
			return loopruntime.Snapshot{}, fmt.Errorf("read ReviewResult: %w", err)
		}
		request.resultData = data
	}
	if request.OccurredAt.IsZero() {
		request.OccurredAt = time.Now().UTC()
	}
	request.preparation = &resultPreparation{}
	snapshot, err := submitResult(root, statePath, journalPath, request)
	if errors.Is(err, context.DeadlineExceeded) && request.Context != nil && errors.Is(request.Context.Err(), context.DeadlineExceeded) {
		finishTail := metrics.Span(request.Context, "reception/s7/recovery_tail")
		tailContext, cancel := reviewDeadlineTail(request.Context, resultRecoveryBudget, resultCleanupBudget+resultRecoveryBudget)
		recovery := request
		recovery.Context = tailContext
		snapshot, err = continueInterruptedResult(root, statePath, journalPath, recovery, entryError == nil)
		cancel()
		finishTail()
	} else if errors.Is(err, loopruntime.ErrStaleRevision) && request.issuedPacket != nil && (request.Context == nil || request.Context.Err() == nil) {
		// Only system-issued packets can continue a concurrent peer's revision.
		// This shares the one continuation slot with deadline recovery: a stale
		// continuation that expires never starts a second deadline attempt.
		finishTail := metrics.Span(request.Context, "reception/s7/concurrency_tail")
		parent := request.Context
		if parent == nil {
			parent = context.Background()
		}
		tailContext, cancel := context.WithTimeout(parent, resultConcurrencyBudget)
		recovery := request
		recovery.Context = tailContext
		snapshot, err = continueIssuedResultAfterPeer(root, statePath, journalPath, recovery)
		cancel()
		finishTail()
	}
	switch {
	case errors.Is(err, errExactResultReplay):
		// A verified replay is a read-only acknowledgement of the original
		// acceptance. Do not count the same durable event twice.
		return snapshot, nil
	case err == nil:
		_ = metrics.RecordS7ResultSubmit(root, "accepted")
	case errors.Is(err, loopruntime.ErrStaleRevision):
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		_ = metrics.RecordS7ResultSubmit(root, "interrupted")
	default:
		_ = metrics.RecordS7ResultSubmit(root, "rejected")
	}
	return snapshot, err
}

// continueInterruptedResult first recovers the original Writer pairing and
// acknowledges an accepted receipt. Otherwise the same frozen original answer
// may continue within its remaining allowance, without adopting old candidates.
func continueInterruptedResult(root, statePath, journalPath string, request SubmitRequest, continueReception bool) (loopruntime.Snapshot, error) {
	writer := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	current, err := writer.Snapshot()
	if err != nil {
		return current, err
	}
	var incoming Result
	if err := json.Unmarshal(request.resultData, &incoming); err != nil {
		return current, err
	}
	if typedResultReceiptsEnabled(current.State) {
		row := resultPacketAssignmentRow(current.State, incoming.AssignmentID)
		replay := workstate.ReviewAssignmentActivity(row) == "consumed" || workstate.ReviewAssignmentActivity(row) == "result_submitted"
		if request.issuedPacket != nil {
			replay = issuedResultRow(current.State, *request.issuedPacket)["result_receipt"] != nil
		}
		if replay {
			if err := validateIssuedResultRequest(root, current.State, request, &incoming, true); err != nil {
				return current, err
			}
			content, err := professionalAnswerContent(request.resultData, request.issuedPacket != nil)
			if err != nil {
				return current, err
			}
			if accepted, err := replayResultRequest(root, current.State, request, &incoming, content); err != nil {
				return current, err
			} else if accepted {
				return current, errExactResultReplay
			}
		}
	}
	// A call that entered after its work deadline may restore the original
	// pending/receipt above, but absence of that fact cannot start reception.
	if !continueReception {
		return current, context.DeadlineExceeded
	}
	if request.ExpectedRevision >= 0 && current.Revision != request.ExpectedRevision {
		if request.issuedPacket == nil {
			return current, loopruntime.ErrStaleRevision
		}
		return continueIssuedResultAfterPeer(root, statePath, journalPath, request)
	}
	return submitResult(root, statePath, journalPath, request)
}

// continueIssuedResultAfterPeer refreshes only the internal CAS coordinate.
// The original issuance, exact answer, live responsibility and execution source
// must still prove the same receipt opportunity before that coordinate is used.
func continueIssuedResultAfterPeer(root, statePath, journalPath string, request SubmitRequest) (loopruntime.Snapshot, error) {
	writer := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	current, err := writer.Snapshot()
	if err != nil {
		return current, err
	}
	var incoming Result
	if err := json.Unmarshal(request.resultData, &incoming); err != nil {
		return current, fmt.Errorf("decode frozen issued ReviewResult: %w", err)
	}
	if stringField(current.State["runtime_id"]) != request.issuedPacket.Binding.RuntimeID {
		return current, errors.New("S7 issued answer Runtime changed during concurrent reception")
	}
	row := resultPacketAssignmentRow(current.State, incoming.AssignmentID)
	isReplay := workstate.ReviewAssignmentActivity(row) == "consumed" || workstate.ReviewAssignmentActivity(row) == "result_submitted"
	if request.issuedPacket != nil {
		isReplay = IssuedResultWorkComplete(current.State, *request.issuedPacket)
	}
	if err := validateIssuedResultRequest(root, current.State, request, &incoming, isReplay); err != nil {
		return current, err
	}
	request.ExpectedRevision = current.Revision
	return submitResult(root, statePath, journalPath, request)
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
	data := request.resultData
	var err error
	if data == nil {
		data, err = readReviewResultInput(request.ResultPath)
	}
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
	if request.parsedResult == nil && (request.preparation == nil || !request.preparation.schemaValidated) {
		if err := schema.NewValidator(root).ValidateBytes("review-result.schema.json", data); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult schema: %w", err)
		}
		if request.preparation != nil {
			request.preparation.schemaValidated = true
		}
	}
	var result Result
	if request.parsedResult != nil {
		result = *request.parsedResult
	} else if err := json.Unmarshal(data, &result); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode ReviewResult: %w", err)
	}
	if result.AssignmentID != request.AssignmentID {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult assignment_id %s does not match --assignment-id %s", result.AssignmentID, request.AssignmentID)
	}
	// v1.2 packet-mode Results have a durable idempotency receipt. Recover a
	// pending Runtime operation before considering artifact publication, then
	// recognize a fully proven replay before reading the mutable capture buffer
	// or applying the caller's now-stale ExpectedRevision.
	var earlyState map[string]any
	var earlySnapshot loopruntime.Snapshot
	var earlyStateErr error
	if stateData, readErr := os.ReadFile(statePath); readErr != nil {
		earlyStateErr = readErr
	} else if err := json.Unmarshal(stateData, &earlyState); err != nil {
		earlyStateErr = fmt.Errorf("decode runtime: %w", err)
	}
	receiptMode := earlyStateErr == nil && typedResultReceiptsEnabled(earlyState)
	var answerContent string
	if receiptMode {
		if err := workpacket.ValidateUniqueJSON(data); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult JSON: %w", err)
		}
		if request.preparation != nil {
			answerContent = request.preparation.answerContent
		}
		if answerContent == "" {
			answerContent, err = professionalAnswerContent(data, request.issuedPacket != nil)
			if err != nil {
				return loopruntime.Snapshot{}, err
			}
			if request.preparation != nil {
				request.preparation.answerContent = answerContent
			}
		}
		store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
		earlySnapshot, err = store.Snapshot()
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("recover/read Runtime before typed ReviewResult: %w", err)
		}
		earlyState = earlySnapshot.State
		row := resultPacketAssignmentRow(earlyState, result.AssignmentID)
		isReplay := workstate.ReviewAssignmentActivity(row) == "consumed" || workstate.ReviewAssignmentActivity(row) == "result_submitted"
		if request.issuedPacket != nil {
			isReplay = IssuedResultWorkComplete(earlyState, *request.issuedPacket)
		}
		if err := validateIssuedResultRequest(root, earlyState, request, &result, isReplay); err != nil {
			return earlySnapshot, err
		}
		if replayed, replayErr := replayResultRequest(root, earlyState, request, &result, answerContent); replayErr != nil {
			return earlySnapshot, replayErr
		} else if replayed {
			return earlySnapshot, errExactResultReplay
		}
	}

	var current map[string]any
	if receiptMode {
		current = earlyState
	} else {
		stateData, err := os.ReadFile(statePath)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
		}
		if err := json.Unmarshal(stateData, &current); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
		}
	}
	// Reject a stale caller before producing any review artifacts. The CAS
	// remains authoritative for races, but this early check prevents the
	// common stale-submit path from leaving orphan evidence on disk.
	if request.ExpectedRevision >= 0 && intField(current["revision"]) != request.ExpectedRevision {
		return loopruntime.Snapshot{}, loopruntime.ErrStaleRevision
	}
	commitRevision := currentCommitRevision(request.ExpectedRevision, current)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	if state, _ := lifecycle["state"].(string); state != "verification" && request.issuedPacket == nil {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResults can only be submitted in the verification stage (current state: %s)", lifecycle["state"])
	}
	var plan *Plan
	var ptr *PlanPointer
	historical := request.issuedPacket != nil && !loopruntime.ReviewResultPacketCurrent(current, *request.issuedPacket)
	// A Plan amendment does not replace an unchanged in-flight Assignment.
	// Its original packet still names the Reviewer's actual responsibility.
	continuesCurrent := false
	if historical {
		continuesCurrent, _ = IssuedResultAppliesToCurrent(root, current, *request.issuedPacket, request.Context)
	}
	if historical {
		plan, ptr, err = loadOriginalResultPlan(root, *request.issuedPacket, request.Context)
	} else {
		plan, ptr, err = LoadPlan(root, current)
	}
	if err != nil {
		if PlanPointerFromState(current) != nil {
			return staleReviewPlanAfterDrift(request.Context, root, statePath, journalPath, current, err)
		}
		return loopruntime.Snapshot{}, err
	}

	generation := baselineGeneration(current)
	if historical {
		generation = result.BaselineGeneration
	}
	switch ptr.Status {
	case "running", "cannot_clean", "discovery_draining", "observation_sealed":
	default:
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan %s is %s; results are only accepted while the round is running or draining", ptr.PlanID, ptr.Status)
	}
	if result.ReviewPlanID != plan.ReviewPlanID {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult binds plan %s but the registered plan is %s", result.ReviewPlanID, plan.ReviewPlanID)
	}
	if result.BaselineGeneration != generation {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult declares baseline_generation %d but the runtime is at generation %d", result.BaselineGeneration, generation)
	}
	if result.AssignmentRevision != ptr.Revision {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewResult declares assignment_revision %d but the registered ReviewPlan is at assignment_revision %d; re-read the current plan before submitting", result.AssignmentRevision, ptr.Revision)
	}

	assignment := findPlanAssignment(plan, result.AssignmentID)
	if assignment == nil {
		return loopruntime.Snapshot{}, fmt.Errorf("assignment %s is not part of ReviewPlan %s (known: %s)", result.AssignmentID, plan.ReviewPlanID, strings.Join(planAssignmentIDs(plan), ", "))
	}

	if err := validateVerdictConsistency(&result, assignment); err != nil {
		return loopruntime.Snapshot{}, err
	}

	runtimeID, _ := current["runtime_id"].(string)
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	lens := assignment.Lens
	responsibility := LensToResponsibility(lens)
	var effectiveAnswer []byte
	var receipt *ResultReceipt
	if receiptMode {
		// Preserve the professional document, including unknown Finding fields.
		// Retired framework metadata is not part of a new saved result. Only
		// these top-level machine members are omitted; author text is untouched.
		var document map[string]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			return loopruntime.Snapshot{}, err
		}
		for _, key := range []string{"subject_digest", "verification_artifact_digest", "answer_sha256", "effective_answer_sha256", "review_plan_sha256", "assignment_sha256", "result_sha256", "packet_sha256"} {
			delete(document, key)
		}
		effectiveAnswer, err = json.Marshal(document)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
	}

	// Publish only by exclusive creation inside the original Writer lock.
	// A pre-pending interruption leaves unknown candidates; original Runtime
	// references and its pending transaction alone select committed artifacts.
	resultBase := append([]string{".claude", "evidence"}, boundedReviewIdentity(runtimeID)...)
	resultBase = append(resultBase, fmt.Sprintf("g%d", generation), "reviews")
	resultBase = append(resultBase, boundedReviewIdentity(result.ProducerAgentID)...)
	resultRel := boundedReviewArtifactPath(resultBase, result.ResultID)

	var findingArtifacts []findingArtifact
	if receiptMode {
		value, err := newResultReceipt(root, current, plan, ptr, assignment, &result, resultRel, request.issuedPacket, request.issuedSourceRoot)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		receipt = &value
	}

	late := (historical && !continuesCurrent) || lifecycle["state"] != "verification"
	sealNow := !late && len(result.Findings) > 0
	cleanNow := false
	prepareArtifacts := func(state map[string]any) error {
		if request.Context != nil {
			if err := request.Context.Err(); err != nil {
				return err
			}
		}
		if intField(state["revision"]) != commitRevision {
			return loopruntime.ErrStaleRevision
		}
		resultEnvelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             result.ResultID,
			"kind":                    "review_result",
			"runtime_id":              runtimeID,
			"baseline_generation":     generation,
			"producer_agent_id":       result.ProducerAgentID,
			"producer_responsibility": responsibility,
			"subject_refs":            []any{},
			"conclusion":              result.Verdict,
			"review_plan_id":          plan.ReviewPlanID,
			"assignment_id":           result.AssignmentID,
			"assignment_revision":     result.AssignmentRevision,
			"claim_results":           result.ClaimResults,
			"blocked_claims":          blockedClaimsOrEmpty(&result),
			"checks":                  result.Checks,
			"deviations":              result.Deviations,
			"verdict":                 result.Verdict,
			"created_at":              occurredAt.UTC().Format(time.RFC3339Nano),
		}
		if receiptMode {
			resultEnvelope["effective_answer"] = json.RawMessage(append([]byte(nil), effectiveAnswer...))
			if request.issuedPacket != nil {
				resultEnvelope["packet_id"] = request.issuedPacket.ID
				resultEnvelope["execution_root"] = request.issuedSourceRoot
			}
		}
		resultBytes, err := marshalArtifact(resultEnvelope)
		if err != nil {
			return fmt.Errorf("encode review result envelope: %w", err)
		}
		resultRel, err = publishResultArtifact(request.Context, root, state, resultRel, resultBytes)
		if err != nil {
			return err
		}
		if receipt != nil {
			receipt.ResultRef = resultRel
		}

		var original struct {
			Findings []json.RawMessage `json:"findings"`
		}
		if err := json.Unmarshal(data, &original); err != nil {
			return fmt.Errorf("decode original Findings: %w", err)
		}
		findingArtifacts = make([]findingArtifact, 0, len(result.Findings))
		for i, finding := range result.Findings {
			// An unfinished responsibility may carry its already accepted
			// Finding into another handoff. Keep the original observation;
			// the new answer remains in this attempt's saved content.
			if existing := acceptedFindingForResponsibility(state, finding.FindingID, result.AssignmentID, finding.ClaimID, result.ReviewPlanID); existing {
				continue
			}
			findingBase := append([]string{".claude", "evidence"}, boundedReviewIdentity(runtimeID)...)
			findingBase = append(findingBase, fmt.Sprintf("g%d", generation), "findings")
			rel := boundedReviewArtifactPath(findingBase, finding.FindingID)

			if i >= len(original.Findings) {
				return fmt.Errorf("original Finding %s is unavailable", finding.FindingID)
			}
			bytes, err := marshalArtifact(original.Findings[i])
			if err != nil {
				return fmt.Errorf("encode finding %s: %w", finding.FindingID, err)
			}
			rel, err = publishResultArtifact(request.Context, root, state, rel, bytes)
			if err != nil {
				return err
			}
			findingArtifacts = append(findingArtifacts, findingArtifact{finding: finding, rel: rel})
		}

		return nil
	}

	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	resultRepoPath := repositoryPath(root, request.ResultPath)
	// The event identity is the original responsibility and the delivery
	// intent, never the Runtime write count. The typed receipt already carries
	// this shape, so both paths publish one identity for the same delivery.
	eventID, idempotencyKey := receiptEventIdentity(runtimeID, generation, ptr, &result)
	if request.issuedPacket != nil {
		eventID, idempotencyKey = receiptEventIdentity(runtimeID, generation, ptr, &result, request.issuedPacket.ID)
	}
	if receipt != nil {
		eventID = receipt.EventID
		idempotencyKey = receipt.IdempotencyKey
	}

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	phaseDurations := make(map[string]int64, 4)
	// Metrics persistence is outside the Runtime lock and cannot delay or
	// fail its candidate validation, pending publication or commit.
	defer func() { _ = metrics.RecordS7SubmitPhases(root, phaseDurations) }()
	snapshot, err := updateRuntime(store, request.ExpectedRevision, loopruntime.Mutation{
		EventID:            eventID,
		TransitionID:       "REVIEW-RESULT",
		Event:              "review_result_submitted",
		Actor:              "orchestrator",
		IdempotencyKey:     idempotencyKey,
		RuntimeID:          runtimeID,
		BaselineGeneration: generation,
		From:               cursor,
		To:                 cursor,
		EvidenceIDs:        append([]string{result.ResultID}, findingIDs(result.Findings)...),
		Message: fmt.Sprintf("Consumed Canonical ReviewResult %s for assignment %s (verdict %s)",
			result.ResultID, result.AssignmentID, result.Verdict),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			if err := validateIssuedResultRequest(root, state, request, &result, false, plan); err != nil {
				return err
			}

			if receipt != nil {
				receipt.AcceptedAfterRevision = intField(state["revision"]) + 1
				journal, _ := state["journal"].(map[string]any)
				receipt.AcceptedSequence = intField(journal["last_sequence"]) + 1
			}
			if err := prepareArtifacts(state); err != nil {
				return err
			}
			// RC-10 observability: per-phase durations of the submit CAS
			// transaction (loop_s7_submit_phase_ms{phase}). Best-effort — the
			// `_ =` discard mirrors recordRoundMetrics and never fails the
			// verb. phaseTotal timers cover the whole closure; seal/clean are
			// recorded by recordRoundMetrics consumers below, not here.
			// Completed phase samples are persisted together after Writer unlock.
			applyStart := time.Now()
			// Decide again under CAS: a concurrent Plan change cannot make
			// the old AssignmentID claim the new Plan's projection.
			originalOnly := request.issuedPacket != nil && !loopruntime.ReviewResultPacketCurrent(state, *request.issuedPacket) && !continuesCurrent
			if originalOnly {
				if err := applyOriginalResultEvidence(state, &result, resultRel, responsibility); err != nil {
					return err
				}
			} else if err := applyResultConsumption(state, plan, assignment, &result, resultRel, responsibility, generation, occurredAt); err != nil {
				return err
			}
			if receipt != nil {
				if request.issuedPacket != nil {
					if err := setIssuedResultReception(state, *request.issuedPacket, *receipt, &result); err != nil {
						return err
					}
				}
				if !originalOnly {
					if err := setAssignmentResultReceipt(state, assignment.AssignmentID, *receipt); err != nil {
						return err
					}
				}
			}
			phaseDurations["result_consumption"] = time.Since(applyStart).Milliseconds()
			findingsStart := time.Now()
			if err := applyFindings(state, &result, findingArtifacts, occurredAt); err != nil {
				return err
			}
			phaseDurations["findings"] = time.Since(findingsStart).Milliseconds()
			if originalOnly {
				return nil
			}
			// A stop verdict and both terminal round transitions close the
			// admission gate before this CAS returns. Promoting a queued
			// Assignment here would dispatch new work after P0/pause/seal.
			advanceStart := time.Now()
			if !late && !sealNow && !cleanNow && result.Verdict != "blocked" && result.Verdict != "req_change_required" && result.Verdict != "release_blocked" {
				if err := releaseQueuedReviewAssignments(state); err != nil {
					return err
				}
			}
			if err := advanceReviewerAgent(state, &result, resultRepoPath, occurredAt); err != nil {
				return err
			}
			phaseDurations["advance"] = time.Since(advanceStart).Milliseconds()
			reviewMap := state["review"].(map[string]any)
			lifecycleMap := state["lifecycle"].(map[string]any)
			if late {
				return nil
			}
			switch result.Verdict {
			case "req_change_required", "release_blocked":
				if sealNow {
					setPlanStatus(reviewMap, lifecycleMap, "observation_sealed")
					return nil
				}
				// One authoritative checkpoint in the verdict transaction;
				// TR-010/TR-011 then carry only the result evidence
				// (L3-S7 §9.2, §13.1 dual-carrier removal).
				pauseStart := time.Now()
				if err := capturePauseCheckpoint(state, "S7 review verdict: "+result.Verdict, occurredAt); err != nil {
					return err
				}
				phaseDurations["pause"] = time.Since(pauseStart).Milliseconds()
				setPlanStatus(reviewMap, lifecycleMap, "paused")
				return nil
			}
			if !late && len(result.Findings) > 0 {
				if status, _ := reviewMap["plan"].(map[string]any)["status"].(string); status == "running" {
					setPlanStatus(reviewMap, lifecycleMap, "cannot_clean")
				} else if status == "cannot_clean" && !sealNow {
					setPlanStatus(reviewMap, lifecycleMap, "discovery_draining")
				}
			}
			if late {
				return nil
			}
			if sealNow {
				setPlanStatus(reviewMap, lifecycleMap, "observation_sealed")
				return nil
			}
			if RequiredResponsibilitiesComplete(state) && len(RoundFindings(state)) == 0 {
				cleanNow = true
				setPlanStatus(reviewMap, lifecycleMap, "clean")
				delete(reviewMap, "clean_round")
			}

			return nil
		},
	})
	if err != nil {
		// A durable pending transaction may have committed despite a lost
		// response. Restore that original fact without touching unknown files.
		if receiptMode {
			completionContext, cancel := ResultCompletionContext(request.Context)
			defer cancel()
			current, readErr := store.WithContext(completionContext).Snapshot()
			if readErr == nil {
				if accepted, replayErr := replayResultRequest(root, current.State, requestWithContext(request, completionContext), &result, answerContent); accepted && replayErr == nil {
					return current, errExactResultReplay
				}
			}
		}
		return snapshot, err
	}
	return snapshot, nil
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

func parsePathEvidenceRef(ref string) (string, error) {
	return evidence.ParsePathReference(ref)
}

// validateVerdictConsistency keeps the verdict from contradicting the local
// claim results (L3-S7 §3.5).
func validateVerdictConsistency(result *Result, assignment *PlanAssignment) error {
	switch result.Verdict {
	case "pass":
		if len(result.Findings) > 0 || hasOwnedBlocked(result, assignment.ClaimIDs) {
			return fmt.Errorf("pass cannot carry Findings or unfinished blocked responsibilities")
		}
	case "finding":
		if len(result.Findings) == 0 {
			return fmt.Errorf("finding requires an actual Finding; use blocked for unfinished work without a Finding")
		}
	case "blocked", "req_change_required", "release_blocked":
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
	case "pass", "finding", "blocked", "req_change_required", "release_blocked":
		return nil, false
	case "fail":
		return fmt.Errorf("ReviewResult verdict=%q is not valid; use finding for an actual Finding or blocked for unfinished work without one. Valid values: pass, finding, blocked, req_change_required, release_blocked", raw), true
	}
	return nil, false
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
	resultRel, responsibility string,
	generation int,
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
	status := workstate.ReviewAssignmentActivity(row)
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

	claims, _ := reviewMap["claims"].(map[string]any)
	ownedClaims := make(map[string]bool, len(assignment.ClaimIDs))
	for _, id := range assignment.ClaimIDs {
		ownedClaims[id] = true
	}
	failFindings := map[string][]string{}
	for _, finding := range result.Findings {
		failFindings[finding.ClaimID] = append(failFindings[finding.ClaimID], finding.FindingID)
	}
	for _, claimResult := range result.ClaimResults {
		if !ownedClaims[claimResult.ClaimID] {
			continue
		}
		claimRow, _ := claims[claimResult.ClaimID].(map[string]any)
		if claimRow == nil {
			return fmt.Errorf("claim %s is missing from the runtime projection", claimResult.ClaimID)
		}
		if claimRow["applicability"] == "not_applicable" {
			return fmt.Errorf("claim %s is not_applicable in the plan; N/A is a plan disposition, never a Reviewer conclusion (L3-S7 §9.3)", claimResult.ClaimID)
		}
		switch claimResult.Conclusion {
		case "pass":
			claimRow["disposition"] = "pass"
		case "fail":
			claimRow["disposition"] = "finding"
		default:
			continue // An explicitly unfinished slot is neither PASS nor Finding.
		}
		claimRow["result_id"] = result.ResultID
		ids := make([]any, 0, len(failFindings[claimResult.ClaimID]))
		for _, id := range failFindings[claimResult.ClaimID] {
			ids = append(ids, id)
		}
		claimRow["finding_ids"] = ids
	}
	if err := applyBlockedDispositions(claims, assignment, result); err != nil {
		return err
	}
	activity, event := "consumed", "review_result_submitted"
	if (result.Verdict != "pass" && result.Verdict != "finding") || hasOwnedBlocked(result, assignment.ClaimIDs) {
		activity, event = "blocked", "review_assignment_blocked"
	}
	if err := workstate.RecordReviewAssignmentActivity(state, row, activity, event); err != nil {
		return err
	}
	row["result_ref"] = resultRel
	delete(row, "result_verdict")

	return appendEvidence(state, map[string]any{
		"id":                  result.ResultID,
		"kind":                "review_result",
		"path":                resultRel,
		"status":              "valid",
		"baseline_generation": generation,
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
}

// applyFindings appends immutable finding rows and their evidence index
// entries. Finding identity is global: an id seen in any round collides.
func applyFindings(
	state map[string]any,
	result *Result,
	artifacts []findingArtifact,
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
			"claim_id":         artifact.finding.ClaimID,
			"assignment_id":    result.AssignmentID,
			"lens":             artifact.finding.Lens,
			"severity":         artifact.finding.Severity,
			"observation_mode": artifact.finding.ObservationMode,
			"original_finder":  result.ProducerAgentID,
			"review_plan_id":   result.ReviewPlanID,
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
			"status":              "valid",
			"baseline_generation": result.BaselineGeneration,
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

// advanceReviewerAgent records the accepted ReviewResult as the reviewer's
// delivery event. The original Assignment receipt is the authority; a
// separate generic PLAN checkpoint is not required for a ReviewAssignment.
func advanceReviewerAgent(state map[string]any, result *Result, resultRepoPath string, occurredAt time.Time) error {
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	assignment, _ := assignments[result.AssignmentID].(map[string]any)
	if assignment == nil || assignment["agent_id"] != result.ProducerAgentID || (workstate.ReviewAssignmentActivity(assignment) != "consumed" && workstate.ReviewAssignmentActivity(assignment) != "blocked") {
		return fmt.Errorf("ReviewResult %s has no matching consumed Reviewer Assignment", result.ResultID)
	}
	if workstate.ReviewAssignmentActivity(assignment) == "blocked" {
		return nil // An accepted handoff does not finish its remaining work.
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent["id"] != result.ProducerAgentID {
			continue
		}
		currentState := workstate.AgentActivity(agent)
		if currentState == "reported" || currentState == "done" {
			return nil
		}
		if !workstate.AgentStarted(stringField(agent["state"])) || currentState == "stopped" || currentState == "cancelled" || currentState == "queued" {
			return fmt.Errorf("reviewer Agent %s is not an active owner of the consumed ReviewResult Assignment", result.ProducerAgentID)
		}
		if err := workstate.RecordAgentActivity(state, agent, "reported", "completion_reported"); err != nil {
			return err
		}
		agent["blocker_resolved_ref"] = nil
		if resultRepoPath != "" {
			agent["completion_reported_ref"] = resultRepoPath
		}
		agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
		return nil
	}
	return fmt.Errorf("Agent %s is not registered", result.ProducerAgentID)
}

// appendEvidence registers one evidence index row, rejecting id collisions.
func appendEvidence(state map[string]any, entry map[string]any) error {
	if identity := activeReviewIdentity(state); identity.PlanID != "" {
		entry["review_plan_id"] = identity.PlanID
		delete(entry, "review_round")
	}

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
		if row == nil || workstate.ReviewAssignmentActivity(row) != "planned" || stringField(row["queue_reason"]) == "" {
			continue
		}
		agentID := stringField(row["queued_agent_id"])
		if agentID == "" || reviewAssignmentLockConflict(id, row, assignments) {
			continue
		}
		if err := workstate.RecordReviewAssignmentActivity(state, row, "dispatched", "review_result_submitted"); err != nil {
			return err
		}
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
		if err := wakeQueuedReviewAgent(state, agentID); err != nil {
			return err
		}
	}
	return nil
}

func wakeQueuedReviewAgent(state map[string]any, agentID string) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent != nil && agent["id"] == agentID && workstate.AgentActivity(agent) == "queued" {
			return workstate.RecordAgentActivity(state, agent, "reading", "reviewer_dispatched")
		}
	}
	return nil
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
	switch workstate.ReviewAssignmentActivity(row) {
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

// staleReviewPlanAfterDrift suspends admission when the original baseline has
// drifted or a required control source cannot be verified. The concrete error
// distinguishes those cases; stale alone does not assert product drift. The
// mutation is plan-only: lifecycle phase and accepted results are preserved.
func staleReviewPlanAfterDrift(
	ctx context.Context,
	root, statePath, journalPath string,
	current map[string]any,
	driftErr error,
) (loopruntime.Snapshot, error) {
	// A budget/cancellation failure says nothing about source freshness and
	// must never authorize a new stale-plan business mutation.
	if errors.Is(driftErr, context.Canceled) || errors.Is(driftErr, context.DeadlineExceeded) {
		return loopruntime.Snapshot{}, driftErr
	}
	plan := PlanPointerFromState(current)
	if plan == nil {
		return loopruntime.Snapshot{}, driftErr
	}
	staleFailure := func() error {
		return fmt.Errorf("%w; ReviewPlan %s is unavailable. Main restores the actual plan source and continues the original responsibility, preserving accepted answers", driftErr, plan.PlanID)
	}
	if plan.Status == "stale" {
		return loopruntime.Snapshot{}, staleFailure()
	}
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{}
	if lifecycle != nil {
		cursor = map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	}
	runtimeID, _ := current["runtime_id"].(string)
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snapshot, err := updateRuntime(store, -1, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-review-plan-stale-%s-r%d", plan.PlanID, plan.Revision),
		TransitionID:   "REVIEW-PLAN-STALE",
		Event:          "review_plan_stale",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:review-plan-stale:%s:%d", plan.PlanID, plan.Revision),
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
	return snapshot, staleFailure()
}

// ---------------------------------------------------------------------------
// round consumer projections
// ---------------------------------------------------------------------------

// projectDispositions returns the claim disposition map as it will look
// after this result is consumed.
func projectDispositions(state map[string]any, assignment *PlanAssignment, result *Result) map[string]ClaimDisposition {
	dispositions := Dispositions(state)
	owned := make(map[string]bool, len(assignment.ClaimIDs))
	for _, id := range assignment.ClaimIDs {
		owned[id] = true
	}
	failByClaim := map[string]bool{}
	for _, finding := range result.Findings {
		failByClaim[finding.ClaimID] = true
	}
	for _, claimResult := range result.ClaimResults {
		if !owned[claimResult.ClaimID] {
			continue
		}
		disp := dispositions[claimResult.ClaimID]
		disp.ResultID = result.ResultID
		if failByClaim[claimResult.ClaimID] {
			disp.Disposition = "finding"
		} else {
			disp.Disposition = "pass"
		}
		dispositions[claimResult.ClaimID] = disp
	}
	projectBlockedDispositions(dispositions, assignment, result)
	return dispositions
}

func roundCompleteWith(dispositions map[string]ClaimDisposition) bool {
	for _, disp := range dispositions {
		if disp.Applicability != "required" {
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

func capturePauseCheckpoint(state map[string]any, reason string, occurredAt time.Time) error {
	if existing, ok := state["pause"]; ok && existing != nil {
		return fmt.Errorf("pause checkpoint already exists; would overwrite")
	}
	lifecycle, ok := state["lifecycle"].(map[string]any)
	if !ok {
		return fmt.Errorf("lifecycle missing")
	}

	state["pause"] = map[string]any{
		"from_state":            lifecycle["state"],
		"from_phase":            lifecycle["phase"],
		"phase_revision":        intField(lifecycle["phase_revision"]),
		"baseline_generation":   baselineGeneration(state),
		"reason":                reason,
		"required_human_action": "review the blocking finding or REQ change, then resume or re-lock the REQ",
		"paused_at":             occurredAt.UTC().Format(time.RFC3339Nano),
	}
	if identity := activeReviewIdentity(state); identity.PlanID != "" {
		pause := state["pause"].(map[string]any)
		pause["review_plan_id"] = identity.PlanID
	}
	return nil
}

func stateDocuments(state map[string]any) []any {
	documents, _ := state["documents"].([]any)
	return documents
}

// Existing identifiers can be long after reversible responsibility encoding.
// Splitting fixed-size components avoids an author-visible length restriction.
func boundedReviewIdentity(id string) []string {
	var parts []string
	for len(id) > 120 {
		parts = append(parts, id[:120])
		id = id[120:]
	}
	return append(parts, id)
}
func boundedReviewArtifactPath(base []string, id string) string {
	parts := boundedReviewIdentity(id)
	parts[len(parts)-1] += ".json"
	return filepath.ToSlash(filepath.Join(append(base, parts...)...))
}

// ---------------------------------------------------------------------------
// artifact IO
// ---------------------------------------------------------------------------

const reviewResultInputLimit int64 = 20 << 20

// readReviewResultInput accepts explicit paths from Main or worker worktrees,
// but bounds both memory use and parsing work. The opened file is checked (not
// just the pre-open path) so a non-regular target cannot be treated as an
// answer file.
func readReviewResultInput(path string) ([]byte, error) {
	pathInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("ReviewResult input must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("ReviewResult input must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, reviewResultInputLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > reviewResultInputLimit {
		return nil, fmt.Errorf("ReviewResult input exceeds the %d-byte limit", reviewResultInputLimit)
	}
	return data, nil
}

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
			return fmt.Errorf("evidence artifact %s already exists but was never indexed by the runtime; the original receiver must resolve staged ownership and pending references before publication; preserve the file and original delivery, without author deletion or artifact-ID changes", rel)
		}
		return fmt.Errorf("write %s without overwrite: %w", rel, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(abs)
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync %s: %w", rel, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", rel, err)
	}
	if err := loopruntime.SyncDirectory(filepath.Dir(abs)); err != nil {
		return fmt.Errorf("sync artifact directory for %s: %w", rel, err)
	}
	return nil
}

func repositoryPath(root, path string) string {
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !filepath.IsAbs(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

func acceptedFindingForResponsibility(state map[string]any, findingID, assignmentID, claimID, planID string) bool {
	entities, _ := state["entities"].(map[string]any)
	findings, _ := entities["findings"].([]any)
	for _, raw := range findings {
		row, _ := raw.(map[string]any)
		if row["finding_id"] == findingID && row["assignment_id"] == assignmentID && row["claim_id"] == claimID && row["review_plan_id"] == planID {
			return true
		}
	}
	return false
}
