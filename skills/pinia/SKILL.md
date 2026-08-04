---
name: pinia
description: Use when Pinia stores, shared client state, state actions, or persistence behavior change
category: best-practice
version: 0.3.0
---
# Pinia
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; server contracts retain authority over server state.
## Applicability
Apply to Pinia stores, cross-view client state, state-derived UI, or store persistence.
## Required Inputs
Read state ownership, lifecycle, consumers, server authority, and reset/logout behavior.
## Quality Criteria
Keep state ownership clear, mutations traceable, derived state stable, and stale data invalidated.
## Outputs
One store-safe implementation or scoped state-management review conclusion.
## N/A Criteria
N/A when no shared client state is created or changed.
## Stop Conditions
Stop on duplicate authority, circular store coupling, or insecure persistence.
## Non-Goals
Do not store server truth indefinitely without freshness and invalidation rules.

## Operating Procedure
1. Classify proposed state as server cache, cross-view client state, transient view state, or durable preference. Keep different lifecycle/authority classes separate.
2. Define one named store id and explicit state, getters, and actions. State mutations occur through the owning store's actions or documented patch path.
3. Design invalidation, reset, logout, tenant-switch, persistence, and SSR behavior before wiring consumers.
4. Consume reactive state directly or with `storeToRefs`, then test state transitions and stale-data removal.

## Evidence Checklist
- Store ownership, authority, persistence classification, and consumer list.
- Reset/invalidation behavior for session, tenant, and failed request boundaries.
- Tests proving reactive consumption and action outcomes without accidental shared state.

## Common Failure Modes
- A store becomes a permanent mirror of server truth with no freshness rule.
- Reactive store properties are destructured without `storeToRefs`.
- Router, injected service, secret, or request-specific object is persisted as store state.

## Primary Sources
- [Pinia store definition and reactivity](https://pinia.vuejs.org/core-concepts/)
- [Pinia testing cookbook](https://pinia.vuejs.org/cookbook/testing.html)
