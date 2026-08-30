---
name: loop-orchestration
description: Use when starting, resuming, recovering, or advancing an active Engineering Loop
category: methodology
version: 2.2.0
---
# Loop Orchestration (Main-session Driver)

## Authority

This Skill is the operational core of **Layer 1** (the Main-session Driver).
It is advisory: it shapes how the main session decides the next action. It is
**not** authoritative on legality or current facts.

| Concern | Authority |
|:---|:---|
| what is currently true | `.claude/loop-state.json` |
| what transition is legal | `docs/loop-definition.json` |
| what the route looks like | `docs/agent-protocol.md` (S0–S11 stage contracts) |
| what the next action is | this Skill, computed from the above |

## What "work-first" means

The Driver never treats missing evidence, a missing deliverable, or an
ordinary multi-option technical choice as a human blocker. The next action is
always the most-forward missing piece of work the current stage requires.

| Situation | Wrong response (transition-first) | Right response (work-first) |
|:---|:---|:---|
| stage `done_when` predicate is false | "no transition qualifies, surface blocker" | produce the missing deliverable / evidence |
| several compliant implementations exist | "surface A/B/C menu to user" | pick the lowest-risk reversible one per the autonomous decision rules and continue |
| Hook returns Quality `not_ready` | "ask the human" | read the Recovery Packet missing list, produce the missing work, and let the next Hook re-evaluate |
| subagent finishes one TASK | "wait for next instruction" | inventory the stage, pick the next missing piece, continue DRIVE() |
| transition requires evidence | "wait for evidence to appear" | do the work that produces the evidence |

## Entry Conditions

- A locked REQ exists; its SHA-256 matches the Runtime baseline.
- `.claude/loop-state.json` is readable and schema-valid.
- The Loop Definition under `docs/loop-definition.json` is readable.
- Hook policy health is `healthy`.

If any entry condition fails, see "Stop conditions" below.

## Required Inputs

| Input | Path / source | Why |
|:---|:---|:---|
| Runtime status | `loop-harness status --root .` | coarse stage projection only — not live gate `missing[]` |
| Next projection | `loop-harness next --root .` | coarse missing-work projection — not live gate checklist |
| Ready diagnostics | `loop-harness ready --root .` | dry-run current Quality Gate (`missing[]`); never hand-push Transition from it |
| Hook packet | PreToolUse / SessionStart Recovery Packet | authoritative live `quality_gate.missing` + candidate |
| Stage contract | `docs/agent-protocol.md#<current stage>` | the stage's `done_when`, `actions`, `failure_route` |
| Bound locked REQ | `runtime.bound_req.path` | verify the locked baseline |
| Evidence map | `runtime.evidence[]` | check validity per stage `done_when` |

If the Hook packet or `loop-harness ready` returns a non-empty `missing[]`,
the named missing work **is** the next action — that is not a blocker.
`status`/`next` are coarse projections only; live checklist authority is the
Hook packet or `ready`.

## Procedure

Run this on every session start, Wake-up, subagent return, Safety
`block`, Quality Gate `not_ready`, or tool result.

```text
DRIVE()
1. Reconcile and read status.
   - If the journal is behind the snapshot, run `runtime reconcile` first.
   - Verify the bound REQ is locked and its SHA-256 matches the Runtime.

2. Resolve the current stage.
   - Read the Runtime's Main Spine stage (S0–S11).
   - Read docs/agent-protocol.md#<stage> for done_when and actions.

3. Inventory completed and open work for this stage.
   - Prefer the Hook packet `quality_gate.missing[]`. When unclear, run
     `loop-harness ready --root .` (diagnostics only).
   - Treat `status`/`next` as coarse projections, not live gate checklist.
   - Treat invalid evidence as missing; treat superseded evidence as not
     usable for this round.

4. Pick the next action.
   - If any Agent assignment is spawned, reading, or waiting for approval:
     the next action = complete the Agent Activation Barrier below.
   - If the stage's done_when is incomplete:
     the next action = produce the most-forward missing deliverable or
     evidence named by Hook/`ready` missing[].
   - Else:
     the next action = produce nothing extra; the next PreToolUse
     auto-commits the stage advance when the gate is satisfied.
     Do not call `runtime transition`.

5. Load the minimum context.
   - The current stage's direct upstream specs.
   - Exactly one primary Methodology Skill — the one named by
     Hook/`next.primary_skill`.
   - Best-practice Skills only if this action's risk tags trigger them.

6. Execute the action.
   - Self-execute, or
   - Spawn one single-responsibility subagent assignment (phase-one
     read-back, phase-two activation). Use `team-planning` to plan a Team
     when multiple responsibilities must be covered.
   - After spawning a subagent assignment, do not self-execute that
     assignment's deliverable. The current Driver action becomes the Agent
     Activation Barrier until the Agent is activated, rejected, or revoked.

7. Verify and record.
   - Verify the artifact against the stage's done_when.
   - Write the deliverable and the evidence; the Harness records the
     evidence fingerprint and validity.

8. Let Hook advance when the stage is complete.
   - When done_when is fully true / gate is satisfied, do not hand-push
     `loop-harness runtime transition`. The next PreToolUse auto-commits
     at most one allowlisted Transition.

9. Loop.
   - Re-read Hook packet (or `ready` if checklist unclear) and recompute.
     Continue until a real stop condition.
```

