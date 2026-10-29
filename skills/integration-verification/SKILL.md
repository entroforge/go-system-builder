---
name: integration-verification
description: Use when two or more components, services, data stores, queues, caches, or frontend/backend paths interact
category: best-practice
version: 1.0.0
---
# Integration Verification
## Authority
The SYNC and component contracts own expected data flow. Stage routing lives in `docs/control/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
## Applicability
Apply to `cross-component` risk and integration responsibilities.
## Required Inputs
Read all participating contracts, environments, credentials boundaries, and failure semantics.
## Quality Criteria
Exercise real serialization, auth, data flow, transactions, timeouts, retries, errors, event ordering, caches, queues, rollback, and E2E paths.
## Outputs
Integration evidence with environment and observed results.
## N/A Criteria
N/A only when evidence proves no integration boundary exists.
## Stop Conditions
Stop the affected integration check when a critical boundary is mocked away or
the authoritative environment is unavailable; report the exact missing boundary
or prerequisite and actual attempts to Main, preserving observations and
continuing other safe assigned checks. Do not claim the omitted integration
verified. Real P0 or unsafe operations stop dangerous work with actual evidence
and capture gaps.
## Non-Goals
Do not substitute isolated unit tests for integration evidence.

## Review method

For each assigned boundary, trace the real producer, serialization, transport,
consumer and persistence or asynchronous effect. Compare both sides with the
actual contract. Exercise success, rejection, timeout, retry, duplicate delivery
and partial-failure paths that apply, including cleanup and recovery. State which
services were real or mocked and what those observations can and cannot prove.
Keep command/request outputs and observed state changes in the existing evidence
locations and reference them from the assigned answer.

Main owns complete-round coverage, scope, dependencies and independent dispatch
through the current ReviewPlan. Each Reviewer executes the assigned Claims and
boundaries, reporting uncovered surfaces to Main without adding a full-module
sweep to every Assignment. Browser, API, command and state-based behavior use
their actual product entry and method; browser assets apply only to browser work.
Record passing rejection, terminal state, side effects and recovery in
`ClaimResult.observed` and real evidence references. Only observed failed Claims
create Findings; continue other safe assigned checks after ordinary Findings.
Stop real P0 or dangerous operations with actual observations and capture gaps. Missing environments go to Main with actual attempts and missing
sources; they do not become product Findings without observed product failure.
This method neither creates a workgroup gate nor declares a clean round.
