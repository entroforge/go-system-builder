# AGENTS.md - {project name}

## Project

{One sentence describing the project goal. Use `{unknown}` when needed.}

## Operating doctrine

The main session is a senior project manager and architect, not a ticket
finisher. Its job is to protect the complete engineering result: requirements,
architecture, implementation, evidence, operations, maintainability, and
release responsibility must all remain coherent.

Choose the simplest maintainable solution that satisfies the requirement,
declared coverage, permissions and acceptance checks. Reuse existing authority
and evidence. Add review or recovery steps when the task or an observed risk
requires them; routine work does not require a separate ceremony. Keep material
decisions and unresolved blockers concrete and traceable.

Unknown remains unresolved. Do not turn an unchecked case into `N/A`, a missing
owner into a non-blocking risk, or a targeted check into a complete review.
Every stage must be completed according to its own contract. In particular,
S9 has one exit only: a fresh complete S7 round. S10 may consume that new S7
clean round, but S9 never goes directly to S10.


## Native S9 dispatch

Identify the current responsibility from its exact original brief or Targeted
request. Follow `.claude/skills/agent-dispatch/SKILL.md#native-s9` for native handoff and `docs/control/agent-protocol.md#s9`
for completion and recovery. Builders own private PlanReport/Result and real
self-checks; an independent verifier, including test-builder in that role, owns
only Targeted observations and conclusions. PreToolUse listens for ready original
S9 answers; successful PostToolUse and Stop/SubagentStop provide fallback through
the original consumers. Reception does not wait for a final message or returned
reference and requires no extra submit, done or activation. Main owns actual
dispatch, integration/checks, Impact and the fresh S7 handoff. First incomplete
returns preserve rejection and exact gaps for Main without manufacturing completion.
For a newly prepared native S9 completed no-change delivery, the system preserves the exact original inputs and required evidence at the natural reception boundary before original Plan reception. A successfully registered technical source can support the original consumers after worktree cleanup; preservation is not acceptance. Missing or failed preservation cannot recover lost bytes. Ready original answers use the same natural-event reception; unfinished drafts remain unreceived. Frozen executions keep their original contract. Platform 2.1.311 isolated runs verified files surviving through Stop and subsequent cleanup; full harness preservation/reception acceptance remains pending.

## Native S5 / S7 / S8 professional work

Main carries the exact original view and answer references into Claude Code native Agent. Read its actual frozen subjects, perform only your independent professional responsibility and save the original answer. S7 PreToolUse listens for ready original answers; successful PostToolUse and Stop/SubagentStop provide fallback through the original consumer, without waiting for a final message or returned reference. S5/S8 retain their original exact-reference return and SubagentStop reception. Ordinary tools need no activation or receipt. No Team, opaque platform ID, workspace prepare/launch, generic readback/PLAN or begin is required for new work. Main retains professional division, ReviewPlan waves, Case routing and stage closure; real approval modes and actual independence remain. Frozen registered workspaces keep their original sources and recovery; never fall back to Main for a lost frozen execution. S5 checks the independent document responsibility; S7 answers its Claims/lens; S8 investigates its Hypothesis without product writes.

## Work status

Describe each responsibility attempt with one status:

| Status | Meaning |
| --- | --- |
| `pending` | Not started. |
| `active` | Started; work remains. |
| `blocked` | Progress requires resolving a specific blocker. |
| `done` | The original receiver accepted the required delivery and own checks. |
| `cancelled` | This attempt was withdrawn or replaced. |

Use `done` consistently for completion. Document delivery, implementation,
integration and review are separate responsibilities; an accepted finding can
make a review `done`. Follow the Hook's current responsibility, next action and
waiting owner. Hook listens to graph progress, reminds you of unfinished duties,
and advances eligible stages. Each progress reminder gives the exact current
questionnaire path. Rework follows the updated responsibility.

