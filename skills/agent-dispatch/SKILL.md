---
name: agent-dispatch
description: Use when spawning, reassigning, or reactivating any specialized Builder or Reviewer Agent; covers the three dispatch modes (plan_checkpoint continuous execution by default, plan_approval_required for high-risk work, one_shot for idempotent single actions)
category: methodology
version: 1.0.0
---
# Agent Dispatch

## Authority
Dispatch modes and the agent lifecycle are defined in `docs/control/loop-definition.json` (`entity_lifecycles.agent`) and this Skill; the runtime events are executed by `loop-harness runtime agent-event` / `runtime task-complete`.

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
| `plan_checkpoint` (default) | Worker reads assignment → sends ONE PLAN_REPORT (message_type `plan_report`) → **continues immediately**; Main stays silent when aligned, sends CORRECTION only on semantic drift | ordinary work — the vast majority |
| `plan_approval_required` | readback (`readback_response`) → Main `understanding_approved` → activation envelope | high-risk or irreversible work (destructive tests, production-adjacent data, spec changes) |
| `one_shot` | single idempotent action, no plan checkpoint | trivial fire-and-forget tasks |

## Procedure
1. Launch the Worker with the assignment (register-workgroup already stamped `dispatch_mode` on the agent row; default `plan_checkpoint`).
2. `plan_checkpoint`: the Worker sends one PLAN_REPORT — assignment_id/revision, objective, planned paths, steps, assertion checks, dependencies, risks — and starts working without waiting. Main reviews asynchronously; silence means aligned, CORRECTION means drift. The PLAN_REPORT file must satisfy the `planReport` branch of the agent-message schema (all 20 required fields — base envelope plus plan body; steps are `{description, target}` objects, assertion_checks are `{assertion, oracle}` objects); write it from the complete example below, not from memory — a malformed report fails the auto-chain and costs a manual `runtime agent-begin` recovery. Send it with the plan file path as a SendMessage `plan_ref` parameter: the PostToolUse(SendMessage) observer chains reading → activated → working automatically. The checkpoint binding is source-specific: S7 assignments are checked against the current ReviewPlan; S6/S8/S9 assignments are checked against the fingerprinted workgroup manifest, with generic non-S7 `assignment_revision=1`. For S9, the platform `assignment-s9-*` alias is distinct from the domain `repair-assignment-*`; the generic checkpoint never replaces the domain repair report.
3. `plan_approval_required`: the Worker submits a readback; Main approves (`understanding_approved`) before the activation envelope is accepted.
4. Register the plan/readback with `loop-harness runtime agent-event --event readback_submitted --message <file>`; then `activation_sent` (the hash chain binds the envelope to the submitted plan/readback file bytes — compute with `shasum -a 256 <file>` / `sha256sum <file>` after writing it). On the `plan_checkpoint` path this step is what the observer automates; run it by hand only when the auto-chain did not fire.
5. `work_started` → work → `runtime task-complete` (Builders) or `runtime review-result submit` (S7 Reviewers).

### PLAN_REPORT file — complete minimal example
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

### Do not confuse platform PLAN_REPORT with a domain plan report

`message_type=plan_report` is the generic platform checkpoint. It proves that a
platform-dispatched Agent understood its Assignment and lets PostToolUse and
the Agent lifecycle release the normal first-write checkpoint. It is not a
stage-specific authorization or completion record.

S9 has a second, domain-specific artifact: `record_type=repair_plan_report`,
submitted with `runtime repair plan-report submit --file <report.json>`. It
must bind the RepairSession/RepairPlan/RepairAssignment, expose the assertion
map, and include a red or blocked pre-fix check before
`runtime repair execution begin` can release product writes. A domain
`repair_plan_report` must not be passed as `SendMessage(plan_ref=...)`, and a
generic PLAN_REPORT does not replace the S9 domain submission. If both gates
apply, submit the generic checkpoint first and the domain artifact second.

## Outputs
- Agent lifecycle events under CAS (journal-visible).
- PLAN_REPORT / readback evidence files bound by the activation hash chain.

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
Loop Engineering dispatches Agents in three modes. The default is continuous execution: one structured PLAN_REPORT is the checkpoint — it makes the plan inspectable and correctable while the work proceeds, without a synchronous approval wait. The two-round readback → approval → activation flow remains for genuinely high-risk or irreversible work, and the activation hash chain (`approved_readback_sha256`) applies in both modes: the envelope proves which plan the Worker actually saw. The first-write barrier (PreToolUse) blocks a dispatched Worker's first product mutation until its PLAN_REPORT is recorded, whenever the hook payload identifies the sender; on platforms whose payloads carry no agent identity the barrier stays dormant (the reviewer product-write freeze and the PLAN_REPORT phase contract in every agent card carry the invariant instead). PostToolUse(SendMessage) observes the report automatically via its three-level identification ladder (payload agent_id → teammate_name → sole agent awaiting its plan checkpoint).

