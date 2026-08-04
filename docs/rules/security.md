# Security Rule

---
rule_id: R-SEC-01
category: Security
status: locked
owner: Project Manager / Architect / Security
scope: auth, authorization, I/O, secrets, audit, dependencies, data security
---

## 1. Rule

Security-sensitive changes block release unless they are documented, testable, and reviewable.

## 2. Hard Rules

- Do not commit secrets, tokens, passwords, certificates, or private keys.
- Validate all external input.
- Use TLS for sensitive data in transit.
- Enforce authorization server-side.
- Audit sensitive operations.
- Production data export, repair, or backfill requires approval and masking policy.

## 3. Checklist

| Area | Requirement | Evidence |
|:---|:---|:---|
| authn | login, token, session strategy defined | tests + review |
| authz | RBAC/ABAC/ACL or equivalent defined | permission matrix tests |
| input | params, files, callbacks validated | tests + scan |
| output | XSS protection and sensitive-field masking | E2E + review |
| data | encryption, backup, retention defined | architecture review |
| dependencies | no critical vulnerabilities | SCA scan |
| audit | actor, time, object, result traceable | logs / DB evidence |

## 4. Approval Required

Security or PM / Architect approval is required for:

- authn/authz model
- secrets, certificates, token lifecycle
- audit fields or audit storage
- encryption, masking, retention
- production data repair or export

## 5. Forbidden

- frontend-only authorization checks
- logging secrets, tokens, identity numbers, bank cards, or sensitive personal data
- releasing with unresolved critical security findings

