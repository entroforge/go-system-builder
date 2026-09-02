# loop-harness — Transition Checklist

> What `loop-harness` checks at every transition. Read the
> relevant section before calling `forward`; verify each bullet
> before requesting the harness to advance.

- **Path**: `loop-harness.md`
- **Harness version**: dev
- **Loop definition SHA-256**: `082fa55027edaa1a382f7b8eb1e7bb963b36f559b0cb66bb2dcc28abb7121232`

---

## Controller recovery protocol

The Hook is an event trigger for the Loop Controller, not only a guard. On `SessionStart`, `PreCompact`, `SubagentStart`, `SubagentStop`, and `TeammateIdle`, the Controller reads the Runtime, refreshes the resumable Milestone through CAS, and emits a positive `LOOP RECOVERY` packet.

### Bootstrap binding boundary

`revision` has no global maximum. A Hook/controller checkpoint may advance the inactive bootstrap runtime before `TR-001`; binding lets the Writer read and commit the current Runtime under lock, archives the complete inactive state/journal pair, and installs a new `loop-REQ-*` runtime at revision `0` with an empty active journal. The `binding_receipt` carries `event=req_bound`, the approved REQ, and source runtime hashes. Do not edit revision by hand or reuse a pre-bind runtime snapshot after binding; the runtime identity changes and stale identities are rejected.

1. Read the `Next` action and current `Stage` from the Hook packet, then follow its `Read in order` list.
2. Read the linked `docs/agent-protocol.md#sN` section before acting.
3. If blocked or the Runtime is unclear, read this Manual. Use `runtime reconcile` only when the Hook reports an integrity/CAS recovery condition; do not call `status`/`next` during normal continuation. When the live Quality Gate checklist is unclear, run `loop-harness ready` (diagnostics; never hand-push a Transition from it). `doctor` is structural schema/manual/policy_ref only — not stage readiness or runtime health; use `loop-harness health` for cumulative runtime signals. For the complete S7→S8→S9→S7→S10 action sequence and compatibility notes, run `loop-harness actions`.
4. Execute the one missing deliverable/evidence named by Hook/`ready` `missing[]`; do not invent a parallel lifecycle.
5. For `SubagentStop`, complete the report, worktree review, merge-back to the current `develop` integration branch and `completion_ack` checklist before acknowledging the stop. For `TeammateIdle`, re-wake the same teammate. The identical integration chain is available explicitly via `runtime task-integrate --assignment-id <id>` when the automatic SubagentStop payload cannot identify the assignment.
6. Builder completion is registered with `runtime task-complete` — one atomic command (message validation + evidence envelope derivation + Agent/TASK advance + evidence registration); the legacy `agent-event completion_reported` + `runtime evidence add` dual write still works but produces a thinner envelope. Before the Builder writes, create its worktree (`git worktree add .worktrees/<assignment-id> -b wt/<assignment-id> develop`) and record `worktree_path`/`branch`/`target_branch` on the manifest row — SubagentStop integration requires them.
7. In S7 (verification) the round is driven by runtime verbs, not by hand-pushed transitions: scaffold and register the ReviewPlan (`s7 draft`, inspect `coverage_inventory`/`e2e_assets`, then `runtime review-plan --file <plan.json>`), dispatch reviewers (`s7 manifest-draft`, `runtime register-workgroup`), consume each Assignment's Canonical ReviewResult (`runtime review-result submit --assignment-id <id> --result <result.json>`; required evidence refs are typed, and observation steps land via `capture step --finding <id> --claim <id>` / `--captures`), and revise a running plan once per round (`runtime review-plan revise`). `loop-harness s7 status` is the board: it prints the plan line, the round counter (current / `max_full_review_rounds`), the `round_entry` block (which TR re-entered the round and its seed/handoff/impact refs) plus the `seed_projection` line when a plan is registered from the S9 seed, the `subject_digest` every result must bind, claim dispositions, any blocked assignment's `blocker_ref` and the recovery verb `runtime agent-event --event blocker_resolved --agent-id <id> --message <file>`, and the single next action; a cold-start E2E round also has `loop-harness s7 workspace-digest` for the `verification_artifact_digest` the E2E result must bind. Re-entering S7 via TR-012 (post S9 repair) re-runs the same verbs from a generated seed: `.claude/review/repair/s7-seeds/review-plan-s9-round-<N>.json` is baseline-complete (it carries the changed-artifact `frozen_subjects`, the change-impact source_refs, and the current-generation TASK coverage) but not a finished plan — the Planner still refines Claims, Assignments and `non_overlap_boundary` per `blueprint/L3-S7-verification-round.md` and `blueprint/L4-runtime-control-plane.md`; refresh the frozen shas if the tree moved, refine the Claim set if needed, then `runtime review-plan --file <seed>`. the change-impact evidence is the source of truth: the registered plan must derive `frozen_subjects`, `coverage_inventory`, and a Claim source_ref from every `changed_artifacts` path/SHA it carries; QA reports additionally carry the §5 Targeted Re-verification table alongside §2–§4, and the round counter tells you which round you are on. A no-repair batch returns via TR-022 (`findings_resolved_without_repair`) instead of TR-012 and runs the same S7 verbs without a seed. A rejected command includes the missing facts, repair action, next command, verification command and protocol ref; fix those facts and resubmit the same artifact. The machine exits are automatic — a sealed ObservationBatch or a machine CleanRound commits TR-008/TR-009 on the next PreToolUse; do not invoke the transition CLI for them. If the PostToolUse auto-activation chain did not fire for a dispatched Worker, recover with `runtime agent-begin --agent-id <id> --plan <plan-report.json>`. Full verb walkthrough: `docs/agent-protocol.md#s7`.
Before registering an S7 draft, inspect its CASE-level E2E Assignments. `s7 draft` projects required browser CASEs from `docs/design/prototypes/<module>/cases.json`; a complete CASE→Playwright spec mapping produces `regression_available` and SHA-pinned `e2e_assets`, while any missing mapping produces `cold_start`, an `e2e-workspace/<round>` write surface, and one behavior Assignment per CASE. If no readable CASE inventory exists, the remaining `TODO(planner)` is intentional and registration explains the missing S2 input. Typed path evidence may use `path:<repo-relative>#sha256=<64-hex>` for drift detection; bare `path:` is compatibility-only existence evidence.
8. In S9 (bug_resolution), consume only the approved RepairContract: open the RepairSession, compile the RepairPlan, dispatch each Assignment with `runtime repair dispatch --assignment-id <assignment> --agent-id <agent>`, send the generic PLAN_REPORT and submit the S9 domain PlanReport, then wait for `runtime repair execution begin` before product writes. Use `runtime repair status` for each Assignment's owner/report/result, `queue_reason`, `lock_state` and next action; do not create a second scheduler or edit Runtime state by hand.
9. If `s7 status` reports `round N of M` with N >= M, finish draining the current round but do not open another one. Submit the human artifact through `runtime s7-budget-decision --file <decision.json> --actor <user>`; `increase_budget` atomically raises `max_full_review_rounds` and leaves the pending round-opening transition to retry, while `return_to_governance` records the decision, invalidates downstream review evidence, resets the review projection, and routes through GTR-006 to planning. The decision file is persisted as scoped `human_decision` evidence; Runtime revision is assigned internally by the Writer.
10. In S10 (acceptance_and_audit), treat the stage as a read-only audit, not a final shortcut: run `s10 status`, freeze the finite coverage inventory and responsibility matrix, record one counterevidence check per item, and validate the machine manifest before registering the human-readable ACC or release-audit envelope. The manifest must prove 100% requirement/contract/changed-path/audit-area coverage with zero UNKNOWN, unsupported PASS, unowned risk, untracked debt, or blocking finding. If any product or architecture defect appears, return through S8 → S9 → a fresh complete S7; never use S9 → S10 or edit product code in S10. Only a current clean package may reach S11.
11. Some authority transactions carry runtime-issued ids that are not TRs and therefore do not appear in the Contents index above: `S8-REPAIR-CONTRACT-APPROVAL` (runtime investigation contract approve — the S8→S9 authority; PTR-BUG-08 is its legacy-catalog alias), plus the entity/record CAS ids (REVIEW-RESULT, REVIEW-PLAN-STALE, S7-BUDGET-DECISION, AGENT-LIFECYCLE, BUG-LIFECYCLE, EVIDENCE-RECORD). They are driven by their runtime verbs, never by `runtime transition`.
12. Stop only at a human Gateway, an external asynchronous wait, or the end of the current turn.

