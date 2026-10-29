---
name: code-quality
description: Use when code is authored or reviewed for maintainability, reuse, abstraction, hardcoding, readability, or complexity
category: best-practice
version: 1.0.0
---
# Code Quality
## Authority
Quality guidance only. Stage routing lives in `docs/control/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
## Applicability
Apply to code changes, QA module-code, and reuse/abstraction responsibilities.
For an S8 Investigator, apply these methods only to the assigned hypothesis and
causal discriminator; automatic loading does not create a ReviewPlan responsibility.
S6 implementation and S7 review retain their original obligations.
## Required Inputs
For implementation/review, read the code in the assigned change or review scope,
local conventions, tests, public contracts and dependency boundaries. For S8,
read the issued hypothesis, frozen source evidence and code/contracts relevant
to its discriminator; no code change is required.
## Quality Criteria
Check correctness clarity, naming, cohesion, dependency direction, duplication, useful reuse, abstraction timing, hardcoding, magic values, oversized units, dead/debug code, and error handling.
## Outputs
For implementation/review, maintainable implementation or one auditable quality
conclusion in the assigned artifact. For S8, record only the hypothesis's actual
method, observations and evidence in the original HypothesisResult; no separate
QA conclusion or report is required.
## N/A Criteria
For implementation/review, N/A only when no executable or configuration code is
in scope. For S8, explain in the original HypothesisResult when no code-quality
method is relevant to the assigned discriminator; absence of a code change alone
does not decide applicability.
## Stop Conditions
For S6 implementation, fix defects found by your own checks within the authorized
TASK and preserve its acceptance oracle. Escalate unrelated refactoring, scope
expansion or a required contract decision to Main. For independent review, report
findings without repairing the product; use the original stage route for repair.
## Non-Goals
Do not optimize for fewer lines or speculative abstraction.

## Review method

For S6 implementation, trace the assigned TASK, its Done when conditions and
your own Plan assertions through the implementation and caller, using the linked
contracts as the acceptance authority. Run actual self-checks and correct in-scope
defects before delivery; this work continues the current TASK. For S7 review, trace
the assigned ReviewPlan Claim from its actual contract to implementation and caller.
For S8 investigation, trace only the assigned hypothesis/discriminator through the relevant contract, implementation
and caller; do not expand it into full-chain validation or unrelated review.
Inspect normal and failure paths, state changes, ownership, error propagation and
boundary conversions. Explain a concrete correctness or maintenance consequence
for each finding; name the entry point and the first broken invariant. Distinguish
useful shared behavior from coincidental similarity before proposing abstraction.
For independent review work, preserve evidence in the assigned answer instead of
editing the product. Reviewer-discovered repair follows Main's original stage route;
Builder self-check corrections within the current TASK do not require a new S9 route.

For S6, use the original TASK, Done when, your own Plan assertions, contracts and
assigned scope; this skill does not require a future S7 ReviewPlan. For S7, use the current ReviewPlan's Claims,
scope and independence constraints. In S8, record the hypothesis's actual observations and
evidence in its original HypothesisResult, without modifying product or formal
tests, adopting module test ratios or requiring test rewrites. Main owns
specialist selection and compatible grouping under team-planning; this method does
not create another actor-count, file-count or dispatch gate.