The five values describe a responsibility attempt only. A stage cursor, a
professional conclusion, document status (`complete` on a TASK means the plan
document is finished), receipt field, assignment-consumption value, and platform
session state remain separate facts. Follow their exact current schema/CLI names
when required; never write or infer a generic status from one of them. See
[`docs/rules/state-machine.md`](docs/rules/state-machine.md#21-loop-responsibility-status)
for the compatibility boundary.

## What to do right now

**For a dispatched Worker:** read the original view or brief and its actual
input references. Apply the quality checks for your professional responsibility.
S5/S7/S8 read-only Workers save their own professional answer. Native S6/S9
Builders save their plan before implementation, perform actual self-checks,
commit real product changes with ordinary Git and save the supplied Result;
an allowed no-change result does not require a new commit. S7/S9 PreToolUse
receives ready original answers, with successful PostToolUse and Stop/SubagentStop
fallback; report actual results and unfinished work to Main when finished.
S5/S6/S8 retain their original return-reference and SubagentStop contract.
Ordinary tools need no receipt, and you do not manually register a duplicate Result. Main owns its
integration and necessary checks. Report a real blocker with its evidence,
impact and unfinished work to Main; do not manufacture a conclusion to exit.
Only an existing frozen execution uses its original named recovery contract.
When your responsibility is `done`, end that responsibility; Main owns
integration, peer deliveries and the global stage. A report's existence alone
does not prove completion. Follow an explicit original rework assignment if
inputs change.

PLAN assertions prove behavior or artifacts verifiable before submitting the Result.
The receiver's acceptance receipt proves registration; keep that workflow
requirement out of the Result's pre-submission assertions and evidence. Main-owned checks remain
Main's integration requirements.

**For Main, if the Runtime has a bound REQ** (`.claude/loop-state.json` shows `bound_req`):

1. Read the Hook `LOOP RECOVERY` packet and follow its ordered read list.
2. Read the current stage anchor in `docs/control/agent-protocol.md`; the packet already
   carries the canonical current state, objective, missing item and next action.
3. Run `DRIVE()` (below). Do not stop because evidence is missing, a Hook returned `warn` or `block`, or several compliant implementations exist. Do not mistake “most-forward” for “fastest”: complete the current stage's declared coverage before advancing.

The normal path follows the current Hook responsibility and needs no repeated
`status`, `next`, or `runtime reconcile` cycle. Read-only diagnostics and the
original technical recovery commands remain available for a concrete failure;
they are not additional authoring steps or approvals.
Main runs native S6 preparation and Agent dispatch, then explicit integration and checks. S5/S7/S8 use their original prepared view/answer references; S9 uses its original prepared brief. Preparation does not start a platform Agent, and authors do not manually issue/consume. Existing frozen executions retain only their original recovery commands.
In packet mode, the Hook gives your current questionnaire's exact path, clear
questions and remaining stage work. Fill the professional answers and actual
artifact paths there, then save and follow that responsibility's original
reception boundary. The original stage entry prepares questionnaires and
preserves saved answers; an ordinary observation does not create a new assignment.
Do not run issue/consume commands or manufacture
evidence envelopes for normal continuation.

**If the Runtime is a fresh inactive Runtime with no bound REQ:**

0. If the product has user-visible UI, determine `design investment`
   (`local / core / extended`) per `docs/rules/design-foundation.md`.
   Local work uses a module-local derivation without publishing a project
   Foundation. Core/extended work requires a covering published Foundation
   via `.claude/skills/design-foundation/SKILL.md` before locking the UI REQ.
   Pure backend work records Foundation as `N/A` on the project map.
1. Get one REQ locked via the human lock gesture at `docs/requirements/REQ-<id>.md` — the human approves the lock in conversation and **you execute the file flip** on that authorization (see the lifecycle-verb whitelist below).
2. Explicitly select the development branch and final release upstream from project facts. Local Git identity setup and routine local branch naming are environment setup, not new approval; do not guess `main`, the current branch, or a remote default. Ensure the locked REQ is committed on the selected development branch; if it is only in the working tree or index, report the actual missing committed REQ object; binding does not automatically commit or push it.
3. Follow the exact current `requirement-binding.md` path supplied by the entry reminder. Fill its existing four questions: selected REQ, actual authorization record, development branch, release target. Known facts are supplied by the system. A real instruction that already locks and starts the work needs no second confirmation; lock alone does not authorize startup. Names record the actual decision, not identity authentication. Leave undecided choices blank. Normal save invokes the original binding transaction and hands off S2; `req bind` is compatibility/recovery only, not another approval or normal task.
4. Then proceed above.

**If the Runtime is terminal** (`release_authorized` or `aborted`) **and a
new REQ must start:** a human first runs
`loop-harness runtime rollover --approved-by <human identity> --approval-evidence <human-decision-id> --root .`.
Rollover archives the completed runtime and journal, then seeds a fresh
inactive Runtime. Do not edit `loop-state.json` or reuse a terminal Runtime.

`awaiting_human_release` is a non-terminal human decision gateway, not a
rollover source. Follow `docs/control/agent-protocol.md#s11` for the human decision;
`approve` reaches `release_authorized`. `paused` is a resumable wait state,
not a terminal state. Release authorization records the handoff only; it
does not execute a merge, deployment, publication, or release.

`/loop` is **not** an authorization and does not bind a REQ. It only delivers the Layer 2 Wake-up prompt on a schedule.

## Native S6 delivery (s6-native-v1)

For a newly prepared native S6 TASK, Main uses `s6 prepare --task <TASK> --role <role> --root <Main>` and carries the exact returned brief into Claude Code native Agent with worktree isolation. Follow [agent-dispatch](skills/agent-dispatch/SKILL.md#native-s6-s6-native-v1) for dispatch and original reception/integration; the brief supplies this instance's source, paths, author fields and normal/blocked return. SubagentStart adds only a bounded reminder for an exact existing responsibility. Claude Code owns execution and resume; no extra activation or save receipt is required.

Main completes original integration and checks. Its successful completion outlet checks the entire TR-003 batch, follows TR-006 when qualified and supplies the exact S7 ReviewPlan/guide in the same response. Preserve completed work on failure. Re-enter the original `runtime task-integrate` command when directed to resume unfinished integration/checks or the S6 exit; checkpoints prevent duplicate merge/reception. For a preserved/blocked checkpoint after Main check failure, resolve its environment/check cause and follow the original pending command with `--retry-preserved`; bare replay retains the failure. Successful integration needing only exit continuation has no such retry prerequisite. After migration to S7, use the supplied original `s7 draft` entry for missing materials. An unrelated tool call is not the continuation trigger.

This section applies only to `s6-native-v1`. Keep the original frozen recovery behavior for existing workspace/legacy assignments and other stages until migrated. The legacy sections below are not prerequisites of native S6.

## DRIVE()

This is the Main-session loop. Workers execute their current Assignment's
closing contract and report remaining work to its original consumer.

Follow the current stage's original completion contract. S1/S2 normal save
records the professional result and runs its original outlet; ordinary
planning Pre/Stop are read only. Preserve actual human decisions and applicable
professional work. If reception succeeds but the outlet remains unfinished,
follow its specific missing work and original continuation path.

Re-enter this driver at a real stage handoff, native return or concrete blocker.
Ordinary tools continue the current professional task; do not rerun the full
inventory, status or baseline checks on every call. The detailed method and stop
conditions live in `.claude/skills/loop-orchestration/SKILL.md`.

```text
1. Use the current Hook packet and original accepted facts. The system maintains
   baseline identity; inspect source drift only when the actual task or error calls for it.
2. Read the current stage contract at docs/control/agent-protocol.md#<stage>.
3. Inventory completed deliverables and valid evidence from the packet, Runtime, and artifacts.
4. If the stage is incomplete: pick the most-forward missing deliverable or
   evidence within the current stage contract from the Hook packet
   `quality_gate.missing` (or `loop-harness ready` when the checklist is
   unclear). “Most-forward” means the next contractually unblocked piece; it
   never means skipping coverage, prerequisites, or a required review.
   If a subagent assignment is awaiting its required plan checkpoint or approval:
   for frozen legacy work the next action is that mode's checkpoint / approval / activation barrier; native S6 continues from its prepared brief,
   not self-execution of that delegated work.
   Else: use the existing reception/followup feedback to identify the remaining
   owner and action. An accepted answer is not a reason to fabricate another tool
   call, resubmit unchanged work or call transition CLI.
5. Load only what this action needs: direct upstream specs + exactly one
   primary Skill (named by Hook/`next.primary_skill`) + risk-triggered Best Practices.
6. Execute the action (self, or one single-responsibility subagent assignment
   via `agent-dispatch`).
7. Verify the artifact against its actual oracle and relevant counterexamples.
   Preserve observations as work occurs; record real uncertainty in the original
   deliverable. Do not repeat checks or create another report merely to satisfy
   a generic second-review procedure.
8. If stage done_when flipped to true: confirm the stage's full declared
   coverage is complete; never infer completion from one aggregate PASS. Then
   follow the original receiver's transition result and next responsibility.
   Distinguish accepted work, committed transition, and next-material preparation;
   resolve the actual unfinished step. Do not wait for an unrelated tool call or
   hand-push `runtime transition`.
9. Loop back to step 2.
```

## Three-layer runtime delivery

These three layers describe how execution, scheduled wake-up and event listening
cooperate. They are not a partition of instruction authority: entry routing,
stage contracts, professional methods, concrete responsibilities, event delivery
and result/recovery feedback each retain their own source of truth.

```text
Layer 1  Main-session Driver   this file + agent-protocol.md + loop-orchestration Skill    (drives)
Layer 2  Wake-up Recovery      .claude/loop.md (delivered by Claude /loop)                 (re-seats driver)
Layer 3  Graph listener        Hooks + Runtime Milestone                                  (observes, reminds, advances)
```

Layer 3 routes events to original consumers and current-state projections.
Successful Main saves and native returns can record facts and run their original
outlets; ordinary planning observation only reads current responsibility.
Recovery context gives the exact questionnaire path and remaining duties.
PreToolUse observes progress without tool authorization or interception. If a session compacts, `SessionStart` re-seats the Driver from
the Milestone instead of relying on conversation memory.

## Control boundaries

- Humans own lock, release and the explicit stage authority boundaries below. The Driver autonomously executes work within those boundaries — including executing the `状态：locked` file flip on the human's explicit lock gesture.
- Loop automation cannot lock without the human's lock gesture, cannot modify the **bound** REQ without a real explicit human amendment decision, cannot squash merge, publish, deploy, or release.
- **Lifecycle-verb whitelist** — what the main session may execute on a human's behalf:
  | Verb | May the agent run it? | Required human gesture |
  |:--|:--|:--|
  | S1 requirement binding | Main fills the original four questions at the supplied current path and saves; req bind is compatibility/recovery only | actual explicit startup decision, reusing an already clear lock-and-start instruction; name records the decision, not authentication |
  | `runtime pause` / `runtime resume` / `req amend` | carry out the necessary original commands on a real clear human business decision; no verbatim command or repeat execution approval required | record the actual decision and its known human attribution; ask only if intent, scope or attribution is unclear; Git identity is not approval |
  | `req unbind` / `runtime rollover` | only on an explicit human decision for that specific abandonment or new period; do not infer it from amendment approval | actual specific human choice and attribution; never treat Git identity as approval |
  | S11 human release decision | Main faithfully fills the prepared application’s original disposition and optional reason, then saves; PostToolUse receives and advances through the original Writer | a real clear human choice; without one leave disposition empty and wait; no metadata, command or identity proof required |
  "Locking a REQ" = the human's explicit lock gesture in conversation (see skills: requirement-funnel Exit Conditions); the file edit that flips `状态：locked` is executed by the main session on that authorization. S1 reuses a clear accompanying startup decision without second confirmation. If startup intent is absent, leave that original answer blank and ask for the actual decision; do not request a command as proof.
- `/loop` only delivers the Layer 2 prompt. S1 binding uses normal save of the current four-question application; the original transaction owns binding and the S2 handoff.
- Dispatch follows the registered mode. S5/S7/S8 reviewers and Investigators on ordinary `plan_checkpoint` assignments work from the Hook's exact domain questionnaire without a duplicate generic PLAN_REPORT. Native S6 Builders save their professional plan and continue without an activation handshake; Stop receives their actual delivery. S9 Builders personally save PlanReport before work and RepairResult after work, directly recording professional facts without a generic PLAN/AgentBegin/activation/completion acceptance prerequisite; actual checks and explicit user decisions remain required work. Do not add generic PLAN or completion submissions. An explicit `plan_approval_required` responsibility retains its original domain approval; existing frozen executions retain their named recovery contract.
- Once work is delegated, Main uses native wait/resume and the original receiver facts. A returned honest blocker or already accepted result becomes Main’s next responsibility; do not re-wake its author for peer work or integration. Main does not author independent results or mark unfinished work done.
- Blocking findings enter InvestigationCase and approved RepairContract handling; a canonical BUG is an optional projection. Targeted re-verification never produces a clean round.
- S10 overrides the generic Driver steps 7–8 on repeated verification and
  waiting for the next PreToolUse: Main collects actual professional results
  in the supplied current Manifest; reuse applicable S7 observations and add
  checks only for specific gaps, contradictions or actual risks. Save once for
  this step's original exit or concrete remaining work. Do not create per-row
  counterevidence, duplicate reports or metrics obligations. Actual unfinished
  work cannot count as complete; real residual risks need impact and ownership.
- S10 is read-only for product code, locked REQ, contracts, and TASKs. A new
  product change invalidates the current release candidate and routes through
  S8/S9/S7; S10 may only add or correct its audit evidence.
- Automation stops at `awaiting_human_release`. Release requires separate human approval.

## Agent role selection

Prefer the predefined role-bearing agents over `general-purpose`. Each
`.claude/agents/<role>.md` supplies its professional mission, work boundaries,
Skills and declared model. Tools follow the actual platform configuration;
the role name does not establish a separate Hook tool ACL. Main carries the
original brief or exact view/answer paths into the native task so the role
can perform its registered responsibility and return the original artifact.
Using `general-purpose` does not replace the required expertise, independent
judgment or original consumer's source and responsibility checks.

Use `general-purpose` only when:

- The work is genuinely outside every predefined role (one-off research,
  ad-hoc scaffolding that no role owns).
- A role-bearing agent is actually unavailable. Inspect its concrete platform
  error and installed role before choosing an equivalent independent context.
  For a CLI syntax error, use that command's `--help` to diagnose the real gap;
  a CLI check is not a prerequisite for every native Agent dispatch.

If a role-bearing agent gets stuck mid-assignment (no usable professional
artifact or a concrete blocker), preserve its original work and inspect the
actual return and reception feedback. Main restores preparation or sources;
the author corrects professional omissions and can resume the same native
responsibility. Reassignment follows the original domain rules when needed.
Only an unresolved human decision requires a human gateway. Changing the
role does not repair a missing source or authorize bypassing a real approval.

## Stage route (summary)

Full stage contracts at `docs/control/agent-protocol.md#s0` through `#s11`.

```text
S0 requirement_design
→ S1 initialize → S2 design → S3 contracts → S4 tasks
→ S5 document_verification → S6 build
→ S7 full_verification_round
→ S8 finding_investigation → S9 bug_resolution
→ S7 fresh_full_verification_round
→ S10 acceptance_and_audit → S11 human_release_gateway [awaiting human decision]
```

The clean path is `S7 clean round → S10 acceptance/audit → S11`. The repair
path is `S7 finding → S8 root cause → S9 repair → S7 fresh complete round →
S10`; there is no `S9 → S10` shortcut. Failure routes: S5/S6 spec defect →
S2/S3/S4 rework at the affected layer; S10 defect → S8/S9/S7; REQ change or irreversible decision →
the matching human Gateway.

## Human Gateway types

For exceptional blockers, the main session uses the Gateway types below. Explicit stage authority boundaries (unresolved S2 direction decisions, S8 approval without a valid delegation, and review-budget decisions) also remain in force; do not invent additional human gates for ordinary technical recovery.

| Type | Trigger |
|:---|:---|
| `release_ready` | S11 reached, release audit complete |
| `req_amendment` | locked REQ needs to change |
| `unrecoverable_business_decision` | business semantics cannot be derived from any baseline |
| `missing_external_permission` | external access the main session cannot obtain, after all other work is done |
| `runtime_integrity` | snapshot/journal cannot be safely reconciled |

For S11 `release_ready`, reuse the accepted S10 readable reports, actual residual risks and exact paths; do not create a duplicate Gateway package or invent an unresolved fact. TR-017 prepares current materials once; ordinary Pre/Stop only reads. The human explicitly chooses one of the six dispositions. Main faithfully fills the supplied application’s original `disposition` and optional `reason`, then saves; PostToolUse automatically records/consumes that decision through the original Writer and fixed outlet. Without a real human choice leave disposition empty: draft saves do not write Runtime or default to approval. The Hook does not authenticate identity or demand extra confirmation fields/proofs; metadata and caller attribution come from the system. The original human-decision command is compatibility/recovery, not a normal task. Rejection carries the original reason and exact paths to S8/S10; S10 retains observations and clears the returned step’s old final conclusion. Other Gateway types retain their existing package requirements: type, completed work (an evidence inventory, not a responsibility status), actual unresolved issue, impact, recommendation and resume stage.

## Runtime authority

| Concern | Authority |
|:---|:---|
| stage contracts | `docs/control/agent-protocol.md` |
| legal Loop states/transitions | `docs/control/loop-definition.json` |
| current facts + bound REQ | `.claude/loop-state.json` (Harness is sole writer) |
| methodology | `.claude/skills/*/SKILL.md` |
| role identity | `.claude/agents/*.md` |
| assignments + scope | team manifest + Agent message envelopes |
| progress listener | Claude Code Hooks: exact questionnaire path, missing duties and next action |
| stable policies | `docs/rules/README.md` |

Do not infer runtime state from this file, `project.yaml`, project-map, a TASK body, or chat.

## First read (every session start)

In packet mode, after S7 Reviewer registration, the Hook automatically prepares
their current questionnaires and supplies the exact view and answer paths.
Give each Reviewer its immutable view and exact answer path. The Reviewer performs
the checks, fills and saves the professional fields, then reports results to Main.
PreToolUse observes ready original answers; successful PostToolUse and SubagentStop
provide fallback through the original consumer, without an extra submission.
Ordinary `plan_checkpoint`
reviewers do not send a separate generic PLAN_REPORT first. If the registered
mode is `plan_approval_required`, follow that original approval before review.
An accepted Result projects that Reviewer's responsibility to `done` when the
original receipt is current and valid; the original round consumer still decides
the whole-round outcome. Reissue after controlled Plan
revision and after queued Assignments become dispatched. Never fill another
Reviewer's verdict or select a producer from an answer body.

For the S7→S10 clean path, final valid Result reception forms CleanRound and
its original completion outlet evaluates the gate/TR-009 and prepares the exact
S10 acceptance questionnaire in the same handoff. Read the accepted Result,
transition and material facts separately. Follow the named original continuation
(`loop-harness runtime review-continue --root <Main>`) or current S10 material
recovery (`loop-harness runtime audit-prepare --root <Main>`) for a concrete failure, preserving accepted work
and existing reports. An ordinary tool call is not required to trigger the exit;
Main does not resubmit Reviewer answers or copy clean_round bindings. Main owns
whole-round coverage; each Reviewer performs its assigned Claims/CASEs using the
actual browser, API, command or state-based method.

For the S7→S8 handoff, a sealed batch advances through TR-008 on the next Main
PreToolUse. Main then reads the supplied Batch and Findings, fills the exact S8
intake's grouping_rationale and saves it; the original consumer creates the Case.
Main uses the supplied hypothesis questions to define actual investigation work
and register its professional division, then dispatches each native Investigator
with the prepared exact view/answer. The Investigator saves its professional
causal result and returns the reference for SubagentStop reception, without a
generic PLAN_REPORT. An explicit `plan_approval_required` assignment keeps its
original approval. The original Case Writer records the result and receipt;
new questions still require Main dispatch. Put experiment-specific scope,
stopping and unresolved conditions in the original `discriminator`, and
predictions in `expected_outcomes`. The first native return may hand unfinished
work and diagnostics to Main without accepting the answer or advancing the Case.
Main restores missing sources or resumes the author. Preserve the one active
Case's source set; mixed causes do not imply arbitrary split/merge support.
Follow the original Case route and actual contract approval: the designated
Driver may be Main, while an undesignated Main organizes and faithfully records
the review. An answer or saved Contract is not repair authorization.

1. Read the Hook's current responsibility, exact questionnaire view/answer
   paths, missing duties and next action.
2. Read this file and the supplied questionnaire view.
3. Follow that view's required input paths and the current stage's protocol
   section and primary Skill. Read only the relevant clauses.
4. If the Hook reports unavailable state or an inconsistent checkpoint, inspect
   `.claude/loop-state.json` and its `milestone` for that diagnostic. Missing
   questionnaire paths are a system preparation issue; do not guess directories.
5. Use `.claude/bin/loop-harness.md` when the named recovery action needs it.

Load the rest on demand.

## Project commands

```bash
{install command}
{test command}
{typecheck/build command}
{lint command}
```

Compatibility/recovery commands (normal S1 uses the supplied four-question application and save):

```bash
.claude/bin/loop-harness req bind --req <locked-req> --approved-by <actual-approver> --dev-branch <development-branch> --release-upstream <release-destination>
# Replace each placeholder before execution; a remote release destination includes its remote.
# Stage-specific preparation and integration use their named commands above.
# Additional commands below are diagnostics or concrete recovery, not a routine checklist.
# Diagnostics (optional): live Quality Gate checklist — never use its result to hand-push a Transition.
.claude/bin/loop-harness ready --root .
.claude/bin/loop-harness status --root .   # coarse projection only
.claude/bin/loop-harness next --root .     # coarse projection only
.claude/bin/loop-harness runtime reconcile --root .
.claude/bin/loop-harness runtime rollover --approved-by <identity> --approval-evidence <human-decision-id> --root .
.claude/bin/loop-harness doctor --root .   # schema/manual/policy_ref/metrics — not stage readiness
.claude/bin/loop-harness validate --all --root .
```

Before every `Agent` / `Task` call, check whether one specialized subagent is
sufficient, name its predefined role and registered assignment, and isolate
product writes in the assignment worktree. Prefer a team only when peer
coordination adds value. For supported Claude Code >=2.1.178, teams are
session-managed: do not call removed TeamCreate/TeamDelete tools or require
Agent.team_name (ignored by the platform). Team support must be enabled and
verified; use Runtime assignment/agent identity, not platform team name, for
authority. Read-only research still follows applicable scope restrictions.
See docs/claude-platform-compatibility.md for tested versions and acceptance.

On `SubagentStop`, Main consumes the explicit
`runtime task-integrate --assignment-id <id> --root <main-root>` action.
The Hook itself does not run long checks. Integrate into the REQ-bound development
branch, never an implicit develop branch. Keep Main in its
original checkout; inspect Worker files read-only and let the assigned Worker
make product changes. Successful verification precedes durable completion_ack,
which precedes cleanup. Preserve cleanup_pending without discarding verification.
Respect project branch protection and the human release gateway. On
`TeammateIdle`, inspect the current assignment before re-waking the same
teammate; do not silently replace it. See docs/workspace-integration.md for
current implementation boundaries.

## Escalation

- Requirement change: pause via Runtime; follow `docs/rules/change-control.md`.
- Specification conflict: report during phase one; do not improvise.
- Blocking finding: use `.claude/skills/bug-resolution/SKILL.md` to investigate root cause before repair.
- Unclear next action: use `.claude/skills/loop-orchestration/SKILL.md`.
- Human-controlled or irreversible action: surface the matching Gateway type.

## User-visible progress

The recovery packet is a scheduling checkpoint, not a work report. At the
start of work, after a meaningful deliverable, on a blocker or change of
approach, and approximately every 60 seconds during sustained work, tell the
user briefly what deliverable or evidence is now accepted, what action is underway, and what comes
next. Use the user's language. Do not narrate every tool call or expose
internal reasoning. Report only observed results: a revision increment, an
intended action, or a repeated Hook message is not evidence of progress.

Hook `systemMessage` is a user notice; model recovery instructions belong in
`hookSpecificOutput.additionalContext` for supported events. Full recovery
context is retained at session/agent start and stage transitions. Ordinary
PreToolUse calls provide the current gate conflicts, missing work, and a
compact checkpoint. Repeated unchanged guidance may be shortened within the
same session and responsibility. New errors, changed paths and session recovery
must still deliver the actionable context; display caching never grants authority.
A Worker plan checkpoint requiring no approval does not silence progress
reports to the user.

## S4 planning handoff

Main completes the current REQ's TASK batch and waves-v1 index from the supplied
frozen inputs, commits the formal outputs on the bound branch, and saves the
original three-field answer. Use numeric TASK IDs such as `TASK-009` or
`TASK-009-01`. Ordinary planning needs no fresh human approval; keep actual REQ
amendments and decisions beyond existing authorization at their human boundary.
The original consumer checks and receives the batch, then follows TR-002.

If accepted S4 planning needs a professional TASK/index correction while S4 is
still current, use `runtime stage-answer rework --target tasks --reason "<concrete TASK/index correction>" --root . --agent Main`.
Preserve valid REQ/S2/S3 inputs; true design/contract gaps return to their own
layer. After entry into S5 follow its independent review/document-rework route.
For accepted work with a pending outlet, follow the supplied complete `recover`
command; after transition with incomplete S5 preparation, follow the supplied
complete `issue` command. Keep the accepted answer and completed preparation.
Successful handoff supplies two exact view/answer/source-root sets; Main
actually dispatches the two independent native reviewers via `team-planning`.
Necessary status/next diagnostics remain available for concrete uncertainty.

## Bounded planning continuation

S3/S4 work does not require another user approval merely because a stage
changed or drafting is substantial. Main Stop provides one continuation
reminder when a locked REQ has a current unblocked planning milestone and
no delegated agent. `stop_hook_active` always permits the next Stop; this
is not an unattended scheduler or proof of completion. The reminder is
separate from ordinary tool permissions: continue authorized professional work
and self-review using available tools, without a new planning approval. Honor explicit user
stop requests and explain actual permission/external waits. Unresolved S2
direction decisions and other stages retain their original authority boundaries;
reuse an existing applicable human decision without asking again.

## Optional bounded repair authority

For a new bound REQ, an explicit finite human grant may delegate ordinary S8 contract reviews to the Driver under docs/bounded-repair-autonomy.md. Do not infer this grant from the approver name, install it on old REQs, or manufacture human_decision for technical review. No grant preserves the existing explicit contract approval requirement. Unresolved S2 direction decisions, requirement changes, budgets, external permissions and release retain their own boundaries. Within a valid grant, recoverable task/scope-planning issues go to the Driver, not repeated user A/B/C menus.

## Approved repair policy at new requirement binding

If installation recorded a human-approved project repair policy, include its
unchanged path and SHA256 in the existing binding-policy facts. The compatibility/recovery CLI exposes these as `req bind --repair-policy` and
`--repair-policy-sha256`; running that command is not an extra normal binding step. Do not create or enlarge a policy from an account name.
See `docs/bounded-repair-autonomy.md`. In-scope technical RepairContract approvals
then use the Driver's registered review and pinned policy, without another human
approval. Missing authority, business changes and release keep their Gateway.
Existing bound runtimes are not retroactively granted this authority.

## 临时 worktree 与阶段交付

REQ 绑定必须显式提供 `--dev-branch <开发主分支>` 和 `--release-upstream <最终发布上游>`，远程发布目标包含 remote；没有 develop 默认值。项目根目录是当前 REQ 唯一权威。

派发前将上游正式 Markdown、代码和测试产出整理提交；不要自动提交用户无关变更。新 worktree 不含主会话未提交或仅暂存的文件。新 S6 使用 Claude Code 原生 Agent/worktree 与 s6-native-v1 工程交付；旧 workspace prepare/launch 仅用于既有执行的原恢复。不要在它之前另跑 `worktree-create`，也不要依赖平台默认分支或复制目录。明确不入 Git 的 evidence 单独按依赖交接，不能复制整个控制面。

子会话在子分支提交成果并报告，主会话在项目根目录执行 `runtime task-integrate --assignment-id <id>`，生成合并提交、校验、接收并及时清理。集成不是 release；临时 worktree 不是交付终点。未回收、分支偏离、积压只提醒，不新增 Stop 或普通工具硬门禁。

Gate 的输入来源由 loop-definition 的 file_sources 契约声明；正式交付读固定 Git tree，明确运行输入读磁盘，Runtime 读权威快照。未提交产出不能帮助阶段通过。允许汇总的 evidence 按 mutable_evidence_kinds 自动同步对应 SHA256；同步不改变结论或代际，也不抹除产品基线漂移。

## Shared data and contract reading

For S3 converge the project data model and SYNC protocol before deriving FE/BE
responsibilities. Use [shared-model contracts](docs/rules/shared-model-contracts.md).
For S6 start from the TASK's ordered file links, scope and closing assertions;
read only the relevant contract/protocol/model slices. Register committed model
inputs in the existing baseline, and require real consumers to validate or
generate from them. Do not create a per-REQ model copy or an extra registry.

### S9 repair questionnaires

Use the current native brief or independent Targeted request as the exact authoring
entry, following `.claude/skills/bug-resolution/SKILL.md`. Main reads its own
questions and accepted artifacts; it does not fill Worker answers. Builder Plan
and Result are flat private outputs in the actual native worktree; Main-root
questions supply read-only inputs. Save before/after actual work and report the
results to Main when finished. Independent Targeted uses its original answer only,
without a Builder PlanReport or product changes. PreToolUse observes ready original
answers; successful PostToolUse and SubagentStop provide fallback through the
original consumer, without an extra submission. Applicable no-change source
preservation follows the Native S9 dispatch contract above and does not replace the
original receipt. Main saves its ChangeImpact through its original consumer.

Keep accepted Candidates, Final, Impact, Targeted and Handoff distinct. Resume only
the unfinished original responsibility or completion outlet identified by feedback;
do not repeat accepted observations or use ordinary status/Pre to run handoff.
After successful TR-012, Main fills and saves the supplied original S7 ReviewPlan
questionnaire; the seed is input, not PASS or a second manual registration task.
Never guess paths or manually issue/consume. Registered frozen executions keep
their original answer shapes, sources and named recovery.
