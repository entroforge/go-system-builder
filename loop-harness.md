# loop-harness — Transition Checklist

> What `loop-harness` checks at every transition. Read the
> relevant section before calling `forward`; verify each bullet
> before requesting the harness to advance.

- **Path**: `loop-harness.md`
- **Harness version**: dev

---

## Controller recovery protocol

Hooks observe current responsibilities and connect natural delivery boundaries. Native Start gives bounded context; native Stop invokes the original professional consumer. S7/S8/S9 PreToolUse listens for ready original answers, with successful PostToolUse and Stop/SubagentStop fallback. Main saves its own questionnaires through their existing consumers. S5/S6 retain their original ordinary-tool reception contract. Ordinary tools do not activate work or run every stage gate. Milestone is an existing recovery cache, not proof that each event committed or context was delivered.

### Bootstrap binding boundary

S6 owners can query `runtime task-receipt --agent-id <owner> --assignment-id <id>`; registered Workers use the exact builder_receipt_command returned by prepare. The read-only receipt distinguishes current qualified delivery, own pending checks and Main integration; checks_source=original_report does not claim new check execution. Lost registered evidence and changed TASK input go to Main recovery/rework. The receipt neither advances TR-006 nor replaces S7 or S9 RepairResult completion.

`revision` has no global maximum. A Hook/controller checkpoint may advance the inactive bootstrap runtime before `TR-001`; binding lets the Writer read and commit the current Runtime under lock, archives the complete inactive state/journal pair, and installs a new `loop-REQ-*` runtime at revision `0` with an empty active journal. The `binding_receipt` carries `event=req_bound`, the approved REQ and the original source runtime identity. Do not edit revision by hand or reuse a pre-bind runtime snapshot after binding; the runtime identity changes and stale identities are rejected.

1. Read the `Next` action and current `Stage` from the Hook packet, then follow its `Read in order` list.
2. Read the linked `docs/control/agent-protocol.md#sN` section before acting.
3. If blocked or the Runtime is unclear, read this Manual. Use `runtime reconcile` only for a provable journal/state integrity gap, not an ordinary CAS conflict; status is optional diagnostics, never a per-tool prerequisite. When the live Quality Gate checklist is unclear, run `loop-harness ready` (diagnostics; never hand-push a Transition from it). `doctor` is structural schema/manual/policy_ref only — not stage readiness or runtime health; use `loop-harness health` for cumulative runtime signals. For the S7→S8→S9→S7→S10 action sequence, run `loop-harness actions`.
4. Execute the one missing deliverable/evidence named by Hook/`ready` `missing[]`; do not invent a parallel lifecycle.
5. Main reads the native return and original receipt. S6 uses the supplied `runtime task-integrate --assignment-id <id> --root <main-root>`; S9 Main integrates actual contributions and runs necessary checks, then saves its original Impact judgment. No particular integration command, Git commit, Final or proof qualifies S9 answer or Impact reception. Do not re-wake a completed Worker for Main integration. No develop fallback or automatic Main checkout.
6. In new native S6, Main runs `s6 prepare --root <authority-root> --task <TASK> --role <Builder-role>` and passes its exact brief into Claude Code native Agent with worktree isolation (`worktree.baseRef: head`). No preselected platform IDs, Team, workspace prepare/launch, generic begin or activation. Builder saves its professional plan before implementation, continues with real TASK self-checks, commits products, saves Result and returns its exact brief and absolute Result reference. SubagentStart supplies short engineering context; SubagentStop invokes the original Result consumer. Ordinary save hooks do not receive native S6 delivery. Main explicitly runs `runtime task-integrate --assignment-id <id>` for the actual merge, required Main checks, ACK and cleanup. Rework uses `s6 prepare --revise-assignment <id> --revision-intent-key <stable-key>` through the original revision Writer. The exact TR-003 batch and TR-006 gate remain. Never launch the same responsibility twice. See `docs/examples/s6-builder/README.md` and `docs/control/agent-protocol.md#s6`.
7. In S7, Main fills and saves the exact supplied original ReviewPlan and actually dispatches each registered responsibility with its exact view/answer. Each Reviewer saves its own original professional answer once. PreToolUse observes it; successful PostToolUse and Stop/SubagentStop provide fallback through the same consumer. The consumer checks parsing, necessary fields, readiness and the direct conclusion once and saves once. No returned reference, re-save, done flag, Git/evidence/hash proof or parallel coverage calculation qualifies reception. Necessary responsibilities complete without a Finding continue TR-009 and prepare S10; any accepted Finding hands received observations and unfinished work to S8 through TR-008, without a new CleanRound/ObservationBatch proof or another Main Pre. Already dispatched S7 responsibilities remain receivable after S8 begins; late Findings go to Main without automatically changing approval. Preserve accepted facts and follow the named original continuation for unfinished preparation. Main owns professional coverage; every Reviewer owns its assigned actual checks.

