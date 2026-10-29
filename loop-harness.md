# loop-harness — Transition Checklist

> What `loop-harness` checks at every transition. Read the
> relevant section before calling `forward`; verify each bullet
> before requesting the harness to advance.

- **Path**: `loop-harness.md`
- **Harness version**: dev
- **Loop definition SHA-256**: `1476e8beb8766d26564829587dbb8a8e4aac88e013cb33d945df63f9eb223bdf`

---

## Controller recovery protocol

Hooks observe current responsibilities and connect natural delivery boundaries. Native Start gives bounded context; native Stop invokes the original professional consumer. Main saves its own questionnaires through their existing consumers. Ordinary Worker tools do not receive answers, activate work or run every stage gate. Milestone is an existing recovery cache, not proof that each event committed or context was delivered.

### Bootstrap binding boundary

S6 owners can query `runtime task-receipt --agent-id <owner> --assignment-id <id>`; registered Workers use the exact builder_receipt_command returned by prepare. The read-only receipt distinguishes current qualified delivery, own pending checks and Main integration; checks_source=original_report does not claim new check execution. Lost registered evidence and changed TASK input go to Main recovery/rework. The receipt neither advances TR-006 nor replaces S7 or S9 RepairResult completion.

`revision` has no global maximum. A Hook/controller checkpoint may advance the inactive bootstrap runtime before `TR-001`; binding lets the Writer read and commit the current Runtime under lock, archives the complete inactive state/journal pair, and installs a new `loop-REQ-*` runtime at revision `0` with an empty active journal. The `binding_receipt` carries `event=req_bound`, the approved REQ, and source runtime hashes. Do not edit revision by hand or reuse a pre-bind runtime snapshot after binding; the runtime identity changes and stale identities are rejected.

