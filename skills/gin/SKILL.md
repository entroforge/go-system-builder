---
name: gin
description: Use when Gin routes, handlers, middleware, request binding, recovery, or HTTP service behavior change
category: best-practice
version: 0.3.0
---
# Gin
## Authority
Quality guidance only. Locked HTTP semantics remain in the API contract; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to Gin routers, handlers, middleware, request binding, validation, recovery, or response serialization.
## Required Inputs
Read API contracts, route topology, middleware order, auth policy, validation rules, and error model.
## Quality Criteria
Keep handlers thin, bind and validate explicitly, preserve cancellation, order middleware deliberately, and return contract-aligned errors.
## Outputs
One Gin-safe implementation or scoped handler and middleware review conclusion.
## N/A Criteria
N/A when no Gin HTTP serving path is affected.
## Stop Conditions
Stop on unhandled panic behavior, ambiguous middleware order, or a handler that owns undeclared domain rules.
## Non-Goals
Do not use framework binding defaults as the sole security boundary.

## Operating Procedure
1. Map each route to contract operation, handler, middleware order, authentication/authorization point, and project error writer.
2. Bind from the expected source and validate before application execution. On failure, write one contract-shaped error and stop the handler path.
3. Translate request to an application command using `c.Request.Context()`; map typed results/errors to the HTTP contract without domain/persistence logic in the handler.
4. Test binding failure, middleware order, cancellation, panic recovery, auth denial, and no-double-write response behavior.

## Evidence Checklist
- Route/middleware order and ownership map.
- Binding/validation and error-envelope tests for each input source.
- Context cancellation and recovery behavior for any downstream work.

## Common Failure Modes
- `Bind` writes a response but the handler continues and writes again.
- Middleware order exposes a handler before identity or correlation is established.
- Handler embeds database transactions or policy decisions that bypass the application layer.

## Primary Sources
- [Gin binding and validation](https://gin-gonic.com/en/docs/examples/binding-and-validation/)
- [Gin middleware](https://gin-gonic.com/en/docs/examples/using-middleware/)
