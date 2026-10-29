---
name: database-change
description: Use when schemas, queries, migrations, persisted formats, transactions, or data integrity rules change
category: best-practice
version: 1.0.0
---
# Database Change
## Authority
The data model and migration contract own persisted semantics. Stage routing lives in `docs/control/agent-protocol.md`. Apply this method to the actual assigned technical concern.
## Applicability
Apply to `database` or `migration` risk.
## Required Inputs
Read model, migration, compatibility, rollout, rollback, volume, and backup constraints.
## Quality Criteria
Check forward/backward compatibility, constraints, transactionality, indexes, query plans, locking, data conversion, resumability, rollout, rollback, and integrity verification.
## Outputs
Migration implementation or one independent migration conclusion with evidence.
## N/A Criteria
N/A when no persisted production format or query behavior changes.
## Stop Conditions
Develop and test migrations on isolated data within the approved model. Do not execute an unauthorized irreversible migration. Escalate a missing required compatibility/recovery policy or necessary model change; ordinary migration mechanics and reversible checks remain engineering work.
## Non-Goals
Do not treat application fallback as data rollback.

## Implementation method
Trace the assigned data change through readers, writers, constraints and rollout
order. Choose migration and transaction mechanisms within the approved model;
inspect query plans and locking where relevant. Exercise forward/backward
compatibility, rollback or the specified recovery strategy, partial failure and
integrity checks on isolated data. Record actual evidence and unresolved risks
in the supplied professional outputs.