1. Read the `Next` action and current `Stage` from the Hook packet, then follow its `Read in order` list.
2. Read the linked `docs/control/agent-protocol.md#sN` section before acting.
3. If blocked or the Runtime is unclear, read this Manual. Use `runtime reconcile` only for a provable journal/state integrity gap, not an ordinary CAS conflict; status is optional diagnostics, never a per-tool prerequisite. When the live Quality Gate checklist is unclear, run `loop-harness ready` (diagnostics; never hand-push a Transition from it). `doctor` is structural schema/manual/policy_ref only — not stage readiness or runtime health; use `loop-harness health` for cumulative runtime signals. For the complete S7→S8→S9→S7→S10 action sequence and compatibility notes, run `loop-harness actions`.
4. Execute the one missing deliverable/evidence named by Hook/`ready` `missing[]`; do not invent a parallel lifecycle.
5. Main reads the native return and original receipt. S6 uses the supplied `runtime task-integrate --assignment-id <id> --root <main-root>`; S9 uses its supplied `runtime workspace integrate` command and necessary Main checks. Do not use one command for both contracts or re-wake a completed Worker for Main integration. No develop fallback or automatic Main checkout.
6. In new native S6, Main runs `s6 prepare --root <authority-root> --task <TASK> --role <Builder-role>` and passes its exact brief into Claude Code native Agent with worktree isolation (`worktree.baseRef: head`). No preselected platform IDs, Team, workspace prepare/launch, generic begin or activation. Builder saves its professional plan before implementation, continues with real TASK self-checks, commits products, saves Result and returns its exact brief and absolute Result reference. SubagentStart supplies short engineering context; SubagentStop invokes the original Result consumer. Ordinary save hooks do not receive native S6 delivery. Main explicitly runs `runtime task-integrate --assignment-id <id>` for the actual merge, required Main checks, ACK and cleanup. Rework uses `s6 prepare --revise-assignment <id> --revision-intent-key <stable-key>` through the original revision Writer. The exact TR-003 batch and TR-006 gate remain. Frozen s6-v1.2/workspace assignments keep their original recovery commands; never launch the same responsibility twice. See `docs/examples/s6-builder/README.md` and `docs/control/agent-protocol.md#s6`.
7. In S7, Main fills and saves the exact Hook-supplied ReviewPlan path; the original RegisterPlan consumer validates and registers it. Do not run `s7 draft` or `runtime review-plan` to submit a duplicate plan in the normal path. Main still dispatches the registered Assignments through `s7 manifest-draft` and `runtime register-workgroup`. Main actually dispatches native Agents with exact references. Each Reviewer saves and returns its exact answer for SubagentStop; the original consumer binds Claim/source facts and registers Findings or the round result. Do not run `runtime review-result submit` or copy subject digests and other machine metadata on the normal path. Use typed evidence and the real capture flow when the assigned questionnaire asks for them; the consumer binds its source and digest. `loop-harness s7 status` is a read-only board for plan, round, seed, claim dispositions, blockers, and next action. In TR-012 re-entry, the generated `.claude/review/repair/s7-seeds/review-plan-s9-round-<N>.json` supplies the changed-artifact baseline and TASK coverage; use the exact current ReviewPlan path the Hook prepares from that seed. A substantive change to an already registered plan follows the original controlled revision. A no-repair batch returns via TR-022 and runs the same S7 review. The Hook advances a sealed ObservationBatch; the final accepted ReviewResult continues the original CleanRound Gate/TR-009 and S10 material preparation in the same completion event. Recover a named pending clean exit with `runtime review-continue`; restore missing current S10 drafts with `runtime audit-prepare`. If the Hook explicitly reports that a lifecycle follow-up did not run, follow that named recovery action. Full role and dispatch contract: `docs/control/agent-protocol.md#s7`.
The supplied ReviewPlan includes its `coverage_inventory` and `e2e_assets`; inspect their current content and answer the professional questions without recreating machine fields. For a live timeline capture, use `loop-harness capture step --finding <id> --claim <id>` only when the assigned answer requires it; the original save consumer binds the buffer. If an explicitly named legacy/recovery command rejects an artifact, follow its missing facts, repair, next, verify, and reference details rather than inventing a command.
S7 questionnaire preparation uses the module's CASE inventory and Playwright mapping to propose the E2E Assignments. A complete CASE→spec mapping can use `regression_available`; missing mappings use `cold_start`, an `e2e-workspace/<round>` write surface, and one behavior Assignment per CASE. If CASE inventory is missing, the Hook names that input gap for Main; do not run `s7 draft` to reconstruct a second plan. In the ReviewPlan answer, Main resolves any professional coverage decision shown by the exact questionnaire. Evidence paths can be supplied as `path:<repo-relative>`; the original consumer binds current source and hashes.
8. Main follows the approved RepairContract and records actual professional division. Carry each exact prepared brief into native Agent with worktree isolation. Each Builder saves its private PlanReport before implementation, continues repair/self-checks, then saves and returns its private Result with the exact brief and absolute Result reference. SubagentStop imports professional Plan/Result through the original consumers; there is no separate plan acceptance wait. Main integrates the candidate with necessary checks before Final, then saves ChangeImpact and actually dispatches independent Targeted verification per approved Contract. The verifier returns its exact answer for Stop reception, without a Builder PlanReport or product writes. First incomplete native returns give Main the original rejection and exact gap without claiming completion. Once necessary work is complete, its original outlet records Handoff and the fresh S7 seed; no extra Handoff report, next-Hook wait or duplicate delivery. If Targeted was accepted but Handoff or S7 preparation failed, follow the supplied `runtime repair handoff complete --root <Main-root>` recovery to finish only the pending exit. The completed outlet supplies the original S7 ReviewPlan questionnaire and guide; Main completes and saves that questionnaire. Missing paths or preparation belong to Main/system. Existing frozen executions retain their named recovery contract. See `docs/control/agent-protocol.md#s9`.
9. If `s7 status` reports `round N of M` with N >= M, finish draining the current round but do not open another one. The actual completion outlet supplies the original budget application. Record only the actual human choice, increased maximum when applicable and reason, then save that exact application. An empty choice grants nothing. `increase_budget` raises `max_full_review_rounds` and resumes the pending successor; `return_to_governance` follows GTR-006 to planning. The original Writer supplies decision metadata and scoped human_decision evidence. `runtime s7-budget-decision --file <decision.json> --actor <user>` remains an explicit legacy/recovery interface, not another normal submission.
10. Main fills the actual acceptance or release-audit Manifest at the supplied exact path and saves once. Explain requirement dispositions, applicable observations, counterevidence, unknowns and actual risks. Eight audit themes are relevance prompts, not a row quota or mandatory new work. The original consumer derives its envelope and metrics; do not run a second validate/render/evidence submission. Unknowns and blockers remain unfinished, while explicit outcomes may be recorded without authorizing advance. S10 does not repair product code; changed products return through investigation/repair and fresh complete S7. Actual human choice remains in S11.
11. Some authority transactions carry runtime-issued ids that are not TRs and therefore do not appear in the Contents index above: `S8-REPAIR-CONTRACT-APPROVAL` (runtime investigation contract approve — the S8→S9 authority; PTR-BUG-08 is its legacy-catalog alias), plus the entity/record CAS ids (REVIEW-RESULT, REVIEW-PLAN-STALE, S7-BUDGET-DECISION, AGENT-LIFECYCLE, BUG-LIFECYCLE, EVIDENCE-RECORD). They are driven by their runtime verbs, never by `runtime transition`.
12. A native Worker returns its actual artifacts or honest blocker and ends its own execution. Main continues its remaining duties; an unknown human decision or external wait is explicit, not a reason to invent more Worker work.

