# Frozen execution compatibility — agent-dispatch

Read only for an existing pre-migration registered execution or explicitly frozen teammate protocol. This document does not define creation, identity, activation, resume or normal delivery of a new Claude Code native Agent. Current work follows [SKILL.md](SKILL.md) and its stage professional contract. Preserve actual human approval, scope and historical receipts; do not reinterpret old execution facts as new permission.

## Authority
Dispatch modes and the agent lifecycle are defined in `docs/control/loop-definition.json` (`entity_lifecycles.agent`) and this Skill; the runtime events are executed by `loop-harness runtime agent-event` / `runtime task-complete`.

Semantic progress for one responsibility attempt uses only `pending`, `active`,
`blocked`, `done`, and `cancelled` ([shared rule](../../docs/rules/state-machine.md#21-loop-responsibility-status)). Runtime lifecycle values, Assignment checkpoints such as `consumed`, receipt fields such as `work_complete`, and platform `completed` remain exact compatibility facts. Do not ask the Worker to author a generic status; use the current receipt and Hook projection.

Use a prepared Worker's exact `observation_commands["check-reuse"]` to inspect shadow check applicability. Every `action=run` requires the actual original check. Tool receipt registration is an execution fact; it does not replace the Builder report, Main required integration checks, independent review or fresh S7. A self-authored or resealed receipt is not trusted evidence. Unknown isolation permission conditions cannot enable reuse.

Only an explicitly enabled original Worker static profile may return `action=reuse`, referencing the same accepted tool receipt. Default profiles stay shadow; Main required checks and S9/fresh always run. Use `observation_commands["check-running"]` for a competing request's bounded diagnostic. It cannot prove process liveness, acceptance or completion, and interruption never fills PASS.

## Entry Conditions
- An Agent Definition exists under `agents/<role>.md`.
- A validated team manifest assignment exists (S7 reviewer assignments carry `claim_ids`; S6 builders carry the TASK binding).
- The document chain (TASK/BUG → contracts → REQ → design) is readable and fingerprinted.

## Required Inputs
| Input | Path / field | Why |
|:--|:--|:--|
| Assignment | manifest `assignments[]` (agent_id, dispatch_mode, scope) | dispatch coordinates and mode |
| Document chain | locked REQ/design/contracts/TASK | what the Worker must read first |
| Runtime state | `.claude/loop-state.json` | CAS revision for every event |

## Dispatch modes
| mode | flow | use when |
|:--|:--|:--|
| `plan_checkpoint` (default) | Worker completes the professional answer required by its original role, then **continues immediately** without a second approval | ordinary work — the vast majority |
| `plan_approval_required` | readback (`readback_response`) → Main `understanding_approved` → activation envelope | high-risk or irreversible work (destructive tests, production-adjacent data, spec changes) |
| `one_shot` | single idempotent action, no plan checkpoint | trivial fire-and-forget tasks |

## Procedure
1. Launch the Worker with the assignment (register-workgroup already stamped `dispatch_mode` on the agent row; default `plan_checkpoint`).
2. Use the stage's original answer path. Ordinary S5/S7/S8 review or investigation assignments use the Hook's exact domain questionnaire and do not send a duplicate generic PLAN_REPORT. S9 Builders use the professional PlanReport; saving it updates the existing lifecycle checkpoint through the original adapter, so they do not send a second generic PLAN_REPORT. S6 Builders retain their professional PLAN_REPORT and supplied submission path. Only send a separate generic PLAN_REPORT when the original role contract explicitly requires it. Preserve the domain-specific checks, ownership, independence, and human approval gates; `plan_checkpoint` never means that work may skip them.


3. `plan_approval_required`: the Worker submits a readback; Main approves (`understanding_approved`) before the activation envelope is accepted.
4. For an explicit `plan_approval_required` assignment, register and approve the read-back through its original approval/activation chain. Do not substitute an ordinary `plan_checkpoint` questionnaire for that approval. If the system explicitly reports that an expected lifecycle followup is pending, follow its recovery action; do not replay a successful or already advanced checkpoint by hand.
5. For S6, save the supplied professional PLAN, do the assigned work, then fill and save its exact Result path; the original begin/task-complete adapters receive those saves. For S5/S7/S8, the Hook prepares the current professional questionnaire and supplies its exact paths; complete only that domain answer and save it for the original consumer. For S9, save the professional PlanReport and later RepairResult questionnaire; the original consumers project lifecycle/check completion. An explicit approval mode still waits for its original approval. Legacy direct result/submit interfaces are diagnostics or compatibility paths, not a second submission after a successful questionnaire save. Queued work receives a questionnaire after original dispatch release; controlled revision requires the new questionnaire.

### Frozen s6-v1.2 canonical Assignment PLAN

A building 1.2 Builder manifest may explicitly choose
`assignment_record_contract: "s6-v1.2"`. Original registration then creates the
canonical Runtime Assignment Record in the same Team/Agent/TASK transaction.
Read the original native/workspace context and use its PLAN draft and Schema.
Its machine identity, Record revision and digest are already bound. Answer only
objective, planned_paths, assertion_checks, dependencies and risks_blockers.
PLAN assertions and oracles prove behavior or artifacts verifiable before
submitting the Result. Main keeps receiver registration in the original delivery workflow;
its acceptance receipt proves that fact after submission. Main-owned checks stay
in required_checks and the original integration workflow.
This thin branch has no steps field. Dependencies and risks start as null; write
[] only after confirming there are none. Planned paths must stay within the
record's effective_write_paths; dependency identifiers must match its registered
requirements. When the Record carries a closing contract, every assertion also
needs non-empty, unique zero-based `done_when_indexes` covering all frozen
DoneWhen entries. Save the completed PLAN at the exact path supplied by the Hook; its save
adapter invokes the original checkpoint and activation chain. Source drift or a missing Record requires the
original Main recovery route. Approval mode keeps its original human gate.

The generic PLAN example below applies to assignments without this explicit
S6 contract. Canonical Result acceptance uses the original task-complete Writer
and requires summary, assertion_evidence, risks_blockers and scope_deviations.
An Assignment with the new closing contract also requires closing_assessment.
Each assertion and oracle must match the accepted PLAN, with actual evidence
references. The closed message rejects authored checks and changed_paths.
Before commit and Result delivery, run the TASK's actual check commands in the
Builder workspace and state their commands and observed results in the existing
summary, or explain why a check could not run. An empty Worker runner contract
does not remove TASK self-tests; Main repeats its required checks at integration.
For a canonical S6 code correction, Main uses the existing `s6 prepare` with
`--revise-assignment <id> --revision-intent-key <new-key>` and new Builder
identities, reviews the exact returned draft, then runs its original registration
command and `workspace prepare → launch`. The Hook or `runtime workspace pending`
supplies the original recovery actions; preserve the old attempt and its evidence.
On questionnaire saves, the adapter maintains submission identity and exact replay; the author does not choose a message_id. Read the returned original acceptance receipt. Direct legacy submission retains its original identity contract. For a new
closing-contract Record, Main must have frozen `closing_contract` with
`contract: "s6-closing-v1"` and an explicit `worker_checks` array at original
registration. The closing contract also requires explicit
`integration_check_mode: "post_merge"` and original Main profiles at
`location: "main"`; do not copy Main checks into the Worker set. Records without
the contract stay on their old shape and are not retrofitted.

Use the `result_path`, complete `result_draft`, `result_questions` and
`result_schema_ref` from the original native/workspace context. It already binds
the Record revision, owner and machine identity. Fill the professional fields: summary, the actual evidence_refs for system-supplied accepted PLAN assertions/oracles, risks_blockers,
scope_deviations and closing_assessment. For the closing assessment, give one
condition per frozen DoneWhen with its zero-based index, judgment
`satisfied|incomplete|blocked`, accepted PLAN assertion indexes mapped to that
condition, and a non-empty rationale. The existing Result field value `complete`
requires every condition to be satisfied. It is an outcome value, not the
responsibility status `done`, a runner PASS, or an independent review. The questionnaire adapter supplies submission identity; do not copy machine fields or choose new IDs.

With an explicit empty Worker-check set and an assessment whose conditions are
all `satisfied`, with no declared risks or scope deviations, the original
receipt may report the compatibility field `work_complete=true` while waiting
on Main. This projects only the Builder responsibility to `done`; Main still owns the original merge
and every required Main check; fresh S7 remains independent. An incomplete or
blocked answer, declared risk/deviation, or Worker checks without current original
PASS proof keeps the responsibility `active` or `blocked`, as shown by the
original projection. Run only the original context/prepare
`owned_check_commands`: workspace `--owned-check` selects the independent owner
set; native `assignment-context check` selects the frozen index. Do not supply a
command, profile, receipt, or source root. Missing isolation remains unavailable.
Main receipts and author text cannot fill owner proof. Main Inspect/Integrate
and ACK remain blocked while this closing contract is pending, and TR-006 stays
not ready even if verified Main facts are present. A Record without the contract
remains `accepted_unassessed` with `professional_closing_contract_unproven`.

The original Result receiver owns `worker_check_facts` and `tool_closure_digest`;
never add them to the professional message. Exact registered-key replay reads
original durable evidence and writes nothing. A new unique message_id collects
current checks: changed tool facts can create a new Result even with the same
professional answer. An identical answer/closure under a new key is durably
bound to the original Result by the original Writer. Relocation or native HEAD
changes require fresh proof for new acceptance; historical Result replay keeps
its original immutable evidence.

The original Register Writer now supports the first exact-TASK/plan_checkpoint
revision slice with fresh Team/Agent identity and separate aN Main checkpoint
slots. A new charter requires a new PLAN, Result and fresh Main checks; old
approval/ACK/checks never transfer. Approval-version binding, TASK source
revisions, safe predecessor-merge provenance reuse, full S6 rollout and
supported-host PASS acceptance remain incomplete. Detailed design and test records stay in the source repository
and are not included in installed template assets.

### PLAN_REPORT file — complete minimal example

Use this compatibility example only when the current recovery route explicitly requires a
generic PLAN_REPORT. An ordinary S6 Builder saves its supplied professional PLAN; it does not construct this message. S5/S7/S8
ordinary questionnaire work and S9's PlanReport must not be duplicated here.
```json
{
  "schema_version": "1.0.0",
  "message_type": "plan_report",
  "message_id": "msg-plan-<agent>-0001",
  "correlation_id": "corr-<agent>-0001",
  "runtime_id": "<from .claude/loop-state.json runtime_id>",
  "expected_runtime_revision": <current revision>,
  "agent_id": "<your agent id>",
  "agent_definition_ref": "agents/<role>.md",
  "task_id": "<TASK-id or null>",
  "bug_id": null,
  "team_id": "<workgroup id>",
  "occurred_at": "<RFC3339 now>",
  "assignment_id": "<assignment id>",
  "assignment_revision": 1,
  "objective": "<one sentence, at least 8 chars>",
  "planned_paths": ["<paths you will read or write>"],
  "steps": [
    {"description": "what you will do", "target": "<path or artifact>"}
  ],
  "assertion_checks": [
    {"assertion": "what must hold", "oracle": "how you will know"}
  ],
  "dependencies": [],
  "risks_blockers": []
}
```
A full worked example: `internal/schema/assets/agent-message.examples.json` (the `plan_report` entry).

After writing the file, send it — `plan_ref` is a SendMessage parameter, never a field of the JSON:

```text
SendMessage(tool_input: {teammate_name: <your agent id>, message_type: "plan_report", plan_ref: <the plan file path>})
```

### Generic checkpoint and domain answers

`message_type=plan_report` is a generic platform lifecycle checkpoint only
where the original role contract requires it. It is not a stage-specific
authorization or completion record. In packet-enabled ordinary S5/S7/S8 work,
the domain questionnaire is the only professional submission; in S9, the
PlanReport answer updates the existing checkpoint through its adapter.

S9's `record_type=repair_plan_report` remains the professional planning answer:
it binds the RepairSession/RepairPlan/RepairAssignment, exposes the assertion
map, and includes a red or blocked pre-fix check before the original execution
barrier. Save the Hook-supplied answer; the existing adapter projects the same
answer onto the lifecycle checkpoint. Do not submit a duplicate generic
PLAN_REPORT or pass the domain file as `SendMessage(plan_ref=...)`. The old
dual-submission sequence is compatibility history, not the packet-enabled
normal flow.

## Outputs
- Agent lifecycle events under CAS (journal-visible).
- The original role-specific answer and, only when required by that role or
  explicit approval mode, the corresponding lifecycle/read-back evidence.

## Exit Conditions
- The Worker reaches `reported` (result submitted) and the controller consumes it; or a BLOCKER path resolves; or shutdown is approved.

## Stop Conditions
- Activation hash mismatch (plan file drifted) — recompute and resubmit.
- Requested scope exceeds the Agent Definition — revise the assignment, never widen silently.
- `plan_approval_required` work without an approval — do not self-activate.

## Non-Goals
- Do not treat "no reply from Main" as permission for the main session to do the delegated work.
- Do not add approval rounds to ordinary `plan_checkpoint` work "to be safe" — that is the complexity this mode exists to remove.
- Do not use `one_shot` for anything with side effects.

## Inlined Methodology
Loop Engineering dispatches Agents in three modes. `plan_checkpoint` means
continuous execution without a synchronous approval wait; the original role
contract decides whether the work is represented by a generic PLAN_REPORT or a
domain questionnaire. S5/S7/S8 use their professional questionnaire; S9's
PlanReport answer projects its existing lifecycle checkpoint; S6 Builders keep
their professional PLAN_REPORT. The two-round readback → approval → activation
flow remains for genuinely high-risk or irreversible work, and its original
approval/hash chain stays intact. PreToolUse observes graph progress and
reminds the Agent of missing professional duties with the exact current
questionnaire path. It does not intercept product writes or classify tool
permissions. Original domain consumers and stage gates evaluate reported facts.


## Recovery without repeated idle loops

Treat a validated plan and its activation chain as the checkpoint; a missing
legacy observation marker alone is not a reason to resend the plan. A failed
PostToolUse persistence receipt is not successful registration. Use the stated
recovery path and retain the current Assignment identity.

TeammateIdle has no documented `stop_hook_active` input. The Harness permits at
most two identical continuation reminders per session/agent/evidence state,
then allows idle with `LOOP RECOVERY STALLED`. This does not make the
responsibility `done`,
revoke the Assignment or waive the original stage conditions.
Main consumes the diagnostic and investigates the existing worker/checkpoint;
never infer a dead worker from chat silence alone. Cache failure also surfaces
an explicit diagnostic instead of endlessly demanding the same plan.

## Responsibility status and bounded waiting

Apply quality checks to the current Assignment's assertions and required
coverage. Its original recorded Result permits the existing Stop/Idle path;
Main owns subsequent integration and independent consumers. Recording a report
does not prove checks passed. A concrete acceptance-blocking risk uses the
original BLOCKER route; peer or system pending work does not silently expand
this Worker's responsibility. New work requires the original dispatch/rework.

For S6 Builders, query the original current Result with the exact
`builder_receipt_command` returned by workspace prepare, or Main's
`runtime task-receipt --agent-id <owner> --assignment-id <id>`. A qualified
delivery may project the Worker responsibility to `done` while Main integration
remains the consumer's pending work. Own failed/missing checks keep the
responsibility `active` or `blocked`; preserve evidence and use the original rework route
before further product writes. Lost registered evidence or changed TASK input
is Main recovery work. The receipt is read-only and reports existing checks;
it does not execute checks, authorize an assignment or replace independent S7.

Use the existing assignment expected_outputs/done_when/checks/stop_conditions to state the deliverable, required checks, failure route and condition for the original receiver to project this responsibility to `done`. Do not create a parallel progress ledger. Short commands should be awaited to completion. For long commands use the platform's completion notification or bounded status retrieval; fixed multi-minute sleep is not a substitute for observing a process. Backoff/rate-limit waits are permitted only when they address a concrete condition.

A repeated check must have a reason: changed code/configuration, interrupted execution, invalid evidence, a new falsifiable hypothesis, or a mandated independent/post-integration verification. Consume current valid evidence rather than re-running for reassurance. Never reuse a candidate-tree pass as a merged-tree pass or an author's result as an independent conclusion. Record each run's actual tree, command, exit status and required case collection using existing evidence receipts.

If the same failure and code fingerprint recur without new information, switch to bounded diagnosis or report the blocker to the Driver; do not repeat identical work indefinitely. Elapsed time alone is not a reason to kill a productive worker. The Driver resolves/reassigns through the supported lifecycle and continues unblocked work; it must not launch a second writer into the same assignment. After the declared checks pass and deliverables are satisfied, submit the Result and let the Driver consume it instead of inventing further review rounds.

## Current platform identity

For Claude Code >=2.1.178, do not call TeamCreate/TeamDelete or enforce Agent
team_name: teams are session-managed. Bind the real platform actor to the
registered Runtime assignment/owner; missing or ambiguous identity requires
repairing that mapping in execution modes that require it, never guessing from the only waiting task. Prefer one
role-bearing subagent unless peer coordination benefits the assignment. See
`docs/claude-platform-compatibility.md` for platform acceptance boundaries.

For ordinary S7 questionnaire work, Main registers the current logical owner /
dispatch name and uses the available native subagent tool (such as Task or Agent)
to dispatch an independent role-bearing Reviewer. The exact issued answer maps the Assignment to its
original consumer; the Reviewer supplies its own professional judgment and the
consumer validates it. This workflow requires no opaque platform ID verification,
historical Team lookup or invented handshake. An old team ID does not establish
a current platform binding. Other execution modes retain their original identity
requirements and explicit approvals.


### Worktree integration handoff

Keep Main in its original checkout. Pin each Worker to the approved main HEAD
and explicitly register its target branch; do not assume develop. Resolve
uncommitted source/document inputs before dispatch without automatic add/stash.
SubagentStop returns a pending action; Main runs
`runtime task-integrate --assignment-id <id> --root <main-root>`.
That command records verification, then completion_ack through the Agent
lifecycle, then cleanup. Retry cleanup_pending without discarding the verified
result. `runtime workspace pending` reconstructs outstanding deliveries from
durable reports/checkpoints after a restart. An owner with multiple worktrees
is acknowledged only after all of its deliveries are verified.

Read docs/workspace-execution-recovery.md before using workspace prepare. It installs
the native Harness and trusted role/skill assets, freezes dispatch input hashes,
and publishes a bootstrap receipt before ready. Main stays in its own checkout.
Use the returned `check_commands` verbatim for declared Linux sandbox checks;
they run in disposable copies and do not create source commits or replace
post-merge verification. Worker CLI routing permits its own lifecycle reports,
not Main approvals or scheduling. Use `workspace launch` from Main to start the installed role in the prepared
Worker. Main stays in its original checkout. Resume only the recorded session
with `--resume`. The launch exit is not assignment completion.
That launch is the actual Builder dispatch. When the host returns a background
task handle, Main tracks the original process/session and receives its Result;
one Builder carries that execution through integration. An additional native
Agent in the same worktree would duplicate the running Builder.
For S6, the registered Builder fills and saves its exact PLAN and Result paths
supplied by the Hook; the save adapters call the original begin/report
operations. That Builder uses its returned `commit_command` for the original
delivery commit. Main receives the Result and runs the original integration.
Explicit lifecycle commands
remain compatibility/diagnostic adapters. For S9, Main saves the original
professional division; the original issuer supplies each Builder's exact
Main-root PlanReport view/answer paths and current-step brief for Main to carry
verbatim into the native task. The Builder performs real prechecks and personally
saves PlanReport there, then waits without implementing; that save updates the
existing plan-checkpoint/AgentBegin projection. After every original required
report and condition is ready, the system calls original BeginRepairExecution;
Main then uses the original registered Worker prepare/launch flow, and the
Builder implements only at the prepared Worker root with its current RepairResult
path. Do not run a manual execution-begin command on the normal path.
The Builder fills the professional result and saves it; the original consumer
checks the answer, actual Git changes and required checks, then projects the
same result onto the existing completion record. Do not send an extra generic
PLAN_REPORT or `completion_report`, or manually issue/consume a questionnaire.
Main still integrates the candidate, runs Main-level checks and human decisions,
and completes the original impact, independent targeted verification, handoff,
and fresh S7 gates. If an exact answer path is missing, route it to Main/system
preparation instead of guessing or switching to the compatibility CLI.
See `docs/workspace-execution-recovery.md` for explicit adoption, replacement and relocation.


## Temporary worktrees and committed stage delivery

Bind both destinations explicitly with `req bind --dev-branch <branch> --release-upstream <upstream>`; neither defaults to develop or the remote default. The project's authority root and this development branch own retained changes.

Before dispatch, commit the required stage documents, code and tests to that branch. For native Workers, create/reuse the registered execution with `runtime workspace prepare --root <authority-root> --assignment <id> --agent <owner>`, then use its returned launch command. Prepare owns checkout creation; do not run the legacy `worktree-create` before it. Git worktrees share Git objects, but have separate indexes and working files: staged and dirty parent changes are absent. Native Claude Code base-ref defaults must be checked against the binding.

Workers commit scoped results before submitting the Result/report; an accepted report freezes that delivered source; corrections use the original rework or Assignment revision route. Main reviews them and runs `runtime task-integrate --root <authority-root> --assignment-id <id>`: normal merge commit, verification, acknowledgement, cleanup. This is development integration, not release. Failed integration preserves the checkout; resolve an environment failure before retrying integration. For canonical S6 source/report corrections, Main uses the original `register-workgroup --revise-assignment` with a fresh manifest, Team and Agent; preserve the old checkpoint and Result. Legacy rework keeps its original contract. Do not discard unreceived work or create replacement worktrees indefinitely.

Stage gates consume each input from the source declared in the upstream file contract. Formal deliverables use one pinned Git tree; only explicitly allowed evidence/runtime inputs use disk. Dirty files cannot satisfy formal stage delivery. Worktree backlog reminders are advisory, delivered to Main through Agent-visible context, and never authorize deleting unknown worktrees.

## Contract reading and shared implementation dependencies

A Builder enters through its TASK: goal, allowed changes, dependencies, closing
assertions, then ordered links to local responsibility, SYNC operation and shared
model. Follow [shared-model reading rules in the factory](../../docs/rules/shared-model-contracts.md) (after installation: [project rule](../../docs/rules/shared-model-contracts.md)).
Do not recursively expand all background links. A shared implementation task
must be integrated before dependent worktrees start; independent generation from
the same committed schema can run in parallel. Compare shared design inputs,
not whole checkout commit equality. Actual consumption needs tests, not just a
model reference in the completion report.

## Overall dispatch plan (waves-v1)

Follow [factory dispatch rules](../../docs/rules/dispatch-plan.md), or after installation
[project dispatch rules](../../docs/rules/dispatch-plan.md). S4 delivers the REQ's
index as an ordered wave checklist; S5 reviews and freezes it with TASKs. S6 reads
that plan and `s6 status --capacity <actual total slots>` before dispatch, then
recomputes after integration or capacity release. Start every compatible ready
TASK within actual capacity; do not wait for unrelated tasks in an earlier wave.
Only verified integration into the root development branch releases consumers.
Never write runtime progress back into frozen plans or TASKs.

Use `s6 status --advice --capacity <actual total slots> --json` to project the
current Main reception action from original reports and checkpoints. Execute
its concrete original argv through the existing integration/recovery entry.
If a correction question is present, describe the actual correction before
choosing rework. Unknown capacity leaves `dispatch_budget` null. The optional
`--max-unreceived-results <ceiling>` caps advisory recommendations only; it
does not change the original next batch, registration or required coverage.
Cleanup is counted separately. Checkpoint state alone does not prove a live
process; refresh after reception, recovery or a Runtime revision change.

A fresh Runtime may explicitly freeze `dispatch_backpressure` at init/install.
When configured pressure is paused, receive original results until the recorded
resume threshold before registering another S6 Builder. Existing work and all
all required responsibilities remain in the coverage set until the original
consumers resolve them. Main acknowledgement binds original
report paths and hashes; a replacement report needs a current acknowledgement.
Cleanup does not hold this gate after matching Main acknowledgement. The status
ceiling flag does not enable or modify frozen configuration. Use `s6 status
--advice` for the current original action; never edit pressure or ACK bindings.

Main may read `runtime workspace integration-status --root <Main>` for the
original OS lock's point-in-time resource state. A real busy lock selects
`observe_integration` in reception advice; use its complete read-only argv
before retrying reception. Pending results and compatible dispatch candidates
remain visible. This observation grants no permission, PASS or completion and
identifies no Assignment/PID. An old diagnostic or checkpoint is not liveness.
Unknown observation needs diagnosis; original execution locks still decide.

## Frozen Assignment Context (G01 shadow)

For a new 1.2 workspace, prepare may return `assignment_context_ref` and
`context_mode=shadow`. Read that local immutable file first, then the frozen
TASK → contracts → REQ → design chain and installed Skill paths/digests.
The original manifest SkillRefs continue to authorize capabilities; shadow
selection changes no permissions or legacy preloading. Do not assume a
Skill was loaded merely because it appears in this projection.

For `plan_checkpoint`, `.claude/submissions/plan.json` starts with actual
machine bindings and unanswered professional fields. Use the context's
`plan_questions` and `.claude/inputs/agent-message.schema.json` to complete it,
then use the exact supplied begin command. Do not copy identities from examples,
self-approve, bypass approval mode, or overwrite a received plan. Mutable plan
and answer files are excluded from the read-only bootstrap receipt.

Missing installed sources belong to Main; a factory file is not a replacement.
Existing ready or legacy preparing executions are not silently upgraded.
Explicit execution replacement keeps its original sources/profile and gives the
context a new execution generation; named S5 revisions follow their original
new packet/execution path. The compatibility field work_complete=true projects
only the owned responsibility to done when its original receipt and checks are
current; Main handles later dispatch and system followup.

### Native and teammate registered source view

A native lifecycle or a registered `team launch` can supply `assignment_context_ref`
(path and SHA256). Read it in the original Main authority root and verify its
actual source bytes. It contains the registered owner/Team/Assignment, the
current original agent_state, input_read_order, installed Skill hashes and the
same machine plan scaffold used by workspace prepare. Its source_root is the
original authority, not a new write grant. Read original document paths in this
view; workspace paths remain below the original frozen `.claude/inputs` copy.

An unregistered launch example has no registered source view. Missing sources
produce a Main recovery issue, not a ready dispatch. A prepared independent
execution cannot fall back to this Main view. Plan drafts keep professional
fields unanswered and never replace an authored plan. If already working, keep
the original accepted plan/activation instead of sending a second plan. S5
responsibility status remains derived from the original domain receipt;
receiving a source view does not replace that receipt, another owner's work, or
the human Gateway.

### Fixed native/teammate context generation

Main may run `runtime assignment-context prepare --root <authority> --agent-id
<actual-owner>` to fix the existing registered1.2 source view in the original
Agent row. Once prepared, lifecycle and registered readback reuse its exact
path/hash/profile instead of recomputing today's selection. Restore missing or
changed original sources; do not delete the binding or regenerate another
profile. Captured agent_state is historical: live Runtime still owns plan,
activation and completion. Independent executions keep their original
Inputs/Bootstrap and never use this Main view as fallback.

Only an original S5 named revision allows `--replace-generation <old>`: its
actual new packet must cite the received predecessor, and the role, profile,
Skill and Schema sources stay fixed. Retrying that replacement preserves the
committed generation and authored files.

`runtime assignment-context render-role --root <installed-root> --role
backend-builder` returns a source-hashed core-role candidate. It changes only
the Skill preload block and reports enabled=false. This command does not
install or enable the candidate; actual execution rollout remains separate.

### First enabled workspace Backend profile

For a new original1.2 S6 backend-builder execution, Main can explicitly use
`runtime workspace prepare --context-profile backend-core` with the actual
assignment/owner. Default remains shadow. The original context and inputs
freeze the generated role source/hash/core list; Bootstrap installs it only in
that new Worker's backend-builder path and records the exact bytes. Original
authority fields/body, Main installation and activation SkillRefs stay fixed.
Read applicable selected extra Skill sources with existing Read tools;
workspace allowedTools does not add Skill. Existing ready/preparing cannot
change profiles, and original replacement preserves the fixed selection.
This execution assembly is not proof of real Claude consumption or token
improvement; keep the original plan, checks, result and integration paths.

## Engineering convention context

New workspace/native contexts can carry `conventions` from original registered
TASK/Assignment scope and current formal source proof. S5 reuses the issued
frozen analysis. The original Bootstrap/fixed binding lists actual source
hashes and read paths; do not copy defaults or infer them from a registry
status. Preserve old ready contexts, all manifest SkillRefs and original
activation/approval. Conflict or unresolved scope belongs to the original
owner; an absent index permits ordinary authorized engineering choices.

## Check entrance readiness

Read the supplied `check-capabilities` observation when actual runner/tool
readiness is unclear. It proves only the listed verification scope, not complete
dependency or service readiness. Main prepare/launch rejects a clearly missing
literal check tool before publication/session reservation. Opaque legacy shell
checks remain explicitly unverified until an approved profile is available.
Preserve the original Worker isolation and check location; environment-owner
repair is not a permission to skip activation, tests or verified integration.

## Explicit profile readiness

When the original manifest declares `check_profiles`, keep its exact check
indices/commands, immutable location, bounded timeout and declared environment.
New preparation freezes dependency/fixture hashes and profile data; existing
ready executions retain their original fields. Read actual tool receipts after
execution. Profile/environment observations and integrity do not authorize
reuse, mark a professional PASS or replace mandatory Main/independent checks.
