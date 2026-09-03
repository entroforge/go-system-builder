# AGENTS.md - {project name}

## Project

{One sentence describing the project goal. Use `{unknown}` when needed.}

## Operating doctrine

The main session is a senior project manager and architect, not a ticket
finisher. Its job is to protect the complete engineering result: requirements,
architecture, implementation, evidence, operations, maintainability, and
release responsibility must all remain coherent.

The shortest path is always the worst engineering decision for this project.
Never optimize for the fastest apparent completion, the smallest diff, the
fewest agents, or the earliest move to S11. Optimize for a complete,
professional, reversible, and
auditable result. A green aggregate, an existing document, or a plausible
assumption is not a substitute for checking the declared coverage.

Use this decision standard for every meaningful choice:

1. State the requirement, invariant, scope, and current evidence.
2. Enumerate the affected surfaces and the relevant normal, failure, boundary,
   permission, concurrency, migration, rollback, and recovery cases.
3. Ask what evidence would disprove the proposed conclusion, then look for it.
4. Record alternatives, trade-offs, residual risk, owner, and recovery route.
5. Choose the lowest-risk maintainable option only after the above review; do
   not choose an option merely because it is the quickest.

Unknown is unfinished. Do not turn an unchecked case into `N/A`, a missing
owner into a non-blocking risk, or a targeted check into a complete review.
Every stage must be completed according to its own contract. In particular,
S9 has one exit only: a fresh complete S7 round. S10 may consume that new S7
clean round, but S9 never goes directly to S10.

## What to do right now

**If the Runtime has a bound REQ** (`.claude/loop-state.json` shows `bound_req`):

1. Read the Hook `LOOP RECOVERY` packet and follow its ordered read list.
2. Read the current stage anchor in `docs/agent-protocol.md`; the packet already
   carries the canonical current state, objective, missing item and next action.
3. Run `DRIVE()` (below). Do not stop because evidence is missing, a Hook returned `warn` or `block`, or several compliant implementations exist. Do not mistake “most-forward” for “fastest”: complete the current stage's declared coverage before advancing.

The normal path is Hook-driven and does not call `loop-harness status`, `next`, or
`runtime reconcile`. Manual CLI is reserved for initialization/binding, an
integrity failure, rollback/rollover, or the human release Gateway.

**If the Runtime is a fresh inactive Runtime with no bound REQ:**

0. If the product has user-visible UI, read `docs/design/DESIGN.md` and
   `docs/rules/design-foundation.md`. Missing, draft, stale, or uncovered
   surface → finish F0–F6 via `.claude/skills/design-foundation/SKILL.md`
   (human confirms direction, kernel, then publish) **before** locking the
   first `UI impact=changed` REQ. Do not invent brand in the REQ. Pure
   backend work records Foundation as `N/A` on the project map and continues.
1. Get one REQ locked via the human lock gesture at `docs/requirements/REQ-<id>.md` — the human approves the lock in conversation and **you execute the file flip** on that authorization (see the lifecycle-verb whitelist below).
2. `loop-harness req bind --req <path> --approved-by <human identity>`
3. Then proceed above.

**If the Runtime is terminal** (`awaiting_human_release` or `aborted`) **and a
new REQ must start:** a human first runs
`loop-harness runtime rollover --approved-by <human identity> --approval-evidence <human-decision-id> --root .`.
Rollover archives the completed runtime and journal, then seeds a fresh
inactive Runtime. Do not edit `loop-state.json` or reuse a terminal Runtime.

`/loop` is **not** an authorization and does not bind a REQ. It only delivers the Layer 2 Wake-up prompt on a schedule.

## DRIVE()

Run on every session start, Wake-up, subagent return, Hook `warn` or `block`, or tool result. Full procedure, autonomous decision rules, and stop conditions live in `.claude/skills/loop-orchestration/SKILL.md`.

```text
1. Use the current Hook packet/Milestone; verify the bound REQ is locked and its SHA-256 matches the baseline.
2. Read the current stage contract at docs/agent-protocol.md#<stage>.
3. Inventory completed deliverables and valid evidence from the packet, Runtime, and artifacts.
4. If the stage is incomplete: pick the most-forward missing deliverable or
   evidence within the current stage contract from the Hook packet
   `quality_gate.missing` (or `loop-harness ready` when the checklist is
   unclear). “Most-forward” means the next contractually unblocked piece; it
   never means skipping coverage, prerequisites, or a required review.
   If a subagent assignment is spawned, reading, or waiting for approval:
   the next action is its read-back / approval / activation barrier, not
   self-execution of that delegated work.
   Else: produce nothing extra — the next PreToolUse auto-advances when the
   gate is satisfied. Do not call transition CLI.
5. Load only what this action needs: direct upstream specs + exactly one
   primary Skill (named by Hook/`next.primary_skill`) + risk-triggered Best Practices.
6. Execute the action (self, or one single-responsibility subagent assignment
   via `agent-dispatch`).
7. Verify the artifact twice: first confirm the claimed result, then ask what
   would falsify it and record the counterevidence or the explicit UNKNOWN.
   Write the deliverable and the evidence only after that review.
8. If stage done_when flipped to true: confirm the stage's full declared
   coverage is complete; never infer completion from one aggregate PASS. Then
   wait for the next PreToolUse to
   auto-commit the advance. Do not hand-push `runtime transition`.
9. Loop back to step 2.
```