The persisted `.claude/loop-state.json` `milestone` is a recovery cache, not a second state machine. `docs/control/loop-definition.json` and the Transition Engine remain the authority for legal lifecycle changes.

During BUG investigation, answer why E2E did not cover or fail the gap (`skills/bug-resolution/SKILL.md`; `loop-harness e2e-coverage`). A contracted behavior that broke without a red CT/AC requires a coverage-gap Closing Contract item.

---

## Approved requirement amendment {#approved-requirement-amendment}

Reuse the actual conversation decision approving the specific correction; do not ask for the same approval again or require the human to type a command verbatim. Record the actual decision attribution with --approved-by, not Git identity. An undecided amendment still waits for the human decision.

If the bound Runtime is not already paused, record its checkpoint with the existing command (skip this step when already paused):

```sh
loop-harness runtime pause --root <project-root> --approved-by <actual-human> --reason <approved-correction>
```

Apply only the approved correction to the same REQ ID, keep its top status locked, raise its semantic version above the bound version, and commit the amended REQ on the bound development branch (for example develop). Then use the existing domain receiver:

```sh
loop-harness req amend --root <project-root> --req docs/requirements/REQ-<id>.md --approved-by <actual-human>
```

Read the exact current S2 questionnaire view/answer paths returned by the original amendment outlet and continue its professional work. The original transaction records the decision, new generation and downstream invalidation; do not assemble evidence bindings, hand-run TR-020, resume the old checkpoint after amendment, or guess a generation directory. If the transaction is recorded but questionnaire preparation fails, retain that fact and follow its actual preparation error and original listener continuation; do not resubmit an already accepted amendment or invent a path.

## S11 Human decision gateway

