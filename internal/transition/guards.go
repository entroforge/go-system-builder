// Guard registry — one GuardFn per declared guard name. The registry is
// referenced by LoadCatalog (catalog.go) which fails closed if a declared
// guard is missing.
//
// BUG-PLANNING-SUBSTATE collapsed the 8 PTR-PLAN-XX stub guards plus
// planning_phase_ready/contracts_reviewed/candidate_tasks_complete (used by
// TR-002) into a single planning_complete guard. The remaining guards in
// the registry either have a real semantic body or are stubbed names that
// other transitions (verification, bug_resolution, acceptance, release audit,
// task lifecycle) still depend on.
package transition

import (
	"crypto/sha256"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/verification"
)

// GuardFn evaluates whether the supplied runtime state satisfies a declared
// guard. Returning a non-nil error rejects the transition. The state argument
// is the decoded loop-state.json map; the evidence argument is the request's
// evidence map (kind → reference).
//
// State-derived guards evaluate the runtime directly. Declarative document
// verdict guards use the evidence-backed implementation after Apply resolves
// every supplied reference against current runtime and on-disk fingerprints.
type GuardFn func(state map[string]any, evidence map[string]string) error

// GuardEnforcement makes the implementation strength explicit. A semantic
// check derives its verdict from runtime/filesystem facts. An evidence
// attestation only confirms that Apply resolved a current fingerprinted
// evidence context; the evidence producer, not this function, owns the
// domain verdict.
type GuardEnforcement string

const (
	GuardSemanticCheck       GuardEnforcement = "semantic_check"
	GuardEvidenceAttestation GuardEnforcement = "evidence_attestation"
)

type GuardRegistration struct {
	Fn          GuardFn
	Enforcement GuardEnforcement
}

var (
	guardRegistryMu sync.RWMutex
	guardRegistry   = map[string]GuardRegistration{}
)

// RegisterGuard inserts (or replaces) a guard in the registry. The registry is
// read at LoadCatalog time to fail closed on missing guards; tests and callers
// that want to override a guard may call RegisterGuard before LoadCatalog.
func RegisterGuard(name string, fn GuardFn) {
	guardRegistryMu.Lock()
	defer guardRegistryMu.Unlock()
	guardRegistry[name] = GuardRegistration{Fn: fn, Enforcement: GuardSemanticCheck}
}

// LookupGuard returns the registered GuardFn for the supplied name. The
// boolean is false when no implementation is registered, which causes
// LoadCatalog to fail closed.
func LookupGuard(name string) (GuardFn, bool) {
	guardRegistryMu.RLock()
	defer guardRegistryMu.RUnlock()
	registration, ok := guardRegistry[name]
	return registration.Fn, ok
}

// LookupGuardRegistration exposes both the executable function and its
// declared enforcement strength to the Manual, doctor, and conformance tests.
func LookupGuardRegistration(name string) (GuardRegistration, bool) {
	guardRegistryMu.RLock()
	defer guardRegistryMu.RUnlock()
	registration, ok := guardRegistry[name]
	return registration, ok
}

// GuardNames returns the registered guard names in a deterministic order. Used
// by conformance tests and by the doctor command.
func GuardNames() []string {
	guardRegistryMu.RLock()
	defer guardRegistryMu.RUnlock()
	names := make([]string, 0, len(guardRegistry))
	for name := range guardRegistry {
		names = append(names, name)
	}
	return names
}

