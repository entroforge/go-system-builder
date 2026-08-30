// Action registry — one ActionFn per declared action name. The registry is
// referenced by LoadCatalog (catalog.go) which fails closed if a declared
// action is missing. Per BUG-001 §4b.2(c) the registry includes
// `start_new_review_round` (resolved by DV round 1 F-CORE-004) and
// `capture_pause_checkpoint` which is the SINGLE capture path covering
// GTR-001..GTR-005 and TR-005 / TR-010 / TR-011 / TR-014 / TR-018
// (BUG-001 §4b.2(c) line 279 and §4b.2(f)).
//
// TASK-015 (BUG-003 §4b.2(e)) owns the inline extension of
// `record_finding_batch` — the previous stub is replaced with a real
// implementation that creates S8 investigation-intake BUG drafts with
// deduplication by finding fingerprint. The signature is unchanged; a draft
// is not an accepted canonical BUG until S8 supplies the causal model and
// Closing Contract.
package transition

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/entroforge/go-system-builder/internal/impact"
)

// ActionFn performs the side effect for one action name and reports whether
// it committed, skipped, or failed.
type ActionFn func(state map[string]any, ctx *ActionContext) (ActionResult, error)

// ActionResult is the per-action outcome returned by an ActionFn.
type ActionResult struct {
	Status          string // "committed" | "skipped" | "failed"
	Detail          string
	MutationApplied bool
}

// ActionContext provides the transition + request context for an action.
// Root is the repository root (so absolute paths are avoidable in actions);
// Spec is the resolved transition spec; From / To are the pre-move cursor;
// Evidence is the request evidence map; OccurredAt is the timestamp.
type ActionContext struct {
	Root       string
	Spec       TransitionSpec
	From       map[string]any
	To         map[string]any
	Evidence   map[string]string
	OccurredAt time.Time
	Params     map[string]any
	Request    *Request
}

var (
	actionRegistryMu sync.RWMutex
	actionRegistry   = map[string]ActionFn{}
)

// RegisterAction inserts (or replaces) an action in the registry.
func RegisterAction(name string, fn ActionFn) {
	actionRegistryMu.Lock()
	defer actionRegistryMu.Unlock()
	actionRegistry[name] = fn
}

// LookupAction returns the registered ActionFn for the supplied name.
func LookupAction(name string) (ActionFn, bool) {
	actionRegistryMu.RLock()
	defer actionRegistryMu.RUnlock()
	fn, ok := actionRegistry[name]
	return fn, ok
}

// ActionNames returns the registered action names in a deterministic order.
// Used by conformance tests and the doctor command.
func ActionNames() []string {
	actionRegistryMu.RLock()
	defer actionRegistryMu.RUnlock()
	names := make([]string, 0, len(actionRegistry))
	for name := range actionRegistry {
		names = append(names, name)
	}
	return names
}

// InitActionRegistry populates the registry with the canonical action set
// from BUG-001 §4b.2(c). Calling it more than once is safe.
func InitActionRegistry() {
	newReg := map[string]ActionFn{
		"bind_loop_req":             actionBindLoopREQ,
		"record_loop_authorization": actionRecordLoopAuthorization,
		"update_bound_req":          actionUpdateBoundREQ,
		"register_locked_contracts": actionRegisterLockedContracts,
		"register_design_documents": actionRegisterDesignDocuments,
		"register_execution_batch":  actionRegisterExecutionBatch,
		// BUG-PLANNING-SUBSTATE: only set_planning_phase_design remains
		// from the planning phase-set family. The other six (initialize,
		// contract_drafting, task_drafting, ui_prototype, rework,
		// ready_for_document_verification) were only used by deleted PTR-PLAN-XX
		// transitions; rework entries from TR-004/007/013/023 now rely on the
		// engine's automatic phase set via phase_machines.planning.initial_phase.
		"set_planning_phase_design":                     actionSetPlanningPhase("design"),
		"set_verification_phase_delivery":               actionSetVerificationPhase("delivery"),
		"set_verification_phase_qa":                     actionSetVerificationPhase("qa"),
		"set_verification_phase_e2e_browser":            actionSetVerificationPhase("e2e_browser"),
		"set_verification_phase_clean_round_evaluation": actionSetVerificationPhase("clean_round_evaluation"),
		"set_verification_phase_clean_round_passed":     actionSetVerificationPhase("clean_round_passed"),
		// L3-S7 P0: S7 entry now lands on `planned`; the phase machine is a
		// ReviewPlan status projection, not a per-lens serial pipeline.
		"set_verification_phase_planned":        actionSetVerificationPhase("planned"),
		"set_bug_phase_investigation":           actionSetBugPhase("investigation"),
		"set_bug_phase_bug_report_review":       actionSetBugPhase("bug_report_review"),
		"set_bug_phase_repair_readback":         actionSetBugPhase("repair_readback"),
		"set_bug_phase_fixing":                  actionSetBugPhase("fixing"),
		"set_bug_phase_targeted_reverification": actionSetBugPhase("targeted_reverification"),
		"set_bug_phase_ready_for_full_review":   actionSetBugPhase("ready_for_full_review"),
		"record_planning_checkpoint":            actionRecordPlanningCheckpoint,
		"record_document_result":                actionRecordDocumentResult,
		"register_planning_tasks":               actionRegisterPlanningTasks,
		"start_review_round":                    actionStartReviewRound,
		"start_new_review_round":                actionStartNewReviewRound,
		"record_bug_drafts":                     actionRecordBugDrafts,
		"record_canonical_bug_batch":            actionRecordCanonicalBugBatch,
		"record_bug_review_feedback":            actionRecordBugReviewFeedback,
		"record_repair_activation":              actionRecordRepairActivation,
		"invalidate_affected_evidence":          actionInvalidateAffectedEvidence,
		"invalidate_consumed_review_evidence":   actionInvalidateConsumedReviewEvidence,
		"record_repair_completion":              actionRecordRepairCompletion,
		"record_targeted_reverification":        actionRecordTargetedReverification,
		"record_finding_batch":                  actionRecordFindingBatch,
		"record_delivery_result":                actionRecordDeliveryResult,
		"record_qa_result":                      actionRecordQAResult,
		"record_e2e_result":                     actionRecordE2EResult,
		"record_clean_round":                    actionRecordCleanRound,
		"record_acc":                            actionRecordACC,
		"record_release_audit":                  actionRecordReleaseAudit,
		"reset_s7_review_after_governance":      actionResetS7ReviewAfterGovernance,
		// SINGLE capture path. Covers GTR-001..GTR-005 and TR-005/010/011/014/018.
		"capture_pause_checkpoint":           actionCapturePauseCheckpoint,
		"restore_state_phase_and_entities":   actionRestoreFromPause,
		"increment_baseline_generation":      actionIncrementBaselineGeneration,
		"invalidate_all_downstream_evidence": actionInvalidateAllDownstreamEvidence,
		"record_abort":                       actionRecordAbort,
	}
	newReg["record_human_release_decision"] = actionRecordHumanReleaseDecision
	newReg["invalidate_human_release_acceptance_evidence"] = actionInvalidateHumanReleaseAcceptanceEvidence
	newReg["invalidate_human_release_release_audit_evidence"] = actionInvalidateHumanReleaseReleaseAuditEvidence
	actionRegistryMu.Lock()
	actionRegistry = newReg
	actionRegistryMu.Unlock()
}

