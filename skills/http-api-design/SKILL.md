---
name: http-api-design
description: Use when HTTP resources, methods, status codes, headers, pagination, or HTTP error semantics change
category: best-practice
version: 0.3.0
---
# HTTP API Design
## Authority
Quality guidance only. Locked interface meaning remains in the applicable contract; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to HTTP request and response behavior, resource design, headers, status codes, caching, pagination, or errors.
## Required Inputs
Read the API contract, consumers, authentication model, compatibility policy, and error taxonomy.
## Quality Criteria
Use explicit resource semantics, validated input, stable error behavior, correct status codes, and compatible evolution.
## Outputs
One contract-aligned HTTP design or a scoped API review conclusion.
## N/A Criteria
N/A when no HTTP boundary is changed.
## Stop Conditions
Stop on ambiguous side effects, undocumented compatibility impact, or authorization semantics that cannot be expressed.
## Non-Goals
Do not replace shared schema compatibility review owned by `api-contracts`.

## Operating Procedure
1. Define resource identity, operation intent, request/response representation, authorization rule, and side effect before choosing URI and method.
2. Choose HTTP method/status/header semantics; explicitly state idempotency, pagination ordering, conditional/caching behavior, and async completion if clients observe them.
3. Validate request boundary inputs and return the shared error envelope with a stable application code.
4. Update the locked contract/OpenAPI together and test successful, malformed, unauthorized, conflict, and retry behavior.

## Evidence Checklist
- Method/URI/status table plus request/response/error schemas.
- Authorization and idempotency/retry statement for every mutating operation.
- Compatibility impact and consumer test or contract-test evidence.

## Common Failure Modes
- `POST` is used to hide an operation whose retry or resource semantics are undefined.
- A 2xx response embeds an application failure without a documented reason.
- Offset pagination lacks stable ordering or authorization is checked after resource lookup.

## Primary Sources
- [RFC 9110 HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110)
- [Project API design rule](../../docs/rules/api-design.md)