## Agent Dispatch Barrier (plan_checkpoint)

Claude Code Agent Teams allow the same subagent to be woken repeatedly. The
Driver therefore treats dispatch as a scheduling barrier, not as a passive
wait. Once the main session delegates an assignment, the most-forward missing
work is that Agent's plan checkpoint until the plan is recorded — then the
Worker executes continuously in the same turn (L4: no second approval gate in
the default `plan_checkpoint` mode).

Apply this barrier before self-executing any stage work:

| Agent lifecycle fact | Driver next action |
|:---|:---|
| assignment dispatched but no plan checkpoint | load `agent-dispatch`; the Worker sends PLAN_REPORT via SendMessage (`message_type=plan_report`) — the PostToolUse(SendMessage) observer records the checkpoint; the first product write is blocked until then |
| PLAN_REPORT sent and aligned | stay silent — the Worker continues executing in the same turn; never reply with a "go ahead" approval |
| PLAN_REPORT drifted (scope/oracle/dependency) | send one CORRECTION to the same Agent; it continues the current assignment |
| plan recorded, no Result yet | the Worker keeps executing; TeammateIdle/SubagentStop before the Result is blocked (exit 2) by the Hook |
| Result registered (`reported`) | the Agent may idle; consume the Result — never let an idle teammate self-claim the next task |
| Agent blocked with a valid blocker | idle is allowed; consume the blocker and repair the condition |
| Agent unrecoverable | re-dispatch a replacement from the shared Assignment + plan checkpoint; never create a second runtime truth |

Rules:

- Work-first does not mean main-first. If an assignment has been delegated,
  the missing work is the plan checkpoint and the Result, not the delegated
  deliverable itself.
- The main session must not implement, verify, or report the delegated
  responsibility while its Agent holds the assignment. To take the work back,
  first revoke the assignment or record an explicit reassignment.
- A missing PLAN_REPORT is not a Human Gateway. Re-wake the Agent, collect
  the plan, then continue DRIVE.
- `plan_approval_required` is reserved for high-risk destructive work; only
  that mode waits for an explicit approval before mutation.

## Autonomous decision rules (apply at DRIVE step 4 and 6)

When a technical question is not already answered upstream, decide and
continue. Do **not** surface A/B/C menus to the user.

Priority, highest first:

```text
locked REQ
> approved design and UI design package
> FE / BE / SYNC contracts
> project rules and existing architecture
> baseline and established conventions
> lowest-risk reversible implementation that fully satisfies the REQ
```

- A question already answered upstream is not a question.
- Workload size is not a reason to reduce scope.
- Missing non-semantic information → pick a reversible default, record the
  assumption, continue.
- Multiple compliant implementations → pick the lowest-risk reversible one,
  record the assumption, continue.
- Before raising any Human Gateway, complete every part of the work that
  does not depend on the blocked decision.

## Methodology routing

The `next.primary_skill` field names the primary Skill for the current
action. The table below is the stable routing used by the Harness projection;
it matches `docs/agent-protocol.md`.

