// GuardSpec registry — the documentation layer that tells an agent what each
// guard verifies so the agent can self-check before calling `forward`.
//
// One sentence per guard. No "What/Fail/Recovery" structure — just the rule
// in plain terms. The runtime registry (guards.go) decides pass/fail; this
// registry tells the reader what's being checked.
//
// Convention: every guard in the runtime registry SHOULD have a matching spec
// here. Missing specs surface as "(no spec)" in generated markdown — a
// visible signal of documentation debt, not a silent gap.
package transition

// GuardSpec is the documentation payload for one guard ID.
type GuardSpec struct {
	// ID matches the guard name declared in loop-definition.json and the key
	// in guardRegistry.
	ID string
	// Check is one sentence in agent-facing terms: what must be true for this
	// guard to pass. Reads as a statement of fact, not as "this guard checks
	// that …". Example: "The REQ file declares status=locked and a version."
	Check string
}

// guardSpecRegistry maps guard IDs to their one-sentence documentation.
var guardSpecRegistry = map[string]GuardSpec{
	"acc_complete": {
		ID:    "acc_complete",
		Check: "A valid acceptance evidence entry for the current baseline generation and review round exists in runtime.evidence[] and its registered sha256 still matches the on-disk acceptance record (RC-06 S10-14: invalidated, stale-round or drifted ACC envelopes are rejected, not attested).",
	},
	"activation_scope_still_valid": {
		ID:    "activation_scope_still_valid",
		Check: "The agent's `write_scope` and `read_scope` declared at activation still cover the documents touched while resolving the blocker, per runtime.entities.agents[].activation_scope.",
	},
	"activation_scope_valid": {
		ID:    "activation_scope_valid",
		Check: "The agent's `activation_scope` (write_scope, read_scope, owner fields) recorded in runtime.entities.agents[] matches the contract referenced by the activation request.",
	},
	"all_builder_tasks_in_review": {
		ID:    "all_builder_tasks_in_review",
		Check: "Every TASK assignment in the current Builder workgroup has reached at least the `reported` state in runtime.entities.tasks[].state.",
	},
	"all_targeted_reverification_passed": {
		ID:    "all_targeted_reverification_passed",
		Check: "Every P0 BUG in runtime.entities.bugs[] has advanced past `retesting`/`fixing`/`investigating` so no blocking bug remains awaiting targeted re-verification.",
	},
	"baselines_unchanged": {
		ID:    "baselines_unchanged",
		Check: "Every document fingerprint captured at pause time matches the on-disk file, so the resume cannot quietly advance on drifted inputs. The re-hash runs in TR-019's restore_from_pause action (fail-closed, sentinel ErrBaselineDrift routes the CLI to amendment); the guard body itself only rejects an empty evidence map.",
	},
	"blocker_recorded": {
		ID:    "blocker_recorded",
		Check: "A blocker evidence item describing the impediment is referenced from runtime.evidence[] for the agent (or task) that raised `work_blocked`/`task_blocked`.",
	},
	"bug_closing_contract_complete": {
		ID:    "bug_closing_contract_complete",
		Check: "The single-BUG closing contract (root-cause, repair plan, and reverification plan) described in docs/rules/bugfix-review.md is referenced by evidence for the BUG leaving `investigating`.",
	},
	"bug_closing_contracts_complete": {
		ID:    "bug_closing_contracts_complete",
		Check: "Every BUG raised in the current review round has its closing-contract evidence referenced from runtime.evidence[] before the verification phase can advance.",
	},
	"bug_phase_ready_for_full_review": {
		ID:    "bug_phase_ready_for_full_review",
		Check: "Every BUG from the round has reached `bug_phase_ready_for_full_review`, i.e. targeted re-verification is complete and the bug sub-machine is ready to fold back into the main verification review.",
	},
	"builder_activation_recorded": {
		ID:    "builder_activation_recorded",
		Check: "An activation evidence item referencing the Builder assignment is recorded in runtime.evidence[] with a fingerprint matching the on-disk activation record under docs/tasks/.",
	},
	"builder_report_complete": {
		ID:    "builder_report_complete",
		Check: "The Builder's completion_report evidence item is referenced from runtime.evidence[] and its fingerprint matches the report file recorded under docs/tasks/.",
	},
	"builder_reports_complete": {
		ID:    "builder_reports_complete",
		Check: "Each Builder assignment's runtime record references a valid completion_report evidence item with a matching fingerprint.",
	},
	"cancellation_reason_recorded": {
		ID:    "cancellation_reason_recorded",
		Check: "A cancellation-reason evidence item is referenced from runtime.evidence[] explaining why the TASK in runtime.entities.tasks[] moved to `cancelled` instead of `done`.",
	},
	"canonical_bug_mapping_complete": {
		ID:    "canonical_bug_mapping_complete",
		Check: "Each finding evidence item in the round maps to a canonical BUG id in runtime.entities.bugs[] so the verification phase cannot advance on unresolved duplicates.",
	},
	"canonical_bug_reference_present": {
		ID:    "canonical_bug_reference_present",
		Check: "The BUG being marked `duplicate` references a canonical BUG id (matching `^BUG-[0-9]{3,}$`) that already exists in runtime.entities.bugs[].",
	},
	"canonical_id_assigned": {
		ID:    "canonical_id_assigned",
		Check: "The BUG being accepted has a canonical `id` matching `^BUG-[0-9]{3,}$` recorded in runtime.entities.bugs[] and referenced by the acceptance evidence.",
	},
	"clean_round_still_valid": {
		ID:    "clean_round_still_valid",
		Check: "verification.EvaluateCleanRound still reports a passing clean round at the current baseline generation, so neither baselines nor evidence drifted between round capture and release.",
	},
	"completion_report_complete": {
		ID:    "completion_report_complete",
		Check: "The agent's completion_report evidence item is referenced from runtime.evidence[] and its fingerprint matches the on-disk report captured under the agent's working directory.",
	},
	"delivery_round_passed": {
		ID:    "delivery_round_passed",
		Check: "The Delivery Verifier team's most recent round result recorded in runtime.evidence[] is a `pass` for the active review round.",
	},
	"delivery_team_complete": {
		ID:    "delivery_team_complete",
		Check: "A Delivery Verifier team manifest is registered in runtime.entities.teams[] listing every mandatory responsibility (VER-REQ-GAP, VER-SPEC-GAP, VER-MODULE-COMPLETE) plus any risk-triggered ones.",
	},
	"document_conflict_recorded": {
		ID:    "document_conflict_recorded",
		Check: "A document-conflict evidence item describing the conflicting sections is referenced from runtime.evidence[] for the agent that reported `document_conflict_reported`.",
	},
	"document_versions_current": {
		ID:    "document_versions_current",
		Check: "The versions of every contract/task document the agent read during readback still match the fingerprints recorded in runtime.evidence[], so the agent is approving against current inputs.",
	},
	"failure_evidence_recorded": {
		ID:    "failure_evidence_recorded",
		Check: "A targeted-reverification failure evidence item (failed assertion, logs, and failing module path) is referenced from runtime.evidence[] for the BUG sent back from `retesting` to `investigating`.",
	},
	"finding_source_present": {
		ID:    "finding_source_present",
		Check: "The BUG being promoted from `draft` carries a non-empty `path` pointing at the finding file on disk and the finding_source evidence is referenced from runtime.evidence[].",
	},
	"human_abort_approved": {
		ID:    "human_abort_approved",
		Check: "The transition's evidence validation enforces that the cited human_decision evidence is current and scoped to `runtime_abort:<runtime_id>@<revision>` (human_decision_scope on TR-021/TR-030) — one approval authorizes exactly one abort at one revision; the guard body itself only rejects an empty evidence map.",
	},
	"no_accepted_bugs": {
		ID:    "no_accepted_bugs",
		Check: "No BUG in runtime.entities.bugs[] for the current review round is in state `accepted`, `assigned`, `fixing`, or `retesting`. Used by TR-022 to confirm a finding-level Loop exit to verification is safe (no accepted BUG requires the S9 repair flow).",
	},
	"bug_report_review_complete": {
		ID:    "bug_report_review_complete",
		Check: "Every blocking S7 finding has a recorded disposition in runtime.evidence[] (accepted canonical BUG, rejected BUG, duplicate link, spec rework handoff, or REQ change pause). Used by TR-022 to confirm the orchestrator has classified every blocking finding before exiting the bug_resolution phase.",
	},
	"no_other_active_loop": {
		ID:    "no_other_active_loop",
		Check: "No other runtime is currently bound to a different REQ; the Loop model allows one active REQ per project.",
	},
	"original_finder_assigned": {
		ID:    "original_finder_assigned",
		Check: "The BUG about to enter `retesting` lists at least one agent id in `original_finder_agent_ids` and that agent is recorded as the re-tester in runtime.entities.agents[].",
	},
	"original_finder_reverification_complete": {
		ID:    "original_finder_reverification_complete",
		Check: "An original-finder re-verification evidence item is referenced from runtime.evidence[] confirming the agent that raised the BUG has re-tested the fix.",
	},
	"outputs_captured": {
		ID:    "outputs_captured",
		Check: "Every artifact the agent produced before shutdown is referenced from runtime.evidence[] so the stopped agent leaves no orphaned outputs.",
	},
	"pm_context_matches_req": {
		ID:    "pm_context_matches_req",
		Check: "docs/project-map.md current stage and bound REQ references are consistent with the REQ being bound.",
	},
	"prompt_contract_valid": {
		ID:    "prompt_contract_valid",
		Check: "The agent's prompt contract (referenced from runtime.entities.agents[].prompt_contract) declares the role, write_scope, read_scope, and owner fields required by docs/agent-protocol.md.",
	},
	"qa_round_passed": {
		ID:    "qa_round_passed",
		Check: "The QA team's most recent round result recorded in runtime.evidence[] is a `pass` for the active review round.",
	},
	"qa_team_complete": {
		ID:    "qa_team_complete",
		Check: "A QA team manifest is registered in runtime.entities.teams[] with the responsibilities required by the locked REQ's risk profile (e.g. VER-QA-REGRESSION, VER-QA-SMOKE).",
	},
	"readback_complete": {
		ID:    "readback_complete",
		Check: "A readback evidence item (per docs/agent-protocol.md readback protocol) is referenced from runtime.evidence[] confirming the agent has acknowledged the prompt contract.",
	},
	"rejection_reason_recorded": {
		ID:    "rejection_reason_recorded",
		Check: "A rejection-reason evidence item describing why the BUG was rejected is referenced from runtime.evidence[] for the BUG moved to `rejected`.",
	},
	"release_audit_approved": {
		ID:    "release_audit_approved",
		Check: "A valid release_audit evidence entry for the current baseline generation and review round exists in runtime.evidence[] and its registered sha256 still matches the on-disk audit record (RC-06 S10-14: the guard resolves and re-hashes the artifact instead of attesting an evidence map).",
	},
	"repair_activation_recorded": {
		ID:    "repair_activation_recorded",
		Check: "A repair-activation evidence item is referenced from runtime.evidence[] confirming the assigned Builder has started the repair task recorded in runtime.entities.tasks[].",
	},
	"repair_evidence_present": {
		ID:    "repair_evidence_present",
		Check: "A repair evidence item (diff, tests, logs) is referenced from runtime.evidence[] for the BUG moved from `assigned` to `fixed`.",
	},
	"repair_reports_complete": {
		ID:    "repair_reports_complete",
		Check: "Every repair task spawned for BUGs in the round has a completion_report evidence item referenced from runtime.evidence[].",
	},
	"repair_task_and_builder_present": {
		ID:    "repair_task_and_builder_present",
		Check: "The BUG moved from `accepted` to `assigned` references a repair TASK id in runtime.entities.tasks[] and the assigned Builder agent id in runtime.entities.agents[].",
	},
	"repair_understanding_approved": {
		ID:    "repair_understanding_approved",
		Check: "An understanding-approval evidence item is referenced from runtime.evidence[] confirming the assigned Builder understood the root-cause writeup before activation.",
	},
	"req_baseline_unchanged": {
		ID:    "req_baseline_unchanged",
		Check: "The locked REQ's sha256 recorded in runtime.bound_req still matches the file at runtime.bound_req.path, so the rework loop cannot silently advance on a changed REQ.",
	},
	"req_exists": {
		ID:    "req_exists",
		Check: "A REQ file exists at the path supplied to `loop-harness req bind`.",
	},
	"req_locked": {
		ID:    "req_locked",
		Check: "The REQ file declares status=locked and a non-empty version field in its frontmatter.",
	},
	"req_questions_non_blocking": {
		ID:    "req_questions_non_blocking",
		Check: "Any open questions recorded in the REQ's 待澄清问题 section are tagged non-blocking, or none exist.",
	},
	"required_evidence_present": {
		ID:    "required_evidence_present",
		Check: "Every evidence kind listed in the transition's `required_evidence` is referenced from runtime.evidence[] with a fingerprint matching the on-disk artifact.",
	},
	"required_verification_evidence_present": {
		ID:    "required_verification_evidence_present",
		Check: "The verification evidence required by the TASK's closing contract (per docs/tasks/TASK-template.md) is referenced from runtime.evidence[] before the TASK can move from `review` to `done`.",
	},
	"resume_checkpoint_valid": {
		ID:    "resume_checkpoint_valid",
		Check: "A pause checkpoint exists at runtime.pause with a non-empty `reason` and `required_human_action`, so the resume transition has a captured state to restore.",
	},
	"review_feedback_recorded": {
		ID:    "review_feedback_recorded",
		Check: "A review-feedback evidence item describing the rejection is referenced from runtime.evidence[] for the agent sent back from `understanding_submitted` to `reading`.",
	},
	"root_cause_evidence_complete": {
		ID:    "root_cause_evidence_complete",
		Check: "A root-cause evidence item (failure mode, triggering input, and minimal-repro path) is referenced from runtime.evidence[] for the BUG promoted out of `investigating`.",
	},
	"targeted_reverification_complete": {
		ID:    "targeted_reverification_complete",
		Check: "A targeted-reverification evidence item is referenced from runtime.evidence[] confirming the re-test for the specific BUG has passed before the BUG moves from `retesting` to `closed`.",
	},
	"task_manifest_complete": {
		ID:    "task_manifest_complete",
		Check: "The TASK moved from `candidate` to `reviewed` references a complete task manifest on disk under docs/tasks/ matching docs/tasks/TASK-template.md.",
	},
	"task_versions_current": {
		ID:    "task_versions_current",
		Check: "Every contract and task document the blocked TASK depends on still matches the fingerprints captured at lock time, so the resumed TASK is not working against drifted inputs.",
	},
	"ui_impact_resolved": {
		ID:    "ui_impact_resolved",
		Check: "runtime.bound_req.metadata.ui_impact is not `unknown`, so the planning phase is not paused on the SM-003 gate waiting for §11 of the REQ to clarify UI impact.",
	},
	"updated_req_locked": {
		ID:    "updated_req_locked",
		Check: "The updated REQ file referenced by the re-bind request declares status=locked with a strictly higher version than the currently bound REQ, and its sha256 matches the on-disk file.",
	},
	"verification_team_manifest_complete": {
		ID:    "verification_team_manifest_complete",
		Check: "A Delivery Verifier team manifest is registered in runtime.entities.teams[] with all mandatory responsibilities (VER-REQ-GAP, VER-SPEC-GAP, VER-MODULE-COMPLETE) plus any risk-triggered responsibilities.",
	},
	"verified_versions_current": {
		ID:    "verified_versions_current",
		Check: "Every current-generation registered document still matches its on-disk sha, so the lock cannot advance on drifted inputs. The real check runs in GATE-DOCUMENT-PASS's registered-document drift screen (a `document_drift:<path>` conflict blocks the gate); the guard body itself only rejects an empty evidence map.",
	},
	"write_scope_enforced": {
		ID:    "write_scope_enforced",
		Check: "Every document the agent intends to touch while in `working` falls within the `write_scope` declared in runtime.entities.agents[].activation_scope, per docs/hook-policy.json.",
	},
	"planning_complete": {
		ID:    "planning_complete",
		Check: "At least one current-baseline contract document has status=locked with a matching on-disk markdown Status field (contracts are registered by PTR-PLAN-02), AND every docs/tasks/TASK-*.md declares status complete or cancelled with at least one complete (the batch is registered by TR-002's own register_planning_tasks action). Fingerprints are owned by registration and reachability, not re-checked here.",
	},
	"scenario_bridge_checked": {
		ID:    "scenario_bridge_checked",
		Check: "S2's AC↔CASE bridge (scenario.GuardBridgeChecked) runs at PTR-PLAN-02: every AC of the bound REQ reaches a rule via FR source_refs (with branches), or carries an endorsed N/A (NFR id / §A4). With no module packages at all, only fully N/A-endorsed REQs pass — an AC pointing at FR- with nothing citing it is a broken denominator.",
	},
	"contracts_checked": {
		ID:    "contracts_checked",
		Check: "S3's mechanical close (semantic.ContractsCheck) runs at PTR-PLAN-02: contract token references resolve against REQ FR tables and module packages, clause cells point at known contracts, and fingerprint columns match disk.",
	},
	"tasks_checked": {
		ID:    "tasks_checked",
		Check: "S4's mechanical close (semantic.TasksCheck) runs at TR-002: the TASK batch is fully complete (cancelled tasks excluded), every task has an existing primary contract and a Closing Contract block, clause coverage between the CONTRACTS index universe and TASK §3 declarations closes in both directions, and the §8 dependency graph is acyclic (cycle path reported).",
	},
}

// LookupGuardSpec returns the documentation payload for the named guard. The
// boolean is false when no spec is registered; callers rendering markdown
// should emit a "(no spec)" placeholder so missing docs are visible.
func LookupGuardSpec(id string) (GuardSpec, bool) {
	spec, ok := guardSpecRegistry[id]
	return spec, ok
}
