---
name: jwt-authentication
description: Use when JWT issuance, validation, claims, token transport, key rotation, or authentication middleware change
category: best-practice
version: 0.3.0
---
# JWT Authentication
## Authority
Quality guidance only. Security policy remains in `docs/rules/security.md`; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to JWT token creation or validation, claims, signing keys, token transport, refresh flows, or authentication middleware.
## Required Inputs
Read identity design, trust boundary, issuer/audience rules, key management, revocation/expiry policy, and downstream authorization requirements.
## Quality Criteria
Validate all registered claims required by the contract, pin accepted algorithms, rotate keys deliberately, minimize claims, and separate authentication from authorization.
## Outputs
One authentication-safe implementation or scoped JWT review conclusion.
## N/A Criteria
N/A when no JWT or bearer-token authentication behavior changes.
## Stop Conditions
Stop on algorithm confusion risk, missing expiry policy, secret exposure, or authorization inferred solely from unverified claims.
## Non-Goals
Do not treat a valid token as automatic permission for a resource.

## Operating Procedure
1. Choose token transport, issuer/audience, accepted algorithms/key source, required claims, expiration/clock skew, and access/refresh/logout lifecycle before implementation.
2. At verification, select allowed keys/algorithms from trusted issuer configuration, verify signature and all required claims, then construct a minimal authenticated principal.
3. Rotate signing keys and refresh tokens with explicit overlap/revocation rules; store only the state necessary for the chosen revocation model.
4. Pass the verified principal to Casbin/domain authorization and test malformed, expired, wrong-issuer/audience, rotated, revoked, and logout cases.

## Evidence Checklist
- Token-profile table: transport, issuer, audience, algorithm, key id/source, claim set, TTL, refresh, revocation, and logout semantics.
- Validation matrix covering bad signature, algorithm/key mismatch, claim failures, expiry, and clock skew.
- Key rotation and refresh/revocation test evidence; no tokens or credentials in logs.

## Common Failure Modes
- Header-selected algorithm/key acceptance enables algorithm or key confusion.
- A valid signature is accepted without issuer/audience/type/expiry validation.
- JWT roles are treated as final resource authorization without server-side policy/ownership checks.

## Primary Sources
- [RFC 8725 JSON Web Token Best Current Practices](https://datatracker.ietf.org/doc/html/rfc8725)
- [RFC 7519 JSON Web Token](https://www.rfc-editor.org/rfc/rfc7519)