// mustInitActionRegistry panics if the canonical action set is missing
// `start_new_review_round` or `capture_pause_checkpoint`. Invoked once from
// package init so the registry invariant is enforced even when callers
// forget to call InitActionRegistry themselves.
func mustInitActionRegistry() {
	InitActionRegistry()
	for _, name := range []string{
		"start_new_review_round",
		"capture_pause_checkpoint",
	} {
		if _, ok := LookupAction(name); !ok {
			panic(fmt.Sprintf("action %s must be registered in actionRegistry", name))
		}
	}
}

func init() {
	mustInitActionRegistry()
}

// CallActionCapturePauseCheckpointForTest exposes the registered
// capture_pause_checkpoint action for catalog_test.go. Test-only entry point.
func CallActionCapturePauseCheckpointForTest(state map[string]any, ctx ActionContext) (ActionResult, error) {
	fn, ok := LookupAction("capture_pause_checkpoint")
	if !ok {
		return ActionResult{Status: "failed", Detail: "not registered"}, fmt.Errorf("capture_pause_checkpoint not registered")
	}
	return fn(state, &ctx)
}

// --- Action implementations ---

// actionSetPlanningPhase / actionSetVerificationPhase / actionSetBugPhase
// return factory functions that mutate lifecycle.phase to the target phase.
// They preserve the lifecycle's owner state and bump phase_revision.
func actionSetPlanningPhase(target string) ActionFn {
	return func(state map[string]any, ctx *ActionContext) (ActionResult, error) {
		lifecycle, ok := state["lifecycle"].(map[string]any)
		if !ok {
			return ActionResult{Status: "failed", Detail: "lifecycle missing"}, fmt.Errorf("lifecycle missing")
		}
		if lifecycle["phase"] != target {
			lifecycle["phase"] = target
			lifecycle["phase_revision"] = integer(lifecycle["phase_revision"]) + 1
		}
		return ActionResult{Status: "committed", MutationApplied: true, Detail: "phase=" + target}, nil
	}
}

func actionSetVerificationPhase(target string) ActionFn {
	return func(state map[string]any, ctx *ActionContext) (ActionResult, error) {
		lifecycle, ok := state["lifecycle"].(map[string]any)
		if !ok {
			return ActionResult{Status: "failed", Detail: "lifecycle missing"}, fmt.Errorf("lifecycle missing")
		}
		if lifecycle["phase"] != target {
			lifecycle["phase"] = target
			lifecycle["phase_revision"] = integer(lifecycle["phase_revision"]) + 1
		}
		return ActionResult{Status: "committed", MutationApplied: true, Detail: "phase=" + target}, nil
	}
}

