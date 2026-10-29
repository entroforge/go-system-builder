---
name: security-review
description: Use when authentication, authorization, trust boundaries, secrets, dependencies, or sensitive data handling changes
category: best-practice
version: 1.0.0
---
# Security Review
## Authority
Quality guidance within assigned scope. Stage routing lives in `docs/control/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
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

## Review method

Trace untrusted inputs across authentication, authorization, storage and outbound
boundaries. Check both allowed and rejected identities, tenant/object ownership,
validation before side effects, failure defaults and sensitive-data exposure.
Use the actual threat model and dependency context; state the observed exploit
condition and affected boundary with evidence, without repairing the reviewed
product. Stop unsafe proof attempts and preserve the observed P0 and capture gaps.

Main owns independent assignment and grouping under the current ReviewPlan. This
method adds no actor count, file cap, activation or second security report. Missing
threat authority or inaccessible evidence is a concrete Main recovery gap, not
proof that the product passed or an excuse to invent a vulnerability.
