# State Machine Rule

---
rule_id: R-STATE-01
category: Architecture
status: locked
owner: Project Manager / Architect
scope: states, flows, retry, dependency, terminal states, audit, concurrency
---

## 1. Rule

The state machine is the source of truth for lifecycle behavior.

Code must not hide undocumented states, transitions, terminal states, retry behavior, or dependency semantics.

## 2. Use When

Use this rule for orders, payments, files, uploads, approvals, sessions, subscriptions, async jobs, pipeline jobs, or any lifecycle entity.

## 2.1 Loop responsibility status

For progress of one Loop responsibility attempt, use only `pending`, `active`,
`blocked`, `done`, or `cancelled`. The definition is in the root `AGENTS.md` Work status section; this section
describes compatibility with existing protocols.
This vocabulary belongs to a responsibility attempt (owner + responsibility +
attempt), not to a TASK document, stage, result conclusion, workspace, team, or
platform session.

New installations default to Runtime 1.3, whose Agent, TASK, and S7
ReviewAssignment work-state fields use these five values. Existing Runtime 1.1
and 1.2 instances remain on their compatibility/read path in their original
format; do not rewrite them in place. Approval, activation, and other original
event evidence remain separate, and `active` alone grants no write authority.

Do not ask an Agent to author a generic status. Follow the current Hook/Runtime
projection and its named action; use the original receiver to submit the result.
Other persisted lifecycle fields and CLI payloads retain their exact wire
values where required. In legacy Runtime 1.1/1.2, preserve the original Agent
and TASK format; interpret every compatibility value below as a source fact,
not an alternative work state:

| Existing value or field | Keep its actual meaning | Do not infer |
| --- | --- | --- |
| document/TASK-plan `complete` | The planning artifact is written and eligible for its original review/gate. | Implementation responsibility is `done`. |
| REQ downstream index `draft` / `executing` / `completed`, and evidence rows such as `pending` / `PASS` | Existing artifact or evidence tracking labels in the REQ template. | A generic responsibility state; use the live original-owner receipt/projection for that. |
| `ready` | A resource, context, or assignment passed its preparation/readiness check. | Work has started or finished. |
| `accepted` / `responsibility_status` | Original receipt/domain disposition fields. | A standalone Loop work state; derive current responsibility progress from the original receiver and validity facts. |
| `work_complete` | Existing compatibility projection for current owner proof. | Stage passed, integration finished, or another owner's work is complete. |
| `execution_may_end` / `consumer_pending` | Read-only stop/idle and downstream-wait projections. | Responsibility states or Agent-authored answers. |
| Gateway `completed work` | Existing human-readable evidence inventory in the Gateway package. | An automatic `done` state without original receipt and current own checks. |
| assignment `consumed` | Existing consumer checkpoint in the dispatch protocol. | A general status an Agent should write. Project the responsibility through the original receipt and current checks. |
| Builder Result `status=completed`, platform `completed`, or Runtime legacy lifecycle values | Exact existing protocol/schema/platform values, where still required. | The five-state vocabulary or permission to bypass its receiver. |
| Workspace `ExecutionRegistry` lifecycle `complete` | Existing execution resource lifecycle fact. Legacy Runtime Agent/TASK fields are read only through the compatibility boundary; new 1.3 fields use the five states. | The responsibility-attempt state or an Agent-authored substitute. |
| `closed` | A domain entity's closure (for example a BUG, decision, or review round). | The owning Agent responsibility is `done`. |

For a human-readable report, state the responsibility status separately from
the stage cursor, professional outcome, receipt/consumer, and platform session
facts. `done` means the current original receiver accepted the required
delivery and the responsibility's own checks are satisfied. A missing consumer
or stage gate remains with that consumer; it does not reopen the Agent's work.

## 3. Hard Rules

- Each entity has exactly one initial state.
- Each state chain has explicit terminal states.
- Undefined transitions are rejected and recorded.
- Transitions are idempotent, or non-idempotency is documented.
- Every transition writes audit evidence.
- New states require entry condition, exit condition, and control-plane explanation.

## 4. Required Sections

State docs must include:

- state diagram
- state definition table
- transition event table
- invalid transition behavior
- concurrency control
- timeout and compensation
- audit log
- monitoring metrics
- test matrix

## 5. Audit Fields

| Field | Meaning |
|:---|:---|
| entity_id | entity identifier |
| old_state | previous state |
| new_state | next state |
| event | triggering event |
| actor | user or system |
| request_id | trace id |
| occurred_at | timestamp |
| metadata | context |

## 6. Gates

- No locked state machine, no implementation contract for lifecycle logic.
- State machine changes require test matrix updates.
- Terminal state, retry, or dependency changes require release architecture audit.

## 7. Forbidden

- `NULL`, magic datetimes, or magic strings as wait reasons
- infinite `pending` / `processing`
- logs as the only explanation for blocked state

