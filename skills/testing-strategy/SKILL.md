---
name: testing-strategy
description: Use when executable behavior changes or unit, integration, regression, or failure-path evidence is reviewed
category: best-practice
version: 1.0.0
---
# Testing Strategy
## Authority
Tests prove contract behavior; they do not redefine it. Stage routing lives in `docs/control/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable testing method is inlined below.
## Applicability
Apply to `behavior-change`, QA unit-test, and QA integration-test responsibilities.
For an S8 Investigator, apply these methods only to the assigned hypothesis and
causal discriminator. Automatic loading does not create S6 implementation or S7
review responsibilities; those stages retain their original obligations.
## Required Inputs
For S6 implementation, read the TASK, Done when, your own Plan assertions and
linked acceptance contracts, changed paths, failure modes and integration boundaries.
For independent S7 review, read the assigned ReviewPlan Claims, their acceptance
clauses, Closing Contract, frozen subjects and independence constraints. For S8, read the issued question,
its source evidence, authoritative expectation and relevant failure boundary.
## Quality Criteria
For S6 implementation and S7 review, map applicable
`source_refs → Rule → CASE → Story → flow → Spec → Evidence`, including PATH for
browser behavior. API, command and state-based behavior use their actual operation
and state assertions without inventing browser assets. Within the assigned scope, cover every required
allow/reject branch, boundaries, errors, states, side effects, rollback, retries,
regressions, and meaningful assertions. Enforce the module positive/negative ratio
and verify tests fail for the intended defect.
For S8, choose observations and safe controls that distinguish the assigned causal
hypothesis from alternatives. Do not impose ReviewPlan Claims, module ratios,
full-chain validation or formal-test rewriting on this read-only investigation.
## Outputs
For implementation/review, test plan, executable evidence, coverage gaps, or one
scoped QA conclusion in the assigned artifact. For S8, record the relevant method,
actual observations, evidence and gaps in the original HypothesisResult; no
separate test plan or QA report is required.
## N/A Criteria
For implementation/review, N/A only with evidence that no executable behavior
changed. For S8, explain when the assigned discriminator has no relevant testing
method or executable check in the original HypothesisResult; absence of product
changes alone does not decide it.
## Stop Conditions
For implementation responsibilities, improve weak assertions, reveal dependencies
and diagnose failing tests within the assigned scope. Correct defects found by
your own S6 checks as part of the current TASK; no new S9 route is required for
these in-scope corrections. Preserve the specified oracle and scope. For read-only
review or S8 investigation, report weak assertions and misleading green tests with honest
evidence instead of editing formal tests. Report an acceptance oracle that needs
a specification decision or unavailable authoritative inputs to Main; never
fabricate PASS. S7 retains its full assigned review obligations. Main organizes complete-round coverage; a Reviewer does not repeat an unassigned full module sweep. Ordinary Findings continue safe assigned work; real P0 or dangerous operations stop with actual observations and gaps. Passing expected rejection belongs in ClaimResult.observed and real evidence references, not a Finding.
## Non-Goals
Do not equate line coverage with behavioral completeness.

## Review method

For S6 implementation, derive tests from TASK Done when, your own Plan assertions
and the linked acceptance clauses and failure model. For independent S7 review,
derive checks from the assigned ReviewPlan Claims and their original acceptance
clauses and failure model. In either responsibility, inspect
whether their assertions can distinguish the intended defect from a healthy run.
Check boundary values, error propagation, state transitions, side effects and
recovery as well as the happy path. Verify relevant regression behavior without
reducing the current module obligation to only the triggering requirement. Explain
unexecuted work and any mocked boundary honestly; coverage counts do not establish
semantic correctness.

For S8, assess only whether existing checks or safe isolated controls distinguish
the issued hypothesis's predicted support/refute outcomes. Read tests and run
existing checks as useful, preserving product, formal tests, locked specifications
and formal configuration. Report a missing or misleading assertion as a causal
observation or detection gap; its correction belongs to the approved repair
route. Do not rewrite tests to finish the investigation.

Record actual methods, observations, evidence and coverage gaps in the current
professional answer or its referenced existing artifacts. Main owns responsibility
grouping and dependencies under the original TASK, Plan and contracts in S6,
and review grouping, dependencies and actual independence under the current
assigned ReviewPlan in S7. Builder self-checks do not replace independent review.
S6 does not require a future S7 ReviewPlan. Main owns Case-wide synthesis and follow-up dispatch in S8; this method
does not add actor-count, file-count, activation or repeat-report requirements.