func actionSetBugPhase(target string) ActionFn {
	return func(state map[string]any, ctx *ActionContext) (ActionResult, error) {
		lifecycle, ok := state["lifecycle"].(map[string]any)
		if !ok {
			return ActionResult{Status: "failed", Detail: "lifecycle missing"}, fmt.Errorf("lifecycle missing")
		}
		if lifecycle["phase"] != target {
			lifecycle["phase"] = target
			lifecycle["phase_revision"] = integer(lifecycle["phase_revision"]) + 1
		}
		return ActionResult{Status: "committed", MutationApplied: true, Detail: "phase=" + target}, nil
	}
}

// actionStartReviewRound increments review.round and clears review.clean_round.
// It is the canonical "open a new review round" action used by TR-006 / TR-012
// / TR-016. PTR-VERIFY-05 uses start_new_review_round instead, which is the
// distinct action registered below.
func actionStartReviewRound(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if ctx == nil {
		return ActionResult{Status: "failed", Detail: "transition context missing"}, fmt.Errorf("start_review_round requires transition context")
	}
	review, ok := state["review"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "review missing"}, fmt.Errorf("review missing")
	}
	currentRound := integer(review["round"])
	if maxRounds, err := configuredMaxFullReviewRounds(state); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	} else if currentRound >= maxRounds {
		err := fmt.Errorf("S7 full-review budget exhausted at round %d of %d; human decision required: run `loop-harness runtime s7-budget-decision --file <decision.json> --expected-revision <N> --actor <user>` with increase_budget or return_to_governance", currentRound, maxRounds)
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	nextRound := currentRound + 1
	roundEntry := map[string]any{
		"transition_id":       ctx.Spec.ID,
		"round":               nextRound,
		"baseline_generation": baselineGeneration(state),
		"change_impact_ref":   nil,
	}
	if ctx.Spec.ID == "TR-012" {
		changeImpactRef := ""
		if ctx != nil && ctx.Evidence != nil {
			changeImpactRef = strings.TrimSpace(ctx.Evidence["change_impact_record"])
		}
		if changeImpactRef == "" {
			return ActionResult{Status: "failed", Detail: "TR-012 requires change_impact_record"}, fmt.Errorf("TR-012 requires a change_impact_record binding so the next S7 plan can freeze the post-repair baseline")
		}
		roundEntry["change_impact_ref"] = changeImpactRef
	}
	review["round"] = nextRound
	review["clean_round"] = nil
	review["round_entry"] = roundEntry
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "review.round++"}, nil
}

func configuredMaxFullReviewRounds(state map[string]any) (int, error) {
	configuration, ok := state["configuration"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("runtime configuration missing while opening S7 review round")
	}
	repair, ok := configuration["repair"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("runtime configuration.repair missing while opening S7 review round")
	}
	max := integer(repair["max_full_review_rounds"])
	if max < 1 {
		return 0, fmt.Errorf("max_full_review_rounds must be at least 1")
	}
	return max, nil
}

// actionResetS7ReviewAfterGovernance clears the review projection when a
// human chooses specification/architecture governance instead of authorizing
// another full round. The current human decision remains valid as the audit
// authorization; every other currently-valid evidence item is superseded so
// the new planning generation cannot accidentally consume old S7/S8/S9 facts.
func actionResetS7ReviewAfterGovernance(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	review, ok := state["review"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "review missing"}, fmt.Errorf("reset_s7_review_after_governance: review missing")
	}
	review["round"] = 0
	review["clean_round"] = nil
	review["round_entry"] = nil
	review["plan"] = nil
	review["claims"] = map[string]any{}
	review["assignments"] = map[string]any{}
	review["observation_batch"] = nil

	items, ok := state["evidence"].([]any)
	if !ok {
		return ActionResult{Status: "committed", MutationApplied: true, Detail: "review projection reset; no evidence to invalidate"}, nil
	}
	excluded := transitionEvidenceIDs(ctx)
	impacts := make([]impact.EvidenceImpact, 0, len(items))
	for _, raw := range items {
		entry, ok := raw.(map[string]any)
		if !ok || entry["status"] != "valid" {
			continue
		}
		id, _ := entry["id"].(string)
		if _, keep := excluded[id]; keep {
			continue
		}
		impacts = append(impacts, impact.EvidenceImpact{
			EvidenceID:    id,
			Rule:          "s7_governance_return",
			Reason:        "S7 budget decision returned the Runtime to specification/architecture governance",
			CurrentStatus: "valid",
		})
	}
	invalidated := impact.InvalidateEvidence(state, impacts, "GTR-006")
	return ActionResult{
		Status: "committed", MutationApplied: true,
		Detail: fmt.Sprintf("review projection reset; %d downstream evidence entries invalidated", len(invalidated)),
	}, nil
}

// actionStartNewReviewRound is the action declared by PTR-VERIFY-05. It is
// intentionally distinct from start_review_round:
// it is invoked when a clean round was incomplete and the verification phase
// must restart at delivery. For TASK-013 the semantic is the same as
// start_review_round; the registered name preserves the spec.
func actionStartNewReviewRound(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionStartReviewRound(state, ctx)
}

