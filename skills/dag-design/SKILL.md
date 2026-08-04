---
name: dag-design
description: Use when directed acyclic graphs, dependency ordering, workflow execution, topological scheduling, or cycle handling change
category: best-practice
version: 0.3.0
---
# DAG Design
## Authority
Quality guidance only. Legal Loop transitions remain in `docs/loop-definition.json`; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to dependency graphs, build graphs, workflow scheduling, task ordering, graph persistence, or cycle validation.
## Required Inputs
Read node and edge semantics, ownership, ordering guarantees, failure policy, persistence model, and scale constraints.
## Quality Criteria
Make edge direction and node identity explicit, reject cycles before execution, define missing/deleted-node behavior, preserve deterministic scheduling, and retain provenance.
## Outputs
One acyclic graph design or a scoped DAG review conclusion.
## N/A Criteria
N/A when the change has no dependency graph or ordering semantics.
## Stop Conditions
Stop on ambiguous edge meaning, undetected cycle, nondeterministic dependency resolution, or unrecoverable partial execution.
## Non-Goals
Do not model cyclic state transitions as a DAG; use `state-machine-design` for lifecycle semantics.

## Operating Procedure
1. Define node identity, input/output, owner, retry/timeout, side effect, idempotency key, and terminal outcomes before connecting nodes.
2. State edge direction and eligibility in one sentence. Validate all node references and reject a mutation that creates a cycle, returning the cycle path.
3. Specify deterministic ordering for equally eligible work and branch/join behavior for success, failure, skip, cancel, and retry.
4. Persist graph revision and run/node-attempt provenance, then test topological execution, interruption/resume, and partial-failure recovery.

## Evidence Checklist
- Node/edge schema, graph revision, edge-direction sentence, and cycle-validation result.
- Eligibility/branch/join matrix including terminal failures and skips.
- Run/attempt audit record and deterministic scheduling/resume test results.

## Common Failure Modes
- "Depends on" has no declared direction and different callers interpret it differently.
- A graph update accepts dangling edges or discovers a cycle only during execution.
- Retry repeats a non-idempotent side effect without an idempotency/compensation design.

## Primary Sources
- [Apache Airflow DAG concepts](https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/dags.html)
- [Project state-machine rule](../../docs/rules/state-machine.md)
