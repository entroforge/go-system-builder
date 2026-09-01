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
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// PlanRequest drives `runtime review-plan` registration.
type PlanRequest struct {
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
func ValidatePlanArtifactForRegistration(root string, plan *Plan) error {
	if err := ValidatePlan(plan); err != nil {
		return fmt.Errorf("ReviewPlan coverage: %w", err)
	}
	if err := verifyFrozenSubjects(root, plan); err != nil {
		return fmt.Errorf("ReviewPlan frozen subject baseline: %w", err)
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
	if err := schema.NewValidator(root).ValidateBytes("review-plan.schema.json", data); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan schema: %w", err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode ReviewPlan: %w", err)
	}
	if err := ValidatePlanArtifactForRegistration(root, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
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
		return loopruntime.Snapshot{}, fmt.Errorf("ReviewPlan %s is already registered for round %d (status %s); revise it via `runtime review-plan revise` (one controlled revision per round, L3-S7 §5.3) or start a new round", existing.PlanID, round, existing.Status)
	}
	// Coverage diff at registration (L3-S7 §4.4): every current-generation
	// TASK must be claimed by at least one Claim's source_refs.
	if err := ValidatePlanTaskCoverage(current, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateCoverageInventory(root, current, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateRepairRoundBaseline(root, current, &plan); err != nil {
		return loopruntime.Snapshot{}, err
	}
	// E2E cold start: create and fingerprint the isolated write surface so
	// result submit / round close can bind it exactly (L3-S7 §1.4.1). Keep a
	// cleanup handle for the failure path; a failed registration must not leave
	// an apparently usable empty workspace behind.
	workspace := ""
	if plan.VerificationArtifactWorkspace != nil {
		workspace = *plan.VerificationArtifactWorkspace
	}
	workspacePath := ""
	workspaceWasAbsent := false
	if workspace != "" {
		workspacePath, err = repositoryContainedPath(root, workspace)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		if _, statErr := os.Stat(workspacePath); os.IsNotExist(statErr) {
			workspaceWasAbsent = true
		} else if statErr != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("inspect verification workspace: %w", statErr)
		}
	}
	artifactDigest, err := prepareVerificationWorkspace(root, &plan)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	cleanupWorkspace := func() {
		if workspaceWasAbsent && workspacePath != "" {
			// Remove only the exact empty leaf we created. If a concurrent
			// writer populated it, preserve its evidence rather than using a
			// recursive delete during recovery.
			_ = os.Remove(workspacePath)
		}
	}

	// Pin the plan into the shared control plane directory; the runtime
	// stores path+sha256 and every consumer hash-verifies on load.
	planRel := filepath.ToSlash(filepath.Join(".claude", "review", "plans", plan.ReviewPlanID+".json"))
	planBytes := append(canonicalJSON(data), '\n')
	if err := writeArtifact(root, planRel, planBytes); err != nil {
		cleanupWorkspace()
		return loopruntime.Snapshot{}, err
	}
	planSHA := sha256Of(planBytes)
	cleanupPlan := func() {
		if path, err := repositoryContainedPath(root, planRel); err == nil {
			_ = os.Remove(path)
		}
	}

	runtimeID, _ := current["runtime_id"].(string)
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, err := updateRuntime(store, request.ExpectedRevision, loopruntime.Mutation{
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
			return ApplyRegisteredPlanProjection(state, plan, planRel, planSHA, round, workspace, artifactDigest, occurredAt)
		},
	})
	if err != nil {
		cleanupStateRevision := func() bool {
			stateBytes, readErr := os.ReadFile(statePath)
			if readErr != nil {
				return false
			}
			var persisted map[string]any
			return json.Unmarshal(stateBytes, &persisted) == nil && intField(persisted["revision"]) == commitRevision
		}
		if errors.Is(err, loopruntime.ErrStaleRevision) || cleanupStateRevision() {
			cleanupPlan()
			cleanupWorkspace()
		}
		return snapshot, err
	}
	return snapshot, nil
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
