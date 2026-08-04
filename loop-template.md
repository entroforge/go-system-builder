# Loop Wake-up Prompt (project `.claude/loop.md`)

> Template source. The release tarball installs this file at `.claude/loop.md`
> in the target project. It is the default Prompt that Claude Code's built-in
> `/loop` scheduler delivers on each wake-up.
>
> Source: `docs/agent-protocol.md`, `docs/loop-definition.json`, and
> `AGENTS-template.md`.

## What this file is — and is not

| Is | Is not |
|:---|:---|
| the Layer 2 Wake-up Prompt that recovers the Main-session Driver | the Engineering Loop itself |
| a short router back into `AGENTS.md`, the Runtime Bookmark, and the current stage of `agent-protocol.md` | a second copy of the Main Spine |
| consumed by Claude Code's built-in `/loop` scheduler | a REQ binder, a state machine, or a `/goal` replacement |

The Hook control plane is the other automatic wake-up path. Lifecycle Hook
events call the Controller, which refreshes `.claude/loop-state.json`'s
`milestone` and emits `LOOP RECOVERY` guidance. `/loop` and this file are a
fallback scheduler; they must resume the Controller's current REQ checkpoint,
not invent a second plan.

`/loop` only delivers this Prompt on a schedule. Engineering Loop binding,
Runtime mutation, and Hook enforcement are owned by other components and are
never triggered by this file.

## Wake-up algorithm

On every wake-up, run in order. Do not skip reconcile, do not collapse steps,
do not wait for the next wake-up before acting.

> **Skip gate (execute-first)** — If the previous turn of this conversation
> already names (a) the bound locked REQ + SHA-256 baseline, (b) the current
> Main Spine stage, and (c) `next.missing` + its `primary_skill`,
> **skip directly to step 7**. The read sequence below is the fallback for
> state-unknown wake-ups; running it on state-fresh wake-ups is the
> `reader-bias verify-loop` anti-pattern (see `loop-orchestration` SKILL
> §"Non-stop conditions"). State-fresh execution is the default;
> state-unknown is the exception.

```text
1. Recover the Engineering Loop, not a new plan. Start from the most recent
   Hook packet and its `Stage`, `Next`, and `Read in order` fields. If no packet
   exists, read the Runtime `milestone`. The normal wake-up path does not call
   `loop-harness`; use it only for initialization/binding, integrity recovery,
   rollback/rollover, or the release Gateway.

2. Read the map, not the entire library. After compact/new-session recovery,
   follow the Hook packet's order: AGENTS.md, .claude/loop-state.json, the
   current stage section of docs/agent-protocol.md, the bound locked REQ path,
   and the one primary Skill.

3. Confirm the bound locked REQ. Verify it is locked and its SHA-256 matches
   the Runtime baseline. On mismatch, surface REQ_FINGERPRINT_MISMATCH and
   stop; do not repair the baseline from this Prompt.

4. Recover current stage, completed deliverables, and open work items from the
   Hook packet, Runtime Milestone, and current artifacts.
   Do not redo work whose evidence is already valid; do not skip work whose
   evidence is missing, stale, or invalidated.

5. Re-read the current stage's purpose and done_when in agent-protocol.md.

6. Load only what the next action needs: direct upstream specs, exactly one
   primary Methodology Skill (named by `next.primary_skill`), and risk-
   triggered Best Practices.

7. Pick the single next legal action: the most-forward missing deliverable
   or evidence named by `next.missing`. Execute it now via DRIVE() in
   `loop-orchestration`. Do not provide a technical-choice menu; pick the
   lowest-risk reversible implementation and continue.

8. Keep driving across TASKs, subagent returns, Hook `warn` or `block`, and
   tool results. Do not stop because a TASK finished, a subagent reported, a
   test failed, or several compliant implementations exist.

9. Stop only at a real Human Gateway (see AGENTS.md), an asynchronous
   external process already started, or Claude Code ending the turn.
```

## Mandated rules

These come from the Loop Definition, the Main Spine, and the project entry
contract. They are not interpretation.

- **Idempotency** — two wake-ups in a row must never create the same TASK, Team, BUG, assignment, or evidence twice. Before creating any entity, check the Runtime.
- **No technical-choice menus** — when several implementations satisfy the locked REQ, contracts, and rules, pick the lowest-risk reversible one, record the assumption, continue.
- **No local-completion-as-REQ-completion** — finishing one TASK, one BUG repair, one targeted re-verification, or one browser flow is not REQ completion. Only a same-round complete Delivery + QA + E2E clean round opens acceptance.
- **No authorization inventions** — this Prompt does not bind a REQ, does not replace `/goal`, does not start a schedule, does not approve release.
- **No silent reporting** — do not report status and then wait for the next wake-up; execute the next action now.
- **Activation first** — subagent work is expected to remain read-only until the main session approves the read-back and activates; Hook v1 reports recoverable violations as warnings.

## Recovery contract

If a Hook returns `warn` or `block` during this turn, follow its
`recovery[]` sequence immediately and continue DRIVE(). `warn` keeps the
original tool action proceeding (observation only); `block` requires human
review and forms a Gateway.

If `loop-harness next` returns `MISSING_DELIVERABLE`, the named missing work
**is** the next action — that is not a blocker.

If the Runtime is `inactive` or has no `bound_req`, this Prompt cannot drive
any REQ work. Surface `NO_BOUND_REQ` and stop. Do not bind a REQ from this
Prompt.

## Reference

- Main Spine: `docs/agent-protocol.md`
- Loop Definition: `docs/loop-definition.json`
- Project Entry: `AGENTS-template.md`
