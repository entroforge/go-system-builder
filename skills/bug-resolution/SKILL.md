---
name: bug-resolution
description: Use when Delivery Verifier, QA, or E2E Browser finds a blocking defect, or when an accepted BUG needs repair
category: methodology
version: 1.1.0
---
# Bug Resolution

## Authority
The main session turns findings into canonical BUG reports before assigning repairs. Runtime authority lives in `docs/loop-definition.json`; the S8/S9 stage contracts live in `docs/agent-protocol.md`; the method summary is inlined below.

## Entry Conditions
- A blocking finding exists from `verification.delivery`, `verification.qa`, `verification.e2e_browser`, or a failed `targeted_reverification`, **or**
- An accepted canonical BUG is ready for repair.
- The finding is reproducible or backed by deterministic evidence.
- The Loop is in top-level `bug_resolution`.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Finding evidence | `docs/reports/...` referenced by the finding | observed gap, severity, scope |
| Affected REQ/contract/TASK | traceability links from the finding | Closing Contract and forbidden scope |
| Existing BUGs | runtime `entities.bugs[]` | deduplication |
| Review round + baseline | runtime `lifecycle` | freeze scope for re-verification |
| Repair limits | runtime BUG counters | pause before exceeding attempts |
| Original finder | finding `responsibility_id` | targeted re-verification owner |

## Procedure
1. S8: read each finding; reproduce it or confirm deterministic evidence; record the before-fix failing evidence.
2. S8: investigate root cause from reproduction, browser traces/logs when applicable, and implementation/spec evidence. Distinguish implementation defect, test defect, specification conflict, environment/dependency failure, and expected behavior.
   - Hard step: ask why E2E did not cover or fail this gap. Check the project E2E scenario inventory (when present) and run `loop-harness e2e-coverage --inventory <path> --root .`. If a contracted behavior broke and the corresponding CT/AC did not go red, the Closing Contract **must** include a **coverage gap** item (raise fidelity or add a system test) — investigation is incomplete without that answer.
3. S8: group findings into canonical BUGs by `same user-visible contradiction AND same root cause AND compatible Closing Contract`. Never merge just because they touch the same file.
4. S8: draft each canonical BUG with: source finding IDs, affected clauses, reproducible actual vs expected, evidence-supported root cause, severity, repair scope, forbidden scope, Closing Contract (including any E2E coverage-gap remediation), before-fix evidence, original finder, required Best Practices.
5. S8: submit for main-session review. Keep these outcomes distinct: an insufficient BUG report returns to investigation; `accepted` enters repair; a final `rejected_no_product_change` disposition means expected behavior, a test-only issue, or a transient environment condition with no product/specification correction; `duplicate` links to its canonical BUG; `req_change_required` pauses; `spec_rework_required` returns to `planning.rework`.
6. S9: for each accepted BUG, request `repair_readback` → `fixing` transition and run `two-phase-activation` for the repair Builder, who reads `BUG → TASK → CONTRACTS → REQ → DESIGN → RULES`.
7. S9: after Builder reports fix, invoke `impact-analysis` to compute the change-impact record and mark affected historical PASS evidence `invalid`.
8. S9: route to `targeted_reverification`. The original finder checks every Closing Contract assertion, before/after reproduction, scope compliance, regression evidence, and root-cause elimination.
9. S9: act on the targeted result: `pass` → BUG may close; `fail` → return to S8 investigation, increment same-contract failure counter; `blocked` → preserve BUG state; `scope_changed` → invalidate activation and return to BUG review.
10. S9: when all accepted BUGs close, enter `ready_for_full_review`, the persisted S9-to-S7 handoff checkpoint. It performs no work and only permits TR-012 back to `verification.delivery` for a new complete Delivery + QA + E2E round; targeted re-verification never creates a clean round.
11. Use TR-022 only when the entire S8 batch has no accepted BUG and every finding is final-rejected without product/specification change or duplicate-linked to a canonical BUG with no remaining repair. A duplicate of an open BUG follows that BUG's repair path; a specification or REQ change must not use TR-022.

## Outputs
- Accepted/rejected/duplicate canonical BUG records with Closing Contracts.
- Repair task assignment, activation evidence, fix evidence.
- Change-impact record and evidence invalidation set (from `impact-analysis`).
- Targeted re-verification result bound to the original finder.

## Exit Conditions
- Every blocking finding has a final disposition: accepted BUGs are `closed` with targeted `pass`, while no-repair findings are final-rejected or duplicate-linked with the required rationale; specification and REQ changes have their separate routes.

## Stop Conditions
Stop immediately and surface to the human if any of:
- Root cause is not supported by reproduction or deterministic evidence.
- The BUG requires a REQ change (`req_change_required`) — pause.
- A proposed final no-repair disposition lacks evidence that product behavior and specification already agree.
- A configured repair limit is reached — pause; never auto-close or downgrade.
- The original finder is unavailable and no continuity replacement exists.

## Non-Goals
- Do not assign repair before the BUG is `accepted`.
- Do not count targeted re-verification as a clean round.
- Do not let the repair Builder change the BUG Closing Contract or source specification.
- Do not restore invalidated PASS evidence — invalid evidence is immutable.

## Inlined Methodology

The BUG lifecycle is one ordered chain: finding -> S8 root-cause investigation -> S8 canonical BUG review -> S9 repair read-back and activation -> S9 fix -> impact analysis and evidence invalidation -> original-finding-responsibility targeted re-verification -> BUG close -> `ready_for_full_review` handoff -> new complete Delivery + QA + E2E Browser round -> clean-round evaluation. An insufficient report returns to investigation; a final no-product-change rejection and a duplicate whose canonical BUG has no remaining repair may use the single no-repair exit, while specification and REQ changes use their own exits. Group findings into canonical BUGs only when `same user-visible contradiction AND same root cause AND compatible Closing Contract`; never merge solely because they touch the same file. Evidence states are `valid`, `invalid`, `superseded`; invalid evidence is never revived and replacement evidence gets a new ID. Change classification distinguishes ten types (requirement, design, contract, task, implementation, test, configuration, database, dependency, documentation_only); documentation_only is valid only when impact analysis proves no normative clause, executable instruction, fingerprinted activation input, or evidence meaning changed. Conservative escalation expands impact when traceability links are absent, trust boundaries are crossed, browser/runtime evidence contradicts unit-level evidence, or test/evidence provenance is unknown. Targeted re-verification is performed by the original finding responsibility and may close a BUG but never creates a clean round. ACC and release architecture audit reference the clean-round record by ID and hash; later changes do not rewrite the record.