## Recovery without repeated idle loops

Treat a validated plan and its activation chain as the checkpoint; a missing
legacy observation marker alone is not a reason to resend the plan. A failed
PostToolUse persistence receipt is not successful registration. Use the stated
recovery path and retain the current Assignment identity.

TeammateIdle has no documented `stop_hook_active` input. The Harness permits at
most two identical continuation reminders per session/agent/evidence state,
then allows idle with `LOOP RECOVERY STALLED`. This does not complete the task,
release write permissions, revoke the Assignment, or waive a safety denial.
Main consumes the diagnostic and investigates the existing worker/checkpoint;
never infer a dead worker from chat silence alone. Cache failure also surfaces
an explicit diagnostic instead of endlessly demanding the same plan.

## Execution completion and bounded waiting

Use the existing assignment expected_outputs/done_when/checks/stop_conditions to state the deliverable, required checks, failure route and completion condition. Do not create a parallel progress ledger. Short commands should be awaited to completion. For long commands use the platform's completion notification or bounded status retrieval; fixed multi-minute sleep is not a substitute for observing a process. Backoff/rate-limit waits are permitted only when they address a concrete condition.

A repeated check must have a reason: changed code/configuration, interrupted execution, invalid evidence, a new falsifiable hypothesis, or a mandated independent/post-integration verification. Consume current valid evidence rather than re-running for reassurance. Never reuse a candidate-tree pass as a merged-tree pass or an author's result as an independent conclusion. Record each run's actual tree, command, exit status and required case collection using existing evidence receipts.

If the same failure and code fingerprint recur without new information, switch to bounded diagnosis or report the blocker to the Driver; do not repeat identical work indefinitely. Elapsed time alone is not a reason to kill a productive worker. The Driver resolves/reassigns through the supported lifecycle and continues unblocked work; it must not launch a second writer into the same assignment. After the declared checks and deliverables are complete, submit the Result and let the Driver consume it instead of inventing further review rounds.

## Current platform identity

For Claude Code >=2.1.178, do not call TeamCreate/TeamDelete or enforce Agent
team_name: teams are session-managed. Bind the real platform actor to the
registered Runtime assignment/owner; missing or ambiguous identity requires
repairing that mapping, never guessing from the only waiting task. Prefer one
role-bearing subagent unless peer coordination benefits the assignment. See
`docs/claude-platform-compatibility.md` for platform acceptance boundaries.


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

Read docs/workspace-integration.md before using workspace prepare. It installs
the native Harness and trusted role/skill assets, freezes dispatch input hashes,
and publishes a bootstrap receipt before ready. Main stays in its own checkout.
Use the returned `check_commands` verbatim for declared Linux sandbox checks;
they run in disposable copies and do not create source commits or replace
post-merge verification. Worker CLI routing permits its own lifecycle reports,
not Main approvals or scheduling. Use `workspace launch` from Main to start the installed role in the prepared
Worker. Main stays in its original checkout. Resume only the recorded session
with `--resume`. The launch exit is not assignment completion.
Use returned `begin_command`, `commit_command`, and `report_command` verbatim;
write plan.json, commit.json, and completion.json under `.claude/submissions`.
For S9, first register the domain PlanReport and begin repair execution on Main;
then prepare/launch. After generic completion, submit repair-result.json with
`s9_delivery_command`. Main integrates that immutable candidate before registering
the final RepairResult; independent verification and fresh S7 remain required.
See `docs/workspace-integration.md` for explicit adoption, replacement and relocation.


## Temporary worktrees and committed stage delivery

Bind both destinations explicitly with `req bind --dev-branch <branch> --release-upstream <upstream>`; neither defaults to develop or the remote default. The project's authority root and this development branch own retained changes.

Before dispatch, commit the required stage documents, code and tests to that branch. Create/reuse registered temporary worktrees with `runtime worktree-create --root <authority-root> --assignment-id <id>`. Git worktrees share Git objects, but have separate indexes and working files: staged and dirty parent changes are absent. Native Claude Code base-ref defaults must be checked against the binding.

Workers commit scoped results and report. Main reviews them and runs `runtime task-integrate --root <authority-root> --assignment-id <id>`: normal merge commit, verification, acknowledgement, cleanup. This is development integration, not release. Failed integration preserves the checkout; retry the same assignment. Do not discard unreceived work or create replacement worktrees indefinitely.

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