// InitGuardRegistry populates the registry with the canonical guard set from
// BUG-001 §4b.2(b). Calling it more than once is safe; the registry is
// replaced atomically.
//
// TODO(BUG-GUARDS-OVER-ENGINEERED step 1): most guards below remain
// `evidenceBackedGuard(name)` stubs that reject only on empty evidence
// map. Their names promise semantic checks that the body does not perform —
// real semantic verification lives in `validateCurrentEvidence`
// (`engine.go:317-364`) and `verification.EvaluateCleanRound`. After
// BUG-PLANNING-SUBSTATE collapsed the planning guards into one
// planning_complete direct-check, the remaining stub population is the
// concern of BUG-GUARDS-OVER-ENGINEERED. Adding new stub entries here is
// forbidden until that inventory lands.
func InitGuardRegistry() {
	newFns := map[string]GuardFn{
		// Top-level / planning / verification / bug guards.
		// TR-001's former stub guards (req_exists / req_locked /
		// req_questions_non_blocking / pm_context_matches_req) were removed:
		// their semantics live in the bind CLI/engine prechecks and the
		// human lock itself (L3-S1 v4.x); only the cross-entity check is a
		// real transition guard.
		"no_other_active_loop": evidenceBackedGuard("no_other_active_loop"),
		"contracts_checked":    guardContractsCheckedFn,
		"tasks_checked":        guardTasksCheckedFn,
		// BUG-PLANNING-SUBSTATE: planning_phase_ready / contracts_reviewed /
		// candidate_tasks_complete are replaced by the single direct-check guard below.
		"planning_complete":         guardPlanningCompleteFn,
		"joint_document_pass":       evidenceBackedGuard("joint_document_pass"),
		"verified_versions_current": evidenceBackedGuard("verified_versions_current"),
		// L3-S6 P0-4: req_baseline_unchanged now compares the bound REQ's
		// registered sha256 against the on-disk file (real body below) —
		// TR-004/TR-007 previously accepted any non-empty evidence map.
		"req_baseline_unchanged": guardReqBaselineUnchangedFn,
		// L3-S6 P0-4: the three former evidenceBackedGuard stubs on TR-006
		// are deleted — their names promised batch semantics the bodies
		// never computed. The real evaluation lives in
		// GATE-BUILDER-BATCH-READY's applyBuilderBatchCompleteness (exact
		// TR-003 set, per-task completion + verified integration).
		// RC-06 (S7-4): the six clean-round-shaped stub names below were
		// registered but never declared by any transition in
		// docs/loop-definition.json — guard-theater inventory. Their real
		// semantics live in `verification.EvaluateCleanRound`, which the
		// DECLARED clean-round guards (clean_round_valid on TR-009,
		// clean_round_still_valid on TR-015/TR-017) already delegate to:
		//   all_required_dimensions_passed / same_review_round /
		//   no_invalidated_pass_evidence / no_open_blocking_bugs /
		//   verification_phase_clean_round_passed
		// The five ReviewPlan-finding-shaped stubs (blocking_findings_present
		// among them) had no declared consumer either; the S7 exit contract
		// is observation_batch_sealed (TR-008) + clean_round_valid (TR-009).
		// L3-S7 P0: the S7 exit guards are real semantic checks over the
		// ReviewPlan projection — TR-008 requires the sealed ObservationBatch
		// carrying the exact Finding set; TR-009 recomputes the machine
		// CleanRound over the exact Claim set.
		"observation_batch_sealed": guardObservationBatchSealedFn,
		"clean_round_valid":        guardCleanRoundValidFn,
		// L3-S7 P1: the angle_complete guards are retired with the whole
		// angle lifecycle — their intent lives in ReviewPlan Claims
		// (claim.source_refs), enforced by the plan validator.
		"acc_complete":            guardACCCurrentFn,
		"clean_round_still_valid": evidenceBackedGuard("clean_round_still_valid"),
		// L3-S7: TR-010/TR-011 no longer capture the checkpoint themselves —
		// the review verdict transaction did. This guard proves the single
		// authoritative checkpoint exists before the cursor moves.
		"pause_checkpoint_recorded":               evidenceBackedGuard("pause_checkpoint_recorded"),
		"release_audit_approved":                  guardReleaseAuditCurrentFn,
		"resume_checkpoint_valid":                 evidenceBackedGuard("resume_checkpoint_valid"),
		"baselines_unchanged":                     evidenceBackedGuard("baselines_unchanged"),
		"updated_req_locked":                      evidenceBackedGuard("updated_req_locked"),
		"human_abort_approved":                    evidenceBackedGuard("human_abort_approved"),
		"bug_phase_ready_for_full_review":         evidenceBackedGuard("bug_phase_ready_for_full_review"),
		"all_targeted_reverification_passed":      guardAllTargetedReverificationPassedFn,
		"root_cause_evidence_complete":            evidenceBackedGuard("root_cause_evidence_complete"),
		"canonical_bug_mapping_complete":          evidenceBackedGuard("canonical_bug_mapping_complete"),
		"bug_closing_contracts_complete":          evidenceBackedGuard("bug_closing_contracts_complete"),
		"repair_understanding_approved":           evidenceBackedGuard("repair_understanding_approved"),
		"repair_activation_recorded":              evidenceBackedGuard("repair_activation_recorded"),
		"repair_reports_complete":                 evidenceBackedGuard("repair_reports_complete"),
		"original_finder_reverification_complete": evidenceBackedGuard("original_finder_reverification_complete"),
		"no_accepted_bugs":                        evidenceBackedGuard("no_accepted_bugs"),
		"bug_report_review_complete":              evidenceBackedGuard("bug_report_review_complete"),
		// BUG-PLANNING-SUBSTATE: ui_impact_resolved keeps its real body; the
		// six planning-only stubs (design_gate_passed / ui_impact_none / ui_impact_changed /
		// ui_prototype_gate_passed / baseline_fingerprint_captured / traceability_complete)
		// and the three TR-002 stubs (planning_phase_ready / contracts_reviewed /
		// candidate_tasks_complete) are deleted and replaced by planning_complete.

		// Agent lifecycle.
		"prompt_contract_valid":        evidenceBackedGuard("prompt_contract_valid"),
		"readback_complete":            evidenceBackedGuard("readback_complete"),
		"document_versions_current":    evidenceBackedGuard("document_versions_current"),
		"review_feedback_recorded":     evidenceBackedGuard("review_feedback_recorded"),
		"document_conflict_recorded":   evidenceBackedGuard("document_conflict_recorded"),
		"activation_scope_valid":       evidenceBackedGuard("activation_scope_valid"),
		"write_scope_enforced":         evidenceBackedGuard("write_scope_enforced"),
		"completion_report_complete":   evidenceBackedGuard("completion_report_complete"),
		"required_evidence_present":    evidenceBackedGuard("required_evidence_present"),
		"blocker_recorded":             evidenceBackedGuard("blocker_recorded"),
		"activation_scope_still_valid": evidenceBackedGuard("activation_scope_still_valid"),
		"outputs_captured":             evidenceBackedGuard("outputs_captured"),

		// Task lifecycle (BUG-001 §4b.2(b) lines 186-192; the 3 task-entity
		// guards resolved by DV round 1 F-CORE-001/002/003 are listed last).
		"task_manifest_complete":                 evidenceBackedGuard("task_manifest_complete"),
		"task_closing_contract_passed":           evidenceBackedGuard("task_closing_contract_passed"),
		"task_versions_current":                  evidenceBackedGuard("task_versions_current"),
		"required_verification_evidence_present": evidenceBackedGuard("required_verification_evidence_present"),
		"builder_activation_recorded":            evidenceBackedGuard("builder_activation_recorded"),
		"builder_report_complete":                evidenceBackedGuard("builder_report_complete"),
		"cancellation_reason_recorded":           evidenceBackedGuard("cancellation_reason_recorded"),

		// BUG lifecycle.
		"finding_source_present":           evidenceBackedGuard("finding_source_present"),
		"bug_closing_contract_complete":    evidenceBackedGuard("bug_closing_contract_complete"),
		"canonical_id_assigned":            evidenceBackedGuard("canonical_id_assigned"),
		"rejection_reason_recorded":        evidenceBackedGuard("rejection_reason_recorded"),
		"canonical_bug_reference_present":  evidenceBackedGuard("canonical_bug_reference_present"),
		"repair_task_and_builder_present":  evidenceBackedGuard("repair_task_and_builder_present"),
		"repair_evidence_present":          evidenceBackedGuard("repair_evidence_present"),
		"original_finder_assigned":         evidenceBackedGuard("original_finder_assigned"),
		"targeted_reverification_complete": evidenceBackedGuard("targeted_reverification_complete"),
		"failure_evidence_recorded":        evidenceBackedGuard("failure_evidence_recorded"),

		// BUG-PLANNING-SUBSTATE: ui_impact_resolved stays (real body in
		// guardUIIImpactResolvedFn). The seven entries that used to live under
		// "Planning phase." in this block are deleted (see comment above).
		"ui_impact_resolved":      guardUIIImpactResolvedFn,
		"scenario_bridge_checked": guardScenarioBridgeCheckedFn,
	}
	semanticChecks := map[string]bool{
		"no_other_active_loop": true, "resume_checkpoint_valid": true,
		"contracts_checked": true, "tasks_checked": true,
		"clean_round_still_valid":   true,
		"pause_checkpoint_recorded": true,
		// RC-06 (S10-14): both guards now resolve + re-hash their evidence
		// artifact on disk, so they are honest semantic checks.
		"acc_complete":           true,
		"release_audit_approved": true,
		"planning_complete":      true, "all_targeted_reverification_passed": true,
		"ui_impact_resolved": true, "scenario_bridge_checked": true,
		"req_baseline_unchanged":   true,
		"observation_batch_sealed": true, "clean_round_valid": true,
	}
	newReg := make(map[string]GuardRegistration, len(newFns))
	for name, fn := range newFns {
		enforcement := GuardEvidenceAttestation
		if semanticChecks[name] {
			enforcement = GuardSemanticCheck
		}
		newReg[name] = GuardRegistration{Fn: fn, Enforcement: enforcement}
	}
	guardRegistryMu.Lock()
	guardRegistry = newReg
	guardRegistryMu.Unlock()
}

