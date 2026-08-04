---
name: state-machine-design
description: Use when lifecycle states, events, guards, transitions, invalid actions, or recovery semantics change
category: best-practice
version: 1.2.0
---
# State Machine Design
## Authority
The machine-readable definition owns legal transitions. Runtime authority lives in `docs/loop-definition.json`; the state-machine review method is inlined below.
## Applicability
Apply to the `state-machine` risk tag or lifecycle review.
## Required Inputs
Read state/event definitions, guards, side effects, persistence, and recovery rules.
## Quality Criteria
Check reachability, determinism, terminal states, invalid transitions, guard completeness, idempotency, concurrency, recovery, versioning, and auditability.
## Outputs
Definition-aligned implementation or one state-machine conclusion.
## N/A Criteria
N/A when no lifecycle or transition semantics change.
## Stop Conditions
Stop on ambiguous authority, two legal next states, or unrecoverable persisted state.
## Non-Goals
Do not implement a second hidden state machine in prose.

## Operating Procedure
1. List durable states, event facts, actor/owner, guards, actions, persistence fields, and recovery semantics. Keep lifecycle state separate from DAG dependency ordering.
2. Build the transition table before code: `from state + event + guard -> to state + idempotent action`; record invalid events as rejected without mutation.
3. Decide terminal scope explicitly: top-level terminal, nested-phase handoff, or entity terminal. Each persisted state has one deterministic resume meaning.
4. Use revision/CAS protection for competing transitions and test legal reachability, rejection, retry/idempotency, interruption/recovery, and version migration.

## Evidence Checklist
- Transition table, guard/action ownership, persistence/revision model, and terminal-scope definition.
- Tests for all legal transitions plus representative invalid, duplicate, competing, and restart events.
- Recovery proof from every persisted state or a documented N/A state that cannot persist.

## Common Failure Modes
- An event name describes a command/intention while transition success is still uncertain.
- A nested handoff state is mistaken for a whole-loop terminal, or a hidden second state machine appears in prose/code.
- A guard has side effects, so a failed or retried transition leaves partial changes.

## Primary Sources
- [Project state-machine rule](../../docs/rules/state-machine.md)
- [State-machine reference](https://www.w3.org/TR/scxml/)

## Inlined Methodology

The Loop uses a layered state model: a top-level Loop state plus constrained phase machines for complex stages plus independent Agent/TASK/BUG lifecycles. The top-level state answers "after interruption, which engineering stage must resume"; the phase answers "within that stage, which guarded method step is currently active"; entity lifecycles answer "what may this specific Agent, TASK or BUG do now". Eleven top-level states with the single-direction main trunk `inactive -> planning -> document_verification -> building -> verification -> acceptance -> release_audit -> awaiting_human_release`, plus the `paused` and `aborted` terminals. Correction loops never bypass the trunk. Three constrained phase machines: `planning`, `verification`, `bug_resolution`. Agent lifecycle: `spawned -> reading -> understanding_submitted -> understanding_approved -> activated -> working -> reported -> done`, plus `blocked` and `stopped`. TASK lifecycle: `candidate -> reviewed -> locked -> in_progress -> review -> done`. Invalid transition policy: undefined or guard-failing events do not change state, execute no side effect, record a rejected event, report the failed guard. Idempotency uses CAS revision checks; one committed transition per runtime revision. The 16 preservation invariants (INV-001..INV-016) map to enforcement layers (schema, hook, guard, combined).
