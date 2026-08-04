---
name: security-review
description: Use when authentication, authorization, trust boundaries, secrets, dependencies, or sensitive data handling changes
category: best-practice
version: 1.0.0
---
# Security Review
## Authority
Quality guidance within assigned scope. Stage routing lives in `docs/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
## Applicability
Apply to the `security` risk tag.
## Required Inputs
Read threat boundaries, identities, permissions, data classification, dependencies, and logging paths.
## Quality Criteria
Check validation, authn/authz, least privilege, injection, XSS/CSRF, secret exposure, dependency risk, sensitive logs, abuse cases, and fail-closed behavior.
## Outputs
One independent security conclusion and reproducible findings.
## N/A Criteria
N/A only when no trust boundary or sensitive asset changes.
## Stop Conditions
Stop and escalate on exploitable critical findings or missing threat authority.
## Non-Goals
Do not repair findings while acting as reviewer.

## Inlined Methodology

Team planning uses a mandatory responsibility baseline plus risk-triggered responsibilities plus scope partitioning plus conflict-graph constraints yielding one single-responsibility assignment per teammate. Agent count is the number of resulting assignments, not a fixed target. QA responsibilities include `QA-SECURITY`; this maps to the security-review scope. Risk-tag derivation: auth, permission or sensitive data -> `security`; API, event or shared schema -> `api`, `cross-component`; shared package or multi-module design -> `architecture`; existing behavior or historical BUG touched -> `regression`. Scope partitioning rules: distinct write ownership or repository boundary; independently testable Closing Contracts. Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, security/release/permission enforcement needs independent evidence, scopes require incompatible Skills or tools, or combining them would exceed workload limits. One assignment may not exceed 30 files or 3 material modules. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, and non-stale teammate state. QA workgroup gate must cover every triggered architecture, security, performance, reliability, and migration responsibility.