// evidenceBackedGuard is the shared evidence-backed implementation for declarative
// guards whose detailed verdict is carried by a fingerprinted evidence item.
// It is deliberately not an unconditional pass: Apply first resolves every
// supplied reference against current runtime evidence, then this guard rejects
// calls without that resolved evidence context. State-derived guards are
// evaluated directly below.
//
// TODO(BUG-GUARDS-OVER-ENGINEERED step 1): this helper is the source
// of the guard-theater pattern. New transitions MUST NOT wire through it
// without a semantic body of their own. Real semantic bodies live both in
// this switch (`no_other_active_loop`, `resume_checkpoint_valid`, plus the
// clean-round names delegated to `verification.EvaluateCleanRound`) and in
// the dedicated guard*Fn functions registered outside it (contracts/tasks/
// planning/bridge/UI-impact/reverification, and the transition-layer
// human_decision scope validation in validateRequest);
// every other name lands in the final `len(evidence) == 0` check, which
// is not a guard — it is a request validator.
func evidenceBackedGuard(name string) GuardFn {
	return func(state map[string]any, evidence map[string]string) error {
		switch name {
		case "no_other_active_loop":
			if err := requireFreshInactiveRuntime(state); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		case "resume_checkpoint_valid", "pause_checkpoint_recorded":
			if pause, ok := state["pause"].(map[string]any); !ok || pause == nil {
				return fmt.Errorf("%s: pause checkpoint missing", name)
			}
		case "clean_round_still_valid":
			// RC-06 (S7-4): the only DECLARED clean-round delegate left in
			// the switch. The five undeclared clean-round-shaped stub names
			// were removed from the registry (see InitGuardRegistry).
			result := verification.EvaluateCleanRound(state)
			if !result.Passed {
				return fmt.Errorf("%s: clean-round evaluation failed: %v", name, result.Reasons)
			}
		}
		if len(evidence) == 0 {
			return fmt.Errorf("%s: resolved evidence context is empty", name)
		}
		return nil
	}
}

