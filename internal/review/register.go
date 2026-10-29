package review

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

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// PlanRequest drives `runtime review-plan` registration.
type PlanRequest struct {
	Context          context.Context `json:"-"`
	ExpectedRevision int
	PlanPath         string
	OccurredAt       time.Time
}

// ValidatePlanArtifactForRegistration contains the artifact-level checks that
// every S7 registration path must consume. The normal ReviewPlan command and
// the S9 TR-012 handoff seed both enter the same control plane, so neither is
// allowed to use a lighter frozen-subject or regression-asset validation path.
// Runtime-coordinate checks remain in RegisterPlan because the handoff builds
// its next-round projection inside its own CAS transaction.
func ValidatePlanArtifactForRegistration(root string, plan *Plan, state map[string]any, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidatePlan(plan); err != nil {
		return fmt.Errorf("ReviewPlan coverage: %w", err)
	}
	if err := verifyFrozenSubjects(root, plan, state, ctx); err != nil {
		return fmt.Errorf("ReviewPlan frozen subject baseline: %w", err)
	}
	if err := validateKnownE2ECaseCoverage(ctx, root, state, plan); err != nil {
		return err
	}
	if err := verifyRegressionAssetFingerprints(root, plan); err != nil {
		return err
	}
	return nil
}

// RegisterPlan is the S7 entry verb: it schema-validates the Planner's
// ReviewPlan, proves exact-set Claim coverage (ValidatePlan), pins the plan
// into the shared control plane under one CAS, initializes the claim /
// assignment disposition projections, and moves the verification phase to
// running (L3-S7 §4.1, §11.1). One round has one plan; the controlled
// revision path is P2 (L3-S7 §13.4).
func RegisterPlan(
	root, statePath, journalPath string,
	request PlanRequest,
) (loopruntime.Snapshot, error) {
	if request.PlanPath == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("--file is required")
	}
	data, err := os.ReadFile(request.PlanPath)
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
	if !input.prepared {
		data, err = preparePlanAssets(root, data, current, request.Context)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("prepare ReviewPlan assets: %w", err)
		}
		input.data, input.prepared = data, true
	}
	if err := schema.NewValidator(root).ValidateBytes("review-plan.schema.json", data); err != nil {
		return loopruntime.Snapshot{}, &PlanAnswerError{fmt.Errorf("ReviewPlan schema: %w", err)}
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode ReviewPlan: %w", err)
	}
	if err := validateAuthoredBehaviorSurfaces(&plan, nil); err != nil {
		return loopruntime.Snapshot{}, err
	}
	// The original CAS below checks the complete frozen baseline. Preflight
	// retains professional coverage and regression assets without repeating it.
	if err := ValidatePlan(&plan); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan coverage: %w", err)
	}
	if err := verifyRegressionAssetFingerprints(root, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if request.ExpectedRevision >= 0 && intField(current["revision"]) != request.ExpectedRevision {
		return loopruntime.Snapshot{}, loopruntime.ErrStaleRevision
	}
	commitRevision := currentCommitRevision(request.ExpectedRevision, current)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	if state, _ := lifecycle["state"].(string); state != "verification" {
		return loopruntime.Snapshot{}, fmt.Errorf("a ReviewPlan can only be registered in the verification stage (current state: %s); enter S7 via TR-006/TR-012 first", lifecycle["state"])
	}
	round := currentReviewRound(current)
	if round < 1 {
		return loopruntime.Snapshot{}, fmt.Errorf("no active review round; TR-006/TR-012 start the round")
	}
	if plan.ReviewRound != round {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan declares review_round %d but the runtime is at round %d", plan.ReviewRound, round)
	}
	if generation := baselineGeneration(current); plan.BaselineGeneration != generation {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan declares baseline_generation %d but the runtime is at generation %d", plan.BaselineGeneration, generation)
	}
	if existing := PlanPointerFromState(current); existing != nil && existing.ReviewRound == round {
		seed, err := canRegisterS9Seed(root, current, &plan)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		if !seed {
			return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan %s is already registered for round %d (status %s); revise it via `runtime review-plan revise` (one controlled revision per round, L3-S7 §5.3) or start a new round", existing.PlanID, round, existing.Status)
		}
	}
	// Coverage diff at registration (L3-S7 §4.4): every current-generation
	// TASK must be claimed by at least one Claim's source_refs.
	if err := ValidatePlanTaskCoverage(current, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if !buildsurface.HasCanonical(current) {
		if err := validateCoverageInventory(root, current, &plan, request.Context); err != nil {
			return loopruntime.Snapshot{}, err
		}
	}
	if err := validateKnownE2ECaseCoverage(request.Context, root, current, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateRepairRoundBaseline(root, current, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	// E2E cold start: create and fingerprint the isolated write surface so
	// result submit / round close can bind it exactly. A prepared directory is
	// not an execution authority; only the committed Plan grants that binding.
	workspace := ""
	if plan.VerificationArtifactWorkspace != nil {
		workspace = *plan.VerificationArtifactWorkspace
	}

	canonical := buildsurface.HasCanonical(current)
	artifactDigest := ""
	if !canonical {
		artifactDigest, err = prepareVerificationWorkspace(root, &plan, request.Context)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
	}

	// Pin the plan into the shared control plane directory; the runtime
	// stores path+sha256 and every consumer hash-verifies on load.
	planRel := filepath.ToSlash(filepath.Join(".claude", "review", "plans", plan.ReviewPlanID+".json"))
	planBytes := append(canonicalJSON(data), '\n')
	publishedPlan := false
	publishPlan := func() error {
		stored, err := workpacket.PublishBounded(root, planRel, planBytes, 20<<20)
		if err != nil {
			return err
		}
		if !bytes.Equal(stored, planBytes) {
			return fmt.Errorf("staged ReviewPlan at %s differs from this answer; Main preserves the original artifact and inspects its receipt/source before revision or recovery", planRel)
		}
		publishedPlan = true
		return nil
	}
	if !canonical {
		if err := publishPlan(); err != nil {
			return loopruntime.Snapshot{}, err
		}
	}
	planSHA := sha256Of(planBytes)

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
		EventID:        fmt.Sprintf("evt-review-plan-%s-r%d", plan.ReviewPlanID, commitRevision+1),
		TransitionID:   "REVIEW-PLAN",
		Event:          "review_plan_registered",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:review-plan:%s:%d", plan.ReviewPlanID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             map[string]any{"state": "verification", "phase": "running"},
		Message: fmt.Sprintf("Registered ReviewPlan %s for round %d (%d claims, %d assignments, e2e=%s)",
			plan.ReviewPlanID, round, len(plan.Claims), len(plan.Assignments), plan.E2ECoverageState),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			if existing := PlanPointerFromState(state); existing != nil && existing.ReviewRound == round {
				seed, err := canRegisterS9Seed(root, state, &plan)
				if err != nil {
					return err
				}
				if !seed {
					return fmt.Errorf("original S9 seed changed before first professional registration")
				}
			}
			if buildsurface.HasCanonical(state) {
				// This original gate retains its source diagnostics and proves the
				// complete denominator against fresh sources inside the Writer.
				if err := validateCoverageInventory(root, state, &plan, request.Context); err != nil {
					return err
				}
				if err := verifyCanonicalFrozenWorktree(root, &plan, state, request.Context); err != nil {
					return fmt.Errorf("canonical product baseline changed before ReviewPlan commit: %w", err)
				}
			} else if err := verifyFrozenSubjects(root, &plan, state, request.Context); err != nil {
				return fmt.Errorf("canonical product baseline changed before ReviewPlan commit: %w", err)
			}
			if err := validateKnownE2ECaseCoverage(store.Context(), root, state, &plan); err != nil {
				return err
			}
			if err := verifyRegressionAssetFingerprints(root, &plan); err != nil {
				return err
			}
			if err := store.Context().Err(); err != nil {
				return err
			}
			if canonical {
				// Reject invalid canonical sources/coverage before creating a
				// workspace or publishing the unaccepted Plan.
				artifactDigest, err = prepareVerificationWorkspace(root, &plan, request.Context)
				if err != nil {
					return err
				}
			}
			// Another failed caller may have safely cleaned an unreferenced
			// preflight artifact. Restore only these exact bytes under the
			// original Writer before publishing their pointer.
			if err := publishPlan(); err != nil {
				return err
			}
			return ApplyRegisteredPlanProjection(state, plan, planRel, planSHA, round, workspace, artifactDigest, occurredAt)
		},
	})
	if err != nil {
		if canonical && !publishedPlan {
			// A validation refusal has not staged this attempt. Preserve any
			// retained artifact from an earlier attempt instead of cleaning it.
			return snapshot, err
		}
		// Error does not prove the original CAS is absent. The existing Writer
		// fence preserves both durable pending and any concurrent accepted record.
		removed, cleanupErr := removeStagedArtifactIfUnreferenced(store, current, commitRevision, planRel, planSHA)
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

func samePlanRegistrationCoordinate(original, current map[string]any) bool {
	lifecycle, _ := current["lifecycle"].(map[string]any)
	return stringField(original["runtime_id"]) != "" && original["runtime_id"] == current["runtime_id"] &&
		baselineGeneration(original) == baselineGeneration(current) && currentReviewRound(original) == currentReviewRound(current) &&
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
		planRel := filepath.ToSlash(filepath.Join(".claude", "review", "plans", plan.ReviewPlanID+".json"))
		ptr := PlanPointerFromState(current.State)
		original := PlanPointerFromState(input.initial)
		newReception := current.Revision > intField(input.initial["revision"])
		if original != nil && original.Path == planRel && original.SHA256 == sha256Of(planBytes) {
			// An untouched TR-012 seed can already pin these bytes. Its
			// pointer alone cannot prove professional registration happened.
			transition, _ := current.State["last_transition"].(map[string]any)
			newReception = newReception && transition["transition_id"] == "REVIEW-PLAN" &&
				transition["event_id"] == fmt.Sprintf("evt-review-plan-%s-r%d", plan.ReviewPlanID, intField(input.initial["revision"])+1)
		}
		if newReception && ptr != nil && ptr.PlanID == plan.ReviewPlanID && ptr.Path == planRel && ptr.SHA256 == sha256Of(planBytes) && ptr.Revision == 1 && ptr.ReviewRound == plan.ReviewRound && plan.ReviewRound == currentReviewRound(input.initial) && plan.BaselineGeneration == baselineGeneration(input.initial) {
			stored, err := workpacket.ReadFile(root, planRel, 20<<20)
			if err != nil {
				return current, err
			}
			if err := request.Context.Err(); err != nil {
				return current, err
			}
			if !bytes.Equal(stored, planBytes) {
				return current, fmt.Errorf("original registered ReviewPlan bytes changed at %s", planRel)
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

// New authoring must resolve applicability before the immutable Plan is pinned.
// Frozen historical assignments without this optional field retain their
// original browser responsibility contract during recovery and revision.
func validateAuthoredBehaviorSurfaces(plan, previous *Plan) error {
	for _, a := range plan.Assignments {
		if a.Lens != "e2e" || a.BehaviorSurface != "" {
			continue
		}
		legacy := false
		if previous != nil {
			for _, old := range previous.Assignments {
				if old.AssignmentID == a.AssignmentID && old.Lens == "e2e" && old.BehaviorSurface == "" {
					legacy = true
				}
			}
		}
		if !legacy {
			return &PlanAnswerError{fmt.Errorf("assignment %s is missing behavior_surface; Main must set browser or non_browser from explicit behavior sources in this original ReviewPlan answer before registration; ui_impact and method prose do not decide this field", a.AssignmentID)}
		}
	}
	return nil
}
