---
name: database-change
description: Use when schemas, queries, migrations, persisted formats, transactions, or data integrity rules change
category: best-practice
version: 1.0.0
---
# Database Change
## Authority
The data model and migration contract own persisted semantics. Stage routing lives in `docs/agent-protocol.md`; Skill routing authority comes from the current assignment and risk tags; the reusable method summary is inlined below.
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
Stop on irreversible unapproved migration or undefined rollback.
## Non-Goals
Do not treat application fallback as data rollback.

## Inlined Methodology

Loop Engineering uses two Skill categories: Methodology (reusable procedure for an engineering event, including entry, steps, output and stop conditions) and Best Practice (professional quality criteria and review techniques for one technical concern). Role identity is not a Skill; Builder, Document Verifier, Delivery Verifier, QA and E2E Browser identities belong to Agent Definitions. An Agent gains capability by combining one role definition, one task instance and the smallest applicable Skill set. Skill files must reference source documents by path. They must not copy REQ, contract, TASK, BUG or report bodies. Best-practice selection maps the `database` risk tag to the `database-change` Skill. Routing order: current runtime state and phase -> pending legal event -> one primary Methodology Skill -> Agent Definition kind -> task artifact and risk tags -> smallest applicable Best-practice set -> two-phase activation when a subagent is involved. Composition rules: one workflow event that requires reusable procedure has exactly one primary Methodology Skill; at most two secondary Methodology dependencies should be active for one action. Anti-patterns include `builder`, `verifier` or `qa` as a general-purpose role Skill; a Skill that reads or writes current Loop state as its own authority; a Skill containing a second state machine; copied REQ/contract/TASK/BUG content; and Best Practices loaded globally without applicability.