The supplied ReviewPlan includes its `coverage_inventory` and `e2e_assets`; inspect their current content and answer the professional questions without recreating machine fields. Record actual observations and cite useful sources in the original answer. Follow the specific feedback for unfinished work and recovery.
S7 preparation supplies current scenario material and exact original plan questions. Main chooses actual browser/API/command/state coverage, reusable assets and any needed isolated verification workspace; script matches and field counts do not prove coverage. Reviewers cite actual observations and readable source locations without recapture or hash admission requirements. S8 Main reads accepted Findings or the actual human report, registers actual questions and dispatches Investigators. Each Investigator saves its original result for Pre/successful Post/Stop reception. new_hypotheses is only a suggestion; Main judges cause, unknowns and route, and actual approval remains with the real approver and accurate Contract. Unsupported new-question source preparation stays a named Main/system gap, without invented aliases or protocols.

8. Main follows the approved RepairContract and records actual professional division. Carry each original prepared brief into native Agent with worktree isolation. Current native S9 responsibility selection accepts its registered absolute control-root path, complete control-root-relative path, or ./ plus that relative path; this does not locate materials under Worker cwd. Main supplies readable absolute control-root material paths and carries actual task restrictions into every delegated task. No basename, arbitrary ../, other root or role guessing is allowed; original responsibility selection, actual integration/checks, other stages and frozen paths remain. Reception does not revalidate Git, evidence or historical hashes. Private outputs stay in the actual worktree. Each Builder saves its private PlanReport before implementation, continues repair/self-checks, then saves its private Result at the exact original path in the actual worktree. Applicable PreToolUse listens for ready original Plan/Result answers, with successful PostToolUse and Stop/SubagentStop fallback through the original consumers; there is no separate plan acceptance wait. Ordinary tools remain available. Report actual results and unfinished work to Main; reception requires no extra submission, re-save or returned reference. An accepted contribution can prepare the original ChangeImpact question. Main owns actual integration, necessary checks and professional Impact judgment and arranges independent Targeted verification per the approved Contract. Impact and Targeted preparation can occur earlier; the framework does not require proof of prior Main completion. Impact reception does not certify that actual integration or checks occurred and requires no new Final, commit or preservation proof. Empty changes and empty dispositions are legitimate even when indistinguishable from the default template; they may be received early through the original natural events. Reception does not prove actual integration, checks, all Builders completed or independent verification. Main may naturally update the same original Impact as further contributions arrive; no new completion signal, re-save or Final prerequisite is required. Optional explanations do not become admission fields. The verifier saves its exact original Targeted answer for applicable PreToolUse reception, with successful PostToolUse and Stop/SubagentStop fallback through its original independent consumer, without a Builder PlanReport or product writes. A final message or returned reference is not a reception prerequisite. First incomplete native returns give Main the actual unfinished work and exact gap without claiming completion. An absent judgment normally stays draft; the original empty Impact exception remains receivable as stated above. A comprehensible blocked or unfinished conclusion is received as stated and handed to Main. After accepted passing conclusions and the applicable responsibilities, the original outlet attempts Handoff and prepares the fresh S7 seed; actual work remains the respective professional responsibility, with no completion proof, extra Handoff report, next-Hook wait or duplicate delivery. Targeted reception requires only a comprehensible overall conclusion and necessary association; missing nonessential observation or evidence fields do not reject it. A fail without classification remains an unknown cause, never an inferred same cause. If Targeted was accepted but Handoff or S7 preparation failed, preserve the accepted result and follow the specific system continuation supplied by the actual feedback; do not repeat the independent work or add Final/Changeset proof. The completed outlet supplies the original S7 ReviewPlan questionnaire and guide; Main completes and saves that questionnaire. Missing paths or preparation belong to Main/system. See `docs/control/agent-protocol.md#s9`.
For S7/S8/S9, an accepted blocked or unfinished handoff does not end the original responsibility. Its author can naturally continue actual unfinished work without another Main approval, submit or resume proof. Receive a new comprehensible conclusion as a new handoff while retaining prior accepted facts. Do not replay unchanged content or treat arbitrary private-file changes as a new responsibility. Accepted completed work and unfinished successor preparation remain separate; continue only the named missing part without re-saving an old answer, re-reading evidence or rechecking Git/hash history.