`awaiting_human_release` is a non-terminal human gateway. The Controller has no automatic candidate or decision at this cursor. Present the actual materials; after a real explicit human choice, Main fills disposition and optional reason in the exact current application and saves normally. Empty disposition remains waiting. The explicit Runtime command below is compatibility/recovery only:

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
- [`TR-002`](#tr-002) planning → document_verification — In questionnaire mode, the current S4 professional answer must be received by the original consumer.
- [`TR-003`](#tr-003) document_verification → building — Lock only the exact contract and task versions jointly verified.
- [`TR-004`](#tr-004) document_verification → planning — Non-REQ document findings return to planning.
- [`TR-005`](#tr-005) document_verification → paused — REQ changes always return control to the human.
- [`TR-006`](#tr-006) building → verification — Start a complete review round after every TASK in the TR-003 exact execution batch has a Builder Result with passing checks, no unapproved scope deviations, and a verified integration checkpoint.
- [`TR-007`](#tr-007) building → planning — Non-REQ execution conflicts return through planning and document verification.
- [`TR-008`](#tr-008) verification → bug_resolution — A sealed ObservationBatch carries the exact immutable Finding set (with encounters, wall actions and evidence refs) into S8 on the next Main PreToolUse.
- [`TR-009`](#tr-009) verification → acceptance — The final accepted ReviewResult continues the original Gate/TR-009 in the same completion event, then prepares the current S10 acceptance questionnaire.
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
- [`TR-031`](#tr-031) release_audit → verification — A functional or contract defect found in the release audit restarts the complete review round (L3-S10 §4.

_Phase: bug_resolution_

- [`PTR-BUG-09`](#ptr-bug-09) repair_readback → planning — S9 enters planning with a dispatchable Assignment set; no implementation write is authorized yet.
- [`PTR-BUG-10`](#ptr-bug-10) planning → reproducing — Builders report their intended repair and failing pre-fix signal before execution.
- [`PTR-BUG-11`](#ptr-bug-11) reproducing → fixing — Only the explicit execution checkpoint releases implementation writes.
- [`PTR-BUG-05`](#ptr-bug-05) fixing → targeted_reverification — Every repair invalidates affected historical PASS evidence before recheck.
- [`PTR-BUG-06`](#ptr-bug-06) targeted_reverification → ready_for_full_review — A targeted pass never substitutes for the full review round; it records the S9-to-S7 handoff checkpoint.
- [`PTR-BUG-12`](#ptr-bug-12) targeted_reverification → targeted_reverification — After an environmental or authority blocker is resolved, reopen the existing targeted verification checkpoint; runtime hard-checks status=blocked, failure_route=blocked, and a non-empty resolution reason.
- [`PTR-BUG-07`](#ptr-bug-07) targeted_reverification → investigation — Failed repair verification returns to the same InvestigationCase for causal reassessment; it does not create a new BUG draft unless the causal model genuinely differs.

See [Legacy (PTR-BUG)](#legacy-ptr-bug) at the end of this Manual for the legacy compatibility transitions (PTR-BUG-01..04, PTR-BUG-08).

_Phase: planning_

- [`PTR-PLAN-01`](#ptr-plan-01) design → contracts — Advance formal planning from design to contracts after the original design quality gate passes.
- [`PTR-PLAN-02`](#ptr-plan-02) contracts → tasks — Advance contracts to tasks through the original S3 gate and PTR-PLAN-02.
- [`PTR-PLAN-03`](#ptr-plan-03) contracts → design — Main explicitly opens a fresh design entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections.
- [`PTR-PLAN-04`](#ptr-plan-04) contracts → contracts — Main explicitly opens a fresh contracts entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections.
- [`PTR-PLAN-05`](#ptr-plan-05) tasks → design — Main explicitly opens a fresh design entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections.
- [`PTR-PLAN-06`](#ptr-plan-06) tasks → contracts — Main explicitly opens a fresh contracts entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections.
- [`PTR-PLAN-07`](#ptr-plan-07) tasks → tasks — Main explicitly opens a fresh tasks entry within the original locked REQ for a named professional reason; preserve REQ/S2/S3 frozen inputs and accepted history; invalidate only tasks, dispatch plans and downstream review projections.

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

In questionnaire mode, the current S4 professional answer must be received by the original consumer. The gate checks the complete accepted REQ-scoped TASK/index batch, clause coverage, DAG and Closing Contracts; accepted source drift is reported as a concrete conflict. TR-002 revalidates the original reception and sources in its Writer transaction and registers that exact contract/TASK delivery before entering document_verification. An incomplete gate keeps the original followup retryable. Legacy packet-off planning retains its original checks and registration path.

- `planning_complete` [semantic_check] — At least one current-baseline contract document has status=locked with a matching on-disk markdown Status field (contracts are registered by PTR-PLAN-02), AND every docs/dev/tasks/TASK-*.md declares status complete or cancelled with at least one complete (the batch is registered by TR-002's own register_planning_tasks action). Fingerprints are owned by registration and reachability, not re-checked here.
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
  Accepted kinds: `builder_report`, `agent_completion`, `completion_report`

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

A sealed ObservationBatch carries the exact immutable Finding set (with encounters, wall actions and evidence refs) into S8 on the next Main PreToolUse. TR-008 records only the cursor handoff. Main then reads the supplied Batch/Findings and saves grouping_rationale in the exact S8 intake questionnaire; its original ingest consumer creates the InvestigationCase after exact-set/hash/baseline validation. The runtime investigation ingest command remains a direct domain/recovery entry, not a second required submission. S8 consumes the observed facts without re-reproducing symptoms by default. No per-Finding BUG draft is created; any legacy BUG draft is only a compatibility projection of the Case.

- `observation_batch_sealed` [semantic_check] — _no spec_

Evidence: `observation_batch_record`

Diagnostic evidence slots for the original sealed batch. The next Main PreToolUse invokes the original transition; authors do not copy these bindings or submit another Result.

- `observation_batch_record`: `--evidence observation_batch_record=<reference>`
  Accepted kinds: `observation_batch`

If the handoff is incomplete, Main inspects the original sealed batch and `loop-harness explain TR-008` feedback. Preserve accepted Reviewer answers. After the transition, fill and save the supplied S8 intake; its original consumer creates the Case.

### `TR-009` {#tr-009}

_verification → acceptance_

The final accepted ReviewResult continues the original Gate/TR-009 in the same completion event, then prepares the current S10 acceptance questionnaire. Every required Claim of the current ReviewPlan needs a consumed PASS; clean_round_valid recomputes the snapshot, current product baseline and applicable E2E assets at promotion. Reception, transition and material preparation are separate facts. Resume a pending original exit with runtime review-continue; recover missing current S10 materials with runtime audit-prepare. PreToolUse remains a compatibility recovery path.

- `clean_round_valid` [semantic_check] — _no spec_

Evidence: `clean_round_record`

Diagnostic evidence slots for the original machine CleanRound. The final accepted ReviewResult invokes Gate/TR-009 and prepares S10 in the same completion event; authors do not copy these bindings.

- `clean_round_record`: `--evidence clean_round_record=<reference>`
  Accepted kinds: `clean_round`

Reception, transition and material preparation are separate facts. After resolving a named original Gate failure, resume with `loop-harness runtime review-continue --root <root>`; after entering S10, restore missing current materials with `loop-harness runtime audit-prepare --root <root>`. Existing reports and accepted Reviewer answers are preserved; material recovery does not repeat TR-009.

For normal S10, save the supplied professional Manifest; its original consumer records the artifact. Explicit unpacketized/recovery commands only:

```text
loop-harness s10 manifest validate --root <root> \
  --file <acceptance-manifest.json> --type acceptance
loop-harness runtime evidence add --root <root> \
  --id <id> --kind acceptance \
  --path <envelope.json> --produced-by <agent> --responsibility <role>
```

The original consumer supplies `audit_manifest_path` and `audit_manifest_sha256`. Use the current professional Manifest to explain actual coverage and counterevidence; a path or row count is not proof of product behavior. Resolve unknowns and blocking findings and identify the owner/disposition of remaining risks. `s10 status` is optional read-only progress; do not call `runtime transition` manually.

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

- `acc_complete` [semantic_check] — The latest valid acceptance report for the current responsibility, baseline generation and review round records pass with no reported unfinished rows or blockers; this reads professional completion, not historical source proofs.
- `clean_round_still_valid` [semantic_check] — verification.EvaluateCleanRound recomputes completion from current registered round facts, including its ReviewPlan, required Claims, invalidations and blockers; it does not reverify every product file's current bytes.

Evidence: `acceptance_record`, `clean_round_record`

Evidence bindings (copy into `runtime transition`):

- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`
- `clean_round_record`: `--evidence clean_round_record=<reference>`
  Accepted kinds: `clean_round`

If a binding is missing, retry with the command above; run `loop-harness explain TR-015` to inspect current candidates.

For normal S10, save the supplied professional Manifest; its original consumer records the artifact. Explicit unpacketized/recovery commands only:

```text
loop-harness s10 manifest validate --root <root> \
  --file <acceptance-manifest.json> --type acceptance
loop-harness runtime evidence add --root <root> \
  --id <id> --kind acceptance \
  --path <envelope.json> --produced-by <agent> --responsibility <role>
```

The original consumer supplies `audit_manifest_path` and `audit_manifest_sha256`. Use the current professional Manifest to explain actual coverage and counterevidence; a path or row count is not proof of product behavior. Resolve unknowns and blocking findings and identify the owner/disposition of remaining risks. `s10 status` is optional read-only progress; do not call `runtime transition` manually.

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

- `release_audit_approved` [semantic_check] — The latest valid release audit for the current responsibility, baseline generation and review round records approved or approved_with_risk with no reported unfinished rows or blockers; this reads professional completion, not historical source proofs.
- `acc_complete` [semantic_check] — The latest valid acceptance report for the current responsibility, baseline generation and review round records pass with no reported unfinished rows or blockers; this reads professional completion, not historical source proofs.
- `clean_round_still_valid` [semantic_check] — verification.EvaluateCleanRound recomputes completion from current registered round facts, including its ReviewPlan, required Claims, invalidations and blockers; it does not reverify every product file's current bytes.

Evidence: `release_audit_record`, `acceptance_record`, `clean_round_record`

Evidence bindings (copy into `runtime transition`):

- `release_audit_record`: `--evidence release_audit_record=<reference>`
  Accepted kinds: `release_audit`
- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`
- `clean_round_record`: `--evidence clean_round_record=<reference>`
  Accepted kinds: `clean_round`

If a binding is missing, retry with the command above; run `loop-harness explain TR-017` to inspect current candidates.

Normal Main save validates the current release-audit Manifest and records its evidence. The commands below are explicit unpacketized/recovery examples:

```text
loop-harness s10 manifest validate --root <root> \
  --file <release-audit-manifest.json> --type release_audit
loop-harness runtime evidence add --root <root> \
  --id <id> --kind release_audit \
  --path <envelope.json> --produced-by <agent> --responsibility "Release Auditor"
```

Eight audit themes guide relevance; use applicable actual observations rather than manufacturing eight rows. The supplied professional Manifest carries the outcome, evidence and risk disposition; Markdown prose alone is not its accepted record.

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

A blocked audit preserves actual observations and blockers in the current Manifest. Main normally saves that exact answer for original TR-018. Explicit unpacketized/recovery examples:

```text
loop-harness s10 manifest validate --root <root> \
  --file <release-audit-manifest.json> --type release_audit --outcome blocked
loop-harness runtime evidence add --root <root> \
  --id <id> --kind release_audit \
  --path <envelope.json> --produced-by <agent> --responsibility "Release Auditor"
```

Keep actual blockers, their evidence and next responsibility in the Manifest. Original TR-018 records the pause; do not call `runtime transition` or manufacture audit rows.

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

Low-level diagnostic evidence slots only; not the normal amendment entry. Follow [Approved requirement amendment](#approved-requirement-amendment); the original receiver binds these facts, not the author.

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`
- `req_lock_record`: `--evidence req_lock_record=<reference>`
  Accepted kinds: `human_decision`

These slots describe the original transaction for diagnosis; use the normal amendment entry above rather than composing or retrying a transition.

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

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

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

### `TR-031` {#tr-031}

_release_audit → verification_

A functional or contract defect found in the release audit restarts the complete review round (L3-S10 §4.6). Affected historical PASS evidence is invalidated at S9.3 impact_reconciliation (agent-protocol §S9), not here: the action fail-closes on the affected paths the automatic PreToolUse path cannot supply while S10 freezes the product surface, and the record-derived sweep would invalidate the change-impact ledger the next round must freeze from.

_No guards._

Evidence: `acceptance_record`, `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`
- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain TR-031` to inspect current candidates.

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

_targeted_reverification → targeted_reverification_

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

Advance formal planning from design to contracts after the original design quality gate passes. Main saves the issued S2 answer with architecture_path and conclusion pass; the original consumer records planning_design evidence and runs this outlet. The gate requires current design evidence and a real ARCHITECTURE-*.md whose top status line (状态/Status) declares locked, plus the bound requirement conditions. Missing tokens document:design:locked / evidence:planning_design_record identify the remaining gate work. Reception, committed transition and S3 material preparation are separate outcomes; authors do not create evidence envelopes or qualify by producer label.

- `ui_impact_resolved` [semantic_check] — runtime.bound_req.metadata.ui_impact is not `unknown`, so the planning phase is not paused on the SM-003 gate waiting for §11 of the REQ to clarify UI impact.

### `PTR-PLAN-02` {#ptr-plan-02}

_contracts → tasks_

Advance contracts to tasks through the original S3 gate and PTR-PLAN-02. In questionnaire mode the listener gives exact answer and output paths; Main saves one professional answer, and the original consumer checks the current bound REQ's complete locked contract batch and shared-model dependencies. The gate rechecks the accepted subjects before the original transaction registers them. Normal successful Post handling retains its original continuation. If the gate remains pending after reception, preserve the accepted answer, repair the concrete gap and execute the complete original runtime stage-answer recover command supplied by feedback. If the transition is already committed but next-material preparation failed, repair its named source gap and execute the supplied original runtime stage-answer issue command to prepare the current stage. Neither path requires refilling or resaving the accepted answer; arbitrary reads or material saves do not promise retry. An accepted answer is not a completed stage. Explicitly unpacketized legacy flows retain their existing disk/evidence path.

- `contracts_checked` [semantic_check] — S3 typed reception checks the declared current-REQ delivery and source view with semantic.ContractsCheck: REQ/CASE references and contract clauses resolve, shared models and examples validate, and system-maintained source fingerprints match. PTR-PLAN-02 rechecks the accepted delivery for registration. Existing disk-based compatibility flows retain their original checks; fingerprint columns are not an author obligation.
- `scenario_bridge_checked` [semantic_check] — S2's AC↔CASE bridge (scenario.GuardBridgeChecked) runs at PTR-PLAN-02: every AC of the bound REQ reaches a rule via FR source_refs (with branches), or carries an endorsed N/A (NFR id / §A4). With no module packages at all, only fully N/A-endorsed REQs pass — an AC pointing at FR- with nothing citing it is a broken denominator.

### `PTR-PLAN-03` {#ptr-plan-03}

_contracts → design_

Main explicitly opens a fresh design entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections. Use runtime stage-answer rework; requirement changes retain the original human boundary.

- `planning_rework_scope` [evidence_attestation] — The current packet-mode S3/S4 entry keeps its authorized locked REQ; the named Main request supplies the professional reason and the original Writer invalidates dependent planning projections.

### `PTR-PLAN-04` {#ptr-plan-04}

_contracts → contracts_

Main explicitly opens a fresh contracts entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections. Use runtime stage-answer rework; requirement changes retain the original human boundary.

- `planning_rework_scope` [evidence_attestation] — The current packet-mode S3/S4 entry keeps its authorized locked REQ; the named Main request supplies the professional reason and the original Writer invalidates dependent planning projections.

### `PTR-PLAN-05` {#ptr-plan-05}

_tasks → design_

Main explicitly opens a fresh design entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections. Use runtime stage-answer rework; requirement changes retain the original human boundary.

- `planning_rework_scope` [evidence_attestation] — The current packet-mode S3/S4 entry keeps its authorized locked REQ; the named Main request supplies the professional reason and the original Writer invalidates dependent planning projections.

### `PTR-PLAN-06` {#ptr-plan-06}

_tasks → contracts_

Main explicitly opens a fresh contracts entry within the original locked REQ for a named professional reason; preserve accepted history and invalidate dependent planning projections. Use runtime stage-answer rework; requirement changes retain the original human boundary.

- `planning_rework_scope` [evidence_attestation] — The current packet-mode S3/S4 entry keeps its authorized locked REQ; the named Main request supplies the professional reason and the original Writer invalidates dependent planning projections.

### `PTR-PLAN-07` {#ptr-plan-07}

_tasks → tasks_

Main explicitly opens a fresh tasks entry within the original locked REQ for a named professional reason; preserve REQ/S2/S3 frozen inputs and accepted history; invalidate only tasks, dispatch plans and downstream review projections. Use runtime stage-answer rework; requirement changes retain the original human boundary.

- `planning_rework_scope` [evidence_attestation] — The current packet-mode S3/S4 entry keeps its authorized locked REQ; the named Main request supplies the professional reason and the original Writer invalidates dependent planning projections.

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

