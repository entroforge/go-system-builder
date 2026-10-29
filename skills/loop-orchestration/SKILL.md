---
name: loop-orchestration
description: Use when starting, resuming, recovering, or advancing an active Engineering Loop
category: methodology
version: 2.2.0
---
# Loop Orchestration (Main-session Driver)

## Native S6 delivery (s6-native-v1)

For a newly prepared native S6 TASK, Main uses `s6 prepare --task <TASK> --role <role> --root <Main>` and carries the exact returned task brief into Claude Code's native Agent prompt. Claude Code owns creation, foreground/background execution, messaging and resume. No Team, opaque platform ID, workspace prepare/launch, team launch, generic readback, begin or activation is required.

The Builder reads the brief and actual TASK inputs, saves its professional plan, continues implementation and actual self-checks, commits deliverables and saves the professional Result. Ordinary saves do not register an execution lifecycle. Return the exact task brief / delivery path. SubagentStart supplies bounded engineering guidance; SubagentStop receives actual professional artifacts through the original domain Writer. Main integrates separately and the original full-batch Gate remains. A real blocker may be handed back; never fabricate PASS or approval.

This S6 section applies to `s6-native-v1`. Existing frozen workspace/legacy assignments retain their recorded recovery behavior. The native S5/S7/S8/S9 paths described below use their own original consumers; legacy procedures are not prerequisites for new native work.


## Native S9 dispatch

