---
name: gorm
description: Use when GORM models, queries, associations, transactions, migrations, or persistence behavior change
category: best-practice
version: 0.3.0
---
# GORM
## Authority
Quality guidance only. Data contracts and migration policy remain authoritative in the locked design and `docs/agent-protocol.md`.
## Applicability
Apply to GORM models, queries, associations, transactions, hooks, migrations, or persistence performance.
## Required Inputs
Read schema and migration design, query intent, transaction boundary, indexes, and data-retention constraints.
## Quality Criteria
Make query scope explicit, bound transactions, avoid accidental writes and N+1 behavior, preserve integrity, and verify migration paths.
## Outputs
One persistence-safe implementation or scoped GORM review conclusion.
## N/A Criteria
N/A when no GORM-managed model, query, or migration changes.
## Stop Conditions
Stop on unsafe migration, unspecified transaction semantics, or unbounded query work.
## Non-Goals
Do not infer schema compatibility without the database-change practice.

## Operating Procedure
1. Start from the data-model/migration contract: keys, tenancy, constraints, query cardinality, and consistency boundary.
2. Compose scoped queries with context and server-side predicates before user filters. Select/preload only declared data and bound every list result.
3. Use `db.Transaction` for atomic domain changes; use the supplied `tx` for every operation inside it and define retry/idempotency around external side effects.
4. Inspect generated SQL/query count and test rollback, not-found, conflict, soft-delete, and migration paths.

## Evidence Checklist
- Query predicate, index/cardinality expectation, and generated-SQL or query-count review for hot/list paths.
- Transaction boundary and rollback test for each multi-write invariant.
- Migration/`AutoMigrate` decision, forward/backward compatibility, and database-change evidence.

## Common Failure Modes
- A transaction starts on `tx` but later writes accidentally use `db`.
- Authorization/tenant filtering is optional or applied after pagination.
- Association preload introduces N+1 work or a list query has no explicit bound/order.

## Primary Sources
- [GORM transactions](https://gorm.io/docs/transactions.html)
- [GORM context](https://gorm.io/docs/context.html)