| Main Spine stage / situation | Primary Skill |
|:---|:---|
| S1 initialize, recovery, pause, resume | `loop-orchestration` (this Skill) |
| S2 design, S3 contracts, S4 tasks | `specification-planning` |
| S5 document_verification | `document-verification` |
| any subagent dispatched (plan_checkpoint) | `agent-dispatch` |
| Team creation / reconstruction | `team-planning` |
| S8 finding_investigation; S9 bug_resolution.* | `bug-resolution` |
| any committed change requiring evidence recalculation | `impact-analysis` |
| verification.clean_round_evaluation | `clean-round-evaluation` |
| S10 acceptance, S11 release_gateway | `acceptance-and-handoff` |

`primary_skill` is the only method-naming field. Legacy `method` is rejected.
Unknown Skill IDs are rejected.

## Outputs

A single turn of DRIVE produces one of:

- An executed action with a written deliverable and evidence (the normal
  case), plus an optional stage-advance transition request when `done_when`
  flipped to true.
- A committed stage-advance transition when the prior turn completed the
  stage.
- A Human Gateway package (only at the stop conditions below).
- A pause checkpoint (only at the stop conditions below).

## Stop Conditions

Stop immediately and surface to the human if **any** of:

- The bound REQ is unlocked or its SHA-256 no longer matches the Runtime
  baseline → `req_amendment` Gateway.
- Runtime integrity check fails (schema invalid, journal behind snapshot,
  unrecoverable reconcile) → `runtime_integrity` Gateway.
- A transition requires human approval (REQ amendment, release, abort) →
  matching Gateway.
- The stage has reached S11 → `release_ready` Gateway.
- Business semantics cannot be derived from any upstream baseline →
  `unrecoverable_business_decision` Gateway.
- An external permission the main session cannot obtain is required, and
  every other work item is complete → `missing_external_permission` Gateway.
- A Hook HS-* `block` decision arrives (`HOOK_LOCKED_ARTIFACT_WRITE`) and
  the required new generation needs human amendment authority → form the
  matching Gateway; the human is the only path forward. A
  `HOOK_SQUASH_MERGE` block is not a Gateway — replace it with a normal
  merge and continue.

A Gateway package must contain: type, completed work, the single fact the
main session cannot resolve, impact scope, recommended decision, and the
stage to resume from after the human acts.

## Non-stop conditions (do not stop here)

The following are **not** stop conditions:

- Missing evidence (produce it; the Quality Gate `not_ready` missing list
  names it).
- A Quality Gate `not_ready` outcome (the original tool already proceeded;
  drive forward by producing the missing work and let the next Hook
  re-evaluate — no tool denial, no human Gateway).