The persisted `.claude/loop-state.json` `milestone` is a recovery cache, not a second state machine. `docs/loop-definition.json` and the Transition Engine remain the authority for legal lifecycle changes.

During BUG investigation, answer why E2E did not cover or fail the gap (`skills/bug-resolution/SKILL.md`; `loop-harness e2e-coverage`). A contracted behavior that broke without a red CT/AC requires a coverage-gap Closing Contract item.

---

## S11 Human decision gateway

`awaiting_human_release` is a non-terminal human gateway. The Controller has no automatic candidate or decision at this cursor. Submit exactly one finite disposition with the explicit Runtime command:

```bash
loop-harness runtime human-decision \
  --disposition <approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort> \
  --actor <user|orchestrator> \
  --decision-evidence <human-decision-reference>
```

Disposition mapping is fixed: `approve` → TR-025 `release_authorized`; `defer` → TR-026 `paused` (the command binds generated `pause_record=generated:pause_checkpoint`); `reject_defect` → TR-027 S8 investigation (also requires `--finding-evidence`); `reject_acceptance` → TR-028 acceptance; `reject_release_audit` → TR-029 release audit; `abort` → TR-030 `aborted`. Arbitrary target states and transition IDs are not accepted.

Human approval records release authorization only. Harness has no squash merge, publication, deployment, or formal release permission. Runtime rollover is eligible only from `release_authorized` or `aborted`.

## Contents

