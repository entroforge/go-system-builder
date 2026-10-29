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
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/docscope"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
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
	Root               string
	Spec               TransitionSpec
	From               map[string]any
	To                 map[string]any
	Evidence           map[string]string
	OccurredAt         time.Time
	Params             map[string]any
	Request            *Request
	EventID            string
	BeforeRevision     int
	AfterRevision      int
	TransitionSequence int
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
		"planning_professional_rework": actionPlanningProfessionalRework,
		"bind_loop_req":                actionBindLoopREQ,
		"record_loop_authorization":    actionRecordLoopAuthorization,
		"update_bound_req":             actionUpdateBoundREQ,
		"register_locked_contracts":    actionRegisterLockedContracts,
		"register_design_documents":    actionRegisterDesignDocuments,
		"register_execution_batch":     actionRegisterExecutionBatch,
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
		err := fmt.Errorf("S7 full-review budget exhausted at round %d of %d; human decision required: run `loop-harness runtime s7-budget-decision --file <decision.json> --actor <user>` with increase_budget or return_to_governance", currentRound, maxRounds)
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	nextRound := currentRound + 1
	roundEntry := map[string]any{
		"transition_id":       ctx.Spec.ID,
		"round":               nextRound,
		"baseline_generation": baselineGeneration(state),
		"change_impact_ref":   nil,
	}
	if ctx.Spec.ID == "TR-012" || ctx.Spec.ID == "TR-016" || ctx.Spec.ID == "TR-031" {
		changeImpactRef := ""
		if ctx != nil && ctx.Evidence != nil {
			changeImpactRef = strings.TrimSpace(ctx.Evidence["change_impact_record"])
		}
		if changeImpactRef == "" {
			return ActionResult{Status: "failed", Detail: "TR-012 requires change_impact_record"}, fmt.Errorf("%s requires a change_impact_record binding so the next S7 plan can freeze its baseline", ctx.Spec.ID)
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

// actionRegisterLockedContracts registers the accepted S3 batch on
// PTR-PLAN-02. Packet-mode TR-002 retains that already-registered S3 source
// set; it must not rescan contracts while registering S4 outputs.
func actionRegisterLockedContracts(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	var accepted *acceptedS3ContractBatch
	if ctx.Spec.ID == "PTR-PLAN-02" && loopruntime.PacketEnabled(state) {
		if ctx.Request == nil || ctx.Request.SubmissionFollowupID == "" {
			err := fmt.Errorf("S3 transition needs the received current S3 questionnaire; save the exact answer path supplied by the listener and let its original consumer continue")
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		var err error
		accepted, err = acceptedContractArtifacts(state, ctx)
		if err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		if err := accepted.validateSources(actionRoot(state, ctx), ctx.Request.Files); err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
	}
	if ctx.Spec.ID == "TR-002" && loopruntime.PacketEnabled(state) {
		if ctx.Request == nil || ctx.Request.SubmissionFollowupID == "" {
			err := fmt.Errorf("TR-002 requires the current accepted S4 planning_task followup")
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		// The S4 packet freezes the contracts already accepted at S3. TR-002
		// must not rescan the repository and pull an unrelated contract into
		// this generation; register_planning_tasks reopens the receipt and
		// validates this same followup before registering the exact S4 outputs.
		return ActionResult{Status: "committed", Detail: "retained the exact locked contract source set accepted at S3"}, nil
	}
	models, modelErr := sharedModelDocuments(state, ctx, true, accepted)
	if modelErr != nil {
		return ActionResult{Status: "failed", Detail: modelErr.Error()}, modelErr
	}
	registered := 0
	var err error
	if accepted != nil {
		for _, ref := range accepted.contracts {
			state["documents"] = appendDocument(state["documents"], map[string]any{
				"id": ref.ID, "kind": "contract", "path": ref.Path, "version": ref.Version,
				"sha256": ref.SHA256, "status": "locked", "generation": integerOf(state["baseline"].(map[string]any)["generation"]),
				"author_agent_id": ctx.Request.Actor, "registered_at": ctx.OccurredAt.UTC().Format(time.RFC3339Nano),
			})
			registered++
		}
	} else {
		registered, err = registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, projectlayout.Contracts, []string{"BE-", "FE-", "SYNC-", "CONTRACTS-"}, "contract", "locked")
	}
	if err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	return ActionResult{Status: "committed", MutationApplied: registered+models > 0,
		Detail: fmt.Sprintf("registered %d locked contract(s) and %d shared input(s)", registered, models)}, nil
}

type acceptedS3ContractBatch struct {
	contracts     []workpacket.Reference
	subjects      map[string]workpacket.Subject
	sharedOutputs map[string]bool
}

// Registration consumes the complete accepted source closure. It does not
// redo the professional review or let a delayed followup adopt newer bytes.
func (batch *acceptedS3ContractBatch) validateSources(root string, files fileview.Reader) error {
	if files == nil {
		files = fileview.Disk{Root: root}
	}
	paths := make([]string, 0, len(batch.subjects))
	for path := range batch.subjects {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if err := fileview.ReadFiles(files, paths, func(i int, data []byte, err error) error {
		path := paths[i]
		if err != nil {
			return fmt.Errorf("read accepted S3 source %s: %w", path, err)
		}
		checkout, err := workpacket.ReadFile(root, path, 4<<20)
		if err != nil {
			return fmt.Errorf("read accepted S3 checkout source %s: %w", path, err)
		}
		if SHA256(data) != batch.subjects[path].SHA256 || SHA256(checkout) != batch.subjects[path].SHA256 {
			return fmt.Errorf("accepted S3 source changed before registration: %s; preserve the accepted evidence and restore its source bytes for the original followup, or use controlled planning rework for a professional change", path)
		}
		return nil
	}); err != nil {
		return err
	}
	if view, ok := files.(interface{ Verify() error }); ok {
		return view.Verify()
	}
	return nil
}

func acceptedContractArtifacts(state map[string]any, ctx *ActionContext) (*acceptedS3ContractBatch, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["followup_event_id"] != ctx.Request.SubmissionFollowupID {
			continue
		}
		packet, found, err := loopruntime.FindContractPacket(state, fmt.Sprint(row["packet_id"]))
		if err != nil || !found {
			return nil, fmt.Errorf("S3 registration has no original contract packet: %v", err)
		}
		record, found, err := loopruntime.FindSubmission(state, packet.ID)
		if err != nil || !found {
			return nil, fmt.Errorf("S3 registration has no original reception: %v", err)
		}
		var files fileview.Reader = fileview.Disk{Root: actionRoot(state, ctx)}
		if ctx.Request.Files != nil {
			files = ctx.Request.Files
		}
		evidence, _ := state["evidence"].([]any)
		for _, raw := range evidence {
			ev, _ := raw.(map[string]any)
			if ev["id"] != record.EvidenceID {
				continue
			}
			data, err := files.ReadFile(fmt.Sprint(ev["path"]))
			if err != nil || SHA256(data) != ev["sha256"] {
				return nil, fmt.Errorf("S3 registration original evidence changed: %v", err)
			}
			var envelope struct {
				Answer   workpacket.ContractAnswer `json:"contract_answer"`
				Subjects []workpacket.Subject      `json:"subject_refs"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				return nil, err
			}
			digest, err := workpacket.ContractAnswerDigest(envelope.Answer)
			if err != nil || digest != record.ContentHash || envelope.Answer.IndexPath == nil {
				return nil, fmt.Errorf("S3 registration original answer differs")
			}
			paths := append([]string{*envelope.Answer.IndexPath}, envelope.Answer.ContractPaths...)
			batch := &acceptedS3ContractBatch{subjects: map[string]workpacket.Subject{}, sharedOutputs: map[string]bool{}}
			frozen := map[string]bool{packet.Binding.Requirement.Path: true}
			for _, ref := range packet.Inputs.Designs {
				frozen[ref.Path] = true
			}
			for _, ref := range packet.Inputs.Sources {
				frozen[ref.Path] = true
			}
			for _, path := range paths {
				frozen[path] = true
			}
			for _, subject := range envelope.Subjects {
				if _, duplicate := batch.subjects[subject.Path]; duplicate {
					return nil, fmt.Errorf("S3 registration original evidence repeats source %s", subject.Path)
				}
				batch.subjects[subject.Path] = subject
				if !frozen[subject.Path] {
					batch.sharedOutputs[subject.Path] = true
				}
			}
			refs := make([]workpacket.Reference, 0, len(paths))
			for _, path := range paths {
				found := false
				for _, subject := range envelope.Subjects {
					if subject.Path != path {
						continue
					}
					refs = append(refs, workpacket.Reference{ID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Path: path, Version: subject.Version, SHA256: subject.SHA256})
					found = true
					break
				}
				if !found {
					return nil, fmt.Errorf("S3 registration received contract is missing: %s", path)
				}
			}
			if verified, ok := files.(interface{ Verify() error }); ok {
				if err := verified.Verify(); err != nil {
					return nil, err
				}
			}
			batch.contracts = refs
			return batch, nil
		}
		return nil, fmt.Errorf("S3 registration original evidence is missing")
	}
	return nil, fmt.Errorf("S3 registration original followup is missing")
}

type acceptedS4TaskPlanningBatch struct {
	answer    workpacket.TaskPlanningAnswer
	artifacts []workpacket.Reference
}

// acceptedTaskPlanningBatch reopens the original S4 receipt inside TR-002's
// Writer transaction. Actions run after the engine moves the cursor, so only
// the read-only projection is restored to the packet's original S4 entry.
func acceptedTaskPlanningBatch(state map[string]any, ctx *ActionContext) (acceptedS4TaskPlanningBatch, error) {
	if ctx == nil || ctx.Spec.ID != "TR-002" || ctx.Request == nil || ctx.Request.SubmissionFollowupID == "" {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 requires the current accepted S4 planning_task followup")
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	var packetID string
	followupRows := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["followup_event_id"] == ctx.Request.SubmissionFollowupID {
			packetID, _ = row["packet_id"].(string)
			followupRows++
		}
	}
	if followupRows != 1 || packetID == "" {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 has no unique original S4 acceptance for its followup")
	}
	record, found, err := loopruntime.FindSubmission(state, packetID)
	if err != nil || !found {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 followup has no original accepted S4 submission: %v", err)
	}
	packet, found, err := loopruntime.FindTaskPlanningPacket(state, packetID)
	if err != nil || !found {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 followup has no original S4 packet: %v", err)
	}
	projection := make(map[string]any, len(state))
	for key, value := range state {
		projection[key] = value
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "tasks", "phase_revision": packet.Binding.StageEntry}
	if _, err := loopruntime.SubmissionFollowup(projection, ctx.Request.SubmissionFollowupID); err != nil {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 original S4 acceptance is no longer current: %w", err)
	}
	var files fileview.Reader = fileview.Disk{Root: actionRoot(state, ctx)}
	if ctx.Request.Files != nil {
		files = ctx.Request.Files
	}
	evidenceRows, _ := state["evidence"].([]any)
	var evidence map[string]any
	evidenceCount := 0
	for _, raw := range evidenceRows {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == record.EvidenceID {
			evidence = row
			evidenceCount++
		}
	}
	producers, _ := evidence["produced_by"].([]any)
	if evidenceCount != 1 || evidence["kind"] != "planning_task" || evidence["status"] != "valid" ||
		integerOf(evidence["baseline_generation"]) != packet.Binding.Generation ||
		evidence["responsibility_id"] != "Orchestrator" || len(producers) != 1 || producers[0] != "Main" || evidence["invalidated_by"] != nil {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 original S4 planning_task evidence registration is missing or changed")
	}
	evidencePath, _ := evidence["path"].(string)
	cleanEvidencePath := filepath.Clean(evidencePath)
	if evidencePath == "" || filepath.IsAbs(evidencePath) || filepath.ToSlash(cleanEvidencePath) != evidencePath || cleanEvidencePath == ".." || strings.HasPrefix(cleanEvidencePath, ".."+string(filepath.Separator)) {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 original S4 evidence path is invalid")
	}
	envelopeBytes, err := files.ReadFile(evidencePath)
	if err != nil || SHA256(envelopeBytes) != evidence["sha256"] {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 original S4 evidence bytes differ from the registered hash: %s", evidencePath)
	}
	var envelope struct {
		Answer      workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
		SubjectRefs []workpacket.Subject          `json:"subject_refs"`
	}
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("decode TR-002 original S4 evidence: %w", err)
	}
	digest, err := workpacket.TaskPlanningAnswerDigest(envelope.Answer)
	if err != nil || digest != record.ContentHash {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 original S4 professional answer differs from its accepted digest")
	}
	if err := loopruntime.ValidateTaskPlanningEnvelope(packet, record.EvidenceID, envelope.Answer, envelopeBytes); err != nil {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("revalidate TR-002 original S4 envelope: %w", err)
	}
	artifacts, err := loopruntime.TaskPlanningArtifacts(actionRoot(state, ctx), projection, packet, envelope.Answer, files)
	if err != nil {
		return acceptedS4TaskPlanningBatch{}, fmt.Errorf("revalidate complete TR-002 S4 batch and frozen source closure: %w", err)
	}
	subjects := make(map[string]workpacket.Subject, len(envelope.SubjectRefs))
	for _, subject := range envelope.SubjectRefs {
		subjects[subject.Path] = subject
	}
	for _, artifact := range artifacts {
		subject, ok := subjects[artifact.Path]
		if !ok || subject.Version != artifact.Version || subject.SHA256 != artifact.SHA256 {
			return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 S4 accepted artifact differs from current source bytes: %s", artifact.Path)
		}
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return acceptedS4TaskPlanningBatch{}, fmt.Errorf("TR-002 S4 source view moved during registration: %w", err)
		}
	}
	return acceptedS4TaskPlanningBatch{answer: envelope.Answer, artifacts: artifacts}, nil
}

// actionRegisterExecutionBatch replaces the evidence-presence placeholder
// on TR-003: the atomic lock registers the execution batch — complete TASK
// files join the already-registered contracts in documents[].
func actionRegisterExecutionBatch(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if err := dispatchPlanDocuments(state, ctx, false); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	if _, err := sharedModelDocuments(state, ctx, false, nil); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	if len(ctx.Evidence) == 0 {
		return ActionResult{Status: "failed", Detail: "execution batch evidence missing"}, fmt.Errorf("register_execution_batch: current evidence missing")
	}
	root := actionRoot(state, ctx)
	var files fileview.Reader = fileview.Disk{Root: root}
	if ctx.Request != nil && ctx.Request.Files != nil {
		files = ctx.Request.Files
	}
	registered := 0
	bound, _ := state["bound_req"].(map[string]any)
	boundID, _ := bound["id"].(string)
	reviewedBaseline, _ := state["baseline"].(map[string]any)
	reviewedDocs, _ := state["documents"].([]any)
	for _, raw := range reviewedDocs {
		doc, _ := raw.(map[string]any)
		if doc["kind"] != "task" || integerOf(doc["generation"]) != integerOf(reviewedBaseline["generation"]) {
			continue
		}
		rel, _ := doc["path"].(string)
		data, err := files.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return ActionResult{Status: "failed"}, err
		}
		if !docscope.Belongs(data, boundID) {
			return ActionResult{Status: "failed"}, fmt.Errorf("foreign TASK in registered batch: %s; repair batch scope before review", rel)
		}
		if SHA256(data) != doc["sha256"] || ParseMarkdownField(string(data), "状态", "Status") != "complete" {
			return ActionResult{Status: "failed"}, fmt.Errorf("reviewed TASK changed: %s; repeat document verification", rel)
		}
		registered++
	}
	if registered == 0 {
		return ActionResult{Status: "failed",
			Detail: "execution batch is empty — no complete TASK under docs/dev/tasks; an empty batch would lock nothing into building"}, fmt.Errorf("register_execution_batch: no complete TASK document under docs/dev/tasks — write and complete the task batch before TR-003")
	}
	// Freeze the reviewed plan and TASKs in the existing artifact lock projection.
	baseline, _ := state["baseline"].(map[string]any)
	generation := integerOf(baseline["generation"])
	docs, _ := state["documents"].([]any)
	planned := false
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		if d["kind"] == "dispatch_plan" && integerOf(d["generation"]) == generation {
			planned = true
		}
	}
	if planned {
		for _, raw := range docs {
			d, _ := raw.(map[string]any)
			if integerOf(d["generation"]) == generation && (d["kind"] == "task" || d["kind"] == "dispatch_plan") {
				d["status"] = "locked"
			}
		}
	}

	detail := fmt.Sprintf("registered %d complete task(s)", registered)
	if workstate.SupportsAssignments(state["schema_version"]) {
		start, err := captureS6MainStart(state, ctx, root)
		if err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		detail = fmt.Sprintf("%s; s6_main_start_sha256=%s", detail, start.ReceiptSHA256)
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: detail}, nil
}

// actionRegisterPlanningTasks runs on TR-002 (tasks→document_verification).
// Packet mode registers only the exact accepted S4 index and complete TASKs;
// cancelled TASKs remain validated members of the batch but are not
// registered. Legacy packet-off callers retain the disk-based path.
func actionRegisterPlanningTasks(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if ctx.Spec.ID == "TR-002" && loopruntime.PacketEnabled(state) {
		batch, err := acceptedTaskPlanningBatch(state, ctx)
		if err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		// Preserve the original index revision/registration contract. The
		// accepted batch above already validated this exact REQ index.
		if err := dispatchPlanDocuments(state, ctx, true); err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		generation := integerOf(state["baseline"].(map[string]any)["generation"])
		root := actionRoot(state, ctx)
		var files fileview.Reader = fileview.Disk{Root: root}
		if ctx.Request.Files != nil {
			files = ctx.Request.Files
		}
		indexPath := *batch.answer.IndexPath
		actor := ctx.Request.Actor
		registered := 0
		for _, ref := range batch.artifacts {
			if ref.Path == indexPath {
				registered++
				continue
			}
			data, err := files.ReadFile(ref.Path)
			if err != nil || SHA256(data) != ref.SHA256 {
				err = fmt.Errorf("accepted S4 TASK bytes changed during TR-002 registration: %s", ref.Path)
				return ActionResult{Status: "failed", Detail: err.Error()}, err
			}
			status := ParseMarkdownField(string(data), "状态", "Status")
			if status == "cancelled" {
				continue
			}
			if status != "complete" {
				err := fmt.Errorf("accepted S4 TASK status is %q, want complete or cancelled: %s", status, ref.Path)
				return ActionResult{Status: "failed", Detail: err.Error()}, err
			}
			if err := refreshRuntimeTaskDocument(state, ref.ID, ref.Path, ref.SHA256); err != nil {
				return ActionResult{Status: "failed", Detail: err.Error()}, err
			}
			state["documents"] = appendDocument(state["documents"], map[string]any{
				"id": ref.ID, "kind": "task", "path": ref.Path, "version": ref.Version,
				"sha256": ref.SHA256, "status": status, "generation": generation,
				"author_agent_id": actor, "registered_at": ctx.OccurredAt.UTC().Format(time.RFC3339Nano),
			})
			registered++
		}
		if verified, ok := files.(interface{ Verify() error }); ok {
			if err := verified.Verify(); err != nil {
				return ActionResult{Status: "failed", Detail: err.Error()}, err
			}
		}
		return ActionResult{Status: "committed", MutationApplied: registered > 0,
			Detail: fmt.Sprintf("registered the accepted S4 dispatch index and %d complete TASK(s)", registered-1)}, nil
	}
	if err := dispatchPlanDocuments(state, ctx, true); err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	registered, err := registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, projectlayout.Tasks, []string{"TASK-"}, "task", "complete")
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
	var files fileview.Reader = fileview.Disk{Root: root}
	if ctx.Request != nil && ctx.Request.Files != nil {
		files = ctx.Request.Files
	}
	if kind == "task" {
		files = semantic.ScopedPlanningFiles(root, files, planningREQ(state))
	}
	entries, err := files.ReadDir(dir)
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
		bound, _ := state["bound_req"].(map[string]any)
		boundID, _ := bound["id"].(string)
		doc, member, err := registrationDocument(files, entry, dirRel, prefixes, boundID)
		if err != nil {
			return registered, err
		}
		if !member {
			continue
		}
		id, rel, status, data := doc.ID, doc.Path, doc.Status, doc.data
		if status != wantStatus {
			// Declares a different state: a real partial batch (e.g. a draft
			// contract at TR-003). Failing here keeps subject fingerprint matching honest.
			return registered, fmt.Errorf("%s status is %q, want %q — register refuses partial batches (fix the file or finish the batch)", rel, status, wantStatus)
		}
		if kind == "task" {
			if err := refreshRuntimeTaskDocument(state, id, rel, SHA256(data)); err != nil {
				return registered, err
			}
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
		if kind == "task" {
			// S8 spec rework re-locks the TASK at a new fingerprint while a
			// prior-generation Assignment Record still pins the old one. The
			// S6 dispatch projection then refuses both the replay branch
			// (stale TASK fingerprint) and the revision branch (changed
			// sources). Re-pin same-generation records to the re-locked TASK
			// in this same Writer transaction so the record, the runtime
			// entity and the locked document agree again.
			repinStaleAssignmentTaskRefs(state, id, version, SHA256(data))
		}
		registered++
	}
	return registered, nil
}

// repinStaleAssignmentTaskRefs re-pins same-generation Assignment Records to
// a re-locked TASK fingerprint. Without it, an S8-driven specification
// rework leaves the prior S6 Assignment Record pointing at the pre-rework
// TASK while the runtime entity already carries the new one, and S6 dispatch
// refuses both branches. Work state and owner bindings are untouched; only
// the TASK source ref (version/sha) is advanced. Best-effort: rows that do
// not decode as records are left alone for the record validator.
func repinStaleAssignmentTaskRefs(state map[string]any, taskID, version, sha256 string) {
	entities, _ := state["entities"].(map[string]any)
	if entities == nil {
		return
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := integerOf(baseline["generation"])
	runtimeID, _ := state["runtime_id"].(string)
	rows, _ := entities["assignments"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if rid, _ := row["runtime_id"].(string); rid != runtimeID {
			continue
		}
		if integerOf(row["baseline_generation"]) != generation {
			continue
		}
		task, _ := row["task"].(map[string]any)
		if task == nil {
			continue
		}
		if tid, _ := task["id"].(string); tid != taskID {
			continue
		}
		if cur, _ := task["sha256"].(string); cur == sha256 {
			continue
		}
		task["version"] = version
		task["sha256"] = sha256
	}
}

// refreshRuntimeTaskDocument keeps the mutable Runtime TASK projection aligned
// with an accepted/re-registered TASK document. Work state and owner facts are
// independent lifecycle data, so this only refreshes the source path/hash.
func refreshRuntimeTaskDocument(state map[string]any, taskID, path, sha256 string) error {
	if taskID == "" || path == "" || sha256 == "" {
		return fmt.Errorf("accepted TASK document identity/path/fingerprint is incomplete")
	}
	entities, _ := state["entities"].(map[string]any)
	if entities == nil {
		return nil
	}
	tasks, _ := entities["tasks"].([]any)
	var found map[string]any
	for _, raw := range tasks {
		row, _ := raw.(map[string]any)
		if row["id"] != taskID {
			continue
		}
		if found != nil {
			return fmt.Errorf("Runtime has duplicate TASK entity %s", taskID)
		}
		found = row
	}
	if found == nil {
		return nil
	}
	found["path"] = path
	found["sha256"] = sha256
	return nil
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
	}
	if ctx.Spec.ID == "TR-016" {
		// S10's original re-review impact owns the scope. A save or later
		// recovery Hook names its tool target, not the affected product.
		ref := ctx.Evidence["change_impact_record"]
		rows, _ := state["evidence"].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if (row["id"] == ref || row["path"] == ref) && row["kind"] == "change_impact" {
				if scope := toStringSlice(row["scope_refs"]); len(scope) > 0 {
					changedPaths = scope
				}
				break
			}
		}
	}
	if len(changedPaths) == 1 && strings.TrimSpace(changedPaths[0]) == "all" {
		all = true
		changedPaths = nil
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
	if ctx.Spec.ID == "TR-027" {
		// The human problem is the new intake source. Retire only the current
		// S7/S8 entry pointers; their recorded observations and files remain.
		if review, _ := state["review"].(map[string]any); review != nil {
			review["observation_batch"] = nil
			review["investigation"] = nil
		}
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
	registered, err := registerDocumentsFromDisk(actionRoot(state, ctx), state, ctx, projectlayout.Architecture, []string{"ARCHITECTURE-"}, "design", "locked")
	if err != nil {
		return ActionResult{Status: "failed", Detail: err.Error()}, err
	}
	if registered == 0 {
		return ActionResult{Status: "failed",
			Detail: "no locked architecture document under docs/architecture — the design stage produced nothing to register"}, fmt.Errorf("register_design_documents: no ARCHITECTURE-*.md with Status locked under docs/architecture — write the architecture document before advancing")
	}
	return ActionResult{Status: "committed", MutationApplied: true,
		Detail: fmt.Sprintf("registered %d locked design document(s)", registered)}, nil
}

// actionInvalidateConsumedReviewEvidence runs on TR-004. It retires the
// exact current S5 reviewer batch and invalidates every accepted review from
// that entry, so a fix that leaves registered documents byte-identical cannot
// reuse an earlier PASS.
func actionInvalidateConsumedReviewEvidence(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	if ctx == nil || ctx.Spec.ID != "TR-004" {
		return ActionResult{Status: "failed", Detail: "TR-004 action context is missing"}, fmt.Errorf("invalidate current S5 review batch requires TR-004 context")
	}
	entry, entryOK := integerValue(ctx.From["phase_revision"])
	generation, generationOK := integerValue(ctx.From["baseline_generation"])
	currentEvidence := map[string]bool{}
	if entryOK && generationOK {
		currentEvidence = currentS5EntryEvidence(state, stringValue(state["runtime_id"]), generation, entry)
	}
	mutated := false
	retiredTeams, retiredAgents, retiredExecutions := 0, 0, 0
	if workstate.SupportsAssignments(state["schema_version"]) && entryOK && generationOK {
		var err error
		retiredTeams, retiredAgents, err = retireCurrentS5Teams(state, generation, entry)
		if err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		retiredExecutions, err = retireCurrentS5Executions(state, stringValue(state["runtime_id"]), generation, entry)
		if err != nil {
			return ActionResult{Status: "failed", Detail: err.Error()}, err
		}
		mutated = retiredTeams > 0 || retiredAgents > 0 || retiredExecutions > 0
	}
	items, _ := state["evidence"].([]any)
	invalidated := 0
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || item["kind"] != "document_review" || item["status"] != "valid" {
			continue
		}
		id, _ := item["id"].(string)
		if !citedByTransition(ctx, id) && !currentEvidence[id] {
			continue
		}
		item["status"] = "invalid"
		item["invalidated_by"] = ctx.Spec.ID
		item["invalidation_rule"] = "consumed_fix_record"
		item["invalidation_reason"] = "consumed by TR-004; all review evidence from the retired S5 entry must be refreshed after the document fix"
		invalidated++
	}
	mutated = mutated || invalidated > 0
	return ActionResult{
		Status:          "committed",
		MutationApplied: mutated,
		Detail:          fmt.Sprintf("retired %d current S5 team(s), %d reviewer(s), %d execution(s); invalidated %d review record(s)", retiredTeams, retiredAgents, retiredExecutions, invalidated),
	}, nil
}

func currentS5EntryEvidence(state map[string]any, runtimeID string, generation, entry int) map[string]bool {
	currentPackets := map[string]bool{}
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		if row == nil || row["template_id"] != "s5-document-review-v1" {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		packetEntry, entryOK := integerValue(binding["stage_entry"])
		packetGeneration, generationOK := integerValue(binding["generation"])
		if entryOK && generationOK && binding["runtime_id"] == runtimeID && packetGeneration == generation && packetEntry == entry {
			currentPackets[stringValue(row["packet_id"])] = true
		}
	}
	evidence := map[string]bool{}
	submissions, _ := entities["submissions"].([]any)
	for _, raw := range submissions {
		row, _ := raw.(map[string]any)
		if row != nil && currentPackets[stringValue(row["packet_id"])] {
			if id := stringValue(row["evidence_id"]); id != "" {
				evidence[id] = true
			}
		}
	}
	return evidence
}

func retireCurrentS5Teams(state map[string]any, generation, entry int) (int, int, error) {
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	agents, _ := entities["agents"].([]any)
	retiredTeams, retiredAgents := 0, 0
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		if team == nil || team["kind"] != "document_verifier" {
			continue
		}
		teamEntry, entryOK := integerValue(team["packet_stage_entry"])
		teamGeneration, generationOK := integerValue(team["packet_generation"])
		if !entryOK || !generationOK || teamEntry != entry || teamGeneration != generation || team["status"] == "retired" {
			continue
		}
		team["status"] = "retired"
		retiredTeams++
		teamID := stringValue(team["id"])
		for _, agentRaw := range agents {
			agent, _ := agentRaw.(map[string]any)
			if agent == nil || agent["team_id"] != teamID || (agent["role"] != "document-verifier" && agent["role"] != "document_verifier") {
				continue
			}
			if err := workstate.RecordAgentActivity(state, agent, "stopped", "shutdown_approved"); err != nil {
				return retiredTeams, retiredAgents, fmt.Errorf("retire document reviewer %s: %w", stringValue(agent["id"]), err)
			}
			retiredAgents++
		}
	}
	return retiredTeams, retiredAgents, nil
}

func retireCurrentS5Executions(state map[string]any, runtimeID string, generation, entry int) (int, error) {
	registry, _ := state["workspace"].(map[string]any)
	if registry == nil {
		return 0, nil
	}
	executions, _ := registry["executions"].(map[string]any)
	if executions == nil {
		return 0, nil
	}
	history, _ := registry["execution_history"].([]any)
	retired := 0
	for key, raw := range executions {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return retired, fmt.Errorf("encode workspace execution %s: %w", key, err)
		}
		var execution map[string]any
		if err := json.Unmarshal(encoded, &execution); err != nil {
			return retired, fmt.Errorf("decode workspace execution %s: %w", key, err)
		}
		stageEntry, entryOK := integerValue(execution["stage_entry"])
		executionGeneration, generationOK := integerValue(execution["baseline_generation"])
		if !entryOK || !generationOK || execution["purpose"] != "document_review" || execution["runtime_id"] != runtimeID || executionGeneration != generation || stageEntry != entry {
			continue
		}
		execution["status"] = "retired"
		history = append(history, execution)
		delete(executions, key)
		retired++
	}
	if retired > 0 {
		registry["execution_history"] = history
	}
	return retired, nil
}

func integerValue(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), number == float64(int(number))
	case int:
		return number, true
	case int64:
		return int(number), int64(int(number)) == number
	default:
		return 0, false
	}
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
