---
name: casbin-authorization
description: Use when Casbin models, policies, enforcement points, roles, or authorization decisions change
category: best-practice
version: 0.3.0
---
# Casbin Authorization
## Authority
Quality guidance only. Product authorization policy and `docs/rules/security.md` remain authoritative; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to Casbin models, policy storage, role relations, matcher functions, enforcers, or authorization middleware.
## Required Inputs
Read the authorization matrix, trust boundaries, identity claims, resource model, policy lifecycle, and audit requirements.
## Quality Criteria
Enforce server-side least privilege, make policy subjects and objects explicit, default deny, test allow and deny cases, and audit policy changes.
## Outputs
One authorization-safe implementation or scoped policy review conclusion.
## N/A Criteria
N/A when no authorization decision or enforcement path changes.
## Stop Conditions
Stop on client-only enforcement, ambiguous resource identity, privilege escalation, or an untestable deny path.
## Non-Goals
Do not use roles as a substitute for ownership or tenant-boundary checks.

## Operating Procedure
1. Define subject, domain/tenant, object, action, policy administration owner, and default-deny behavior from the authorization matrix.
2. Place enforcement after verified authentication and before any resource read/write or externally visible side effect.
3. Keep matcher logic deterministic and policy data reviewable; combine Casbin grants with ownership/tenant predicates required by the domain.
4. Test each operation's allow, deny, cross-tenant, stale-policy, and administration paths.

## Evidence Checklist
- Model/matcher version, policy source, request tuple definition, and enforcement-point map.
- Allow/deny/cross-tenant test matrix for protected operations.
- Policy update, cache/reload, audit, and rollback behavior.

## Common Failure Modes
- UI hides a control while the API endpoint remains unenforced.
- A role name encodes ownership and bypasses object/tenant validation.
- Policy reload makes a grant take effect unpredictably or without auditability.

## Primary Sources
- [Casbin overview](https://casbin.apache.org/docs/overview/)
- [Casbin RBAC model](https://casbin.apache.org/docs/rbac/)