// actionCapturePauseCheckpoint is the SINGLE capture path. It writes the
// pause checkpoint into state["pause"]. If a pause checkpoint already exists
// the action refuses to overwrite (BUG-001 §4b.2(f) guard) — this is what
// makes single-call-per-pause-event provable: there is exactly one writer
// and it rejects a second capture within the same transition. A nil value
// (the schema-default for "no active checkpoint") is treated as absent.
func actionCapturePauseCheckpoint(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if err := CapturePauseCheckpoint(state, ctx.Spec.Description, ctx.OccurredAt); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "pause checkpoint captured"}, nil
}

// CapturePauseCheckpoint writes the authoritative pause checkpoint into
// state["pause"] and refuses to overwrite an existing one. The S7
// review-result submit uses it to create the pause checkpoint inside the
// verdict transaction (L3-S7 §9.2: one authoritative checkpoint, no dual
// carrier); TR-010/TR-011 then only move the cursor.
func CapturePauseCheckpoint(state map[string]any, reason string, occurredAt time.Time) error {
	if existing, ok := state["pause"]; ok && existing != nil {
		return fmt.Errorf("capture_pause_checkpoint: pause checkpoint already exists; would overwrite")
	}
	lifecycle, ok := state["lifecycle"].(map[string]any)
	if !ok {
		return fmt.Errorf("capture_pause_checkpoint: lifecycle missing")
	}
	fromState, _ := lifecycle["state"].(string)
	fromPhase := lifecycle["phase"]
	phaseRevision := integer(lifecycle["phase_revision"])

	baseline, _ := state["baseline"].(map[string]any)
	baselineGeneration := 0
	if baseline != nil {
		baselineGeneration = integer(baseline["generation"])
	}
	review, _ := state["review"].(map[string]any)
	reviewRound := 0
	if review != nil {
		reviewRound = integer(review["round"])
	}
	documents := documentFingerprints(state)

	state["pause"] = map[string]any{
		"from_state":            fromState,
		"from_phase":            fromPhase,
		"phase_revision":        phaseRevision,
		"baseline_generation":   baselineGeneration,
		"review_round":          reviewRound,
		"reason":                reason,
		"required_human_action": pauseRequiredAction(fromState),
		"document_fingerprints": documents,
		"paused_at":             occurredAt.UTC().Format(time.RFC3339Nano),
	}
	return nil
}

// actionRestoreFromPause re-hashes every document referenced in the pause
// checkpoint's document_fingerprints and rejects the resume if any file has
// drifted. On success it clears state["pause"]. The target state / phase are
// read from the pause checkpoint so the engine can move the cursor back.
func actionRestoreFromPause(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if err := restoreFromPauseAction(ctx.Root, state); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "restored from pause"}, nil
}

// actionIncrementBaselineGeneration bumps baseline.generation and invalidates
// every evidence entry whose baseline_generation <= old. Used by TR-020.
func actionIncrementBaselineGeneration(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return incrementBaselineAndInvalidateAction(state, ctx)
}

// actionInvalidateAllDownstreamEvidence marks every currently-valid evidence
// entry as invalid via the impact package. Used by repair transitions that
// need a full evidence sweep without a baseline bump.
func actionInvalidateAllDownstreamEvidence(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	invalidateAllDownstreamAction(state, ctx.Spec.ID)
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "all valid evidence invalidated"}, nil
}

// --- Placeholder actions for the remaining registry entries. These record
// the action was invoked but do not mutate state beyond a journal entry.
// TASK-015 will specialize the semantic-bearing ones. ---

func actionBindLoopREQ(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if ctx.Request == nil || ctx.Request.REQ == nil {
		return ActionResult{Status: "failed", Detail: "locked REQ request missing"}, fmt.Errorf("bind_loop_req: locked REQ request missing")
	}
	if err := bindREQ(ctx.Root, state, *ctx.Request, ctx.OccurredAt); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "locked REQ bound"}, nil
}

func actionUpdateBoundREQ(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if ctx.Request == nil || ctx.Request.REQ == nil {
		return ActionResult{Status: "failed", Detail: "amended REQ request missing"}, fmt.Errorf("update_bound_req: amended REQ request missing")
	}
	if err := updateBoundREQ(ctx.Root, state, *ctx.Request, ctx.OccurredAt); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "amended REQ swapped into the running cycle"}, nil
}

// actionRoot resolves the disk root for registration actions: state["root"]
// first (the runtime's own tree — guards read the same slot), then ctx.Root,
// then registerDocumentsFromDisk's "." fallback. Priority matters in unit
// tests where the runtime state lives in a temp root distinct from the
// catalog root passed to Apply (L3-S4 v4.0.1).
func actionRoot(state map[string]any, ctx *ActionContext) string {
	if root, _ := state["root"].(string); root != "" {
		return root
	}
	return ctx.Root
}

// actionRegisterLockedContracts runs on PTR-PLAN-02 (contracts→tasks):
// it scans docs/contracts/*.md for status=locked files and registers each
// into documents[] (same-generation-same-id replaces, appendDocument) —
// feeding the S3 exit gate that consumes documents[], and arming hook
// write-protection for contracts (L3-S3 v4.0.1).
func actionRegisterLockedContracts(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	registered, err := registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, "docs/contracts", []string{"BE-", "FE-", "SYNC-", "CONTRACTS-"}, "contract", "locked")
	if err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: registered > 0,
		Detail: fmt.Sprintf("registered %d locked contract(s)", registered)}, nil
}

