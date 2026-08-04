---
name: eslint
description: Use when ESLint rules, lint failures, JavaScript or TypeScript static-analysis configuration change
category: best-practice
version: 0.3.0
---
# ESLint
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; product rules remain in contracts and tests.
## Applicability
Apply to ESLint configuration, rule changes, lint failures, or static-analysis suppressions.
## Required Inputs
Read project lint configuration, parser and plugin versions, target files, and existing suppression policy.
## Quality Criteria
Keep rules intentional, diagnostics actionable, suppressions narrow, and lint execution reproducible.
## Outputs
Lint-clean scoped changes or a review conclusion with command evidence.
## N/A Criteria
N/A when the changed code is outside the configured lint surface.
## Stop Conditions
Stop on a rule conflict, broad disable, or formatter-driven semantic change.
## Non-Goals
Do not encode product rules only in lint configuration.

## Operating Procedure
1. Reproduce the diagnostic with the repository configuration and identify its class: correctness, security, maintainability, or style.
2. Fix code when the rule exposes a real violation; change a rule only when its project-wide intent, parser/plugin compatibility, and scope are explicit.
3. Keep suppressions local and justified. Separate bulk mechanical fixes from functional diffs.
4. Run lint without write mode in CI-equivalent validation and inspect ignored/generated-file scope.

## Evidence Checklist
- Effective configuration, parser/plugin versions, affected file globs, and lint command.
- Rationale for any rule-level change or suppression, including expiry/removal plan.
- Proof Prettier owns formatting where both tools apply.

## Common Failure Modes
- Disabling a rule globally to fix one file.
- Rules silently do not apply because the flat-config file match or parser is wrong.
- `--fix` changes behavior and is reviewed as a cosmetic-only diff.

## Primary Sources
- [ESLint configuration](https://eslint.org/docs/latest/use/configure/)
- [ESLint rule configuration](https://eslint.org/docs/latest/use/configure/rules)