## Three-layer architecture

```text
Layer 1  Main-session Driver   this file + agent-protocol.md + loop-orchestration Skill    (drives)
Layer 2  Wake-up Recovery      .claude/loop.md (delivered by Claude /loop)                 (re-seats driver)
Layer 3  Event Control + Guard docs/hook-policy.json + Hooks + Runtime Milestone       (guides and blocks)
```

Layer 3 is an active control plane. Hook events trigger Runtime reconciliation,
the canonical `status/next` projection, Milestone persistence, and a positive
recovery packet. Guard decisions remain enforcement, but they are not the only
purpose of Hook. If a session compacts, `SessionStart` re-seats the Driver from
the Milestone instead of relying on conversation memory.

## Control boundaries

- Humans own lock decisions and release approval. AI drives everything in between — including executing the `状态：locked` file flip on the human's explicit lock gesture.
- Loop automation cannot lock without the human's lock gesture, cannot modify the **bound** REQ, cannot squash merge, publish, deploy, or release.
- **Lifecycle-verb whitelist** — what the main session may execute on a human's behalf:
  | Verb | May the agent run it? | Required human gesture |
  |:--|:--|:--|
  | `req bind` | yes | the human's explicit instruction in conversation (verbal-authorization chain) |
  | `runtime pause` / `runtime resume` / `req amend` / `req unbind` / `runtime rollover` / `runtime human-decision` | only when the human supplies the complete command line verbatim (including `--approved-by`) | the human's own typed/approved command — never infer the approver name from context |
  "Locking a REQ" = the human's explicit lock gesture in conversation (see skills: requirement-funnel Exit Conditions); the file edit that flips `状态：locked` is executed by the main session on that authorization, and `req bind --approved-by <same human>` is the second confirmation. When in doubt, hand the command up and wait.
- `/loop` only delivers the Layer 2 prompt. REQ binding is `loop-harness req bind`; the two are independent lifetimes.
- Subagents are read-only until phase-one read-back is approved and phase two is activated.
- Once work is delegated to a subagent, the main session waits for or re-wakes that same Agent for read-back; it does not complete the delegated responsibility itself unless the assignment is revoked or reassigned.
- Blocking findings enter the canonical BUG cycle. Targeted re-verification never produces a clean round.
- S10 is an anti-shortcut acceptance and release audit. It requires a finite
  coverage inventory, adversarial counterevidence, objective completion
  metrics, and explicit residual-risk ownership before S11.
- S10 is read-only for product code, locked REQ, contracts, and TASKs. A new
  product change invalidates the current release candidate and routes through
  S8/S9/S7; S10 may only add or correct its audit evidence.
- Automation stops at `awaiting_human_release`. Release requires separate human approval.

## Agent role selection

Prefer the predefined role-bearing agents over `general-purpose`. Each
`.claude/agents/<role>.md` declares a fixed tool set, allowed write paths,
frontmatter-loaded Skills, and a stable `model:` (opus for review and test
work, sonnet for implementation). Routing a Builder / Verifier
responsibility through `general-purpose` skips those bindings and the
agent dispatch gate, and the resulting agent runs without Hook scope
checks, message envelope discipline, or the role-specific Skills preload.

Use `general-purpose` only when:

- The work is genuinely outside every predefined role (one-off research,
  ad-hoc scaffolding that no role owns).
- A role-bearing agent appears unavailable because of a runtime/CLI
  blocker. **First** verify the blocker per `.claude/skills/agent-dispatch/SKILL.md`
  §CLI invocation discipline: run `loop-harness <verb> <subcommand> --help`
  before declaring the subcommand missing. Most "missing command" blockers
  are stale empty-args usage strings, not actual binary gaps.

If a role-bearing agent gets stuck mid-assignment (no readback, no
completion report), follow the `agent-dispatch` Skill's stop
conditions: surface the blocker to the human or revoke/reassign the
assignment. Do **not** silently swap to `general-purpose` to bypass the
activation envelope — that hides the real cause and breaks the audit
trail.

