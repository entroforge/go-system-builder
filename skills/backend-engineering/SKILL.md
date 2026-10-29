---
name: backend-engineering
description: Use when backend services, domain logic, transactions, concurrency, or error behavior changes
category: best-practice
version: 1.0.0
---
# Backend Engineering
## Authority
Quality guidance only. Stage routing lives in `docs/control/agent-protocol.md`. Apply this method to the actual assigned technical concern.
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
Resolve implementation details, transaction mechanisms and reversible diagnostics within the TASK. Report a real locked-specification gap, unavailable authoritative source or decision outside the existing authorization to Main; do not invent domain invariants, external effects or failure semantics.
## Non-Goals
Do not redesign APIs or migrations without their contracts.

## Implementation method
Trace the TASK's contract clauses to the service entry, domain operation and
persistence or external-effect boundary. Choose transaction and concurrency
mechanisms that preserve the specified invariants. Exercise success, rejection,
cancellation, retry and rollback paths that apply; inspect observable effects
and retain real check evidence in the supplied professional outputs.
