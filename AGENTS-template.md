# AGENTS.md - {project name}

## Project

{One sentence describing the project goal. Use `{unknown}` when needed.}

## What to do right now

**If the Runtime has a bound REQ** (`.claude/loop-state.json` shows `bound_req`):

1. Read the Hook `LOOP RECOVERY` packet and follow its ordered read list.
2. Read the current stage anchor in `docs/agent-protocol.md`; the packet already
   carries the canonical current state, objective, missing item and next action.
3. Run `DRIVE()` (below). Do not stop because evidence is missing, a Hook returned `warn` or `block`, or several compliant implementations exist.

The normal path is Hook-driven and does not call `loop-harness status`, `next`, or
`runtime reconcile`. Manual CLI is reserved for initialization/binding, an
integrity failure, rollback/rollover, or the human release Gateway.

**If the Runtime is a fresh inactive Runtime with no bound REQ:**

1. Get one REQ human-locked at `docs/requirements/REQ-<id>.md` ( REQ locking is human-only).
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
   evidence from the Hook packet `quality_gate.missing` (or `loop-harness ready`
   when the checklist is unclear). That is the next action.
   If a subagent assignment is spawned, reading, or waiting for approval:
   the next action is its read-back / approval / activation barrier, not
   self-execution of that delegated work.
   Else: produce nothing extra — the next PreToolUse auto-advances when the
   gate is satisfied. Do not call transition CLI.
5. Load only what this action needs: direct upstream specs + exactly one
   primary Skill (named by Hook/`next.primary_skill`) + risk-triggered Best Practices.
6. Execute the action (self, or one single-responsibility subagent assignment
   via `two-phase-activation`).
7. Verify the artifact; write the deliverable and the evidence.
8. If stage done_when flipped to true: wait for the next PreToolUse to
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

- Humans lock REQs and approve release. AI drives everything in between.
- Loop automation cannot lock or modify the bound REQ, cannot squash merge, publish, deploy, or release.
- `/loop` only delivers the Layer 2 prompt. REQ binding is `loop-harness req bind`; the two are independent lifetimes.
- Subagents are read-only until phase-one read-back is approved and phase two is activated.
- Once work is delegated to a subagent, the main session waits for or re-wakes that same Agent for read-back; it does not complete the delegated responsibility itself unless the assignment is revoked or reassigned.
- Blocking findings enter the canonical BUG cycle. Targeted re-verification never produces a clean round.
- Automation stops at `awaiting_human_release`. Release requires separate human approval.

## Agent role selection

Prefer the predefined role-bearing agents over `general-purpose`. Each
`.claude/agents/<role>.md` declares a fixed tool set, allowed write paths,
frontmatter-loaded Skills, and a stable `model:` (opus for review and test
work, sonnet for implementation). Routing a Builder / Verifier
responsibility through `general-purpose` skips those bindings and the
two-phase activation gate, and the resulting agent runs without Hook scope
checks, message envelope discipline, or the role-specific Skills preload.

Use `general-purpose` only when:

- The work is genuinely outside every predefined role (one-off research,
  ad-hoc scaffolding that no role owns).
- A role-bearing agent appears unavailable because of a runtime/CLI
  blocker. **First** verify the blocker per `.claude/skills/two-phase-activation/SKILL.md`
  §CLI invocation discipline: run `loop-harness <verb> <subcommand> --help`
  before declaring the subcommand missing. Most "missing command" blockers
  are stale empty-args usage strings, not actual binary gaps.

If a role-bearing agent gets stuck mid-assignment (no readback, no
completion report), follow the `two-phase-activation` Skill's stop
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
→ S8 finding_investigation ↔ S9 bug_resolution
→ S10 acceptance_and_audit → S11 human_release_gateway [terminal]
```

Failure routes: S5/S6 spec defect → S2/S3 rework; S7 blocking finding → S8 root-cause investigation → S9 repair → fresh complete S7 round.

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