// actionRegisterExecutionBatch replaces the evidence-presence placeholder
// on TR-003: the atomic lock registers the execution batch — complete TASK
// files join the already-registered contracts in documents[].
func actionRegisterExecutionBatch(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if len(ctx.Evidence) == 0 {
		return ActionResult{Status: "failed", Detail: "execution batch evidence missing"}, fmt.Errorf("register_execution_batch: current evidence missing")
	}
	registered, err := registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, "docs/tasks", []string{"TASK-"}, "task", "complete")
	if err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	if registered == 0 {
		return ActionResult{Status: "failed",
			Detail: "execution batch is empty — no complete TASK under docs/tasks; an empty batch would lock nothing into building"}, fmt.Errorf("register_execution_batch: no complete TASK document under docs/tasks — write and complete the task batch before TR-003")
	}
	return ActionResult{Status: "committed", MutationApplied: true,
		Detail: fmt.Sprintf("registered %d complete task(s)", registered)}, nil
}

// actionRegisterPlanningTasks runs on TR-002 (tasks→document_verification):
// it registers the complete TASK batch into documents[], so S5's exit gates
// and the independence check consume real registrations instead of seeded
// fixtures. Unlike TR-003's register_execution_batch it carries no evidence
// precondition — TR-002's required_evidence is empty (L3-S4 v4.0.1 B4).
func actionRegisterPlanningTasks(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	registered, err := registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, "docs/tasks", []string{"TASK-"}, "task", "complete")
	if err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: registered > 0,
		Detail: fmt.Sprintf("registered %d complete task(s)", registered)}, nil
}

// registerDocumentsFromDisk scans a docs subtree for files whose top-of-file
// status field matches; each is registered into documents[] with the current
// baseline generation and the registering actor as author. Status mismatch
// on a filename-matching file FAILS (skip would silently starve the
// subject fingerprint matching manifest downstream — axiom five).
func registerDocumentsFromDisk(root string, state map[string]any, ctx *ActionContext, dirRel string, prefixes []string, kind, wantStatus string) (int, error) {
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, filepath.FromSlash(dirRel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// No directory = nothing to register (a repo without this docs
			// subtree yet); an absent batch is not a partial batch.
			return 0, nil
		}
		return 0, fmt.Errorf("read %s: %w", dirRel, err)
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := 0
	if baseline != nil {
		generation = integerOf(baseline["generation"])
	}
	actor := ""
	if ctx.Request != nil {
		actor = ctx.Request.Actor
	}
	occurredAt := ""
	if !ctx.OccurredAt.IsZero() {
		occurredAt = ctx.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	registered := 0
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".md")
		if entry.IsDir() || !strings.HasSuffix(name, ".md") ||
			strings.Contains(strings.ToLower(name), "template") ||
			strings.EqualFold(name, "README.md") ||
			!hasAnyPrefix(id, prefixes) {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(dirRel, name))
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return registered, fmt.Errorf("read %s: %w", rel, err)
		}
		status := ParseMarkdownField(string(data), "状态", "Status")
		if status == "" {
			// No status field: the file declares no batch membership (legacy
			// fixture, README-like stub). Not part of this batch — skip.
			continue
		}
		if status == "cancelled" {
			// Cancelled declares the task out of the batch (S4 document
			// status vocabulary) — skip instead of failing the partial-batch
			// check, matching tasks check / TaskBatchComplete semantics.
			continue
		}
		if status != wantStatus {
			// Declares a different state: a real partial batch (e.g. a draft
			// contract at TR-003). Failing here keeps subject fingerprint matching honest.
			return registered, fmt.Errorf("%s status is %q, want %q — register refuses partial batches (fix the file or finish the batch)", rel, status, wantStatus)
		}
		version := ParseMarkdownField(string(data), "版本", "Version")
		if version == "" {
			// Legacy fixtures and minimal drafts may omit the version field;
			// the document id is the join key — register with a placeholder
			// rather than failing the whole batch (version presence is the
			// template's job, enforced by S5 review, not the registration).
			version = "unversioned"
		}
		state["documents"] = appendDocument(state["documents"], map[string]any{
			"id": id, "kind": kind, "path": rel, "version": version,
			"sha256": SHA256(data), "status": status, "generation": generation,
			"author_agent_id": actor, "registered_at": occurredAt,
		})
		registered++
	}
	return registered, nil
}

func hasAnyPrefix(id string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

func integerOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func actionEvidenceRecorded(ctx *ActionContext, label string) (ActionResult, error) {
	if len(ctx.Evidence) == 0 {
		return ActionResult{Status: "failed", Detail: label + " evidence missing"}, fmt.Errorf("%s: current evidence missing", label)
	}
	return ActionResult{Status: "committed", Detail: label + " references current runtime evidence"}, nil
}
func actionRecordLoopAuthorization(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "loop authorization")
}
func actionRecordPlanningCheckpoint(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "planning checkpoint")
}
func actionRecordDocumentResult(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "document result")
}