- [`TR-001`](#tr-001) inactive → planning — Start exactly one Loop for the named locked REQ.
- [`TR-002`](#tr-002) planning → document_verification — Planning completes when contracts are locked and the TASK batch is fully complete on disk (planning_complete), with clause coverage, DAG acyclicity, and closing contracts verified (tasks_checked).
- [`TR-003`](#tr-003) document_verification → building — Lock only the exact contract and task versions jointly verified.
- [`TR-004`](#tr-004) document_verification → planning — Non-REQ document findings return to planning.
- [`TR-005`](#tr-005) document_verification → paused — REQ changes always return control to the human.
- [`TR-006`](#tr-006) building → verification — Start a complete review round after every TASK in the TR-003 exact execution batch has a Builder Result with passing checks, no unapproved scope deviations, and a verified integration checkpoint.
- [`TR-007`](#tr-007) building → planning — Non-REQ execution conflicts return through planning and document verification.
- [`TR-008`](#tr-008) verification → bug_resolution — A sealed ObservationBatch carries the exact immutable Finding set (with encounters, wall actions and evidence refs) into the S8 Runtime cursor.
- [`TR-009`](#tr-009) verification → acceptance — Acceptance requires the machine CleanRound: every required Claim of the current ReviewPlan has a consumed pass Result, no current-round Finding exists, and the clean-round snapshot is recomputed by the clean_round_valid guard at promotion time.
- [`TR-010`](#tr-010) verification → paused — A ReviewResult verdict of req_change_required pauses the loop for a human REQ decision.
- [`TR-011`](#tr-011) verification → paused — A ReviewResult verdict of release_blocked pauses the loop for a human release decision.
- [`TR-012`](#tr-012) bug_resolution → verification — Only the ready_for_full_review handoff checkpoint for an approved RepairContract may enter a complete Delivery + QA + E2E Browser round; targeted re-verification never substitutes for that round.
- [`TR-013`](#tr-013) bug_resolution → planning — Repair-driven specification changes return through planning and document verification.
- [`TR-014`](#tr-014) bug_resolution → paused — Repair work cannot modify the locked REQ.
- [`TR-015`](#tr-015) acceptance → release_audit — Release audit starts only from current ACC and clean-round evidence.
- [`TR-016`](#tr-016) acceptance → verification — Acceptance discrepancies restart the complete review.
- [`TR-017`](#tr-017) release_audit → awaiting_human_release — Approved or approved-with-risk audit reaches the human release boundary.
- [`TR-018`](#tr-018) release_audit → paused — A blocked release audit pauses the Loop.
- [`TR-019`](#tr-019) paused → RESUME_FROM_PAUSE — Resume the exact validated state, phase and entity checkpoint.
- [`TR-020`](#tr-020) paused → planning — A changed locked REQ starts a new planning generation.
- [`TR-021`](#tr-021) paused → aborted — Only a human may permanently abort the Loop.
- [`TR-022`](#tr-022) bug_resolution → verification — When the InvestigationCase has dispositioned every source Finding as no-change, duplicate, or another non-repair route, and no approved RepairContract remains, the Loop returns to verification for a fresh complete round.
- [`TR-023`](#tr-023) bug_resolution → planning — Finding-level specification rework (S8) routes back to planning.
- [`TR-024`](#tr-024) bug_resolution → paused — A finding that requires modifying the locked REQ cannot proceed autonomously; the Loop pauses for human amendment.
- [`TR-025`](#tr-025) awaiting_human_release → release_authorized — Record human release authorization without performing merge, publication, deployment, or formal release.
- [`TR-026`](#tr-026) awaiting_human_release → paused — Defer the release decision and capture the S11 checkpoint before entering paused.
- [`TR-027`](#tr-027) awaiting_human_release → bug_resolution — Route a human defect rejection to bug-resolution investigation with its finding evidence.
- [`TR-028`](#tr-028) awaiting_human_release → acceptance — Re-enter acceptance and invalidate only prior acceptance and release-audit evidence.
- [`TR-029`](#tr-029) awaiting_human_release → release_audit — Re-enter release audit and invalidate only prior release-audit evidence.
- [`TR-030`](#tr-030) awaiting_human_release → aborted — Record a human release abort without performing any release side effect.

_Phase: bug_resolution_

- [`PTR-BUG-09`](#ptr-bug-09) repair_readback → planning — S9 enters planning with a dispatchable Assignment set; no implementation write is authorized yet.
- [`PTR-BUG-10`](#ptr-bug-10) planning → reproducing — Builders report their intended repair and failing pre-fix signal before execution.
- [`PTR-BUG-11`](#ptr-bug-11) reproducing → fixing — Only the explicit execution checkpoint releases implementation writes.
- [`PTR-BUG-05`](#ptr-bug-05) fixing → targeted_reverification — Every repair invalidates affected historical PASS evidence before recheck.
- [`PTR-BUG-06`](#ptr-bug-06) targeted_reverification → ready_for_full_review — A targeted pass never substitutes for the full review round; it records the S9-to-S7 handoff checkpoint.
- [`PTR-BUG-12`](#ptr-bug-12) investigation → targeted_reverification — After an environmental or authority blocker is resolved, reopen the existing targeted verification checkpoint; runtime hard-checks status=blocked, failure_route=blocked, and a non-empty resolution reason.
- [`PTR-BUG-07`](#ptr-bug-07) targeted_reverification → investigation — Failed repair verification returns to the same InvestigationCase for causal reassessment; it does not create a new BUG draft unless the causal model genuinely differs.

See [Legacy (PTR-BUG)](#legacy-ptr-bug) at the end of this Manual for the legacy compatibility transitions (PTR-BUG-01..04, PTR-BUG-08).

_Phase: planning_

- [`PTR-PLAN-01`](#ptr-plan-01) design → contracts — Advance formal planning from design to contracts after the design quality gate passes.
- [`PTR-PLAN-02`](#ptr-plan-02) contracts → tasks — Advance formal planning from contracts to tasks after the contract quality gate passes.

_Global_

- [`GTR-001`](#gtr-001) → paused — A user may pause from any active state.
- [`GTR-002`](#gtr-002) → paused — Any required locked REQ change pauses automation.
- [`GTR-003`](#gtr-003) → paused — Production-data, security, compliance or irreversible actions require human approval.
- [`GTR-004`](#gtr-004) → paused — Configured repair limits never silently close an InvestigationCase or RepairContract; exhaustion pauses the Loop with the current repair evidence.
- [`GTR-005`](#gtr-005) → paused — Runtime/document inconsistency fails closed.
- [`GTR-006`](#gtr-006) → planning — When the S7 full-review budget is exhausted, a human may return the Runtime to planning for specification or architecture governance instead of authorizing another review round.

---

## Transitions

### `TR-001` {#tr-001}

_inactive → planning_

Start exactly one Loop for the named locked REQ.

- `no_other_active_loop` [semantic_check] — No other runtime is currently bound to a different REQ; the Loop model allows one active REQ per project.

Evidence: `req_lock_record`, `loop_authorization_record`

Evidence bindings (copy into `runtime transition`):

- `req_lock_record`: `--evidence req_lock_record=<reference>`
  Accepted kinds: `human_decision`
- `loop_authorization_record`: `--evidence loop_authorization_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-001` to inspect current candidates.

### `TR-002` {#tr-002}

_planning → document_verification_

Planning completes when contracts are locked and the TASK batch is fully complete on disk (planning_complete), with clause coverage, DAG acyclicity, and closing contracts verified (tasks_checked). Gate readiness facts: a TASK declaring `Status: complete` on disk (or a registered complete task) plus valid planning_task evidence — the missing token `document:task:complete` means neither was found. On commit, locked contracts and complete tasks are registered into documents[].

- `planning_complete` [semantic_check] — At least one current-baseline contract document has status=locked with a matching on-disk markdown Status field (contracts are registered by PTR-PLAN-02), AND every docs/tasks/TASK-*.md declares status complete or cancelled with at least one complete (the batch is registered by TR-002's own register_planning_tasks action). Fingerprints are owned by registration and reachability, not re-checked here.
- `tasks_checked` [semantic_check] — S4's mechanical close (semantic.TasksCheck) runs at TR-002: the TASK batch is fully complete (cancelled tasks excluded), every task has an existing primary contract and a Closing Contract block, clause coverage between the CONTRACTS index universe and TASK §3 declarations closes in both directions, and the §8 dependency graph is acyclic (cycle path reported).

### `TR-003` {#tr-003}

_document_verification → building_

Lock only the exact contract and task versions jointly verified.

- `joint_document_pass` [evidence_attestation] — _no spec_
- `verified_versions_current` [evidence_attestation] — Every current-generation registered document still matches its on-disk sha, so the lock cannot advance on drifted inputs. The real check runs in GATE-DOCUMENT-PASS's registered-document drift screen (a `document_drift:<path>` conflict blocks the gate); the guard body itself only rejects an empty evidence map.

Evidence: `document_review_record`

Evidence bindings (copy into `runtime transition`):

- `document_review_record`: `--evidence document_review_record=<reference>`
  Accepted kinds: `document_review`

If a binding is missing, retry with the command above; run `loop-harness explain TR-003` to inspect current candidates.

### `TR-004` {#tr-004}

_document_verification → planning_

Non-REQ document findings return to planning.design for reworking the failing artifacts.

- `req_baseline_unchanged` [semantic_check] — The locked REQ's sha256 recorded in runtime.bound_req still matches the file at runtime.bound_req.path, so the rework loop cannot silently advance on a changed REQ.

Evidence: `document_review_record`

Evidence bindings (copy into `runtime transition`):

- `document_review_record`: `--evidence document_review_record=<reference>`
  Accepted kinds: `document_review`

If a binding is missing, retry with the command above; run `loop-harness explain TR-004` to inspect current candidates.

### `TR-005` {#tr-005}

_document_verification → paused_

REQ changes always return control to the human.

_No guards._

Evidence: `document_review_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `document_review_record`: `--evidence document_review_record=<reference>`
  Accepted kinds: `document_review`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-005` to inspect current candidates.

### `TR-006` {#tr-006}

_building → verification_

Start a complete review round after every TASK in the TR-003 exact execution batch has a Builder Result with passing checks, no unapproved scope deviations, and a verified integration checkpoint. S7 verification planning starts from the real integrated diff at its own entry; the building stage no longer demands S7 team-manifest evidence it cannot legitimately produce.

_No guards._

Evidence: `builder_report_record`

Evidence bindings (copy into `runtime transition`):

- `builder_report_record`: `--evidence builder_report_record=<reference>`
  Accepted kinds: `builder_report`, `agent_completion`

If a binding is missing, retry with the command above; run `loop-harness explain TR-006` to inspect current candidates.

### `TR-007` {#tr-007}

_building → planning_

Non-REQ execution conflicts return through planning and document verification.

- `req_baseline_unchanged` [semantic_check] — The locked REQ's sha256 recorded in runtime.bound_req still matches the file at runtime.bound_req.path, so the rework loop cannot silently advance on a changed REQ.

Evidence: `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain TR-007` to inspect current candidates.

### `TR-008` {#tr-008}

_verification → bug_resolution_

A sealed ObservationBatch carries the exact immutable Finding set (with encounters, wall actions and evidence refs) into the S8 Runtime cursor. TR-008 records the cursor handoff; the next mandatory S8 verb is `runtime investigation ingest`, which creates the InvestigationCase after exact-set/hash/baseline validation. S8 consumes the facts and does not re-reproduce symptoms by default. No per-Finding BUG draft is created; any legacy BUG draft is only a compatibility projection of the Case.

- `observation_batch_sealed` [semantic_check] — _no spec_

Evidence: `observation_batch_record`

Evidence bindings (copy into `runtime transition`):

- `observation_batch_record`: `--evidence observation_batch_record=<reference>`
  Accepted kinds: `observation_batch`

If a binding is missing, retry with the command above; run `loop-harness explain TR-008` to inspect current candidates.

### `TR-009` {#tr-009}

_verification → acceptance_

Acceptance requires the machine CleanRound: every required Claim of the current ReviewPlan has a consumed pass Result, no current-round Finding exists, and the clean-round snapshot is recomputed by the clean_round_valid guard at promotion time.

- `clean_round_valid` [semantic_check] — _no spec_

Evidence: `clean_round_record`

Evidence bindings (copy into `runtime transition`):

- `clean_round_record`: `--evidence clean_round_record=<reference>`
  Accepted kinds: `clean_round`

If a binding is missing, retry with the command above; run `loop-harness explain TR-009` to inspect current candidates.

S10 acceptance machine artifact (required before the Controller can consume the acceptance evidence):

```text
loop-harness s10 manifest validate --root <root> \
  --file <acceptance-manifest.json> --type acceptance
loop-harness runtime evidence add --root <root> \
  --id <id> --kind acceptance \
  --path <envelope.json> --produced-by <agent> --responsibility <role>
```

The envelope must contain `audit_manifest_path` and `audit_manifest_sha256`. The manifest must have a frozen `coverage_inventory` with explicit `requirement`, `contract`, and `changed_path` rows (use evidence-backed `not_applicable`, never an omitted hard category), exactly one counterevidence row per item, and zero UNKNOWN/unsupported PASS/unowned risk/untracked debt/blocking finding metrics. Start from the copyable shape `docs/examples/s10/acceptance-manifest.json` when useful. Run `loop-harness s10 status --root <root>` to see the current round and the next recovery action. Do not call `runtime transition` manually.

### `TR-010` {#tr-010}

_verification → paused_

A ReviewResult verdict of req_change_required pauses the loop for a human REQ decision. The verdict transaction (runtime review-result submit) already created the single authoritative pause checkpoint; this transition only moves the cursor.

- `pause_checkpoint_recorded` [semantic_check] — _no spec_

Evidence: `review_result_record`

Evidence bindings (copy into `runtime transition`):

- `review_result_record`: `--evidence review_result_record=<reference>`
  Accepted kinds: `review_result`, `delivery_review`, `qa_review`, `e2e_review`

If a binding is missing, retry with the command above; run `loop-harness explain TR-010` to inspect current candidates.

### `TR-011` {#tr-011}

_verification → paused_

A ReviewResult verdict of release_blocked pauses the loop for a human release decision. The verdict transaction (runtime review-result submit) already created the single authoritative pause checkpoint; this transition only moves the cursor.

- `pause_checkpoint_recorded` [semantic_check] — _no spec_

Evidence: `review_result_record`

Evidence bindings (copy into `runtime transition`):

- `review_result_record`: `--evidence review_result_record=<reference>`
  Accepted kinds: `review_result`, `delivery_review`, `qa_review`, `e2e_review`

If a binding is missing, retry with the command above; run `loop-harness explain TR-011` to inspect current candidates.

### `TR-012` {#tr-012}

_bug_resolution → verification_

Only the ready_for_full_review handoff checkpoint for an approved RepairContract may enter a complete Delivery + QA + E2E Browser round; targeted re-verification never substitutes for that round.

- `bug_phase_ready_for_full_review` [evidence_attestation] — Every BUG from the round has reached `bug_phase_ready_for_full_review`, i.e. targeted re-verification is complete and the bug sub-machine is ready to fold back into the main verification review.
- `all_targeted_reverification_passed` [semantic_check] — Every P0 BUG in runtime.entities.bugs[] has advanced past `retesting`/`fixing`/`investigating` so no blocking bug remains awaiting targeted re-verification.

Evidence: `targeted_reverification_record`, `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `targeted_reverification_record`: `--evidence targeted_reverification_record=<reference>`
  Accepted kinds: `targeted_reverification`
- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain TR-012` to inspect current candidates.

### `TR-013` {#tr-013}

_bug_resolution → planning_

Repair-driven specification changes return through planning and document verification.

- `req_baseline_unchanged` [semantic_check] — The locked REQ's sha256 recorded in runtime.bound_req still matches the file at runtime.bound_req.path, so the rework loop cannot silently advance on a changed REQ.

Evidence: `change_impact_record`, `repair_record`

Evidence bindings (copy into `runtime transition`):

- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`
- `repair_record`: `--evidence repair_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain TR-013` to inspect current candidates.

### `TR-014` {#tr-014}

_bug_resolution → paused_

Repair work cannot modify the locked REQ.

_No guards._

Evidence: `repair_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `repair_record`: `--evidence repair_record=<reference>`
  Accepted kinds: `bug`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-014` to inspect current candidates.

### `TR-015` {#tr-015}

_acceptance → release_audit_

Release audit starts only from current ACC and clean-round evidence.

- `acc_complete` [semantic_check] — A valid acceptance evidence entry for the current baseline generation and review round exists in runtime.evidence[] and its registered sha256 still matches the on-disk acceptance record (RC-06 S10-14: invalidated, stale-round or drifted ACC envelopes are rejected, not attested).
- `clean_round_still_valid` [semantic_check] — verification.EvaluateCleanRound still reports a passing clean round at the current baseline generation, so neither baselines nor evidence drifted between round capture and release.

Evidence: `acceptance_record`, `clean_round_record`

Evidence bindings (copy into `runtime transition`):

- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`
- `clean_round_record`: `--evidence clean_round_record=<reference>`
  Accepted kinds: `clean_round`

If a binding is missing, retry with the command above; run `loop-harness explain TR-015` to inspect current candidates.

S10 acceptance machine artifact (required before the Controller can consume the acceptance evidence):

```text
loop-harness s10 manifest validate --root <root> \
  --file <acceptance-manifest.json> --type acceptance
loop-harness runtime evidence add --root <root> \
  --id <id> --kind acceptance \
  --path <envelope.json> --produced-by <agent> --responsibility <role>
```

The envelope must contain `audit_manifest_path` and `audit_manifest_sha256`. The manifest must have a frozen `coverage_inventory` with explicit `requirement`, `contract`, and `changed_path` rows (use evidence-backed `not_applicable`, never an omitted hard category), exactly one counterevidence row per item, and zero UNKNOWN/unsupported PASS/unowned risk/untracked debt/blocking finding metrics. Start from the copyable shape `docs/examples/s10/acceptance-manifest.json` when useful. Run `loop-harness s10 status --root <root>` to see the current round and the next recovery action. Do not call `runtime transition` manually.

### `TR-016` {#tr-016}

_acceptance → verification_

Acceptance discrepancies restart the complete review.

_No guards._

Evidence: `acceptance_record`, `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`
- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain TR-016` to inspect current candidates.

### `TR-017` {#tr-017}

_release_audit → awaiting_human_release_

Approved or approved-with-risk audit reaches the human release boundary.

- `release_audit_approved` [semantic_check] — A valid release_audit evidence entry for the current baseline generation and review round exists in runtime.evidence[] and its registered sha256 still matches the on-disk audit record (RC-06 S10-14: the guard resolves and re-hashes the artifact instead of attesting an evidence map).
- `acc_complete` [semantic_check] — A valid acceptance evidence entry for the current baseline generation and review round exists in runtime.evidence[] and its registered sha256 still matches the on-disk acceptance record (RC-06 S10-14: invalidated, stale-round or drifted ACC envelopes are rejected, not attested).
- `clean_round_still_valid` [semantic_check] — verification.EvaluateCleanRound still reports a passing clean round at the current baseline generation, so neither baselines nor evidence drifted between round capture and release.

Evidence: `release_audit_record`, `acceptance_record`, `clean_round_record`

Evidence bindings (copy into `runtime transition`):

- `release_audit_record`: `--evidence release_audit_record=<reference>`
  Accepted kinds: `release_audit`
- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`
- `clean_round_record`: `--evidence clean_round_record=<reference>`
  Accepted kinds: `clean_round`

If a binding is missing, retry with the command above; run `loop-harness explain TR-017` to inspect current candidates.

The release-audit evidence must point to a separately validated manifest:

```text
loop-harness s10 manifest validate --root <root> \
  --file <release-audit-manifest.json> --type release_audit
loop-harness runtime evidence add --root <root> \
  --id <id> --kind release_audit \
  --path <envelope.json> --produced-by <agent> --responsibility "Release Auditor"
```

The manifest must include all eight audit areas from `internal/schema/assets/s10-audit-manifest.schema.json`; use `docs/examples/s10/release-audit-manifest.json` as the copyable shape. Markdown audit prose does not replace this machine-checked ledger.

### `TR-018` {#tr-018}

_release_audit → paused_

A blocked release audit pauses the Loop.

_No guards._

Evidence: `release_audit_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `release_audit_record`: `--evidence release_audit_record=<reference>`
  Accepted kinds: `release_audit`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-018` to inspect current candidates.

The BLOCKED release-audit evidence must preserve its machine-readable blocker ledger:

```text
loop-harness s10 manifest validate --root <root> \
  --file <release-audit-manifest.json> --type release_audit --outcome blocked
loop-harness runtime evidence add --root <root> \
  --id <id> --kind release_audit \
  --path <envelope.json> --produced-by <agent> --responsibility "Release Auditor"
```

Keep the blocking finding, route, evidence references, and all eight audit areas in the manifest. Let the Controller take TR-018 to `paused`; do not call `runtime transition` manually.

### `TR-019` {#tr-019}

_paused → RESUME_FROM_PAUSE_

Resume the exact validated state, phase and entity checkpoint.

- `resume_checkpoint_valid` [semantic_check] — A pause checkpoint exists at runtime.pause with a non-empty `reason` and `required_human_action`, so the resume transition has a captured state to restore.
- `baselines_unchanged` [evidence_attestation] — Every document fingerprint captured at pause time matches the on-disk file, so the resume cannot quietly advance on drifted inputs. The re-hash runs in TR-019's restore_from_pause action (fail-closed, sentinel ErrBaselineDrift routes the CLI to amendment); the guard body itself only rejects an empty evidence map.

Evidence: `human_decision_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-019` to inspect current candidates.

### `TR-020` {#tr-020}

_paused → planning_

A changed locked REQ starts a new planning generation.

- `updated_req_locked` [evidence_attestation] — The updated REQ file referenced by the re-bind request declares status=locked with a strictly higher version than the currently bound REQ, and its sha256 matches the on-disk file.

Evidence: `human_decision_record`, `req_lock_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`
- `req_lock_record`: `--evidence req_lock_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-020` to inspect current candidates.

### `TR-021` {#tr-021}

_paused → aborted_

Only a human may permanently abort the Loop.

- `human_abort_approved` [evidence_attestation] — The transition's evidence validation enforces that the cited human_decision evidence is current and scoped to `runtime_abort:<runtime_id>` (human_decision_scope on TR-021/TR-030); the fixed transition and one-time evidence id define the approval boundary, while Runtime revision remains internal.

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-021` to inspect current candidates.

### `TR-022` {#tr-022}

_bug_resolution → verification_

When the InvestigationCase has dispositioned every source Finding as no-change, duplicate, or another non-repair route, and no approved RepairContract remains, the Loop returns to verification for a fresh complete round. The legacy bug_batch_record is only a compatibility projection; spec and REQ changes use TR-023 and TR-024.

- `no_accepted_bugs` [evidence_attestation] — No BUG in runtime.entities.bugs[] for the current review round is in state `accepted`, `assigned`, `fixing`, or `retesting`. Used by TR-022 to confirm a finding-level Loop exit to verification is safe (no accepted BUG requires the S9 repair flow).
- `bug_report_review_complete` [evidence_attestation] — Every blocking S7 finding has a recorded disposition in runtime.evidence[] (accepted canonical BUG, rejected BUG, duplicate link, spec rework handoff, or REQ change pause). Used by TR-022 to confirm the orchestrator has classified every blocking finding before exiting the bug_resolution phase.

Evidence: `bug_batch_record`

Evidence bindings (copy into `runtime transition`):

- `bug_batch_record`: `--evidence bug_batch_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain TR-022` to inspect current candidates.

### `TR-023` {#tr-023}

_bug_resolution → planning_

Finding-level specification rework (S8) routes back to planning.design for a new TR-002 cycle; complements TR-013 which handles repair-level spec change.

- `req_baseline_unchanged` [semantic_check] — The locked REQ's sha256 recorded in runtime.bound_req still matches the file at runtime.bound_req.path, so the rework loop cannot silently advance on a changed REQ.

Evidence: `bug_batch_record`, `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `bug_batch_record`: `--evidence bug_batch_record=<reference>`
  Accepted kinds: `bug`
- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain TR-023` to inspect current candidates.

### `TR-024` {#tr-024}

_bug_resolution → paused_

A finding that requires modifying the locked REQ cannot proceed autonomously; the Loop pauses for human amendment. Complements TR-014 which handles repair-level REQ change.

_No guards._

Evidence: `bug_batch_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `bug_batch_record`: `--evidence bug_batch_record=<reference>`
  Accepted kinds: `bug`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-024` to inspect current candidates.

### `TR-025` {#tr-025}

_awaiting_human_release → release_authorized_

Record human release authorization without performing merge, publication, deployment, or formal release.

_No guards._

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-025` to inspect current candidates.

### `TR-026` {#tr-026}

_awaiting_human_release → paused_

Defer the release decision and capture the S11 checkpoint before entering paused.

_No guards._

Evidence: `human_decision_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-026` to inspect current candidates.

### `TR-027` {#tr-027}

_awaiting_human_release → bug_resolution_

Route a human defect rejection to bug-resolution investigation with its finding evidence.

_No guards._

Evidence: `human_decision_record`, `finding_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`
- `finding_record`: `--evidence finding_record=<reference>`
  Accepted kinds: `finding`, `bug`

If a binding is missing, retry with the command above; run `loop-harness explain TR-027` to inspect current candidates.

### `TR-028` {#tr-028}

_awaiting_human_release → acceptance_

Re-enter acceptance and invalidate only prior acceptance and release-audit evidence.

_No guards._

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-028` to inspect current candidates.

### `TR-029` {#tr-029}

_awaiting_human_release → release_audit_

Re-enter release audit and invalidate only prior release-audit evidence.

_No guards._

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-029` to inspect current candidates.

### `TR-030` {#tr-030}

_awaiting_human_release → aborted_

Record a human release abort without performing any release side effect.

- `human_abort_approved` [evidence_attestation] — The transition's evidence validation enforces that the cited human_decision evidence is current and scoped to `runtime_abort:<runtime_id>` (human_decision_scope on TR-021/TR-030); the fixed transition and one-time evidence id define the approval boundary, while Runtime revision remains internal.

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain TR-030` to inspect current candidates.

## Phase transitions: bug_resolution

### `PTR-BUG-09` {#ptr-bug-09}

_repair_readback → planning_

S9 enters planning with a dispatchable Assignment set; no implementation write is authorized yet.

_No guards._

### `PTR-BUG-10` {#ptr-bug-10}

_planning → reproducing_

Builders report their intended repair and failing pre-fix signal before execution.

_No guards._

### `PTR-BUG-11` {#ptr-bug-11}

_reproducing → fixing_

Only the explicit execution checkpoint releases implementation writes.

_No guards._

### `PTR-BUG-05` {#ptr-bug-05}

_fixing → targeted_reverification_

Every repair invalidates affected historical PASS evidence before recheck.

- `repair_reports_complete` [evidence_attestation] — Every repair task spawned for BUGs in the round has a completion_report evidence item referenced from runtime.evidence[].

Evidence: `repair_record`, `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `repair_record`: `--evidence repair_record=<reference>`
  Accepted kinds: `bug`
- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-05` to inspect current candidates.

### `PTR-BUG-06` {#ptr-bug-06}

_targeted_reverification → ready_for_full_review_

A targeted pass never substitutes for the full review round; it records the S9-to-S7 handoff checkpoint.

- `original_finder_reverification_complete` [evidence_attestation] — An original-finder re-verification evidence item is referenced from runtime.evidence[] confirming the agent that raised the BUG has re-tested the fix.

Evidence: `targeted_reverification_record`

Evidence bindings (copy into `runtime transition`):

- `targeted_reverification_record`: `--evidence targeted_reverification_record=<reference>`
  Accepted kinds: `targeted_reverification`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-06` to inspect current candidates.

### `PTR-BUG-12` {#ptr-bug-12}

_investigation → targeted_reverification_

After an environmental or authority blocker is resolved, reopen the existing targeted verification checkpoint; runtime hard-checks status=blocked, failure_route=blocked, and a non-empty resolution reason. A new independent TargetedReverification is still required, and this transition never authorizes a repair or a pass.

_No guards._

### `PTR-BUG-07` {#ptr-bug-07}

_targeted_reverification → investigation_

Failed repair verification returns to the same InvestigationCase for causal reassessment; it does not create a new BUG draft unless the causal model genuinely differs.

_No guards._

Evidence: `targeted_reverification_record`

Evidence bindings (copy into `runtime transition`):

- `targeted_reverification_record`: `--evidence targeted_reverification_record=<reference>`
  Accepted kinds: `targeted_reverification`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-07` to inspect current candidates.

## Phase transitions: planning

### `PTR-PLAN-01` {#ptr-plan-01}

_design → contracts_

Advance formal planning from design to contracts after the design quality gate passes. Gate readiness facts: an ARCHITECTURE-*.md whose top status line (状态/Status) declares `locked` plus a valid planning_design JSON envelope (responsibility Architect, conclusion pass — a markdown file as --path yields `evidence:<id>:schema`). Missing tokens `document:design:locked` / `evidence:planning_design_record` mean one of these is absent.

- `ui_impact_resolved` [semantic_check] — runtime.bound_req.metadata.ui_impact is not `unknown`, so the planning phase is not paused on the SM-003 gate waiting for §11 of the REQ to clarify UI impact.

### `PTR-PLAN-02` {#ptr-plan-02}

_contracts → tasks_

Advance formal planning from contracts to tasks after the contract quality gate passes. Gate readiness facts: a contract declaring `Status: locked` on disk (or an already-registered locked contract in runtime documents[]) plus valid planning_contract evidence — the missing token `document:contract:locked` means neither was found; flip the contract's top status line（状态/Status）to locked.

- `contracts_checked` [semantic_check] — S3's mechanical close (semantic.ContractsCheck) runs at PTR-PLAN-02: contract token references resolve against REQ FR tables and module packages, clause cells point at known contracts, and fingerprint columns match disk.
- `scenario_bridge_checked` [semantic_check] — S2's AC↔CASE bridge (scenario.GuardBridgeChecked) runs at PTR-PLAN-02: every AC of the bound REQ reaches a rule via FR source_refs (with branches), or carries an endorsed N/A (NFR id / §A4). With no module packages at all, only fully N/A-endorsed REQs pass — an AC pointing at FR- with nothing citing it is a broken denominator.

## Global transitions

### `GTR-001` {#gtr-001}

_planning|document_verification|building|verification|bug_resolution|acceptance|release_audit → paused_

A user may pause from any active state.

_No guards._

Evidence: `human_decision_record`, `pause_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`
- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-001` to inspect current candidates.

### `GTR-002` {#gtr-002}

_planning|document_verification|building|verification|bug_resolution|acceptance|release_audit → paused_

Any required locked REQ change pauses automation.

_No guards._

Evidence: `pause_record`

Evidence bindings (copy into `runtime transition`):

- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-002` to inspect current candidates.

### `GTR-003` {#gtr-003}

_planning|document_verification|building|verification|bug_resolution|acceptance|release_audit → paused_

Production-data, security, compliance or irreversible actions require human approval.

_No guards._

Evidence: `pause_record`

Evidence bindings (copy into `runtime transition`):

- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-003` to inspect current candidates.

### `GTR-004` {#gtr-004}

_verification|bug_resolution → paused_

Configured repair limits never silently close an InvestigationCase or RepairContract; exhaustion pauses the Loop with the current repair evidence.

_No guards._

Evidence: `pause_record`, `bug_batch_record`

Evidence bindings (copy into `runtime transition`):

- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`
- `bug_batch_record`: `--evidence bug_batch_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-004` to inspect current candidates.

### `GTR-005` {#gtr-005}

_planning|document_verification|building|verification|bug_resolution|acceptance|release_audit → paused_

Runtime/document inconsistency fails closed.

_No guards._

Evidence: `pause_record`

Evidence bindings (copy into `runtime transition`):

- `pause_record`: `--evidence pause_record=generated:pause_checkpoint` (generated pause checkpoint)
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-005` to inspect current candidates.

### `GTR-006` {#gtr-006}

_bug_resolution|acceptance → planning_

When the S7 full-review budget is exhausted, a human may return the Runtime to planning for specification or architecture governance instead of authorizing another review round.

_No guards._

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-006` to inspect current candidates.


## Legacy (PTR-BUG)

_These four transitions are legacy compatibility paths only. They exist so
pre-S9 journals and synthetic fixtures still replay; new code must use the
S8 InvestigationCase / S9 repair machinery above instead. See also the
authority-transaction note in the Contents preamble (S8-REPAIR-CONTRACT-APPROVAL
is the real S8→S9 authority; PTR-BUG-08 is its legacy-catalog alias)._ 

### `PTR-BUG-01` {#ptr-bug-01}

_investigation → bug_report_review_

Legacy compatibility transition only. If an older implementation emits bug_drafts_ready, the resulting BUG records are projections that must be reconciled into an InvestigationCase; new paths keep the Case status in its artifact and do not create a second runtime phase machine.

- `root_cause_evidence_complete` [evidence_attestation] — A root-cause evidence item (failure mode, triggering input, and minimal-repro path) is referenced from runtime.evidence[] for the BUG promoted out of `investigating`.

Evidence: `observation_batch_record`, `root_cause_record`

Evidence bindings (copy into `runtime transition`):

- `observation_batch_record`: `--evidence observation_batch_record=<reference>`
  Accepted kinds: `observation_batch`
- `root_cause_record`: `--evidence root_cause_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-01` to inspect current candidates.

### `PTR-BUG-02` {#ptr-bug-02}

_bug_report_review → repair_readback_

Legacy compatibility projection only. A canonical BUG acceptance event cannot authorize S9 unless an approved RepairContract is already present; new paths do not enter bug_report_review.

- `canonical_bug_mapping_complete` [evidence_attestation] — Each finding evidence item in the round maps to a canonical BUG id in runtime.entities.bugs[] so the verification phase cannot advance on unresolved duplicates.
- `bug_closing_contracts_complete` [evidence_attestation] — Every BUG raised in the current review round has its closing-contract evidence referenced from runtime.evidence[] before the verification phase can advance.

Evidence: `bug_batch_record`

Evidence bindings (copy into `runtime transition`):

- `bug_batch_record`: `--evidence bug_batch_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-02` to inspect current candidates.

### `PTR-BUG-03` {#ptr-bug-03}

_bug_report_review → investigation_

Legacy compatibility projection only. Rejected BUG projections return to the InvestigationCase rather than creating another per-Finding BUG draft.

_No guards._

Evidence: `bug_batch_record`

Evidence bindings (copy into `runtime transition`):

- `bug_batch_record`: `--evidence bug_batch_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-03` to inspect current candidates.

### `PTR-BUG-08` {#ptr-bug-08}

_investigation → repair_readback_

Legacy compatibility projection only. The current S8 main path is `runtime investigation contract approve`, which writes immutable approved Case/Contract revisions, pins their hashes, and enters S9 repair_readback directly; it does not invoke this PTR-BUG-08 catalog transition. No canonical BUG acceptance is required or sufficient.

- `root_cause_evidence_complete` [evidence_attestation] — A root-cause evidence item (failure mode, triggering input, and minimal-repro path) is referenced from runtime.evidence[] for the BUG promoted out of `investigating`.
- `bug_closing_contract_complete` [evidence_attestation] — The single-BUG closing contract (root-cause, repair plan, and reverification plan) described in docs/rules/bugfix-review.md is referenced by evidence for the BUG leaving `investigating`.
- `bug_closing_contracts_complete` [evidence_attestation] — Every BUG raised in the current review round has its closing-contract evidence referenced from runtime.evidence[] before the verification phase can advance.

Evidence: `root_cause_record`, `repair_record`

Evidence bindings (copy into `runtime transition`):

- `root_cause_record`: `--evidence root_cause_record=<reference>`
  Accepted kinds: `bug`
- `repair_record`: `--evidence repair_record=<reference>`
  Accepted kinds: `bug`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-08` to inspect current candidates.

### `PTR-BUG-04` {#ptr-bug-04}

_repair_readback → fixing_

Legacy compatibility projection only. Current S9 dispatch uses `runtime repair dispatch` for each RepairAssignment, then the explicit `runtime repair execution begin` checkpoint releases implementation writes.

- `repair_understanding_approved` [evidence_attestation] — An understanding-approval evidence item is referenced from runtime.evidence[] confirming the assigned Builder understood the root-cause writeup before activation.
- `repair_activation_recorded` [evidence_attestation] — A repair-activation evidence item is referenced from runtime.evidence[] confirming the assigned Builder has started the repair task recorded in runtime.entities.tasks[].

Evidence: `activation_record`

Evidence bindings (copy into `runtime transition`):

- `activation_record`: `--evidence activation_record=<reference>`
  Accepted kinds: `agent_activation`

If a binding is missing, retry with the command above; run `loop-harness explain PTR-BUG-04` to inspect current candidates.