Identify the current responsibility from its exact original brief or Targeted
request. Follow [agent-dispatch](../agent-dispatch/SKILL.md#native-s9) for native handoff and [S9 protocol](../../docs/control/agent-protocol.md#s9)
for completion and recovery. Builders own private PlanReport/Result and real
self-checks; an independent verifier, including test-builder in that role, owns
only Targeted observations and conclusions. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. Reception does not wait for a final message or returned reference. Main owns actual
dispatch, integration/checks, Impact and the fresh S7 handoff. First incomplete
returns preserve rejection and exact gaps for Main without manufacturing completion.
Existing registered frozen execution keeps its original sources and recovery.

## S7 clean-round completion

Main organizes complete-round module and affected-surface coverage; Reviewers
perform only assigned Claims/CASEs through the actual browser, API, command or
state-based method. Final valid Result reception forms CleanRound and the same
original completion outlet evaluates the gate/TR-009 and prepares the exact S10
acceptance questionnaire. Report reception, transition and preparation separately.
For a concrete exit failure, `loop-harness runtime review-continue --root <Main>`
rechecks the current clean-round exit. After entry into S10,
`loop-harness runtime audit-prepare --root <Main>` restores missing current
materials while preserving accepted results and existing reports. No next ordinary tool
call, repeated Reviewer answer or copied clean_round binding is required.

## Native S5 / S7 / S8 professional work

Main carries the exact original view and answer references into Claude Code native Agent. Read its actual frozen subjects, perform only your independent professional responsibility and save the original answer. For S5/S8, return the original reference for SubagentStop reception. For S7, PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. S5/S8 ordinary tools do not activate or receive work. Ordinary tools need no receipt. No Team, opaque platform ID, workspace prepare/launch, generic readback/PLAN or begin is required for new work. Main retains professional division, ReviewPlan waves, Case routing and stage closure; real approval modes and actual independence remain. Frozen registered workspaces keep their original sources and recovery; never fall back to Main for a lost frozen execution. S5 checks the independent document responsibility; S7 answers its Claims/lens; S8 investigates its Hypothesis without product writes.

For S8, follow the current Main intake → hypotheses/actual dispatch → causal
route → Contract → actual decision questionnaires. Put experiment-specific scope,
stopping and unresolved conditions in `discriminator`; predictions belong in
`expected_outcomes`. Main owns synthesis and routing, while Investigators own
their results. One active Case preserves its source set; mixed mechanisms do not
authorize arbitrary Case split/merge. An incomplete first native return hands
work to Main without accepting it. Restore sources or resume the author from the
exact reference and diagnostic. Contract approval follows its actual authority:
a designated Driver may be Main; an undesignated Main cannot substitute its own
review. Read [S8 protocol](../../docs/control/agent-protocol.md#s8) for exact shapes
and [bounded authority](../../docs/bounded-repair-autonomy.md) when applicable.

## Authority

This Skill is the operational core of **Layer 1** (the Main-session Driver).
It is advisory: it shapes how the main session decides the next action. It is
**not** authoritative on legality or current facts.

Workers use their own Assignment's scope, required checks and closing contract.
They return their actual artifacts for original reception and end their own
responsibility when its current receipt says it is complete. Main applies
DRIVE to integration, peer results and full stage coverage; global missing work
does not extend a Worker's assignment. A concrete risk that blocks the Worker's
acceptance uses the original BLOCKER route with evidence and impact. Recording
a report does not prove required checks passed or bypass independent review.

Loop responsibility progress uses the five statuses in
[`docs/rules/state-machine.md`](../../docs/rules/state-machine.md#21-loop-responsibility-status).
Stage gates, document/package status, professional outcomes, Runtime/CLI wire
values, and host session events remain separate facts; use their exact current
fields without treating `complete`, `ready`, or `consumed` as synonyms for
responsibility `done`.

| Concern | Authority |
|:---|:---|
| what is currently true | `.claude/loop-state.json` |
| what transition is legal | `docs/control/loop-definition.json` |
| what the route looks like | `docs/control/agent-protocol.md` (S0–S11 stage contracts) |
| what the next action is | this Skill, computed from the above |

## What "work-first" means

The Driver never treats missing evidence, a missing deliverable, or an
ordinary multi-option technical choice as a human blocker. The next action is
always the most-forward missing piece of work the current stage requires.

| Situation | Wrong response (transition-first) | Right response (work-first) |
|:---|:---|:---|
| stage `done_when` predicate is false | "no transition qualifies, surface blocker" | produce the missing deliverable / evidence |
| several compliant implementations exist | "surface A/B/C menu to user" | pick the lowest-risk reversible one per the autonomous decision rules and continue |
| Hook reminder names missing work | "ask the human" | produce the named work and let the original consumer/gate evaluate it |
| a responsibility attempt becomes `done` | "wait for next instruction" | inventory the stage, pick the next missing piece, continue DRIVE() |
| transition requires evidence | "wait for evidence to appear" | do the work that produces the evidence |

## Entry Conditions

- Use the current supplied responsibility and exact questionnaire or brief.
- S0 and unbound S1 are valid work: they do not require an already bound REQ.
- Original preparation and domain consumers own source digests, bindings and
  Runtime validation. Report their concrete preparation errors; do not compute,
  copy or script machine metadata as an opening ritual.

If any entry condition fails, see "Stop conditions" below.

## Required Inputs

Start with the current supplied responsibility and its actual materials. This
is not a requirement to inspect the full Runtime or run diagnostic commands.

| Input | Path / source | Why |
|:---|:---|:---|
| Hook context | PreToolUse / SessionStart / Stop reminder | current progress, exact questionnaire path, owner-specific missing work and next action |
| Stage contract | `docs/control/agent-protocol.md#<current stage>` | the stage's `done_when`, `actions`, `failure_route` |
| Actual requirement | the path supplied with this responsibility | understand current commitments; source verification belongs to the original consumer |
| Existing receipts and evidence | the references supplied for this responsibility | understand actual accepted work and remaining owners; no duplicate evidence registration |

### Optional diagnostics

| Tool | When it helps | Boundary |
|:---|:---|:---|
| `loop-harness status --root .` | current stage is genuinely unknown | coarse projection, not live gate `missing[]` |
| `loop-harness next --root .` | the current responsibility needs routing diagnosis | coarse next-work projection, not an opening prerequisite |
| `loop-harness ready --root .` | a concrete gate checklist is unclear | read-only diagnostics; never hand-push a Transition from it |

If Hook context or `loop-harness ready` names missing work,
the named work **is** the next action — that is not a tool blocker.
`status`/`next` are coarse projections only; live checklist authority is the
original stage contract and domain gate, with Hook context as a reminder.

## Procedure

Run this on session start, wake-up, subagent return, lifecycle reminder, or
meaningful tool result.

```text
DRIVE()
1. Start from the current supplied responsibility.
   - Use its exact input and output references and original feedback.
   - Only an actual Runtime recovery diagnostic calls for its named recovery;
     do not reconcile, rehash the REQ or scan the journal before ordinary work.

2. Resolve the current stage.
   - Use the Hook's current stage (S0–S11); inspect a coarse status only if the
     current stage is genuinely unknown.
   - Read docs/control/agent-protocol.md#<stage> for done_when and actions.

3. Inventory responsibility attempts by `pending` / `active` / `blocked` /
   `done` / `cancelled`; separately inventory stage deliverables and valid
   evidence from Hook context, Runtime, and artifacts.
   - Use the Hook's current owner-specific reminder and exact answer path.
     When the checklist is unclear, run `loop-harness ready --root .`
     (diagnostics only).
   - Treat `status`/`next` as coarse projections, not live gate checklist.
   - Treat invalid evidence as missing; treat superseded evidence as not
     usable for this round.

4. Pick the next action.
   - If an original explicit approval is pending, follow that real approval
     path; native execution does not grant it. A frozen legacy Agent follows
     its documented recovery; its lifecycle values are not new native prerequisites.
   - If the stage's `done_when` is not satisfied:
     the next action = produce the most-forward missing deliverable or
     evidence named by the Hook reminder or `ready` diagnostics.
   - Else:
     the next action = produce nothing extra; let the existing stage gate
     and graph transition flow report or apply the legal next stage.
     Do not bypass the original gate with a manual transition.

5. Load the minimum context.
   - The current stage's direct upstream specs.
   - Exactly one primary Methodology Skill — the one named by
     Hook/`next.primary_skill`.
   - Best-practice Skills when the responsibility's risk tags or its actual
     TASK/inputs meet the role's stated conditions; read the supplied paths.

6. Execute the action.
   - Self-execute, or
   - Use native Agent with one concrete professional responsibility and exact
     original references. Use `team-planning` for coverage and independence
     when multiple responsibilities must be covered.
   - After spawning a subagent assignment, do not self-execute that
     assignment's deliverable. Main follows actual native progress, return and original consumer facts;
     no generic activation handshake precedes execution.

7. Verify and record.
   - Verify the artifact against the stage's done_when.
   - Save the original professional deliverable and reference real observations.
     The original consumer creates its necessary evidence envelope, fingerprints
     and receipt; do not hand-assemble another machine artifact.

8. Let Hook advance when the stage is complete.
   - When the original `done_when` and domain gate are satisfied, let the
     existing consumer and graph transition path report or apply the legal
     advance. Do not hand-push `loop-harness runtime transition`.

9. Loop.
   - Continue from the latest reception or progress feedback. Use `ready` only
     for a concrete diagnostic gap; unchanged guidance needs no repeated read.
     Continue until a real stop condition.
```

## Native responsibility handoff

Main passes the original exact inputs and output references into native Agent. The Worker performs its professional work and saves its original artifacts; Start/Stop connect them to the original consumer. Main follows actual native return, continues unrelated ready work, and handles integration or blockers. Do not create a framework wait/activation queue, infer a task from role, or self-author delegated independent judgments.

Professional S6/S9 Builder plans precede implementation but do not require a generic readback/activation exchange. S5/S7/S8 read-only responsibility uses its domain questionnaire directly. A missing artifact is a concrete handoff for that responsibility, not a human approval request or a reason to re-wake an invented teammate. Use native messaging/resume with exact references when more work is needed. A returned actor ID may differ after resume; keep the same original engineering responsibility.

Real explicit approval remains in its original domain before applicable work. A frozen pre-migration teammate/execution alone uses [agent-dispatch's frozen recovery](../agent-dispatch/frozen-execution.md); do not copy that procedure into new native work. Main never reports a delegated independent conclusion on the reviewer's behalf.

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
it matches `docs/control/agent-protocol.md`.

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

For a packet-enabled Runtime in `planning.design`, original responsibility
preparation supplies Main's exact S2 view and answer paths. Complete the design
using `specification-planning` and commit formal upstream inputs to the bound
development branch before saving pass. Fill only the original answer's conclusion
and architecture path; normal Main save uses the original evidence consumer and
design gate. Reception, PTR-PLAN-01 and preparation of S3 inputs are separate
outcomes. Follow the precise missing work or original continuation in feedback.
For a concrete technical failure, use
`.claude/bin/loop-harness runtime stage-answer receipt --agent Main --packet <packet_id> --root .`
to diagnose the original receipt, or
`.claude/bin/loop-harness runtime stage-answer recover --agent Main --root .`
for its supported followup; execute the complete recovery command supplied by feedback.
Neither is a routine authoring step.
Keep the original human amendment for unknown UI impact and later independent
review duties. A design receipt does not authorize ending the project.

For packet-enabled Main at `planning.contracts`, use the current S3 answer
delivered by the Hook. It provides the exact view and answer paths, along with
known or candidate output paths and templates. Do not submit it while the
cursor is still `planning.design`, or copy S2's `architecture_path` or UI
impact into it. Fill only the existing answer fields: `conclusion=pass`, the
actual index path, all applicable contract paths for the bound REQ, and an
explicit applicability decision and rationale for FE, BE and SYNC. A
`required` area needs its contract; `not_applicable` needs a reason grounded in
the REQ and registered S2 design. One answer covers one bound REQ index and
its complete contract set. Historical and other REQ documents do not need to
be repeated and cannot fill coverage for this REQ.

Issuance freezes the locked REQ, registered S2 design inputs and conditional
sources those inputs reference. It does not use the S3 index or contracts as
their own issuance prerequisite. S3-created shared models and examples are
outputs. On save, the original shared-model checker finds their full dependency
closure and records it in the existing evidence subjects. S3 may create or
refine new models within the established design; changing frozen S2 content or
source membership requires formal design rework. REQ semantic changes retain
the original human amendment boundary. For outputs declared `git_tree`, first commit completed professional files to
the bound development branch named by the Hook. S3 outputs can change in this
entry. Save the supplied answer once; correct an unreceived answer at its original path.
No manual issue/consume or parallel evidence envelope is needed. Progress feedback gives
the exact answer path, concrete missing work and next action. Receipt/recovery
commands are for diagnosing or resuming an explicitly pending system followup.

The original consumer and gate run the contract/source checks and PTR-PLAN-02
registers the current locked contract set when its full gate passes. A Main
`pass` is not a gate result. `contracts check` and `scenario bridge` are
available for autonomous professional self-review and diagnosis; they are not
mandatory duplicate submission checks and do not replace the consumer or gate. Only an actual REQ semantic change or an unresolved decision outside existing
authorization uses its original human gateway; ordinary compatibility and
breaking-risk analysis is S3 professional work within the approved requirement, and S5's independent review remains separate. Same-content replay
returns the original receipt; different content conflicts. The command/Hook/
receipt/recovery mechanism has directed verification; live model consumption
remains separate.

For S3/S4, tool access uses the exact supplied absolute filesystem paths;
answer fields `index_path`, `contract_paths` and `task_paths` contain
repository-relative values. For example, read/write the same contracts file
under its supplied absolute path with a tool, but put its repository-relative
path (`docs/dev/contracts/CONTRACTS-{id}.md`) in `index_path`. Save the
questionnaire at its exact supplied absolute answer path. Markdown links remain
relative to the containing document; do not convert them to answer-field
root-relative paths.

Professional S3/S4 rework is separate from technical continuation. Within the
same bound REQ, use
`.claude/bin/loop-harness runtime stage-answer rework --target design --reason "<concrete design correction>" --root . --agent Main`
when frozen S2 inputs must change. Use
`.claude/bin/loop-harness runtime stage-answer rework --target contracts --reason "<concrete contract correction>" --root . --agent Main`
when accepted contracts must change while S2 stays valid. While S4 is still current, use
`.claude/bin/loop-harness runtime stage-answer rework --target tasks --reason "<concrete TASK/index correction>" --root . --agent Main`
when accepted TASKs or the dispatch plan need professional correction and the
frozen REQ/S2/S3 inputs remain valid. Keep correct upstream work; a tasks return
does not refresh drifted upstream sources. After entry into S5, follow the
independent review and original document-rework route. These explicit routes
preserve the accepted history, invalidate affected downstream planning, and
supply the new current view/answer paths. If preparation fails after the
return is recorded, follow the supplied `runtime stage-answer issue` command.
No packet/entry ID or authored evidence envelope is required. Current
unreceived outputs are ordinary professional corrections, followed by required
Git delivery and saving the original answer, even if marked `locked`.
`issue` repairs missing issuance/preparation; `recover` resumes an accepted
pending outlet. Neither changes an accepted professional judgment or authorizes
editing frozen sources. `revise` is not an S3/S4 professional rework route.
REQ semantic changes retain the actual human amendment boundary; ordinary
contract gaps and compatibility choices within authorization remain professional work.
Any questionnaire reminder about human decisions means an actual unresolved
REQ amendment or decision outside existing authorization. It does not require
a fresh approval for every ADR, compatibility choice, N/A rationale or S3/S4 run;
reuse applicable existing decisions.

In `planning.tasks`, use the Hook's exact view and answer paths, current REQ
index path, TASK candidate paths and templates, frozen upstream inputs, bound
Git branch, missing items, and next action. Use numeric TASK IDs such as
`TASK-009` or `TASK-009-01`. Main decides the TASK IDs and
professional breakdown, completes the TASK/index outputs, marks TASK document
statuses `complete` or `cancelled`, and commits Git-sourced outputs to the named
branch. Keep the supplied REQ/S3 source closure frozen; do not replace a missing
or stale registered source with a live workspace file. Fill the existing
answer's `conclusion`, `index_path`, and complete `task_paths` once, then save
it. The original consumer performs `tasks check`,
projects planning evidence, and continues through its gate and `TR-002`. Use
check output for autonomous self-review, diagnosis or professional judgment;
do not add mandatory duplicate checks, envelopes or issue/consume procedures.
TASK document statuses remain separate from the shared work states. For S3 or
S4 accepted work with pending gate followup, repair the named gap and execute
the complete original `runtime stage-answer recover` command supplied by
feedback, without refilling or resaving the accepted answer. If transition has already committed but next-stage preparation failed, repair
its named source gap and execute the complete original `runtime stage-answer issue` command supplied by feedback; `recover` continues a pending gate outlet.
Ordinary reads or
arbitrary material saves do not promise automatic recovery. Before reception,
complete required Git delivery and save the original answer normally. Explicit packet-off legacy Runtime flows retain their legacy procedure.

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
  every other required responsibility has a current `done` attempt, or its
  original cancellation route confirms no replacement is required →
  `missing_external_permission` Gateway.
- The original workflow reaches a real human decision boundary, such as a
  required REQ amendment or release approval → form the matching Gateway.
  Hook reminders do not create that boundary or make a tool-permission
  decision.

A Gateway package must contain: type, `completed work` (a concise evidence list
of work already accepted or otherwise available, not a responsibility status),
the single fact the main session cannot resolve, impact scope, recommended
decision, and the stage to resume from after the human acts.

## Non-stop conditions (do not stop here)

The following are **not** stop conditions:

- Missing evidence (produce the named work and let the original stage gate
  evaluate it).
- A missing item in Hook progress context (produce the work and follow the
  original stage gate; the reminder is not a tool-permission decision).
- An unclear or stale projection (use its original Runtime recovery path).
- A failing test (route through `bug-resolution`).
- A `done` responsibility attempt (continue DRIVE at step 9; this does not
  make its stage or another owner's responsibility `done`).
- Several compliant technical implementations (autonomous decision).
- A `MISSING_DELIVERABLE` projection (the named missing work is the action).
- A reminder about activation, scope, Team, UI prototype, clean round,
  subagent report, or teammate idle staleness: use it as forward scheduling
  guidance and follow the relevant original assignment or stage gate.
- A `reader-bias verify-loop` — the imperative read/verify/recover/re-load
  steps (this Skill §"Procedure" DRIVE() steps 1–5; loop-template.md
  §Wake-up steps 1–6) are run reflexively even though prior-turn context
  already names the Hook reminder / `ready` diagnostics and its `primary_skill`. When the bound REQ,
  current stage, and next action are already established in this
  conversation history, **skip straight to DRIVE() step 6 (Execute the
  action)** — do not re-run the load sequence as a precondition gate.
  State-fresh execution is the default; the load sequence is the
  state-unknown fallback.

## Hook Context Processing

Hook events provide graph engineering context: they listen for progress,
remind the responsible owner of the current questionnaire and next work, and
report the graph's stage flow. PreToolUse does not authenticate the caller,
grant path-specific tool access, or deny ordinary tool calls. Its context is
not a professional acceptance or a substitute for the original stage gate.

| Hook context | Driver action |
|:---|:---|
| Current questionnaire or exact answer path | Read the published requirements, fill the named answer, and save it through the existing answer path. Do not search Runtime or infer a path. |
| Missing item or unsatisfied original gate | Produce the named work and evidence; let the original domain consumer and gate decide acceptance. |
| Accepted answer with system followup pending | Do not submit the answer again; continue the original recovery/followup path. |
| Stop/idle reminder for this owner's incomplete responsibility | Finish or report that responsibility, using its delivered answer/result path. Other owners' missing work does not reopen a completed responsibility. |
| Stale or unreadable Runtime context | Follow the original Runtime recovery or human gateway when the source authority is genuinely unrecoverable. |

Main's answer save and each stage's native reception route use their original
domain consumer, evidence and binding checks, and Writer/CAS. S7/S9 use
PreToolUse reception of ready original answers, with successful PostToolUse and
Stop/SubagentStop fallback; other stages retain their original routes. Only those original consumers produce acceptance facts;
the Hook reminder itself does not register evidence, approve a professional
judgment, or advance a stage. Stage transitions continue through the existing
graph and transition path after their original prerequisites hold.

Main performs the original integration and required checks at their real stage.
This Skill and Hook do not publish, deploy or execute a formal release.
A real release or REQ amendment still requires its original human
decision and domain gate; a Hook event does not enforce those boundaries by
per-tool denial.

### What this Skill does NOT do

- It does **not** decide tool permissions or invoke policy checks.
- It does **not** modify `.claude/loop-state.json` directly — the Harness
  transition engine is the sole writer.
- It does **not** treat a Hook reminder or saved questionnaire as professional
  acceptance; only the original consumer and stage gate record those facts.
- It does **not** bypass actual human decisions, original domain validation,
  or required evidence by taking a different tool path.

## Non-Goals

## Exit Conditions

- The current turn produced and verified the next required deliverable, or
  left it saved for the original consumer/gate to evaluate; then recomputed
  the next action from current progress context.
- Automation reached a documented Human Gateway or a recoverable pause
  checkpoint.

- The Driver does not implement TASKs itself when a Builder assignment
  exists — it dispatches the original brief through native Agent.
- The Driver does not judge review conclusions — independent Verifier / QA professionals do.
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

## User-visible progress

Hook progress context is a scheduling reminder, not a work report. At the
start of work, after a meaningful deliverable, on a blocker or change of
approach, and approximately every 60 seconds during sustained work, tell the
user briefly what deliverable or evidence is now accepted, what action is
underway, and what comes next. Use the user's language. Do not narrate every
tool call or expose
internal reasoning. Report only observed results: a revision increment, an
intended action, or a repeated Hook message is not evidence of progress.

Hook `systemMessage` is a user notice; model recovery instructions belong in
`hookSpecificOutput.additionalContext` for supported events. Full recovery
context is retained at session/agent start and stage transitions. Ordinary
PreToolUse calls provide the current owner responsibilities, exact questionnaire
path, missing work, and a compact checkpoint. Keep the exact path intact when
summarizing. Repeated unchanged guidance may be shortened within the same
session and responsibility. New errors, changed paths, session recovery and a
Stop reminder for that owner's own missing Result must remain available. A Worker
plan checkpoint requiring no approval does not silence progress reports to the user.

## Technical barriers and progress

A Worker missing its next action does not automatically mean the human must decide. Resolve missing task ownership, dependency mistakes, overlapping writes and reviewer independence through existing planning/assignment paths before escalating. If a change requires amendment or another human decision, follow that original route; ordinary tools remain available while work is prepared. Advisory guidance never manufactures `human_decision` evidence.

When a command response or Hook already supplies a current next_action and the relevant artifact identity, execute it without another full status/read cycle. Refresh on restart, conflicting external updates, stale CAS, missing context or a contradictory checkpoint. Progress evidence is an accepted deliverable, resolved assertion or new discriminating evidence, not Runtime revision churn, file timestamps or tool count. Repeated unchanged failures require diagnosis; normal long-running checks remain legitimate. Use the dispatch Skill's bounded waiting and evidence reuse rules.

## Routine repair authority

Before escalating an ordinary technical RepairContract approval, check the
Runtime's `configuration.repair.bound_policy` and existing current bounded grants.
A policy pinned when the human locked/bound this REQ authorizes the Driver review
path described in docs/bounded-repair-autonomy.md; use the pinned digest rather
than asking for repeated approval. Do not infer, fabricate, extend or auto-renew
policy. Scope/semantic changes outside authority keep their existing Gateway.


## Temporary worktrees and committed stage delivery

Record actual development and release destinations in the original S1 application; neither is guessed from a branch default. The project's authority root and selected development branch own retained changes. Explicit `req bind` remains the original compatibility/recovery API.

Before dispatch, commit the actual required stage deliverables to that branch. New writing responsibilities use Claude Code native worktree isolation from the committed bound Main branch. Only existing frozen registered workspaces retain their original `runtime worktree-create` recovery route. Git worktrees share Git objects but have separate indexes and working files: staged and dirty parent changes are absent. The original delivery consumer checks the real source and binding; no new identity or worktree registration is required from the author.

Workers commit scoped results and report. Main reviews them and runs `runtime task-integrate --root <authority-root> --assignment-id <id>`: normal merge commit, verification, acknowledgement, cleanup. This is development integration, not release. Failed integration preserves the checkout; retry the same assignment. Do not discard unreceived work or create replacement worktrees indefinitely.

Stage gates consume each input from the source declared in the upstream file contract. Formal deliverables use one pinned Git tree; only explicitly allowed evidence/runtime inputs use disk. Dirty files cannot satisfy formal stage delivery. Worktree backlog reminders are advisory, delivered to Main through Agent-visible context, and never authorize deleting unknown worktrees.
