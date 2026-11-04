package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// PlanRequest drives original ReviewPlan registration.
type PlanRequest struct {
	Context          context.Context `json:"-"`
	ExpectedRevision int
	PlanPath         string
	AnswerBytes      []byte `json:"-"`
	OccurredAt       time.Time
}

// ValidatePlanArtifactForRegistration shares the original Plan's observable
// content and responsibility checks across S7 entry routes. Main owns the
// professional coverage and asset decisions.
func ValidatePlanArtifactForRegistration(root string, plan *Plan, state map[string]any, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidatePlan(plan); err != nil {
		return fmt.Errorf("ReviewPlan content and responsibility: %w", err)
	}

	return nil
}

// RegisterPlan receives the first actual S7 professional Plan, saves its
// original content and responsibility projection under the existing Writer,
// and permits Main to complete or change that plan naturally.
func RegisterPlan(
	root, statePath, journalPath string,
	request PlanRequest,
) (loopruntime.Snapshot, error) {
	if request.PlanPath == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("--file is required")
	}
	data := request.AnswerBytes
	var err error
	if data == nil {
		data, err = os.ReadFile(request.PlanPath)
	}
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read ReviewPlan: %w", err)
	}

	if request.OccurredAt.IsZero() {
		request.OccurredAt = time.Now().UTC()
	}
	if request.Context == nil {
		request.Context = context.Background()
	}
	request.Context = buildsurface.WithReadCache(request.Context)
	input := &planRegistrationInput{data: data}
	snapshot, err := registerPlanBytes(root, statePath, journalPath, request, input)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || request.Context.Err() == context.Canceled || input.initial == nil {
		return snapshot, err
	}
	// This bounded tail belongs to the same original registration. It never
	// reopens the author's file, refreshes their expected revision, or loops.
	ctx, cancel := reviewDeadlineTail(request.Context, 400*time.Millisecond, 400*time.Millisecond)
	defer cancel()
	defer metrics.Span(ctx, "review/register_plan/deadline_tail")()
	request.Context = ctx
	recovered, recoveryErr := resumePlanRegistration(root, statePath, journalPath, request, input)
	if recoveryErr == nil {
		return recovered, nil
	}
	return snapshot, fmt.Errorf("%w; internal ReviewPlan handoff remains incomplete: %v", err, recoveryErr)
}

// data is the one author-file read, then the one prepared answer. Later
// attempts reuse it verbatim; initial retains the original business cursor.
type planRegistrationInput struct {
	data          []byte
	prepared      bool
	initial       map[string]any
	commitStarted bool
	retry         bool
}

