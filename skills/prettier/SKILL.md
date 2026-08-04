---
name: prettier
description: Use when Prettier configuration, formatting automation, generated formatting, or formatting conflicts change
category: best-practice
version: 0.3.0
---
# Prettier
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; formatter output does not establish behavioral correctness.
## Applicability
Apply to Prettier configuration, formatting commands, formatter integration, or formatting-only diffs.
## Required Inputs
Read formatter configuration, editor and CI commands, ignore rules, and conflicting tool configuration.
## Quality Criteria
Keep formatting deterministic, separate mechanical churn from behavior changes, and align formatter ownership with linting.
## Outputs
Reproducible formatting configuration or scoped formatting evidence.
## N/A Criteria
N/A when no formatter-managed files or configuration are affected.
## Stop Conditions
Stop on conflicting formatters, repository-wide unscoped churn, or generated-file corruption.
## Non-Goals
Do not treat formatting success as code-quality or test evidence.

## Operating Procedure
1. Locate the effective repository-local configuration and ignore file from the changed file's directory; identify any applicable override.
2. Preserve parser inference. Add an override only for a documented file class that cannot be inferred correctly.
3. Format the smallest intended scope, review the mechanical diff, and run the repository check command in non-write mode.
4. Coordinate ESLint so formatting has a single owner and formatter churn cannot conceal behavioral edits.

## Evidence Checklist
- Effective config/override and ignored-file scope for the changed paths.
- Exact format/check command and confirmation that output is deterministic.
- Mechanical diff separated from semantic changes when both are necessary.

## Common Failure Modes
- A global parser breaks unrelated file formats.
- A developer-global config changes output across machines.
- Repository-wide formatting is included in an unrelated defect repair.

## Primary Sources
- [Prettier configuration](https://prettier.io/docs/configuration)
- [Prettier integration with linters](https://prettier.io/docs/integrating-with-linters)