func requireFreshInactiveRuntime(state map[string]any) error {
	if err := loopruntime.ValidateBindEligibleState(state); err != nil {
		return fmt.Errorf("requires a fresh inactive runtime (unbound, revision-independent): %w", err)
	}
	return nil
}

// guardCleanRoundValidFn is the real body behind TR-009's clean_round_valid
// guard (L3-S7 §10): the machine CleanRound is recomputed over the current
// ReviewPlan's exact Claim set by verification.EvaluateCleanRound — an
// agent hand-written aggregate PASS is not a substitute.
func guardCleanRoundValidFn(state map[string]any, _ map[string]string) error {
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		return fmt.Errorf("clean_round_valid: %v", result.Reasons)
	}
	return nil
}

// guardACCCurrentFn is the real body behind TR-015/TR-017's acc_complete
// guard (RC-06, S10-14 — formerly an evidenceBackedGuard stub that accepted
// any non-empty evidence map). It requires a CURRENT acceptance evidence
// entry in runtime.evidence[] whose on-disk artifact still matches its
// registered sha256: an invalidated, stale-round, or drifted ACC no longer
// satisfies the release-audit precondition. The same resolution rules as
// the engine's validateCurrentEvidence apply (status=valid, current baseline
// generation, current review round, fingerprint match), so the guard cannot
// be satisfied by a re-used or re-hashed envelope.
func guardACCCurrentFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	if err := acceptance.ValidateCurrentS10Evidence(root, state, "acceptance"); err != nil {
		return fmt.Errorf("acc_complete: %w", err)
	}
	return nil
}

