---
name: domain-driven-design
description: Use when domain boundaries, aggregates, value objects, domain events, or ubiquitous language change
category: best-practice
version: 0.3.0
---
# Domain-Driven Design
## Authority
Quality guidance only. The locked REQ and design own product semantics; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to domain models, bounded contexts, aggregates, invariants, domain services, or domain events.
## Required Inputs
Read the REQ, domain vocabulary, architecture and data-model design, consistency requirements, and external boundaries.
## Quality Criteria
Make domain language explicit, protect aggregate invariants, separate bounded contexts, and make consistency choices observable.
## Outputs
One domain-model decision or a scoped domain-design review conclusion.
## N/A Criteria
N/A when the change is purely mechanical and does not alter domain ownership or rules.
## Stop Conditions
Stop on undefined invariant, mixed bounded-context vocabulary, or an aggregate with unbounded consistency scope.
## Non-Goals
Do not force tactical DDD patterns onto simple CRUD behavior without a domain reason.

## Operating Procedure
1. Extract business behaviors, terms, invariants, and decision owners from the REQ before naming packages, entities, or services.
2. Classify the work: simple CRUD stays simple; introduce value objects, aggregates, domain events, or bounded contexts only for a named invariant or language boundary.
3. Define aggregate transaction scope and cross-context integration semantics, including idempotency and failure ownership.
4. Record vocabulary and boundary decisions in the design/ADR surface, then prove each invariant with tests.

## Evidence Checklist
- Ubiquitous-language terms, invariant owner, and bounded-context/context-map decision.
- Aggregate transaction boundary and cross-aggregate or external-event consistency choice.
- Tests demonstrating accepted and rejected invariant transitions.

## Common Failure Modes
- Entity names reuse one word with different meanings across contexts.
- Aggregates span unbounded collections or remote calls to preserve a transaction illusion.
- Folder-level "DDD" patterns add indirection without an owned business rule.

## Primary Sources
- [Bounded Context](https://martinfowler.com/bliki/BoundedContext.html)
- [Domain-Driven Design reference](https://www.domainlanguage.com/ddd/reference/)
