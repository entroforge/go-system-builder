---
name: vue-devtools
description: Use when Vue DevTools inspection, component-state debugging, performance profiling, or development diagnostics are needed
category: best-practice
version: 0.3.0
---
# Vue DevTools
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; DevTools observations must be converted into reproducible test evidence.
## Applicability
Apply when Vue component hierarchy, reactive state, emitted events, routing, or browser performance needs diagnosis.
## Required Inputs
Read the observed symptom, reproduction path, component/store ownership, and production-debug policy.
## Quality Criteria
Inspect the smallest relevant runtime surface, preserve reproduction evidence, and remove development-only diagnostics from production paths.
## Outputs
One diagnosis record, performance observation, or targeted debugging conclusion.
## N/A Criteria
N/A when the issue can be demonstrated and resolved without Vue runtime inspection.
## Stop Conditions
Stop on inaccessible reproduction, sensitive data exposure, or a diagnosis without observable evidence.
## Non-Goals
Do not use DevTools observations as a replacement for automated verification.

## Operating Procedure
1. Start with an approved reproduction route and record browser, route, persona, and visible symptom before inspecting internals.
2. Inspect the smallest explanatory surface: component tree/props/emits, Pinia action/state, router state, or performance timeline.
3. Form one falsifiable cause hypothesis, change only the relevant boundary, and reproduce the resolved state.
4. Convert the observation into a Vitest or Playwright regression check; remove temporary diagnostics and protect sensitive values.

## Evidence Checklist
- Reproduction route, expected/observed visible outcome, and the inspected runtime surface.
- State/event/route/render evidence supporting the diagnosis.
- Automated regression test or a justified reason why it cannot be automated.

## Common Failure Modes
- Browsing DevTools first and reverse-engineering a story that no user can reproduce.
- Treating a transient component-state screenshot as a root-cause conclusion.
- Leaving debug logging, exposed store data, or dev-only tooling in production paths.

## Primary Sources
- [Vue tooling and browser DevTools](https://vuejs.org/guide/scaling-up/tooling.html#browser-devtools)
- [Vue DevTools guide](https://devtools.vuejs.org/guide/)