// guardReleaseAuditCurrentFn is the real body behind TR-017's
// release_audit_approved guard (RC-06, S10-14 — same stub lineage as
// acc_complete). It requires a CURRENT release_audit evidence entry whose
// registered fingerprint still matches the on-disk audit record.
func guardReleaseAuditCurrentFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	if err := acceptance.ValidateCurrentS10Evidence(root, state, "release_audit"); err != nil {
		return fmt.Errorf("release_audit_approved: %w", err)
	}
	return nil
}

// requireCurrentEvidenceKind scans runtime.evidence[] for a valid entry of
// the supplied kind registered against the current baseline generation and
// review round, then re-hashes the artifact on disk. Returns a descriptive
// error when no such entry exists (fail-closed: an empty evidence list is a
// rejection, not a pass).
func requireCurrentEvidenceKind(state map[string]any, kind string) error {
	items, _ := state["evidence"].([]any)
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	var found map[string]any
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["status"] != "valid" {
			continue
		}
		if itemKind, _ := item["kind"].(string); itemKind != kind {
			continue
		}
		if integer(item["baseline_generation"]) != integer(baseline["generation"]) {
			continue
		}
		if evidenceRound := integer(item["review_round"]); evidenceRound > 0 && evidenceRound != integer(review["round"]) {
			continue
		}
		found = item
		break
	}
	if found == nil {
		return fmt.Errorf("no valid %s evidence entry for the current baseline/round — record the artifact before advancing", kind)
	}
	rel, _ := found["path"].(string)
	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s evidence %v carries unsafe path %q", kind, found["id"], rel)
	}
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	data, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		return fmt.Errorf("%s evidence %v unreadable at %s: %w", kind, found["id"], rel, err)
	}
	sum := sha256.Sum256(data)
	actual := fmt.Sprintf("%x", sum[:])
	want, _ := found["sha256"].(string)
	if want == "" || actual != want {
		return fmt.Errorf("%s evidence %v fingerprint drifted (registered %s…, on disk %s…) — re-record the artifact", kind, found["id"], want[:min(12, len(want))], actual[:12])
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// guardObservationBatchSealedFn is the real body behind TR-008's
// observation_batch_sealed guard (L3-S7 §3.7): the sealed ObservationBatch
// must exist for the current round, its finding_ids must equal the exact
// current-round Finding set, and an ordinary batch (complete_required_claims)
// must seal with every required Claim dispositioned — unobserved Claims are
// only legal on a critical immediate-stop batch.
func guardObservationBatchSealedFn(state map[string]any, _ map[string]string) error {
	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
		return fmt.Errorf("observation_batch_sealed: runtime review section missing")
	}
	plan, _ := reviewMap["plan"].(map[string]any)
	if plan == nil {
		return fmt.Errorf("observation_batch_sealed: no ReviewPlan registered for this round")
	}
	if status, _ := plan["status"].(string); status != "observation_sealed" {
		return fmt.Errorf("observation_batch_sealed: ReviewPlan status is %s; the batch seals automatically when the final required Claim disposition lands", status)
	}
	batch, _ := reviewMap["observation_batch"].(map[string]any)
	if batch == nil {
		return fmt.Errorf("observation_batch_sealed: no sealed ObservationBatch in the runtime")
	}
	batchRound := currentRoundOf(reviewMap)
	if round := intFrom(batch["review_round"]); round != 0 && round != batchRound {
		return fmt.Errorf("observation_batch_sealed: batch belongs to round %d, current round is %d", round, batchRound)
	}
	batchIDs := map[string]bool{}
	if raw, ok := batch["finding_ids"].([]any); ok {
		for _, value := range raw {
			if id, _ := value.(string); id != "" {
				batchIDs[id] = true
			}
		}
	}
	if len(batchIDs) == 0 {
		// The state pointer only carries ids; fall back to the schema-level
		// invariant that a sealed batch never has an empty exact set.
		return fmt.Errorf("observation_batch_sealed: sealed batch carries no finding ids")
	}
	// Exact-set check: batch finding_ids == current-round Finding entities.
	roundFindings := review.RoundFindings(state)
	actual := map[string]bool{}
	for _, row := range roundFindings {
		if id, ok := row["finding_id"].(string); ok {
			actual[id] = true
		}
	}
	for id := range batchIDs {
		if !actual[id] {
			return fmt.Errorf("observation_batch_sealed: batch references finding %s which is not a current-round Finding entity", id)
		}
	}
	for id := range actual {
		if !batchIDs[id] {
			return fmt.Errorf("observation_batch_sealed: current-round finding %s is missing from the sealed batch; the handoff must carry the exact set (L3-S7 §3.7)", id)
		}
	}
	if policy, _ := batch["drain_policy"].(string); policy != "immediate_stop" {
		if pending := review.UndispositionedRequired(state); len(pending) > 0 {
			return fmt.Errorf("observation_batch_sealed: ordinary batch sealed with unobserved required claims %v; only a critical immediate-stop batch may carry safety gaps", pending)
		}
	}
	return nil
}

