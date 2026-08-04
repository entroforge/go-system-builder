---
name: typescript-type-safety
description: Use when TypeScript types, compiler configuration, type boundaries, or type-check failures change
category: best-practice
version: 0.3.0
---
# TypeScript Type Safety
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; the locked contract owns externally visible semantics.
## Applicability
Apply to TypeScript source, generated declaration boundaries, compiler settings, or type-check failures.
## Required Inputs
Read the affected TypeScript configuration, public types, runtime validation boundary, and type-check command.
## Quality Criteria
Preserve sound public boundaries, avoid unsound escape hatches, and make the project type check deterministically.
## Outputs
One type-safe implementation or a scoped type-safety review conclusion with command evidence.
## N/A Criteria
N/A when no TypeScript code, declarations, or compiler configuration is affected.
## Stop Conditions
Stop on an unmodelled runtime input, unresolved type-check failure, or incompatible public type change.
## Non-Goals
Do not use types to invent runtime behavior or replace runtime validation.

## Operating Procedure
1. Identify the type boundary: public API, component props/emits, store state, external input, generated declaration, or compiler configuration.
2. Select the narrowest truthful model. Default new code to `strict`; model absence and variants explicitly, with discriminated unions when states differ.
3. Validate unknown runtime data before it enters trusted types. Any assertion or compiler opt-out records its scope, reason, owner, and removal condition.
4. Run the repository type-check command and review public declaration changes as compatibility changes.

## Evidence Checklist
- Changed `tsconfig`/project-reference scope and the exact type-check command.
- Runtime validation location for each untrusted boundary.
- Compatibility assessment for exported types, serialized data, and generated declarations.

## Common Failure Modes
- `any`, broad assertions, or `@ts-ignore` hide a boundary mismatch.
- Optional fields encode mutually exclusive states and allow impossible combinations.
- Passing type-check evidence is presented as proof of runtime input validation.

## Primary Sources
- [TypeScript strictness](https://www.typescriptlang.org/docs/handbook/2/basic-types.html)
- [Choosing compiler options](https://www.typescriptlang.org/docs/handbook/modules/guides/choosing-compiler-options)