// actionRecordBugDrafts is PTR-BUG-01's legacy compatibility action
// (RC-06, S8-9). PTR-BUG-01 is declared as a legacy projection path only
// (deprecated: legacy compatibility; see loop-harness.md "Legacy (PTR-BUG)";
// see its loop-definition.json description): the real BUG-draft producer on
// the modern path is record_finding_batch (TR-008), which derives drafts from
// the sealed ObservationBatch with content-hash dedup. This stub therefore
// only asserts that current evidence context exists — it must not grow real
// draft-creation logic, or the two paths would diverge on identity rules.
func actionRecordBugDrafts(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "bug drafts (legacy PTR-BUG-01 projection)")
}
func actionRecordCanonicalBugBatch(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "canonical bug batch")
}
func actionRecordBugReviewFeedback(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "bug review feedback")
}
func actionRecordRepairActivation(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "repair activation")
}
func actionInvalidateAffectedEvidence(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	// RC-05 (S10-6): an empty AffectedPaths list used to degrade into a
	// silent "no affected evidence to invalidate" commit — the invalidation
	// write barrier became a no-op exactly when the caller forgot to declare
	// what changed. The transition now fails closed: declare the affected
	// paths, or explicitly declare the full surface with the single "all"
	// token (which the Controller emits only when the request itself
	// authorizes a full sweep).
	var changedPaths []string
	all := false
	if ctx.Request != nil {
		changedPaths = append([]string(nil), ctx.Request.AffectedPaths...)
		if len(changedPaths) == 1 && strings.TrimSpace(changedPaths[0]) == "all" {
			all = true
			changedPaths = nil
		}
	}
	if !all && len(changedPaths) == 0 {
		return ActionResult{Status: "failed", Detail: "affected_paths required for " + ctx.Spec.ID}, fmt.Errorf(
			"transition %s requires the affected paths of the change: pass --affected-paths <repo-relative paths> (or the single token \"all\" for an explicit full-surface sweep); an empty affected-path list would silently skip invalidating historical PASS evidence (TR-016/TR-007/TR-013/TR-023/PTR-BUG-05)",
			ctx.Spec.ID,
		)
	}
	if all {
		invalidated := invalidateAllDownstreamAction(state, ctx.Spec.ID)
		return ActionResult{
			Status:          "committed",
			MutationApplied: invalidated > 0,
			Detail:          fmt.Sprintf("explicit full-surface sweep: %d evidence entries invalidated", invalidated),
		}, nil
	}
	impacts := impact.ComputeImpact(state, changedPaths)
	if len(impacts) == 0 {
		return ActionResult{Status: "committed", Detail: "no affected evidence to invalidate"}, nil
	}
	excluded := transitionEvidenceIDs(ctx)
	filtered := make([]impact.EvidenceImpact, 0, len(impacts))
	for _, item := range impacts {
		if _, skip := excluded[item.EvidenceID]; skip {
			continue
		}
		filtered = append(filtered, item)
	}
	invalidated := impact.InvalidateEvidence(state, filtered, ctx.Spec.ID)
	return ActionResult{
		Status:          "committed",
		MutationApplied: len(invalidated) > 0,
		Detail:          fmt.Sprintf("%d evidence entries invalidated", len(invalidated)),
	}, nil
}

func transitionEvidenceIDs(ctx *ActionContext) map[string]struct{} {
	excluded := make(map[string]struct{})
	if ctx == nil || ctx.Evidence == nil {
		return excluded
	}
	for _, id := range ctx.Evidence {
		if id != "" {
			excluded[id] = struct{}{}
		}
	}
	return excluded
}
func actionRecordRepairCompletion(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "repair completion")
}
func actionRecordTargetedReverification(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "targeted reverification")
}