func currentRoundOf(reviewMap map[string]any) int {
	return intFrom(reviewMap["round"])
}

func intFrom(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// guardReqBaselineUnchangedFn is the real body behind the
// req_baseline_unchanged guard on TR-004/TR-007 (L3-S6 §11.2): the bound
// REQ's sha256 recorded in runtime.bound_req must still match the file at
// bound_req.path. Formerly this name was an evidenceBackedGuard stub that
// accepted any non-empty evidence map — a reworked REQ could slip through
// a "return to planning" transition that is only legal for non-REQ
// findings.
func guardReqBaselineUnchangedFn(state map[string]any, _ map[string]string) error {
	bound, ok := state["bound_req"].(map[string]any)
	if !ok || bound == nil {
		return fmt.Errorf("req_baseline_unchanged: runtime has no bound REQ — rebind before transitioning")
	}
	reqPath, _ := bound["path"].(string)
	registeredSHA, _ := bound["sha256"].(string)
	if reqPath == "" || registeredSHA == "" {
		return fmt.Errorf("req_baseline_unchanged: bound REQ records no path/sha256 fingerprint")
	}
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	data, err := os.ReadFile(filepath.Join(root, reqPath))
	if err != nil {
		return fmt.Errorf("req_baseline_unchanged: bound REQ %s unreadable: %w", reqPath, err)
	}
	sum := sha256.Sum256(data)
	actual := fmt.Sprintf("%x", sum[:])
	if actual != registeredSHA {
		return fmt.Errorf(
			"req_baseline_unchanged: bound REQ %s drifted (registered %s…, on disk %s…) — the REQ baseline changed since bind; REQ-affecting findings must go through the human amendment boundary, not TR-004/TR-007",
			reqPath, registeredSHA[:12], actual[:12])
	}
	return nil
}

// guardAllTargetedReverificationPassedFn is the only guard that delegates to
// a real semantic check (see guardAllTargetedReverificationPassed below).
func guardAllTargetedReverificationPassedFn(state map[string]any, evidence map[string]string) error {
	return guardAllTargetedReverificationPassed(state)
}

// guardUIIImpactResolvedFn is the SM-003 guard (LOOP-STATE-MACHINE.md §15):
// once `req bind` registers a REQ with `ui_impact = unknown`, planning must
// pause until PM clarifies the value in the REQ's §D (待澄清). The guard is
// state-derived (it inspects bound_req.metadata.ui_impact directly) and is
// wired into the registry as `ui_impact_resolved`. Loop-Definition wiring
// is a follow-on human-decision change.
func guardUIIImpactResolvedFn(state map[string]any, _ map[string]string) error {
	bound, ok := state["bound_req"].(map[string]any)
	if !ok || bound == nil {
		return nil
	}
	metadata, _ := bound["metadata"].(map[string]any)
	if metadata == nil {
		return nil
	}
	ui, _ := metadata["ui_impact"].(string)
	if ui == "unknown" {
		reqID, _ := bound["id"].(string)
		return fmt.Errorf("ui_impact_resolved: bound REQ %s declares ui_impact=unknown; planning cannot advance until §D (待澄清问题) clarifies the value", reqID)
	}
	return nil
}

// guardPlanningCompleteFn is the BUG-PLANNING-SUBSTATE direct-check
// guard for TR-002. It collapses the three legacy stub guards
// (planning_phase_ready, contracts_reviewed, candidate_tasks_complete) into
// one rule aligned with GATE-PLANNING-TASKS-COMPLETE:
//
//   - At least one current-baseline contract document has status=locked and an
//     on-disk markdown status that matches.
//   - At least one current-baseline task document has status=complete and an
//     on-disk markdown status that matches.
//
// When runtime documents are absent (legacy harnesses), the guard falls back
// to the CONTRACTS-*.md / TASK-*.md filename patterns under docs/.
//
// On failure the error names the offending file and its observed status so
// the caller can fix it without grepping the manual. Root is taken from the
// state["root"] slot populated by Apply; the guard does not require
// evidence (the only transition that uses it is TR-002, whose required_evidence
// is empty).
// guardContractsCheckedFn runs S3's mechanical close (token reconciliation,
// clause cells, fingerprint column) as a real transition guard — the check
// happens on the natural path (PTR-PLAN-02 evaluation), not as a voluntary
// CLI invocation (L3-S3 v4.0.1: D2 wiring).
func guardContractsCheckedFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	result, err := semantic.ContractsCheck(root)
	if err != nil {
		return fmt.Errorf("contracts_checked: %w", err)
	}
	if result.Contracts == 0 {
		return fmt.Errorf("contracts_checked: no contracts found under docs/contracts — the contracts stage produced nothing; write the contracts before advancing planning")
	}
	if len(result.Problems) > 0 {
		return fmt.Errorf("contracts_checked: %d problem(s): %s", len(result.Problems), strings.Join(result.Problems, "; "))
	}
	return nil
}