func registerPlanBytes(root, statePath, journalPath string, request PlanRequest, input *planRegistrationInput) (loopruntime.Snapshot, error) {
	if err := request.Context.Err(); err != nil {
		return loopruntime.Snapshot{}, err
	}
	data := input.data

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	if input.initial == nil {
		input.initial = current
	}
	if input.retry && (!samePlanRegistrationCoordinate(input.initial, current) || intField(current["revision"]) != intField(input.initial["revision"])) {
		return loopruntime.Snapshot{}, loopruntime.ErrStaleRevision
	}
	input.data, input.prepared = data, true

	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return loopruntime.Snapshot{}, &PlanAnswerError{fmt.Errorf("ReviewPlan JSON: %w", err)}
	}
	if err := schema.NewValidator(root).ValidateBytes("review-plan.schema.json", data); err != nil {
		return loopruntime.Snapshot{}, &PlanAnswerError{fmt.Errorf("ReviewPlan schema: %w", err)}
	}
	input.data = data
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode ReviewPlan: %w", err)
	}
	// Receive the author's expression, without certifying professional coverage.
	if err := ValidatePlan(&plan); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan content and responsibility: %w", err)
	}

	if request.ExpectedRevision >= 0 && intField(current["revision"]) != request.ExpectedRevision {
		return loopruntime.Snapshot{}, loopruntime.ErrStaleRevision
	}
	commitRevision := currentCommitRevision(request.ExpectedRevision, current)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	if state, _ := lifecycle["state"].(string); state != "verification" {
		return loopruntime.Snapshot{}, fmt.Errorf("a ReviewPlan can only be registered in the verification stage (current state: %s); enter S7 via TR-006/TR-012 first", lifecycle["state"])
	}
	if reviewmeta.CompareIdentity(plan.Identity(), activeReviewIdentity(current)) == reviewmeta.Different {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan %s does not belong to the active review entry", plan.ReviewPlanID)
	}
	if generation := baselineGeneration(current); plan.BaselineGeneration != generation {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan declares baseline_generation %d but the runtime is at generation %d", plan.BaselineGeneration, generation)
	}
	if existing := PlanPointerFromState(current); existing != nil && reviewmeta.CompareIdentity(reviewmeta.ArtifactIdentity(existing.PlanID), activeReviewIdentity(current)) == reviewmeta.Same {
		seed, err := canRegisterS9Seed(root, current, &plan)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		if !seed {
			return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan %s is already registered (status %s); update and save the original ReviewPlan questionnaire or enter a new independent verification", existing.PlanID, existing.Status)
		}
	}

	workspace := ""
	if plan.VerificationArtifactWorkspace != nil {
		workspace = *plan.VerificationArtifactWorkspace
	}
	// Reserve the actual Plan path only within the original Writer. An
	// unregistered occupant is never read or claimed as this responsibility.
	planRel := ""
	planBytes := append(canonicalJSON(data), '\n')
	publishedPlan := false
	publishPlan := func() error {
		parent, _, err := workpacket.OpenParent(root, ".claude/review/s7-plan-probe.json", true)
		if err != nil {
			return err
		}
		parent.Close()
		for attempt := 0; ; attempt++ {
			if err := request.Context.Err(); err != nil {
				return err
			}
			base := []string{".claude", "review", "plans"}
			if attempt > 0 {
				base[2] = fmt.Sprintf("plans-candidate-%d", attempt)
			}
			planRel = boundedReviewArtifactPath(base, plan.ReviewPlanID)
			created, err := workpacket.PublishNewBounded(root, planRel, planBytes, 20<<20)
			if err != nil {
				return err
			}
			if created {
				publishedPlan = true
				return nil
			}
		}
	}

	runtimeID, _ := current["runtime_id"].(string)
	occurredAt := request.OccurredAt
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	commitExpected := request.ExpectedRevision
	if input.retry && commitExpected < 0 {
		// An omitted caller revision cannot let the retry adopt a newer one.
		commitExpected = intField(input.initial["revision"])
	}
	input.commitStarted = true
	snapshot, err := updateRuntime(store, commitExpected, loopruntime.Mutation{
		EventID:        reviewPlanEventID(plan.ReviewPlanID),
		TransitionID:   "REVIEW-PLAN",
		Event:          "review_plan_registered",
		Actor:          "orchestrator",
		IdempotencyKey: reviewPlanIdempotencyKey(plan.ReviewPlanID),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             map[string]any{"state": "verification", "phase": "running"},
		Message: fmt.Sprintf("Registered ReviewPlan %s (%d claims, %d assignments, e2e=%s)",
			plan.ReviewPlanID, len(plan.Claims), len(plan.Assignments), plan.E2ECoverageState),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			if existing := PlanPointerFromState(state); existing != nil && reviewmeta.CompareIdentity(reviewmeta.ArtifactIdentity(existing.PlanID), activeReviewIdentity(state)) == reviewmeta.Same {
				seed, err := canRegisterS9Seed(root, state, &plan)
				if err != nil {
					return err
				}
				if !seed {
					return fmt.Errorf("original S9 seed changed before first professional registration")
				}
			}

			// Another failed caller may have safely cleaned an unreferenced
			// preflight artifact. Restore only these exact bytes under the
			// original Writer before publishing their pointer.
			if err := publishPlan(); err != nil {
				return err
			}
			return ApplyRegisteredPlanProjection(state, plan, planRel, workspace, occurredAt)
		},
	})
	if err != nil {
		if !publishedPlan {
			return snapshot, err
		}
		// Error does not prove the original CAS is absent. The existing Writer
		// fence preserves both durable pending and any concurrent accepted record.
		removed, cleanupErr := store.RemoveUnreferencedArtifact(loopruntime.ArtifactCleanupRequest{ExpectedRevision: commitRevision, ArtifactPath: planRel, ArtifactBytes: planBytes, ReferencedPaths: stateArtifactReferences(current, planRel)})
		if cleanupErr != nil {
			return snapshot, fmt.Errorf("%w; original Writer retained Plan/pending recovery before cleanup: %v", err, cleanupErr)
		}
		if !removed {
			return snapshot, fmt.Errorf("%w; cleanup was deferred by the original Writer fence; the original Plan and pending facts remain preserved for receiver recovery", err)
		}
		// An unregistered empty verification directory carries no authority; keep
		// it rather than racing another caller's preparation with a directory unlink.

		return snapshot, err
	}
	return snapshot, nil
}

// reviewPlanEventID names the registration of one ReviewPlan by the Plan
// responsibility itself. One Plan is registered once per review round, so the
// Plan identity already separates the registrations; the Runtime write count
// is never part of an identity (G18 §1.1).
func reviewPlanEventID(planID string) string {
	if strings.TrimSpace(planID) == "" {
		return ""
	}
	return "evt-review-plan-" + planID
}

