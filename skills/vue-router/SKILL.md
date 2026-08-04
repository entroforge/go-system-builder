---
name: vue-router
description: Use when Vue Router routes, navigation guards, route state, or page transitions change
category: best-practice
version: 0.3.0
---
# Vue Router
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; the approved user flow owns navigation intent.
## Applicability
Apply to route records, navigation, guards, route parameters, or route-driven page state.
## Required Inputs
Read the route map, page ownership, navigation requirements, auth policy, and user flows.
## Quality Criteria
Keep route contracts explicit, guard behavior deterministic, navigation accessible, and deep links recoverable.
## Outputs
One route-safe implementation or scoped navigation review conclusion.
## N/A Criteria
N/A when no client-side routing or route-dependent behavior is affected.
## Stop Conditions
Stop on ambiguous route ownership, redirect loops, or an unguarded authorization boundary.
## Non-Goals
Do not substitute URL changes for required user-flow interaction evidence.

## Operating Procedure
1. Map the affected user-flow entry, control, route name, route parameters/query, destination state, and ownership of each route record.
2. Choose the smallest guard scope: global/meta for shared policy, `beforeEnter` for route admission, and component leave/update guards for lifecycle-specific protection.
3. Make every guard return exactly one outcome: continue, cancel, or redirect. A redirect carries an explicit loop-prevention condition.
4. Exercise normal navigation, denied navigation, refresh/deep link, and back/forward through the controls named in the flow.

## Evidence Checklist
- Route-record and route-meta change with parsing/default rules for parameters and query.
- Guard outcome matrix, including unauthenticated, unauthorized, and unsaved-work behavior where applicable.
- Browser evidence that navigation was initiated by the declared control, not a direct URL jump.

## Common Failure Modes
- A global guard acquires page data or mutates stores beyond navigation policy.
- Redirects use a path string without excluding the destination and loop indefinitely.
- Route params are treated as trusted typed state without parse/validation.

## Primary Sources
- [Vue Router navigation guards](https://router.vuejs.org/guide/advanced/navigation-guards.html)
- [Vue Router routes and navigation](https://router.vuejs.org/guide/essentials/navigation.html)