9. Continue the original S7/S8/S9 responsibility and professional findings without a default iteration budget or count-based human gate. Follow actual user constraints and existing real decisions through their original consumers; no review-count budget or decision application is required.
10. Main fills the actual acceptance or release-audit Manifest at the supplied exact path and saves once. Explain requirement dispositions, applicable observations, counterevidence, unknowns and actual risks. Eight audit themes are relevance prompts, not a row quota or mandatory new work. The original consumer derives its envelope and metrics; do not run a second validate/render/evidence submission. Unknowns and blockers remain unfinished, while explicit outcomes may be recorded without authorizing advance. S10 does not repair product code; changed products return through investigation/repair and fresh complete S7. Actual human choice remains in S11.
11. Some authority transactions carry runtime-issued ids that are not TRs and therefore do not appear in the Contents index above: `S8-REPAIR-CONTRACT-APPROVAL` (runtime investigation contract approve — the S8→S9 authority), plus the entity/record CAS ids (REVIEW-RESULT, AGENT-LIFECYCLE, BUG-LIFECYCLE, EVIDENCE-RECORD). They are driven by their runtime verbs, never by `runtime transition`.
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
- [`TR-006`](#tr-006) building → verification — Start a complete review round after every TASK in the TR-003 exact execution batch has an accepted Builder Result with its completion judgment and a verified integration checkpoint with required actual checks.
- [`TR-007`](#tr-007) building → planning — Non-REQ execution conflicts return through planning and document verification.
- [`TR-008`](#tr-008) verification → bug_resolution — An accepted Finding enters S8 in the same original reception handoff; preserve in-flight responsibilities for Main without waiting for them to complete.
- [`TR-009`](#tr-009) verification → acceptance — Continue from original accepted S7 conclusions and necessary responsibility completion in the same reception event; preserve in-flight original responsibilities.
- [`TR-010`](#tr-010) verification → paused — An accepted Reviewer verdict of req_change_required pauses the loop for a human REQ decision; the original answer consumer prepares the pause checkpoint.
- [`TR-011`](#tr-011) verification → paused — An accepted Reviewer verdict of release_blocked pauses the loop for a human release decision; the original answer consumer prepares the pause checkpoint.
- [`TR-012`](#tr-012) bug_resolution → verification — Only the ready_for_full_review handoff checkpoint for an approved RepairContract may enter a complete Delivery + QA + E2E Browser round; targeted re-verification never substitutes for that round.
- [`TR-013`](#tr-013) bug_resolution → planning — Repair-driven specification changes return through planning and document verification.
- [`TR-014`](#tr-014) bug_resolution → paused — Repair work cannot modify the locked REQ.
- [`TR-015`](#tr-015) acceptance → release_audit — Release audit starts from the accepted S10 report and the original completed S7 handoff.
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
- [`GTR-006`](#gtr-006) → planning — Consume an existing real human governance decision through its original compatibility route.

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

- `planning_complete` [semantic_check] — A current-generation locked contract and the actual applicable task batch satisfy the planning completion check. Document fingerprints are not a registration or transition prerequisite.
- `tasks_checked` [semantic_check] — S4's mechanical close (semantic.TasksCheck) runs at TR-002: the TASK batch is fully complete (cancelled tasks excluded), every task has an existing primary contract and a Closing Contract block, clause coverage between the CONTRACTS index universe and TASK §3 declarations closes in both directions, and the §8 dependency graph is acyclic (cycle path reported).

### `TR-003` {#tr-003}

_document_verification → building_

Lock only the exact contract and task versions jointly verified.

- `joint_document_pass` [evidence_attestation] — _no spec_
- `verified_versions_current` [evidence_attestation] — The transition resolves the existing document-review evidence context; it does not compare historical document hashes or claim that stored paths prove unchanged bytes.

Evidence: `document_review_record`

Evidence bindings (copy into `runtime transition`):

- `document_review_record`: `--evidence document_review_record=<reference>`
  Accepted kinds: `document_review`

If a binding is missing, retry with the command above; run `loop-harness explain TR-003` to inspect current candidates.

### `TR-004` {#tr-004}

_document_verification → planning_

Non-REQ document findings return to planning.design for reworking the failing artifacts.

- `req_baseline_unchanged` [semantic_check] — The Runtime has a bound REQ with its original path. Main judges the effect of actual requirement changes; this guard does not compare a digest or establish unchanged bytes.

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

Start a complete review round after every TASK in the TR-003 exact execution batch has an accepted Builder Result with its completion judgment and a verified integration checkpoint with required actual checks. Main considers the original risk and scope statements against the TASK and actual changes; a nonempty list alone neither blocks the exit nor approves a deviation. S7 verification planning starts from the real integrated diff at its own entry; the building stage no longer demands S7 team-manifest evidence it cannot legitimately produce.

_No guards._

Evidence: `builder_report_record`

Evidence bindings (copy into `runtime transition`):

- `builder_report_record`: `--evidence builder_report_record=<reference>`
  Accepted kinds: `builder_report`, `agent_completion`, `completion_report`

If a binding is missing, retry with the command above; run `loop-harness explain TR-006` to inspect current candidates.

### `TR-007` {#tr-007}

_building → planning_

Non-REQ execution conflicts return through planning and document verification.

- `req_baseline_unchanged` [semantic_check] — The Runtime has a bound REQ with its original path. Main judges the effect of actual requirement changes; this guard does not compare a digest or establish unchanged bytes.

Evidence: `change_impact_record`

Evidence bindings (copy into `runtime transition`):

- `change_impact_record`: `--evidence change_impact_record=<reference>`
  Accepted kinds: `change_impact`

If a binding is missing, retry with the command above; run `loop-harness explain TR-007` to inspect current candidates.

### `TR-008` {#tr-008}

_verification → bug_resolution_

An accepted Finding enters S8 in the same original reception handoff; preserve in-flight responsibilities for Main without waiting for them to complete.

- `accepted_finding_present` [semantic_check] — _no spec_

### `TR-009` {#tr-009}

_verification → acceptance_

Continue from original accepted S7 conclusions and necessary responsibility completion in the same reception event; preserve in-flight original responsibilities.

- `clean_round_valid` [semantic_check] — _no spec_

### `TR-010` {#tr-010}

_verification → paused_

An accepted Reviewer verdict of req_change_required pauses the loop for a human REQ decision; the original answer consumer prepares the pause checkpoint.

- `pause_checkpoint_recorded` [semantic_check] — _no spec_

Evidence: `review_result_record`

Evidence bindings (copy into `runtime transition`):

- `review_result_record`: `--evidence review_result_record=<reference>`
  Accepted kinds: `review_result`, `delivery_review`, `qa_review`, `e2e_review`

If a binding is missing, retry with the command above; run `loop-harness explain TR-010` to inspect current candidates.

### `TR-011` {#tr-011}

_verification → paused_

An accepted Reviewer verdict of release_blocked pauses the loop for a human release decision; the original answer consumer prepares the pause checkpoint.

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

- `req_baseline_unchanged` [semantic_check] — The Runtime has a bound REQ with its original path. Main judges the effect of actual requirement changes; this guard does not compare a digest or establish unchanged bytes.

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

Release audit starts from the accepted S10 report and the original completed S7 handoff.

- `acc_complete` [semantic_check] — The latest valid acceptance report for the current responsibility, baseline generation and review round records pass with no reported unfinished rows or blockers; this reads professional completion, not historical source proofs.

Evidence: `acceptance_record`

Evidence bindings (copy into `runtime transition`):

- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`

If a binding is missing, retry with the command above; run `loop-harness explain TR-015` to inspect current candidates.

For normal S10, save the supplied professional Manifest; its original consumer records the artifact. Explicit unpacketized/recovery commands only:

```text
loop-harness s10 manifest validate --root <root> \
  --file <acceptance-manifest.json> --type acceptance
loop-harness runtime evidence add --root <root> \
  --id <id> --kind acceptance \
  --path <envelope.json> --produced-by <agent> --responsibility <role>
```

The original consumer supplies the actual `audit_manifest_path`. Use the current professional Manifest to explain actual coverage and counterevidence; a path or row count is not proof of product behavior. Resolve unknowns and blocking findings and identify the owner/disposition of remaining risks. `s10 status` is optional read-only progress; do not call `runtime transition` manually.

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

Evidence: `release_audit_record`, `acceptance_record`

Evidence bindings (copy into `runtime transition`):

- `release_audit_record`: `--evidence release_audit_record=<reference>`
  Accepted kinds: `release_audit`
- `acceptance_record`: `--evidence acceptance_record=<reference>`
  Accepted kinds: `acceptance`

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
- `baselines_unchanged` [evidence_attestation] — The existing pause checkpoint identifies the original continuation; the transition resolves its existing evidence context. Main judges the professional effect of actual changes. Historical digests are not rechecked and cannot establish unchanged bytes.

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

- `updated_req_locked` [evidence_attestation] — The re-bind request uses the actual updated REQ and its original decision; this evidence-context guard does not authenticate a stored SHA.

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

- `req_baseline_unchanged` [semantic_check] — The Runtime has a bound REQ with its original path. Main judges the effect of actual requirement changes; this guard does not compare a digest or establish unchanged bytes.

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

- `contracts_checked` [semantic_check] — S3 typed reception uses the accepted current-REQ delivery; compatible disk-based flows check actual REQ/CASE references, contract clauses, shared models and examples. Historical fingerprint columns do not qualify reception or registration.
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

Consume an existing real human governance decision through its original compatibility route. Legacy review-budget configuration does not itself request a decision or limit continued professional work.

_No guards._

Evidence: `human_decision_record`

Evidence bindings (copy into `runtime transition`):

- `human_decision_record`: `--evidence human_decision_record=<reference>`
  Accepted kinds: `human_decision`

If a binding is missing, retry with the command above; run `loop-harness explain GTR-006` to inspect current candidates.