// legacyReviewPlanEventID recognizes the historical shape of the registration
// event of one Plan. The old id is located by its saved prefix and numeric
// suffix — the Runtime write count it carried is never recomputed — so a
// journal written before the identity change keeps working (G18 §1.4).
func legacyReviewPlanEventID(eventID, planID string) bool {
	prefix := "evt-review-plan-" + planID + "-r"
	if planID == "" || !strings.HasPrefix(eventID, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(eventID, prefix)
	if suffix == "" {
		return false
	}
	for _, digit := range suffix {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// reviewPlanIdempotencyKey names the registration intent: the Plan identity is
// the responsibility coordinate and the intent key of this reception
// (G18 §1.3). Two Plans never share it.
func reviewPlanIdempotencyKey(planID string) string {
	if strings.TrimSpace(planID) == "" {
		return ""
	}
	return "runtime:review-plan:" + planID
}

func samePlanRegistrationCoordinate(original, current map[string]any) bool {
	lifecycle, _ := current["lifecycle"].(map[string]any)
	return stringField(original["runtime_id"]) != "" && original["runtime_id"] == current["runtime_id"] &&
		baselineGeneration(original) == baselineGeneration(current) && reviewmeta.CompareIdentity(activeReviewIdentity(original), activeReviewIdentity(current)) != reviewmeta.Different &&
		lifecycle["state"] == "verification"
}

func resumePlanRegistration(root, statePath, journalPath string, request PlanRequest, input *planRegistrationInput) (loopruntime.Snapshot, error) {
	// Only the original Writer may recover durable pending. An observation
	// Store, staged file, or matching plan id alone is never an acceptance.
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	current, err := store.Snapshot()
	if err != nil {
		return current, err
	}
	if err := request.Context.Err(); err != nil {
		return current, err
	}
	if !samePlanRegistrationCoordinate(input.initial, current.State) {
		return current, fmt.Errorf("original ReviewPlan runtime, generation, round or stage changed")
	}
	if input.commitStarted && input.prepared {
		var plan Plan
		if err := json.Unmarshal(input.data, &plan); err != nil {
			return current, err
		}
		planBytes := append(canonicalJSON(input.data), '\n')
		ptr := PlanPointerFromState(current.State)
		transition, _ := current.State["last_transition"].(map[string]any)
		savedEventID, _ := transition["event_id"].(string)
		newReception := current.Revision > intField(input.initial["revision"]) &&
			transition["transition_id"] == "REVIEW-PLAN" &&
			(savedEventID == reviewPlanEventID(plan.ReviewPlanID) ||
				legacyReviewPlanEventID(savedEventID, plan.ReviewPlanID))
		if newReception && ptr != nil && ptr.PlanID == plan.ReviewPlanID && ptr.Path != "" && ptr.Revision == 1 &&
			reviewmeta.CompareIdentity(plan.Identity(), activeReviewIdentity(input.initial)) != reviewmeta.Different &&
			plan.BaselineGeneration == baselineGeneration(input.initial) {
			stored, err := workpacket.ReadFile(root, ptr.Path, 20<<20)
			if err != nil {
				return current, err
			}
			if err := request.Context.Err(); err != nil {
				return current, err
			}
			if !bytes.Equal(stored, planBytes) {
				return current, fmt.Errorf("original registered ReviewPlan bytes changed at %s", ptr.Path)
			}
			return current, nil
		}
	}
	if current.Revision != intField(input.initial["revision"]) {
		return current, loopruntime.ErrStaleRevision
	}
	input.retry = true
	return registerPlanBytes(root, statePath, journalPath, request, input)
}

// canonicalJSON re-marshals the plan so the pinned bytes are stable.
func canonicalJSON(data []byte) []byte {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return data
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return data
	}
	return out
}

// mergedResourceLocks returns the sorted unique non-empty union of the
// assignment's declared resource_locks and the resource_locks declared on
// each of its Claims. Duplicate entries collapse; the result is the keys
// the runtime uses for conflict detection at dispatch (L3-S7 §4.5, L4
// §6.2). The function tolerates a nil plan and unknown claim IDs (defensive
// against rev that runs before the claim map is rebuilt).
func mergedResourceLocks(assignmentLocks []string, plan *Plan, claimIDs []string) []any {
	seen := map[string]bool{}
	out := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, lock := range assignmentLocks {
		add(lock)
	}
	if plan != nil {
		byID := map[string]Claim{}
		for _, claim := range plan.Claims {
			byID[claim.ClaimID] = claim
		}
		for _, claimID := range claimIDs {
			if claim, ok := byID[claimID]; ok {
				for _, lock := range claim.ResourceLocks {
					add(lock)
				}
			}
		}
	}
	sort.Strings(out)
	result := make([]any, len(out))
	for i, value := range out {
		result[i] = value
	}
	return result
}
