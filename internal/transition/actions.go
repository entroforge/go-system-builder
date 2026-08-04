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
// implementation that creates canonical BUG entities with deduplication by
// finding fingerprint. The signature is unchanged.
package transition

import (
	"crypto/sha256"
	"fmt"
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
		"set_bug_phase_investigation":                   actionSetBugPhase("investigation"),
		"set_bug_phase_bug_report_review":               actionSetBugPhase("bug_report_review"),
		"set_bug_phase_repair_readback":                 actionSetBugPhase("repair_readback"),
		"set_bug_phase_fixing":                          actionSetBugPhase("fixing"),
		"set_bug_phase_targeted_reverification":         actionSetBugPhase("targeted_reverification"),
		"set_bug_phase_ready_for_full_review":           actionSetBugPhase("ready_for_full_review"),
		"record_planning_checkpoint":                    actionRecordPlanningCheckpoint,
		"record_document_result":                        actionRecordDocumentResult,
		"atomically_lock_execution_batch":               actionAtomicallyLockExecutionBatch,
		"start_review_round":                            actionStartReviewRound,
		"start_new_review_round":                        actionStartNewReviewRound,
		"record_bug_drafts":                             actionRecordBugDrafts,
		"record_canonical_bug_batch":                    actionRecordCanonicalBugBatch,
		"record_bug_review_feedback":                    actionRecordBugReviewFeedback,
		"record_repair_activation":                      actionRecordRepairActivation,
		"invalidate_affected_evidence":                  actionInvalidateAffectedEvidence,
		"record_repair_completion":                      actionRecordRepairCompletion,
		"record_targeted_reverification":                actionRecordTargetedReverification,
		"record_finding_batch":                          actionRecordFindingBatch,
		"record_delivery_result":                        actionRecordDeliveryResult,
		"record_qa_result":                              actionRecordQAResult,
		"record_e2e_result":                             actionRecordE2EResult,
		"record_clean_round":                            actionRecordCleanRound,
		"record_acc":                                    actionRecordACC,
		"record_release_audit":                          actionRecordReleaseAudit,
		// SINGLE capture path. Covers GTR-001..GTR-005 and TR-005/010/011/014/018.
		"capture_pause_checkpoint":           actionCapturePauseCheckpoint,
		"restore_state_phase_and_entities":   actionRestoreFromPause,
		"increment_baseline_generation":      actionIncrementBaselineGeneration,
		"invalidate_all_downstream_evidence": actionInvalidateAllDownstreamEvidence,
		"record_abort":                       actionRecordAbort,
	}
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
	review, ok := state["review"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "review missing"}, fmt.Errorf("review missing")
	}
	review["round"] = integer(review["round"]) + 1
	review["clean_round"] = nil
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "review.round++"}, nil
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
	if existing, ok := state["pause"]; ok && existing != nil {
		return ActionResult{Status: "failed", Detail: "pause checkpoint already exists"}, fmt.Errorf("capture_pause_checkpoint: pause checkpoint already exists; would overwrite")
	}
	lifecycle, ok := state["lifecycle"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "lifecycle missing"}, fmt.Errorf("capture_pause_checkpoint: lifecycle missing")
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
	entities, _ := state["entities"].(map[string]any)
	entitySnapshotRevision := 0
	if entities != nil {
		entitySnapshotRevision = len(asEntityArray(entities))
	}
	documents := documentFingerprints(state)
	keys := committedIdempotencyKeys(state)

	state["pause"] = map[string]any{
		"from_state":                 fromState,
		"from_phase":                 fromPhase,
		"phase_revision":             phaseRevision,
		"baseline_generation":        baselineGeneration,
		"review_round":               reviewRound,
		"entity_snapshot_revision":   entitySnapshotRevision,
		"reason":                     ctx.Spec.Description,
		"required_human_action":      pauseRequiredAction(fromState),
		"document_fingerprints":      documents,
		"committed_idempotency_keys": keys,
		"paused_at":                  ctx.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "pause checkpoint captured"}, nil
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
func actionAtomicallyLockExecutionBatch(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "execution batch")
}
func actionRecordBugDrafts(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "bug drafts")
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
	var changedPaths []string
	if ctx.Request != nil && len(ctx.Request.AffectedPaths) > 0 {
		changedPaths = append([]string(nil), ctx.Request.AffectedPaths...)
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
// Per BUG-003 §4b.2(e) this action consumes the `findings` payload from
// ctx.Params (set by the orchestrator when TR-008 fires), creates one
// canonical BUG entity per finding with deduplication by finding
// fingerprint, then returns the count of newly registered vs. deduplicated
// findings. The follow-up `set_bug_phase_investigation` action runs via the
// engine's action chain — this function does not invoke it directly.
//
// Inputs (ctx.Params):
// - "findings": []any of map[string]any. Each finding has keys:
// reporter_agent_id (string, required)
// finding_body (string, required)
// finding_path (string, required — must exist on disk)
// severity (string, optional; defaults to "P0")
//
// Outputs:
// - Status: "committed" with Detail containing the registered/dedup counts.
// - MutationApplied: true when at least one new BUG was appended.
//
// BUG schema constraints (loop-state.schema.json §bug):
// - additionalProperties: false; only the 7 canonical keys are allowed.
// - original_finder_agent_ids: array, minItems: 1, uniqueItems.
// - id: pattern ^BUG-[0-9]{3,}$.
// - state: enum {draft, investigating, ...}; new BUGs land in "draft".
//
// Finding fingerprint: sha256 of reporter_agent_id + sorted(finding_body lines)
// + finding_path. The fingerprint is encoded in the BUG's path field as
// `<finding_path>#fp=<sha256>` so dedup can be re-computed by walking all
// existing BUGs and parsing the suffix — no extra state field required.
func actionRecordFindingBatch(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	rawFindings, _ := ctx.Params["findings"].([]any)
	if len(rawFindings) == 0 {
		return ActionResult{Status: "skipped", Detail: "no findings in batch"}, nil
	}
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "entities missing"}, fmt.Errorf("record_finding_batch: entities missing")
	}
	bugs, _ := entities["bugs"].([]any)

	registered := 0
	deduplicated := 0
	registeredIDs := make([]string, 0)
	for _, raw := range rawFindings {
		finding, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		reporter, _ := finding["reporter_agent_id"].(string)
		body, _ := finding["finding_body"].(string)
		path, _ := finding["finding_path"].(string)
		if reporter == "" || path == "" {
			// Skip malformed entries; the orchestrator is expected to
			// validate the batch shape before invoking TR-008.
			continue
		}
		fp := computeFindingFingerprint(reporter, body, path)
		// Dedupe: look for any existing BUG whose path ends with the same
		// fingerprint.
		if existing := findBugByFingerprint(bugs, fp); existing != "" {
			deduplicated++
			continue
		}
		severity, _ := finding["severity"].(string)
		if severity == "" {
			severity = "P0"
		}
		// Allocate the next BUG id. We scan entities.bugs for the highest
		// existing numeric suffix and increment. This is intentionally
		// simple — collision-free allocation is not the dedup mechanism
		// (the fingerprint is); id reuse after deletion is out of scope for
		// TASK-015.
		nextID := nextBugID(bugs)
		newBug := map[string]any{
			"id":                          nextID,
			"state":                       "draft",
			"path":                        path + "#fp=" + fp,
			"severity":                    severity,
			"attempt_count":               0,
			"same_contract_failure_count": 0,
			"original_finder_agent_ids":   []any{reporter},
		}
		bugs = append(bugs, newBug)
		registered++
		registeredIDs = append(registeredIDs, nextID)
	}
	entities["bugs"] = bugs
	detail := fmt.Sprintf("finding batch processed: registered=%d deduplicated=%d ids=%v", registered, deduplicated, registeredIDs)
	if registered == 0 && deduplicated == 0 {
		return ActionResult{Status: "skipped", Detail: detail}, nil
	}
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
func actionRecordAbort(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	return actionEvidenceRecorded(ctx, "human abort")
}
