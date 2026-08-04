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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
		"req_exists":                 evidenceBackedGuard("req_exists"),
		"req_locked":                 evidenceBackedGuard("req_locked"),
		"req_questions_non_blocking": evidenceBackedGuard("req_questions_non_blocking"),
		"pm_context_matches_req":     evidenceBackedGuard("pm_context_matches_req"),
		"no_other_active_loop":       evidenceBackedGuard("no_other_active_loop"),
		// BUG-PLANNING-SUBSTATE: planning_phase_ready / contracts_reviewed /
		// candidate_tasks_complete are replaced by the single direct-check guard below.
		"planning_complete":                     guardPlanningCompleteFn,
		"joint_document_pass":                   evidenceBackedGuard("joint_document_pass"),
		"verified_versions_current":             evidenceBackedGuard("verified_versions_current"),
		"req_baseline_unchanged":                evidenceBackedGuard("req_baseline_unchanged"),
		"all_builder_tasks_in_review":           evidenceBackedGuard("all_builder_tasks_in_review"),
		"builder_reports_complete":              evidenceBackedGuard("builder_reports_complete"),
		"verification_team_manifest_complete":   evidenceBackedGuard("verification_team_manifest_complete"),
		"blocking_findings_present":             evidenceBackedGuard("blocking_findings_present"),
		"same_review_round":                     evidenceBackedGuard("same_review_round"),
		"all_required_dimensions_passed":        evidenceBackedGuard("all_required_dimensions_passed"),
		"no_invalidated_pass_evidence":          evidenceBackedGuard("no_invalidated_pass_evidence"),
		"no_open_blocking_bugs":                 evidenceBackedGuard("no_open_blocking_bugs"),
		"verification_phase_clean_round_passed": evidenceBackedGuard("verification_phase_clean_round_passed"),
		// REQ-003 TASK-003-C (FR-009): the three legacy evidenceBackedGuard
		// stubs on the DV/QA/E2E phase transitions are replaced by the
		// dedicated angle_complete guards. The legacy ids are intentionally
		// REMOVED from the registry (no longer wired into any transition);
		// the evidenceBackedGuard helper itself is preserved for the
		// remaining declarative guards above.
		"delivery_angle_complete":                 guardDeliveryAngleCompleteFn,
		"qa_angle_complete":                       guardQAAngleCompleteFn,
		"e2e_angle_complete":                      guardE2EAngleCompleteFn,
		"acc_complete":                            evidenceBackedGuard("acc_complete"),
		"clean_round_still_valid":                 evidenceBackedGuard("clean_round_still_valid"),
		"release_audit_approved":                  evidenceBackedGuard("release_audit_approved"),
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
		"ui_impact_resolved": guardUIIImpactResolvedFn,
	}
	semanticChecks := map[string]bool{
		"no_other_active_loop": true, "resume_checkpoint_valid": true,
		"same_review_round": true, "all_required_dimensions_passed": true,
		"no_invalidated_pass_evidence": true, "no_open_blocking_bugs": true,
		"verification_phase_clean_round_passed": true, "clean_round_still_valid": true,
		"planning_complete": true, "all_targeted_reverification_passed": true,
		"ui_impact_resolved": true,
		// REQ-003 TASK-003-C: the three angle_complete guards run semantic
		// checks against on-disk angle_declaration + team_manifest evidence
		// (FR-002 + FR-003 + FR-004 + FR-010). They are not declarative
		// attestation guards.
		"delivery_angle_complete": true, "qa_angle_complete": true, "e2e_angle_complete": true,
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
// without a semantic body of their own. The switch above carries the only
// four real checks (`no_other_active_loop`, `resume_checkpoint_valid`, plus
// the clean-round names delegated to `verification.EvaluateCleanRound`);
// every other name lands in the final `len(evidence) == 0` check, which
// is not a guard — it is a request validator.
func evidenceBackedGuard(name string) GuardFn {
	return func(state map[string]any, evidence map[string]string) error {
		switch name {
		case "no_other_active_loop":
			if err := requireFreshInactiveRuntime(state); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		case "resume_checkpoint_valid":
			if pause, ok := state["pause"].(map[string]any); !ok || pause == nil {
				return fmt.Errorf("%s: pause checkpoint missing", name)
			}
		case "same_review_round", "all_required_dimensions_passed", "no_invalidated_pass_evidence", "no_open_blocking_bugs", "verification_phase_clean_round_passed", "clean_round_still_valid":
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
	if err := loopruntime.ValidateFreshInactiveState(state); err != nil {
		return fmt.Errorf("requires a fresh inactive runtime: %w", err)
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
// pause until PM clarifies the value in §11 of the REQ. The guard is
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
		return fmt.Errorf("ui_impact_resolved: bound REQ %s declares ui_impact=unknown; planning cannot advance until §11 clarifies the value", reqID)
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
func guardPlanningCompleteFn(state map[string]any, _ map[string]string) error {
	root, _ := state["root"].(string)
	if root == "" {
		// Defensive: when the guard runs in a unit test the engine is not in
		// the call path; fall back to "." so the helper can be exercised.
		root = "."
	}
	if err := checkPlanningDocumentsComplete(root, state); err == nil {
		return nil
	}
	if err := checkArtifactStatus(root, "docs/contracts", "CONTRACTS-*.md", "locked", "planning"); err != nil {
		return err
	}
	return checkArtifactStatus(root, "docs/tasks", "TASK-*.md", "complete", "planning")
}

// checkPlanningDocumentsComplete mirrors GATE-PLANNING-TASKS-COMPLETE: it
// inspects current-baseline runtime documents instead of legacy filename
// patterns so organic fixtures (e.g. BE-039-loop-controller.md) satisfy TR-002
// when contracts and tasks are locked/complete in state and on disk.
func checkPlanningDocumentsComplete(root string, state map[string]any) error {
	baseline, _ := state["baseline"].(map[string]any)
	generation := integer(baseline["generation"])
	documents, _ := state["documents"].([]any)
	hasLockedContract := false
	hasCompleteTask := false
	for _, raw := range documents {
		doc, ok := raw.(map[string]any)
		if !ok || integer(doc["generation"]) != generation {
			continue
		}
		kind, _ := doc["kind"].(string)
		status, _ := doc["status"].(string)
		path, _ := doc["path"].(string)
		switch {
		case kind == "contract" && strings.EqualFold(status, "locked"):
			if err := verifyDocumentStatusOnDisk(root, path, "locked"); err != nil {
				return fmt.Errorf("planning not complete: contract %s: %w", path, err)
			}
			hasLockedContract = true
		case kind == "task" && strings.EqualFold(status, "complete"):
			if err := verifyDocumentStatusOnDisk(root, path, "complete"); err != nil {
				return fmt.Errorf("planning not complete: task %s: %w", path, err)
			}
			hasCompleteTask = true
		}
	}
	if !hasLockedContract || !hasCompleteTask {
		return fmt.Errorf("planning not complete: runtime documents missing locked contract or complete task at generation %d", generation)
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

// checkArtifactStatus returns nil when at least one file matching `pattern`
// exists in `dir` whose markdown carries `status: <required>` on a top-level
// blockquote line. Returns a direct error message naming the file and the
// observed status so the caller can fix it without grepping the manual.
// scope is the error prefix (e.g. "planning") used in the rejection line.
//
// Used by guardPlanningCompleteFn; exported here so it can be reused by
// future direct-check guards.
func checkArtifactStatus(root, dir, pattern, required, scope string) error {
	fullDir := filepath.Join(root, dir)
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return fmt.Errorf("%s not complete: %s not readable: %v", scope, dir, err)
	}
	var observed []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matched, err := filepath.Match(pattern, entry.Name())
		if err != nil || !matched {
			continue
		}
		path := filepath.Join(fullDir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			observed = append(observed, fmt.Sprintf("%s (unreadable)", entry.Name()))
			continue
		}
		got := markdownStatusField(string(data))
		if strings.EqualFold(got, required) {
			return nil
		}
		observed = append(observed, fmt.Sprintf("%s status=%q", entry.Name(), got))
	}
	if len(observed) == 0 {
		return fmt.Errorf("%s not complete: no %s matching %s in %s", scope, pattern, pattern, dir)
	}
	return fmt.Errorf("%s not complete: %s — none has status=%q (observed: %s)",
		scope, dir, required, strings.Join(observed, "; "))
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