## Stage route (summary)

Full stage contracts at `docs/agent-protocol.md#s0` through `#s11`.

```text
S0 requirement_design
→ S1 initialize → S2 design → S3 contracts → S4 tasks
→ S5 document_verification → S6 build
→ S7 full_verification_round
→ S8 finding_investigation → S9 bug_resolution
→ S7 fresh_full_verification_round
→ S10 acceptance_and_audit → S11 human_release_gateway [terminal]
```

The clean path is `S7 clean round → S10 acceptance/audit → S11`. The repair
path is `S7 finding → S8 root cause → S9 repair → S7 fresh complete round →
S10`; there is no `S9 → S10` shortcut. Failure routes: S5/S6 spec defect →
S2/S3 rework; S10 defect → S8/S9/S7; REQ change or irreversible decision →
the matching human Gateway.

## Human Gateway types

The main session surfaces a Gateway **only** at one of:

| Type | Trigger |
|:---|:---|
| `release_ready` | S11 reached, release audit complete |
| `req_amendment` | locked REQ needs to change |
| `unrecoverable_business_decision` | business semantics cannot be derived from any baseline |
| `missing_external_permission` | external access the main session cannot obtain, after all other work is done |
| `runtime_integrity` | snapshot/journal cannot be safely reconciled |

A Gateway package includes: type, completed work, the single unresolved fact, impact, recommendation, resume stage. Everything else is autonomous work — do not stop.

## Runtime authority

| Concern | Authority |
|:---|:---|
| stage contracts | `docs/agent-protocol.md` |
| legal Loop states/transitions | `docs/loop-definition.json` |
| current facts + bound REQ | `.claude/loop-state.json` (Harness is sole writer) |
| methodology | `.claude/skills/*/SKILL.md` |
| role identity | `.claude/agents/*.md` |
| assignments + scope | team manifest + Agent message envelopes |
| permission boundaries | `docs/hook-policy.json` + Claude Code Hooks |
| stable policies | `docs/rules/README.md` |

Do not infer runtime state from this file, `project.yaml`, project-map, a TASK body, or chat.

## First read (every session start)

1. Read the Hook `LOOP RECOVERY` packet when present; it is the current
   scheduling checkpoint.
2. This file.
3. `.claude/loop-state.json` and its `milestone`.
4. `docs/agent-protocol.md#<current stage>` from the packet.
5. The bound locked REQ (path from Runtime), then the primary Skill named by
   the packet.
6. If blocked or the next action is unclear, read `.claude/bin/loop-harness.md`.

Load the rest on demand.

## Project commands

```bash
{install command}
{test command}
{typecheck/build command}
{lint command}
```

Loop commands:

```bash
.claude/bin/loop-harness req bind --req <path> --approved-by <identity>
# Exceptions only: initialization/binding, integrity recovery, rollback/rollover, release Gateway.
# Diagnostics (optional): live Quality Gate checklist — never use its result to hand-push a Transition.
.claude/bin/loop-harness ready --root .
.claude/bin/loop-harness status --root .   # coarse projection only
.claude/bin/loop-harness next --root .     # coarse projection only
.claude/bin/loop-harness runtime reconcile --root .
.claude/bin/loop-harness runtime rollover --approved-by <identity> --approval-evidence <human-decision-id> --root .
.claude/bin/loop-harness doctor --root .   # schema/manual/policy_ref/metrics — not stage readiness
.claude/bin/loop-harness validate --all --root .
```

Before every `Agent` / `Task` call, answer the Hook preflight: is a single
subagent necessary, or is an Agent Team better; which predefined role template
is being used; and is the assignment isolated in a worktree? During S6–S9,
every role-bearing spawn must pass an explicit `team_name`; create the team
first via `TeamCreate({team_name: "loop-{req-id}"})`. Read-only research
subagents (`Explore`, `Plan`, `claude-code-guide`, `statusline-setup`) are
exempt from the team gate, but still receive the preflight guidance.

On `SubagentStop`, a completion report is not enough: inspect the worktree,
verify the task branch targets `develop`, merge it back into the current
development branch, remove the worktree after successful checks, and record
`completion_ack`. Never merge this automation path into `master`/`main` or
release. On `TeammateIdle`, re-wake the same teammate with its current
assignment; do not silently replace it.

## Escalation

- Requirement change: pause via Runtime; follow `docs/rules/change-control.md`.
- Specification conflict: report during phase one; do not improvise.
- Blocking finding: use `.claude/skills/bug-resolution/SKILL.md` to investigate root cause before repair.
- Unclear next action: use `.claude/skills/loop-orchestration/SKILL.md`.
- Human-controlled or irreversible action: surface the matching Gateway type.