// actionRecordFindingBatch implements TR-008's record_finding_batch action.
//
// L3-S7 P0: the batch source is the sealed ObservationBatch in
// state.review.observation_batch — the exact Finding set the round consumer
// sealed — not a free-form ctx.Params payload. For every Finding entity in
// the batch this creates one investigation-intake BUG draft with
// deduplication by finding content hash, so S8 starts from immutable
// observation facts (finding file + encounter) instead of a hand-carried
// summary. S8 still owns causal acceptance; this action does not claim that
// the symptom has a root cause or that a repair is authorized.
//
// BUG schema constraints (loop-state.schema.json §bug):
// - additionalProperties: false; only the 7 canonical keys are allowed.
// - original_finder_agent_ids: array, minItems: 1, uniqueItems.
// - id: pattern ^BUG-[0-9]{3,}$.
// - state: enum {draft, investigating, ...}; new BUGs land in "draft".
//
// Dedup fingerprint: the finding entity's sha256 (the finding is immutable,
// so its content hash is a stable identity), encoded as `<path>#fp=<sha256>`.
func actionRecordFindingBatch(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	reviewMap, _ := state["review"].(map[string]any)
	batch, _ := reviewMap["observation_batch"].(map[string]any)
	if batch == nil {
		return ActionResult{Status: "failed", Detail: "observation batch missing"}, fmt.Errorf("record_finding_batch: no sealed ObservationBatch — the S7 round consumer seals it when the final required Claim disposition lands")
	}
	batchIDs, _ := batch["finding_ids"].([]any)
	if len(batchIDs) == 0 {
		return ActionResult{Status: "failed", Detail: "observation batch carries no findings"}, fmt.Errorf("record_finding_batch: sealed ObservationBatch has an empty finding set")
	}
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "entities missing"}, fmt.Errorf("record_finding_batch: entities missing")
	}
	findings, _ := entities["findings"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range findings {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if id, _ := row["finding_id"].(string); id != "" {
			byID[id] = row
		}
	}
	bugs, _ := entities["bugs"].([]any)

	registered := 0
	deduplicated := 0
	registeredIDs := make([]string, 0)
	for _, rawID := range batchIDs {
		findingID, _ := rawID.(string)
		row := byID[findingID]
		if row == nil {
			return ActionResult{Status: "failed", Detail: "finding entity missing"}, fmt.Errorf("record_finding_batch: batch references finding %s which has no entity row", findingID)
		}
		path, _ := row["path"].(string)
		sha, _ := row["sha256"].(string)
		finder, _ := row["original_finder"].(string)
		severity, _ := row["severity"].(string)
		if path == "" || sha == "" || finder == "" || severity == "" {
			return ActionResult{Status: "failed", Detail: "finding row incomplete"}, fmt.Errorf("record_finding_batch: finding %s row is missing path/sha256/original_finder/severity", findingID)
		}
		if existing := findBugByFingerprint(bugs, sha); existing != "" {
			deduplicated++
			continue
		}
		nextID := nextBugID(bugs)
		newBug := map[string]any{
			"id":                          nextID,
			"state":                       "draft",
			"path":                        path + "#fp=" + sha,
			"severity":                    severity,
			"attempt_count":               0,
			"same_contract_failure_count": 0,
			"original_finder_agent_ids":   []any{finder},
		}
		// RC-02 (L3-S7 §10.1): blocking is a business judgment carried from
		// the Finding, never a severity synonym. P0 is implicitly blocking;
		// a non-P0 Finding with blocking=true stays blocking downstream.
		if findingBlocking, ok := row["blocking"].(bool); ok {
			newBug["blocking"] = findingBlocking
		} else if severity == "P0" {
			newBug["blocking"] = true
		}
		bugs = append(bugs, newBug)
		registered++
		registeredIDs = append(registeredIDs, nextID)
	}
	entities["bugs"] = bugs
	detail := fmt.Sprintf("finding batch processed from sealed ObservationBatch: registered=%d deduplicated=%d ids=%v", registered, deduplicated, registeredIDs)
	return ActionResult{Status: "committed", MutationApplied: registered > 0, Detail: detail}, nil
}

// computeFindingFingerprint derives a stable hash for a single finding.
// The fingerprint is deterministic so duplicate findings (same reporter,
// same body, same path) hash to the same value and are deduplicated.
//
// Formula: sha256(reporter_agent_id + "\n" + sorted(finding_body lines) +
// "\n" + finding_path). Sorting the body lines (instead of the whole body)
// is intentional: whitespace-only reorderings of multi-line findings still
// dedup, but semantic re-orderings within a paragraph do not.
func computeFindingFingerprint(reporter, body, path string) string {
	lines := strings.Split(body, "\n")
	sort.Strings(lines)
	normalized := strings.Join(lines, "\n")
	h := sha256.New()
	h.Write([]byte(reporter))
	h.Write([]byte{'\n'})
	h.Write([]byte(normalized))
	h.Write([]byte{'\n'})
	h.Write([]byte(path))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// findBugByFingerprint scans an entities.bugs array for an entry whose path
// encodes the supplied fingerprint. The expected encoding is
// "<path>#fp=<sha256>". When a match is found the BUG id is returned;
// otherwise the empty string.
func findBugByFingerprint(bugs []any, fingerprint string) string {
	suffix := "#fp=" + fingerprint
	for _, raw := range bugs {
		bug, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		p, _ := bug["path"].(string)
		if strings.HasSuffix(p, suffix) {
			id, _ := bug["id"].(string)
			return id
		}
	}
	return ""
}

// nextBugID returns the next "BUG-NNN" id for entities.bugs. It scans the
// existing array for the highest numeric suffix and increments. IDs that
// already exist in [closed, rejected, duplicate] states still count toward
// the suffix (deletion is out of scope for TASK-015).
func nextBugID(bugs []any) string {
	highest := 0
	for _, raw := range bugs {
		bug, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := bug["id"].(string)
		n := parseBugID(id)
		if n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("BUG-%03d", highest+1)
}

// parseBugID extracts the numeric suffix from a BUG id. Returns 0 for any
// string that does not match the BUG-NNN pattern.
func parseBugID(id string) int {
	if !strings.HasPrefix(id, "BUG-") {
		return 0
	}
	suffix := strings.TrimPrefix(id, "BUG-")
	n := 0
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
func actionRecordDeliveryResult(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "delivery result")
}
func actionRecordQAResult(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "qa result")
}
func actionRecordE2EResult(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "e2e browser result")
}
func actionRecordCleanRound(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	review, ok := state["review"].(map[string]any)
	if !ok || integer(review["round"]) < 1 {
		return ActionResult{Status: "failed", Detail: "active review round missing"}, fmt.Errorf("record_clean_round: active review round missing")
	}
	review["clean_round"] = integer(review["round"])
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "clean round recorded"}, nil
}
func actionRecordACC(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "acceptance")
}
func actionRecordReleaseAudit(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "release audit")
}

