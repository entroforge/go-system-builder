---
name: acceptance-and-handoff
description: Use when a valid clean round enters acceptance, release audit, or human release handoff
category: methodology
version: 1.2.0
---
# Acceptance And Handoff

## Authority
Automation stops before squash merge, main-branch publication, or formal release. Runtime authority lives in `docs/loop-definition.json`; the stage contract lives in `docs/agent-protocol.md`; the method summary is inlined below.

## Entry Conditions
- A `clean_round_valid` evidence record exists and is referenced by ID and hash.
- The Loop is in top-level `acceptance` or `release_audit`.
- The locked REQ fingerprint still matches the runtime baseline.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Clean-round record | runtime evidence ID/hash | acceptance and audit anchor |
| Locked baseline | runtime `bound_req` + REQ fingerprint | ACC source of truth |
| ACC inputs | REQ acceptance criteria, Closing Contracts, evidence map | assemble acceptance evidence |
| Release architecture | design docs, deployment config, migration/rollback | release audit scope |
| Operations readiness | runbooks, observability, on-call | operational gate |
| Evidence fingerprints | runtime `evidence[]` | detect stale acceptance evidence |

## Procedure
1. Re-verify the clean-round record is still `valid`. If stale, invoke `clean-round-evaluation` to require a new complete round.
2. Assemble the ACC: map every REQ acceptance criterion and every TASK Closing Contract to its PASS/N/A evidence ID from the clean-round record.
3. Confirm every referenced evidence ID is `valid` and belongs to the same baseline and review round.
4. Request TR-015 (`acceptance_completed` → `release_audit`) once the ACC is complete.
5. Execute the release architecture audit: architecture matches locked design, migration plan present and reversible, rollback plan present, operational readiness satisfied.
6. Classify audit findings: correctable → return to a complete review round; blocking → record and pause; integrity → pause for human.
7. On audit pass, prepare the `awaiting_human_release` handoff: clean-round ID/hash, ACC ID/hash, audit evidence ID, baseline generation, required human actions, explicit statement that automation stops.
8. Request TR-017 (`audit_approved` → `awaiting_human_release`). Do not request, attempt, or proxy squash merge, publication, deployment, or formal release.

## Outputs
- ACC evidence record (criterion → evidence mapping, baseline, clean-round reference).
- Release architecture audit evidence.
- `awaiting_human_release` handoff record with all referenced evidence IDs and hashes.

## Exit Conditions
- Release audit passes and control is explicitly handed to a human at terminal `awaiting_human_release`.

## Stop Conditions
Stop immediately and surface to the human if any of:
- The clean-round evidence is missing, invalid, or superseded.
- ACC cannot map a required acceptance criterion to valid evidence.
- Release audit has a blocking finding (architecture drift, missing migration/rollback, operations not ready).
- An irreversible action (merge, publish, deploy, release) is requested without separate human approval.
- The locked REQ fingerprint no longer matches the runtime baseline.

## Non-Goals
- Do not merge, publish, deploy, or formally release — acceptance never performs release. Hook Policy does not block these commands, so the constraint is procedural: reaching S11 raises a `release_ready` Gateway package and the human decides whether release proceeds.
- Do not re-evaluate the clean round — delegate to `clean-round-evaluation`.
- Do not modify the locked REQ or specifications to make acceptance pass.

## Inlined Methodology

The Loop uses a layered state model: a top-level Loop state plus constrained phase machines for complex stages plus independent Agent/TASK/BUG lifecycles. The eleven top-level states follow the single-direction main trunk `inactive -> planning -> document_verification -> building -> verification -> acceptance -> release_audit -> awaiting_human_release`, plus the `paused` and `aborted` terminals. Correction loops never bypass the trunk. `awaiting_human_release` is terminal for Loop automation: Hooks must strong-block squash merge, production deployment, and formal release when initiated by Loop automation. Invalid transition policy: undefined or guard-failing events do not change state, execute no side effect, record a rejected event, report the failed guard, and pause on repeated runtime-integrity failure. Idempotency uses compare-and-swap or equivalent revision checks; one committed transition per runtime revision; idempotency keys for state-changing actions; stale writers reload instead of overwriting. The 16 preservation invariants (INV-001..INV-016) map to enforcement layers (schema, hook, guard, combined). ACC and release architecture audit reference the clean-round record by ID and hash; later changes do not rewrite the record.
