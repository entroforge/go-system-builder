---
name: openapi-swagger
description: Use when OpenAPI or Swagger specifications, generated clients, API documentation, or schema publication change
category: best-practice
version: 0.3.0
---
# OpenAPI and Swagger
## Authority
Quality guidance only. The locked API contract remains authoritative; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to OpenAPI documents, generated server or client code, schema components, documentation publication, or contract validation.
## Required Inputs
Read API contracts, published specification, generation configuration, consumers, and compatibility policy.
## Quality Criteria
Keep the specification executable, schema reuse intentional, generation reproducible, and published contracts synchronized with behavior.
## Outputs
One valid specification update or scoped API-documentation review conclusion.
## N/A Criteria
N/A when no OpenAPI/Swagger artifact or generated boundary is affected.
## Stop Conditions
Stop on generated/manual drift, incompatible published change, or undocumented security scheme.
## Non-Goals
Do not treat an OpenAPI file as proof that runtime behavior is implemented.

## Operating Procedure
1. Start from the locked contract, then model every operation's parameters, body, response variants, security, and stable operation id.
2. Reuse schema components only for one stable semantic meaning; define required/nullability/type constraints explicitly rather than relying on tool defaults.
3. Validate OAS syntax and references, generate/check derived artifacts deterministically, and compare runtime behavior with published schemas.
4. Record any intentional OAS limitation in the contract and cover it with runtime tests.

## Evidence Checklist
- OAS validation output and changed operation/component inventory.
- Generated artifact provenance, version, and drift check.
- Contract/runtime checks for success, validation, auth, and error responses.

## Common Failure Modes
- Generated files are hand-edited and diverge from the specification.
- `format` is assumed to validate data without an explicit validator.
- Similar but semantically different fields reuse a schema component and couple future changes.

## Primary Sources
- [OpenAPI Specification](https://spec.openapis.org/oas/latest.html)
- [OpenAPI interoperability guidance](https://swagger.io/specification/)