func actionRecordHumanReleaseDecision(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	decisionRef := ""
	if ctx != nil {
		decisionRef = ctx.Evidence["human_decision_record"]
	}
	if strings.TrimSpace(decisionRef) == "" {
		return ActionResult{Status: "failed", Detail: "human release decision evidence missing"}, fmt.Errorf("record_human_release_decision: human_decision_record evidence missing")
	}
	return ActionResult{
		Status: "committed",
		Detail: fmt.Sprintf("human release decision %s recorded from evidence %s", ctx.Spec.Event, decisionRef),
	}, nil
}

func actionInvalidateHumanReleaseAcceptanceEvidence(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return invalidateHumanReleaseEvidence(state, ctx, map[string]struct{}{
		"acceptance":           {},
		"acceptance_record":    {},
		"release_audit":        {},
		"release_audit_record": {},
	})
}

func actionInvalidateHumanReleaseReleaseAuditEvidence(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return invalidateHumanReleaseEvidence(state, ctx, map[string]struct{}{
		"release_audit":        {},
		"release_audit_record": {},
	})
}

func invalidateHumanReleaseEvidence(state map[string]any, ctx *ActionContext, kinds map[string]struct{}) (ActionResult, error) {
	items, ok := state["evidence"].([]any)
	if !ok {
		return ActionResult{Status: "committed", Detail: "no evidence entries to invalidate"}, nil
	}
	invalidated := 0
	for _, raw := range items {
		entry, ok := raw.(map[string]any)
		if !ok || entry["status"] != "valid" {
			continue
		}
		kind, _ := entry["kind"].(string)
		if _, allowed := kinds[kind]; !allowed {
			continue
		}
		entry["status"] = "invalid"
		entry["invalidated_by"] = ctx.Spec.ID
		entry["invalidation_rule"] = "human_release_decision"
		entry["invalidation_reason"] = "human release rejection requires refreshed evidence"
		invalidated++
	}
	return ActionResult{
		Status:          "committed",
		MutationApplied: invalidated > 0,
		Detail:          fmt.Sprintf("%d human release evidence entries invalidated", invalidated),
	}, nil
}

func actionRecordAbort(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "human abort")
}

// actionRegisterDesignDocuments runs on PTR-PLAN-01: the architecture
// document is registered into documents[] the same way contracts (PTR-PLAN-02)
// and tasks (TR-002) are — S5's review evidence needs a registered design
// fact to sign over (previously nothing registered kind=design,
// so the S2 exit gate could never be satisfied on the organic path).
func actionRegisterDesignDocuments(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	registered, err := registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, "docs/design/architecture", []string{"ARCHITECTURE-"}, "design", "locked")
	if err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	if registered == 0 {
		return ActionResult{Status: "failed",
			Detail: "no locked architecture document under docs/design/architecture — the design stage produced nothing to register"}, fmt.Errorf("register_design_documents: no ARCHITECTURE-*.md with Status locked under docs/design/architecture — write the architecture document before advancing")
	}
	return ActionResult{Status: "committed", MutationApplied: true,
		Detail: fmt.Sprintf("registered %d locked design document(s)", registered)}, nil
}

// actionInvalidateConsumedReviewEvidence runs on TR-004: the fix_required
// record that triggered the rework loop is consumed by this commit — it
// stays valid forever otherwise, and a fix that touches no registered
// document would re-select TR-004 on every subsequent PreToolUse (the
// livelock). Fingerprints still scope the *re-run* side (a
// fix that changed documents already invalidates the old pass records by
// subject mismatch); this action closes the no-drift half.
func actionInvalidateConsumedReviewEvidence(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	items, _ := state["evidence"].([]any)
	invalidated := 0
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || item["kind"] != "document_review" || item["status"] != "valid" {
			continue
		}
		id, _ := item["id"].(string)
		if !citedByTransition(ctx, id) {
			continue
		}
		item["status"] = "invalid"
		item["invalidated_by"] = ctx.Spec.ID
		item["invalidation_rule"] = "consumed_fix_record"
		item["invalidation_reason"] = "consumed by TR-004 (document fix routed back to planning); the affected responsibility re-runs on fresh fingerprints"
		invalidated++
	}
	return ActionResult{
		Status:          "committed",
		MutationApplied: invalidated > 0,
		Detail:          fmt.Sprintf("%d consumed review record(s) invalidated", invalidated),
	}, nil
}

func citedByTransition(ctx *ActionContext, id string) bool {
	if ctx == nil || ctx.Evidence == nil {
		return false
	}
	for _, ref := range ctx.Evidence {
		if ref == id {
			return true
		}
	}
	return false
}

func actorOf(ctx *ActionContext) string {
	if ctx.Request != nil {
		return ctx.Request.Actor
	}
	return ""
}