// guardScenarioBridgeCheckedFn runs the S2 AC↔CASE bridge at PTR-PLAN-02
// evaluation — the single-denominator rule made a natural-path gate instead
// of a voluntary `scenario bridge` invocation (D2).
func guardScenarioBridgeCheckedFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	if err := scenario.GuardBridgeChecked(root); err != nil {
		return fmt.Errorf("scenario_bridge_checked: %w", err)
	}
	return nil
}

// guardTasksCheckedFn runs S4's mechanical close (batch quality: coverage,
// DAG, closing contracts) at TR-002 evaluation time. Like contracts_checked,
// the real check happens on the natural path (TR-002 evaluation), not as a
// voluntary CLI invocation (L3-S4 v4.0.1).
func guardTasksCheckedFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		root = "."
	}
	result, err := semantic.TasksCheck(root)
	if err != nil {
		return fmt.Errorf("tasks_checked: %w", err)
	}
	if len(result.Problems) > 0 {
		return fmt.Errorf("tasks_checked: %d problem(s): %s", len(result.Problems), strings.Join(result.Problems, "; "))
	}
	return nil
}

// guardPlanningCompleteFn is S4's state-readiness gate (L3-S4 v4.0.1).
// Contracts: current-baseline documents[] registration is the authority —
// PTR-PLAN-02 registers locked contracts before TR-002 can fire, so the
// former filename-scan fallback would only silently weaken the check.
// Tasks: the batch is registered by TR-002's own action
// (register_planning_tasks); at gate time the disk batch must already be
// fully complete (or cancelled). "Disk-consistent" means the markdown Status
// field matches — fingerprints are owned by registration and reachability.
func guardPlanningCompleteFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		// Defensive: when the guard runs in a unit test the engine is not in
		// the call path; fall back to "." so the helper can be exercised.
		root = "."
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := integer(baseline["generation"])
	documents, _ := state["documents"].([]any)
	hasLockedContract := false
	for _, raw := range documents {
		doc, ok := raw.(map[string]any)
		if !ok || integer(doc["generation"]) != generation {
			continue
		}
		if kind, _ := doc["kind"].(string); kind != "contract" {
			continue
		}
		status, _ := doc["status"].(string)
		path, _ := doc["path"].(string)
		if !strings.EqualFold(status, "locked") {
			continue
		}
		if err := verifyDocumentStatusOnDisk(root, path, "locked"); err != nil {
			return fmt.Errorf("planning not complete: contract %s: %w", path, err)
		}
		hasLockedContract = true
	}
	if !hasLockedContract {
		// Phase-aware routing: from the contracts phase the
		// PTR-PLAN-02 transition is the natural next PreToolUse advance;
		// from the tasks phase it has already fired and cannot re-fire —
		// the actionable gap there is the contract file's own Status field.
		if lifecycle, _ := state["lifecycle"].(map[string]any); lifecycle != nil {
			if phase, _ := lifecycle["phase"].(string); phase == "tasks" {
				return fmt.Errorf("planning not complete: no locked contract registered at generation %d — PTR-PLAN-02 already advanced past contracts; flip the contract markdown Status to `locked` (a finalized contract declares locked at authoring time, see docs/agent-protocol.md#s3) and TR-002 itself re-registers locked contracts when it commits", generation)
			}
		}
		return fmt.Errorf("planning not complete: no locked contract registered at generation %d — PTR-PLAN-02 (contracts→tasks) fires on the next PreToolUse and registers contracts whose markdown Status is `locked` (see docs/agent-protocol.md#s3); TR-002 does not scan filenames", generation)
	}
	_, _, problems, err := semantic.TaskBatchComplete(root)
	if err != nil {
		return fmt.Errorf("planning not complete: %w", err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("planning not complete: %s", strings.Join(problems, "; "))
	}
	return nil
}

