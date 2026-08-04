---
name: backend-engineering
description: Use when backend services, domain logic, transactions, concurrency, or error behavior changes
category: best-practice
version: 1.0.0
---
# Backend Engineering
## Authority
Quality guidance only. Stage routing lives in `docs/agent-protocol.md`; Skill routing authority comes from the current assignment and risk tags; the reusable method summary is inlined below.
## Applicability
Apply to backend paths and the `backend` risk tag.
## Required Inputs
Read BE contract, domain/state design, persistence boundaries, and operational constraints.
## Quality Criteria
Check layering, invariants, transaction boundaries, error propagation, cancellation, concurrency ownership, idempotency, observability, and failure recovery.
## Outputs
Implementation choices or one backend quality conclusion with evidence.
## N/A Criteria
N/A when no server-side executable behavior changes.
## Stop Conditions
Stop on undefined domain invariant, side effect, or failure contract.
## Non-Goals
Do not redesign APIs or migrations without their contracts.

## Inlined Methodology

Loop Engineering uses two Skill categories: Methodology (reusable procedure for an engineering event, including entry, steps, output and stop conditions) and Best Practice (professional quality criteria and review techniques for one technical concern). Role identity is not a Skill; Builder, Document Verifier, Delivery Verifier, QA and E2E Browser identities belong to Agent Definitions. An Agent gains capability by combining one role definition, one task instance and the smallest applicable Skill set. Skill files must reference source documents by path. They must not copy REQ, contract, TASK, BUG or report bodies. Routing order: current runtime state and phase -> pending legal event -> one primary Methodology Skill -> Agent Definition kind -> task artifact and risk tags -> smallest applicable Best-practice set -> two-phase activation when a subagent is involved. Best-practice selection maps the `backend` risk tag to the `backend-engineering` Skill. Composition rules: one workflow event that requires reusable procedure has exactly one primary Methodology Skill; at most two secondary Methodology dependencies should be active for one action. Two Skills must not own the same transition decision or evidence conclusion. Anti-patterns include `builder`, `verifier` or `qa` as a general-purpose role Skill; a Skill that reads or writes current Loop state as its own authority; a Skill containing a second state machine; copied REQ/contract/TASK/BUG content; and hidden dependencies expressed only in prose or chat.