- A Quality `unknown` outcome with a stable `LOOP_*` code (follow the
  code's recovery once and continue).
- A failing test (route through `bug-resolution`).
- A finished TASK or subagent (continue DRIVE at step 9).
- Several compliant technical implementations (autonomous decision).
- A `MISSING_DELIVERABLE` projection (the named missing work is the action).
- A Recovery Packet from any other concern that used to be a Hook rule
  (activation, scope, Team, UI prototype, clean round, subagent report,
  teammate idle staleness): these flow as forward scheduling guidance, not
  as PreToolUse denials.
- A `reader-bias verify-loop` — the imperative read/verify/recover/re-load
  steps (this Skill §"Procedure" DRIVE() steps 1–5; loop-template.md
  §Wake-up steps 1–6) are run reflexively even though prior-turn context
  already names Hook/`ready` `missing[]` + its `primary_skill`. When the bound REQ,
  current stage, and next action are already established in this
  conversation history, **skip straight to DRIVE() step 6 (Execute the
  action)** — do not re-run the load sequence as a precondition gate.
  State-fresh execution is the default; the load sequence is the
  state-unknown fallback.

## Hook Decision Processing (REQ-039 three-layer model)

Hook decisions arrive after every PreToolUse (and on the lifecycle events
listed below). They are inputs to DRIVE; they do **not** advance the Main
Spine on their own. The Driver reads the `quality_gate.status` and
`safety.decision` fields and dispatches by effect:

| Family | Classification | Examples | Driver path |
|:---|:---|:---|:---|
| Quality Gate (forward scheduling) | `quality_gate.not_ready` (or `satisfied` / `advanced` / `unknown`) | missing contract traceability, missing clean-round evidence, missing UI prototype | read Recovery Packet; original tool already proceeded; produce the named missing work and let next PreToolUse re-evaluate |
| Safety (minimal policy) | `safety.block` | exact-path locked artifact write, squash merge | block current tool; surface rework / new-version / normal-merge path; no retry loop |
| Lifecycle write | `transition_committed` / `transition_rejected` | S2→S3 advance, CAS retry | refresh Milestone if committed; on `transition_rejected` follow the stable `LOOP_*` error code |

There is no longer a recoverable `deny` family, no `warn` family, no
`HW-*` / `HA-*` taxonomy. Concerns that used to be Hook rules now flow as
follows:

| Removed concern | Where it lives now |
|:---|:---|
| Agent plan checkpoint / dispatch | `agent-dispatch` Skill + Assignment lifecycle events |
| Scope / permission expansion | Activation envelope revision, never a PreToolUse block |
| Team required for subagent | `team-planning` Skill + SubagentStart guidance |
| Policy tamper / self-evolution | Capability-controlled assignment, not a runtime rule |
| UI prototype / clean round | Quality Gate preconditions, surfaced as `missing[]` |
| Subagent report incomplete / teammate idle stale | SubagentStop / TeammateIdle guidance |
| Runtime integrity | `LOOP_RUNTIME_INVALID` recovery code, no tool block |

Activation, scope, Team, UI prototype, clean round, subagent report and idle
staleness are **not** permission gates any more; they are forward scheduling
guidance surfaced via the Quality Gate, Transition Guard or Recovery
Packet. Quality Gate `not_ready` permits the original tool action and lists
the missing work the Agent should produce next; only Safety `block` (locked
artifact write or squash merge) may reject a tool call.

This Skill describes how to process a Hook decision. It does **not** call
Hook policy, write runtime, or set `runtime.lifecycle.state` — those are
Hook and Harness responsibilities respectively. The Hook Adapter is
stateless: `allow` permits the action, Quality `not_ready` permits the action
and surfaces the missing list, and Safety `block` is reserved for the locked
baseline and squash merge. `docs/hook-policy.json` defines the minimal
Safety rule set; the surrounding policy shape is owned by BUG-039-01 and is
no longer describing activation / scope / quality gates.

### Decision type → Driver action

| Hook outcome | PreToolUse protocol payload | Driver action | Retry? | Surfaces? |
|:---|:---|:---|:---|:---|
| `allow` | (no override) | continue DRIVE normally | n/a | n/a |
| Quality `not_ready` | `permissionDecision="allow"` + `quality_gate.missing[]` | read Recovery Packet missing list, drive forward, re-evaluate on next Hook | n/a | no |
| Quality `satisfied` / `advanced` | `permissionDecision="allow"` + transition result | continue DRIVE; refresh Milestone if `advanced` | n/a | no |
| Quality `unknown` | `permissionDecision="allow"` + recovery code | follow the stable `LOOP_*` error code once, then continue | per code | no |
| Safety `block` (locked artifact / squash merge) | `permissionDecision="deny"` + `permissionDecisionReason` | do **not** retry in a loop; produce new generation / amendment / normal-merge path | **no** | yes — rework or documented Human Gateway |

### Recovery Packet processing path

For each Quality Gate `not_ready` the Hook emits a Recovery Packet that the
Agent consumes as a human-readable plan. The original tool action has
already proceeded; nothing is in a denial state.

1. Read `quality_gate.gate_id`, `quality_gate.missing[]`, and
   `quality_gate.candidate_transition`. The Agent treats the missing list
   as the next forward work, not as a blocker.
2. Confirm `safety.decision=allow` in the same packet. Quality Gate outcomes
   are not tool permissions; ordinary Write / Edit / Bash / Task / Agent
   calls continue even when `not_ready` is returned.
3. Drive forward: execute the next tool calls that produce each missing
   item. The Quality Gate re-evaluates on the next PreToolUse using the
   stable `fingerprint`; no extra CLI invocation is required.
4. When `transition_committed` arrives (e.g. design → contracts), refresh
   the local view of `quality_gate.next_cursor` and continue DRIVE.
5. If `transition_rejected` arrives, follow the stable `LOOP_*` error code
   guidance (e.g. `LOOP_CAS_STALE` → one immediate re-read; `LOOP_GATE_UNKNOWN`
   → wait for next event). Never retry blindly.
6. A repeated `not_ready` for the same `fingerprint` is a no-op
   (idempotency), not a Human Gateway.

Activation / scope / Team / UI prototype / clean round / subagent report /
teammate idle concerns are **not** part of this packet. They live in the
`agent-dispatch` Skill, the team-manifest envelope, the
SubagentStart / TeammateIdle Guidance, or the stage's `done_when` predicate
— never as a Quality Gate denial. The Driver pulls them forward as normal
scheduling work, not as a tool block.

Safety `block` decisions do not enter this recovery loop. The tool that was
blocked must be replaced with the new-generation, amendment, or normal-merge
equivalent. The Driver surfaces the rework path or hands off to the
documented Human Gateway package when the path requires human authority.

### HS-* processing path

For each HS-* decision:

1. Read `rule_id` and `reason`. Record in `.claude/hook-decisions.jsonl`
   correlation context.
2. **Do not retry the same call.** The Adapter has already denied the tool
   call at the protocol layer. Read the rule's own `human_required` and
   `retry` fields rather than assuming — `HOOK_LOCKED_ARTIFACT_WRITE` is
   `human_required=true`, `retry="never"`, while `HOOK_SQUASH_MERGE`
   is `human_required=false`, `retry="rerun after recovery validation"`.
3. Take the rule's recovery path:
   - `HOOK_LOCKED_ARTIFACT_WRITE` → write a new generation under
     `docs/{kind}/versions/{REQ-ID}/g{N+1}/`. If changing the locked
     baseline needs human amendment authority, form a `req_amendment`
     Gateway.
   - `HOOK_SQUASH_MERGE` → re-issue as a normal merge without `--squash`.
     No Gateway.
4. A Gateway package, when one is required, must contain: type, completed
   work, the single fact the main session cannot resolve (the locked
   artifact needs an amendment the Agent cannot authorize), impact scope,
   recommended decision, and the stage to resume from after the human acts.

Release-shaped operations (`git push <remote> master|main`, `gh pr merge`,
`gh release create`) are **not** Hook block reasons. Hook Policy carries no
rule for them. They are constrained by the stage's release-ready Gateway and
human approval: reaching S11 raises a `release_ready` Gateway, and the human
decides whether release proceeds.

### What this Skill does NOT do

- It does **not** call Hook policy.
- It does **not** invoke `loop-harness` commands to "request" Hook decisions
  or trigger TR-xxx.
- It does **not** modify `.claude/loop-state.json` directly — the Harness
  transition engine is the sole writer.
- It does **not** retry a Safety `block`; severity comes from the Safety
  decision itself, not retry count.
- It does **not** treat Quality `not_ready` as a Human Gateway; the Agent
  produces the missing work forward and lets the next Hook re-evaluate.
- It does **not** block ordinary tool calls on activation / scope / Team /
  UI prototype / clean round concerns; those flow as forward scheduling
  guidance, not as PreToolUse denials.

## Non-Goals

## Exit Conditions

- The current turn produced and verified the next required deliverable, or
  left evidence that lets the next PreToolUse auto-commit a legal stage
  advance, then recomputed the next action.
- Automation reached a documented Human Gateway or a recoverable pause
  checkpoint.

- The Driver does not implement TASKs itself when a Builder assignment
  exists — it activates a subagent.
- The Driver does not judge review conclusions — Verifier / QA Teams do.
- The Driver does not edit `.claude/loop-state.json` directly — the Harness
  transition engine is the sole writer.
- The Driver does not perform squash merge, publication, deployment, or
  formal release.
- The Driver does not lock, modify, or amend the bound REQ.
- The Driver does not start or stop Claude `/loop` schedules — that is a
  separate lifetime.

## Idempotency

Re-running DRIVE must not:

- create a TASK, Team, BUG, assignment, or evidence entity that already
  exists in the Runtime;
- re-execute an action whose evidence is already valid and current;
- skip an action whose evidence is missing, stale, or invalidated.

The Runtime `revision` and `entities` arrays are the deduplication source.
Before creating any entity, check the Runtime for an existing match.