func verifyDocumentStatusOnDisk(root, relPath, required string) error {
	if relPath == "" {
		return fmt.Errorf("document path is empty")
	}
	data, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		return fmt.Errorf("not readable: %w", err)
	}
	got := markdownStatusField(string(data))
	if !strings.EqualFold(got, required) {
		return fmt.Errorf("status=%q, want %q", got, required)
	}
	return nil
}

// markdownStatusField extracts the value of a top-level `> 状态：value` or
// `> Status: value` blockquote line. Returns the trimmed value when found,
// empty string otherwise. Mirrors the behavior the harness uses elsewhere
// for the same field; intentionally narrow so a stray mid-document status
// line cannot satisfy the check.
func markdownStatusField(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ">") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		for _, sep := range []string{"：", ":"} {
			parts := strings.SplitN(body, sep, 2)
			if len(parts) != 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			if strings.EqualFold(name, "状态") || strings.EqualFold(name, "status") {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// mustInitGuardRegistry panics if the canonical guard set is missing any of
// the three task-entity guards. Invoked once from package init so the
// registry invariant is enforced even when callers forget to call
// InitGuardRegistry themselves.
func mustInitGuardRegistry() {
	InitGuardRegistry()
	for _, name := range []string{
		"required_verification_evidence_present",
		"builder_activation_recorded",
		"builder_report_complete",
	} {
		if _, ok := LookupGuard(name); !ok {
			panic(fmt.Sprintf("guard %s must be registered in guardRegistry", name))
		}
	}
}

func init() {
	mustInitGuardRegistry()
}
