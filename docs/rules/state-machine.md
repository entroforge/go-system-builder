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

